package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/refactors"

	"github.com/go-chi/chi/v5"
)

// RefactorBacklogAPIResponse is the JSON response envelope for refactor backlog
// discovery task and candidate endpoints.
type RefactorBacklogAPIResponse struct {
	Success        bool                            `json:"success"`
	Count          int                             `json:"count,omitempty"`
	DiscoveryTask  *refactors.DiscoveryTaskResult  `json:"discoveryTask,omitempty"`
	DiscoveryTasks []refactors.DiscoveryTaskResult `json:"discoveryTasks,omitempty"`
	Candidate      *refactors.CandidateResult      `json:"candidate,omitempty"`
	Candidates     []refactors.CandidateResult     `json:"candidates,omitempty"`
	Validation     []refactors.ValidationIssue     `json:"validation,omitempty"`
}

// RefactorDiscoveryTaskAPIRequest is the snake_case request payload for discovery
// task create/update.
type RefactorDiscoveryTaskAPIRequest struct {
	DiscoveryTaskID string                `json:"discovery_task_id"`
	Title           string                `json:"title"`
	AnalysisPrompt  string                `json:"analysis_prompt"`
	TargetScope     refactors.TargetScope `json:"target_scope"`
	Priority        string                `json:"priority"`
	Tags            []string              `json:"tags"`
	Metadata        map[string]string     `json:"metadata"`
}

// RefactorDiscoveryTaskLifecycleAPIRequest carries discovery lifecycle params.
type RefactorDiscoveryTaskLifecycleAPIRequest struct {
	ClosureReason      string `json:"closure_reason"`
	SupersededByTaskID string `json:"superseded_by_task_id"`
}

// RefactorCandidateAPIRequest is the snake_case request payload for candidate
// create/update.
type RefactorCandidateAPIRequest struct {
	CandidateID            string            `json:"candidate_id"`
	Title                  string            `json:"title"`
	ProblemSummary         string            `json:"problem_summary"`
	CurrentBehavior        string            `json:"current_behavior"`
	DesiredBehavior        string            `json:"desired_behavior"`
	Rationale              string            `json:"rationale"`
	ProposedPassName       string            `json:"proposed_pass_name"`
	ProposedPassGoal       string            `json:"proposed_pass_goal"`
	ProposedPassScope      []string          `json:"proposed_pass_scope"`
	NonGoals               []string          `json:"non_goals"`
	TargetFiles            []string          `json:"target_files"`
	ValidationCommands     []string          `json:"validation_commands"`
	AuditFocus             []string          `json:"audit_focus"`
	Constraints            []string          `json:"constraints"`
	RiskLevel              string            `json:"risk_level"`
	DependencyNotes        string            `json:"dependency_notes"`
	SourceDiscoveryTaskIDs []string          `json:"source_discovery_task_ids"`
	CandidateDependencyIDs []string          `json:"candidate_dependency_ids"`
	Metadata               map[string]string `json:"metadata"`
}

// RefactorCandidateLifecycleAPIRequest carries candidate lifecycle params.
type RefactorCandidateLifecycleAPIRequest struct {
	DeferReason             string `json:"defer_reason"`
	RejectReason            string `json:"reject_reason"`
	SupersedeReason         string `json:"supersede_reason"`
	SupersededByCandidateID string `json:"superseded_by_candidate_id"`
}

// RefactorCandidateScheduleAPIRequest is the snake_case request payload for
// marking a candidate scheduled.
type RefactorCandidateScheduleAPIRequest struct {
	ScheduleRefID string `json:"schedule_ref_id"`
	ScheduleKind  string `json:"schedule_kind"`
	PlanID        string `json:"plan_id"`
	PassID        string `json:"pass_id"`
	RunID         string `json:"run_id"`
	Note          string `json:"note"`
}

// ---------------------------------------------------------------------------
// Discovery task handlers
// ---------------------------------------------------------------------------

// ListRefactorDiscoveryTasks lists project-scoped discovery tasks.
func (h *APIHandler) ListRefactorDiscoveryTasks(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	limit, ok := parseRefactorLimit(w, r)
	if !ok {
		return
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	tasks, err := h.refactorService.ListDiscoveryTasks(r.Context(), projectID, status, limit)
	if err != nil {
		writeRefactorStoreError(w, err, "Failed to list discovery tasks")
		return
	}

	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{
		Success:        true,
		Count:          len(tasks),
		DiscoveryTasks: tasks,
	})
}

// CreateRefactorDiscoveryTask creates a new discovery task.
func (h *APIHandler) CreateRefactorDiscoveryTask(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	var req RefactorDiscoveryTaskAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	task, issues, err := h.refactorService.CreateDiscoveryTask(r.Context(), projectID, refactors.DiscoveryTaskInput{
		DiscoveryTaskID: req.DiscoveryTaskID,
		ProjectID:       projectID,
		Title:           req.Title,
		AnalysisPrompt:  req.AnalysisPrompt,
		TargetScope:     req.TargetScope,
		Priority:        req.Priority,
		Tags:            req.Tags,
		Metadata:        req.Metadata,
	})
	if !writeRefactorOutcome(w, issues, err, "Failed to create discovery task") {
		return
	}

	writeJSON(w, http.StatusCreated, RefactorBacklogAPIResponse{Success: true, DiscoveryTask: task})
}

// GetRefactorDiscoveryTask returns a single discovery task.
func (h *APIHandler) GetRefactorDiscoveryTask(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	taskID, ok := refactorRouteParam(w, r, "taskId")
	if !ok {
		return
	}

	task, err := h.refactorService.GetDiscoveryTask(r.Context(), projectID, taskID)
	if err != nil {
		writeRefactorStoreError(w, err, "Failed to load discovery task")
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, DiscoveryTask: task})
}

// UpdateRefactorDiscoveryTask updates mutable discovery task fields.
func (h *APIHandler) UpdateRefactorDiscoveryTask(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	taskID, ok := refactorRouteParam(w, r, "taskId")
	if !ok {
		return
	}
	var req RefactorDiscoveryTaskAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	task, issues, err := h.refactorService.UpdateDiscoveryTask(r.Context(), projectID, taskID, refactors.DiscoveryTaskInput{
		DiscoveryTaskID: taskID,
		ProjectID:       projectID,
		Title:           req.Title,
		AnalysisPrompt:  req.AnalysisPrompt,
		TargetScope:     req.TargetScope,
		Priority:        req.Priority,
		Tags:            req.Tags,
		Metadata:        req.Metadata,
	})
	if !writeRefactorOutcome(w, issues, err, "Failed to update discovery task") {
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, DiscoveryTask: task})
}

// CompleteRefactorDiscoveryTask marks a discovery task completed.
func (h *APIHandler) CompleteRefactorDiscoveryTask(w http.ResponseWriter, r *http.Request) {
	h.discoveryLifecycle(w, r, h.refactorService.CompleteDiscoveryTask, "Failed to complete discovery task")
}

// CloseRefactorDiscoveryTask closes a discovery task.
func (h *APIHandler) CloseRefactorDiscoveryTask(w http.ResponseWriter, r *http.Request) {
	h.discoveryLifecycle(w, r, h.refactorService.CloseDiscoveryTask, "Failed to close discovery task")
}

// SupersedeRefactorDiscoveryTask supersedes a discovery task.
func (h *APIHandler) SupersedeRefactorDiscoveryTask(w http.ResponseWriter, r *http.Request) {
	h.discoveryLifecycle(w, r, h.refactorService.SupersedeDiscoveryTask, "Failed to supersede discovery task")
}

type discoveryLifecycleFn func(ctx context.Context, projectID, taskID string, input refactors.DiscoveryTaskLifecycleInput) (*refactors.DiscoveryTaskResult, []refactors.ValidationIssue, error)

func (h *APIHandler) discoveryLifecycle(w http.ResponseWriter, r *http.Request, fn discoveryLifecycleFn, failMsg string) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	taskID, ok := refactorRouteParam(w, r, "taskId")
	if !ok {
		return
	}
	var req RefactorDiscoveryTaskLifecycleAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	task, issues, err := fn(r.Context(), projectID, taskID, refactors.DiscoveryTaskLifecycleInput{
		ClosureReason:      req.ClosureReason,
		SupersededByTaskID: req.SupersededByTaskID,
	})
	if !writeRefactorOutcome(w, issues, err, failMsg) {
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, DiscoveryTask: task})
}

// ---------------------------------------------------------------------------
// Candidate handlers
// ---------------------------------------------------------------------------

// ListRefactorCandidates lists or searches project-scoped candidates.
func (h *APIHandler) ListRefactorCandidates(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	limit, ok := parseRefactorLimit(w, r)
	if !ok {
		return
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	candidates, err := h.refactorService.ListCandidates(r.Context(), projectID, status, query, limit)
	if err != nil {
		writeRefactorStoreError(w, err, "Failed to list candidates")
		return
	}

	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{
		Success:    true,
		Count:      len(candidates),
		Candidates: candidates,
	})
}

// CreateRefactorCandidate creates a new pass-ready candidate.
func (h *APIHandler) CreateRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	var req RefactorCandidateAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	candidate, issues, err := h.refactorService.CreateCandidate(r.Context(), projectID, candidateInputFromRequest(projectID, req))
	if !writeRefactorOutcome(w, issues, err, "Failed to create candidate") {
		return
	}
	writeJSON(w, http.StatusCreated, RefactorBacklogAPIResponse{Success: true, Candidate: candidate})
}

// GetRefactorCandidate returns a single candidate.
func (h *APIHandler) GetRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	candidateID, ok := refactorRouteParam(w, r, "candidateId")
	if !ok {
		return
	}

	candidate, err := h.refactorService.GetCandidate(r.Context(), projectID, candidateID)
	if err != nil {
		writeRefactorStoreError(w, err, "Failed to load candidate")
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, Candidate: candidate})
}

// UpdateRefactorCandidate updates pass-ready candidate fields.
func (h *APIHandler) UpdateRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	candidateID, ok := refactorRouteParam(w, r, "candidateId")
	if !ok {
		return
	}
	var req RefactorCandidateAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	candidate, issues, err := h.refactorService.UpdateCandidate(r.Context(), projectID, candidateID, candidateInputFromRequest(projectID, req))
	if !writeRefactorOutcome(w, issues, err, "Failed to update candidate") {
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, Candidate: candidate})
}

// DeferRefactorCandidate moves a candidate to deferred.
func (h *APIHandler) DeferRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	h.candidateLifecycle(w, r, h.refactorService.DeferCandidate, "Failed to defer candidate")
}

// RejectRefactorCandidate moves a candidate to rejected.
func (h *APIHandler) RejectRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	h.candidateLifecycle(w, r, h.refactorService.RejectCandidate, "Failed to reject candidate")
}

// SupersedeRefactorCandidate moves a candidate to superseded.
func (h *APIHandler) SupersedeRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	h.candidateLifecycle(w, r, h.refactorService.SupersedeCandidate, "Failed to supersede candidate")
}

// MarkScheduledRefactorCandidate marks a ready candidate scheduled, recording a
// passive scheduling reference for a managed plan/pass. It does not create or
// mutate plan, pass, or run records.
func (h *APIHandler) MarkScheduledRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	candidateID, ok := refactorRouteParam(w, r, "candidateId")
	if !ok {
		return
	}
	var req RefactorCandidateScheduleAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	candidate, issues, err := h.refactorService.MarkCandidateScheduled(r.Context(), projectID, candidateID, refactors.CandidateScheduleInput{
		ScheduleRefID: req.ScheduleRefID,
		ScheduleKind:  req.ScheduleKind,
		PlanID:        req.PlanID,
		PassID:        req.PassID,
		RunID:         req.RunID,
		Note:          req.Note,
	})
	if !writeRefactorOutcome(w, issues, err, "Failed to mark candidate scheduled") {
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, Candidate: candidate})
}

type candidateLifecycleFn func(ctx context.Context, projectID, candidateID string, input refactors.CandidateLifecycleInput) (*refactors.CandidateResult, []refactors.ValidationIssue, error)

func (h *APIHandler) candidateLifecycle(w http.ResponseWriter, r *http.Request, fn candidateLifecycleFn, failMsg string) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	candidateID, ok := refactorRouteParam(w, r, "candidateId")
	if !ok {
		return
	}
	var req RefactorCandidateLifecycleAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	candidate, issues, err := fn(r.Context(), projectID, candidateID, refactors.CandidateLifecycleInput{
		DeferReason:             req.DeferReason,
		RejectReason:            req.RejectReason,
		SupersedeReason:         req.SupersedeReason,
		SupersededByCandidateID: req.SupersededByCandidateID,
	})
	if !writeRefactorOutcome(w, issues, err, failMsg) {
		return
	}
	writeJSON(w, http.StatusOK, RefactorBacklogAPIResponse{Success: true, Candidate: candidate})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func candidateInputFromRequest(projectID string, req RefactorCandidateAPIRequest) refactors.CandidateInput {
	return refactors.CandidateInput{
		CandidateID:            req.CandidateID,
		ProjectID:              projectID,
		Title:                  req.Title,
		ProblemSummary:         req.ProblemSummary,
		CurrentBehavior:        req.CurrentBehavior,
		DesiredBehavior:        req.DesiredBehavior,
		Rationale:              req.Rationale,
		ProposedPassName:       req.ProposedPassName,
		ProposedPassGoal:       req.ProposedPassGoal,
		ProposedPassScope:      req.ProposedPassScope,
		NonGoals:               req.NonGoals,
		TargetFiles:            req.TargetFiles,
		ValidationCommands:     req.ValidationCommands,
		AuditFocus:             req.AuditFocus,
		Constraints:            req.Constraints,
		RiskLevel:              req.RiskLevel,
		DependencyNotes:        req.DependencyNotes,
		SourceDiscoveryTaskIDs: req.SourceDiscoveryTaskIDs,
		CandidateDependencyIDs: req.CandidateDependencyIDs,
		Metadata:               req.Metadata,
	}
}

func refactorRouteParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := strings.TrimSpace(chi.URLParam(r, name))
	if value == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", name+" is required")
		return "", false
	}
	return value, true
}

func decodeRefactorJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return false
	}
	return true
}

func parseRefactorLimit(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
		return 0, false
	}
	return parsed, true
}

// writeRefactorOutcome writes a validation/store error when present and reports
// whether the caller should continue to write a success response.
func writeRefactorOutcome(w http.ResponseWriter, issues []refactors.ValidationIssue, err error, failMsg string) bool {
	if err != nil {
		writeRefactorStoreError(w, err, failMsg)
		return false
	}
	if len(issues) > 0 {
		writeRefactorValidationError(w, issues)
		return false
	}
	return true
}

func writeRefactorValidationError(w http.ResponseWriter, issues []refactors.ValidationIssue) {
	writeJSON(w, http.StatusBadRequest, RelayApiErrorShape{
		Error:   "VALIDATION_ERROR",
		Message: "Refactor backlog validation failed",
		Details: map[string]interface{}{"validation": issues},
	})
}

func writeRefactorStoreError(w http.ResponseWriter, err error, failMsg string) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Refactor backlog record not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", failMsg)
}
