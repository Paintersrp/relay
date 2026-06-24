package refactors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"relay/internal/store"

	"github.com/google/uuid"
)

// Service implements the project-scoped refactor backlog business rules on top
// of the PASS-002 persistence layer.
type Service struct {
	store *store.Store
}

// NewService constructs a refactor backlog service backed by the given store.
func NewService(st *store.Store) *Service {
	return &Service{store: st}
}

// ---------------------------------------------------------------------------
// Discovery tasks
// ---------------------------------------------------------------------------

// CreateDiscoveryTask validates and persists a new discovery task (analysis
// prompt). It does not create candidates, plans, runs, or audits.
func (s *Service) CreateDiscoveryTask(ctx context.Context, projectID string, input DiscoveryTaskInput) (*DiscoveryTaskResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateDiscoveryTaskInput(input, true); len(issues) > 0 {
		return nil, issues, nil
	}

	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, err
	}

	scopeJSON, err := marshalTargetScope(input.TargetScope)
	if err != nil {
		return nil, nil, err
	}
	tagsJSON, err := marshalStringSlice(input.Tags)
	if err != nil {
		return nil, nil, err
	}
	metadataJSON, err := marshalMetadata(input.Metadata)
	if err != nil {
		return nil, nil, err
	}

	row, err := s.store.CreateRefactorDiscoveryTask(store.CreateRefactorDiscoveryTaskParams{
		TaskID:          strings.TrimSpace(input.DiscoveryTaskID),
		ProjectRowID:    project.ID,
		ProjectID:       project.ProjectID,
		Title:           strings.TrimSpace(input.Title),
		Prompt:          input.AnalysisPrompt,
		TargetScopeJSON: scopeJSON,
		Priority:        strings.TrimSpace(input.Priority),
		TagsJSON:        tagsJSON,
		MetadataJSON:    metadataJSON,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapDiscoveryTask(row)
	return &res, nil, nil
}

// GetDiscoveryTask returns a single discovery task scoped to the project.
func (s *Service) GetDiscoveryTask(ctx context.Context, projectID, taskID string) (*DiscoveryTaskResult, error) {
	_ = ctx
	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, err
	}
	row, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	res := mapDiscoveryTask(row)
	return &res, nil
}

// ListDiscoveryTasks lists discovery tasks for a project, optionally filtered by
// status.
func (s *Service) ListDiscoveryTasks(ctx context.Context, projectID, status string, limit int64) ([]DiscoveryTaskResult, error) {
	_ = ctx
	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, err
	}

	limit = normalizeLimit(limit)
	var rows []store.RefactorDiscoveryTask
	if status = strings.TrimSpace(status); status != "" {
		rows, err = s.store.ListRefactorDiscoveryTasksByProjectAndStatus(project.ID, status, limit)
	} else {
		rows, err = s.store.ListRefactorDiscoveryTasksByProject(project.ID, limit)
	}
	if err != nil {
		return nil, err
	}

	results := make([]DiscoveryTaskResult, 0, len(rows))
	for i := range rows {
		results = append(results, mapDiscoveryTask(&rows[i]))
	}
	return results, nil
}

// UpdateDiscoveryTask updates mutable discovery task fields. Only open tasks may
// be edited.
func (s *Service) UpdateDiscoveryTask(ctx context.Context, projectID, taskID string, input DiscoveryTaskInput) (*DiscoveryTaskResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateDiscoveryTaskInput(input, false); len(issues) > 0 {
		return nil, issues, nil
	}

	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, strings.TrimSpace(taskID))
	if err != nil {
		return nil, nil, err
	}
	if issue := discoveryTransitionIssue(existing.Status, DiscoveryStatusOpen); issue != nil {
		return nil, []ValidationIssue{*issue}, nil
	}

	scopeJSON, err := marshalTargetScope(input.TargetScope)
	if err != nil {
		return nil, nil, err
	}
	tagsJSON, err := marshalStringSlice(input.Tags)
	if err != nil {
		return nil, nil, err
	}
	metadataJSON, err := marshalMetadata(input.Metadata)
	if err != nil {
		return nil, nil, err
	}

	row, err := s.store.UpdateRefactorDiscoveryTask(store.UpdateRefactorDiscoveryTaskParams{
		ProjectRowID:    project.ID,
		TaskID:          existing.TaskID,
		Title:           strings.TrimSpace(input.Title),
		Prompt:          input.AnalysisPrompt,
		TargetScopeJSON: scopeJSON,
		Priority:        strings.TrimSpace(input.Priority),
		TagsJSON:        tagsJSON,
		MetadataJSON:    metadataJSON,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapDiscoveryTask(row)
	return &res, nil, nil
}

// CompleteDiscoveryTask marks an open discovery task completed.
func (s *Service) CompleteDiscoveryTask(ctx context.Context, projectID, taskID string, input DiscoveryTaskLifecycleInput) (*DiscoveryTaskResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateDiscoveryLifecycleInput("complete", input); len(issues) > 0 {
		return nil, issues, nil
	}
	project, existing, issues, err := s.loadDiscoveryForTransition(projectID, taskID, DiscoveryStatusOpen)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	row, err := s.store.UpdateRefactorDiscoveryTaskStatus(store.UpdateRefactorDiscoveryTaskStatusParams{
		ProjectRowID: project.ID,
		TaskID:       existing.TaskID,
		Status:       DiscoveryStatusCompleted,
		ClosedReason: input.ClosureReason,
		CompletedAt:  nowRFC3339(),
	})
	if err != nil {
		return nil, nil, err
	}
	res := mapDiscoveryTask(row)
	return &res, nil, nil
}

// CloseDiscoveryTask closes an open discovery task with a required reason.
func (s *Service) CloseDiscoveryTask(ctx context.Context, projectID, taskID string, input DiscoveryTaskLifecycleInput) (*DiscoveryTaskResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateDiscoveryLifecycleInput("close", input); len(issues) > 0 {
		return nil, issues, nil
	}
	project, existing, issues, err := s.loadDiscoveryForTransition(projectID, taskID, DiscoveryStatusOpen)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	row, err := s.store.UpdateRefactorDiscoveryTaskStatus(store.UpdateRefactorDiscoveryTaskStatusParams{
		ProjectRowID: project.ID,
		TaskID:       existing.TaskID,
		Status:       DiscoveryStatusClosed,
		ClosedReason: input.ClosureReason,
		ClosedAt:     nowRFC3339(),
	})
	if err != nil {
		return nil, nil, err
	}
	res := mapDiscoveryTask(row)
	return &res, nil, nil
}

// SupersedeDiscoveryTask supersedes an open or completed discovery task. The
// superseding task must exist in the same project and must not be the task
// itself. The discovery row has no superseded-by column; the reference is
// validated for referential integrity only.
func (s *Service) SupersedeDiscoveryTask(ctx context.Context, projectID, taskID string, input DiscoveryTaskLifecycleInput) (*DiscoveryTaskResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateDiscoveryLifecycleInput("supersede", input); len(issues) > 0 {
		return nil, issues, nil
	}

	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, strings.TrimSpace(taskID))
	if err != nil {
		return nil, nil, err
	}
	if issue := discoveryTransitionIssue(existing.Status, DiscoveryStatusOpen, DiscoveryStatusCompleted); issue != nil {
		return nil, []ValidationIssue{*issue}, nil
	}

	target := strings.TrimSpace(input.SupersededByTaskID)
	if target == existing.TaskID {
		return nil, []ValidationIssue{{Field: "superseded_by_task_id", Code: CodeSelfReference, Message: "a discovery task cannot supersede itself"}}, nil
	}
	if _, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, target); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, []ValidationIssue{{Field: "superseded_by_task_id", Code: CodeNotFound, Message: fmt.Sprintf("discovery task %q not found in project", target)}}, nil
		}
		return nil, nil, err
	}

	row, err := s.store.UpdateRefactorDiscoveryTaskStatus(store.UpdateRefactorDiscoveryTaskStatusParams{
		ProjectRowID: project.ID,
		TaskID:       existing.TaskID,
		Status:       DiscoveryStatusSuperseded,
		ClosedReason: input.ClosureReason,
		ClosedAt:     nowRFC3339(),
	})
	if err != nil {
		return nil, nil, err
	}
	res := mapDiscoveryTask(row)
	return &res, nil, nil
}

// ---------------------------------------------------------------------------
// Candidates
// ---------------------------------------------------------------------------

// CreateCandidate validates pass-readiness and same-project references, then
// persists a new candidate with status ready, along with its discovery links and
// dependencies.
func (s *Service) CreateCandidate(ctx context.Context, projectID string, input CandidateInput) (*CandidateResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateCandidateInput(input, true); len(issues) > 0 {
		return nil, issues, nil
	}

	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, err
	}

	refIssues, err := s.validateCandidateReferences(project, strings.TrimSpace(input.CandidateID), input.SourceDiscoveryTaskIDs, input.CandidateDependencyIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(refIssues) > 0 {
		return nil, refIssues, nil
	}

	arrays, err := marshalCandidateArrays(input)
	if err != nil {
		return nil, nil, err
	}

	row, err := s.store.CreateRefactorCandidate(store.CreateRefactorCandidateParams{
		CandidateID:            strings.TrimSpace(input.CandidateID),
		ProjectRowID:           project.ID,
		ProjectID:              project.ProjectID,
		Title:                  strings.TrimSpace(input.Title),
		ProblemSummary:         input.ProblemSummary,
		CurrentBehavior:        input.CurrentBehavior,
		DesiredBehavior:        input.DesiredBehavior,
		Rationale:              input.Rationale,
		ProposedPassName:       strings.TrimSpace(input.ProposedPassName),
		ProposedPassGoal:       input.ProposedPassGoal,
		ProposedPassScopeJSON:  arrays.proposedPassScope,
		ProposedNonGoalsJSON:   arrays.nonGoals,
		TargetFilesJSON:        arrays.targetFiles,
		ValidationCommandsJSON: arrays.validationCommands,
		AuditFocusJSON:         arrays.auditFocus,
		ConstraintsJSON:        arrays.constraints,
		RiskLevel:              strings.TrimSpace(input.RiskLevel),
		Status:                 CandidateStatusReady,
		DependencyNotes:        input.DependencyNotes,
		MetadataJSON:           arrays.metadata,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := s.applyCandidateReferences(project, row, input.SourceDiscoveryTaskIDs, input.CandidateDependencyIDs, false); err != nil {
		return nil, nil, err
	}

	res := mapCandidate(row)
	return &res, nil, nil
}

// GetCandidate returns a single candidate scoped to the project.
func (s *Service) GetCandidate(ctx context.Context, projectID, candidateID string) (*CandidateResult, error) {
	_ = ctx
	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, err
	}
	row, err := s.store.GetRefactorCandidateByCandidateID(project.ID, strings.TrimSpace(candidateID))
	if err != nil {
		return nil, err
	}
	res := mapCandidate(row)
	return &res, nil
}

// ListCandidates lists candidates for a project. A non-empty query performs a
// search; otherwise an optional status filter is applied.
func (s *Service) ListCandidates(ctx context.Context, projectID, status, query string, limit int64) ([]CandidateResult, error) {
	_ = ctx
	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, err
	}

	limit = normalizeLimit(limit)
	var rows []store.RefactorCandidate
	switch {
	case strings.TrimSpace(query) != "":
		rows, err = s.store.SearchRefactorCandidatesByProject(project.ID, query, limit)
	case strings.TrimSpace(status) != "":
		rows, err = s.store.ListRefactorCandidatesByProjectAndStatus(project.ID, strings.TrimSpace(status), limit)
	default:
		rows, err = s.store.ListRefactorCandidatesByProject(project.ID, limit)
	}
	if err != nil {
		return nil, err
	}

	results := make([]CandidateResult, 0, len(rows))
	for i := range rows {
		results = append(results, mapCandidate(&rows[i]))
	}
	return results, nil
}

// UpdateCandidate updates pass-ready candidate fields. Updates are allowed only
// for candidates in ready or deferred status.
func (s *Service) UpdateCandidate(ctx context.Context, projectID, candidateID string, input CandidateInput) (*CandidateResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateCandidateInput(input, false); len(issues) > 0 {
		return nil, issues, nil
	}

	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	existing, err := s.store.GetRefactorCandidateByCandidateID(project.ID, strings.TrimSpace(candidateID))
	if err != nil {
		return nil, nil, err
	}
	if issue := candidateTransitionIssue(existing.Status, CandidateStatusReady, CandidateStatusDeferred); issue != nil {
		return nil, []ValidationIssue{*issue}, nil
	}

	refIssues, err := s.validateCandidateReferences(project, existing.CandidateID, input.SourceDiscoveryTaskIDs, input.CandidateDependencyIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(refIssues) > 0 {
		return nil, refIssues, nil
	}

	arrays, err := marshalCandidateArrays(input)
	if err != nil {
		return nil, nil, err
	}

	row, err := s.store.UpdateRefactorCandidate(store.UpdateRefactorCandidateParams{
		ProjectRowID:           project.ID,
		CandidateID:            existing.CandidateID,
		Title:                  strings.TrimSpace(input.Title),
		ProblemSummary:         input.ProblemSummary,
		CurrentBehavior:        input.CurrentBehavior,
		DesiredBehavior:        input.DesiredBehavior,
		Rationale:              input.Rationale,
		ProposedPassName:       strings.TrimSpace(input.ProposedPassName),
		ProposedPassGoal:       input.ProposedPassGoal,
		ProposedPassScopeJSON:  arrays.proposedPassScope,
		ProposedNonGoalsJSON:   arrays.nonGoals,
		TargetFilesJSON:        arrays.targetFiles,
		ValidationCommandsJSON: arrays.validationCommands,
		AuditFocusJSON:         arrays.auditFocus,
		ConstraintsJSON:        arrays.constraints,
		DependencyNotes:        input.DependencyNotes,
		MetadataJSON:           arrays.metadata,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := s.applyCandidateReferences(project, row, input.SourceDiscoveryTaskIDs, input.CandidateDependencyIDs, true); err != nil {
		return nil, nil, err
	}

	res := mapCandidate(row)
	return &res, nil, nil
}

// DeferCandidate moves a ready candidate to deferred with a required reason.
func (s *Service) DeferCandidate(ctx context.Context, projectID, candidateID string, input CandidateLifecycleInput) (*CandidateResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateCandidateLifecycleInput("defer", input); len(issues) > 0 {
		return nil, issues, nil
	}
	project, existing, issues, err := s.loadCandidateForTransition(projectID, candidateID, CandidateStatusReady)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	row, err := s.store.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID: project.ID,
		CandidateID:  existing.CandidateID,
		Status:       CandidateStatusDeferred,
		DeferReason:  input.DeferReason,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := s.recordStatusEvent(project, existing.ID, "deferred", existing.Status, CandidateStatusDeferred, input.DeferReason); err != nil {
		return nil, nil, err
	}
	res := mapCandidate(row)
	return &res, nil, nil
}

// RejectCandidate moves a ready or deferred candidate to rejected with a
// required reason.
func (s *Service) RejectCandidate(ctx context.Context, projectID, candidateID string, input CandidateLifecycleInput) (*CandidateResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateCandidateLifecycleInput("reject", input); len(issues) > 0 {
		return nil, issues, nil
	}
	project, existing, issues, err := s.loadCandidateForTransition(projectID, candidateID, CandidateStatusReady, CandidateStatusDeferred)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	row, err := s.store.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID:   project.ID,
		CandidateID:    existing.CandidateID,
		Status:         CandidateStatusRejected,
		RejectedReason: input.RejectReason,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := s.recordStatusEvent(project, existing.ID, "rejected", existing.Status, CandidateStatusRejected, input.RejectReason); err != nil {
		return nil, nil, err
	}
	res := mapCandidate(row)
	return &res, nil, nil
}

// SupersedeCandidate moves a ready or deferred candidate to superseded. The
// superseding candidate must exist in the same project and must not be the
// candidate itself.
func (s *Service) SupersedeCandidate(ctx context.Context, projectID, candidateID string, input CandidateLifecycleInput) (*CandidateResult, []ValidationIssue, error) {
	_ = ctx
	if issues := validateCandidateLifecycleInput("supersede", input); len(issues) > 0 {
		return nil, issues, nil
	}
	project, existing, issues, err := s.loadCandidateForTransition(projectID, candidateID, CandidateStatusReady, CandidateStatusDeferred)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	target := strings.TrimSpace(input.SupersededByCandidateID)
	if target == existing.CandidateID {
		return nil, []ValidationIssue{{Field: "superseded_by_candidate_id", Code: CodeSelfReference, Message: "a candidate cannot supersede itself"}}, nil
	}
	if _, err := s.store.GetRefactorCandidateByCandidateID(project.ID, target); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, []ValidationIssue{{Field: "superseded_by_candidate_id", Code: CodeNotFound, Message: fmt.Sprintf("candidate %q not found in project", target)}}, nil
		}
		return nil, nil, err
	}

	row, err := s.store.UpdateRefactorCandidateStatusMetadata(store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID:            project.ID,
		CandidateID:             existing.CandidateID,
		Status:                  CandidateStatusSuperseded,
		SupersededByCandidateID: target,
		SupersededReason:        input.SupersedeReason,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := s.recordStatusEvent(project, existing.ID, "superseded", existing.Status, CandidateStatusSuperseded, input.SupersedeReason); err != nil {
		return nil, nil, err
	}
	res := mapCandidate(row)
	return &res, nil, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *Service) resolveProject(projectID string) (*store.Project, error) {
	return s.store.GetProjectByProjectID(strings.TrimSpace(projectID))
}

// loadDiscoveryForTransition resolves the project and discovery task and checks
// the allowed source statuses. It returns a validation issue slice (never with
// an error) when the transition is not allowed.
func (s *Service) loadDiscoveryForTransition(projectID, taskID string, allowed ...string) (*store.Project, *store.RefactorDiscoveryTask, []ValidationIssue, error) {
	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	existing, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, strings.TrimSpace(taskID))
	if err != nil {
		return nil, nil, nil, err
	}
	if issue := discoveryTransitionIssue(existing.Status, allowed...); issue != nil {
		return nil, nil, []ValidationIssue{*issue}, nil
	}
	return project, existing, nil, nil
}

// loadCandidateForTransition resolves the project and candidate and checks the
// allowed source statuses.
func (s *Service) loadCandidateForTransition(projectID, candidateID string, allowed ...string) (*store.Project, *store.RefactorCandidate, []ValidationIssue, error) {
	project, err := s.resolveProject(projectID)
	if err != nil {
		return nil, nil, nil, err
	}
	existing, err := s.store.GetRefactorCandidateByCandidateID(project.ID, strings.TrimSpace(candidateID))
	if err != nil {
		return nil, nil, nil, err
	}
	if issue := candidateTransitionIssue(existing.Status, allowed...); issue != nil {
		return nil, nil, []ValidationIssue{*issue}, nil
	}
	return project, existing, nil, nil
}

// validateCandidateReferences confirms every referenced discovery task and
// dependency candidate exists in the same project, and rejects self-dependency.
// It returns validation issues for client-facing problems and an error only for
// unexpected store failures.
func (s *Service) validateCandidateReferences(project *store.Project, candidateID string, taskIDs, dependencyIDs []string) ([]ValidationIssue, error) {
	var issues []ValidationIssue

	for _, raw := range taskIDs {
		taskID := strings.TrimSpace(raw)
		if taskID == "" {
			continue
		}
		if _, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, taskID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				issues = append(issues, ValidationIssue{Field: "source_discovery_task_ids", Code: CodeNotFound, Message: fmt.Sprintf("discovery task %q not found in project", taskID)})
				continue
			}
			return nil, err
		}
	}

	for _, raw := range dependencyIDs {
		depID := strings.TrimSpace(raw)
		if depID == "" {
			continue
		}
		if depID == candidateID {
			issues = append(issues, ValidationIssue{Field: "candidate_dependency_ids", Code: CodeSelfReference, Message: "a candidate cannot depend on itself"})
			continue
		}
		if _, err := s.store.GetRefactorCandidateByCandidateID(project.ID, depID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				issues = append(issues, ValidationIssue{Field: "candidate_dependency_ids", Code: CodeNotFound, Message: fmt.Sprintf("dependency candidate %q not found in project", depID)})
				continue
			}
			return nil, err
		}
	}

	return issues, nil
}

// applyCandidateReferences persists discovery links and candidate dependencies
// for the given candidate. When reset is true, existing dependencies are cleared
// and rebuilt. Discovery links are additive (the schema enforces uniqueness), so
// already-present links are skipped.
func (s *Service) applyCandidateReferences(project *store.Project, candidate *store.RefactorCandidate, taskIDs, dependencyIDs []string, reset bool) error {
	if reset {
		if err := s.store.DeleteRefactorCandidateDependencies(project.ID, candidate.ID); err != nil {
			return err
		}
	}

	existingLinks, err := s.store.ListRefactorCandidateDiscoveryLinks(project.ID, candidate.ID)
	if err != nil {
		return err
	}
	linkedTaskRows := make(map[int64]bool, len(existingLinks))
	for _, link := range existingLinks {
		linkedTaskRows[link.DiscoveryTaskRowID] = true
	}

	for _, raw := range taskIDs {
		taskID := strings.TrimSpace(raw)
		if taskID == "" {
			continue
		}
		task, err := s.store.GetRefactorDiscoveryTaskByTaskID(project.ID, taskID)
		if err != nil {
			return err
		}
		if linkedTaskRows[task.ID] {
			continue
		}
		if _, err := s.store.CreateRefactorCandidateDiscoveryLink(store.CreateRefactorCandidateDiscoveryLinkParams{
			LinkID:             "rlink-" + uuid.NewString(),
			ProjectRowID:       project.ID,
			ProjectID:          project.ProjectID,
			CandidateRowID:     candidate.ID,
			DiscoveryTaskRowID: task.ID,
		}); err != nil {
			return err
		}
		linkedTaskRows[task.ID] = true
	}

	for _, raw := range dependencyIDs {
		depID := strings.TrimSpace(raw)
		if depID == "" {
			continue
		}
		dep, err := s.store.GetRefactorCandidateByCandidateID(project.ID, depID)
		if err != nil {
			return err
		}
		if _, err := s.store.CreateRefactorCandidateDependency(store.CreateRefactorCandidateDependencyParams{
			DependencyID:            "rdep-" + uuid.NewString(),
			ProjectRowID:            project.ID,
			ProjectID:               project.ProjectID,
			CandidateRowID:          candidate.ID,
			DependsOnCandidateRowID: dep.ID,
		}); err != nil {
			return err
		}
	}

	return nil
}

// recordStatusEvent appends a candidate status event for a lifecycle change.
func (s *Service) recordStatusEvent(project *store.Project, candidateRowID int64, eventType, fromStatus, toStatus, reason string) error {
	_, err := s.store.CreateRefactorCandidateStatusEvent(store.CreateRefactorCandidateStatusEventParams{
		EventID:        "revent-" + uuid.NewString(),
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: candidateRowID,
		EventType:      eventType,
		FromStatus:     fromStatus,
		ToStatus:       toStatus,
		Reason:         reason,
	})
	return err
}

// candidateTransitionIssue returns a validation issue when the current candidate
// status is not in the allowed set.
func candidateTransitionIssue(current string, allowed ...string) *ValidationIssue {
	for _, a := range allowed {
		if current == a {
			return nil
		}
	}
	code := CodeInvalidTransition
	switch current {
	case CandidateStatusRejected, CandidateStatusSuperseded, CandidateStatusCompleted, CandidateStatusCompletedWithWarnings:
		code = CodeTerminalStatus
	}
	return &ValidationIssue{
		Field:   "status",
		Code:    code,
		Message: fmt.Sprintf("candidate in status %q does not allow this operation", current),
	}
}

// discoveryTransitionIssue returns a validation issue when the current discovery
// task status is not in the allowed set.
func discoveryTransitionIssue(current string, allowed ...string) *ValidationIssue {
	for _, a := range allowed {
		if current == a {
			return nil
		}
	}
	code := CodeInvalidTransition
	switch current {
	case DiscoveryStatusClosed, DiscoveryStatusSuperseded:
		code = CodeTerminalStatus
	}
	return &ValidationIssue{
		Field:   "status",
		Code:    code,
		Message: fmt.Sprintf("discovery task in status %q does not allow this operation", current),
	}
}

// ---------------------------------------------------------------------------
// Mapping and JSON helpers
// ---------------------------------------------------------------------------

type candidateArrays struct {
	proposedPassScope  string
	nonGoals           string
	targetFiles        string
	validationCommands string
	auditFocus         string
	constraints        string
	metadata           string
}

func marshalCandidateArrays(input CandidateInput) (candidateArrays, error) {
	var out candidateArrays
	var err error
	if out.proposedPassScope, err = marshalStringSlice(input.ProposedPassScope); err != nil {
		return out, err
	}
	if out.nonGoals, err = marshalStringSlice(input.NonGoals); err != nil {
		return out, err
	}
	if out.targetFiles, err = marshalStringSlice(input.TargetFiles); err != nil {
		return out, err
	}
	if out.validationCommands, err = marshalStringSlice(input.ValidationCommands); err != nil {
		return out, err
	}
	if out.auditFocus, err = marshalStringSlice(input.AuditFocus); err != nil {
		return out, err
	}
	if out.constraints, err = marshalStringSlice(input.Constraints); err != nil {
		return out, err
	}
	if out.metadata, err = marshalMetadata(input.Metadata); err != nil {
		return out, err
	}
	return out, nil
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func marshalStringSlice(values []string) (string, error) {
	b, err := json.Marshal(cleanStringSlice(values))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalMetadata(metadata map[string]string) (string, error) {
	if metadata == nil {
		return "{}", nil
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalTargetScope(scope TargetScope) (string, error) {
	cleaned := TargetScope{
		Kind:   strings.TrimSpace(scope.Kind),
		Values: cleanStringSlice(scope.Values),
	}
	b, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func unmarshalMetadata(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}

func unmarshalTargetScope(raw string) TargetScope {
	scope := TargetScope{Values: []string{}}
	if strings.TrimSpace(raw) == "" {
		return scope
	}
	if err := json.Unmarshal([]byte(raw), &scope); err != nil {
		return TargetScope{Values: []string{}}
	}
	if scope.Values == nil {
		scope.Values = []string{}
	}
	return scope
}

func mapDiscoveryTask(row *store.RefactorDiscoveryTask) DiscoveryTaskResult {
	return DiscoveryTaskResult{
		DiscoveryTaskID: row.TaskID,
		ProjectID:       row.ProjectID,
		Title:           row.Title,
		AnalysisPrompt:  row.Prompt,
		TargetScope:     unmarshalTargetScope(row.TargetScopeJson),
		Status:          row.Status,
		Priority:        row.Priority,
		Tags:            unmarshalStringSlice(row.TagsJson),
		CreatedFrom:     row.CreatedFrom,
		Metadata:        unmarshalMetadata(row.MetadataJson),
		ClosureReason:   row.ClosedReason,
		CompletedAt:     row.CompletedAt,
		ClosedAt:        row.ClosedAt,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func mapCandidate(row *store.RefactorCandidate) CandidateResult {
	return CandidateResult{
		CandidateID:             row.CandidateID,
		ProjectID:               row.ProjectID,
		Title:                   row.Title,
		ProblemSummary:          row.ProblemSummary,
		CurrentBehavior:         row.CurrentBehavior,
		DesiredBehavior:         row.DesiredBehavior,
		Rationale:               row.Rationale,
		ProposedPassName:        row.ProposedPassName,
		ProposedPassGoal:        row.ProposedPassGoal,
		ProposedPassScope:       unmarshalStringSlice(row.ProposedPassScopeJson),
		NonGoals:                unmarshalStringSlice(row.ProposedNonGoalsJson),
		TargetFiles:             unmarshalStringSlice(row.TargetFilesJson),
		ValidationCommands:      unmarshalStringSlice(row.ValidationCommandsJson),
		AuditFocus:              unmarshalStringSlice(row.AuditFocusJson),
		Constraints:             unmarshalStringSlice(row.ConstraintsJson),
		RiskLevel:               row.RiskLevel,
		Status:                  row.Status,
		DependencyNotes:         row.DependencyNotes,
		DeferReason:             row.DeferReason,
		RejectReason:            row.RejectedReason,
		SupersededByCandidateID: row.SupersededByCandidateID,
		SupersedeReason:         row.SupersededReason,
		Metadata:                unmarshalMetadata(row.MetadataJson),
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func validateDiscoveryLifecycleInput(action string, input DiscoveryTaskLifecycleInput) []ValidationIssue {
	var issues []ValidationIssue
	switch action {
	case "close":
		issues = append(issues, validateNonEmptyString("closure_reason", input.ClosureReason, CodeRequired)...)
		issues = append(issues, scanSecretLikeStrings("closure_reason", input.ClosureReason)...)
	case "supersede":
		issues = append(issues, validateNonEmptyString("superseded_by_task_id", input.SupersededByTaskID, CodeRequired)...)
		issues = append(issues, scanSecretLikeStrings("closure_reason", input.ClosureReason)...)
	case "complete":
		issues = append(issues, scanSecretLikeStrings("closure_reason", input.ClosureReason)...)
	}
	return issues
}
