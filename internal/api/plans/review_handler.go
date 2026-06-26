package plans

import (
	"encoding/json"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	appdrift "relay/internal/app/drift"
	appplans "relay/internal/app/plans"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetPlanReviewSettings(w http.ResponseWriter, r *http.Request) {
	settings, blocked, err := h.service.GetPlanReviewSettings(r.Context(), strings.TrimSpace(chi.URLParam(r, "projectId")))
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if blocked != nil {
		shared.JSON(w, attemptBlockerHTTPStatus(blocked.BlockerCode), PlanReviewSettingsAPIResponse{
			Success:     false,
			BlockerCode: string(blocked.BlockerCode),
			Message:     blocked.Message,
		})
		return
	}
	api := mapPlanReviewSettingsToAPI(*settings)
	shared.JSON(w, http.StatusOK, PlanReviewSettingsAPIResponse{Success: true, Settings: &api})
}

func (h *Handler) UpdatePlanReviewSettings(w http.ResponseWriter, r *http.Request) {
	var req UpdatePlanReviewSettingsAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	settings, blocked, err := h.service.UpdatePlanReviewSettings(r.Context(), appplans.UpdatePlanReviewSettingsRequest{
		ProjectID:       strings.TrimSpace(chi.URLParam(r, "projectId")),
		DriftReviewMode: req.DriftReviewMode,
		ModelTier:       req.ModelTier,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if blocked != nil {
		shared.JSON(w, attemptBlockerHTTPStatus(blocked.BlockerCode), PlanReviewSettingsAPIResponse{
			Success:     false,
			BlockerCode: string(blocked.BlockerCode),
			Message:     blocked.Message,
		})
		return
	}
	api := mapPlanReviewSettingsToAPI(*settings)
	shared.JSON(w, http.StatusOK, PlanReviewSettingsAPIResponse{Success: true, Settings: &api})
}

func (h *Handler) GetPlanAttemptReviewGate(w http.ResponseWriter, r *http.Request) {
	gate, blocked, err := h.service.GetPlanAttemptReviewGate(r.Context(), appplans.PlanAttemptReviewGateRequest{
		ProjectID:     strings.TrimSpace(chi.URLParam(r, "projectId")),
		PlanAttemptID: strings.TrimSpace(chi.URLParam(r, "planAttemptId")),
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if blocked != nil {
		shared.JSON(w, attemptBlockerHTTPStatus(blocked.BlockerCode), PlanAttemptReviewGateAPIResponse{
			Success:     false,
			BlockerCode: string(blocked.BlockerCode),
			Message:     blocked.Message,
		})
		return
	}
	api := mapReviewGateToAPI(*gate)
	shared.JSON(w, http.StatusOK, PlanAttemptReviewGateAPIResponse{Success: true, ReviewGate: &api})
}

func (h *Handler) RunPlanAttemptDriftReview(w http.ResponseWriter, r *http.Request) {
	var req RunPlanAttemptDriftReviewAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	planAttemptID := strings.TrimSpace(chi.URLParam(r, "planAttemptId"))
	gate, blocked, err := h.service.GetPlanAttemptReviewGate(r.Context(), appplans.PlanAttemptReviewGateRequest{
		ProjectID:     projectID,
		PlanAttemptID: planAttemptID,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if blocked != nil {
		writePlanAttemptResult(w, blocked, http.StatusOK)
		return
	}
	if gate.DriftReviewMode == appplans.DriftReviewModeDisabled {
		writePlanAttemptResult(w, &appplans.PlanAttemptResult{
			OK:          false,
			BlockerCode: appplans.BlockerDriftReviewBlocked,
			Message:     "disabled drift review mode does not run internal reviews",
			ReviewGate:  gate,
		}, http.StatusOK)
		return
	}
	if gate.DriftReviewMode == appplans.DriftReviewModeExternal {
		writePlanAttemptResult(w, &appplans.PlanAttemptResult{
			OK:          false,
			BlockerCode: appplans.BlockerDriftReviewRequired,
			Message:     "external drift review mode requires review packet retrieval and external review submission",
			ReviewGate:  gate,
		}, http.StatusOK)
		return
	}
	if gate.DriftReviewMode == appplans.DriftReviewModeManual && !req.AllowModelCall {
		writePlanAttemptResult(w, &appplans.PlanAttemptResult{
			OK:          false,
			BlockerCode: appplans.BlockerDriftReviewBlocked,
			Message:     "model call is not explicitly allowed in the request",
			ReviewGate:  gate,
			ReviewAction: &appplans.PlanAttemptReviewAction{
				Action:      "run_drift_review",
				OK:          false,
				FailureCode: string(appdrift.FailureModelCallNotAllowed),
				Message:     "model call is not explicitly allowed in the request",
			},
		}, http.StatusOK)
		return
	}
	if h.driftService == nil {
		writePlanAttemptResult(w, &appplans.PlanAttemptResult{
			OK:          false,
			BlockerCode: appplans.BlockerDriftReviewBlocked,
			Message:     "drift review service is unavailable",
			ReviewGate:  gate,
		}, http.StatusOK)
		return
	}
	requestedTier := strings.TrimSpace(req.RequestedTier)
	if requestedTier == "" {
		requestedTier = gate.ModelTier
	}
	reviewResult, err := h.driftService.RunInternalReview(r.Context(), appdrift.InternalReviewRequest{
		ProjectID:          projectID,
		PlanAttemptID:      planAttemptID,
		RequestedTier:      requestedTier,
		AllowModelCall:     req.AllowModelCall || gate.DriftReviewMode == appplans.DriftReviewModeAutomatic,
		ForceHighAssurance: req.ForceHighAssurance,
	})
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	result := mapInternalReviewResultToAttemptResult(reviewResult)
	if updatedGate, _, gateErr := h.service.GetPlanAttemptReviewGate(r.Context(), appplans.PlanAttemptReviewGateRequest{ProjectID: projectID, PlanAttemptID: planAttemptID}); gateErr == nil {
		result.ReviewGate = updatedGate
	}
	writePlanAttemptResult(w, result, http.StatusOK)
}

func mapInternalReviewResultToAttemptResult(reviewResult *appdrift.InternalReviewResult) *appplans.PlanAttemptResult {
	if reviewResult == nil {
		return &appplans.PlanAttemptResult{OK: false, BlockerCode: appplans.BlockerDriftReviewBlocked, Message: "drift review returned no result"}
	}
	result := &appplans.PlanAttemptResult{
		OK:          reviewResult.OK,
		DriftReview: reviewResult.DriftReview,
		ReviewAction: &appplans.PlanAttemptReviewAction{
			Action:           "run_drift_review",
			OK:               reviewResult.OK,
			FailureCode:      string(reviewResult.FailureCode),
			Message:          reviewResult.Message,
			Escalated:        reviewResult.Escalated,
			EscalationReason: reviewResult.EscalationReason,
			FinalTier:        reviewResult.FinalTier,
		},
	}
	if !reviewResult.OK {
		result.BlockerCode = appplans.BlockerDriftReviewBlocked
		result.Message = reviewResult.Message
	}
	return result
}
