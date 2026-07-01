package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"relay/internal/store"
)

const (
	defaultRGTimeout       = 10 * time.Second
	defaultRGMaxResults    = 50
	hardRGMaxResults       = 200
	defaultRGContextLines  = 0
	hardRGContextLines     = 3
	defaultRGStdoutMaxByte = 256 * 1024
	hardRGStdoutMaxByte    = 1024 * 1024
	maxRGPatternBytes      = 512
)

func (s *Service) SearchProjectFiles(ctx context.Context, input SourceSearchInput) (*SourceSearchResult, error) {
	generatedAt := nowSQLUTC()
	result := &SourceSearchResult{
		ProjectID:        strings.TrimSpace(input.ProjectID),
		SourceSnapshotID: strings.TrimSpace(input.SourceSnapshotID),
		GeneratedAt:      generatedAt,
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	if len([]byte(pattern)) > maxRGPatternBytes || strings.ContainsRune(pattern, 0) {
		return nil, fmt.Errorf("pattern is invalid")
	}
	if _, err := exec.LookPath("rg"); err != nil {
		result.Blockers = append(result.Blockers, SourceBlocker{Code: SourceBlockerRipgrepMissing, Message: "rg executable is not available"})
		return result, nil
	}

	resolved, err := s.resolveSourceSnapshot(ctx, input.ProjectID, input.SourceSnapshotID)
	if err != nil {
		if blocker, ok := operationBlocker("", err); ok {
			result.Blockers = append(result.Blockers, blocker)
			result.FreshnessReport = unavailableFreshnessReport(result.SourceSnapshotID, generatedAt, blocker)
			return result, nil
		}
		return nil, err
	}
	result.ProjectID = resolved.project.ProjectID
	result.SourceSnapshotID = resolved.snapshot.SourceSnapshotID
	result.FreshnessReport = s.evaluateSourceSnapshotFreshness(ctx, resolved, generatedAt)

	normalizedRepoIDs, err := normalizeRepoIDList(input.RepoIDs, resolved.projectRepos)
	if err != nil {
		if blocker, ok := operationBlocker("", err); ok {
			result.Blockers = append(result.Blockers, blocker)
			return result, nil
		}
		return nil, err
	}
	allowedRepos := repoIDSet(normalizedRepoIDs)
	maxResults := boundedPositive(input.MaxResults, defaultRGMaxResults, hardRGMaxResults)
	maxBytes := boundedPositive(input.MaxBytes, defaultRGStdoutMaxByte, hardRGStdoutMaxByte)
	contextLines := boundedPositive(input.ContextLines, defaultRGContextLines, hardRGContextLines)
	if input.ContextLines <= 0 {
		contextLines = 0
	}

	for _, snapshotRepo := range sortedSourceSnapshotRepos(resolved.repositories) {
		if len(result.Matches) >= maxResults {
			result.Truncated = true
			break
		}
		if len(allowedRepos) > 0 {
			if _, ok := allowedRepos[snapshotRepo.RepoID]; !ok {
				continue
			}
		}
		repo, ok := resolved.projectRepos[snapshotRepo.RepoID]
		if !ok || repo.Enabled == 0 {
			continue
		}
		included := resolved.searchableFiles(snapshotRepo.RepoID, input.IncludeExcluded)
		if len(included) == 0 {
			continue
		}
		args, err := buildRGArgs(repo, pattern, input.CaseSensitive, contextLines)
		if err != nil {
			return nil, err
		}
		stdout, truncated, err := runRGCommand(ctx, repo.LocalPath, maxBytes, args)
		if err != nil {
			return nil, err
		}
		if truncated {
			result.Truncated = true
		}
		matches, blockers, err := parseRGMatches(stdout, resolved, snapshotRepo.RepoID, included, maxResults-len(result.Matches), generatedAt)
		if err != nil {
			return nil, err
		}
		result.Blockers = append(result.Blockers, blockers...)
		result.Matches = append(result.Matches, matches...)
		if len(result.Matches) >= maxResults {
			result.Truncated = true
		}
	}
	return result, nil
}

func buildRGArgs(repo store.ProjectRepository, pattern string, caseSensitive bool, contextLines int) ([]string, error) {
	allowedRoots, err := decodePolicyStringArray(repo.AllowedRootsJson)
	if err != nil {
		return nil, fmt.Errorf("decode allowed_roots_json: %w", err)
	}
	ignoredGlobs, err := decodePolicyStringArray(repo.IgnoredGlobsJson)
	if err != nil {
		return nil, fmt.Errorf("decode ignored_globs_json: %w", err)
	}
	args := []string{
		"--json",
		"--line-number",
		"--with-filename",
		"--no-heading",
		"--color=never",
		"--fixed-strings",
	}
	if !caseSensitive {
		args = append(args, "--ignore-case")
	}
	if contextLines > 0 {
		args = append(args, fmt.Sprintf("--context=%d", contextLines))
	}
	for _, glob := range ignoredGlobs {
		glob = strings.TrimSpace(glob)
		if glob != "" {
			args = append(args, "--glob", "!"+glob)
		}
	}
	args = append(args, "-e", pattern)
	roots := searchRoots(allowedRoots)
	args = append(args, roots...)
	return args, nil
}

func searchRoots(allowedRoots []string) []string {
	roots := make([]string, 0, len(allowedRoots))
	for _, root := range allowedRoots {
		root = strings.TrimSpace(root)
		if root == "" || root == "." {
			return []string{"."}
		}
		if cleaned, err := validateRepoRelativePath(root); err == nil {
			roots = append(roots, cleaned)
		}
	}
	if len(roots) == 0 {
		return []string{"."}
	}
	return roots
}

func runRGCommand(ctx context.Context, repoRoot string, maxBytes int, args []string) ([]byte, bool, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, defaultRGTimeout)
	defer cancel()
	stdoutBuf := &cappedBuffer{maxBytes: maxBytes}
	stderrBuf := &cappedBuffer{maxBytes: 16 * 1024}
	cmd := exec.CommandContext(cmdCtx, "rg", args...)
	cmd.Dir = repoRoot
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	err := cmd.Run()
	if cmdCtx.Err() == context.DeadlineExceeded {
		return bytes.Clone(stdoutBuf.Bytes()), stdoutBuf.truncated, fmt.Errorf("rg command timed out")
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return bytes.Clone(stdoutBuf.Bytes()), stdoutBuf.truncated, nil
		}
		stderr, status := redactSourceContent(string(stderrBuf.Bytes()))
		if status == RedactionStatusBlocked {
			stderr = "[BLOCKED]"
		}
		return bytes.Clone(stdoutBuf.Bytes()), stdoutBuf.truncated, fmt.Errorf("rg command failed: %s", strings.TrimSpace(stderr))
	}
	return bytes.Clone(stdoutBuf.Bytes()), stdoutBuf.truncated, nil
}

type rgJSONMessage struct {
	Type string        `json:"type"`
	Data rgJSONPayload `json:"data"`
}

type rgJSONPayload struct {
	Path       rgJSONText `json:"path"`
	Lines      rgJSONText `json:"lines"`
	LineNumber int        `json:"line_number"`
}

type rgJSONText struct {
	Text string `json:"text"`
}

func parseRGMatches(data []byte, resolved *sourceSnapshotContext, repoID string, included map[string]store.SourceSnapshotFile, remaining int, generatedAt string) ([]SourceSearchMatch, []SourceBlocker, error) {
	var matches []SourceSearchMatch
	var blockers []SourceBlocker
	for _, rawLine := range bytes.Split(data, []byte("\n")) {
		rawLine = bytes.TrimSpace(rawLine)
		if len(rawLine) == 0 {
			continue
		}
		var msg rgJSONMessage
		if err := json.Unmarshal(rawLine, &msg); err != nil {
			return nil, nil, fmt.Errorf("parse rg json: %w", err)
		}
		if msg.Type != "match" && msg.Type != "context" {
			continue
		}
		relPath, err := validateRepoRelativePath(filepath.ToSlash(msg.Data.Path.Text))
		if err != nil {
			continue
		}
		file, ok := included[relPath]
		if !ok {
			continue
		}
		snippet := msg.Data.Lines.Text
		redacted, status := redactSourceContent(snippet)
		if status == RedactionStatusBlocked {
			blockers = append(blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerRedactionBlocked, Message: "search match contains blocked secret material"})
			continue
		}
		if remaining <= 0 {
			break
		}
		line := msg.Data.LineNumber
		if line <= 0 {
			line = 1
		}
		matches = append(matches, SourceSearchMatch{
			ProjectID:        resolved.project.ProjectID,
			RepoID:           repoID,
			SourceSnapshotID: resolved.snapshot.SourceSnapshotID,
			Path:             relPath,
			LineStart:        line,
			LineEnd:          line,
			Snippet:          redacted,
			SnippetHash:      sha256HexString(redacted),
			ContentHash:      file.ContentHash,
			RedactionStatus:  status,
			GeneratedAt:      generatedAt,
		})
		remaining--
	}
	return matches, blockers, nil
}

func (c *sourceSnapshotContext) searchableFiles(repoID string, includeExcluded bool) map[string]store.SourceSnapshotFile {
	out := make(map[string]store.SourceSnapshotFile)
	for _, file := range c.files[repoID] {
		if file.Included == 1 || includeExcluded {
			out[file.Path] = file
		}
	}
	return out
}
