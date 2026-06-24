package plans

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	appplans "relay/internal/app/plans"

	"github.com/go-chi/chi/v5"
)

// Handler is the plan feature HTTP transport adapter. It owns request/response
// DTO decoding and mapping and delegates all business behavior to the plan app
// services. It must not call store query methods directly for plan business logic.
type Handler struct {
	service      *appplans.Service
	orchestrator *appplans.OrchestratorWorkService
}

// NewHandler constructs a plan Handler.
func NewHandler(
	service *appplans.Service,
	orchestrator *appplans.OrchestratorWorkService,
) *Handler {
	return &Handler{
		service:      service,
		orchestrator: orchestrator,
	}
}

// POST /api/plans/validate
func (h *Handler) ValidatePlan(w http.ResponseWriter, r *http.Request) {
	var req PlanAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	rawPlan, ok, err := rawPlanFromRequest(req)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid plan payload")
		return
	}
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Plan is required")
		return
	}

	report, err := h.service.ValidatePlanForSubmission(r.Context(), rawPlan, req.ProjectID)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	shared.JSON(w, http.StatusOK, PlanAPIResponse{
		Success:    report.Valid,
		Validation: report,
	})
}

// POST /api/plans
func (h *Handler) SubmitPlan(w http.ResponseWriter, r *http.Request) {
	var req PlanAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	rawPlan, ok, err := rawPlanFromRequest(req)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid plan payload")
		return
	}
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Plan is required")
		return
	}

	result, err := h.service.SubmitPlan(r.Context(), appplans.SubmitPlanRequest{
		RawJSON:            rawPlan,
		SourceArtifactPath: req.SourceArtifactPath,
		ProjectID:          req.ProjectID,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if !result.Report.Valid {
		status := http.StatusUnprocessableEntity
		if hasPlanIssue(result.Report, appplans.IssuePlanDuplicatePlanID) {
			status = http.StatusConflict
		}
		shared.JSON(w, status, PlanAPIResponse{
			Success:    false,
			Validation: result.Report,
		})
		return
	}

	apiPasses := make([]PlanAPIPass, 0, len(result.Passes))
	for _, pass := range result.Passes {
		apiPasses = append(apiPasses, mapPlanPassToAPI(pass, nil))
	}
	apiPlan := mapPlanToAPI(result.Plan)

	shared.JSON(w, http.StatusCreated, PlanAPIResponse{
		Success:    true,
		Plan:       &apiPlan,
		Passes:     apiPasses,
		Validation: result.Report,
	})
}

// GET /api/plans
func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	const defaultLimit int64 = 50
	const maxLimit int64 = 100

	validStatuses := map[string]bool{"active": true, "complete": true, "abandoned": true}

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !validStatuses[status] {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid status filter")
		return
	}

	limit := defaultLimit
	limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitStr != "" {
		parsed, err := strconv.ParseInt(limitStr, 10, 64)
		if err != nil || parsed <= 0 {
			shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
			return
		}
		if parsed > maxLimit {
			parsed = maxLimit
		}
		limit = parsed
	}

	projectIDStr := strings.TrimSpace(r.URL.Query().Get("projectId"))

	summaries, err := h.service.ListPlanReadSummaries(r.Context(), appplans.PlanListQuery{
		Status:    status,
		Limit:     limit,
		ProjectID: projectIDStr,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list plans")
		return
	}

	apiPlans := make([]PlanAPIReadPlan, 0, len(summaries))
	for _, summary := range summaries {
		apiPlans = append(apiPlans, buildPlanAPIReadPlan(summary.Plan, summary.Passes, summary.CompletionReady))
	}

	shared.JSON(w, http.StatusOK, PlanReadAPIResponse{
		Success: true,
		Count:   len(apiPlans),
		Plans:   apiPlans,
	})
}

// GET /api/plans/{planId}
func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	planID := strings.TrimSpace(chi.URLParam(r, "planId"))
	if planID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Plan ID is required")
		return
	}

	projectIDStr := strings.TrimSpace(r.URL.Query().Get("projectId"))

	detail, err := h.service.GetPlanDetail(r.Context(), appplans.PlanDetailQuery{
		PlanID:    planID,
		ProjectID: projectIDStr,
	})
	if err != nil {
		switch {
		case errors.Is(err, appplans.ErrPlanNotFound):
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Plan with ID %q not found", planID))
		case errors.Is(err, appplans.ErrPlanProjectMismatch):
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Plan with ID %q not found in project %q", planID, projectIDStr))
		default:
			shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load plan detail")
		}
		return
	}

	apiPasses := make([]PlanAPIPass, 0, len(detail.Passes))
	for _, pass := range detail.Passes {
		apiPasses = append(apiPasses, mapPlanPassToAPI(pass, detail.RunsByPass[pass.ID]))
	}

	readPlan := buildPlanAPIReadPlan(detail.Plan, detail.Passes, detail.CompletionReady)
	shared.JSON(w, http.StatusOK, PlanReadAPIResponse{
		Success:         true,
		Plan:            &readPlan,
		Passes:          apiPasses,
		CompletionReady: detail.CompletionReady,
	})
}

// GET /api/plans/{planId}/passes/{passId}
func (h *Handler) GetPlanPass(w http.ResponseWriter, r *http.Request) {
	planID := strings.TrimSpace(chi.URLParam(r, "planId"))
	passID := strings.TrimSpace(chi.URLParam(r, "passId"))
	if planID == "" || passID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Plan ID and pass ID are required")
		return
	}

	projectIDStr := strings.TrimSpace(r.URL.Query().Get("projectId"))

	detail, err := h.service.GetPlanPassDetail(r.Context(), appplans.PlanPassDetailQuery{
		PlanID:    planID,
		PassID:    passID,
		ProjectID: projectIDStr,
	})
	if err != nil {
		switch {
		case errors.Is(err, appplans.ErrPlanNotFound):
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Plan with ID %q not found", planID))
		case errors.Is(err, appplans.ErrPlanProjectMismatch):
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Plan with ID %q not found in project %q", planID, projectIDStr))
		case errors.Is(err, appplans.ErrPlanPassNotFound):
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Pass with ID %q not found", passID))
		default:
			shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list associated runs")
		}
		return
	}

	readPlan := buildPlanAPIReadPlan(detail.Plan, detail.Passes, detail.CompletionReady)
	apiPass := mapPlanPassToAPI(detail.Pass, detail.AssociatedRuns)
	shared.JSON(w, http.StatusOK, PlanReadAPIResponse{
		Success:         true,
		Plan:            &readPlan,
		Pass:            &apiPass,
		CompletionReady: detail.CompletionReady,
	})
}

// GET /api/projects/{projectId}/plans/{planId}/next-pass-work
func (h *Handler) GetNextPassWork(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	planID := chi.URLParam(r, "planId")

	resp, err := h.orchestrator.GetNextPassWork(r.Context(), appplans.NextPassWorkRequest{
		ProjectID: projectID,
		PlanID:    planID,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred")
		return
	}

	if !resp.OK && len(resp.Blockers) > 0 && resp.Blockers[0].Code == appplans.BlockerUnsafeRequest {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", resp.Blockers[0].Message)
		return
	}

	shared.JSON(w, http.StatusOK, resp)
}

// GET /api/projects/{projectId}/plans/{planId}/next-audit-work
func (h *Handler) GetNextAuditWork(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	planID := strings.TrimSpace(chi.URLParam(r, "planId"))
	passID, ok := resolveOptionalQueryAlias(r, "passId", "pass_id")
	if !ok {
		shared.JSON(w, http.StatusBadRequest, appplans.NextAuditWorkResponse{
			OK:   false,
			Tool: appplans.NextAuditWorkTool,
			Blockers: []appplans.WorkBlocker{{
				Code:        appplans.BlockerUnsafeRequest,
				Message:     "passId and pass_id query parameters conflict",
				Recoverable: true,
			}},
		})
		return
	}
	runID, ok := resolveOptionalQueryAlias(r, "runId", "run_id")
	if !ok {
		shared.JSON(w, http.StatusBadRequest, appplans.NextAuditWorkResponse{
			OK:   false,
			Tool: appplans.NextAuditWorkTool,
			Blockers: []appplans.WorkBlocker{{
				Code:        appplans.BlockerUnsafeRequest,
				Message:     "runId and run_id query parameters conflict",
				Recoverable: true,
			}},
		})
		return
	}

	response, err := h.orchestrator.GetNextAuditWork(r.Context(), appplans.NextAuditWorkRequest{
		ProjectID: projectID,
		PlanID:    planID,
		PassID:    passID,
		RunID:     runID,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred")
		return
	}

	status := http.StatusOK
	if !response.OK && hasOnlyUnsafeRequestBlockers(response.Blockers) {
		status = http.StatusBadRequest
	}
	shared.JSON(w, status, response)
}

func resolveOptionalQueryAlias(r *http.Request, camel, snake string) (string, bool) {
	valCamel := r.URL.Query().Get(camel)
	valSnake := r.URL.Query().Get(snake)
	if valCamel != "" && valSnake != "" && valCamel != valSnake {
		return "", false
	}
	if valCamel != "" {
		return valCamel, true
	}
	return valSnake, true
}

func hasOnlyUnsafeRequestBlockers(blockers []appplans.WorkBlocker) bool {
	if len(blockers) == 0 {
		return false
	}
	for _, b := range blockers {
		if b.Code != appplans.BlockerUnsafeRequest {
			return false
		}
	}
	return true
}
