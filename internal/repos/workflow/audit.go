package workflowrepos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
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

func InspectAuditCommit(ctx context.Context, localPath, expectedBranch, baseCommit, auditedCommit string) (AuditCommitEvidence, error) {
	runner := boundedGitRunner{}
	return InspectAuditCommitWithRunner(ctx, localPath, expectedBranch, baseCommit, auditedCommit, runner)
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
	changes, err := parseAuditFileChangeStatuses(statusBytes)
	if err != nil {
		return nil, err
	}
	changes, err = parseAuditFileChangeCounts(changes, numstatBytes)
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

func parseAuditFileChangeCounts(changes []AuditFileChange, raw []byte) ([]AuditFileChange, error) {
	tokens, err := splitNulTerminated(raw)
	if err != nil {
		return nil, fmt.Errorf("count stream: %w", err)
	}
	if len(changes) == 0 && len(tokens) > 0 {
		return nil, errors.New("extra numstat entries")
	}
	result := make([]AuditFileChange, len(changes))
	copy(result, changes)
	seenIdentity := map[string]struct{}{}
	i := 0
	for idx, change := range changes {
		if i+1 >= len(tokens) {
			return nil, fmt.Errorf("missing numstat entry for %q", change.Path)
		}
		additionsToken := tokens[i]
		deletionsToken := tokens[i+1]
		i += 2
		var additions, deletions int64
		if additionsToken == "-" && deletionsToken == "-" {
			additions = 0
			deletions = 0
		} else {
			additions, err = parseAuditNumstatValue(additionsToken)
			if err != nil {
				return nil, fmt.Errorf("malformed addition count %q for %q: %w", additionsToken, change.Path, err)
			}
			deletions, err = parseAuditNumstatValue(deletionsToken)
			if err != nil {
				return nil, fmt.Errorf("malformed deletion count %q for %q: %w", deletionsToken, change.Path, err)
			}
		}
		var previousPath, path string
		if change.PreviousPath == "" {
			if i >= len(tokens) {
				return nil, fmt.Errorf("numstat entry missing resulting path for %q", change.Path)
			}
			path = tokens[i]
			i++
			previousPath = ""
			if path != change.Path {
				return nil, fmt.Errorf("numstat path disagreement for %q: got %q", change.Path, path)
			}
		} else {
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("numstat entry missing rename/copy paths for %q", change.Path)
			}
			previousPath = tokens[i]
			path = tokens[i+1]
			i += 2
			if previousPath != change.PreviousPath || path != change.Path {
				return nil, fmt.Errorf("numstat rename/copy path disagreement for %q -> %q: got %q -> %q", change.PreviousPath, change.Path, previousPath, path)
			}
		}
		if err := validateAuditPath(path); err != nil {
			return nil, fmt.Errorf("numstat resulting path: %w", err)
		}
		if previousPath != "" {
			if err := validateAuditPath(previousPath); err != nil {
				return nil, fmt.Errorf("numstat previous path: %w", err)
			}
		}
		identity := path
		if previousPath != "" {
			identity = previousPath + "\x00" + path
		}
		if _, ok := seenIdentity[identity]; ok {
			return nil, fmt.Errorf("duplicate numstat identity %q", identity)
		}
		seenIdentity[identity] = struct{}{}
		result[idx].Additions = additions
		result[idx].Deletions = deletions
	}
	if i != len(tokens) {
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
