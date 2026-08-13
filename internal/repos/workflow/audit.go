package workflowrepos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	auditGitTimeout       = 30 * time.Second
	MaxAuditDiffBytes     = 1024 * 1024
	maxAuditMetadataBytes = 256 * 1024
)

var ErrAuditGitOutputTooLarge = errors.New("audit Git output exceeds the configured bound")

type AuditCommitEvidence struct {
	Branch        string            `json:"branch"`
	BaseCommit    string            `json:"base_commit"`
	AuditedCommit string            `json:"audited_commit"`
	ChangedFiles  []string          `json:"changed_files"`
	NameStatus    string            `json:"name_status"`
	DiffStat      string            `json:"diff_stat"`
	CommitLog     string            `json:"commit_log"`
	Diff          string            `json:"diff"`
	FileChanges   []AuditFileChange `json:"file_changes"`
}

// AuditFileChange is a structured, machine-readable description of a file
// change in the audited commit range.
//
// Additions and Deletions are line counts from git diff --numstat. For binary
// files Git reports unavailable counts as 0; a zero value must not be
// interpreted as proof that no bytes changed.
type AuditFileChange struct {
	Path         string
	PreviousPath string
	ChangeType   string
	Additions    int64
	Deletions    int64
}

type AuditGitRunner interface {
	Run(ctx context.Context, directory string, maxBytes int, args ...string) ([]byte, error)
}

const integrationConflictEvidenceVersion = 1

var integrationGitObjectID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// IntegrationConflictEvidence is the canonical runtime evidence returned by
// an external Merge for a mechanically resolved conflict. It is transport
// evidence, not authored planning authority.
type IntegrationConflictEvidence struct {
	Version            int                       `json:"version"`
	AssignmentID       string                    `json:"assignment_id"`
	BaseCommit         string                    `json:"base_commit"`
	ConstituentCommits []string                  `json:"constituent_commits"`
	IntegratedCommit   string                    `json:"integrated_commit"`
	IntegratedParents  []string                  `json:"integrated_parents"`
	Conflicts          []IntegrationConflictPath `json:"conflicts"`
}

type IntegrationConflictPath struct {
	Path     string                  `json:"path"`
	Base     IntegrationConflictBlob `json:"base"`
	Ours     IntegrationConflictBlob `json:"ours"`
	Theirs   IntegrationConflictBlob `json:"theirs"`
	Resolved IntegrationConflictBlob `json:"resolved"`
}

type IntegrationConflictBlob struct {
	Commit string `json:"commit"`
	Mode   string `json:"mode"`
	OID    string `json:"oid"`
}

// ParseIntegrationConflictEvidence accepts only the canonical JSON form. The
// repository verifier performs the stronger assignment and Git-object checks.
func ParseIntegrationConflictEvidence(integrated, encoded string) (IntegrationConflictEvidence, error) {
	if strings.TrimSpace(encoded) != encoded || encoded == "" {
		return IntegrationConflictEvidence{}, errors.New("conflict evidence must be canonical JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var evidence IntegrationConflictEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return IntegrationConflictEvidence{}, fmt.Errorf("decode conflict evidence: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return IntegrationConflictEvidence{}, errors.New("conflict evidence contains multiple JSON values")
		}
		return IntegrationConflictEvidence{}, fmt.Errorf("decode conflict evidence trailer: %w", err)
	}
	canonical, err := json.Marshal(evidence)
	if err != nil || string(canonical) != encoded {
		return IntegrationConflictEvidence{}, errors.New("conflict evidence is not canonical JSON")
	}
	if evidence.Version != integrationConflictEvidenceVersion || !integrationGitObjectID.MatchString(integrated) || evidence.IntegratedCommit != integrated || evidence.AssignmentID == "" || strings.TrimSpace(evidence.AssignmentID) != evidence.AssignmentID || evidence.BaseCommit == "" || len(evidence.ConstituentCommits) == 0 || len(evidence.IntegratedParents) == 0 || len(evidence.Conflicts) == 0 {
		return IntegrationConflictEvidence{}, errors.New("conflict evidence identity is incomplete")
	}
	if !integrationGitObjectID.MatchString(evidence.BaseCommit) || !integrationGitObjectID.MatchString(evidence.IntegratedCommit) {
		return IntegrationConflictEvidence{}, errors.New("conflict evidence commit identity is invalid")
	}
	for _, commit := range append(append([]string{}, evidence.ConstituentCommits...), evidence.IntegratedParents...) {
		if !integrationGitObjectID.MatchString(commit) {
			return IntegrationConflictEvidence{}, errors.New("conflict evidence parent identity is invalid")
		}
	}
	seenPaths := map[string]bool{}
	previousPath := ""
	for _, conflict := range evidence.Conflicts {
		if !validGitPath(conflict.Path) || seenPaths[conflict.Path] || (previousPath != "" && conflict.Path <= previousPath) {
			return IntegrationConflictEvidence{}, errors.New("conflict evidence path is invalid")
		}
		seenPaths[conflict.Path] = true
		previousPath = conflict.Path
		for _, object := range []IntegrationConflictBlob{conflict.Base, conflict.Ours, conflict.Theirs, conflict.Resolved} {
			if !integrationGitObjectID.MatchString(object.Commit) || !validGitTreeObject(object.Mode, object.OID) {
				return IntegrationConflictEvidence{}, errors.New("conflict evidence object identity is invalid")
			}
		}
	}
	return evidence, nil
}

func validGitPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.ContainsAny(path, "\x00\r\n\t") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validGitMode(mode string) bool {
	if len(mode) != 6 {
		return false
	}
	for _, char := range mode {
		if char < '0' || char > '7' {
			return false
		}
	}
	return true
}

func validGitTreeObject(mode, oid string) bool {
	return (mode == "" && oid == "") || (validGitMode(mode) && integrationGitObjectID.MatchString(oid))
}

type auditNumstatEntry struct {
	Path         string
	PreviousPath string
	Additions    int64
	Deletions    int64
}

type boundedGitRunner struct{}

type auditBoundedBuffer struct {
	limit int
	data  bytes.Buffer
}

func (b *auditBoundedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && b.data.Len()+len(p) > b.limit {
		remaining := b.limit - b.data.Len()
		if remaining > 0 {
			_, _ = b.data.Write(p[:remaining])
		}
		return 0, ErrAuditGitOutputTooLarge
	}
	return b.data.Write(p)
}

func (boundedGitRunner) Run(ctx context.Context, directory string, maxBytes int, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, auditGitTimeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, "git", args...)
	command.Dir = directory
	stdout := &auditBoundedBuffer{limit: maxBytes}
	stderr := &auditBoundedBuffer{limit: 64 * 1024}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, ErrAuditGitOutputTooLarge) {
			return nil, ErrAuditGitOutputTooLarge
		}
		detail := strings.TrimSpace(stderr.data.String())
		if detail != "" {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, detail)
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return append([]byte(nil), stdout.data.Bytes()...), nil
}

func (boundedGitRunner) runMergeTree(ctx context.Context, directory string, base, ours, theirs string) ([]string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, auditGitTimeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, "git", "merge-tree", "--write-tree", "--no-messages", "--name-only", "-z", "--merge-base", base, ours, theirs)
	command.Dir = directory
	stdout := &auditBoundedBuffer{limit: 64 * 1024}
	stderr := &auditBoundedBuffer{limit: 64 * 1024}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || (exitErr.ExitCode() != 1 && exitErr.ExitCode() != 0) {
			detail := strings.TrimSpace(stderr.data.String())
			if detail != "" {
				return nil, fmt.Errorf("git merge-tree: %w: %s", err, detail)
			}
			return nil, fmt.Errorf("git merge-tree: %w", err)
		}
	}
	parts := bytes.Split(stdout.data.Bytes(), []byte{0})
	if len(parts) == 0 || len(parts[0]) != 40 || !integrationGitObjectID.MatchString(string(parts[0])) {
		return nil, errors.New("Git merge-tree output does not contain a valid result tree")
	}
	paths := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		if len(part) == 0 {
			continue
		}
		path := string(part)
		if !validGitPath(path) {
			return nil, errors.New("Git merge-tree output contains an invalid conflict path")
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func InspectAuditCommit(ctx context.Context, localPath, expectedBranch, baseCommit, auditedCommit string) (AuditCommitEvidence, error) {
	runner := boundedGitRunner{}
	return InspectAuditCommitWithRunner(ctx, localPath, expectedBranch, baseCommit, auditedCommit, runner)
}

// VerifyIntegrationRepository grounds an external Merge claim in the target
// repository and the exact immutable Integration Assignment.
func VerifyIntegrationRepository(ctx context.Context, localPath, assignmentID, branch, base, integrated string, bound, omitted []string, conflictResolution, conflictEvidence string) (string, error) {
	return verifyIntegrationRepository(ctx, localPath, assignmentID, branch, base, integrated, bound, omitted, conflictResolution, conflictEvidence)
}

func verifyIntegrationRepository(ctx context.Context, localPath, assignmentID, branch, base, integrated string, bound, omitted []string, conflictResolution, conflictEvidence string) (string, error) {
	if strings.TrimSpace(assignmentID) == "" || strings.TrimSpace(assignmentID) != assignmentID {
		return "Integration Assignment identity is required", fmt.Errorf("invalid assignment identity")
	}
	runner := boundedGitRunner{}
	check := func(args ...string) error {
		_, err := runner.Run(ctx, localPath, 64*1024, args...)
		return err
	}
	if err := check("cat-file", "-e", integrated+"^{commit}"); err != nil {
		return "integrated commit does not exist", err
	}
	branchHead, err := runner.Run(ctx, localPath, 64*1024, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil || strings.TrimSpace(string(branchHead)) != integrated {
		return "integrated commit is not the bound target branch tip", fmt.Errorf("branch tip mismatch")
	}
	if err := check("merge-base", "--is-ancestor", base, integrated); err != nil {
		return "integrated commit is not based on the bound common baseline", err
	}
	for _, commit := range bound {
		if err := check("cat-file", "-e", commit+"^{commit}"); err != nil {
			return "bound accepted commit does not exist", err
		}
		if err := check("merge-base", "--is-ancestor", commit, integrated); err != nil {
			return "integrated commit does not preserve a bound accepted commit", err
		}
	}
	for _, commit := range omitted {
		if err := check("cat-file", "-e", commit+"^{commit}"); err != nil {
			return "omitted Program constituent commit cannot be resolved", err
		}
		if err := check("merge-base", "--is-ancestor", commit, integrated); err == nil {
			return "integrated commit includes an omitted Program constituent", fmt.Errorf("omitted commit %s is an ancestor", commit)
		}
	}
	if conflictResolution == "material_conflict" {
		return "material merge conflict cannot be admitted", fmt.Errorf("material conflict")
	}
	if conflictResolution != "clean" && conflictResolution != "mechanically_resolved" {
		return "merge conflict resolution state is invalid", fmt.Errorf("invalid conflict resolution")
	}
	if conflictResolution == "clean" {
		if conflictEvidence != "" {
			return "clean integration must not carry conflict evidence", fmt.Errorf("unexpected conflict evidence")
		}
		return "repository preservation verified", nil
	}
	evidence, err := ParseIntegrationConflictEvidence(integrated, conflictEvidence)
	if err != nil {
		return "mechanically resolved conflict evidence is not factual canonical evidence", err
	}
	if evidence.AssignmentID != assignmentID {
		return "conflict evidence does not bind the Integration Assignment", fmt.Errorf("assignment mismatch")
	}
	if evidence.BaseCommit != base || !equalStrings(evidence.ConstituentCommits, bound) {
		return "conflict evidence does not bind the repository baseline and constituents", fmt.Errorf("integration identity mismatch")
	}
	parents, err := integrationCommitParents(ctx, runner, localPath, integrated)
	if err != nil || !equalStrings(parents, evidence.IntegratedParents) {
		return "conflict evidence does not match the integrated commit parents", fmt.Errorf("integrated parent mismatch")
	}
	for _, conflict := range evidence.Conflicts {
		if !containsString(bound, conflict.Ours.Commit) || !containsString(bound, conflict.Theirs.Commit) || conflict.Ours.Commit != parents[0] || !containsString(parents[1:], conflict.Theirs.Commit) || conflict.Base.Commit != base {
			return "conflict evidence source commits are not the bound integration", fmt.Errorf("conflict source mismatch")
		}
		for _, source := range []IntegrationConflictBlob{conflict.Base, conflict.Ours, conflict.Theirs} {
			actual, err := integrationTreeEntry(ctx, runner, localPath, source.Commit, conflict.Path)
			if err != nil || actual.Mode != source.Mode || actual.OID != source.OID {
				return "conflict evidence source tree entry does not match Git", fmt.Errorf("source tree mismatch for %s", conflict.Path)
			}
		}
		if conflictBlobIdentity(conflict.Base) == conflictBlobIdentity(conflict.Ours) || conflictBlobIdentity(conflict.Base) == conflictBlobIdentity(conflict.Theirs) || conflictBlobIdentity(conflict.Ours) == conflictBlobIdentity(conflict.Theirs) {
			return "conflict evidence does not identify three divergent Git conflict stages", fmt.Errorf("conflict stages are not divergent for %s", conflict.Path)
		}
		conflictedPaths, err := runner.runMergeTree(ctx, localPath, base, conflict.Ours.Commit, conflict.Theirs.Commit)
		if err != nil || !containsString(conflictedPaths, conflict.Path) {
			return "Git does not report the supplied path as an actual merge conflict", fmt.Errorf("merge conflict mismatch for %s: %v", conflict.Path, err)
		}
		actual, err := integrationTreeEntry(ctx, runner, localPath, integrated, conflict.Path)
		if err != nil || actual.Mode != conflict.Resolved.Mode || actual.OID != conflict.Resolved.OID || conflict.Resolved.Commit != integrated {
			return "conflict evidence resolved tree entry does not match Git", fmt.Errorf("resolved tree mismatch for %s", conflict.Path)
		}
	}
	return "repository preservation verified", nil
}

func conflictBlobIdentity(blob IntegrationConflictBlob) string {
	return blob.Mode + ":" + blob.OID
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func integrationCommitParents(ctx context.Context, runner AuditGitRunner, localPath, integrated string) ([]string, error) {
	output, err := runner.Run(ctx, localPath, 64*1024, "rev-list", "--parents", "-n", "1", integrated)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 || fields[0] != integrated {
		return nil, errors.New("integrated commit parent list is invalid")
	}
	return fields[1:], nil
}

func integrationTreeEntry(ctx context.Context, runner AuditGitRunner, localPath, commit, path string) (IntegrationConflictBlob, error) {
	output, err := runner.Run(ctx, localPath, 64*1024, "ls-tree", "-z", "--full-tree", commit, "--", path)
	if err != nil {
		return IntegrationConflictBlob{}, err
	}
	entries := bytes.Split(output, []byte{0})
	var found IntegrationConflictBlob
	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}
		parts := bytes.SplitN(entry, []byte{'\t'}, 2)
		if len(parts) != 2 || string(parts[1]) != path {
			continue
		}
		fields := strings.Fields(string(parts[0]))
		if len(fields) != 3 || !validGitMode(fields[0]) || fields[1] == "" || !integrationGitObjectID.MatchString(fields[2]) {
			return IntegrationConflictBlob{}, errors.New("Git tree entry is invalid")
		}
		if found.OID != "" {
			return IntegrationConflictBlob{}, errors.New("Git tree path is not unique")
		}
		found.Mode, found.OID = fields[0], fields[2]
	}
	if found.OID == "" {
		return IntegrationConflictBlob{}, nil
	}
	return found, nil
}

func InspectAuditCommitWithRunner(ctx context.Context, localPath, expectedBranch, baseCommit, auditedCommit string, runner AuditGitRunner) (AuditCommitEvidence, error) {
	if runner == nil {
		return AuditCommitEvidence{}, fmt.Errorf("audit Git runner is required")
	}
	baseCommit = strings.ToLower(strings.TrimSpace(baseCommit))
	auditedCommit = strings.ToLower(strings.TrimSpace(auditedCommit))
	if len(baseCommit) != 40 || len(auditedCommit) != 40 {
		return AuditCommitEvidence{}, fmt.Errorf("base and audited commits must be full 40-character SHAs")
	}
	preflightRunner := auditPreflightRunner{runner: runner}
	preflight := VerifyExecutionPreflightWithRunner(ctx, localPath, expectedBranch, auditedCommit, preflightRunner)
	if !preflight.OK {
		return AuditCommitEvidence{}, fmt.Errorf("%s: %s", preflight.BlockerCode, preflight.BlockerText)
	}
	if _, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "cat-file", "-e", auditedCommit+"^{commit}"); err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("audited commit does not exist: %w", err)
	}
	if _, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "merge-base", "--is-ancestor", baseCommit, auditedCommit); err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("audited commit is not descended from the Run base commit: %w", err)
	}
	rangeSpec := baseCommit + ".." + auditedCommit
	nameStatusBytes, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "diff", "--name-status", "--no-renames", rangeSpec)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("capture changed files: %w", err)
	}
	nameStatus := strings.TrimSpace(string(nameStatusBytes))
	changedFiles := parseChangedFiles(nameStatus)
	if len(changedFiles) == 0 {
		return AuditCommitEvidence{}, fmt.Errorf("audited commit range contains no changes")
	}
	diffStatBytes, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "diff", "--stat", "--no-renames", rangeSpec)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("capture diff stat: %w", err)
	}
	commitLogBytes, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "log", "--format=%H%x09%an%x09%aI%x09%s", rangeSpec)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("capture commit log: %w", err)
	}
	diffBytes, err := runner.Run(ctx, localPath, MaxAuditDiffBytes, "diff", "--binary", "--no-ext-diff", "--no-renames", rangeSpec)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("capture audit diff: %w", err)
	}
	structuredStatusBytes, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "diff", "--name-status", "-z", "--find-renames", "--find-copies", rangeSpec)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("capture structured changed-file statuses: %w", err)
	}
	structuredNumstatBytes, err := runner.Run(ctx, localPath, maxAuditMetadataBytes, "diff", "--numstat", "-z", "--find-renames", "--find-copies", rangeSpec)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("capture structured changed-file counts: %w", err)
	}
	fileChanges, err := parseAuditFileChanges(structuredStatusBytes, structuredNumstatBytes)
	if err != nil {
		return AuditCommitEvidence{}, fmt.Errorf("parse structured changed-file evidence: %w", err)
	}
	return AuditCommitEvidence{
		Branch:        preflight.CurrentBranch,
		BaseCommit:    baseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles:  changedFiles,
		NameStatus:    nameStatus,
		DiffStat:      strings.TrimSpace(string(diffStatBytes)),
		CommitLog:     strings.TrimSpace(string(commitLogBytes)),
		Diff:          string(diffBytes),
		FileChanges:   fileChanges,
	}, nil
}

type auditPreflightRunner struct {
	runner AuditGitRunner
}

func (r auditPreflightRunner) Run(ctx context.Context, directory string, args ...string) ([]byte, error) {
	return r.runner.Run(ctx, directory, maxAuditMetadataBytes, args...)
}

func parseChangedFiles(nameStatus string) []string {
	if strings.TrimSpace(nameStatus) == "" {
		return []string{}
	}
	seen := map[string]struct{}{}
	var files []string
	for _, line := range strings.Split(nameStatus, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		path := strings.TrimSpace(parts[len(parts)-1])
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	sort.Strings(files)
	return files
}

func parseAuditFileChanges(statusBytes, numstatBytes []byte) ([]AuditFileChange, error) {
	if len(statusBytes) == 0 {
		return nil, errors.New("structured status evidence is empty")
	}
	if len(numstatBytes) == 0 {
		return nil, errors.New("structured numstat evidence is empty")
	}
	changes, err := parseAuditFileChangeStatuses(statusBytes)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, errors.New("structured status evidence contains no changes")
	}
	entries, err := parseAuditNumstat(numstatBytes)
	if err != nil {
		return nil, fmt.Errorf("count stream: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("structured numstat evidence contains no changes")
	}
	changes, err = joinAuditFileChangeCounts(changes, entries)
	if err != nil {
		return nil, err
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].PreviousPath < changes[j].PreviousPath
	})
	return changes, nil
}

func parseAuditFileChangeStatuses(raw []byte) ([]AuditFileChange, error) {
	tokens, err := splitNulTerminated(raw)
	if err != nil {
		return nil, fmt.Errorf("status stream: %w", err)
	}
	var changes []AuditFileChange
	seenIdentity := map[string]struct{}{}
	seenResulting := map[string]struct{}{}
	i := 0
	for i < len(tokens) {
		status := tokens[i]
		i++
		if status == "" {
			return nil, errors.New("status stream contains empty status token")
		}
		switch status {
		case "A", "M", "D", "T":
			if i >= len(tokens) {
				return nil, errors.New("status stream is missing resulting path")
			}
			path := tokens[i]
			i++
			if err := validateAuditPath(path); err != nil {
				return nil, fmt.Errorf("status stream resulting path: %w", err)
			}
			if _, ok := seenResulting[path]; ok {
				return nil, fmt.Errorf("duplicate resulting path %q", path)
			}
			seenResulting[path] = struct{}{}
			if _, ok := seenIdentity[path]; ok {
				return nil, fmt.Errorf("duplicate status identity %q", path)
			}
			seenIdentity[path] = struct{}{}
			changes = append(changes, AuditFileChange{Path: path, ChangeType: mapSingleStatus(status)})
		default:
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				changeType, err := renameOrCopyStatus(status)
				if err != nil {
					return nil, fmt.Errorf("status stream: %w", err)
				}
				if i+1 >= len(tokens) {
					return nil, errors.New("status stream is missing rename/copy paths")
				}
				previousPath := tokens[i]
				path := tokens[i+1]
				i += 2
				if err := validateAuditPath(previousPath); err != nil {
					return nil, fmt.Errorf("status stream previous path: %w", err)
				}
				if err := validateAuditPath(path); err != nil {
					return nil, fmt.Errorf("status stream resulting path: %w", err)
				}
				if previousPath == path {
					return nil, fmt.Errorf("rename/copy old and resulting path are identical: %q", path)
				}
				if _, ok := seenResulting[path]; ok {
					return nil, fmt.Errorf("duplicate resulting path %q", path)
				}
				seenResulting[path] = struct{}{}
				identity := previousPath + "\x00" + path
				if _, ok := seenIdentity[identity]; ok {
					return nil, fmt.Errorf("duplicate status identity %q -> %q", previousPath, path)
				}
				seenIdentity[identity] = struct{}{}
				changes = append(changes, AuditFileChange{
					Path:         path,
					PreviousPath: previousPath,
					ChangeType:   changeType,
				})
			} else {
				return nil, fmt.Errorf("unsupported status %q", status)
			}
		}
	}
	return changes, nil
}

func parseAuditNumstat(raw []byte) ([]auditNumstatEntry, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	rest := raw
	var entries []auditNumstatEntry
	for len(rest) > 0 {
		nulIdx := bytes.IndexByte(rest, '\x00')
		if nulIdx < 0 {
			return nil, errors.New("numstat stream is missing terminating NUL")
		}
		header := rest[:nulIdx]
		rest = rest[nulIdx+1:]
		tab1 := bytes.IndexByte(header, '\t')
		if tab1 < 0 {
			return nil, errors.New("numstat entry missing first tab")
		}
		addToken := string(header[:tab1])
		afterTab1 := header[tab1+1:]
		tab2 := bytes.IndexByte(afterTab1, '\t')
		if tab2 < 0 {
			return nil, errors.New("numstat entry missing second tab")
		}
		delToken := string(afterTab1[:tab2])
		pathField := string(afterTab1[tab2+1:])
		additions, deletions, err := parseAuditNumstatCounts(addToken, delToken)
		if err != nil {
			return nil, err
		}
		var previousPath, path string
		if pathField == "" {
			if len(rest) == 0 {
				return nil, errors.New("numstat rename/copy missing previous path")
			}
			nulIdx := bytes.IndexByte(rest, '\x00')
			if nulIdx < 0 {
				return nil, errors.New("numstat rename/copy missing previous path terminator")
			}
			previousPath = string(rest[:nulIdx])
			rest = rest[nulIdx+1:]
			if len(rest) == 0 {
				return nil, errors.New("numstat rename/copy missing resulting path")
			}
			nulIdx = bytes.IndexByte(rest, '\x00')
			if nulIdx < 0 {
				return nil, errors.New("numstat rename/copy missing resulting path terminator")
			}
			path = string(rest[:nulIdx])
			rest = rest[nulIdx+1:]
		} else {
			path = pathField
		}
		entries = append(entries, auditNumstatEntry{
			Path:         path,
			PreviousPath: previousPath,
			Additions:    additions,
			Deletions:    deletions,
		})
	}
	return entries, nil
}

func parseAuditNumstatCounts(addToken, delToken string) (int64, int64, error) {
	if addToken == "-" && delToken == "-" {
		return 0, 0, nil
	}
	if addToken == "-" || delToken == "-" {
		return 0, 0, errors.New("mixed binary/numeric count forms")
	}
	additions, err := parseAuditNumstatValue(addToken)
	if err != nil {
		return 0, 0, fmt.Errorf("malformed addition count %q: %w", addToken, err)
	}
	deletions, err := parseAuditNumstatValue(delToken)
	if err != nil {
		return 0, 0, fmt.Errorf("malformed deletion count %q: %w", delToken, err)
	}
	return additions, deletions, nil
}

func joinAuditFileChangeCounts(changes []AuditFileChange, entries []auditNumstatEntry) ([]AuditFileChange, error) {
	numstatByIdentity := make(map[string]*auditNumstatEntry, len(entries))
	seenResulting := map[string]struct{}{}
	for i := range entries {
		e := &entries[i]
		if err := validateAuditPath(e.Path); err != nil {
			return nil, fmt.Errorf("numstat resulting path: %w", err)
		}
		if e.PreviousPath != "" {
			if err := validateAuditPath(e.PreviousPath); err != nil {
				return nil, fmt.Errorf("numstat previous path: %w", err)
			}
		}
		if _, ok := seenResulting[e.Path]; ok {
			return nil, fmt.Errorf("duplicate numstat resulting path %q", e.Path)
		}
		seenResulting[e.Path] = struct{}{}
		identity := e.Path
		if e.PreviousPath != "" {
			identity = e.PreviousPath + "\x00" + e.Path
		}
		if _, ok := numstatByIdentity[identity]; ok {
			return nil, fmt.Errorf("duplicate numstat identity %q", identity)
		}
		numstatByIdentity[identity] = e
	}
	result := make([]AuditFileChange, len(changes))
	copy(result, changes)
	for i := range result {
		identity := result[i].Path
		if result[i].PreviousPath != "" {
			identity = result[i].PreviousPath + "\x00" + result[i].Path
		}
		entry, ok := numstatByIdentity[identity]
		if !ok {
			return nil, fmt.Errorf("missing numstat entry for identity %q", identity)
		}
		delete(numstatByIdentity, identity)
		result[i].Additions = entry.Additions
		result[i].Deletions = entry.Deletions
	}
	if len(numstatByIdentity) > 0 {
		return nil, errors.New("extra numstat entries")
	}
	return result, nil
}

func splitNulTerminated(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	parts := strings.Split(string(data), "\x00")
	if parts[len(parts)-1] != "" {
		return nil, errors.New("structured Git output is missing terminating NUL")
	}
	return parts[:len(parts)-1], nil
}

func mapSingleStatus(status string) string {
	switch status {
	case "A":
		return "added"
	case "M":
		return "modified"
	case "D":
		return "deleted"
	case "T":
		return "type_changed"
	default:
		return "unknown"
	}
}

func renameOrCopyStatus(status string) (string, error) {
	prefix := status[0]
	scoreStr := status[1:]
	if scoreStr == "" {
		return "", fmt.Errorf("malformed %s score in status %q", string(prefix), status)
	}
	score, err := strconv.Atoi(scoreStr)
	if err != nil {
		return "", fmt.Errorf("malformed %s score in status %q: %w", string(prefix), status, err)
	}
	if score < 0 || score > 100 {
		return "", fmt.Errorf("%s score %d out of valid range", string(prefix), score)
	}
	if prefix == 'R' {
		return "renamed", nil
	}
	return "copied", nil
}

func parseAuditNumstatValue(v string) (int64, error) {
	if v == "" {
		return 0, errors.New("numeric value is empty")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("negative numeric value")
	}
	return n, nil
}

func validateAuditPath(p string) error {
	if p == "" {
		return errors.New("path is empty")
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("path %q contains invalid UTF-8", p)
	}
	if p[0] == '/' || p[0] == '\\' {
		return fmt.Errorf("path %q has leading separator", p)
	}
	if len(p) >= 2 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' {
		return fmt.Errorf("path %q has Windows drive prefix", p)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("path %q contains backslash", p)
	}
	runes := []rune(p)
	if unicode.IsSpace(runes[0]) || unicode.IsSpace(runes[len(runes)-1]) {
		return fmt.Errorf("path %q has leading or trailing whitespace", p)
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return fmt.Errorf("path %q contains control character", p)
		}
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" {
			return fmt.Errorf("path %q contains empty segment", p)
		}
		if segment == "." || segment == ".." {
			return fmt.Errorf("path %q contains %q segment", p, segment)
		}
	}
	return nil
}
