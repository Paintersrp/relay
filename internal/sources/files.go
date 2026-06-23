package sources

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"relay/internal/store"
)

const (
	defaultInventoryMaxResults = 1000
	hardInventoryMaxResults    = 10000
	defaultReadMaxBytes        = 64 * 1024
	hardReadMaxBytes           = 256 * 1024
	defaultReadMaxLines        = 200
	hardReadMaxLines           = 500
)

type sourceSnapshotContext struct {
	project      *store.Project
	snapshot     *store.SourceSnapshot
	repositories []store.SourceSnapshotRepository
	files        map[string][]store.SourceSnapshotFile
	projectRepos map[string]store.ProjectRepository
}

func (s *Service) resolveSourceSnapshot(ctx context.Context, projectID string, sourceSnapshotID string) (*sourceSnapshotContext, error) {
	_ = ctx
	if s.store == nil {
		return nil, fmt.Errorf("store is required")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	project, err := s.store.GetProjectByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	var snapshot *store.SourceSnapshot
	if strings.TrimSpace(sourceSnapshotID) == "" {
		snapshot, err = s.store.GetLatestSourceSnapshotForProject(project.ID)
	} else {
		snapshot, err = s.store.GetSourceSnapshotByID(strings.TrimSpace(sourceSnapshotID))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sourceOperationError{Code: SourceBlockerSnapshotMissing, Message: "source snapshot not found"}
	}
	if err != nil {
		return nil, err
	}
	if snapshot.ProjectRowID != project.ID || snapshot.ProjectID != project.ProjectID {
		return nil, sourceOperationError{Code: SourceBlockerSnapshotMissing, Message: "source snapshot does not belong to project"}
	}

	snapshotRepos, err := s.store.ListSourceSnapshotRepositories(snapshot.ID)
	if err != nil {
		return nil, err
	}
	projectRepos, err := s.store.ListProjectRepositories(project.ID)
	if err != nil {
		return nil, err
	}
	byRepoID := make(map[string]store.ProjectRepository, len(projectRepos))
	for _, repo := range projectRepos {
		byRepoID[repo.RepoID] = repo
	}

	files := make(map[string][]store.SourceSnapshotFile, len(snapshotRepos))
	for _, snapshotRepo := range snapshotRepos {
		rows, err := s.store.ListSourceSnapshotFiles(snapshotRepo.ID)
		if err != nil {
			return nil, err
		}
		files[snapshotRepo.RepoID] = rows
	}

	return &sourceSnapshotContext{
		project:      project,
		snapshot:     snapshot,
		repositories: snapshotRepos,
		files:        files,
		projectRepos: byRepoID,
	}, nil
}

type sourceOperationError struct {
	Code    string
	Message string
}

func (e sourceOperationError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func (s *Service) ListProjectFiles(ctx context.Context, input FileInventoryInput) (*FileInventoryResult, error) {
	generatedAt := nowSQLUTC()
	resolved, err := s.resolveSourceSnapshot(ctx, input.ProjectID, input.SourceSnapshotID)
	result := &FileInventoryResult{
		ProjectID:        strings.TrimSpace(input.ProjectID),
		SourceSnapshotID: strings.TrimSpace(input.SourceSnapshotID),
		GeneratedAt:      generatedAt,
	}
	if err != nil {
		if blocker, ok := operationBlocker("", err); ok {
			result.Blockers = append(result.Blockers, blocker)
			return result, nil
		}
		return nil, err
	}
	result.ProjectID = resolved.project.ProjectID
	result.SourceSnapshotID = resolved.snapshot.SourceSnapshotID

	allowedRepos := repoIDSet(input.RepoIDs)
	maxResults := boundedPositive(input.MaxResults, defaultInventoryMaxResults, hardInventoryMaxResults)

	for _, snapshotRepo := range resolved.repositories {
		if len(allowedRepos) > 0 {
			if _, ok := allowedRepos[snapshotRepo.RepoID]; !ok {
				continue
			}
		}
		if repo, ok := resolved.projectRepos[snapshotRepo.RepoID]; ok && repo.Enabled == 0 && !input.IncludeDisabled {
			continue
		}
		for _, file := range resolved.files[snapshotRepo.RepoID] {
			if file.Included == 0 && !input.IncludeExcluded {
				continue
			}
			if len(result.Files) >= maxResults {
				result.Truncated = true
				return result, nil
			}
			result.Files = append(result.Files, SourceFileRecord{
				ProjectID:        resolved.project.ProjectID,
				RepoID:           snapshotRepo.RepoID,
				SourceSnapshotID: resolved.snapshot.SourceSnapshotID,
				Path:             file.Path,
				SizeBytes:        file.SizeBytes,
				ContentHash:      file.ContentHash,
				HashAlgorithm:    file.HashAlgorithm,
				Tracked:          file.Tracked == 1,
				Included:         file.Included == 1,
				ExclusionReason:  file.ExclusionReason,
				RedactionStatus:  file.RedactionStatus,
				IndexedAt:        file.CreatedAt,
			})
		}
	}
	return result, nil
}

func (s *Service) ReadProjectFile(ctx context.Context, input BoundedFileReadInput) (*BoundedFileReadResult, error) {
	generatedAt := nowSQLUTC()
	result := &BoundedFileReadResult{
		ProjectID:        strings.TrimSpace(input.ProjectID),
		RepoID:           strings.TrimSpace(input.RepoID),
		SourceSnapshotID: strings.TrimSpace(input.SourceSnapshotID),
		Path:             strings.TrimSpace(input.Path),
		GeneratedAt:      generatedAt,
	}

	resolved, err := s.resolveSourceSnapshot(ctx, input.ProjectID, input.SourceSnapshotID)
	if err != nil {
		if blocker, ok := operationBlocker(input.RepoID, err); ok {
			result.Blockers = append(result.Blockers, blocker)
			return result, nil
		}
		return nil, err
	}
	result.ProjectID = resolved.project.ProjectID
	result.SourceSnapshotID = resolved.snapshot.SourceSnapshotID

	repoID := strings.TrimSpace(input.RepoID)
	repo, ok := resolved.projectRepos[repoID]
	if !ok {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerExcludedPath, Message: "repository is not registered for project"})
		return result, nil
	}
	if repo.Enabled == 0 {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerExcludedPath, Message: "repository is disabled"})
		return result, nil
	}

	relPath, err := validateRepoRelativePath(input.Path)
	if err != nil {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerUnsafePath, Message: err.Error()})
		return result, nil
	}
	result.Path = relPath

	snapshotFile, ok := resolved.includedFile(repoID, relPath)
	if !ok {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerExcludedPath, Message: "path is not included in source snapshot"})
		return result, nil
	}
	result.ContentHash = snapshotFile.ContentHash

	included, reason, err := pathAllowedByRepositoryPolicy(repo, relPath)
	if err != nil {
		return nil, err
	}
	if !included {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerExcludedPath, Message: reason})
		return result, nil
	}
	absolutePath, err := resolveRepoRelativePath(repo.LocalPath, relPath)
	if err != nil {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerUnsafePath, Message: err.Error()})
		return result, nil
	}
	info, err := os.Lstat(absolutePath)
	if err != nil {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerExcludedPath, Message: err.Error()})
		return result, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerExcludedPath, Message: "path is not a regular file"})
		return result, nil
	}
	if repo.MaxFileSizeBytes > 0 && info.Size() > repo.MaxFileSizeBytes {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerOversized, Message: "file exceeds repository max_file_size_bytes"})
		return result, nil
	}

	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, err
	}
	if isBinarySample(data) {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerBinary, Message: "binary file content is not returned"})
		return result, nil
	}
	if computedHash := sha256HexBytes(data); computedHash != "" {
		result.ContentHash = computedHash
	}

	content, lineStart, lineEnd, truncated := boundedLineContent(data, input.LineStart, input.LineEnd, input.MaxBytes)
	redacted, status := redactSourceContent(content)
	if status == RedactionStatusBlocked {
		result.RedactionStatus = status
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerRedactionBlocked, Message: "source content contains blocked secret material"})
		return result, nil
	}
	result.LineStart = lineStart
	result.LineEnd = lineEnd
	result.Content = redacted
	result.SnippetHash = sha256HexString(redacted)
	result.RedactionStatus = status
	result.Truncated = truncated
	return result, nil
}

func (c *sourceSnapshotContext) includedFile(repoID, relPath string) (store.SourceSnapshotFile, bool) {
	for _, file := range c.files[repoID] {
		if file.Path == relPath && file.Included == 1 {
			return file, true
		}
	}
	return store.SourceSnapshotFile{}, false
}

func operationBlocker(repoID string, err error) (SourceBlocker, bool) {
	var opErr sourceOperationError
	if !errors.As(err, &opErr) {
		return SourceBlocker{}, false
	}
	return SourceBlocker{RepoID: repoID, Code: opErr.Code, Message: opErr.Message}, true
}

func validateRepoRelativePath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", fmt.Errorf("path contains NUL")
	}
	if strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("path must use slash separators")
	}
	if path.IsAbs(trimmed) || filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path escapes repository root")
	}
	return cleaned, nil
}

func resolveRepoRelativePath(repoRoot string, relPath string) (string, error) {
	cleaned, err := validateRepoRelativePath(relPath)
	if err != nil {
		return "", err
	}
	return resolveRepoFilePath(repoRoot, cleaned)
}

func pathAllowedByRepositoryPolicy(repo store.ProjectRepository, relPath string) (bool, string, error) {
	cleaned, err := validateRepoRelativePath(relPath)
	if err != nil {
		return false, "invalid_path", err
	}
	allowedRoots, err := decodePolicyStringArray(repo.AllowedRootsJson)
	if err != nil {
		return false, "", fmt.Errorf("decode allowed_roots_json: %w", err)
	}
	ignoredGlobs, err := decodePolicyStringArray(repo.IgnoredGlobsJson)
	if err != nil {
		return false, "", fmt.Errorf("decode ignored_globs_json: %w", err)
	}
	if !pathAllowedByPolicy(cleaned, allowedRoots) {
		return false, "outside_allowed_roots", nil
	}
	if pathMatchesAnyGlob(cleaned, ignoredGlobs) {
		return false, "ignored_glob", nil
	}
	return true, "", nil
}

func isBinarySample(data []byte) bool {
	if len(data) > 8192 {
		data = data[:8192]
	}
	return bytes.IndexByte(data, 0) >= 0
}

func boundedLineContent(data []byte, requestedStart, requestedEnd, requestedMaxBytes int) (string, int, int, bool) {
	maxBytes := boundedPositive(requestedMaxBytes, defaultReadMaxBytes, hardReadMaxBytes)
	lines := bytes.SplitAfter(data, []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		lines = nil
	}
	start := requestedStart
	if start <= 0 {
		start = 1
	}
	end := requestedEnd
	if end <= 0 {
		end = start + defaultReadMaxLines - 1
	}
	if end < start {
		end = start
	}
	if end-start+1 > hardReadMaxLines {
		end = start + hardReadMaxLines - 1
	}
	truncated := false
	if requestedEnd <= 0 && end-start+1 >= defaultReadMaxLines && len(lines) > end {
		truncated = true
	}
	if start > len(lines) {
		return "", start, start - 1, false
	}
	if end > len(lines) {
		end = len(lines)
	}
	var out bytes.Buffer
	for i := start - 1; i < end; i++ {
		next := lines[i]
		if out.Len()+len(next) > maxBytes {
			remaining := maxBytes - out.Len()
			if remaining > 0 {
				out.Write(next[:remaining])
			}
			truncated = true
			break
		}
		out.Write(next)
	}
	if end < len(lines) && (requestedEnd > 0 || requestedEnd <= 0) && end-start+1 >= hardReadMaxLines {
		truncated = true
	}
	return out.String(), start, end, truncated
}

func repoIDSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func boundedPositive(value, defaultValue, hardCap int) int {
	if value <= 0 {
		value = defaultValue
	}
	if value > hardCap {
		value = hardCap
	}
	return value
}

func sortedSourceSnapshotRepos(repos []store.SourceSnapshotRepository) []store.SourceSnapshotRepository {
	out := append([]store.SourceSnapshotRepository(nil), repos...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role == out[j].Role {
			return out[i].RepoID < out[j].RepoID
		}
		return out[i].Role < out[j].Role
	})
	return out
}
