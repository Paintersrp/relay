// Package packages exposes the packet-admitted execution-package workflow.
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
	"relay/internal/operations/registry"

	"github.com/go-chi/chi/v5"
)

type WorkflowHandler struct {
	service *appoperations.PackageWorkflowService
}

func NewWorkflowHandler(service *appoperations.PackageWorkflowService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

type dependencyRequest struct {
	Class string `json:"class"`
	Key   string `json:"key"`
}

type admissionRequest struct {
	PacketID             string              `json:"packetId"`
	OperationID          string              `json:"operationId"`
	RequiredDependencies []dependencyRequest `json:"requiredDependencies"`
}

type artifactRequest struct {
	DisplayName    string `json:"displayName"`
	ExpectedSHA256 string `json:"expectedSha256"`
	BytesBase64    string `json:"bytesBase64"`
}

type prepareRequest struct {
	admissionRequest
	SelectionID        string            `json:"selectionId"`
	TicketDesignBriefs []artifactRequest `json:"ticketDesignBriefs"`
	ExecutionSpec      artifactRequest   `json:"executionSpec"`
}

type approveRequest struct {
	admissionRequest
	ExpectedPackageSha256       string `json:"expectedPackageSha256"`
	OperatorConfirmationEvidence string `json:"operatorConfirmationEvidence"`
}

type reconcileRequest struct{ admissionRequest }

func (h *WorkflowHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	var request prepareRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid execution package preparation request")
		return
	}
	briefs := make([]packages.ArtifactInput, 0, len(request.TicketDesignBriefs))
	for _, brief := range request.TicketDesignBriefs {
		value, err := artifactInput(brief)
		if err != nil {
			badRequest(w, "Invalid package artifact bytes")
			return
		}
		briefs = append(briefs, value)
	}
	spec, err := artifactInput(request.ExecutionSpec)
	if err != nil {
		badRequest(w, "Invalid execution package bytes")
		return
	}
	input := packages.PrepareInput{SelectionID: request.SelectionID, TicketDesignBriefs: briefs, ExecutionSpec: spec}
	payload, err := appoperations.PackagePreparePayloadSHA256(input)
	if err != nil {
		badRequest(w, "Invalid execution package preparation request")
		return
	}
	result, err := h.service.Prepare(r.Context(), appoperations.PackagePrepareOperationInput{
		Admission: admission(request.admissionRequest, appoperations.PackageOperationRequest{Action: "prepare_execution_package", SelectionID: request.SelectionID, PayloadSHA256: payload}),
		Prepare:   input,
	})
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
	payload, err := appoperations.PackageApprovePayloadSHA256(packageID(r), request.ExpectedPackageSha256, request.OperatorConfirmationEvidence)
	if err != nil {
		badRequest(w, "Invalid execution package approval request")
		return
	}
	result, err := h.service.Approve(r.Context(), appoperations.PackageApproveOperationInput{
		Admission: admission(request.admissionRequest, appoperations.PackageOperationRequest{
			Action:                       "approve_execution_package",
			PackageID:                    packageID(r),
			PayloadSHA256:                payload,
			ExpectedPackageSha256:        request.ExpectedPackageSha256,
			OperatorConfirmationEvidence: request.OperatorConfirmationEvidence,
		}),
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
	status, err := h.service.GetMutationLease(r.Context(), runID(r))
	if err != nil {
		writePackageError(w, err)
		return
	}
	if status == nil {
		shared.Error(w, http.StatusConflict, "LEASE_CONFLICT", "No active mutation lease is available for reconciliation")
		return
	}
	payload, err := appoperations.MutationLeaseReconcilePayloadSHA256(status.OwnerRunID, status.LeaseID)
	if err != nil {
		badRequest(w, "Invalid mutation lease reconciliation request")
		return
	}
	updated, err := h.service.ReconcileMutationLease(r.Context(), appoperations.MutationLeaseReconcileOperationInput{
		Admission: admission(request.admissionRequest, appoperations.PackageOperationRequest{Action: "reconcile_mutation_lease", RunID: status.OwnerRunID, LeaseID: status.LeaseID, PayloadSHA256: payload}),
	})
	if err != nil {
		writePackageError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, updated)
}

func admission(value admissionRequest, base appoperations.PackageOperationRequest) appoperations.PackageOperationRequest {
	dependencies := make([]appoperations.DependencyRequirement, 0, len(value.RequiredDependencies))
	for _, dependency := range value.RequiredDependencies {
		dependencies = append(dependencies, appoperations.DependencyRequirement{Class: dependency.Class, Key: dependency.Key})
	}
	base.PacketID = value.PacketID
	base.OperationID = registry.LocalOperatorTicketWorkflowOperationID
	if value.OperationID != "" {
		base.OperationID = registry.OperationID(value.OperationID)
	}
	base.RequiredDependencies = dependencies
	return base
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
	case appoperations.ErrorCode(err) != "" && appoperations.ErrorCode(err) != appoperations.CodeInternalFailure:
		shared.Error(w, http.StatusConflict, "CONFLICT", "Operation packet is stale, unavailable, or does not authorize this package action")
	case errors.Is(err, appoperations.ErrPackageAdmission), errors.Is(err, packages.ErrPackageAlreadyRun), errors.Is(err, packages.ErrPackageBasisChanged):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Execution package basis, Run link, or packet admission is stale")
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
