package projects

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	appprojects "relay/internal/app/projects"

	"github.com/go-chi/chi/v5"
)

// Handler is the project feature HTTP transport adapter. It owns request/response
// DTO decoding and mapping and delegates all business behavior to the project
// app service. It must not call store methods directly.
type Handler struct {
	service *appprojects.Service
}

func NewHandler(service *appprojects.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	limit := int64(appprojects.DefaultListProjectsLimit)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
			return
		}
		limit = parsed
	}

	rows, err := h.service.ListProjects(r.Context(), limit)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list projects")
		return
	}

	items := make([]ProjectAPIProject, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapProjectToAPI(row))
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:  true,
		Count:    len(items),
		Projects: items,
	})
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req ProjectAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	project, issues, err := h.service.CreateProject(r.Context(), appprojects.ProjectInput{
		ProjectID:           req.ProjectID,
		Name:                req.Name,
		Description:         req.Description,
		Status:              req.Status,
		DefaultRepositoryID: req.DefaultRepositoryID,
	})
	if err != nil {
		if isUniqueConstraintError(err) {
			shared.Error(w, http.StatusConflict, "CONFLICT", "project_id already exists")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create project")
		return
	}
	if len(issues) > 0 {
		writeProjectValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusCreated, ProjectAPIResponse{
		Success: true,
		Project: projectAPIProjectPtr(mapProjectToAPI(*project)),
	})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}

	project, err := h.service.GetProjectByProjectID(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load project")
		return
	}

	repositories, err := h.service.ListProjectRepositories(r.Context(), projectID)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load project repositories")
		return
	}

	apiProject := mapProjectToAPI(*project)
	apiProject.Repositories = mapProjectRepositoriesToAPI(repositories)

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success: true,
		Project: projectAPIProjectPtr(apiProject),
	})
}

func (h *Handler) UpsertProjectRepository(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}

	var req ProjectRepositoryAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	repoID := strings.TrimSpace(req.RepoID)
	if repoID == "" {
		repoID = strings.TrimSpace(chi.URLParam(r, "repoId"))
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	repo, issues, err := h.service.UpsertProjectRepository(r.Context(), projectID, appprojects.ProjectRepositoryInput{
		ProjectID:        projectID,
		RepoID:           repoID,
		Role:             req.Role,
		LocalPath:        req.LocalPath,
		RemoteLabel:      req.RemoteLabel,
		RemoteURL:        req.RemoteURL,
		DefaultBranch:    req.DefaultBranch,
		AllowedRoots:     req.AllowedRoots,
		IgnoredGlobs:     req.IgnoredGlobs,
		MaxFileSizeBytes: req.MaxFileSizeBytes,
		IncludeUntracked: req.IncludeUntracked,
		Enabled:          enabled,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to save project repository")
		return
	}
	if len(issues) > 0 {
		writeProjectValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:    true,
		Repository: projectAPIRepositoryPtr(mapProjectRepositoryToAPI(*repo)),
	})
}

func (h *Handler) UpdateProjectRepository(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	repoID := strings.TrimSpace(chi.URLParam(r, "repoId"))
	if projectID == "" || repoID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and repoId are required")
		return
	}

	var req ProjectRepositoryAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	repo, issues, err := h.service.UpdateProjectRepository(r.Context(), projectID, repoID, appprojects.ProjectRepositoryInput{
		ProjectID:        projectID,
		RepoID:           req.RepoID,
		Role:             req.Role,
		LocalPath:        req.LocalPath,
		RemoteLabel:      req.RemoteLabel,
		RemoteURL:        req.RemoteURL,
		DefaultBranch:    req.DefaultBranch,
		AllowedRoots:     req.AllowedRoots,
		IgnoredGlobs:     req.IgnoredGlobs,
		MaxFileSizeBytes: req.MaxFileSizeBytes,
		IncludeUntracked: req.IncludeUntracked,
		Enabled:          req.Enabled != nil && *req.Enabled,
	}, req.Enabled != nil)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or repository not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update project repository")
		return
	}
	if len(issues) > 0 {
		writeProjectValidationError(w, issues)
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:    true,
		Repository: projectAPIRepositoryPtr(mapProjectRepositoryToAPI(*repo)),
	})
}

func (h *Handler) SetProjectRepositoryEnabled(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	repoID := strings.TrimSpace(chi.URLParam(r, "repoId"))
	if projectID == "" || repoID == "" {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and repoId are required")
		return
	}

	var req ProjectRepositoryEnabledAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	repo, err := h.service.SetProjectRepositoryEnabled(r.Context(), projectID, repoID, req.Enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project or repository not found")
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update repository enabled state")
		return
	}

	shared.JSON(w, http.StatusOK, ProjectAPIResponse{
		Success:    true,
		Repository: projectAPIRepositoryPtr(mapProjectRepositoryToAPI(*repo)),
	})
}

func writeProjectValidationError(w http.ResponseWriter, issues []appprojects.ProjectValidationIssue) {
	shared.JSON(w, http.StatusBadRequest, shared.ErrorShape{
		Error:   "VALIDATION_ERROR",
		Message: "Project configuration validation failed",
		Details: map[string]interface{}{
			"validation": issues,
		},
	})
}
