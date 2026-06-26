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

func (h *Handler) CreatePlanAttemptWithIntent(w http.ResponseWriter, r *http.Request) {
	var req CreatePlanAttemptWithIntentAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	appReq, blocked, err := req.toApp(chi.URLParam(r, "projectId"))
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if blocked != nil {
		writePlanAttemptResult(w, blocked, http.StatusCreated)
		return
	}

	result, err := h.service.CreatePlanAttemptWithIntent(r.Context(), appReq)
	if err == nil && result != nil && result.OK {
		if result.ReviewPolicy != nil && result.ReviewPolicy.DriftReviewMode == appplans.DriftReviewModeAutomatic && result.PlanAttempt != nil {
			prepared, blocked, prepErr := h.service.PreparePlanAttemptDriftReview(r.Context(), appplans.RunPlanAttemptDriftReviewRequest{
				ProjectID:     appReq.ProjectID,
				PlanAttemptID: result.PlanAttempt.PlanAttemptID,
				AutomaticFlow: true,
			})
			if prepErr != nil {
				err = prepErr
			} else if blocked != nil {
				result.ReviewAction = blocked.ReviewAction
				result.ReviewGate = blocked.ReviewGate
			} else if prepared != nil {
				result.ReviewGate = prepared.ReviewGate
				if h.driftService == nil {
					result.ReviewAction = &appplans.PlanAttemptReviewAction{Action: "run_drift_review", OK: false, FailureCode: "drift_service_unavailable", Message: "drift review service is unavailable"}
				} else {
					reviewResult, reviewErr := h.driftService.RunInternalReview(r.Context(), appdrift.InternalReviewRequest{
						ProjectID:          prepared.ProjectID,
						PlanAttemptID:      prepared.PlanAttemptID,
						RequestedTier:      prepared.RequestedTier,
						AllowModelCall:     prepared.AllowModelCall,
						ForceHighAssurance: prepared.ForceHighAssurance,
					})
					if reviewErr != nil {
						err = reviewErr
					} else {
						mapped := mapInternalReviewResultToAttemptResult(reviewResult)
						result.DriftReview = mapped.DriftReview
						result.ReviewAction = mapped.ReviewAction
					}
				}
			}
		}
		if result.PlanAttempt != nil {
			if gate, _, gateErr := h.service.GetPlanAttemptReviewGate(r.Context(), appplans.PlanAttemptReviewGateRequest{ProjectID: appReq.ProjectID, PlanAttemptID: result.PlanAttempt.PlanAttemptID}); gateErr == nil {
				result.ReviewGate = gate
			}
		}
	}
	writePlanAttemptResultOrError(w, result, err, http.StatusCreated)
}

func (h *Handler) GetPlanIntentReviewPacket(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetPlanIntentReviewPacket(r.Context(), appplans.GetPlanIntentReviewPacketRequest{
		ProjectID:     strings.TrimSpace(chi.URLParam(r, "projectId")),
		PlanAttemptID: strings.TrimSpace(chi.URLParam(r, "planAttemptId")),
	})
	writePlanAttemptResultOrError(w, result, err, http.StatusOK)
}

func (h *Handler) SubmitIntentDriftReview(w http.ResponseWriter, r *http.Request) {
	var req SubmitIntentDriftReviewAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	planAttemptID := strings.TrimSpace(chi.URLParam(r, "planAttemptId"))
	result, err := h.service.SubmitIntentDriftReview(r.Context(), appplans.SubmitIntentDriftReviewRequest{
		ProjectID:     projectID,
		PlanAttemptID: planAttemptID,
		DriftReview:   driftReviewAPIToApp(req.DriftReview, planAttemptID),
	})
	writePlanAttemptResultOrError(w, result, err, http.StatusOK)
}

func (h *Handler) RevisePlanAttempt(w http.ResponseWriter, r *http.Request) {
	var req RevisePlanAttemptAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	appReq, blocked, err := req.toApp(chi.URLParam(r, "projectId"), chi.URLParam(r, "planAttemptId"))
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if blocked != nil {
		writePlanAttemptResult(w, blocked, http.StatusCreated)
		return
	}
	result, err := h.service.RevisePlanAttempt(r.Context(), appReq)
	writePlanAttemptResultOrError(w, result, err, http.StatusCreated)
}

func (h *Handler) VoidPlanAttempt(w http.ResponseWriter, r *http.Request) {
	var req VoidPlanAttemptAPIRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	result, err := h.service.VoidPlanAttempt(r.Context(), appplans.VoidPlanAttemptRequest{
		ProjectID:     strings.TrimSpace(chi.URLParam(r, "projectId")),
		PlanAttemptID: strings.TrimSpace(chi.URLParam(r, "planAttemptId")),
	})
	writePlanAttemptResultOrError(w, result, err, http.StatusOK)
}

func (h *Handler) ApprovePlanAttempt(w http.ResponseWriter, r *http.Request) {
	var req ApprovePlanAttemptAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	result, err := h.service.ApprovePlanAttempt(r.Context(), appplans.ApprovePlanAttemptRequest{
		ProjectID:                 strings.TrimSpace(chi.URLParam(r, "projectId")),
		PlanAttemptID:             strings.TrimSpace(chi.URLParam(r, "planAttemptId")),
		Approved:                  req.Approved,
		AcceptedDriftReviewID:     req.AcceptedDriftReviewID,
		DriftAcknowledged:         req.DriftAcknowledged,
		NoDriftReviewAcknowledged: req.NoDriftReviewAcknowledged,
	})
	writePlanAttemptResultOrError(w, result, err, http.StatusOK)
}

func (h *Handler) SubmitPlanAttempt(w http.ResponseWriter, r *http.Request) {
	var req SubmitPlanAttemptAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	result, err := h.service.SubmitPlanAttempt(r.Context(), appplans.SubmitPlanAttemptRequest{
		ProjectID:                      strings.TrimSpace(chi.URLParam(r, "projectId")),
		PlanAttemptID:                  strings.TrimSpace(chi.URLParam(r, "planAttemptId")),
		SubmissionConfirmed:            req.SubmissionConfirmed,
		ReviewedPlanJSONArtifactSHA256: req.ReviewedPlanJSONArtifactSHA256,
		AcceptedDriftReviewID:          req.AcceptedDriftReviewID,
	})
	writePlanAttemptResultOrError(w, result, err, http.StatusCreated)
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(dst)
}

func writePlanAttemptResultOrError(w http.ResponseWriter, result *appplans.PlanAttemptResult, err error, successStatus int) {
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writePlanAttemptResult(w, result, successStatus)
}

func writePlanAttemptResult(w http.ResponseWriter, result *appplans.PlanAttemptResult, successStatus int) {
	if result == nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "plan attempt action returned no result")
		return
	}
	status := successStatus
	if !result.OK {
		status = attemptBlockerHTTPStatus(result.BlockerCode)
	}
	shared.JSON(w, status, mapPlanAttemptResultToAPI(result))
}
