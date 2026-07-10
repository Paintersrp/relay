package workflowrepos

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const executionPreflightTimeout = 15 * time.Second

type GitCommandRunner interface {
	Run(ctx context.Context, directory string, args ...string) ([]byte, error)
}

type execGitRunner struct{}

func (execGitRunner) Run(ctx context.Context, directory string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type ExecutionPreflightCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type ExecutionPreflightResult struct {
	OK            bool                      `json:"ok"`
	CurrentBranch string                    `json:"current_branch,omitempty"`
	HeadCommit    string                    `json:"head_commit,omitempty"`
	Checks        []ExecutionPreflightCheck `json:"checks"`
	BlockerCode   string                    `json:"blocker_code,omitempty"`
	BlockerText   string                    `json:"blocker_text,omitempty"`
}

func VerifyExecutionPreflight(ctx context.Context, localPath, expectedBranch, expectedCommit string) ExecutionPreflightResult {
	return VerifyExecutionPreflightWithRunner(ctx, localPath, expectedBranch, expectedCommit, execGitRunner{})
}

func VerifyExecutionPreflightWithRunner(ctx context.Context, localPath, expectedBranch, expectedCommit string, runner GitCommandRunner) ExecutionPreflightResult {
	result := ExecutionPreflightResult{OK: true, Checks: []ExecutionPreflightCheck{}}
	add := func(name string, ok bool, detail, code string) {
		result.Checks = append(result.Checks, ExecutionPreflightCheck{Name: name, OK: ok, Detail: detail})
		if !ok && result.OK {
			result.OK = false
			result.BlockerCode = code
			result.BlockerText = detail
		}
	}

	if strings.TrimSpace(localPath) == "" {
		add("repository_path", false, "registered repository path is empty", "repository_unavailable")
		return result
	}
	info, err := os.Stat(localPath)
	if err != nil || !info.IsDir() {
		add("repository_path", false, "registered repository path is unavailable", "repository_unavailable")
		return result
	}
	add("repository_path", true, "registered repository path is available", "")

	if runner == nil {
		add("git_available", false, "Git command runner is unavailable", "git_unavailable")
		return result
	}
	run := func(args ...string) ([]byte, error) {
		commandCtx, cancel := context.WithTimeout(ctx, executionPreflightTimeout)
		defer cancel()
		return runner.Run(commandCtx, localPath, args...)
	}

	branchBytes, err := run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		add("exact_branch", false, "repository HEAD is detached or the current branch cannot be resolved", "branch_mismatch")
		return result
	}
	result.CurrentBranch = strings.TrimSpace(string(branchBytes))
	if result.CurrentBranch != expectedBranch {
		add("exact_branch", false, fmt.Sprintf("current branch %q does not match required branch %q", result.CurrentBranch, expectedBranch), "branch_mismatch")
	} else {
		add("exact_branch", true, "current branch matches the Run", "")
	}

	headBytes, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		add("exact_head", false, "repository HEAD cannot be resolved", "head_unavailable")
		return result
	}
	result.HeadCommit = strings.ToLower(strings.TrimSpace(string(headBytes)))
	if result.HeadCommit != strings.ToLower(expectedCommit) {
		add("exact_head", false, fmt.Sprintf("current HEAD %q does not match required base commit %q", result.HeadCommit, expectedCommit), "head_mismatch")
	} else {
		add("exact_head", true, "repository HEAD matches the Run base commit", "")
	}

	statusBytes, err := run("status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		add("clean_repository", false, "repository status cannot be inspected", "git_status_unavailable")
		return result
	}
	if len(bytes.TrimSpace(statusBytes)) != 0 {
		add("clean_repository", false, "repository index, worktree, or untracked set is not clean", "repository_dirty")
	} else {
		add("clean_repository", true, "repository index, worktree, and untracked set are clean", "")
	}

	gitDirBytes, err := run("rev-parse", "--absolute-git-dir")
	if err != nil {
		add("git_operation", false, "repository Git directory cannot be resolved", "git_operation_unavailable")
		return result
	}
	gitDir := strings.TrimSpace(string(gitDirBytes))
	active, operation, inspectErr := activeGitOperation(gitDir)
	if inspectErr != nil {
		add("git_operation", false, "in-progress Git operation state cannot be inspected", "git_operation_unavailable")
	} else if active {
		add("git_operation", false, fmt.Sprintf("repository has an in-progress Git operation: %s", operation), "git_operation_in_progress")
	} else {
		add("git_operation", true, "repository has no in-progress Git operation", "")
	}

	return result
}

func activeGitOperation(gitDir string) (bool, string, error) {
	markers := []struct {
		path string
		name string
	}{
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
		{"BISECT_LOG", "bisect"},
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase or am"},
		{"sequencer", "sequencer"},
	}
	for _, marker := range markers {
		_, err := os.Lstat(filepath.Join(gitDir, marker.path))
		if err == nil {
			return true, marker.name, nil
		}
		if !os.IsNotExist(err) {
			return false, "", err
		}
	}
	return false, "", nil
}
