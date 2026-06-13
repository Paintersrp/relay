package repos

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const gitCaptureTimeout = 5 * time.Second
const gitEvidenceTimeout = 10 * time.Second

type GitSnapshot struct {
	RepoPath        string `json:"repo_path"`
	Branch          string `json:"branch"`
	HeadSHA         string `json:"head_sha"`
	IsGitRepo       bool   `json:"is_git_repo"`
	Dirty           bool   `json:"dirty"`
	StatusPorcelain string `json:"status_porcelain,omitempty"`
	CapturedAt      string `json:"captured_at"`
	CaptureStage    string `json:"capture_stage"`
	Error           string `json:"error,omitempty"`
}

type GitBaselineArtifact struct {
	RunCreated                 *GitSnapshot `json:"run_created,omitempty"`
	AgentStart                 *GitSnapshot `json:"agent_start,omitempty"`
	AuthoritativeBaselineStage string       `json:"authoritative_baseline_stage"`
	AuthoritativeBaselineSHA   string       `json:"authoritative_baseline_sha"`
}

func CaptureGitSnapshot(repoPath, captureStage string) *GitSnapshot {
	snap := &GitSnapshot{
		RepoPath:     repoPath,
		CaptureStage: captureStage,
		CapturedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	if strings.TrimSpace(repoPath) == "" {
		snap.Error = "repo path is empty"
		return snap
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitCaptureTimeout)
	defer cancel()

	headSHA, errHead := gitCommandOutput(ctx, repoPath, "rev-parse", "--verify", "HEAD")
	if errHead != nil {
		snap.Error = fmt.Sprintf("git rev-parse HEAD failed: %v", errHead)
		return snap
	}
	snap.HeadSHA = headSHA
	snap.IsGitRepo = true

	branch, errBranch := gitCommandOutput(ctx, repoPath, "branch", "--show-current")
	if errBranch == nil {
		snap.Branch = branch
	}

	porcelain, errStatus := gitCommandOutput(ctx, repoPath, "status", "--porcelain=v1")
	if errStatus == nil {
		snap.StatusPorcelain = porcelain
		snap.Dirty = strings.TrimSpace(porcelain) != ""
	}

	return snap
}

type GitCommitSummary struct {
	SHA        string `json:"sha"`
	ShortSHA   string `json:"short_sha"`
	Subject    string `json:"subject"`
	AuthorName string `json:"author_name,omitempty"`
	AuthorDate string `json:"author_date,omitempty"`
}

type GitChangeEvidence struct {
	RepoPath              string             `json:"repo_path"`
	Mode                  string             `json:"mode"`
	BaselineSHA           string             `json:"baseline_sha,omitempty"`
	CurrentHeadSHA        string             `json:"current_head_sha,omitempty"`
	Branch                string             `json:"branch,omitempty"`
	StatusPorcelain       string             `json:"status_porcelain,omitempty"`
	Dirty                 bool               `json:"dirty"`
	CommitCount           int                `json:"commit_count"`
	Commits               []GitCommitSummary `json:"commits,omitempty"`
	NameStatus            string             `json:"name_status,omitempty"`
	Stat                  string             `json:"stat,omitempty"`
	Numstat               string             `json:"numstat,omitempty"`
	Patch                 string             `json:"patch,omitempty"`
	UncommittedNameStatus string             `json:"uncommitted_name_status,omitempty"`
	UncommittedStat       string             `json:"uncommitted_stat,omitempty"`
	UncommittedNumstat    string             `json:"uncommitted_numstat,omitempty"`
	UncommittedPatch      string             `json:"uncommitted_patch,omitempty"`
	Warning               string             `json:"warning,omitempty"`
	Error                 string             `json:"error,omitempty"`
	CapturedAt            string             `json:"captured_at"`
}

const (
	EvidenceModeNoChanges                 = "no_changes"
	EvidenceModeUncommittedWorktree       = "uncommitted_worktree"
	EvidenceModeCommittedRange            = "committed_range"
	EvidenceModeMixedCommittedUncommitted = "mixed_committed_and_uncommitted"
	EvidenceModeBaselineUnavailableDirty  = "baseline_unavailable_uncommitted"
	EvidenceModeBaselineUnavailableClean  = "baseline_unavailable_no_changes"
)

func CaptureGitChangeEvidence(repoPath string, baselineSHA string) *GitChangeEvidence {
	ev := &GitChangeEvidence{
		RepoPath:   repoPath,
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
	}

	snap := CaptureGitSnapshot(repoPath, "evidence_capture")
	if snap.Error != "" {
		ev.Error = snap.Error
		return ev
	}

	ev.CurrentHeadSHA = snap.HeadSHA
	ev.Branch = snap.Branch
	ev.Dirty = snap.Dirty
	ev.StatusPorcelain = snap.StatusPorcelain

	baselineKnown := baselineSHA != ""

	if !baselineKnown {
		if snap.Dirty {
			ev.Mode = EvidenceModeBaselineUnavailableDirty
		} else {
			ev.Mode = EvidenceModeBaselineUnavailableClean
		}
		collectUncommittedEvidence(ev, repoPath)
		return ev
	}

	ev.BaselineSHA = baselineSHA

	if snap.HeadSHA == baselineSHA && !snap.Dirty {
		ev.Mode = EvidenceModeNoChanges
		ev.CommitCount = 0
		return ev
	}

	if snap.HeadSHA == baselineSHA && snap.Dirty {
		ev.Mode = EvidenceModeUncommittedWorktree
		collectUncommittedEvidence(ev, repoPath)
		return ev
	}

	if snap.HeadSHA != baselineSHA && !snap.Dirty {
		ev.Mode = EvidenceModeCommittedRange
		collectCommittedRangeEvidence(ev, repoPath, baselineSHA, snap.HeadSHA)
		return ev
	}

	ev.Mode = EvidenceModeMixedCommittedUncommitted
	collectCommittedRangeEvidence(ev, repoPath, baselineSHA, snap.HeadSHA)
	collectUncommittedSideEvidence(ev, repoPath)
	ev.StatusPorcelain = snap.StatusPorcelain
	ev.Dirty = true
	ev.Warning = "Committed changes exist since the run baseline, but the working tree is also dirty. Resolve uncommitted changes before generating a normal audit packet."
	return ev
}

func collectUncommittedEvidence(ev *GitChangeEvidence, repoPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitEvidenceTimeout)
	defer cancel()

	// Combine unstaged and staged diffs for full worktree picture
	if ns, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--name-status"); err == nil {
		ev.NameStatus = ns
	}
	if st, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--stat"); err == nil {
		ev.Stat = st
	}
	if nm, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--numstat"); err == nil {
		ev.Numstat = nm
	}
	if pt, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--no-ext-diff", "--patch"); err == nil {
		ev.Patch = pt
	}
}

func collectUncommittedSideEvidence(ev *GitChangeEvidence, repoPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitEvidenceTimeout)
	defer cancel()

	if ns, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--name-status"); err == nil {
		ev.UncommittedNameStatus = ns
	}
	if st, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--stat"); err == nil {
		ev.UncommittedStat = st
	}
	if nm, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--numstat"); err == nil {
		ev.UncommittedNumstat = nm
	}
	if pt, err := gitCommandOutput(ctx, repoPath, "diff", "HEAD", "--no-ext-diff", "--patch"); err == nil {
		ev.UncommittedPatch = pt
	}
}

func collectCommittedRangeEvidence(ev *GitChangeEvidence, repoPath, baselineSHA, headSHA string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitEvidenceTimeout)
	defer cancel()

	rangeSpec := baselineSHA + ".." + headSHA

	if ns, err := gitCommandOutput(ctx, repoPath, "diff", "--name-status", rangeSpec); err == nil {
		ev.NameStatus = ns
	}
	if st, err := gitCommandOutput(ctx, repoPath, "diff", "--stat", rangeSpec); err == nil {
		ev.Stat = st
	}
	if nm, err := gitCommandOutput(ctx, repoPath, "diff", "--numstat", rangeSpec); err == nil {
		ev.Numstat = nm
	}
	if pt, err := gitCommandOutput(ctx, repoPath, "diff", "--no-ext-diff", "--patch", rangeSpec); err == nil {
		ev.Patch = pt
	}

	// Collect commit log using unit-separator format
	logOutput, err := gitCommandOutput(ctx, repoPath, "log",
		"--format=%H%x1f%h%x1f%s%x1f%an%x1f%aI%x1e",
		rangeSpec)
	if err == nil && logOutput != "" {
		records := strings.Split(logOutput, "\x1e")
		for _, record := range records {
			record = strings.TrimSpace(record)
			if record == "" {
				continue
			}
			fields := strings.Split(record, "\x1f")
			if len(fields) < 3 {
				continue
			}
			summary := GitCommitSummary{
				SHA:      fields[0],
				ShortSHA: fields[1],
				Subject:  fields[2],
			}
			if len(fields) > 3 {
				summary.AuthorName = fields[3]
			}
			if len(fields) > 4 {
				summary.AuthorDate = fields[4]
			}
			ev.Commits = append(ev.Commits, summary)
		}
	}
	ev.CommitCount = len(ev.Commits)
}

// CommitState describes the step 8 commit/finalization state.
type CommitState string

const (
	CommitStateBlockedValidation       CommitState = "blocked_validation"
	CommitStateBlockedAuditNotAccepted CommitState = "blocked_audit_not_accepted"
	CommitStateBlockedNoDiffInspection CommitState = "blocked_no_diff_inspection"
	CommitStateBlockedMixedChanges     CommitState = "blocked_mixed_changes"
	CommitStateBlockedNoUpstream       CommitState = "blocked_no_upstream"
	CommitStateNoChanges               CommitState = "no_changes"
	CommitStateReadyToCommit           CommitState = "ready_to_commit"
	CommitStateCommittedLocal          CommitState = "committed_local"
	CommitStatePushed                  CommitState = "pushed"
	CommitStatePushFailed              CommitState = "push_failed"
	CommitStateCommitFailed            CommitState = "commit_failed"
	CommitStateUnknown                 CommitState = "unknown"
)

type AuditClearance struct {
	Status                 string `json:"status"`
	AcceptedAt             string `json:"accepted_at,omitempty"`
	Source                 string `json:"source,omitempty"`
	AuditHandoffArtifactID int64  `json:"audit_handoff_artifact_id,omitempty"`
}

type GitCommitState struct {
	State                    CommitState `json:"state"`
	ValidationPassed         bool        `json:"validation_passed"`
	ValidationFailedAccepted bool        `json:"validation_failed_accepted"`
	AuditAccepted            bool        `json:"audit_accepted"`
	EvidenceMode             string      `json:"evidence_mode,omitempty"`
	HasGitDiffEvidence       bool        `json:"has_git_diff_evidence"`
	Branch                   string      `json:"branch,omitempty"`
	HeadSHA                  string      `json:"head_sha,omitempty"`
	UpstreamRemote           string      `json:"upstream_remote,omitempty"`
	UpstreamBranch           string      `json:"upstream_branch,omitempty"`
	AheadCount               int         `json:"ahead_count"`
	BehindCount              int         `json:"behind_count"`
	HasUpstream              bool        `json:"has_upstream"`
	WorktreeClean            bool        `json:"worktree_clean"`
	CommitMessage            string      `json:"commit_message,omitempty"`
	CommitSHA                string      `json:"commit_sha,omitempty"`
	CommitSubject            string      `json:"commit_subject,omitempty"`
	Warnings                 []string    `json:"warnings,omitempty"`
	Error                    string      `json:"error,omitempty"`
}

type GitUpstreamInfo struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

type GitCommitResult struct {
	SHA       string `json:"sha,omitempty"`
	ShortSHA  string `json:"short_sha,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type GitPushResult struct {
	Success    bool   `json:"success"`
	DryRunPass bool   `json:"dry_run_pass,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Timestamp  string `json:"timestamp,omitempty"`
	Error      string `json:"error,omitempty"`
}

type CommitStateInput struct {
	RepoPath                 string
	ValidationPassed         bool
	ValidationFailedAccepted bool
	AuditAccepted            bool
	EvidenceMode             string
	HasGitDiffEvidence       bool
	EvidenceHeadSHA          string
	EvidenceBranch           string
	HasCommitResult          bool
	CommitResultSuccess      bool
	CommitResultSHA          string
	HasPushResult            bool
	PushResultSuccess        bool
}

func ResolveCommitState(input CommitStateInput) GitCommitState {
	state := GitCommitState{
		ValidationPassed:         input.ValidationPassed,
		ValidationFailedAccepted: input.ValidationFailedAccepted,
		AuditAccepted:            input.AuditAccepted,
		EvidenceMode:             input.EvidenceMode,
		HasGitDiffEvidence:       input.HasGitDiffEvidence,
		HeadSHA:                  input.EvidenceHeadSHA,
		Branch:                   input.EvidenceBranch,
	}

	if !input.ValidationPassed && !input.ValidationFailedAccepted {
		state.State = CommitStateBlockedValidation
		return state
	}

	if !input.AuditAccepted {
		state.State = CommitStateBlockedAuditNotAccepted
		return state
	}

	if !input.HasGitDiffEvidence {
		state.State = CommitStateBlockedNoDiffInspection
		return state
	}

	if input.HasPushResult && !input.PushResultSuccess {
		state.State = CommitStatePushFailed
		return state
	}

	if input.HasCommitResult && !input.CommitResultSuccess {
		state.State = CommitStateCommitFailed
		return state
	}

	if input.EvidenceMode == EvidenceModeMixedCommittedUncommitted {
		state.State = CommitStateBlockedMixedChanges
		state.Warnings = append(state.Warnings, "Mixed committed and uncommitted changes detected. Resolve before committing or pushing.")
		return state
	}

	// no_changes and baseline_unavailable_clean without commit result: return early, no git needed
	if !input.CommitResultSuccess {
		if input.EvidenceMode == EvidenceModeNoChanges || input.EvidenceMode == EvidenceModeBaselineUnavailableClean {
			state.State = CommitStateNoChanges
			return state
		}
	}

	// Check actual Git state
	ctx, cancel := context.WithTimeout(context.Background(), gitEvidenceTimeout)
	defer cancel()

	if input.RepoPath == "" {
		state.State = CommitStateUnknown
		state.Error = "repo path is empty"
		return state
	}

	// Check if worktree is clean
	porcelain, err := gitCommandOutput(ctx, input.RepoPath, "status", "--porcelain=v1")
	if err != nil {
		state.State = CommitStateUnknown
		state.Error = fmt.Sprintf("git status failed: %v", err)
		return state
	}
	state.WorktreeClean = strings.TrimSpace(porcelain) == ""

	// Resolve upstream info
	upstream, err := gitCommandOutput(ctx, input.RepoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err == nil && upstream != "" {
		parts := strings.SplitN(upstream, "/", 2)
		if len(parts) == 2 {
			state.UpstreamRemote = parts[0]
			state.UpstreamBranch = parts[1]
			state.HasUpstream = true
		}
	}

	if state.HasUpstream {
		counts, err := gitCommandOutput(ctx, input.RepoPath, "rev-list", "--left-right", "--count", "@{u}...HEAD")
		if err == nil {
			parts := strings.Fields(counts)
			if len(parts) == 2 {
				state.BehindCount, _ = strconvAtoi(parts[0])
				state.AheadCount, _ = strconvAtoi(parts[1])
			}
		}
	}

	// Dirty worktree: uncommitted changes exist → ready_to_commit
	if !state.WorktreeClean {
		if input.EvidenceMode == EvidenceModeCommittedRange || input.CommitResultSuccess || input.PushResultSuccess {
			state.State = CommitStateBlockedMixedChanges
			state.Warnings = append(state.Warnings, "Committed changes exist since the run baseline, but the working tree is also dirty. Resolve uncommitted changes before committing or pushing.")
			return state
		}
		state.State = CommitStateReadyToCommit
		return state
	}

	// Clean worktree: classify based on committed evidence or commit result
	hasCommittedEvidence := input.EvidenceMode == EvidenceModeCommittedRange
	hasCommitResult := input.CommitResultSuccess
	hasPushResult := input.PushResultSuccess
	isNoChanges := input.EvidenceMode == EvidenceModeNoChanges || input.EvidenceMode == EvidenceModeBaselineUnavailableClean
	isUncommittedEvidence := input.EvidenceMode == EvidenceModeUncommittedWorktree || input.EvidenceMode == EvidenceModeBaselineUnavailableDirty

	if hasPushResult {
		state.State = CommitStatePushed
		return state
	}

	if hasCommittedEvidence || hasCommitResult {
		if input.CommitResultSHA != "" {
			state.CommitSHA = input.CommitResultSHA
		}
		if state.HasUpstream {
			if state.AheadCount > 0 {
				state.State = CommitStateCommittedLocal
			} else if state.AheadCount == 0 {
				state.State = CommitStatePushed
			} else {
				state.State = CommitStateCommittedLocal
			}
			return state
		}
		state.State = CommitStateBlockedNoUpstream
		return state
	}

	if isNoChanges {
		state.State = CommitStateNoChanges
		return state
	}

	if isUncommittedEvidence {
		state.State = CommitStateReadyToCommit
		return state
	}

	state.State = CommitStateUnknown
	return state
}

func GetUpstreamInfo(repoPath string) (*GitUpstreamInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCaptureTimeout)
	defer cancel()

	upstream, err := gitCommandOutput(ctx, repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(upstream, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected upstream format: %s", upstream)
	}
	return &GitUpstreamInfo{Remote: parts[0], Branch: parts[1]}, nil
}

func CreateGitCommit(repoPath, message string) *GitCommitResult {
	result := &GitCommitResult{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if strings.TrimSpace(repoPath) == "" {
		result.Error = "repo path is empty"
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitEvidenceTimeout)
	defer cancel()

	// git add -A
	if _, err := gitCommandOutput(ctx, repoPath, "add", "-A"); err != nil {
		result.Error = fmt.Sprintf("git add failed: %v", err)
		return result
	}

	// git commit -m "<message>"
	if _, err := gitCommandOutput(ctx, repoPath, "commit", "-m", message); err != nil {
		result.Error = fmt.Sprintf("git commit failed: %v", err)
		return result
	}

	// Capture result
	sha, err := gitCommandOutput(ctx, repoPath, "rev-parse", "HEAD")
	if err != nil {
		result.Error = fmt.Sprintf("commit succeeded but rev-parse failed: %v", err)
		result.Success = true
		return result
	}
	result.SHA = sha
	if len(sha) > 7 {
		result.ShortSHA = sha[:7]
	} else {
		result.ShortSHA = sha
	}

	subject, err := gitCommandOutput(ctx, repoPath, "log", "--format=%s", "-1")
	if err == nil {
		result.Subject = subject
	}

	branch, err := gitCommandOutput(ctx, repoPath, "branch", "--show-current")
	if err == nil {
		result.Branch = branch
	}

	result.Success = true
	return result
}

func DryRunPush(repoPath string) *GitPushResult {
	result := &GitPushResult{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmdArgs := append([]string{"-C", repoPath}, "push", "--porcelain", "--dry-run")
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.Output()
	result.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Stderr = string(exitErr.Stderr)
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
		result.DryRunPass = false
		return result
	}

	result.Stdout = strings.TrimSpace(string(out))
	result.ExitCode = 0
	result.DryRunPass = true
	return result
}

func PushGitCommit(repoPath string) *GitPushResult {
	result := &GitPushResult{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmdArgs := append([]string{"-C", repoPath}, "push", "--porcelain")
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.Output()
	result.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Stderr = string(exitErr.Stderr)
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
		result.Success = false
		return result
	}

	result.Stdout = strings.TrimSpace(string(out))
	result.ExitCode = 0
	result.Success = true
	return result
}

// strconvAtoi is a non-import version of strconv.Atoi for use in this file.
func strconvAtoi(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func gitCommandOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %v: %w", args, err)
	}
	return strings.TrimSpace(string(out)), nil
}
