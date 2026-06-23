package auditor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/sources"
	"relay/internal/store"
)

const (
	defaultLocalAuditMaxFiles     = 200
	hardLocalAuditMaxFiles        = 1000
	defaultLocalAuditMaxBytes     = 256 * 1024
	hardLocalAuditMaxBytes        = 1024 * 1024
	defaultLocalAuditContextLines = 3
	hardLocalAuditContextLines    = 5
	hardLocalAuditPaths           = 50
	hardLocalAuditSearchTerms     = 20
)

type LocalAuditService struct {
	store   *store.Store
	sources *sources.Service
}

type LocalAuditInput struct {
	Mode             string   `json:"mode"`
	ProjectID        string   `json:"project_id"`
	RepoIDs          []string `json:"repo_ids"`
	PlanID           string   `json:"plan_id"`
	PassID           string   `json:"pass_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	ContextPacketID  string   `json:"context_packet_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Paths            []string `json:"paths"`
	SearchTerms      []string `json:"search_terms"`
	DiffMode         string   `json:"diff_mode"`
	MaxFiles         int      `json:"max_files"`
	MaxBytes         int      `json:"max_bytes"`
	ContextLines     int      `json:"context_lines"`
}

type LocalAuditResult struct {
	Success          bool              `json:"success"`
	AuditID          string            `json:"audit_id"`
	Mode             string            `json:"mode"`
	Status           string            `json:"status"`
	ProjectID        string            `json:"project_id"`
	ManifestPath     string            `json:"manifest_path"`
	PacketPath       string            `json:"packet_path"`
	InputSummaryPath string            `json:"input_summary_path"`
	Blockers         []string          `json:"blockers"`
	Warnings         []string          `json:"warnings"`
	Record           *store.LocalAudit `json:"-"`
}

type LocalAuditRecordResult struct {
	AuditID          string   `json:"audit_id"`
	Mode             string   `json:"mode"`
	Status           string   `json:"status"`
	ProjectID        string   `json:"project_id"`
	Title            string   `json:"title"`
	PlanID           string   `json:"plan_id"`
	PassID           string   `json:"pass_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	ContextPacketID  string   `json:"context_packet_id"`
	ManifestPath     string   `json:"manifest_path"`
	PacketPath       string   `json:"packet_path"`
	InputSummaryPath string   `json:"input_summary_path"`
	Blockers         []string `json:"blockers"`
	Warnings         []string `json:"warnings"`
	CreatedAt        string   `json:"created_at"`
	CompletedAt      string   `json:"completed_at"`
}

type LocalAuditManifest struct {
	SchemaVersion    string                 `json:"schema_version"`
	AuditID          string                 `json:"audit_id"`
	Mode             string                 `json:"mode"`
	ProjectID        string                 `json:"project_id"`
	RepoIDs          []string               `json:"repo_ids"`
	PlanID           string                 `json:"plan_id"`
	PassID           string                 `json:"pass_id"`
	SourceSnapshotID string                 `json:"source_snapshot_id"`
	ContextPacketID  string                 `json:"context_packet_id"`
	GeneratedAt      string                 `json:"generated_at"`
	Status           string                 `json:"status"`
	Evidence         LocalAuditEvidence     `json:"evidence"`
	ArtifactPaths    map[string]string      `json:"artifact_paths"`
	Warnings         []string               `json:"warnings"`
	Blockers         []string               `json:"blockers"`
	LocalOnly        bool                   `json:"local_only"`
	RemoteEvidence   map[string]string      `json:"remote_evidence"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

type LocalAuditEvidence struct {
	SourceSnapshot map[string]interface{}        `json:"source_snapshot"`
	GitStatus      []sources.RepositoryGitStatus `json:"git_status"`
	RecentCommits  []sources.RecentCommit        `json:"recent_commits"`
	ChangedFiles   []sources.ChangedFile         `json:"changed_files"`
	Diffs          []LocalAuditDiffMetadata      `json:"diffs"`
	FileInventory  map[string]interface{}        `json:"file_inventory"`
	SearchMatches  []sources.SourceSearchMatch   `json:"search_matches"`
	FileReads      []LocalAuditFileReadMetadata  `json:"file_reads,omitempty"`
}

type LocalAuditDiffMetadata struct {
	RepoID          string `json:"repo_id"`
	Mode            string `json:"mode"`
	ContentHash     string `json:"content_hash"`
	Truncated       bool   `json:"truncated"`
	MaxBytes        int    `json:"max_bytes"`
	RedactionStatus string `json:"redaction_status"`
	PreviewBytes    int    `json:"preview_bytes"`
}

type LocalAuditFileReadMetadata struct {
	RepoID          string `json:"repo_id"`
	Path            string `json:"path"`
	LineStart       int    `json:"line_start"`
	LineEnd         int    `json:"line_end"`
	ContentHash     string `json:"content_hash"`
	SnippetHash     string `json:"snippet_hash"`
	RedactionStatus string `json:"redaction_status"`
	Truncated       bool   `json:"truncated"`
}

func NewLocalAuditService(st *store.Store) *LocalAuditService {
	return &LocalAuditService{store: st, sources: sources.NewService(st)}
}

func (s *LocalAuditService) Create(ctx context.Context, input LocalAuditInput) (*LocalAuditResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store is required")
	}
	normalized, err := normalizeLocalAuditInput(input)
	if err != nil {
		return nil, err
	}
	project, err := s.store.GetProjectByProjectID(normalized.ProjectID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	auditID, err := newLocalAuditID(now)
	if err != nil {
		return nil, err
	}
	date := now.Format("2006-01-02")
	slug := localAuditSlug(normalized.Title, normalized.Mode)

	manifest := LocalAuditManifest{
		SchemaVersion:    "1.0.0",
		AuditID:          auditID,
		Mode:             normalized.Mode,
		ProjectID:        project.ProjectID,
		RepoIDs:          normalized.RepoIDs,
		PlanID:           normalized.PlanID,
		PassID:           normalized.PassID,
		SourceSnapshotID: normalized.SourceSnapshotID,
		ContextPacketID:  normalized.ContextPacketID,
		GeneratedAt:      now.Format(time.RFC3339),
		Status:           string(LocalAuditStatusCreated),
		Evidence: LocalAuditEvidence{
			SourceSnapshot: map[string]interface{}{},
			FileInventory:  map[string]interface{}{},
		},
		ArtifactPaths:  map[string]string{},
		LocalOnly:      true,
		RemoteEvidence: notUsedRemoteEvidence(),
		Metadata:       map[string]interface{}{},
	}

	var diffPreviews []sources.BoundedDiff
	switch LocalAuditMode(normalized.Mode) {
	case LocalAuditModeRecentCommit:
		s.collectRecentCommit(ctx, project, normalized, &manifest, &diffPreviews)
	case LocalAuditModeSelectedPassChanges:
		s.collectSelectedPassChanges(ctx, project, normalized, &manifest, &diffPreviews)
	case LocalAuditModeFeatureSlice:
		s.collectFeatureSlice(ctx, project, normalized, &manifest)
	case LocalAuditModeFullRepository:
		s.collectFullRepository(ctx, project, normalized, &manifest)
	}

	manifest.Status = localAuditStatus(manifest.Blockers, manifest.Warnings)
	inputSummary := renderLocalAuditInputSummary(normalized, manifest)
	packet := renderLocalAuditPacket(normalized, manifest, diffPreviews)

	inputSummaryPath, err := artifacts.WriteAudit(date, slug, "local_audit_input_summary", []byte(inputSummary))
	if err != nil {
		return nil, err
	}
	packetPath, err := artifacts.WriteAudit(date, slug, "local_audit_packet", []byte(packet))
	if err != nil {
		return nil, err
	}
	manifest.ArtifactPaths["packet"] = packetPath
	manifest.ArtifactPaths["input_summary"] = inputSummaryPath
	manifest.ArtifactPaths["manifest"] = mustAuditPath(date, slug, "local_audit_manifest_json")
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	manifestPath, err := artifacts.WriteAudit(date, slug, "local_audit_manifest_json", manifestJSON)
	if err != nil {
		return nil, err
	}

	blockersJSON, _ := json.Marshal(manifest.Blockers)
	warningsJSON, _ := json.Marshal(manifest.Warnings)
	record, err := s.store.CreateLocalAudit(store.CreateLocalAuditParams{
		AuditID:          auditID,
		ProjectRowID:     project.ID,
		ProjectID:        project.ProjectID,
		Mode:             normalized.Mode,
		Title:            normalized.Title,
		Status:           manifest.Status,
		PlanID:           normalized.PlanID,
		PassID:           normalized.PassID,
		SourceSnapshotID: manifest.SourceSnapshotID,
		ContextPacketID:  normalized.ContextPacketID,
		ManifestPath:     manifestPath,
		PacketPath:       packetPath,
		InputSummaryPath: inputSummaryPath,
		BlockersJSON:     string(blockersJSON),
		WarningsJSON:     string(warningsJSON),
		CompletedAt:      now.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		return nil, err
	}

	return &LocalAuditResult{
		Success:          true,
		AuditID:          auditID,
		Mode:             normalized.Mode,
		Status:           manifest.Status,
		ProjectID:        project.ProjectID,
		ManifestPath:     manifestPath,
		PacketPath:       packetPath,
		InputSummaryPath: inputSummaryPath,
		Blockers:         manifest.Blockers,
		Warnings:         manifest.Warnings,
		Record:           record,
	}, nil
}

func (s *LocalAuditService) Get(ctx context.Context, auditID string) (*LocalAuditRecordResult, error) {
	_ = ctx
	row, err := s.store.GetLocalAuditByAuditID(strings.TrimSpace(auditID))
	if err != nil {
		return nil, err
	}
	return localAuditRecordResult(*row), nil
}

func (s *LocalAuditService) ListByProject(ctx context.Context, projectID string, mode string, limit int64) ([]LocalAuditRecordResult, error) {
	_ = ctx
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rows []store.LocalAudit
	var err error
	if strings.TrimSpace(mode) == "" {
		rows, err = s.store.ListLocalAuditsByProject(strings.TrimSpace(projectID), limit)
	} else {
		rows, err = s.store.ListLocalAuditsByProjectAndMode(strings.TrimSpace(projectID), strings.TrimSpace(mode), limit)
	}
	if err != nil {
		return nil, err
	}
	out := make([]LocalAuditRecordResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, *localAuditRecordResult(row))
	}
	return out, nil
}

func (s *LocalAuditService) collectRecentCommit(ctx context.Context, project *store.Project, input LocalAuditInput, manifest *LocalAuditManifest, diffPreviews *[]sources.BoundedDiff) {
	repo, err := s.store.GetProjectRepositoryByRepoID(store.GetProjectRepositoryByRepoIDParams{ProjectRowID: project.ID, RepoID: input.RepoIDs[0]})
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "repository not found: "+input.RepoIDs[0])
		return
	}
	commit, err := s.sources.GetRecentCommit(ctx, *repo)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "recent commit unavailable for "+repo.RepoID+": "+err.Error())
	} else {
		manifest.Evidence.RecentCommits = append(manifest.Evidence.RecentCommits, commit)
	}
	files, err := s.sources.GetRecentCommitChangedFiles(ctx, *repo)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "recent commit changed files unavailable for "+repo.RepoID+": "+err.Error())
	} else {
		manifest.Evidence.ChangedFiles = append(manifest.Evidence.ChangedFiles, limitChangedFiles(files, input.MaxFiles)...)
		if len(files) > input.MaxFiles {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("changed files limited to %d entries for %s", input.MaxFiles, repo.RepoID))
		}
	}
	diff, err := s.sources.GetRecentCommitBoundedDiff(ctx, *repo, input.MaxBytes, input.ContextLines)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "recent commit diff unavailable for "+repo.RepoID+": "+err.Error())
		return
	}
	if diff.RedactionStatus == sources.RedactionStatusBlocked {
		manifest.Blockers = append(manifest.Blockers, "recent commit diff blocked by redaction for "+repo.RepoID)
		return
	}
	if diff.Truncated {
		manifest.Warnings = append(manifest.Warnings, "recent commit diff was truncated for "+repo.RepoID)
	}
	manifest.Evidence.Diffs = append(manifest.Evidence.Diffs, diffMetadata(diff))
	*diffPreviews = append(*diffPreviews, diff)
}

func (s *LocalAuditService) collectSelectedPassChanges(ctx context.Context, project *store.Project, input LocalAuditInput, manifest *LocalAuditManifest, diffPreviews *[]sources.BoundedDiff) {
	plan, err := s.store.GetPlanByPlanID(input.PlanID)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "plan not found: "+input.PlanID)
		return
	}
	pass, err := s.store.GetPlanPassByPassID(plan.ID, input.PassID)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "plan pass not found: "+input.PassID)
		return
	}
	manifest.Metadata["plan"] = map[string]interface{}{"title": plan.Title, "status": plan.Status}
	manifest.Metadata["pass"] = boundedPassMetadata(*pass)
	if input.ContextPacketID != "" {
		packet, err := s.store.GetContextPacketByID(input.ContextPacketID)
		if err != nil || packet.ProjectID != project.ProjectID || packet.PlanID != input.PlanID || packet.PassID != input.PassID {
			manifest.Blockers = append(manifest.Blockers, "context packet not found for selected plan/pass: "+input.ContextPacketID)
			return
		}
	}
	if input.SourceSnapshotID != "" {
		snapshot, err := s.store.GetSourceSnapshotByID(input.SourceSnapshotID)
		if err != nil || snapshot.ProjectRowID != project.ID {
			manifest.Blockers = append(manifest.Blockers, "source snapshot not found for project: "+input.SourceSnapshotID)
			return
		}
		manifest.Evidence.SourceSnapshot = sourceSnapshotMetadata(*snapshot)
	}
	repos := s.selectAuditRepositories(project, input, manifest)
	for _, repo := range repos {
		status, err := s.sources.GetRepositoryGitStatus(ctx, repo)
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "git status unavailable for "+repo.RepoID+": "+err.Error())
		}
		manifest.Evidence.GitStatus = append(manifest.Evidence.GitStatus, status)
		files, err := s.sources.GetChangedFiles(ctx, repo, input.DiffMode)
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "changed files unavailable for "+repo.RepoID+": "+err.Error())
		} else {
			manifest.Evidence.ChangedFiles = append(manifest.Evidence.ChangedFiles, limitChangedFiles(files, input.MaxFiles)...)
		}
		diff, err := s.sources.GetBoundedDiff(ctx, repo, input.DiffMode, input.MaxBytes, input.ContextLines)
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "diff unavailable for "+repo.RepoID+": "+err.Error())
			continue
		}
		if diff.RedactionStatus == sources.RedactionStatusBlocked {
			manifest.Blockers = append(manifest.Blockers, "diff blocked by redaction for "+repo.RepoID)
			continue
		}
		if diff.Truncated {
			manifest.Warnings = append(manifest.Warnings, "diff was truncated for "+repo.RepoID)
		}
		manifest.Evidence.Diffs = append(manifest.Evidence.Diffs, diffMetadata(diff))
		*diffPreviews = append(*diffPreviews, diff)
	}
	if len(manifest.Evidence.ChangedFiles) == 0 && len(manifest.Blockers) == 0 {
		manifest.Warnings = append(manifest.Warnings, "no changed files found for selected pass diff mode")
	}
}

func (s *LocalAuditService) collectFeatureSlice(ctx context.Context, project *store.Project, input LocalAuditInput, manifest *LocalAuditManifest) {
	snapshotID := s.ensureSnapshot(ctx, project, input, manifest, true)
	if snapshotID == "" {
		return
	}
	inventory, err := s.sources.ListProjectFiles(ctx, sources.FileInventoryInput{
		ProjectID:        project.ProjectID,
		SourceSnapshotID: snapshotID,
		RepoIDs:          input.RepoIDs,
		MaxResults:       input.MaxFiles,
	})
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "file inventory unavailable: "+err.Error())
		return
	}
	manifest.Evidence.FileInventory = fileInventoryMetadata(inventory)
	manifest.Blockers = append(manifest.Blockers, sourceBlockerMessages(inventory.Blockers)...)
	for _, term := range input.SearchTerms {
		result, err := s.sources.SearchProjectFiles(ctx, sources.SourceSearchInput{
			ProjectID:        project.ProjectID,
			SourceSnapshotID: snapshotID,
			RepoIDs:          input.RepoIDs,
			Pattern:          term,
			Literal:          true,
			ContextLines:     input.ContextLines,
			MaxResults:       input.MaxFiles,
			MaxBytes:         input.MaxBytes,
		})
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "search unavailable for term "+term+": "+err.Error())
			continue
		}
		if result.Truncated {
			manifest.Warnings = append(manifest.Warnings, "search results truncated for term "+term)
		}
		manifest.Blockers = append(manifest.Blockers, sourceBlockerMessages(result.Blockers)...)
		manifest.Evidence.SearchMatches = append(manifest.Evidence.SearchMatches, result.Matches...)
	}
	repoID := ""
	if len(input.RepoIDs) == 1 {
		repoID = input.RepoIDs[0]
	} else if project.DefaultRepositoryID != "" {
		repoID = project.DefaultRepositoryID
	}
	for _, p := range input.Paths {
		if repoID == "" {
			manifest.Warnings = append(manifest.Warnings, "skipped explicit file read without a single repo_id: "+p)
			continue
		}
		read, err := s.sources.ReadProjectFile(ctx, sources.BoundedFileReadInput{
			ProjectID:        project.ProjectID,
			SourceSnapshotID: snapshotID,
			RepoID:           repoID,
			Path:             p,
			MaxBytes:         input.MaxBytes,
		})
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "file read unavailable for "+p+": "+err.Error())
			continue
		}
		manifest.Blockers = append(manifest.Blockers, sourceBlockerMessages(read.Blockers)...)
		if len(read.Blockers) == 0 {
			manifest.Evidence.FileReads = append(manifest.Evidence.FileReads, fileReadMetadata(read))
		}
	}
	if len(manifest.Evidence.SearchMatches) == 0 && len(manifest.Evidence.FileReads) == 0 && len(manifest.Blockers) == 0 {
		manifest.Warnings = append(manifest.Warnings, "feature slice produced no search matches or file reads")
	}
}

func (s *LocalAuditService) collectFullRepository(ctx context.Context, project *store.Project, input LocalAuditInput, manifest *LocalAuditManifest) {
	repos := s.selectAuditRepositories(project, input, manifest)
	if len(repos) == 0 {
		return
	}
	snapshotID := s.ensureSnapshot(ctx, project, input, manifest, true)
	if snapshotID == "" {
		return
	}
	inventory, err := s.sources.ListProjectFiles(ctx, sources.FileInventoryInput{
		ProjectID:        project.ProjectID,
		SourceSnapshotID: snapshotID,
		RepoIDs:          input.RepoIDs,
		MaxResults:       input.MaxFiles,
	})
	if err == nil {
		manifest.Evidence.FileInventory = fileInventoryMetadata(inventory)
		if inventory.Truncated {
			manifest.Warnings = append(manifest.Warnings, "full repository file inventory was summarized and truncated")
		}
		manifest.Blockers = append(manifest.Blockers, sourceBlockerMessages(inventory.Blockers)...)
	} else {
		manifest.Warnings = append(manifest.Warnings, "file inventory unavailable: "+err.Error())
	}
	for _, repo := range repos {
		status, err := s.sources.GetRepositoryGitStatus(ctx, repo)
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "git status unavailable for "+repo.RepoID+": "+err.Error())
		}
		manifest.Evidence.GitStatus = append(manifest.Evidence.GitStatus, status)
		commit, err := s.sources.GetRecentCommit(ctx, repo)
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, "recent commit unavailable for "+repo.RepoID+": "+err.Error())
			continue
		}
		manifest.Evidence.RecentCommits = append(manifest.Evidence.RecentCommits, commit)
	}
}

func (s *LocalAuditService) ensureSnapshot(ctx context.Context, project *store.Project, input LocalAuditInput, manifest *LocalAuditManifest, includeFiles bool) string {
	if input.SourceSnapshotID != "" {
		snapshot, err := s.store.GetSourceSnapshotByID(input.SourceSnapshotID)
		if err != nil || snapshot.ProjectRowID != project.ID {
			manifest.Blockers = append(manifest.Blockers, "source snapshot not found for project: "+input.SourceSnapshotID)
			return ""
		}
		manifest.SourceSnapshotID = snapshot.SourceSnapshotID
		manifest.Evidence.SourceSnapshot = sourceSnapshotMetadata(*snapshot)
		return snapshot.SourceSnapshotID
	}
	snapshot, err := s.sources.CreateSourceSnapshot(ctx, sources.SourceSnapshotInput{
		ProjectID:           project.ProjectID,
		RepoIDs:             input.RepoIDs,
		IncludeFileMetadata: includeFiles,
		MaxFilesPerRepo:     input.MaxFiles,
	})
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "source snapshot unavailable: "+err.Error())
		return ""
	}
	manifest.SourceSnapshotID = snapshot.SourceSnapshotID
	manifest.Evidence.SourceSnapshot = map[string]interface{}{
		"source_snapshot_id": snapshot.SourceSnapshotID,
		"snapshot_kind":      snapshot.SnapshotKind,
		"status":             snapshot.Status,
		"repository_count":   len(snapshot.Repositories),
		"blocker_count":      len(snapshot.Blockers),
	}
	manifest.Blockers = append(manifest.Blockers, sourceBlockerMessages(snapshot.Blockers)...)
	return snapshot.SourceSnapshotID
}

func (s *LocalAuditService) selectAuditRepositories(project *store.Project, input LocalAuditInput, manifest *LocalAuditManifest) []store.ProjectRepository {
	all, err := s.store.ListProjectRepositories(project.ID)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, "project repositories unavailable: "+err.Error())
		return nil
	}
	allowed := map[string]struct{}{}
	for _, repoID := range input.RepoIDs {
		allowed[repoID] = struct{}{}
	}
	repos := make([]store.ProjectRepository, 0, len(all))
	for _, repo := range all {
		if repo.Enabled == 0 {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[repo.RepoID]; !ok {
				continue
			}
		}
		repos = append(repos, repo)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].RepoID < repos[j].RepoID })
	if len(repos) == 0 {
		manifest.Blockers = append(manifest.Blockers, "no enabled project repositories matched audit input")
	}
	return repos
}

func normalizeLocalAuditInput(input LocalAuditInput) (LocalAuditInput, error) {
	input.Mode = strings.TrimSpace(input.Mode)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.PlanID = strings.TrimSpace(input.PlanID)
	input.PassID = strings.TrimSpace(input.PassID)
	input.SourceSnapshotID = strings.TrimSpace(input.SourceSnapshotID)
	input.ContextPacketID = strings.TrimSpace(input.ContextPacketID)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.DiffMode = strings.TrimSpace(input.DiffMode)
	input.RepoIDs = cleanStrings(input.RepoIDs, 100)
	input.Paths = cleanStrings(input.Paths, hardLocalAuditPaths)
	input.SearchTerms = cleanStrings(input.SearchTerms, hardLocalAuditSearchTerms)
	if input.ProjectID == "" {
		return input, fmt.Errorf("project_id is required")
	}
	switch LocalAuditMode(input.Mode) {
	case LocalAuditModeRecentCommit:
		if len(input.RepoIDs) != 1 {
			return input, fmt.Errorf("recent_commit requires exactly one repo_id")
		}
		input.DiffMode = sources.DiffModeRecentCommit
	case LocalAuditModeSelectedPassChanges:
		if input.PlanID == "" || input.PassID == "" {
			return input, fmt.Errorf("selected_pass_changes requires plan_id and pass_id")
		}
		if input.DiffMode == "" {
			input.DiffMode = sources.DiffModeWorktree
		}
		if input.DiffMode != sources.DiffModeWorktree && input.DiffMode != sources.DiffModeStaged {
			return input, fmt.Errorf("selected_pass_changes diff_mode must be worktree or staged")
		}
	case LocalAuditModeFeatureSlice:
		if len(input.Paths) == 0 && len(input.SearchTerms) == 0 {
			return input, fmt.Errorf("feature_slice requires paths or search_terms")
		}
	case LocalAuditModeFullRepository:
	default:
		return input, fmt.Errorf("invalid local audit mode")
	}
	for _, p := range input.Paths {
		if err := validateLocalAuditRelPath(p); err != nil {
			return input, err
		}
	}
	input.MaxFiles = bounded(input.MaxFiles, defaultLocalAuditMaxFiles, hardLocalAuditMaxFiles)
	input.MaxBytes = bounded(input.MaxBytes, defaultLocalAuditMaxBytes, hardLocalAuditMaxBytes)
	input.ContextLines = bounded(input.ContextLines, defaultLocalAuditContextLines, hardLocalAuditContextLines)
	return input, nil
}

func localAuditRecordResult(row store.LocalAudit) *LocalAuditRecordResult {
	return &LocalAuditRecordResult{
		AuditID:          row.AuditID,
		Mode:             row.Mode,
		Status:           row.Status,
		ProjectID:        row.ProjectID,
		Title:            row.Title,
		PlanID:           row.PlanID,
		PassID:           row.PassID,
		SourceSnapshotID: row.SourceSnapshotID,
		ContextPacketID:  row.ContextPacketID,
		ManifestPath:     row.ManifestPath,
		PacketPath:       row.PacketPath,
		InputSummaryPath: row.InputSummaryPath,
		Blockers:         decodeStringArray(row.BlockersJson),
		Warnings:         decodeStringArray(row.WarningsJson),
		CreatedAt:        row.CreatedAt,
		CompletedAt:      row.CompletedAt,
	}
}

func renderLocalAuditInputSummary(input LocalAuditInput, manifest LocalAuditManifest) string {
	var b strings.Builder
	b.WriteString("# Local Audit Input Summary\n\n")
	b.WriteString("- Audit ID: " + manifest.AuditID + "\n")
	b.WriteString("- Mode: " + input.Mode + "\n")
	b.WriteString("- Project: " + input.ProjectID + "\n")
	b.WriteString("- Status: " + manifest.Status + "\n")
	b.WriteString(fmt.Sprintf("- Bounds: max_files=%d max_bytes=%d context_lines=%d\n", input.MaxFiles, input.MaxBytes, input.ContextLines))
	if input.PlanID != "" || input.PassID != "" {
		b.WriteString("- Plan/pass: " + input.PlanID + " / " + input.PassID + "\n")
	}
	if len(input.RepoIDs) > 0 {
		b.WriteString("- Repositories: " + strings.Join(input.RepoIDs, ", ") + "\n")
	}
	if len(input.Paths) > 0 {
		b.WriteString("- Paths: " + strings.Join(input.Paths, ", ") + "\n")
	}
	if len(input.SearchTerms) > 0 {
		b.WriteString("- Search terms: " + strings.Join(input.SearchTerms, ", ") + "\n")
	}
	return b.String()
}

func renderLocalAuditPacket(input LocalAuditInput, manifest LocalAuditManifest, diffs []sources.BoundedDiff) string {
	var b strings.Builder
	b.WriteString("# Local Audit Packet\n\n")
	b.WriteString("> Local-only audit evidence. GitHub PRs, CI, Actions, and issues were not used.\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString("- Audit ID: " + manifest.AuditID + "\n")
	b.WriteString("- Mode: " + manifest.Mode + "\n")
	b.WriteString("- Project: " + manifest.ProjectID + "\n")
	b.WriteString("- Status: " + manifest.Status + "\n")
	b.WriteString(fmt.Sprintf("- Changed files: %d\n", len(manifest.Evidence.ChangedFiles)))
	b.WriteString(fmt.Sprintf("- Search matches: %d\n", len(manifest.Evidence.SearchMatches)))
	b.WriteString("\n## Blockers\n\n")
	writeStringList(&b, manifest.Blockers)
	b.WriteString("\n## Warnings\n\n")
	writeStringList(&b, manifest.Warnings)
	b.WriteString("\n## Evidence\n\n")
	for _, commit := range manifest.Evidence.RecentCommits {
		b.WriteString(fmt.Sprintf("- Recent commit `%s` in `%s`: %s\n", shortSHA(commit.CommitSHA), commit.RepoID, commit.Subject))
	}
	for _, status := range manifest.Evidence.GitStatus {
		b.WriteString(fmt.Sprintf("- Git status `%s`: dirty=%t changed=%d staged=%d unstaged=%d untracked=%d\n", status.RepoID, status.Dirty, status.ChangedFileCount, status.StagedCount, status.UnstagedCount, status.UntrackedCount))
	}
	if len(manifest.Evidence.ChangedFiles) > 0 {
		b.WriteString("\n## Changed Files\n\n")
		for _, file := range manifest.Evidence.ChangedFiles {
			b.WriteString(fmt.Sprintf("- `%s` %s `%s`\n", file.RepoID, file.Status, file.Path))
		}
	}
	if len(diffs) > 0 {
		b.WriteString("\n## Bounded Diff Previews\n\n")
		for _, diff := range diffs {
			if diff.Content == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("### %s %s\n\n", diff.RepoID, diff.Mode))
			b.WriteString("```diff\n")
			b.WriteString(limitString(diff.Content, input.MaxBytes))
			if !strings.HasSuffix(diff.Content, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
	}
	if len(manifest.Evidence.SearchMatches) > 0 {
		b.WriteString("\n## Search Matches\n\n")
		for _, match := range manifest.Evidence.SearchMatches {
			b.WriteString(fmt.Sprintf("- `%s:%d` `%s` hash `%s`\n", match.Path, match.LineStart, match.RepoID, match.SnippetHash))
		}
	}
	return b.String()
}

func localAuditStatus(blockers, warnings []string) string {
	if len(blockers) > 0 {
		return string(LocalAuditStatusBlocked)
	}
	if len(warnings) > 0 {
		return string(LocalAuditStatusPartial)
	}
	return string(LocalAuditStatusCreated)
}

func notUsedRemoteEvidence() map[string]string {
	return map[string]string{
		"github_pr":      "not_used",
		"github_ci":      "not_used",
		"github_actions": "not_used",
		"github_issues":  "not_used",
	}
}

func diffMetadata(diff sources.BoundedDiff) LocalAuditDiffMetadata {
	return LocalAuditDiffMetadata{
		RepoID:          diff.RepoID,
		Mode:            diff.Mode,
		ContentHash:     diff.ContentHash,
		Truncated:       diff.Truncated,
		MaxBytes:        diff.MaxBytes,
		RedactionStatus: diff.RedactionStatus,
		PreviewBytes:    len([]byte(diff.Content)),
	}
}

func sourceSnapshotMetadata(snapshot store.SourceSnapshot) map[string]interface{} {
	return map[string]interface{}{
		"source_snapshot_id": snapshot.SourceSnapshotID,
		"snapshot_kind":      snapshot.SnapshotKind,
		"status":             snapshot.Status,
		"created_at":         snapshot.CreatedAt,
		"completed_at":       snapshot.CompletedAt,
	}
}

func fileInventoryMetadata(inventory *sources.FileInventoryResult) map[string]interface{} {
	counts := map[string]int{}
	for _, file := range inventory.Files {
		counts[file.RepoID]++
	}
	return map[string]interface{}{
		"source_snapshot_id": inventory.SourceSnapshotID,
		"file_count":         len(inventory.Files),
		"repo_file_counts":   counts,
		"truncated":          inventory.Truncated,
		"blocker_count":      len(inventory.Blockers),
	}
}

func fileReadMetadata(read *sources.BoundedFileReadResult) LocalAuditFileReadMetadata {
	return LocalAuditFileReadMetadata{
		RepoID:          read.RepoID,
		Path:            read.Path,
		LineStart:       read.LineStart,
		LineEnd:         read.LineEnd,
		ContentHash:     read.ContentHash,
		SnippetHash:     read.SnippetHash,
		RedactionStatus: read.RedactionStatus,
		Truncated:       read.Truncated,
	}
}

func boundedPassMetadata(pass store.PlanPass) map[string]interface{} {
	return map[string]interface{}{
		"pass_id":                      pass.PassID,
		"name":                         pass.Name,
		"status":                       pass.Status,
		"risk_level":                   pass.RiskLevel,
		"context_plan_json":            limitString(pass.ContextPlanJson, 8192),
		"source_snapshot_requirements": limitString(pass.SourceSnapshotRequirementsJson, 8192),
		"handoff_readiness_criteria":   limitString(pass.HandoffReadinessCriteriaJson, 8192),
	}
}

func sourceBlockerMessages(blockers []sources.SourceBlocker) []string {
	out := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		prefix := blocker.Code
		if blocker.RepoID != "" {
			prefix = blocker.RepoID + ":" + prefix
		}
		out = append(out, prefix+": "+blocker.Message)
	}
	return out
}

func limitChangedFiles(files []sources.ChangedFile, max int) []sources.ChangedFile {
	if len(files) <= max {
		return files
	}
	return files[:max]
}

func cleanStrings(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		out = append(out, value)
		seen[value] = struct{}{}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func validateLocalAuditRelPath(value string) error {
	if strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return fmt.Errorf("invalid path %q", value)
	}
	cleaned := path.Clean(value)
	if path.IsAbs(value) || filepath.IsAbs(value) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path must be repository-relative: %s", value)
	}
	return nil
}

func bounded(value, defaultValue, hardCap int) int {
	if value <= 0 {
		value = defaultValue
	}
	if value > hardCap {
		return hardCap
	}
	return value
}

func decodeStringArray(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func localAuditSlug(title, mode string) string {
	source := title
	if source == "" {
		source = strings.ReplaceAll(mode, "_", "-")
	}
	source = strings.ToLower(source)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	source = strings.Trim(re.ReplaceAllString(source, "-"), "-")
	if source == "" {
		source = "local-audit"
	}
	if len(source) > 80 {
		source = strings.Trim(source[:80], "-")
	}
	return source
}

func newLocalAuditID(now time.Time) (string, error) {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return "localaudit-" + now.UTC().Format("2006-01-02") + "-" + hex.EncodeToString(data[:]), nil
}

func mustAuditPath(date, slug, kind string) string {
	p, err := artifacts.AuditPath(date, slug, kind)
	if err != nil {
		return ""
	}
	return p
}

func writeStringList(b *strings.Builder, values []string) {
	if len(values) == 0 {
		b.WriteString("- None\n")
		return
	}
	for _, value := range values {
		b.WriteString("- " + value + "\n")
	}
}

func shortSHA(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func limitString(value string, maxBytes int) string {
	if maxBytes <= 0 || len([]byte(value)) <= maxBytes {
		return value
	}
	data := []byte(value)
	return string(data[:maxBytes]) + "\n[TRUNCATED]\n"
}
