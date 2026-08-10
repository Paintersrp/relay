// Package packages exposes the direct-domain execution-package workflow.
package packages

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	appoperations "relay/internal/app/operations"
	"relay/internal/app/packages"

	"github.com/go-chi/chi/v5"
)

type WorkflowHandler struct {
	service *appoperations.PackageWorkflowService
}

func NewWorkflowHandler(service *appoperations.PackageWorkflowService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

type artifactRequest struct {
	DisplayName    string `json:"displayName"`
	ExpectedSHA256 string `json:"expectedSha256"`
	BytesBase64    string `json:"bytesBase64"`
}

type prepareRequest struct {
	SelectionID             string           `json:"selectionId"`
	DeterministicOperations *artifactRequest `json:"deterministicOperations,omitempty"`
}

type approveRequest struct {
	ExpectedPackageSha256        string `json:"expectedPackageSha256"`
	OperatorConfirmationEvidence string `json:"operatorConfirmationEvidence"`
}

type reconcileRequest struct {
	LeaseID string `json:"leaseId"`
}

func (h *WorkflowHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	var request prepareRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid execution package preparation request")
		return
	}
	var operations *packages.ArtifactInput
	if request.DeterministicOperations != nil {
		value, decodeErr := artifactInput(*request.DeterministicOperations)
		if decodeErr != nil {
			badRequest(w, "Invalid package artifact bytes")
			return
		}
		operations = &value
	}
	input := packages.PrepareInput{SelectionID: request.SelectionID, DeterministicOperations: operations}
	result, err := h.service.Prepare(r.Context(), input)
	if err != nil {
		writePackageError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"package": result})
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.Get(r.Context(), packageID(r))
	if err != nil {
		writePackageError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"package": detail})
}

func (h *WorkflowHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var request approveRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid execution package approval request")
		return
	}
	if strings.TrimSpace(request.ExpectedPackageSha256) == "" || strings.TrimSpace(request.OperatorConfirmationEvidence) == "" {
		badRequest(w, "Expected package SHA-256 and operator confirmation evidence are required")
		return
	}
	result, err := h.service.Approve(r.Context(), packages.ApproveInput{
		PackageID:                    packageID(r),
		ExpectedPackageSha256:        request.ExpectedPackageSha256,
		OperatorConfirmationEvidence: request.OperatorConfirmationEvidence,
	})
	if err != nil {
		writePackageError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, result)
}

func (h *WorkflowHandler) GetMutationLease(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.GetMutationLease(r.Context(), runID(r))
	if err != nil {
		writePackageError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"lease": status})
}

func (h *WorkflowHandler) ReconcileMutationLease(w http.ResponseWriter, r *http.Request) {
	var request reconcileRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid mutation lease reconciliation request")
		return
	}
	if request.LeaseID == "" || strings.TrimSpace(request.LeaseID) != request.LeaseID {
		badRequest(w, "A nonblank mutation lease ID is required")
		return
	}
	updated, err := h.service.ReconcileMutationLease(r.Context(), runID(r), request.LeaseID)
	if err != nil {
		writePackageError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, updated)
}

func artifactInput(value artifactRequest) (packages.ArtifactInput, error) {
	bytes, err := base64.StdEncoding.DecodeString(value.BytesBase64)
	if err != nil {
		return packages.ArtifactInput{}, err
	}
	return packages.ArtifactInput{DisplayName: value.DisplayName, ExpectedSHA256: value.ExpectedSHA256, Bytes: bytes}, nil
}

func packageID(r *http.Request) string { return strings.TrimSpace(chi.URLParam(r, "packageID")) }
func runID(r *http.Request) string     { return strings.TrimSpace(chi.URLParam(r, "runID")) }

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

func writePackageError(w http.ResponseWriter, err error) {
	switch {
	case appoperations.IsPackageWorkflowNotFound(err):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Execution package or Run was not found")
	case errors.Is(err, appoperations.ErrNoActiveMutationLease), errors.Is(err, appoperations.ErrMutationLeaseConflict):
		shared.Error(w, http.StatusConflict, "LEASE_CONFLICT", "Mutation lease is missing, stale, or does not match the Run")
	case errors.Is(err, packages.ErrSelectionNotActive), errors.Is(err, packages.ErrSelectionInvalid):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Execution package selection is stale or unavailable")
	case errors.Is(err, packages.ErrPackageAlreadyRun), errors.Is(err, packages.ErrPackageBasisChanged):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Execution package basis is stale or already linked to a Run")
	case errors.Is(err, packages.ErrInvalidPackageInput):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Execution package operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Post("/execution-packages", handler.Prepare)
	r.Get("/execution-packages/{packageID}", handler.Get)
	r.Post("/execution-packages/{packageID}/approvals", handler.Approve)
	r.Get("/runs/{runID}/mutation-lease", handler.GetMutationLease)
	r.Post("/runs/{runID}/mutation-lease/reconcile", handler.ReconcileMutationLease)
}
