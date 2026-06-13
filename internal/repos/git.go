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

func gitCommandOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %v: %w", args, err)
	}
	return strings.TrimSpace(string(out)), nil
}
