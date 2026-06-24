package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"relay/internal/store/generated"
)

// Type aliases for the generated refactor backlog row types.
type RefactorDiscoveryTask = generated.RefactorDiscoveryTask
type RefactorCandidate = generated.RefactorCandidate
type RefactorCandidateDiscoveryLink = generated.RefactorCandidateDiscoveryLink
type RefactorCandidateDependency = generated.RefactorCandidateDependency
type RefactorCandidateScheduleRef = generated.RefactorCandidateScheduleRef
type RefactorCandidateStatusEvent = generated.RefactorCandidateStatusEvent

const defaultRefactorBacklogListLimit = 50

// ---------------------------------------------------------------------------
// Validation helpers
//
// Validation runs at the store boundary so non-pass-ready candidates and
// malformed records are rejected before any row is inserted or updated.
// ---------------------------------------------------------------------------

func validateRequiredString(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required and must not be blank", field)
	}
	return nil
}

func validateJSONArray(field, value string, minItems int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be a JSON array with at least %d item(s)", field, minItems)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(value), &arr); err != nil {
		return fmt.Errorf("%s must be a valid JSON array: %w", field, err)
	}
	if len(arr) < minItems {
		return fmt.Errorf("%s must contain at least %d item(s)", field, minItems)
	}
	return nil
}

func validateJSONObject(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must be a JSON object", field)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		return fmt.Errorf("%s must be a valid JSON object: %w", field, err)
	}
	if obj == nil {
		return fmt.Errorf("%s must be a JSON object", field)
	}
	return nil
}

func validateRefactorDiscoveryTaskStatus(status string) error {
	switch status {
	case "open", "completed", "closed", "superseded":
		return nil
	default:
		return fmt.Errorf("invalid discovery task status %q (allowed: open, completed, closed, superseded)", status)
	}
}

func validateRefactorCandidateStatus(status string) error {
	switch status {
	case "ready", "scheduled", "scheduled_revision_required", "completed", "completed_with_warnings", "deferred", "rejected", "superseded":
		return nil
	default:
		return fmt.Errorf("invalid refactor candidate status %q", status)
	}
}

func validateRefactorCandidateInitialStatus(status string) error {
	if status != "ready" {
		return fmt.Errorf("refactor candidate create requires status \"ready\", got %q", status)
	}
	return nil
}

func validateRiskLevel(risk string) error {
	switch risk {
	case "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("invalid risk_level %q (allowed: low, medium, high)", risk)
	}
}

func validateProjectScope(projectRowID int64, projectID string) error {
	if projectRowID <= 0 {
		return errors.New("project_row_id must be a positive project row reference")
	}
	if strings.TrimSpace(projectID) == "" {
		return errors.New("project_id is required")
	}
	return nil
}

// validateProjectOwnership enforces that the supplied project_row_id and
// project_id refer to the same projects row. This closes the gap where a caller
// could persist a refactor backlog record under a row ID that does not match the
// human-facing project ID, leaving ambiguous project ownership for later
// consumers (MCP/UI/orchestrator).
func (s *Store) validateProjectOwnership(projectRowID int64, projectID string) error {
	if err := validateProjectScope(projectRowID, projectID); err != nil {
		return err
	}
	project, err := s.GetProject(projectRowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("project row %d not found", projectRowID)
		}
		return err
	}
	if project.ProjectID != projectID {
		return fmt.Errorf("project_id %q does not match project row %d (expected %q)", projectID, projectRowID, project.ProjectID)
	}
	return nil
}

// allowedRefactorTargetScopeKinds is the contract-defined set of discovery task
// target scope kinds.
var allowedRefactorTargetScopeKinds = map[string]bool{
	"repository": true,
	"subsystem":  true,
	"directory":  true,
	"file_set":   true,
	"plan":       true,
	"pass":       true,
}

// validateTargetScopeJSON enforces the structured discovery task target scope
// contract: a JSON object with a valid "kind" and a non-empty "values" array of
// non-empty strings.
func validateTargetScopeJSON(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("target_scope_json is required and must be a JSON object with kind and values")
	}
	var scope struct {
		Kind   *string  `json:"kind"`
		Values []string `json:"values"`
	}
	if err := json.Unmarshal([]byte(value), &scope); err != nil {
		return fmt.Errorf("target_scope_json must be a valid {kind, values} object: %w", err)
	}
	if scope.Kind == nil || strings.TrimSpace(*scope.Kind) == "" {
		return errors.New("target_scope_json.kind is required")
	}
	if !allowedRefactorTargetScopeKinds[*scope.Kind] {
		return fmt.Errorf("invalid target_scope_json.kind %q (allowed: repository, subsystem, directory, file_set, plan, pass)", *scope.Kind)
	}
	if len(scope.Values) == 0 {
		return errors.New("target_scope_json.values must be a non-empty array")
	}
	for i, v := range scope.Values {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("target_scope_json.values[%d] must be a non-empty string", i)
		}
	}
	return nil
}

func normalizeRefactorListLimit(limit int64) int64 {
	if limit <= 0 {
		return defaultRefactorBacklogListLimit
	}
	return limit
}

func nullInt64FromPtr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// refactorCandidateRowInProject verifies the candidate row exists within the
// given project. It returns an error suitable for rejecting cross-project links.
func (s *Store) refactorCandidateRowInProject(projectRowID, candidateRowID int64) error {
	_, err := s.queries.GetRefactorCandidateByRowID(context.Background(), generated.GetRefactorCandidateByRowIDParams{
		ProjectRowID: projectRowID,
		ID:           candidateRowID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("refactor candidate row %d not found in project %d", candidateRowID, projectRowID)
	}
	return err
}

func (s *Store) refactorDiscoveryTaskRowInProject(projectRowID, taskRowID int64) error {
	_, err := s.queries.GetRefactorDiscoveryTaskByRowID(context.Background(), generated.GetRefactorDiscoveryTaskByRowIDParams{
		ProjectRowID: projectRowID,
		ID:           taskRowID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("refactor discovery task row %d not found in project %d", taskRowID, projectRowID)
	}
	return err
}

// validateSupersededByReference enforces that a non-empty supersession reference
// points to an existing candidate in the same project and is not the candidate
// itself. Empty references are allowed (no supersession recorded). This keeps
// supersession metadata consistent with the same-project validation already
// applied to dependencies and discovery links, without applying any candidate
// lifecycle/promotion behavior.
func (s *Store) validateSupersededByReference(projectRowID int64, candidateID, supersededByCandidateID string) error {
	ref := strings.TrimSpace(supersededByCandidateID)
	if ref == "" {
		return nil
	}
	if ref == candidateID {
		return errors.New("superseded_by_candidate_id must not reference the candidate itself")
	}
	_, err := s.queries.GetRefactorCandidateByCandidateID(context.Background(), generated.GetRefactorCandidateByCandidateIDParams{
		ProjectRowID: projectRowID,
		CandidateID:  ref,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("superseded_by_candidate_id %q not found in project %d", ref, projectRowID)
	}
	return err
}

// ---------------------------------------------------------------------------
// Discovery tasks
// ---------------------------------------------------------------------------

type CreateRefactorDiscoveryTaskParams struct {
	TaskID          string
	ProjectRowID    int64
	ProjectID       string
	Title           string
	Prompt          string
	TargetScopeJSON string
	Priority        string
	Status          string
	TagsJSON        string
	CreatedFrom     string
	MetadataJSON    string
	ClosedReason    string
	CompletedAt     string
	ClosedAt        string
}

func (s *Store) CreateRefactorDiscoveryTask(params CreateRefactorDiscoveryTaskParams) (*RefactorDiscoveryTask, error) {
	if err := s.validateProjectOwnership(params.ProjectRowID, params.ProjectID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("task_id", params.TaskID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("title", params.Title); err != nil {
		return nil, err
	}
	if err := validateRequiredString("prompt", params.Prompt); err != nil {
		return nil, err
	}
	if err := validateTargetScopeJSON(params.TargetScopeJSON); err != nil {
		return nil, err
	}

	priority := params.Priority
	if priority == "" {
		priority = "normal"
	}
	status := params.Status
	if status == "" {
		status = "open"
	}
	if err := validateRefactorDiscoveryTaskStatus(status); err != nil {
		return nil, err
	}
	tagsJSON := params.TagsJSON
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	if err := validateJSONArray("tags_json", tagsJSON, 0); err != nil {
		return nil, err
	}
	createdFrom := params.CreatedFrom
	if createdFrom == "" {
		createdFrom = "manual"
	}
	metadataJSON := params.MetadataJSON
	if metadataJSON == "" {
		metadataJSON = "{}"
	}
	if err := validateJSONObject("metadata_json", metadataJSON); err != nil {
		return nil, err
	}

	row, err := s.queries.CreateRefactorDiscoveryTask(context.Background(), generated.CreateRefactorDiscoveryTaskParams{
		TaskID:          params.TaskID,
		ProjectRowID:    params.ProjectRowID,
		ProjectID:       params.ProjectID,
		Title:           params.Title,
		Prompt:          params.Prompt,
		TargetScopeJson: params.TargetScopeJSON,
		Priority:        priority,
		Status:          status,
		TagsJson:        tagsJSON,
		CreatedFrom:     createdFrom,
		MetadataJson:    metadataJSON,
		ClosedReason:    params.ClosedReason,
		CompletedAt:     params.CompletedAt,
		ClosedAt:        params.ClosedAt,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetRefactorDiscoveryTaskByTaskID(projectRowID int64, taskID string) (*RefactorDiscoveryTask, error) {
	row, err := s.queries.GetRefactorDiscoveryTaskByTaskID(context.Background(), generated.GetRefactorDiscoveryTaskByTaskIDParams{
		ProjectRowID: projectRowID,
		TaskID:       taskID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRefactorDiscoveryTasksByProject(projectRowID int64, limit int64) ([]RefactorDiscoveryTask, error) {
	return s.queries.ListRefactorDiscoveryTasksByProject(context.Background(), generated.ListRefactorDiscoveryTasksByProjectParams{
		ProjectRowID: projectRowID,
		Limit:        normalizeRefactorListLimit(limit),
	})
}

func (s *Store) ListRefactorDiscoveryTasksByProjectAndStatus(projectRowID int64, status string, limit int64) ([]RefactorDiscoveryTask, error) {
	return s.queries.ListRefactorDiscoveryTasksByProjectAndStatus(context.Background(), generated.ListRefactorDiscoveryTasksByProjectAndStatusParams{
		ProjectRowID: projectRowID,
		Status:       status,
		Limit:        normalizeRefactorListLimit(limit),
	})
}

type UpdateRefactorDiscoveryTaskParams struct {
	ProjectRowID    int64
	TaskID          string
	Title           string
	Prompt          string
	TargetScopeJSON string
	Priority        string
	TagsJSON        string
	MetadataJSON    string
}

func (s *Store) UpdateRefactorDiscoveryTask(params UpdateRefactorDiscoveryTaskParams) (*RefactorDiscoveryTask, error) {
	if params.ProjectRowID <= 0 {
		return nil, errors.New("project_row_id must be a positive project row reference")
	}
	if err := validateRequiredString("task_id", params.TaskID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("title", params.Title); err != nil {
		return nil, err
	}
	if err := validateRequiredString("prompt", params.Prompt); err != nil {
		return nil, err
	}
	if err := validateTargetScopeJSON(params.TargetScopeJSON); err != nil {
		return nil, err
	}
	priority := params.Priority
	if priority == "" {
		priority = "normal"
	}
	tagsJSON := params.TagsJSON
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	if err := validateJSONArray("tags_json", tagsJSON, 0); err != nil {
		return nil, err
	}
	metadataJSON := params.MetadataJSON
	if metadataJSON == "" {
		metadataJSON = "{}"
	}
	if err := validateJSONObject("metadata_json", metadataJSON); err != nil {
		return nil, err
	}

	row, err := s.queries.UpdateRefactorDiscoveryTask(context.Background(), generated.UpdateRefactorDiscoveryTaskParams{
		Title:           params.Title,
		Prompt:          params.Prompt,
		TargetScopeJson: params.TargetScopeJSON,
		Priority:        priority,
		TagsJson:        tagsJSON,
		MetadataJson:    metadataJSON,
		ProjectRowID:    params.ProjectRowID,
		TaskID:          params.TaskID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

type UpdateRefactorDiscoveryTaskStatusParams struct {
	ProjectRowID int64
	TaskID       string
	Status       string
	ClosedReason string
	CompletedAt  string
	ClosedAt     string
}

func (s *Store) UpdateRefactorDiscoveryTaskStatus(params UpdateRefactorDiscoveryTaskStatusParams) (*RefactorDiscoveryTask, error) {
	if params.ProjectRowID <= 0 {
		return nil, errors.New("project_row_id must be a positive project row reference")
	}
	if err := validateRequiredString("task_id", params.TaskID); err != nil {
		return nil, err
	}
	if err := validateRefactorDiscoveryTaskStatus(params.Status); err != nil {
		return nil, err
	}

	row, err := s.queries.UpdateRefactorDiscoveryTaskStatus(context.Background(), generated.UpdateRefactorDiscoveryTaskStatusParams{
		Status:       params.Status,
		ClosedReason: params.ClosedReason,
		CompletedAt:  params.CompletedAt,
		ClosedAt:     params.ClosedAt,
		ProjectRowID: params.ProjectRowID,
		TaskID:       params.TaskID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ---------------------------------------------------------------------------
// Refactor candidates
// ---------------------------------------------------------------------------

type CreateRefactorCandidateParams struct {
	CandidateID            string
	ProjectRowID           int64
	ProjectID              string
	Title                  string
	ProblemSummary         string
	CurrentBehavior        string
	DesiredBehavior        string
	Rationale              string
	ProposedPassName       string
	ProposedPassGoal       string
	ProposedPassScopeJSON  string
	ProposedNonGoalsJSON   string
	TargetFilesJSON        string
	ValidationCommandsJSON string
	AuditFocusJSON         string
	ConstraintsJSON        string
	RiskLevel              string
	Status                 string
	DependencyNotes        string
	MetadataJSON           string
}

// validatePassReadyCandidateFields enforces all pass-ready field requirements
// shared by candidate create and candidate update. The returned constraints and
// metadata JSON values are normalized with defaults applied.
func validatePassReadyCandidateFields(
	title, problemSummary, desiredBehavior, rationale, proposedPassName, proposedPassGoal string,
	proposedPassScopeJSON, proposedNonGoalsJSON, targetFilesJSON, validationCommandsJSON, auditFocusJSON string,
	constraintsJSON, metadataJSON string,
) (normalizedConstraints string, normalizedMetadata string, err error) {
	for _, f := range []struct {
		field string
		value string
	}{
		{"title", title},
		{"problem_summary", problemSummary},
		{"desired_behavior", desiredBehavior},
		{"rationale", rationale},
		{"proposed_pass_name", proposedPassName},
		{"proposed_pass_goal", proposedPassGoal},
	} {
		if err := validateRequiredString(f.field, f.value); err != nil {
			return "", "", err
		}
	}

	for _, f := range []struct {
		field string
		value string
	}{
		{"proposed_pass_scope_json", proposedPassScopeJSON},
		{"proposed_non_goals_json", proposedNonGoalsJSON},
		{"target_files_json", targetFilesJSON},
		{"validation_commands_json", validationCommandsJSON},
		{"audit_focus_json", auditFocusJSON},
	} {
		if err := validateJSONArray(f.field, f.value, 1); err != nil {
			return "", "", err
		}
	}

	normalizedConstraints = constraintsJSON
	if normalizedConstraints == "" {
		normalizedConstraints = "[]"
	}
	if err := validateJSONArray("constraints_json", normalizedConstraints, 0); err != nil {
		return "", "", err
	}

	normalizedMetadata = metadataJSON
	if normalizedMetadata == "" {
		normalizedMetadata = "{}"
	}
	if err := validateJSONObject("metadata_json", normalizedMetadata); err != nil {
		return "", "", err
	}

	return normalizedConstraints, normalizedMetadata, nil
}

func (s *Store) CreateRefactorCandidate(params CreateRefactorCandidateParams) (*RefactorCandidate, error) {
	if err := s.validateProjectOwnership(params.ProjectRowID, params.ProjectID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("candidate_id", params.CandidateID); err != nil {
		return nil, err
	}

	status := params.Status
	if status == "" {
		status = "ready"
	}
	if err := validateRefactorCandidateInitialStatus(status); err != nil {
		return nil, err
	}
	if err := validateRiskLevel(params.RiskLevel); err != nil {
		return nil, err
	}

	constraintsJSON, metadataJSON, err := validatePassReadyCandidateFields(
		params.Title, params.ProblemSummary, params.DesiredBehavior, params.Rationale,
		params.ProposedPassName, params.ProposedPassGoal,
		params.ProposedPassScopeJSON, params.ProposedNonGoalsJSON, params.TargetFilesJSON,
		params.ValidationCommandsJSON, params.AuditFocusJSON,
		params.ConstraintsJSON, params.MetadataJSON,
	)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.CreateRefactorCandidate(context.Background(), generated.CreateRefactorCandidateParams{
		CandidateID:            params.CandidateID,
		ProjectRowID:           params.ProjectRowID,
		ProjectID:              params.ProjectID,
		Title:                  params.Title,
		ProblemSummary:         params.ProblemSummary,
		CurrentBehavior:        params.CurrentBehavior,
		DesiredBehavior:        params.DesiredBehavior,
		Rationale:              params.Rationale,
		ProposedPassName:       params.ProposedPassName,
		ProposedPassGoal:       params.ProposedPassGoal,
		ProposedPassScopeJson:  params.ProposedPassScopeJSON,
		ProposedNonGoalsJson:   params.ProposedNonGoalsJSON,
		TargetFilesJson:        params.TargetFilesJSON,
		ValidationCommandsJson: params.ValidationCommandsJSON,
		AuditFocusJson:         params.AuditFocusJSON,
		ConstraintsJson:        constraintsJSON,
		RiskLevel:              params.RiskLevel,
		Status:                 status,
		DependencyNotes:        params.DependencyNotes,
		MetadataJson:           metadataJSON,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetRefactorCandidateByCandidateID(projectRowID int64, candidateID string) (*RefactorCandidate, error) {
	row, err := s.queries.GetRefactorCandidateByCandidateID(context.Background(), generated.GetRefactorCandidateByCandidateIDParams{
		ProjectRowID: projectRowID,
		CandidateID:  candidateID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRefactorCandidatesByProject(projectRowID int64, limit int64) ([]RefactorCandidate, error) {
	return s.queries.ListRefactorCandidatesByProject(context.Background(), generated.ListRefactorCandidatesByProjectParams{
		ProjectRowID: projectRowID,
		Limit:        normalizeRefactorListLimit(limit),
	})
}

func (s *Store) ListRefactorCandidatesByProjectAndStatus(projectRowID int64, status string, limit int64) ([]RefactorCandidate, error) {
	return s.queries.ListRefactorCandidatesByProjectAndStatus(context.Background(), generated.ListRefactorCandidatesByProjectAndStatusParams{
		ProjectRowID: projectRowID,
		Status:       status,
		Limit:        normalizeRefactorListLimit(limit),
	})
}

func (s *Store) SearchRefactorCandidatesByProject(projectRowID int64, query string, limit int64) ([]RefactorCandidate, error) {
	pattern := "%" + strings.TrimSpace(query) + "%"
	return s.queries.SearchRefactorCandidatesByProject(context.Background(), generated.SearchRefactorCandidatesByProjectParams{
		ProjectRowID: projectRowID,
		Query:        pattern,
		Limit:        normalizeRefactorListLimit(limit),
	})
}

type UpdateRefactorCandidateParams struct {
	ProjectRowID           int64
	CandidateID            string
	Title                  string
	ProblemSummary         string
	CurrentBehavior        string
	DesiredBehavior        string
	Rationale              string
	ProposedPassName       string
	ProposedPassGoal       string
	ProposedPassScopeJSON  string
	ProposedNonGoalsJSON   string
	TargetFilesJSON        string
	ValidationCommandsJSON string
	AuditFocusJSON         string
	ConstraintsJSON        string
	DependencyNotes        string
	MetadataJSON           string
}

func (s *Store) UpdateRefactorCandidate(params UpdateRefactorCandidateParams) (*RefactorCandidate, error) {
	if params.ProjectRowID <= 0 {
		return nil, errors.New("project_row_id must be a positive project row reference")
	}
	if err := validateRequiredString("candidate_id", params.CandidateID); err != nil {
		return nil, err
	}

	constraintsJSON, metadataJSON, err := validatePassReadyCandidateFields(
		params.Title, params.ProblemSummary, params.DesiredBehavior, params.Rationale,
		params.ProposedPassName, params.ProposedPassGoal,
		params.ProposedPassScopeJSON, params.ProposedNonGoalsJSON, params.TargetFilesJSON,
		params.ValidationCommandsJSON, params.AuditFocusJSON,
		params.ConstraintsJSON, params.MetadataJSON,
	)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.UpdateRefactorCandidate(context.Background(), generated.UpdateRefactorCandidateParams{
		Title:                  params.Title,
		ProblemSummary:         params.ProblemSummary,
		CurrentBehavior:        params.CurrentBehavior,
		DesiredBehavior:        params.DesiredBehavior,
		Rationale:              params.Rationale,
		ProposedPassName:       params.ProposedPassName,
		ProposedPassGoal:       params.ProposedPassGoal,
		ProposedPassScopeJson:  params.ProposedPassScopeJSON,
		ProposedNonGoalsJson:   params.ProposedNonGoalsJSON,
		TargetFilesJson:        params.TargetFilesJSON,
		ValidationCommandsJson: params.ValidationCommandsJSON,
		AuditFocusJson:         params.AuditFocusJSON,
		ConstraintsJson:        constraintsJSON,
		DependencyNotes:        params.DependencyNotes,
		MetadataJson:           metadataJSON,
		ProjectRowID:           params.ProjectRowID,
		CandidateID:            params.CandidateID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

type UpdateRefactorCandidateStatusMetadataParams struct {
	ProjectRowID            int64
	CandidateID             string
	Status                  string
	DeferReason             string
	DeferredUntil           string
	RejectedReason          string
	SupersededByCandidateID string
	SupersededReason        string
	ScheduledAt             string
	CompletedAt             string
}

func (s *Store) UpdateRefactorCandidateStatusMetadata(params UpdateRefactorCandidateStatusMetadataParams) (*RefactorCandidate, error) {
	if params.ProjectRowID <= 0 {
		return nil, errors.New("project_row_id must be a positive project row reference")
	}
	if err := validateRequiredString("candidate_id", params.CandidateID); err != nil {
		return nil, err
	}
	if err := validateRefactorCandidateStatus(params.Status); err != nil {
		return nil, err
	}
	if err := s.validateSupersededByReference(params.ProjectRowID, params.CandidateID, params.SupersededByCandidateID); err != nil {
		return nil, err
	}

	row, err := s.queries.UpdateRefactorCandidateStatusMetadata(context.Background(), generated.UpdateRefactorCandidateStatusMetadataParams{
		Status:                  params.Status,
		DeferReason:             params.DeferReason,
		DeferredUntil:           params.DeferredUntil,
		RejectedReason:          params.RejectedReason,
		SupersededByCandidateID: params.SupersededByCandidateID,
		SupersededReason:        params.SupersededReason,
		ScheduledAt:             params.ScheduledAt,
		CompletedAt:             params.CompletedAt,
		ProjectRowID:            params.ProjectRowID,
		CandidateID:             params.CandidateID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ---------------------------------------------------------------------------
// Candidate-to-discovery links
// ---------------------------------------------------------------------------

type CreateRefactorCandidateDiscoveryLinkParams struct {
	LinkID             string
	ProjectRowID       int64
	ProjectID          string
	CandidateRowID     int64
	DiscoveryTaskRowID int64
	Note               string
}

func (s *Store) CreateRefactorCandidateDiscoveryLink(params CreateRefactorCandidateDiscoveryLinkParams) (*RefactorCandidateDiscoveryLink, error) {
	if err := s.validateProjectOwnership(params.ProjectRowID, params.ProjectID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("link_id", params.LinkID); err != nil {
		return nil, err
	}
	if err := s.refactorCandidateRowInProject(params.ProjectRowID, params.CandidateRowID); err != nil {
		return nil, err
	}
	if err := s.refactorDiscoveryTaskRowInProject(params.ProjectRowID, params.DiscoveryTaskRowID); err != nil {
		return nil, err
	}

	row, err := s.queries.CreateRefactorCandidateDiscoveryLink(context.Background(), generated.CreateRefactorCandidateDiscoveryLinkParams{
		LinkID:             params.LinkID,
		ProjectRowID:       params.ProjectRowID,
		ProjectID:          params.ProjectID,
		CandidateRowID:     params.CandidateRowID,
		DiscoveryTaskRowID: params.DiscoveryTaskRowID,
		Note:               params.Note,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRefactorCandidateDiscoveryLinks(projectRowID int64, candidateRowID int64) ([]RefactorCandidateDiscoveryLink, error) {
	return s.queries.ListRefactorCandidateDiscoveryLinks(context.Background(), generated.ListRefactorCandidateDiscoveryLinksParams{
		ProjectRowID:   projectRowID,
		CandidateRowID: candidateRowID,
	})
}

func (s *Store) ListRefactorDiscoveryTaskCandidateLinks(projectRowID int64, discoveryTaskRowID int64) ([]RefactorCandidateDiscoveryLink, error) {
	return s.queries.ListRefactorDiscoveryTaskCandidateLinks(context.Background(), generated.ListRefactorDiscoveryTaskCandidateLinksParams{
		ProjectRowID:       projectRowID,
		DiscoveryTaskRowID: discoveryTaskRowID,
	})
}

// ---------------------------------------------------------------------------
// Candidate dependencies
// ---------------------------------------------------------------------------

type CreateRefactorCandidateDependencyParams struct {
	DependencyID            string
	ProjectRowID            int64
	ProjectID               string
	CandidateRowID          int64
	DependsOnCandidateRowID int64
	DependencyType          string
	Note                    string
}

func (s *Store) CreateRefactorCandidateDependency(params CreateRefactorCandidateDependencyParams) (*RefactorCandidateDependency, error) {
	if err := s.validateProjectOwnership(params.ProjectRowID, params.ProjectID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("dependency_id", params.DependencyID); err != nil {
		return nil, err
	}
	if params.CandidateRowID == params.DependsOnCandidateRowID {
		return nil, errors.New("a refactor candidate cannot depend on itself")
	}
	if err := s.refactorCandidateRowInProject(params.ProjectRowID, params.CandidateRowID); err != nil {
		return nil, err
	}
	if err := s.refactorCandidateRowInProject(params.ProjectRowID, params.DependsOnCandidateRowID); err != nil {
		return nil, err
	}

	dependencyType := params.DependencyType
	if dependencyType == "" {
		dependencyType = "blocks"
	}

	row, err := s.queries.CreateRefactorCandidateDependency(context.Background(), generated.CreateRefactorCandidateDependencyParams{
		DependencyID:            params.DependencyID,
		ProjectRowID:            params.ProjectRowID,
		ProjectID:               params.ProjectID,
		CandidateRowID:          params.CandidateRowID,
		DependsOnCandidateRowID: params.DependsOnCandidateRowID,
		DependencyType:          dependencyType,
		Note:                    params.Note,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRefactorCandidateDependencies(projectRowID int64, candidateRowID int64) ([]RefactorCandidateDependency, error) {
	return s.queries.ListRefactorCandidateDependencies(context.Background(), generated.ListRefactorCandidateDependenciesParams{
		ProjectRowID:   projectRowID,
		CandidateRowID: candidateRowID,
	})
}

func (s *Store) DeleteRefactorCandidateDependencies(projectRowID int64, candidateRowID int64) error {
	return s.queries.DeleteRefactorCandidateDependencies(context.Background(), generated.DeleteRefactorCandidateDependenciesParams{
		ProjectRowID:   projectRowID,
		CandidateRowID: candidateRowID,
	})
}

// ---------------------------------------------------------------------------
// Candidate scheduling references
// ---------------------------------------------------------------------------

type CreateRefactorCandidateScheduleRefParams struct {
	ScheduleRefID  string
	ProjectRowID   int64
	ProjectID      string
	CandidateRowID int64
	ScheduleKind   string
	Status         string
	PlanRowID      *int64
	PlanPassRowID  *int64
	RunRowID       *int64
	PlanID         string
	PassID         string
	RunID          string
	Note           string
}

func validateScheduleKind(kind string) error {
	switch kind {
	case "existing_plan_bonus_pass", "generated_refactor_only_plan":
		return nil
	default:
		return fmt.Errorf("invalid schedule_kind %q (allowed: existing_plan_bonus_pass, generated_refactor_only_plan)", kind)
	}
}

func validateScheduleRefStatus(status string) error {
	switch status {
	case "scheduled", "stale", "completed", "cancelled":
		return nil
	default:
		return fmt.Errorf("invalid schedule ref status %q (allowed: scheduled, stale, completed, cancelled)", status)
	}
}

func (s *Store) CreateRefactorCandidateScheduleRef(params CreateRefactorCandidateScheduleRefParams) (*RefactorCandidateScheduleRef, error) {
	if err := s.validateProjectOwnership(params.ProjectRowID, params.ProjectID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("schedule_ref_id", params.ScheduleRefID); err != nil {
		return nil, err
	}
	if err := validateScheduleKind(params.ScheduleKind); err != nil {
		return nil, err
	}
	status := params.Status
	if status == "" {
		status = "scheduled"
	}
	if err := validateScheduleRefStatus(status); err != nil {
		return nil, err
	}
	if err := s.refactorCandidateRowInProject(params.ProjectRowID, params.CandidateRowID); err != nil {
		return nil, err
	}

	row, err := s.queries.CreateRefactorCandidateScheduleRef(context.Background(), generated.CreateRefactorCandidateScheduleRefParams{
		ScheduleRefID:  params.ScheduleRefID,
		ProjectRowID:   params.ProjectRowID,
		ProjectID:      params.ProjectID,
		CandidateRowID: params.CandidateRowID,
		ScheduleKind:   params.ScheduleKind,
		Status:         status,
		PlanRowID:      nullInt64FromPtr(params.PlanRowID),
		PlanPassRowID:  nullInt64FromPtr(params.PlanPassRowID),
		RunRowID:       nullInt64FromPtr(params.RunRowID),
		PlanID:         params.PlanID,
		PassID:         params.PassID,
		RunID:          params.RunID,
		Note:           params.Note,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRefactorCandidateScheduleRefs(projectRowID int64, candidateRowID int64) ([]RefactorCandidateScheduleRef, error) {
	return s.queries.ListRefactorCandidateScheduleRefs(context.Background(), generated.ListRefactorCandidateScheduleRefsParams{
		ProjectRowID:   projectRowID,
		CandidateRowID: candidateRowID,
	})
}

// GetActiveRefactorCandidateScheduleRef returns the most recent scheduled ref for
// a candidate, or (nil, nil) when none is active.
func (s *Store) GetActiveRefactorCandidateScheduleRef(projectRowID int64, candidateRowID int64) (*RefactorCandidateScheduleRef, error) {
	row, err := s.queries.GetActiveRefactorCandidateScheduleRef(context.Background(), generated.GetActiveRefactorCandidateScheduleRefParams{
		ProjectRowID:   projectRowID,
		CandidateRowID: candidateRowID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

type UpdateRefactorCandidateScheduleRefStatusParams struct {
	ProjectRowID  int64
	ScheduleRefID string
	Status        string
	Note          string
}

func (s *Store) UpdateRefactorCandidateScheduleRefStatus(params UpdateRefactorCandidateScheduleRefStatusParams) (*RefactorCandidateScheduleRef, error) {
	if params.ProjectRowID <= 0 {
		return nil, errors.New("project_row_id must be a positive project row reference")
	}
	if err := validateRequiredString("schedule_ref_id", params.ScheduleRefID); err != nil {
		return nil, err
	}
	if err := validateScheduleRefStatus(params.Status); err != nil {
		return nil, err
	}

	row, err := s.queries.UpdateRefactorCandidateScheduleRefStatus(context.Background(), generated.UpdateRefactorCandidateScheduleRefStatusParams{
		Status:        params.Status,
		Note:          params.Note,
		ProjectRowID:  params.ProjectRowID,
		ScheduleRefID: params.ScheduleRefID,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ---------------------------------------------------------------------------
// Candidate status events (append-only in this pass)
// ---------------------------------------------------------------------------

type CreateRefactorCandidateStatusEventParams struct {
	EventID        string
	ProjectRowID   int64
	ProjectID      string
	CandidateRowID int64
	EventType      string
	FromStatus     string
	ToStatus       string
	Reason         string
	DetailJSON     string
}

func validateRefactorCandidateEventType(eventType string) error {
	switch eventType {
	case "created", "updated", "deferred", "rejected", "superseded", "scheduled", "completed", "completed_with_warnings", "scheduled_revision_required", "reopened":
		return nil
	default:
		return fmt.Errorf("invalid status event type %q", eventType)
	}
}

func (s *Store) CreateRefactorCandidateStatusEvent(params CreateRefactorCandidateStatusEventParams) (*RefactorCandidateStatusEvent, error) {
	if err := s.validateProjectOwnership(params.ProjectRowID, params.ProjectID); err != nil {
		return nil, err
	}
	if err := validateRequiredString("event_id", params.EventID); err != nil {
		return nil, err
	}
	if err := validateRefactorCandidateEventType(params.EventType); err != nil {
		return nil, err
	}
	if err := s.refactorCandidateRowInProject(params.ProjectRowID, params.CandidateRowID); err != nil {
		return nil, err
	}

	detailJSON := params.DetailJSON
	if detailJSON == "" {
		detailJSON = "{}"
	}
	if err := validateJSONObject("detail_json", detailJSON); err != nil {
		return nil, err
	}

	row, err := s.queries.CreateRefactorCandidateStatusEvent(context.Background(), generated.CreateRefactorCandidateStatusEventParams{
		EventID:        params.EventID,
		ProjectRowID:   params.ProjectRowID,
		ProjectID:      params.ProjectID,
		CandidateRowID: params.CandidateRowID,
		EventType:      params.EventType,
		FromStatus:     params.FromStatus,
		ToStatus:       params.ToStatus,
		Reason:         params.Reason,
		DetailJson:     detailJSON,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRefactorCandidateStatusEvents(projectRowID int64, candidateRowID int64, limit int64) ([]RefactorCandidateStatusEvent, error) {
	return s.queries.ListRefactorCandidateStatusEvents(context.Background(), generated.ListRefactorCandidateStatusEventsParams{
		ProjectRowID:   projectRowID,
		CandidateRowID: candidateRowID,
		Limit:          normalizeRefactorListLimit(limit),
	})
}
