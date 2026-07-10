package canonical

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"relay/internal/api/shared"
	workflowplans "relay/internal/app/plans/workflow"
	workflowsubmissions "relay/internal/app/submissions"

	"github.com/go-chi/chi/v5"
)

type WorkflowCanonicalService interface {
	ValidateArtifact(context.Context, workflowsubmissions.ValidationInput) (workflowsubmissions.ValidationResult, error)
	SubmitPlan(context.Context, workflowsubmissions.SubmitPlanInput) (workflowsubmissions.SubmitPlanResult, error)
	CreateRun(context.Context, workflowsubmissions.CreateRunInput) (workflowsubmissions.CreateRunResult, error)
}

type WorkflowPlanMover interface {
	MovePlan(context.Context, workflowplans.MovePlanInput) (workflowplans.MovePlanResult, error)
}

type WorkflowHandler struct {
	canonical WorkflowCanonicalService
	plans     WorkflowPlanMover
}

type browserValidationRequest struct {
	FileName         string `json:"fileName"`
	CanonicalContent string `json:"canonicalContent"`
}

type submitPlanRequest struct {
	ProjectID        string `json:"projectId"`
	FileName         string `json:"fileName"`
	CanonicalContent string `json:"canonicalContent"`
	ExpectedSHA256   string `json:"expectedSha256"`
}

type movePlanRequest struct {
	ProjectID string `json:"projectId"`
}

type createRunRequest struct {
	FileName         string `json:"fileName"`
	CanonicalContent string `json:"canonicalContent"`
	ExpectedSHA256   string `json:"expectedSha256"`
	PlanID           string `json:"planId,omitempty"`
	PassNumber       int64  `json:"passNumber,omitempty"`
	RemediatesRunID  string `json:"remediatesRunId,omitempty"`
}

type projectReferenceResponse struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

type planResponse struct {
	PlanID          string                   `json:"planId"`
	FeatureSlug     string                   `json:"featureSlug"`
	Status          string                   `json:"status"`
	CanonicalSHA256 string                   `json:"canonicalSha256"`
	Project         projectReferenceResponse `json:"project"`
	CreatedAt       string                   `json:"createdAt"`
	UpdatedAt       string                   `json:"updatedAt"`
}

type passResponse struct {
	PassID     string `json:"passId"`
	Number     int64  `json:"number"`
	Name       string `json:"name"`
	RepoTarget string `json:"repoTarget"`
	Status     string `json:"status"`
}

type artifactResponse struct {
	ArtifactID string `json:"artifactId"`
	OwnerType  string `json:"ownerType"`
	Kind       string `json:"kind"`
	MediaType  string `json:"mediaType"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
	ContentURL string `json:"contentUrl"`
}

type runResponse struct {
	RunID           string `json:"runId"`
	FeatureSlug     string `json:"featureSlug"`
	RepoTarget      string `json:"repoTarget"`
	Status          string `json:"status"`
	Branch          string `json:"branch"`
	BaseCommit      string `json:"baseCommit"`
	CanonicalSHA256 string `json:"canonicalSha256"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	ReviewURL       string `json:"reviewUrl"`
}

func NewWorkflowHandler(canonical WorkflowCanonicalService, plans WorkflowPlanMover) *WorkflowHandler {
	return &WorkflowHandler{canonical: canonical, plans: plans}
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

func (h *WorkflowHandler) SubmitPlan(w http.ResponseWriter, r *http.Request) {
	var request submitPlanRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Plan submission request")
		return
	}
	result, err := h.canonical.SubmitPlan(r.Context(), workflowsubmissions.SubmitPlanInput{
		ProjectID:      request.ProjectID,
		DisplayName:    request.FileName,
		ExpectedSHA256: request.ExpectedSHA256,
		CanonicalBytes: []byte(request.CanonicalContent),
	})
	if err != nil {
		writeCanonicalError(w, err)
		return
	}
	passes := make([]passResponse, 0, len(result.Passes))
	for _, value := range result.Passes {
		passes = append(passes, passDTO(value))
	}
	artifacts := make([]artifactResponse, 0, len(result.Artifacts))
	for _, value := range result.Artifacts {
		artifacts = append(artifacts, artifactDTO(value))
	}
	shared.JSON(w, http.StatusCreated, map[string]any{
		"plan":      planDTO(result.Plan, result.Project),
		"passes":    passes,
		"artifacts": artifacts,
	})
}

func (h *WorkflowHandler) MovePlan(w http.ResponseWriter, r *http.Request) {
	var request movePlanRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Plan move request")
		return
	}
	result, err := h.plans.MovePlan(r.Context(), workflowplans.MovePlanInput{
		PlanID:    chi.URLParam(r, "planID"),
		ProjectID: request.ProjectID,
	})
	if err != nil {
		writePlanMoveError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, planDTO(result.Plan, result.Project))
}

func (h *WorkflowHandler) CreateRun(w http.ResponseWriter, r *http.Request) {
	var request createRunRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Run creation request")
		return
	}
	result, err := h.canonical.CreateRun(r.Context(), workflowsubmissions.CreateRunInput{
		DisplayName:     request.FileName,
		ExpectedSHA256:  request.ExpectedSHA256,
		CanonicalBytes:  []byte(request.CanonicalContent),
		PlanID:          request.PlanID,
		PassNumber:      request.PassNumber,
		RemediatesRunID: request.RemediatesRunID,
	})
	if err != nil {
		writeCanonicalError(w, err)
		return
	}
	artifacts := make([]artifactResponse, 0, len(result.Artifacts))
	for _, value := range result.Artifacts {
		artifacts = append(artifacts, artifactDTO(value))
	}
	response := runResponse{
		RunID:           result.Run.RunID,
		FeatureSlug:     result.Run.FeatureSlug,
		RepoTarget:      result.Run.RepoTarget,
		Status:          result.Run.Status,
		Branch:          result.Run.Branch,
		BaseCommit:      result.Run.BaseCommit,
		CanonicalSHA256: result.Run.CanonicalSHA256,
		CreatedAt:       result.Run.CreatedAt,
		UpdatedAt:       result.Run.UpdatedAt,
		ReviewURL:       runReviewURL(result.Run.RunID),
	}
	shared.JSON(w, http.StatusCreated, map[string]any{
		"run":       response,
		"artifacts": artifacts,
	})
}

func planDTO(plan workflowsubmissions.Plan, project workflowsubmissions.Project) planResponse {
	return planResponse{
		PlanID:          plan.PlanID,
		FeatureSlug:     plan.FeatureSlug,
		Status:          plan.Status,
		CanonicalSHA256: plan.CanonicalSHA256,
		Project: projectReferenceResponse{
			ProjectID: project.ProjectID,
			Name:      project.Name,
			Status:    project.Status,
		},
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	}
}

func passDTO(value workflowsubmissions.PlanPass) passResponse {
	return passResponse{
		PassID:     value.PassID,
		Number:     value.PassNumber,
		Name:       value.Name,
		RepoTarget: value.RepoTarget,
		Status:     value.Status,
	}
}

func artifactDTO(value workflowsubmissions.Artifact) artifactResponse {
	return artifactResponse{
		ArtifactID: value.ArtifactID,
		OwnerType:  value.OwnerType,
		Kind:       value.Kind,
		MediaType:  value.MediaType,
		SHA256:     value.SHA256,
		SizeBytes:  value.SizeBytes,
		CreatedAt:  value.CreatedAt,
		ContentURL: "/api/artifacts/" + url.PathEscape(value.ArtifactID) + "/content",
	}
}

func runReviewURL(runID string) string {
	base := strings.TrimSpace(os.Getenv("RELAY_WEB_BASE_URL"))
	if base == "" {
		base = "http://localhost:3000"
	}
	return strings.TrimRight(base, "/") + "/runs/" + url.PathEscape(runID) + "/specification"
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
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Canonical submission failed")
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
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Canonical submission failed")
	}
}

func writePlanMoveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflowplans.ErrProjectNotFound):
		shared.Error(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "Destination Project was not found")
	case errors.Is(err, workflowplans.ErrProjectArchived):
		shared.Error(w, http.StatusConflict, "PROJECT_ARCHIVED", "Only active Projects may receive Plans")
	case errors.Is(err, workflowplans.ErrPlanNotFound):
		shared.Error(w, http.StatusNotFound, "PLAN_NOT_FOUND", "Plan was not found")
	default:
		shared.Error(w, http.StatusInternalServerError, "PERSISTENCE_FAILED", "Plan move failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Post("/canonical-artifacts/validate", handler.ValidateArtifact)
	r.Post("/plans", handler.SubmitPlan)
	r.Patch("/plans/{planID}/project", handler.MovePlan)
	r.Post("/runs", handler.CreateRun)
}
