package cutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relay/internal/api/shared"
	appcutover "relay/internal/app/cutover"
)

type ReadService interface {
	State(ctx context.Context) (*appcutover.State, bool, error)
	Readiness(ctx context.Context, activationID string) (*appcutover.Readiness, error)
	History(ctx context.Context) ([]appcutover.State, error)
}

type WorkflowService interface {
	Prepare(ctx context.Context, request appcutover.PrepareRequest) (*appcutover.State, error)
	Activate(ctx context.Context, request appcutover.ActivationRequest) (*appcutover.State, error)
	Rollback(ctx context.Context, request appcutover.RollbackRequest) (*appcutover.State, error)
	RecordRollForwardEvidence(ctx context.Context, request appcutover.RollForwardEvidenceRequest) error
}

type WorkflowHandler struct {
	read      ReadService
	mutations WorkflowService
}

func NewWorkflowHandler(read ReadService, mutations WorkflowService) *WorkflowHandler {
	return &WorkflowHandler{read: read, mutations: mutations}
}

func (h *WorkflowHandler) State(w http.ResponseWriter, r *http.Request) {
	state, found, err := h.read.State(r.Context())
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	if !found {
		shared.JSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"active": true, "state": state})
}

func (h *WorkflowHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	activationID := strings.TrimSpace(chi.URLParam(r, "activationID"))
	readiness, err := h.read.Readiness(r.Context(), activationID)
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"readiness": readiness})
}

func (h *WorkflowHandler) History(w http.ResponseWriter, r *http.Request) {
	values, err := h.read.History(r.Context())
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"items": values, "count": len(values)})
}

func (h *WorkflowHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	var request appcutover.PrepareRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid cutover preparation request")
		return
	}
	state, err := h.mutations.Prepare(r.Context(), request)
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"activation": state})
}

type activateRequest struct {
	ActivationID string `json:"activationId"`
}

func (h *WorkflowHandler) Activate(w http.ResponseWriter, r *http.Request) {
	var request activateRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid cutover activation request")
		return
	}
	state, err := h.mutations.Activate(r.Context(), appcutover.ActivationRequest{ActivationID: request.ActivationID})
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"activation": state})
}

type rollbackRequest struct {
	ActivationID string `json:"activationId"`
}

func (h *WorkflowHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	var request rollbackRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid cutover rollback request")
		return
	}
	state, err := h.mutations.Rollback(r.Context(), appcutover.RollbackRequest{ActivationID: request.ActivationID})
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"activation": state})
}

type rollForwardEvidenceRequest struct {
	ActivationID      string `json:"activationId"`
	CriterionSequence int64  `json:"criterionSequence"`
	Evidence          string `json:"evidence"`
}

func (h *WorkflowHandler) RollForwardEvidence(w http.ResponseWriter, r *http.Request) {
	var request rollForwardEvidenceRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid cutover roll-forward evidence request")
		return
	}
	err := h.mutations.RecordRollForwardEvidence(r.Context(), appcutover.RollForwardEvidenceRequest{
		ActivationID:      request.ActivationID,
		CriterionSequence: request.CriterionSequence,
		Evidence:          request.Evidence,
	})
	if err != nil {
		writeCutoverError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"recorded": true})
}

func decodeStrict(r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		return false
	}
	var extra any
	return errors.Is(decoder.Decode(&extra), io.EOF)
}

func badRequest(w http.ResponseWriter, message string) {
	shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", message)
}

func writeCutoverError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, appcutover.ErrCutoverNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Cutover activation was not found")
	case errors.Is(err, appcutover.ErrCutoverConfigurationInvalid):
		shared.Error(w, http.StatusBadRequest, "CUTOVER_CONFIGURATION_INVALID", "Cutover gateway configuration is invalid")
	case errors.Is(err, appcutover.ErrCutoverConfigurationMismatch):
		shared.Error(w, http.StatusConflict, "CUTOVER_CONFIGURATION_MISMATCH", "Persisted cutover gateway configuration failed integrity validation")
	case errors.Is(err, appcutover.ErrCutoverNotReady):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Cutover activation is not in prepared ready state")
	case errors.Is(err, appcutover.ErrCutoverAlreadyActive):
		shared.Error(w, http.StatusConflict, "CONFLICT", "A cutover activation is already active")
	case errors.Is(err, appcutover.ErrCutoverRollbackBlocked):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Cutover rollback is blocked after first new execution")
	case errors.Is(err, appcutover.ErrCutoverBoundaryQualification):
		shared.Error(w, http.StatusConflict, "CUTOVER_BOUNDARY_QUALIFICATION_FAILED", "Run does not qualify for cutover boundary crossing")
	case errors.Is(err, appcutover.ErrLegacyAdmissionClosed):
		shared.Error(w, http.StatusConflict, "LEGACY_ADMISSION_CLOSED", "Legacy admission is closed")
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Cutover operation failed")
	}
}
