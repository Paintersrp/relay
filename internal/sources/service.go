package sources

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"relay/internal/store"
	"relay/internal/store/generated"
)

const defaultMaxFilesPerRepo = 5000

type Service struct {
	store *store.Store
}

type fileCaptureSummary struct {
	rows     int
	included int
}

func NewService(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) CreateSourceSnapshot(ctx context.Context, input SourceSnapshotInput) (*SourceSnapshotResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store is required")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	project, err := s.store.GetProjectByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	repositories, err := s.selectRepositories(project.ID, input)
	if err != nil {
		return nil, err
	}

	snapshotID, err := newSourceSnapshotID()
	if err != nil {
		return nil, err
	}

	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin source snapshot transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txQueries := generated.New(tx)
	snapshotRow, err := txQueries.CreateSourceSnapshot(ctx, generated.CreateSourceSnapshotParams{
		SourceSnapshotID: snapshotID,
		ProjectRowID:     project.ID,
		ProjectID:        project.ProjectID,
		SnapshotKind:     SnapshotKindUnavailable,
		Status:           SnapshotStatusCreated,
		CompletedAt:      "",
		SummaryJson:      "{}",
	})
	if err != nil {
		return nil, fmt.Errorf("create source snapshot row: %w", err)
	}

	result := &SourceSnapshotResult{
		SourceSnapshotID: snapshotID,
		ProjectID:        project.ProjectID,
		Repositories:     make([]RepositorySnapshotResult, 0, len(repositories)),
	}

	availableCount := 0
	unavailableCount := 0
	cleanCount := 0
	dirtyCount := 0
	fileRowCount := 0
	includedFileRowCount := 0

	for _, repo := range repositories {
		capture, captureErr := s.inspectRepositoryGitStatus(ctx, repo)
		repoRow, err := txQueries.CreateSourceSnapshotRepository(ctx, generated.CreateSourceSnapshotRepositoryParams{
			SourceSnapshotRowID:    snapshotRow.ID,
			ProjectRepositoryRowID: repo.ID,
			RepoID:                 repo.RepoID,
			Role:                   repo.Role,
			LocalPath:              repo.LocalPath,
			DefaultBranch:          repo.DefaultBranch,
			CurrentBranch:          capture.status.CurrentBranch,
			HeadSha:                capture.status.HeadSHA,
			Dirty:                  boolToInt64(capture.status.Dirty),
			StagedCount:            int64(capture.status.StagedCount),
			UnstagedCount:          int64(capture.status.UnstagedCount),
			UntrackedCount:         int64(capture.status.UntrackedCount),
			ChangedFileCount:       int64(capture.status.ChangedFileCount),
			GitStatusAvailable:     boolToInt64(capture.status.GitStatusAvailable),
			GitError:               capture.status.GitError,
			StatusPorcelainHash:    capture.status.PorcelainHash,
		})
		if err != nil {
			return nil, fmt.Errorf("create source snapshot repository row for %s: %w", repo.RepoID, err)
		}

		repositoryResult := RepositorySnapshotResult{
			RepoID:        repo.RepoID,
			Role:          repo.Role,
			LocalPath:     repo.LocalPath,
			DefaultBranch: repo.DefaultBranch,
			GitStatus:     capture.status,
		}

		if captureErr != nil {
			unavailableCount++
			result.Blockers = append(result.Blockers, SourceBlocker{
				RepoID:  repo.RepoID,
				Code:    "git_status_unavailable",
				Message: capture.status.GitError,
			})
			result.Repositories = append(result.Repositories, repositoryResult)
			continue
		}

		availableCount++
		if capture.status.Dirty {
			dirtyCount++
		} else {
			cleanCount++
		}

		recentCommit, commitErr := s.GetRecentCommit(ctx, repo)
		if commitErr != nil {
			result.Blockers = append(result.Blockers, SourceBlocker{
				RepoID:  repo.RepoID,
				Code:    "recent_commit_unavailable",
				Message: commitErr.Error(),
			})
		} else {
			repositoryResult.RecentCommit = &recentCommit
		}

		if input.IncludeFileMetadata {
			fileSummary, blockers, err := s.captureSourceSnapshotFiles(ctx, txQueries, repoRow.ID, repo, capture, input.MaxFilesPerRepo)
			if err != nil {
				return nil, fmt.Errorf("capture source snapshot files for %s: %w", repo.RepoID, err)
			}
			repositoryResult.FileCount = fileSummary.rows
			repositoryResult.IncludedFileCount = fileSummary.included
			fileRowCount += fileSummary.rows
			includedFileRowCount += fileSummary.included
			result.Blockers = append(result.Blockers, blockers...)
		}

		result.Repositories = append(result.Repositories, repositoryResult)
	}

	snapshotStatus, snapshotKind := finalizeSnapshotState(len(repositories), availableCount, unavailableCount, cleanCount, dirtyCount)
	result.Status = snapshotStatus
	result.SnapshotKind = snapshotKind

	summaryJSON, err := json.Marshal(map[string]any{
		"repository_count":             len(repositories),
		"available_repository_count":   availableCount,
		"unavailable_repository_count": unavailableCount,
		"file_row_count":               fileRowCount,
		"included_file_row_count":      includedFileRowCount,
		"blocker_count":                len(result.Blockers),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal source snapshot summary: %w", err)
	}

	if _, err := txQueries.UpdateSourceSnapshotStatus(ctx, generated.UpdateSourceSnapshotStatusParams{
		SourceSnapshotID: snapshotID,
		SnapshotKind:     snapshotKind,
		Status:           snapshotStatus,
		CompletedAt:      nowSQLUTC(),
		SummaryJson:      string(summaryJSON),
	}); err != nil {
		return nil, fmt.Errorf("update source snapshot status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit source snapshot transaction: %w", err)
	}
	committed = true

	return result, nil
}

func (s *Service) selectRepositories(projectRowID int64, input SourceSnapshotInput) ([]store.ProjectRepository, error) {
	allRepositories, err := s.store.ListProjectRepositories(projectRowID)
	if err != nil {
		return nil, err
	}

	byRepoID := make(map[string]store.ProjectRepository, len(allRepositories))
	for _, repo := range allRepositories {
		byRepoID[repo.RepoID] = repo
	}

	if len(input.RepoIDs) > 0 {
		selected := make([]store.ProjectRepository, 0, len(input.RepoIDs))
		seen := map[string]struct{}{}
		for _, rawRepoID := range input.RepoIDs {
			repoID, err := normalizeRepoID(rawRepoID, byRepoID)
			if err != nil {
				return nil, err
			}
			if repoID == "" {
				continue
			}
			if _, exists := seen[repoID]; exists {
				continue
			}
			repo, ok := byRepoID[repoID]
			if !ok {
				return nil, fmt.Errorf("project repository %q not found", repoID)
			}
			if repo.Enabled == 0 && !input.IncludeDisabled {
				return nil, fmt.Errorf("project repository %q is disabled", repoID)
			}
			selected = append(selected, repo)
			seen[repoID] = struct{}{}
		}
		sort.Slice(selected, func(i, j int) bool {
			return selected[i].RepoID < selected[j].RepoID
		})
		return selected, nil
	}

	selected := make([]store.ProjectRepository, 0, len(allRepositories))
	for _, repo := range allRepositories {
		if repo.Enabled == 0 && !input.IncludeDisabled {
			continue
		}
		selected = append(selected, repo)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Role == selected[j].Role {
			return selected[i].RepoID < selected[j].RepoID
		}
		return selected[i].Role < selected[j].Role
	})
	return selected, nil
}

func (s *Service) captureSourceSnapshotFiles(
	ctx context.Context,
	queries *generated.Queries,
	sourceSnapshotRepositoryRowID int64,
	repo store.ProjectRepository,
	capture gitStatusCapture,
	maxFilesPerRepo int,
) (fileCaptureSummary, []SourceBlocker, error) {
	trackedPaths, err := listTrackedFiles(ctx, repo)
	if err != nil {
		return fileCaptureSummary{}, nil, err
	}

	allowedRoots, err := decodePolicyStringArray(repo.AllowedRootsJson)
	if err != nil {
		return fileCaptureSummary{}, nil, fmt.Errorf("decode allowed_roots_json: %w", err)
	}
	ignoredGlobs, err := decodePolicyStringArray(repo.IgnoredGlobsJson)
	if err != nil {
		return fileCaptureSummary{}, nil, fmt.Errorf("decode ignored_globs_json: %w", err)
	}

	candidates := make(map[string]bool, len(trackedPaths)+len(capture.entries))
	for _, trackedPath := range trackedPaths {
		candidates[trackedPath] = true
	}
	if repo.IncludeUntracked == 1 {
		for _, entry := range capture.entries {
			if entry.untracked {
				if _, exists := candidates[entry.path]; !exists {
					candidates[entry.path] = false
				}
			}
		}
	}

	paths := make([]string, 0, len(candidates))
	for candidate := range candidates {
		paths = append(paths, candidate)
	}
	sort.Strings(paths)

	limit := maxFilesPerRepo
	if limit <= 0 {
		limit = defaultMaxFilesPerRepo
	}
	var blockers []SourceBlocker
	if len(paths) > limit {
		blockers = append(blockers, SourceBlocker{
			RepoID:  repo.RepoID,
			Code:    "max_files_per_repo",
			Message: fmt.Sprintf("file metadata capture limited to %d paths", limit),
		})
		paths = paths[:limit]
	}

	summary := fileCaptureSummary{}
	for _, relPath := range paths {
		summary.rows++
		tracked := candidates[relPath]
		rowParams, included, err := buildSourceSnapshotFileParams(repo, sourceSnapshotRepositoryRowID, relPath, tracked, allowedRoots, ignoredGlobs)
		if err != nil {
			return fileCaptureSummary{}, blockers, err
		}
		if included {
			summary.included++
		}
		if _, err := queries.CreateSourceSnapshotFile(ctx, rowParams); err != nil {
			return fileCaptureSummary{}, blockers, fmt.Errorf("create source snapshot file row for %s: %w", relPath, err)
		}
	}

	return summary, blockers, nil
}

func buildSourceSnapshotFileParams(
	repo store.ProjectRepository,
	sourceSnapshotRepositoryRowID int64,
	relPath string,
	tracked bool,
	allowedRoots []string,
	ignoredGlobs []string,
) (generated.CreateSourceSnapshotFileParams, bool, error) {
	params := generated.CreateSourceSnapshotFileParams{
		SourceSnapshotRepositoryRowID: sourceSnapshotRepositoryRowID,
		Path:                          relPath,
		HashAlgorithm:                 "sha256",
		Tracked:                       boolToInt64(tracked),
		Included:                      0,
		RedactionStatus:               RedactionStatusNotScanned,
	}

	if !pathAllowedByPolicy(relPath, allowedRoots) {
		params.ExclusionReason = "outside_allowed_roots"
		return params, false, nil
	}
	if pathMatchesAnyGlob(relPath, ignoredGlobs) {
		params.ExclusionReason = "ignored_glob"
		return params, false, nil
	}

	absolutePath, err := resolveRepoFilePath(repo.LocalPath, relPath)
	if err != nil {
		params.ExclusionReason = "invalid_path"
		return params, false, nil
	}
	info, err := os.Lstat(absolutePath)
	if err != nil {
		params.ExclusionReason = "stat_failed"
		return params, false, nil
	}
	params.SizeBytes = info.Size()
	if info.Mode()&os.ModeSymlink != 0 {
		params.ExclusionReason = "symlink_unsupported"
		return params, false, nil
	}
	if !info.Mode().IsRegular() {
		params.ExclusionReason = "not_regular_file"
		return params, false, nil
	}
	if info.Size() > repo.MaxFileSizeBytes {
		params.ExclusionReason = "max_file_size_exceeded"
		return params, false, nil
	}

	contentHash, err := hashFileSHA256(absolutePath)
	if err != nil {
		params.ExclusionReason = "hash_failed"
		return params, false, nil
	}

	params.ContentHash = contentHash
	params.Included = 1
	return params, true, nil
}

func finalizeSnapshotState(repoCount, availableCount, unavailableCount, cleanCount, dirtyCount int) (string, string) {
	if repoCount == 0 || availableCount == 0 {
		return SnapshotStatusBlocked, SnapshotKindUnavailable
	}
	if unavailableCount > 0 {
		return SnapshotStatusPartial, SnapshotKindMixed
	}
	if dirtyCount == 0 {
		return SnapshotStatusCreated, SnapshotKindCleanCommit
	}
	if cleanCount == 0 {
		return SnapshotStatusCreated, SnapshotKindDirtyWorktree
	}
	return SnapshotStatusCreated, SnapshotKindMixed
}

func decodePolicyStringArray(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func pathAllowedByPolicy(relPath string, allowedRoots []string) bool {
	if len(allowedRoots) == 0 {
		return true
	}
	for _, allowedRoot := range allowedRoots {
		normalizedRoot := strings.TrimSpace(allowedRoot)
		if normalizedRoot == "" || normalizedRoot == "." {
			return true
		}
		if relPath == normalizedRoot || strings.HasPrefix(relPath, normalizedRoot+"/") {
			return true
		}
	}
	return false
}

func pathMatchesAnyGlob(relPath string, globs []string) bool {
	for _, glob := range globs {
		if matchPolicyGlob(strings.TrimSpace(glob), relPath) {
			return true
		}
	}
	return false
}

func matchPolicyGlob(pattern, relPath string) bool {
	if pattern == "" {
		return false
	}
	return matchPolicySegments(splitPolicyPath(pattern), splitPolicyPath(relPath))
}

func splitPolicyPath(value string) []string {
	cleaned := path.Clean(strings.TrimSpace(value))
	if cleaned == "." {
		return nil
	}
	return strings.Split(cleaned, "/")
}

func matchPolicySegments(patternSegments, pathSegments []string) bool {
	if len(patternSegments) == 0 {
		return len(pathSegments) == 0
	}
	if patternSegments[0] == "**" {
		if matchPolicySegments(patternSegments[1:], pathSegments) {
			return true
		}
		if len(pathSegments) == 0 {
			return false
		}
		return matchPolicySegments(patternSegments, pathSegments[1:])
	}
	if len(pathSegments) == 0 {
		return false
	}
	ok, err := path.Match(patternSegments[0], pathSegments[0])
	if err != nil || !ok {
		return false
	}
	return matchPolicySegments(patternSegments[1:], pathSegments[1:])
}

func resolveRepoFilePath(repoRoot, relPath string) (string, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, filepath.FromSlash(relPath))
	absolutePath, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	relativeToRoot, err := filepath.Rel(root, absolutePath)
	if err != nil {
		return "", err
	}
	if relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository root: %s", relPath)
	}
	return absolutePath, nil
}

func hashFileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func newSourceSnapshotID() (string, error) {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate source snapshot id: %w", err)
	}
	return "srcsnap_" + hex.EncodeToString(data[:]), nil
}

func normalizeRepoRelativePath(value string) string {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "." {
		return ""
	}
	return normalized
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func nowSQLUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
