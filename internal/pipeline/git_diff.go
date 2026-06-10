package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type GitDiffEvidence struct {
	RepoPath    string
	StatusText  string
	DiffStat    string
	DiffNumstat string
	DiffPatch   string
	NameStatus  string
	HasChanges  bool
}

const gitDiffTimeout = 30 * time.Second

func runGitCmd(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\nstderr: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func CollectGitDiffEvidence(ctx context.Context, repoPath string, timeout time.Duration) (GitDiffEvidence, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var ev GitDiffEvidence
	ev.RepoPath = repoPath

	var out string
	var err error

	out, err = runGitCmd(ctx, repoPath, "status", "--short")
	if err != nil {
		return ev, fmt.Errorf("git status: %w", err)
	}
	ev.StatusText = strings.TrimSpace(out)

	out, err = runGitCmd(ctx, repoPath, "diff", "--stat")
	if err != nil {
		return ev, fmt.Errorf("git diff --stat: %w", err)
	}
	ev.DiffStat = strings.TrimSpace(out)

	out, err = runGitCmd(ctx, repoPath, "diff", "--numstat")
	if err != nil {
		return ev, fmt.Errorf("git diff --numstat: %w", err)
	}
	ev.DiffNumstat = strings.TrimSpace(out)

	out, err = runGitCmd(ctx, repoPath, "diff", "--name-status")
	if err != nil {
		return ev, fmt.Errorf("git diff --name-status: %w", err)
	}
	ev.NameStatus = strings.TrimSpace(out)

	out, err = runGitCmd(ctx, repoPath, "diff", "--no-ext-diff", "--patch")
	if err != nil {
		return ev, fmt.Errorf("git diff --patch: %w", err)
	}
	ev.DiffPatch = strings.TrimSpace(out)

	ev.HasChanges = ev.StatusText != "" || ev.DiffStat != "" || ev.DiffNumstat != "" || ev.DiffPatch != ""

	return ev, nil
}
