package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	appplans "relay/internal/app/plans"
	appruns "relay/internal/app/runs"

	"github.com/go-chi/chi/v5"
)

// Handler is the run feature HTTP transport adapter.
type Handler struct {
	service   *appruns.Service
	lifecycle *appplans.RunLifecycleService
}

// NewHandler constructs a run Handler.
func NewHandler(service *appruns.Service, lifecycle *appplans.RunLifecycleService) *Handler {
	return &Handler{service: service, lifecycle: lifecycle}
}

// writeRunError maps a typed app RunError to an HTTP response. Returns true if
// it handled the error.
func writeRunError(w http.ResponseWriter, err error) bool {
	var re *appruns.RunError
	if errors.As(err, &re) {
		if re.Body != nil {
			shared.JSON(w, re.HTTPStatus, re.Body)
			return true
		}
		shared.Error(w, re.HTTPStatus, re.Code, re.Message)
		return true
	}
	return false
}

func parseRunID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return 0, false
	}
	return id, true
}

// GET /api/runs
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	details, err := h.service.ListRuns(r.Context(), 100)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list recent runs")
		return
	}
	result := make([]RelayRun, 0, len(details))
	for _, d := range details {
		result = append(result, MapRunToRelayRun(d))
	}
	shared.JSON(w, http.StatusOK, result)
}

// GET /api/runs/{id}
func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	details, err := h.service.GetRun(r.Context(), id)
	if err != nil {
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}
	shared.JSON(w, http.StatusOK, MapRunToRelayRun(details))
}

// GET /api/runs/{id}/events
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	exists, err := h.service.RunExists(r.Context(), id)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list events")
		return
	}
	if !exists {
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}
	events, err := h.service.ListEvents(r.Context(), id)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list events")
		return
	}
	idStr := strconv.FormatInt(id, 10)
	result := make([]RelayRunEvent, 0)
	for _, e := range events {
		result = append(result, RelayRunEvent{
			ID:        strconv.FormatInt(e.ID, 10),
			RunID:     idStr,
			Kind:      mapEventKind(e.Level, e.Message),
			Message:   e.Message,
			CreatedAt: shared.ParseAndFormatTime(e.CreatedAt),
		})
	}
	if len(result) > 100 {
		result = result[len(result)-100:]
	}
	shared.JSON(w, http.StatusOK, result)
}

// POST /api/runs/{id}/approve-intake
func (h *Handler) ApproveIntake(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	var req appruns.ApproveIntakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	result, err := h.service.ApproveIntake(r.Context(), id, req, h.lifecycle)
	if err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	status, _, lifecycleState, _, _ := ResolveRunDisplayState(result.Run.Status)
	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          strconv.FormatInt(id, 10),
		"status":         status,
		"lifecycleState": lifecycleState,
		"updatedAt":      shared.ParseAndFormatTime(result.Run.UpdatedAt),
	})
}

// POST /api/runs/{id}/prepare
func (h *Handler) PrepareRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	idStr := strconv.FormatInt(id, 10)
	result, err := h.service.PrepareRun(r.Context(), id)
	if err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if !result.Success {
		shared.JSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"success":          false,
			"runId":            idStr,
			"status":           "packet_validation_failed",
			"lifecycleState":   "prepare",
			"packetId":         result.PacketID,
			"issues":           result.Issues,
			"validationReport": result.ValidationReport,
		})
		return
	}
	status, _, lifecycleState, _, _ := ResolveRunDisplayState(result.Run.Status)
	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"runId":            idStr,
		"packetId":         result.PacketID,
		"status":           status,
		"lifecycleState":   lifecycleState,
		"validationReport": result.ValidationReport,
	})
}

func (h *Handler) writeBriefResult(w http.ResponseWriter, id int64, result appruns.BriefResult) {
	idStr := strconv.FormatInt(id, 10)
	if !result.Success {
		shared.JSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"success": false,
			"runId":   idStr,
			"issues":  result.Issues,
		})
		return
	}
	if result.RunLoaded {
		status, _, lifecycleState, _, _ := ResolveRunDisplayState(result.Run.Status)
		shared.JSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"runId":          idStr,
			"status":         status,
			"lifecycleState": lifecycleState,
			"updatedAt":      shared.ParseAndFormatTime(result.Run.UpdatedAt),
		})
		return
	}
	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"runId":   idStr,
	})
}

// POST /api/runs/{id}/render-brief
func (h *Handler) RenderBrief(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	result, err := h.service.RenderBrief(r.Context(), id)
	if err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	h.writeBriefResult(w, id, result)
}

// POST /api/runs/{id}/approve-brief
func (h *Handler) ApproveBrief(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	result, err := h.service.ApproveBrief(r.Context(), id)
	if err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	h.writeBriefResult(w, id, result)
}

// POST /api/runs/{id}/execute
func (h *Handler) ExecuteRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	idStr := strconv.FormatInt(id, 10)
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Action = "start"
	}

	switch req.Action {
	case "start":
		result, err := h.service.ExecuteRun(r.Context(), id)
		if err != nil {
			if writeRunError(w, err) {
				return
			}
			shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		status, _, lifecycleState, _, _ := ResolveRunDisplayState(result.Run.Status)
		shared.JSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"runId":          idStr,
			"status":         status,
			"lifecycleState": lifecycleState,
			"updatedAt":      shared.ParseAndFormatTime(result.Run.UpdatedAt),
		})
	case "cancel":
		shared.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Cancel action is not yet available for executor dispatch")
	case "recover":
		shared.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Recover action is not yet available for executor dispatch")
	default:
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Unknown execute action %q", req.Action))
	}
}

// POST /api/runs/{id}/validate
func (h *Handler) ValidateRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	idStr := strconv.FormatInt(id, 10)
	result, err := h.service.ValidateRun(r.Context(), id)
	if err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"runId":     idStr,
		"status":    result.ValidationStatus,
		"runStatus": result.RunStatus,
		"commands":  result.Commands,
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"progress":  result.Progress,
	})
}

// POST /api/runs/{id}/validate/accept-failure
func (h *Handler) AcceptFailedValidation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	idStr := strconv.FormatInt(id, 10)
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Reason is required and cannot be empty")
		return
	}
	if err := h.service.AcceptFailedValidation(r.Context(), id, req.Reason, h.lifecycle); err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"runId":   idStr,
		"status":  "validation_failed_accepted",
	})
}

// POST /api/runs/{id}/repair/validation
func (h *Handler) RepairValidation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseRunID(w, r)
	if !ok {
		return
	}
	result, err := h.service.RepairValidation(r.Context(), id)
	if err != nil {
		if writeRunError(w, err) {
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared.JSON(w, result.HTTPStatus, result.Body)
}
