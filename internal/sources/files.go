package sources

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	Blocker SourceBlocker
}

func (e sourceOperationError) Error() string {
	code := e.Code
	message := e.Message
	if code == "" {
		code = e.Blocker.Code
	}
	if message == "" {
		message = e.Blocker.Message
	}
	if message == "" {
		return code
	}
	return code + ": " + message
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
			result.FreshnessReport = unavailableFreshnessReport(result.SourceSnapshotID, generatedAt, blocker)
			return result, nil
		}
		return nil, err
	}
	result.ProjectID = resolved.project.ProjectID
	result.SourceSnapshotID = resolved.snapshot.SourceSnapshotID
	result.FreshnessReport = s.evaluateSourceSnapshotFreshness(ctx, resolved, generatedAt)

	repoID, err := normalizeRepoID(input.RepoID, resolved.projectRepos)
	if err != nil {
		if blocker, ok := operationBlocker(input.RepoID, err); ok {
			result.Blockers = append(result.Blockers, blocker)
			return result, nil
		}
		return nil, err
	}
	result.RepoID = repoID
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

	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	sample := make([]byte, 8192)
	n, err := file.Read(sample)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if isBinarySample(sample[:n]) {
		result.Blockers = append(result.Blockers, SourceBlocker{RepoID: repoID, Code: SourceBlockerBinary, Message: "binary file content is not returned"})
		return result, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	currentHash, err := streamFileSHA256(file)
	if err != nil {
		return nil, err
	}
	result.CurrentHash = currentHash
	if snapshotFile.ContentHash != "" && currentHash != "" && snapshotFile.ContentHash != currentHash {
		blocker := SourceBlocker{RepoID: repoID, Code: SourceBlockerSnapshotFileChanged, Message: "current file hash differs from source snapshot hash"}
		result.Blockers = append(result.Blockers, blocker)
		result.FreshnessReport = appendFreshnessBlocker(result.FreshnessReport, blocker)
		return result, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	content, lineStart, lineEnd, truncated, err := streamBoundedLineContent(file, input.LineStart, input.LineEnd, input.MaxBytes)
	if err != nil {
		return nil, err
	}
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

func (s *Service) GetSourceSnapshotFreshness(ctx context.Context, projectID string, sourceSnapshotID string) (SourceFreshnessReport, error) {
	generatedAt := nowSQLUTC()
	resolved, err := s.resolveSourceSnapshot(ctx, projectID, sourceSnapshotID)
	if err != nil {
		return SourceFreshnessReport{}, err
	}
	return s.evaluateSourceSnapshotFreshness(ctx, resolved, generatedAt), nil
}

func (s *Service) evaluateSourceSnapshotFreshness(ctx context.Context, resolved *sourceSnapshotContext, generatedAt string) SourceFreshnessReport {
	report := SourceFreshnessReport{
		Status:              SourceFreshnessStatusFresh,
		ReusableForHandoff:  true,
		SourceSnapshotID:    resolved.snapshot.SourceSnapshotID,
		GeneratedAt:         generatedAt,
		SnapshotCreatedAt:   resolved.snapshot.CreatedAt,
		SnapshotCompletedAt: resolved.snapshot.CompletedAt,
		AgeSeconds:          freshnessAgeSeconds(snapshotFreshnessAnchor(resolved.snapshot), generatedAt),
		MaxAgeSeconds:       DefaultSourceSnapshotFreshnessMaxAgeSeconds,
		RepositoryReports:   make([]RepositoryFreshnessReport, 0, len(resolved.repositories)),
	}

	availableCount := 0
	unavailableCount := 0
	dirtyCount := 0
	driftedCount := 0
	for _, snapshotRepo := range sortedSourceSnapshotRepos(resolved.repositories) {
		captured := repositoryGitStatusFromSnapshotRow(snapshotRepo)
		repoReport := RepositoryFreshnessReport{
			RepoID:              snapshotRepo.RepoID,
			Status:              SourceFreshnessStatusFresh,
			ReusableForHandoff:  true,
			CapturedBranch:      captured.CurrentBranch,
			CapturedHeadSHA:     captured.HeadSHA,
			CapturedDirty:       captured.Dirty,
			CapturedChangeCount: captured.ChangedFileCount,
			GitStatusAvailable:  captured.GitStatusAvailable,
		}
		repo, ok := resolved.projectRepos[snapshotRepo.RepoID]
		if !ok || !captured.GitStatusAvailable {
			unavailableCount++
			repoReport.Status = SourceFreshnessStatusBlocked
			repoReport.ReusableForHandoff = false
			repoReport.Blockers = append(repoReport.Blockers, freshnessBlocker(snapshotRepo.RepoID, SourceFreshnessCodeUnavailable, "repository git status metadata is unavailable for this source snapshot"))
			report.RepositoryReports = append(report.RepositoryReports, repoReport)
			report.Blockers = append(report.Blockers, repoReport.Blockers...)
			continue
		}

		current, err := s.inspectRepositoryGitStatus(ctx, repo)
		if err != nil {
			unavailableCount++
			repoReport.Status = SourceFreshnessStatusPartial
			repoReport.ReusableForHandoff = false
			repoReport.GitStatusAvailable = false
			repoReport.Blockers = append(repoReport.Blockers, freshnessBlocker(snapshotRepo.RepoID, SourceFreshnessCodeUnavailable, "current repository git status is unavailable for source snapshot freshness comparison"))
			report.RepositoryReports = append(report.RepositoryReports, repoReport)
			report.Blockers = append(report.Blockers, repoReport.Blockers...)
			continue
		}

		availableCount++
		repoReport.CurrentBranch = current.status.CurrentBranch
		repoReport.CurrentHeadSHA = current.status.HeadSHA
		repoReport.CurrentDirty = current.status.Dirty
		repoReport.CurrentChangeCount = current.status.ChangedFileCount

		drifted := false
		switch {
		case captured.HeadSHA != "" && current.status.HeadSHA != "" && captured.HeadSHA != current.status.HeadSHA:
			drifted = true
		case captured.PorcelainHash != "" && current.status.PorcelainHash != "" && captured.PorcelainHash != current.status.PorcelainHash:
			drifted = true
		case !captured.Dirty && current.status.Dirty:
			drifted = true
		}
		if drifted {
			driftedCount++
			repoReport.Status = SourceFreshnessStatusDrifted
			repoReport.ReusableForHandoff = false
			repoReport.Warnings = append(repoReport.Warnings, freshnessBlocker(snapshotRepo.RepoID, SourceFreshnessCodeDrifted, "current repository metadata differs from the captured source snapshot metadata"))
		} else if current.status.Dirty || captured.Dirty {
			dirtyCount++
			repoReport.Status = SourceFreshnessStatusDirtyWorktree
			repoReport.ReusableForHandoff = false
			repoReport.Warnings = append(repoReport.Warnings, freshnessBlocker(snapshotRepo.RepoID, SourceFreshnessCodeDirtyWorktree, "repository has uncommitted or untracked changes associated with this source snapshot"))
		}
		report.RepositoryReports = append(report.RepositoryReports, repoReport)
		report.Warnings = append(report.Warnings, repoReport.Warnings...)
		report.Blockers = append(report.Blockers, repoReport.Blockers...)
	}

	switch {
	case len(resolved.repositories) == 0 || availableCount == 0 || resolved.snapshot.Status == SnapshotStatusBlocked:
		report.Status = SourceFreshnessStatusBlocked
	case unavailableCount > 0 || resolved.snapshot.Status == SnapshotStatusPartial:
		report.Status = SourceFreshnessStatusPartial
	case driftedCount > 0:
		report.Status = SourceFreshnessStatusDrifted
	case report.AgeSeconds > report.MaxAgeSeconds:
		report.Status = SourceFreshnessStatusStaleByAge
		report.Warnings = append(report.Warnings, freshnessBlocker("", SourceFreshnessCodeStale, "source snapshot is older than the freshness guidance window"))
	case dirtyCount > 0:
		report.Status = SourceFreshnessStatusDirtyWorktree
	default:
		report.Status = SourceFreshnessStatusFresh
	}
	report.ReusableForHandoff = report.Status == SourceFreshnessStatusFresh
	if !report.ReusableForHandoff {
		report.NextActions = append(report.NextActions, SourceFreshnessNextAction{
			Action: "create_source_snapshot",
			Reason: "Create a new source snapshot after repository cleanup or project repository configuration correction.",
		})
	}
	return report
}

func snapshotFreshnessAnchor(snapshot *store.SourceSnapshot) string {
	if strings.TrimSpace(snapshot.CompletedAt) != "" {
		return snapshot.CompletedAt
	}
	return snapshot.CreatedAt
}

func unavailableFreshnessReport(sourceSnapshotID string, generatedAt string, blocker SourceBlocker) SourceFreshnessReport {
	if blocker.Code == "" {
		blocker.Code = SourceFreshnessCodeUnavailable
	}
	return SourceFreshnessReport{
		Status:             SourceFreshnessStatusBlocked,
		ReusableForHandoff: false,
		SourceSnapshotID:   strings.TrimSpace(sourceSnapshotID),
		GeneratedAt:        generatedAt,
		MaxAgeSeconds:      DefaultSourceSnapshotFreshnessMaxAgeSeconds,
		Blockers:           []SourceBlocker{blocker},
		NextActions: []SourceFreshnessNextAction{{
			Action: "create_source_snapshot",
			Reason: "Create a new source snapshot after repository cleanup or project repository configuration correction.",
		}},
	}
}

func appendFreshnessBlocker(report SourceFreshnessReport, blocker SourceBlocker) SourceFreshnessReport {
	report.Blockers = append(report.Blockers, blocker)
	report.ReusableForHandoff = false
	if report.Status == "" || report.Status == SourceFreshnessStatusFresh {
		report.Status = SourceFreshnessStatusBlocked
	}
	if len(report.NextActions) == 0 {
		report.NextActions = append(report.NextActions, SourceFreshnessNextAction{
			Action: "create_source_snapshot",
			Reason: "Create a new source snapshot after repository cleanup or project repository configuration correction.",
		})
	}
	return report
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
	if opErr.Blocker.Code != "" {
		blocker := opErr.Blocker
		if blocker.RepoID == "" {
			blocker.RepoID = repoID
		}
		return blocker, true
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

func streamFileSHA256(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func streamBoundedLineContent(r io.Reader, requestedStart, requestedEnd, requestedMaxBytes int) (string, int, int, bool, error) {
	maxBytes := boundedPositive(requestedMaxBytes, defaultReadMaxBytes, hardReadMaxBytes)
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

	reader := bufio.NewReader(r)
	var out bytes.Buffer
	lineNo := 0
	lastWritten := start - 1
	truncated := false
	for {
		next, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", start, lastWritten, false, err
		}
		if err == io.EOF && next == "" {
			break
		}
		lineNo++
		if lineNo < start {
			if err == io.EOF {
				break
			}
			continue
		}
		if lineNo > end {
			if requestedEnd <= 0 || end-start+1 >= hardReadMaxLines {
				truncated = true
			}
			break
		}
		if out.Len()+len(next) > maxBytes {
			remaining := maxBytes - out.Len()
			if remaining > 0 {
				out.WriteString(next[:remaining])
			}
			lastWritten = lineNo
			truncated = true
			break
		}
		out.WriteString(next)
		lastWritten = lineNo
		if err == io.EOF {
			break
		}
	}
	if requestedEnd <= 0 && lineNo > end {
		truncated = true
	}
	if start > lineNo {
		return "", start, start - 1, false, nil
	}
	return out.String(), start, lastWritten, truncated, nil
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

func normalizeRepoIDList(repoIDs []string, repos map[string]store.ProjectRepository) ([]string, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(repoIDs))
	seen := map[string]struct{}{}
	for _, raw := range repoIDs {
		repoID, err := normalizeRepoID(raw, repos)
		if err != nil {
			return nil, err
		}
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		out = append(out, repoID)
		seen[repoID] = struct{}{}
	}
	return out, nil
}

func normalizeRepoID(raw string, repos map[string]store.ProjectRepository) (string, error) {
	repoID := strings.TrimSpace(raw)
	if repoID == "" {
		return "", nil
	}
	resolution := ResolveProjectRepository(repoID, repos)
	if len(resolution.Blockers) > 0 {
		return "", sourceOperationError{Blocker: resolution.Blockers[0]}
	}
	return resolution.CanonicalRepoID, nil
}

func repoIDAliasMatches(alias, registered string) bool {
	return repositoryAliasMatches(strings.TrimSpace(alias), acceptedRepositoryAliases(registered))
}
