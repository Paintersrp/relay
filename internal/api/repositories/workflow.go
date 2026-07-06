package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	workflowapp "relay/internal/app/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type WorkflowRepositoryService interface {
	ListRepositories(context.Context) ([]workflowstore.RepositoryTarget, error)
	GetRepository(context.Context, string) (workflowstore.RepositoryTarget, error)
	RegisterRepository(context.Context, string, string) (workflowstore.RepositoryTarget, error)
}

type WorkflowHandler struct {
	service WorkflowRepositoryService
}

func NewWorkflowHandler(service WorkflowRepositoryService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

type repositoryResponse struct {
	RepoTarget string `json:"repoTarget"`
	LocalPath  string `json:"localPath"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type createRepositoryRequest struct {
	RepoTarget string `json:"repoTarget"`
	LocalPath  string `json:"localPath"`
}

func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.ListRepositories(r.Context())
	if err != nil {
		writeWorkflowRepositoryError(w, err)
		return
	}
	items := make([]repositoryResponse, 0, len(values))
	for _, value := range values {
		items = append(items, repositoryDTO(value))
	}
	shared.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetRepository(r.Context(), strings.TrimSpace(chi.URLParam(r, "repoTarget")))
	if err != nil {
		writeWorkflowRepositoryError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, repositoryDTO(value))
}

func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createRepositoryRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid repository request")
		return
	}
	value, err := h.service.RegisterRepository(r.Context(), request.RepoTarget, request.LocalPath)
	if err != nil {
		writeWorkflowRepositoryError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, repositoryDTO(value))
}

func repositoryDTO(value workflowstore.RepositoryTarget) repositoryResponse {
	return repositoryResponse{
		RepoTarget: value.RepoTarget,
		LocalPath:  value.LocalPath,
		CreatedAt:  value.CreatedAt,
		UpdatedAt:  value.UpdatedAt,
	}
}

func writeWorkflowRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Repository target was not found")
	case errors.Is(err, workflowapp.ErrInvalidWorkflowRequest),
		strings.Contains(err.Error(), "repository target"),
		strings.Contains(err.Error(), "repository path"):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "unique"),
		strings.Contains(strings.ToLower(err.Error()), "constraint"):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Repository target already exists")
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Repository operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/repositories", handler.List)
	r.Post("/repositories", handler.Create)
	r.Get("/repositories/{repoTarget}", handler.Get)
}
