package plans

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	workflowapp "relay/internal/app/workflow"

	"github.com/go-chi/chi/v5"
)

type WorkflowPlanService interface {
	ListPlans(context.Context, workflowapp.ListPlansInput) ([]workflowapp.PlanSummary, error)
	GetPlan(context.Context, string) (workflowapp.PlanDetail, error)
	GetPlanPass(context.Context, string, string) (workflowapp.PlanPassDetail, error)
}

type WorkflowHandler struct {
	service WorkflowPlanService
}

func NewWorkflowHandler(service WorkflowPlanService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

type workflowProjectReferenceResponse struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

type workflowPlanSummaryResponse struct {
	PlanID              string                           `json:"planId"`
	Project             workflowProjectReferenceResponse `json:"project"`
	FeatureSlug         string                           `json:"featureSlug"`
	Status              string                           `json:"status"`
	CanonicalSHA256     string                           `json:"canonicalSha256"`
	CreatedAt           string                           `json:"createdAt"`
	UpdatedAt           string                           `json:"updatedAt"`
	CompletedAt         string                           `json:"completedAt,omitempty"`
	PassCount           int                              `json:"passCount"`
	CompletedPassCount  int                              `json:"completedPassCount"`
	InProgressPassCount int                              `json:"inProgressPassCount"`
	PlannedPassCount    int                              `json:"plannedPassCount"`
	CurrentPassID       string                           `json:"currentPassId,omitempty"`
}

type workflowPlanRepositoryResponse struct {
	RepoTarget         string `json:"repoTarget"`
	Branch             string `json:"branch"`
	PlanningBaseCommit string `json:"planningBaseCommit"`
	Sequence           int64  `json:"sequence"`
}

type workflowArtifactResponse struct {
	ArtifactID string `json:"artifactId"`
	OwnerType  string `json:"ownerType"`
	Kind       string `json:"kind"`
	MediaType  string `json:"mediaType"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
	ContentURL string `json:"contentUrl"`
}

type workflowRunReferenceResponse struct {
	RunID           string `json:"runId"`
	Status          string `json:"status"`
	Stage           string `json:"stage"`
	Branch          string `json:"branch"`
	BaseCommit      string `json:"baseCommit"`
	RemediatesRunID string `json:"remediatesRunId,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type workflowPassResponse struct {
	PassID      string                         `json:"passId"`
	Number      int64                          `json:"number"`
	Name        string                         `json:"name"`
	RepoTarget  string                         `json:"repoTarget"`
	Status      string                         `json:"status"`
	DependsOn   []string                       `json:"dependsOn"`
	CreatedAt   string                         `json:"createdAt"`
	UpdatedAt   string                         `json:"updatedAt"`
	StartedAt   string                         `json:"startedAt,omitempty"`
	CompletedAt string                         `json:"completedAt,omitempty"`
	Runs        []workflowRunReferenceResponse `json:"runs"`
}

func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := workflowPlanLimit(r)
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
		return
	}
	values, err := h.service.ListPlans(r.Context(), workflowapp.ListPlansInput{
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		ProjectID: strings.TrimSpace(r.URL.Query().Get("projectId")),
		Limit:     limit,
	})
	if err != nil {
		writeWorkflowPlanError(w, err)
		return
	}
	items := make([]workflowPlanSummaryResponse, 0, len(values))
	for _, value := range values {
		items = append(items, workflowPlanSummaryDTO(value))
	}
	shared.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetPlan(r.Context(), strings.TrimSpace(chi.URLParam(r, "planID")))
	if err != nil {
		writeWorkflowPlanError(w, err)
		return
	}
	passes := make([]workflowPassResponse, 0, len(detail.Passes))
	for _, pass := range detail.Passes {
		passes = append(passes, workflowPassDTO(pass))
	}
	repositories := make([]workflowPlanRepositoryResponse, 0, len(detail.Repositories))
	for _, repository := range detail.Repositories {
		repositories = append(repositories, workflowPlanRepositoryResponse{
			RepoTarget:         repository.RepoTarget,
			Branch:             repository.Branch,
			PlanningBaseCommit: repository.PlanningBaseCommit,
			Sequence:           repository.Sequence,
		})
	}
	artifacts := make([]workflowArtifactResponse, 0, len(detail.Artifacts))
	for _, artifact := range detail.Artifacts {
		artifacts = append(artifacts, workflowPlanArtifactDTO(artifact))
	}
	summary := workflowapp.PlanSummary{Plan: detail.Plan, Project: detail.Project, PassCount: len(detail.Passes)}
	for _, pass := range detail.Passes {
		switch pass.Pass.Status {
		case "completed":
			summary.CompletedPassCount++
		case "in_progress":
			summary.InProgressPassCount++
			if summary.CurrentPassID == "" {
				summary.CurrentPassID = pass.Pass.PassID
			}
		case "planned":
			summary.PlannedPassCount++
			if summary.CurrentPassID == "" {
				summary.CurrentPassID = pass.Pass.PassID
			}
		}
	}
	shared.JSON(w, http.StatusOK, map[string]any{
		"plan":         workflowPlanSummaryDTO(summary),
		"repositories": repositories,
		"passes":       passes,
		"artifacts":    artifacts,
	})
}

func (h *WorkflowHandler) GetPass(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetPlanPass(
		r.Context(),
		strings.TrimSpace(chi.URLParam(r, "planID")),
		strings.TrimSpace(chi.URLParam(r, "passID")),
	)
	if err != nil {
		writeWorkflowPlanError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, workflowPassDTO(detail))
}

func workflowPlanLimit(r *http.Request) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(value)
	return limit, err == nil && limit > 0
}

func workflowPlanSummaryDTO(value workflowapp.PlanSummary) workflowPlanSummaryResponse {
	response := workflowPlanSummaryResponse{
		PlanID: value.Plan.PlanID,
		Project: workflowProjectReferenceResponse{
			ProjectID: value.Project.ProjectID,
			Name:      value.Project.Name,
			Status:    value.Project.Status,
		},
		FeatureSlug:         value.Plan.FeatureSlug,
		Status:              value.Plan.Status,
		CanonicalSHA256:     value.Plan.CanonicalSHA256,
		CreatedAt:           value.Plan.CreatedAt,
		UpdatedAt:           value.Plan.UpdatedAt,
		PassCount:           value.PassCount,
		CompletedPassCount:  value.CompletedPassCount,
		InProgressPassCount: value.InProgressPassCount,
		PlannedPassCount:    value.PlannedPassCount,
		CurrentPassID:       value.CurrentPassID,
	}
	if value.Plan.CompletedAt.Valid {
		response.CompletedAt = value.Plan.CompletedAt.String
	}
	return response
}

func workflowPassDTO(value workflowapp.PlanPassDetail) workflowPassResponse {
	response := workflowPassResponse{
		PassID:     value.Pass.PassID,
		Number:     value.Pass.PassNumber,
		Name:       value.Pass.Name,
		RepoTarget: value.Pass.RepoTarget,
		Status:     value.Pass.Status,
		DependsOn:  append([]string(nil), value.DependsOn...),
		CreatedAt:  value.Pass.CreatedAt,
		UpdatedAt:  value.Pass.UpdatedAt,
		Runs:       make([]workflowRunReferenceResponse, 0, len(value.Runs)),
	}
	if value.Pass.StartedAt.Valid {
		response.StartedAt = value.Pass.StartedAt.String
	}
	if value.Pass.CompletedAt.Valid {
		response.CompletedAt = value.Pass.CompletedAt.String
	}
	for _, run := range value.Runs {
		response.Runs = append(response.Runs, workflowRunReferenceResponse{
			RunID:           run.Run.RunID,
			Status:          run.Run.Status,
			Stage:           run.Stage,
			Branch:          run.Run.Branch,
			BaseCommit:      run.Run.BaseCommit,
			RemediatesRunID: run.RemediatesRunID,
			CreatedAt:       run.Run.CreatedAt,
			UpdatedAt:       run.Run.UpdatedAt,
		})
	}
	return response
}

func workflowPlanArtifactDTO(value workflowapp.ArtifactMetadata) workflowArtifactResponse {
	return workflowArtifactResponse{
		ArtifactID: value.ArtifactID,
		OwnerType:  value.OwnerType,
		Kind:       value.Kind,
		MediaType:  value.MediaType,
		SHA256:     value.SHA256,
		SizeBytes:  value.SizeBytes,
		CreatedAt:  value.CreatedAt,
		ContentURL: "/api/artifacts/" + value.ArtifactID + "/content",
	}
}

func writeWorkflowPlanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Plan or pass was not found")
	case errors.Is(err, workflowapp.ErrInvalidWorkflowRequest):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Plan operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/plans", handler.List)
	r.Get("/plans/{planID}", handler.Get)
	r.Get("/plans/{planID}/passes/{passID}", handler.GetPass)
}
