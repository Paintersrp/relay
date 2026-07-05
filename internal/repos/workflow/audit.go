package workflowrepos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	auditGitTimeout       = 30 * time.Second
	MaxAuditDiffBytes     = 1024 * 1024
	maxAuditMetadataBytes = 256 * 1024
)

var ErrAuditGitOutputTooLarge = errors.New("audit Git output exceeds the configured bound")

type AuditCommitEvidence struct {
	Branch        string   `json:"branch"`
	BaseCommit    string   `json:"base_commit"`
	AuditedCommit string   `json:"audited_commit"`
	ChangedFiles  []string `json:"changed_files"`
	NameStatus    string   `json:"name_status"`
	DiffStat      string   `json:"diff_stat"`
	CommitLog     string   `json:"commit_log"`
	Diff          string   `json:"diff"`
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
	return AuditCommitEvidence{
		Branch:        preflight.CurrentBranch,
		BaseCommit:    baseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles:  changedFiles,
		NameStatus:    nameStatus,
		DiffStat:      strings.TrimSpace(string(diffStatBytes)),
		CommitLog:     strings.TrimSpace(string(commitLogBytes)),
		Diff:          string(diffBytes),
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
