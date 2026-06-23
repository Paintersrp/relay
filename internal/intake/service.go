package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/store/generated"
)

// Service provides shared intake logic for creating Relay runs from planner handoff markdown.
// The current MCP run-submission path uses this service for durable association and provenance handling.
type Service struct {
	store *store.Store
}

// NewService constructs an intake Service backed by the given store.
func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

// CreateRunInput holds the caller-supplied arguments for creating a run from a planner handoff.
type CreateRunInput struct {
	// Markdown is the full planner handoff markdown text. Required.
	Markdown string
	// RepoTarget is an explicit repo name or path. Falls back to frontmatter if empty.
	RepoTarget string
	// BranchContext is an explicit branch name. Falls back to frontmatter, then "main".
	BranchContext string
	// Name is an explicit run title. Falls back to frontmatter title, then markdown H1.
	Name string
	// Source tags the creation origin (e.g., "mcp_chat", "api"). Default "api".
	Source string
	// ClientTraceID is an optional opaque trace identifier from the calling client.
	ClientTraceID string
	// PlanID optionally associates the run to an existing Relay plan.
	PlanID string
	// PassID optionally associates the run to an existing Relay plan pass.
	PassID string
}

type ProvenanceSummary struct {
	PlannerHandoffSHA256 string `json:"planner_handoff_sha256"`
	SourceArtifactPath   string `json:"source_artifact_path,omitempty"`
	PlanID               string `json:"plan_id,omitempty"`
	PassID               string `json:"pass_id,omitempty"`
	ContextPacketID      string `json:"context_packet_id,omitempty"`
	SourceSnapshotID     string `json:"source_snapshot_id,omitempty"`
	ArtifactKind         string `json:"artifact_kind"`
}

// ValidationSummary carries intake validation results.
type ValidationSummary struct {
	Warnings []string `json:"warnings"`
	Blockers []string `json:"blockers"`
	Passed   bool     `json:"passed"`
}

// CreateRunOutput holds the result of a successful run creation.
type CreateRunOutput struct {
	RunID             int64             `json:"run_id"`
	Status            string            `json:"status"`
	LifecycleState    string            `json:"lifecycle_state"`
	ReviewURL         string            `json:"review_url"`
	ArtifactKinds     []string          `json:"artifact_kinds"`
	ValidationSummary ValidationSummary `json:"validation_summary"`
	PlanID            string            `json:"plan_id,omitempty"`
	PassID            string            `json:"pass_id,omitempty"`
	Provenance        ProvenanceSummary `json:"provenance"`
}

// CreateRunFromHandoff creates a new Relay run from planner handoff markdown.
//
// Safety: this function does not read arbitrary files. Markdown must be passed as
// an explicit argument; the caller is responsible for not including secrets, tokens,
// auth headers, private keys, or signed URLs in the markdown content.
func (svc *Service) CreateRunFromHandoff(input CreateRunInput) (*CreateRunOutput, error) {
	if strings.TrimSpace(input.Markdown) == "" {
		return nil, fmt.Errorf("planner_handoff_markdown is required and must not be empty")
	}

	metadata := ExtractHandoffMetadata(input.Markdown)
	warnings, blockers := ValidateHandoffText(input.Markdown)

	if len(blockers) > 0 {
		return nil, fmt.Errorf("handoff validation blocked: %s", strings.Join(blockers, "; "))
	}

	// Resolve repo target: explicit arg > frontmatter repo > frontmatter repo_target.
	repoTarget := input.RepoTarget
	if repoTarget == "" {
		repoTarget = metadata["repo"]
	}
	if repoTarget == "" {
		repoTarget = metadata["repo_target"]
	}
	if repoTarget == "" {
		return nil, fmt.Errorf("no repository target found in arguments or frontmatter (set repo_target or include repo: in frontmatter)")
	}

	repo, err := resolveRepo(svc.store, repoTarget)
	if err != nil {
		return nil, fmt.Errorf("resolve repository %q: %w", repoTarget, err)
	}

	// Resolve branch: explicit arg > frontmatter branch > frontmatter branch_context > "main".
	branchContext := input.BranchContext
	if branchContext == "" {
		branchContext = metadata["branch"]
	}
	if branchContext == "" {
		branchContext = metadata["branch_context"]
	}
	if branchContext == "" {
		branchContext = "main"
	}

	// Resolve title: explicit name > frontmatter title > markdown H1 > "Untitled Run".
	title := input.Name
	if title == "" {
		title = metadata["title"]
	}
	if title == "" {
		title = deriveTitle(input.Markdown)
	}

	recommendedModel := metadata["recommended_model"]
	if recommendedModel == "" {
		recommendedModel = "deepseek-v4-flash"
	}
	selectedModel := recommendedModel
	association, err := ResolveRunPlanAssociation(context.Background(), svc.store, input.PlanID, input.PassID)
	if err != nil {
		return nil, err
	}
	if err := validateAssociationAgainstHandoffMetadata(association, metadata, repoTarget, branchContext); err != nil {
		return nil, err
	}

	status := "intake_received"
	if len(warnings) > 0 {
		status = "intake_needs_review"
	}

	source := input.Source
	if source == "" {
		source = "api"
	}

	tx, err := svc.store.DB().BeginTx(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("begin intake transaction: %w", err)
	}

	txQueries := generated.New(tx)
	committed := false
	writtenPaths := make([]string, 0, 5)
	defer func() {
		if !committed {
			_ = tx.Rollback()
			for _, path := range writtenPaths {
				_ = os.Remove(path)
			}
		}
	}()

	run, err := txQueries.CreateRun(context.Background(), generated.CreateRunParams{
		RepoID:           repo.ID,
		Title:            title,
		Status:           status,
		RecommendedModel: recommendedModel,
		SelectedModel:    selectedModel,
		ExecutorAdapter:  store.DefaultExecutorAdapter,
		BranchName:       branchContext,
		BaseCommit:       "",
		HeadCommit:       "",
		PlanRowID:        association.PlanRowID,
		PlanPassRowID:    association.PlanPassRowID,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	if len(warnings) > 0 {
		for _, w := range warnings {
			if _, err := txQueries.CreateCheck(context.Background(), generated.CreateCheckParams{
				RunID:       run.ID,
				Kind:        "validation",
				Status:      "warning",
				Summary:     w,
				DetailsJson: "{}",
			}); err != nil {
				return nil, fmt.Errorf("create validation warning check: %w", err)
			}
		}
	} else {
		if _, err := txQueries.CreateCheck(context.Background(), generated.CreateCheckParams{
			RunID:       run.ID,
			Kind:        "validation",
			Status:      "pass",
			Summary:     "Intake validation successful",
			DetailsJson: "{}",
		}); err != nil {
			return nil, fmt.Errorf("create validation pass check: %w", err)
		}
	}

	artifactKinds := []string{}

	if err := writeArtifactWithRow(txQueries, run.ID, "planner_handoff", "planner_handoff.md", "text/markdown", []byte(input.Markdown), &writtenPaths); err != nil {
		return nil, fmt.Errorf("write planner handoff artifact: %w", err)
	}
	artifactKinds = append(artifactKinds, "planner_handoff")

	fmJSON, _ := json.MarshalIndent(metadata, "", "  ")
	if err := writeArtifactWithRow(txQueries, run.ID, "parsed_frontmatter", "parsed_frontmatter.json", "application/json", fmJSON, &writtenPaths); err != nil {
		return nil, fmt.Errorf("write parsed frontmatter artifact: %w", err)
	}
	artifactKinds = append(artifactKinds, "parsed_frontmatter")

	configMap := map[string]string{
		"repo_target":    repo.Path,
		"branch_context": branchContext,
		"source":         source,
		"created_from":   "intake_service",
	}
	if association.PlanID != "" {
		configMap["plan_id"] = association.PlanID
	}
	if association.PassID != "" {
		configMap["pass_id"] = association.PassID
	}
	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	if err := writeArtifactWithRow(txQueries, run.ID, "run_config", "run_config.json", "application/json", configJSON, &writtenPaths); err != nil {
		return nil, fmt.Errorf("write run config artifact: %w", err)
	}
	artifactKinds = append(artifactKinds, "run_config")

	report := map[string]interface{}{
		"status":   run.Status,
		"warnings": warnings,
		"blockers": blockers,
	}
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := writeArtifactWithRow(txQueries, run.ID, "intake_validation_report", "intake_validation_report.json", "application/json", reportJSON, &writtenPaths); err != nil {
		return nil, fmt.Errorf("write intake validation report artifact: %w", err)
	}
	artifactKinds = append(artifactKinds, "intake_validation_report")

	provenanceNotes := make([]string, 0, 1)
	if association.PassID != "" && strings.TrimSpace(metadata["managed_plan_pass"]) == "" {
		provenanceNotes = append(provenanceNotes, "associated run was submitted with pass_id but the handoff metadata did not declare managed_plan_pass")
	}

	handoffHash := sha256.Sum256([]byte(input.Markdown))
	handoffSHA := hex.EncodeToString(handoffHash[:])
	sourceArtifactPath := firstNonEmpty(metadata["source_artifact_path"], metadata["intended_handoff_path"])
	handoffMetadataJSON, err := marshalProvenanceMetadata(metadata, provenanceNotes)
	if err != nil {
		return nil, fmt.Errorf("marshal handoff metadata: %w", err)
	}
	submissionArgsJSON, err := marshalSubmissionArgs(source, input, association)
	if err != nil {
		return nil, fmt.Errorf("marshal submission args: %w", err)
	}

	if _, err := txQueries.CreateRunSubmissionProvenance(context.Background(), generated.CreateRunSubmissionProvenanceParams{
		RunID:                run.ID,
		PlannerHandoffSha256: handoffSHA,
		PlannerHandoffBytes:  int64(len([]byte(input.Markdown))),
		Source:               source,
		ClientTraceID:        strings.TrimSpace(input.ClientTraceID),
		SourceArtifactPath:   sourceArtifactPath,
		RepoTarget:           repoTarget,
		BranchContext:        branchContext,
		PlanID:               association.PlanID,
		PassID:               association.PassID,
		PlanRowID:            association.PlanRowID,
		PlanPassRowID:        association.PlanPassRowID,
		ManagedPlanPass:      metadata["managed_plan_pass"],
		ManagedPlanPassName:  metadata["managed_plan_pass_name"],
		ContextPacketID:      metadata["context_packet_id"],
		SourceSnapshotID:     metadata["source_snapshot_id"],
		HandoffMetadataJson:  handoffMetadataJSON,
		SubmissionArgsJson:   submissionArgsJSON,
	}); err != nil {
		return nil, fmt.Errorf("create run submission provenance: %w", err)
	}

	provenanceArtifact := map[string]interface{}{
		"schema_version":         "1.0.0",
		"run_id":                 run.ID,
		"planner_handoff_sha256": handoffSHA,
		"planner_handoff_bytes":  len([]byte(input.Markdown)),
		"source":                 source,
		"client_trace_id":        strings.TrimSpace(input.ClientTraceID),
		"source_artifact_path":   sourceArtifactPath,
		"repo_target":            repoTarget,
		"branch_context":         branchContext,
		"plan_id":                association.PlanID,
		"pass_id":                association.PassID,
		"managed_plan_pass":      metadata["managed_plan_pass"],
		"managed_plan_pass_name": metadata["managed_plan_pass_name"],
		"context_packet_id":      metadata["context_packet_id"],
		"source_snapshot_id":     metadata["source_snapshot_id"],
		"handoff_metadata":       metadata,
		"submission_args":        map[string]interface{}{"has_plan_id": association.PlanID != "", "has_pass_id": association.PassID != "", "has_client_trace_id": strings.TrimSpace(input.ClientTraceID) != "", "source": source},
	}
	if len(provenanceNotes) > 0 {
		provenanceArtifact["provenance_notes"] = provenanceNotes
	}
	if association.PlanRowID.Valid {
		provenanceArtifact["plan_row_id"] = association.PlanRowID.Int64
	}
	if association.PlanPassRowID.Valid {
		provenanceArtifact["plan_pass_row_id"] = association.PlanPassRowID.Int64
	}
	provenanceJSON, err := json.MarshalIndent(provenanceArtifact, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal provenance artifact: %w", err)
	}
	if err := writeArtifactWithRow(txQueries, run.ID, "planner_handoff_provenance_json", "planner_handoff_provenance.json", "application/json", provenanceJSON, &writtenPaths); err != nil {
		return nil, fmt.Errorf("write provenance artifact: %w", err)
	}
	artifactKinds = append(artifactKinds, "planner_handoff_provenance_json")

	if association.Pass != nil && association.Pass.Status == "planned" {
		if _, err := txQueries.UpdatePlanPassStatus(context.Background(), generated.UpdatePlanPassStatusParams{
			ID:     association.Pass.ID,
			Status: "in_progress",
		}); err != nil {
			return nil, fmt.Errorf("mark associated plan pass in progress: %w", err)
		}
	}

	if _, err := txQueries.CreateEvent(context.Background(), generated.CreateEventParams{
		RunID:        run.ID,
		Level:        "info",
		Message:      fmt.Sprintf("Handoff intake receipt via %s: planner handoff registered", source),
		MetadataJson: "{}",
	}); err != nil {
		return nil, fmt.Errorf("create intake event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit intake transaction: %w", err)
	}
	committed = true

	return &CreateRunOutput{
		RunID:          run.ID,
		Status:         run.Status,
		LifecycleState: "intake",
		ReviewURL:      fmt.Sprintf("/runs/%d/intake", run.ID),
		ArtifactKinds:  artifactKinds,
		ValidationSummary: ValidationSummary{
			Warnings: warnings,
			Blockers: blockers,
			Passed:   len(warnings) == 0 && len(blockers) == 0,
		},
		PlanID: association.PlanID,
		PassID: association.PassID,
		Provenance: ProvenanceSummary{
			PlannerHandoffSHA256: handoffSHA,
			SourceArtifactPath:   sourceArtifactPath,
			PlanID:               association.PlanID,
			PassID:               association.PassID,
			ContextPacketID:      metadata["context_packet_id"],
			SourceSnapshotID:     metadata["source_snapshot_id"],
			ArtifactKind:         "planner_handoff_provenance_json",
		},
	}, nil
}

// resolveRepo finds or creates a repo by name or path in the store.
// Mirrors the same logic used in internal/api's resolveRepo helper.
func resolveRepo(s *store.Store, repoNameOrPath string) (*store.Repo, error) {
	if repoNameOrPath == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if repo, err := s.GetRepoByName(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	if repo, err := s.GetRepoByPath(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	baseName := filepath.Base(repoNameOrPath)
	if repo, err := s.GetRepoByName(baseName); err == nil && repo != nil {
		return repo, nil
	}
	if repos, err := s.ListRepos(); err == nil {
		for _, r := range repos {
			if strings.EqualFold(r.Name, repoNameOrPath) || strings.EqualFold(r.Name, baseName) {
				rCopy := r
				return &rCopy, nil
			}
		}
	}
	return s.CreateRepo(baseName, repoNameOrPath)
}

// deriveTitle extracts the first H1 heading from markdown, or returns "Untitled Run".
func deriveTitle(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return "Untitled Run"
}

func validateAssociationAgainstHandoffMetadata(association RunPlanAssociation, metadata map[string]string, repoTarget string, branchContext string) error {
	if association.PassID != "" {
		managedPlanPass := strings.TrimSpace(metadata["managed_plan_pass"])
		if managedPlanPass != "" && !strings.EqualFold(managedPlanPass, association.PassID) {
			return &InputError{
				Code:    ErrCodeValidation,
				Message: fmt.Sprintf("handoff managed_plan_pass %q does not match submitted pass_id %q", managedPlanPass, association.PassID),
			}
		}
	}
	if association.Plan != nil {
		if err := validateFieldConflict("repo_target", repoTarget, association.Plan.RepoTarget); err != nil {
			return err
		}
		if err := validateFieldConflict("branch_context", branchContext, association.Plan.BranchContext); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldConflict(field string, handoffValue string, planValue string) error {
	handoffValue = strings.TrimSpace(handoffValue)
	planValue = strings.TrimSpace(planValue)
	if handoffValue != "" && planValue != "" && handoffValue != planValue {
		return &InputError{
			Code:    ErrCodeValidation,
			Message: fmt.Sprintf("handoff %s %q conflicts with associated plan value %q", field, handoffValue, planValue),
		}
	}
	return nil
}

func writeArtifactWithRow(queries *generated.Queries, runID int64, kind string, filename string, mimeType string, data []byte, writtenPaths *[]string) error {
	path, err := artifacts.Write(runID, kind, filename, data)
	if err != nil {
		return err
	}
	*writtenPaths = append(*writtenPaths, path)
	if _, err := queries.CreateArtifact(context.Background(), generated.CreateArtifactParams{
		RunID:    runID,
		Kind:     kind,
		Path:     path,
		MimeType: mimeType,
	}); err != nil {
		return err
	}
	return nil
}

func marshalProvenanceMetadata(metadata map[string]string, notes []string) (string, error) {
	payload := map[string]interface{}{}
	for key, value := range metadata {
		payload[key] = value
	}
	if len(notes) > 0 {
		payload["provenance_notes"] = notes
	}
	return marshalJSON(payload)
}

func marshalSubmissionArgs(source string, input CreateRunInput, association RunPlanAssociation) (string, error) {
	payload := map[string]interface{}{
		"has_plan_id":         association.PlanID != "",
		"has_pass_id":         association.PassID != "",
		"has_client_trace_id": strings.TrimSpace(input.ClientTraceID) != "",
		"source":              source,
	}
	return marshalJSON(payload)
}

func marshalJSON(value interface{}) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
