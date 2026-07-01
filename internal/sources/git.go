package sources

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"relay/internal/store"
)

const (
	defaultGitCommandTimeout = 5 * time.Second
	defaultGitStdoutLimit    = 256 * 1024
	defaultGitStderrLimit    = 16 * 1024
	defaultGitListFilesLimit = 1024 * 1024
	defaultDiffMaxBytes      = 128 * 1024
	defaultDiffContextLines  = 3
)

type GitCommandError struct {
	Command   string
	Message   string
	Timeout   bool
	Truncated bool
}

func (e *GitCommandError) Error() string {
	if e.Message == "" {
		return e.Command
	}
	return fmt.Sprintf("%s: %s", e.Command, e.Message)
}

type gitRunResult struct {
	stdout []byte
	stderr string
}

type gitStatusCapture struct {
	status       RepositoryGitStatus
	entries      []statusEntry
	rawPorcelain []byte
}

type statusEntry struct {
	path      string
	staged    byte
	unstaged  byte
	untracked bool
}

type cappedBuffer struct {
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.maxBytes <= 0 {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.maxBytes - b.buf.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.buf.Write(p[:remaining])
	}
	if len(p) > remaining {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func isAllowedGitCommand(args []string) bool {
	switch {
	case len(args) == 3 && args[0] == "rev-parse" && args[1] == "--abbrev-ref" && args[2] == "HEAD":
		return true
	case len(args) == 2 && args[0] == "rev-parse" && args[1] == "HEAD":
		return true
	case len(args) == 3 && args[0] == "status" && args[1] == "--porcelain=v1" && args[2] == "-z":
		return true
	case len(args) == 2 && args[0] == "ls-files" && args[1] == "-z":
		return true
	case len(args) == 4 && args[0] == "show" && args[1] == "-s" && args[2] == "--format=%H%x00%an%x00%ae%x00%aI%x00%s" && args[3] == "HEAD":
		return true
	case len(args) == 4 && args[0] == "diff" && args[1] == "--name-status" && args[2] == "--no-ext-diff" && args[3] == "-z":
		return true
	case len(args) == 5 && args[0] == "diff" && args[1] == "--cached" && args[2] == "--name-status" && args[3] == "--no-ext-diff" && args[4] == "-z":
		return true
	case len(args) == 6 && args[0] == "show" && args[1] == "--name-status" && args[2] == "--format=" && args[3] == "--no-ext-diff" && args[4] == "-z" && args[5] == "HEAD":
		return true
	case len(args) == 6 && args[0] == "show" && args[1] == "--format=" && args[2] == "--no-ext-diff" && strings.HasPrefix(args[3], "--unified=") && args[4] == "HEAD" && args[5] == "--":
		return validUnifiedArg(args[3])
	case len(args) == 4 && args[0] == "diff" && args[1] == "--no-ext-diff" && strings.HasPrefix(args[2], "--unified=") && args[3] == "--":
		return validUnifiedArg(args[2])
	case len(args) == 5 && args[0] == "diff" && args[1] == "--cached" && args[2] == "--no-ext-diff" && strings.HasPrefix(args[3], "--unified=") && args[4] == "--":
		return validUnifiedArg(args[3])
	default:
		return false
	}
}

func validUnifiedArg(arg string) bool {
	value := strings.TrimPrefix(arg, "--unified=")
	if value == "" {
		return false
	}
	n, err := strconv.Atoi(value)
	return err == nil && n >= 0
}

func runGitCommand(ctx context.Context, repoPath string, stdoutLimit, stderrLimit int, args ...string) (gitRunResult, error) {
	if !isAllowedGitCommand(args) {
		return gitRunResult{}, &GitCommandError{
			Command:   "git " + strings.Join(args, " "),
			Message:   "command is not allowlisted",
			Truncated: false,
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, defaultGitCommandTimeout)
	defer cancel()

	stdoutBuf := &cappedBuffer{maxBytes: stdoutLimit}
	stderrBuf := &cappedBuffer{maxBytes: stderrLimit}

	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(cmdCtx, "git", cmdArgs...)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()
	result := gitRunResult{
		stdout: bytes.Clone(stdoutBuf.Bytes()),
	}
	if stderrText, redactionStatus := redactSourceContent(string(stderrBuf.Bytes())); redactionStatus == RedactionStatusBlocked {
		result.stderr = "[BLOCKED]"
	} else {
		result.stderr = strings.TrimSpace(stderrText)
	}

	if stdoutBuf.truncated || stderrBuf.truncated {
		return result, &GitCommandError{
			Command:   "git " + strings.Join(args, " "),
			Message:   "output exceeded byte limit",
			Truncated: true,
		}
	}
	if cmdCtx.Err() == context.DeadlineExceeded {
		return result, &GitCommandError{
			Command: "git " + strings.Join(args, " "),
			Message: "command timed out",
			Timeout: true,
		}
	}
	if err != nil {
		message := result.stderr
		if message == "" {
			message = err.Error()
		}
		return result, &GitCommandError{
			Command: "git " + strings.Join(args, " "),
			Message: message,
		}
	}
	return result, nil
}

func (s *Service) GetRepositoryGitStatus(ctx context.Context, repo store.ProjectRepository) (RepositoryGitStatus, error) {
	capture, err := s.inspectRepositoryGitStatus(ctx, repo)
	return capture.status, err
}

func (s *Service) GetRecentCommit(ctx context.Context, repo store.ProjectRepository) (RecentCommit, error) {
	result, err := runGitCommand(ctx, repo.LocalPath, defaultGitStdoutLimit, defaultGitStderrLimit,
		"show", "-s", "--format=%H%x00%an%x00%ae%x00%aI%x00%s", "HEAD")
	if err != nil {
		return RecentCommit{RepoID: repo.RepoID}, err
	}

	fields := bytes.Split(result.stdout, []byte{0})
	if len(fields) < 5 {
		return RecentCommit{RepoID: repo.RepoID}, fmt.Errorf("unexpected git show output for repo %s", repo.RepoID)
	}

	return RecentCommit{
		RepoID:      repo.RepoID,
		CommitSHA:   strings.TrimSpace(string(fields[0])),
		AuthorName:  strings.TrimSpace(string(fields[1])),
		AuthorEmail: strings.TrimSpace(string(fields[2])),
		AuthorDate:  strings.TrimSpace(string(fields[3])),
		Subject:     strings.TrimSpace(string(fields[4])),
	}, nil
}

func (s *Service) GetChangedFiles(ctx context.Context, repo store.ProjectRepository, mode string) ([]ChangedFile, error) {
	args, err := diffNameStatusArgs(mode)
	if err != nil {
		return nil, err
	}
	result, err := runGitCommand(ctx, repo.LocalPath, defaultGitStdoutLimit, defaultGitStderrLimit, args...)
	if err != nil {
		return nil, err
	}
	files, err := parseNameStatusZ(result.stdout, repo.RepoID, mode == DiffModeStaged)
	if err != nil {
		return nil, err
	}
	if mode != DiffModeWorktree {
		return files, nil
	}
	capture, err := s.inspectRepositoryGitStatus(ctx, repo)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, file := range files {
		seen[file.Path] = struct{}{}
	}
	for _, entry := range capture.entries {
		if !entry.untracked {
			continue
		}
		if _, ok := seen[entry.path]; ok {
			continue
		}
		files = append(files, ChangedFile{
			RepoID: repo.RepoID,
			Path:   entry.path,
			Status: "A",
			Staged: false,
		})
		seen[entry.path] = struct{}{}
	}
	return files, nil
}

func (s *Service) GetRecentCommitChangedFiles(ctx context.Context, repo store.ProjectRepository) ([]ChangedFile, error) {
	result, err := runGitCommand(ctx, repo.LocalPath, defaultGitStdoutLimit, defaultGitStderrLimit,
		"show", "--name-status", "--format=", "--no-ext-diff", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	return parseNameStatusZ(result.stdout, repo.RepoID, false)
}

func (s *Service) GetBoundedDiff(ctx context.Context, repo store.ProjectRepository, mode string, maxBytes int, contextLines int) (BoundedDiff, error) {
	if maxBytes <= 0 {
		maxBytes = defaultDiffMaxBytes
	}
	if contextLines <= 0 {
		contextLines = defaultDiffContextLines
	}

	args, err := diffPatchArgs(mode, contextLines)
	if err != nil {
		return BoundedDiff{RepoID: repo.RepoID, Mode: mode, MaxBytes: maxBytes}, err
	}

	result, runErr := runGitCommand(ctx, repo.LocalPath, maxBytes, defaultGitStderrLimit, args...)
	truncated := false
	if runErr != nil {
		var gitErr *GitCommandError
		if errors.As(runErr, &gitErr) && gitErr.Truncated {
			truncated = true
		} else {
			return BoundedDiff{RepoID: repo.RepoID, Mode: mode, MaxBytes: maxBytes}, runErr
		}
	}

	content, redactionStatus := redactSourceContent(string(result.stdout))
	return BoundedDiff{
		RepoID:          repo.RepoID,
		Mode:            mode,
		Content:         content,
		ContentHash:     sha256HexString(content),
		Truncated:       truncated,
		MaxBytes:        maxBytes,
		RedactionStatus: redactionStatus,
	}, nil
}

func (s *Service) GetRecentCommitBoundedDiff(ctx context.Context, repo store.ProjectRepository, maxBytes int, contextLines int) (BoundedDiff, error) {
	if maxBytes <= 0 {
		maxBytes = defaultDiffMaxBytes
	}
	if contextLines <= 0 {
		contextLines = defaultDiffContextLines
	}

	args := []string{"show", "--format=", "--no-ext-diff", fmt.Sprintf("--unified=%d", contextLines), "HEAD", "--"}
	result, runErr := runGitCommand(ctx, repo.LocalPath, maxBytes, defaultGitStderrLimit, args...)
	truncated := false
	if runErr != nil {
		var gitErr *GitCommandError
		if errors.As(runErr, &gitErr) && gitErr.Truncated {
			truncated = true
		} else {
			return BoundedDiff{RepoID: repo.RepoID, Mode: DiffModeRecentCommit, MaxBytes: maxBytes}, runErr
		}
	}

	content, redactionStatus := redactSourceContent(string(result.stdout))
	return BoundedDiff{
		RepoID:          repo.RepoID,
		Mode:            DiffModeRecentCommit,
		Content:         content,
		ContentHash:     sha256HexString(content),
		Truncated:       truncated,
		MaxBytes:        maxBytes,
		RedactionStatus: redactionStatus,
	}, nil
}

func (s *Service) inspectRepositoryGitStatus(ctx context.Context, repo store.ProjectRepository) (gitStatusCapture, error) {
	capture := gitStatusCapture{
		status: RepositoryGitStatus{RepoID: repo.RepoID},
	}

	branchResult, err := runGitCommand(ctx, repo.LocalPath, defaultGitStdoutLimit, defaultGitStderrLimit, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		capture.status.GitError = err.Error()
		return capture, err
	}
	headResult, err := runGitCommand(ctx, repo.LocalPath, defaultGitStdoutLimit, defaultGitStderrLimit, "rev-parse", "HEAD")
	if err != nil {
		capture.status.GitError = err.Error()
		return capture, err
	}
	statusResult, err := runGitCommand(ctx, repo.LocalPath, defaultGitStdoutLimit, defaultGitStderrLimit, "status", "--porcelain=v1", "-z")
	if err != nil {
		capture.status.GitError = err.Error()
		return capture, err
	}

	entries, err := parsePorcelainStatusV1Z(statusResult.stdout)
	if err != nil {
		capture.status.GitError = err.Error()
		return capture, err
	}

	currentBranch := strings.TrimSpace(string(branchResult.stdout))
	if currentBranch == "HEAD" {
		currentBranch = ""
	}

	capture.entries = entries
	capture.rawPorcelain = bytes.Clone(statusResult.stdout)
	capture.status.CurrentBranch = currentBranch
	capture.status.HeadSHA = strings.TrimSpace(string(headResult.stdout))
	capture.status.PorcelainHash = sha256HexBytes(statusResult.stdout)
	capture.status.GitStatusAvailable = true

	for _, entry := range entries {
		capture.status.ChangedFileCount++
		if entry.untracked {
			capture.status.UntrackedCount++
		} else {
			if entry.staged != ' ' {
				capture.status.StagedCount++
			}
			if entry.unstaged != ' ' {
				capture.status.UnstagedCount++
			}
		}
	}
	capture.status.Dirty = len(entries) > 0
	return capture, nil
}

func repositoryGitStatusFromSnapshotRow(row store.SourceSnapshotRepository) RepositoryGitStatus {
	return RepositoryGitStatus{
		RepoID:             row.RepoID,
		CurrentBranch:      row.CurrentBranch,
		HeadSHA:            row.HeadSha,
		Dirty:              row.Dirty == 1,
		StagedCount:        int(row.StagedCount),
		UnstagedCount:      int(row.UnstagedCount),
		UntrackedCount:     int(row.UntrackedCount),
		ChangedFileCount:   int(row.ChangedFileCount),
		PorcelainHash:      row.StatusPorcelainHash,
		GitStatusAvailable: row.GitStatusAvailable == 1,
		GitError:           row.GitError,
	}
}

func listTrackedFiles(ctx context.Context, repo store.ProjectRepository) ([]string, error) {
	result, err := runGitCommand(ctx, repo.LocalPath, defaultGitListFilesLimit, defaultGitStderrLimit, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	tokens := nulSeparatedStrings(result.stdout)
	paths := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		paths = append(paths, normalizeRepoRelativePath(token))
	}
	return paths, nil
}

func diffNameStatusArgs(mode string) ([]string, error) {
	switch mode {
	case DiffModeWorktree:
		return []string{"diff", "--name-status", "--no-ext-diff", "-z"}, nil
	case DiffModeStaged:
		return []string{"diff", "--cached", "--name-status", "--no-ext-diff", "-z"}, nil
	default:
		return nil, fmt.Errorf("unsupported diff mode %q", mode)
	}
}

func diffPatchArgs(mode string, contextLines int) ([]string, error) {
	unified := fmt.Sprintf("--unified=%d", contextLines)
	switch mode {
	case DiffModeWorktree:
		return []string{"diff", "--no-ext-diff", unified, "--"}, nil
	case DiffModeStaged:
		return []string{"diff", "--cached", "--no-ext-diff", unified, "--"}, nil
	default:
		return nil, fmt.Errorf("unsupported diff mode %q", mode)
	}
}

func parsePorcelainStatusV1Z(data []byte) ([]statusEntry, error) {
	tokens := nulSeparatedBytes(data)
	entries := make([]statusEntry, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if len(token) < 4 {
			return nil, fmt.Errorf("unexpected git status token %q", string(token))
		}
		staged := token[0]
		unstaged := token[1]
		path := normalizeRepoRelativePath(string(token[3:]))
		if staged == '?' && unstaged == '?' {
			entries = append(entries, statusEntry{path: path, staged: staged, unstaged: unstaged, untracked: true})
			continue
		}
		if staged == 'R' || staged == 'C' || unstaged == 'R' || unstaged == 'C' {
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("rename/copy entry missing destination path")
			}
			path = normalizeRepoRelativePath(string(tokens[i+1]))
			i++
		}
		entries = append(entries, statusEntry{path: path, staged: staged, unstaged: unstaged})
	}
	return entries, nil
}

func parseNameStatusZ(data []byte, repoID string, staged bool) ([]ChangedFile, error) {
	tokens := nulSeparatedStrings(data)
	files := make([]ChangedFile, 0, len(tokens)/2)
	for i := 0; i < len(tokens); {
		statusToken := strings.TrimSpace(tokens[i])
		i++
		if statusToken == "" {
			continue
		}
		if i >= len(tokens) {
			return nil, fmt.Errorf("missing path for diff status %q", statusToken)
		}
		pathValue := normalizeRepoRelativePath(tokens[i])
		i++
		status := string(statusToken[0])
		if strings.HasPrefix(statusToken, "R") || strings.HasPrefix(statusToken, "C") {
			if i >= len(tokens) {
				return nil, fmt.Errorf("rename/copy entry missing destination path")
			}
			pathValue = normalizeRepoRelativePath(tokens[i])
			i++
		}
		files = append(files, ChangedFile{
			RepoID: repoID,
			Path:   pathValue,
			Status: status,
			Staged: staged,
		})
	}
	return files, nil
}

func nulSeparatedBytes(data []byte) [][]byte {
	raw := bytes.Split(data, []byte{0})
	out := make([][]byte, 0, len(raw))
	for _, token := range raw {
		if len(token) == 0 {
			continue
		}
		out = append(out, token)
	}
	return out
}

func nulSeparatedStrings(data []byte) []string {
	raw := nulSeparatedBytes(data)
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		out = append(out, string(token))
	}
	return out
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sha256HexString(value string) string {
	return sha256HexBytes([]byte(value))
}
