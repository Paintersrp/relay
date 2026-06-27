package projects

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	appprojects "relay/internal/app/projects"

	"github.com/go-chi/chi/v5"
)

func writePlanSeedValidationError(w http.ResponseWriter, issues []appprojects.PlanSeedValidationIssue) {
	shared.JSON(w, http.StatusBadRequest, shared.ErrorShape{
		Error:   "VALIDATION_ERROR",
		Message: "Plan Seed validation failed",
		Details: map[string]interface{}{
			"validation": issues,
		},
	})
}

func validateAPIInput(req PlanSeedAPIRequest) []appprojects.PlanSeedValidationIssue {
	var issues []appprojects.PlanSeedValidationIssue
	if len(req.Title) > 200 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "title",
			Code:    "too_long",
			Message: "title must be at most 200 characters",
		})
	}
	if len(req.QuickContext) > 6000 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "quick_context",
			Code:    "too_long",
			Message: "quick_context must be at most 6000 characters",
		})
	}
	if len(req.Priority) > 80 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "priority",
			Code:    "too_long",
			Message: "priority must be at most 80 characters",
		})
	}
	if len(req.SourceLabel) > 200 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "source_label",
			Code:    "too_long",
			Message: "source_label must be at most 200 characters",
		})
	}
	if len(req.Tags) > 50 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "tags",
			Code:    "too_many_items",
			Message: "tags must have at most 50 items",
		})
	}
	for i, val := range req.Tags {
		if len(val) > 500 {
			issues = append(issues, appprojects.PlanSeedValidationIssue{
				Field:   fmt.Sprintf("tags[%d]", i),
				Code:    "too_long",
				Message: "tag item must be at most 500 characters",
			})
		}
	}
	if len(req.Constraints) > 50 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "constraints",
			Code:    "too_many_items",
			Message: "constraints must have at most 50 items",
		})
	}
	for i, val := range req.Constraints {
		if len(val) > 500 {
			issues = append(issues, appprojects.PlanSeedValidationIssue{
				Field:   fmt.Sprintf("constraints[%d]", i),
				Code:    "too_long",
				Message: "constraint item must be at most 500 characters",
			})
		}
	}
	if len(req.NonGoals) > 50 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "non_goals",
			Code:    "too_many_items",
			Message: "non_goals must have at most 50 items",
		})
	}
	for i, val := range req.NonGoals {
		if len(val) > 500 {
			issues = append(issues, appprojects.PlanSeedValidationIssue{
				Field:   fmt.Sprintf("non_goals[%d]", i),
				Code:    "too_long",
				Message: "non_goal item must be at most 500 characters",
			})
		}
	}
	return issues
}

func validateAPIUpdateInput(req PlanSeedUpdateAPIRequest) []appprojects.PlanSeedValidationIssue {
	var issues []appprojects.PlanSeedValidationIssue
	if req.Title != nil && len(*req.Title) > 200 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "title",
			Code:    "too_long",
			Message: "title must be at most 200 characters",
		})
	}
	if req.QuickContext != nil && len(*req.QuickContext) > 6000 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "quick_context",
			Code:    "too_long",
			Message: "quick_context must be at most 6000 characters",
		})
	}
	if req.Priority != nil && len(*req.Priority) > 80 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   "priority",
			Code:    "too_long",
			Message: "priority must be at most 80 characters",
		})
	}
	if req.Tags != nil {
		issues = appendPlanSeedListIssues(issues, "tags", "tag item", *req.Tags)
	}
	if req.Constraints != nil {
		issues = appendPlanSeedListIssues(issues, "constraints", "constraint item", *req.Constraints)
	}
	if req.NonGoals != nil {
		issues = appendPlanSeedListIssues(issues, "non_goals", "non_goal item", *req.NonGoals)
	}
	return issues
}

func appendPlanSeedListIssues(issues []appprojects.PlanSeedValidationIssue, field, itemLabel string, values []string) []appprojects.PlanSeedValidationIssue {
	if len(values) > 50 {
		issues = append(issues, appprojects.PlanSeedValidationIssue{
			Field:   field,
			Code:    "too_many_items",
			Message: field + " must have at most 50 items",
		})
	}
	for i, val := range values {
		if len(val) > 500 {
			issues = append(issues, appprojects.PlanSeedValidationIssue{
				Field:   fmt.Sprintf("%s[%d]", field, i),
				Code:    "too_long",
				Message: itemLabel + " must be at most 500 characters",
			})
		}
	}
	return issues
}

func decodeStrictPlanSeedJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		shared.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON payload: "+err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		shared.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON payload")
		return false
	}
	return true
}

func (h *Handler) CreatePlanSeed(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}

	var req PlanSeedAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	if boundsIssues := validateAPIInput(req); len(boundsIssues) > 0 {
		writePlanSeedValidationError(w, boundsIssues)
		return
	}

	result, issues, err := h.service.CreatePlanSeed(r.Context(), projectID, appprojects.PlanSeedInput{
		Title:        req.Title,
		QuickContext: req.QuickContext,
		Priority:     req.Priority,
		Constraints:  req.Constraints,
		NonGoals:     req.NonGoals,
		Tags:         req.Tags,
		SourceType:   appprojects.PlanSeedSourceManual,
		SourceLabel:  req.SourceLabel,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create plan seed")
		return
	}
	if len(issues) > 0 {
		writePlanSeedValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusCreated, ProjectAPIResponse{
		Success:  true,
		PlanSeed: planSeedAPIPtr(mapPlanSeedToAPI(*result)),
	})
}

func (h *Handler) ListPlanSeeds(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))

	var limit int64
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.ParseInt(rawLimit, 10, 64)
		if err != nil || parsed <= 0 {
			shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
			return
		}
		limit = parsed
	}

	seeds, issues, err := h.service.ListPlanSeeds(r.Context(), projectID, status, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list plan seeds")
		return
	}
	if len(issues) > 0 {
		writePlanSeedValidationError(w, issues)
		return
	}

	mapped := mapPlanSeedsToAPI(seeds)
	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:   true,
		Count:     len(mapped),
		PlanSeeds: mapped,
	})
}

func (h *Handler) GetPlanSeed(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	seedID := strings.TrimSpace(chi.URLParam(r, "seedId"))
	if projectID == "" || seedID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and seedId are required")
		return
	}

	seed, err := h.service.GetPlanSeed(r.Context(), projectID, seedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load plan seed")
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:  true,
		PlanSeed: planSeedAPIPtr(mapPlanSeedToAPI(*seed)),
	})
}

func (h *Handler) GetPlanSeedPlanningContext(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	seedID := strings.TrimSpace(chi.URLParam(r, "seedId"))
	if projectID == "" || seedID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and seedId are required")
		return
	}

	ctx, err := h.service.GetPlanSeedPlanningContext(r.Context(), projectID, seedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load plan seed planning context")
		return
	}

	shared.JSON(w, http.StatusOK, PlanSeedPlanningContextAPIResponse{
		Success:         true,
		PlanningContext: mapPlanSeedPlanningContextToAPI(*ctx),
	})
}

func (h *Handler) CreatePlanAttemptFromSeed(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	seedID := strings.TrimSpace(chi.URLParam(r, "seedId"))
	if projectID == "" || seedID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and seedId are required")
		return
	}

	var req CreatePlanAttemptFromSeedAPIRequest
	if !decodeStrictPlanSeedJSON(w, r, &req) {
		return
	}

	result, err := h.service.CreatePlanAttemptFromSeed(r.Context(), projectID, seedID, appprojects.CreatePlanAttemptFromSeedInput{
		PlannerPassPlanJSON: req.PlannerPassPlanJSON,
		SourceArtifactPath:  req.SourceArtifactPath,
		DriftReviewMode:     req.DriftReviewMode,
		ModelTier:           req.ModelTier,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create plan attempt from seed")
		return
	}

	status := http.StatusCreated
	if result == nil || !result.OK {
		status = planSeedAttemptBlockerHTTPStatus(result)
	}
	shared.JSON(w, status, mapPlanSeedAttemptResultToAPI(result))
}

func planSeedAttemptBlockerHTTPStatus(result *appprojects.CreatePlanAttemptFromSeedResult) int {
	if result == nil {
		return http.StatusServiceUnavailable
	}
	switch result.BlockerCode {
	case appprojects.PlanSeedBlockerSeedAlreadyPlanned:
		return http.StatusConflict
	case appprojects.PlanSeedBlockerSeedNotExpandable:
		return http.StatusUnprocessableEntity
	case appprojects.PlanSeedBlockerMissingPlanArtifact, appprojects.PlanSeedBlockerUnsafeSeedContext:
		return http.StatusBadRequest
	case appprojects.PlanSeedBlockerDraftAttemptsUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusUnprocessableEntity
	}
}

func (h *Handler) UpdatePlanSeed(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	seedID := strings.TrimSpace(chi.URLParam(r, "seedId"))
	if projectID == "" || seedID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and seedId are required")
		return
	}

	var req PlanSeedUpdateAPIRequest
	if !decodeStrictPlanSeedJSON(w, r, &req) {
		return
	}

	if boundsIssues := validateAPIUpdateInput(req); len(boundsIssues) > 0 {
		writePlanSeedValidationError(w, boundsIssues)
		return
	}

	// First load the seed to check existence and preserve SourceType.
	existing, err := h.service.GetPlanSeed(r.Context(), projectID, seedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load existing plan seed")
		return
	}

	input := appprojects.PlanSeedInput{
		Title:        existing.Title,
		QuickContext: existing.QuickContext,
		Priority:     existing.Priority,
		Constraints:  existing.Constraints,
		NonGoals:     existing.NonGoals,
		Tags:         existing.Tags,
		SourceType:   existing.SourceType,
		SourceLabel:  existing.SourceLabel,
		SourceRefID:  existing.SourceRefID,
	}
	if req.Title != nil {
		input.Title = *req.Title
	}
	if req.QuickContext != nil {
		input.QuickContext = *req.QuickContext
	}
	if req.Priority != nil {
		input.Priority = *req.Priority
	}
	if req.Constraints != nil {
		input.Constraints = *req.Constraints
	}
	if req.NonGoals != nil {
		input.NonGoals = *req.NonGoals
	}
	if req.Tags != nil {
		input.Tags = *req.Tags
	}

	result, issues, err := h.service.UpdatePlanSeed(r.Context(), projectID, seedID, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update plan seed")
		return
	}
	if len(issues) > 0 {
		writePlanSeedValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:  true,
		PlanSeed: planSeedAPIPtr(mapPlanSeedToAPI(*result)),
	})
}

func (h *Handler) DeferPlanSeed(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	seedID := strings.TrimSpace(chi.URLParam(r, "seedId"))
	if projectID == "" || seedID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and seedId are required")
		return
	}

	var req PlanSeedLifecycleAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	result, issues, err := h.service.DeferPlanSeed(r.Context(), projectID, seedID, appprojects.PlanSeedLifecycleInput{
		DeferReason: req.DeferReason,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to defer plan seed")
		return
	}
	if len(issues) > 0 {
		writePlanSeedValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:  true,
		PlanSeed: planSeedAPIPtr(mapPlanSeedToAPI(*result)),
	})
}

func (h *Handler) RejectPlanSeed(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	seedID := strings.TrimSpace(chi.URLParam(r, "seedId"))
	if projectID == "" || seedID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and seedId are required")
		return
	}

	var req PlanSeedLifecycleAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	result, issues, err := h.service.RejectPlanSeed(r.Context(), projectID, seedID, appprojects.PlanSeedLifecycleInput{
		RejectReason: req.RejectReason,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or Plan Seed not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to reject plan seed")
		return
	}
	if len(issues) > 0 {
		writePlanSeedValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:  true,
		PlanSeed: planSeedAPIPtr(mapPlanSeedToAPI(*result)),
	})
}
