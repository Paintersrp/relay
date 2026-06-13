package repos

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const gitCaptureTimeout = 5 * time.Second

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

func gitCommandOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %v: %w", args, err)
	}
	return strings.TrimSpace(string(out)), nil
}
