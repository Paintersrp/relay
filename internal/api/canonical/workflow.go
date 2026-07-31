// Package canonical exposes the browser-facing canonical artifact validation
// endpoint. Legacy Plan submission and Plan mutation are retired; no route in
// this package creates or changes a Plan.
package canonical

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"relay/internal/api/shared"
	workflowsubmissions "relay/internal/app/submissions"

	"github.com/go-chi/chi/v5"
)

type WorkflowCanonicalService interface {
	ValidateArtifact(context.Context, workflowsubmissions.ValidationInput) (workflowsubmissions.ValidationResult, error)
}

type WorkflowHandler struct {
	canonical WorkflowCanonicalService
}

type browserValidationRequest struct {
	FileName         string `json:"fileName"`
	CanonicalContent string `json:"canonicalContent"`
}

func NewWorkflowHandler(canonical WorkflowCanonicalService) *WorkflowHandler {
	return &WorkflowHandler{canonical: canonical}
}

func (h *WorkflowHandler) ValidateArtifact(w http.ResponseWriter, r *http.Request) {
	var request browserValidationRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid canonical artifact request")
		return
	}
	result, err := h.canonical.ValidateArtifact(r.Context(), workflowsubmissions.ValidationInput{
		DisplayName:    request.FileName,
		CanonicalBytes: []byte(request.CanonicalContent),
	})
	if err != nil {
		writeCanonicalError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{
		"ok":          result.OK,
		"status":      result.Status,
		"kind":        result.Kind,
		"sha256":      result.SHA256,
		"diagnostics": result.Diagnostics,
		"notices":     result.Notices,
	})
}

func decodeStrict(r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeCanonicalError(w http.ResponseWriter, err error) {
	application, ok := workflowsubmissions.AsApplicationError(err)
	if !ok {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Canonical validation failed")
		return
	}
	switch application.Code {
	case workflowsubmissions.ErrorCompilerRejected:
		shared.JSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":       "COMPILER_REJECTED",
			"message":     application.Message,
			"diagnostics": application.Diagnostics,
			"notices":     application.Notices,
		})
	case workflowsubmissions.ErrorExpectedHashMismatch:
		shared.Error(w, http.StatusConflict, "HASH_MISMATCH", application.Message)
	case workflowsubmissions.ErrorInvalidExpectedHash:
		shared.Error(w, http.StatusBadRequest, "INVALID_EXPECTED_HASH", application.Message)
	case workflowsubmissions.ErrorInvalidArtifactKind:
		shared.Error(w, http.StatusBadRequest, "ARTIFACT_KIND_MISMATCH", application.Message)
	case workflowsubmissions.ErrorProjectNotFound:
		shared.Error(w, http.StatusNotFound, "PROJECT_NOT_FOUND", application.Message)
	case workflowsubmissions.ErrorProjectArchived:
		shared.Error(w, http.StatusConflict, "PROJECT_ARCHIVED", application.Message)
	case workflowsubmissions.ErrorRepositoryNotFound:
		shared.Error(w, http.StatusNotFound, "UNKNOWN_REPOSITORY", application.Message)
	case workflowsubmissions.ErrorPlanPassAssociation,
		workflowsubmissions.ErrorSelectedPassFilename,
		workflowsubmissions.ErrorRemediationAssociation:
		shared.Error(w, http.StatusBadRequest, "ASSOCIATION_INVALID", application.Message)
	case workflowsubmissions.ErrorPersistence:
		shared.Error(w, http.StatusInternalServerError, "PERSISTENCE_FAILED", application.Message)
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Canonical validation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Post("/canonical-artifacts/validate", handler.ValidateArtifact)
}
