package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/projects"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

type ProjectAPIProject struct {
	ProjectID           string                 `json:"projectId"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	Status              string                 `json:"status"`
	DefaultRepositoryID string                 `json:"defaultRepositoryId"`
	CreatedAt           string                 `json:"createdAt"`
	UpdatedAt           string                 `json:"updatedAt"`
	Repositories        []ProjectAPIRepository `json:"repositories,omitempty"`
}

type ProjectAPIRepository struct {
	RepoID           string   `json:"repoId"`
	Role             string   `json:"role"`
	LocalPath        string   `json:"localPath"`
	RemoteLabel      string   `json:"remoteLabel"`
	RemoteURL        string   `json:"remoteUrl"`
	DefaultBranch    string   `json:"defaultBranch"`
	AllowedRoots     []string `json:"allowedRoots"`
	IgnoredGlobs     []string `json:"ignoredGlobs"`
	MaxFileSizeBytes int64    `json:"maxFileSizeBytes"`
	IncludeUntracked bool     `json:"includeUntracked"`
	Enabled          bool     `json:"enabled"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
}

type ProjectAPIResponse struct {
	Success      bool                              `json:"success"`
	Count        int                               `json:"count,omitempty"`
	Projects     []ProjectAPIProject               `json:"projects,omitempty"`
	Project      *ProjectAPIProject                `json:"project,omitempty"`
	Repository   *ProjectAPIRepository             `json:"repository,omitempty"`
	Repositories []ProjectAPIRepository            `json:"repositories,omitempty"`
	Validation   []projects.ProjectValidationIssue `json:"validation,omitempty"`
}

type ProjectAPIRequest struct {
	ProjectID           string `json:"project_id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Status              string `json:"status"`
	DefaultRepositoryID string `json:"default_repository_id"`
}

type ProjectRepositoryAPIRequest struct {
	RepoID           string   `json:"repo_id"`
	Role             string   `json:"role"`
	LocalPath        string   `json:"local_path"`
	RemoteLabel      string   `json:"remote_label"`
	RemoteURL        string   `json:"remote_url"`
	DefaultBranch    string   `json:"default_branch"`
	AllowedRoots     []string `json:"allowed_roots"`
	IgnoredGlobs     []string `json:"ignored_globs"`
	MaxFileSizeBytes int64    `json:"max_file_size_bytes"`
	IncludeUntracked bool     `json:"include_untracked"`
	Enabled          *bool    `json:"enabled,omitempty"`
}

type ProjectRepositoryEnabledAPIRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *APIHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	limit := int64(projects.DefaultListProjectsLimit)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
			return
		}
		limit = parsed
	}

	rows, err := h.projectService.ListProjects(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list projects")
		return
	}

	items := make([]ProjectAPIProject, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapProjectToAPI(row))
	}

	writeJSON(w, http.StatusOK, ProjectAPIResponse{
		Success:  true,
		Count:    len(items),
		Projects: items,
	})
}

func (h *APIHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req ProjectAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	project, issues, err := h.projectService.CreateProject(r.Context(), projects.ProjectInput{
		ProjectID:           req.ProjectID,
		Name:                req.Name,
		Description:         req.Description,
		Status:              req.Status,
		DefaultRepositoryID: req.DefaultRepositoryID,
	})
	if err != nil {
		if isUniqueConstraintError(err) {
			writeError(w, http.StatusConflict, "CONFLICT", "project_id already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create project")
		return
	}
	if len(issues) > 0 {
		writeProjectValidationError(w, issues)
		return
	}

	writeJSON(w, http.StatusCreated, ProjectAPIResponse{
		Success: true,
		Project: projectAPIProjectPtr(mapProjectToAPI(*project)),
	})
}

func (h *APIHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}

	project, err := h.projectService.GetProjectByProjectID(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load project")
		return
	}

	repositories, err := h.projectService.ListProjectRepositories(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load project repositories")
		return
	}

	apiProject := mapProjectToAPI(*project)
	apiProject.Repositories = mapProjectRepositoriesToAPI(repositories)

	writeJSON(w, http.StatusOK, ProjectAPIResponse{
		Success: true,
		Project: projectAPIProjectPtr(apiProject),
	})
}

func (h *APIHandler) UpsertProjectRepository(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "projectId is required")
		return
	}

	var req ProjectRepositoryAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
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

	repo, issues, err := h.projectService.UpsertProjectRepository(r.Context(), projectID, projects.ProjectRepositoryInput{
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
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to save project repository")
		return
	}
	if len(issues) > 0 {
		writeProjectValidationError(w, issues)
		return
	}

	writeJSON(w, http.StatusOK, ProjectAPIResponse{
		Success:    true,
		Repository: projectAPIRepositoryPtr(mapProjectRepositoryToAPI(*repo)),
	})
}

func (h *APIHandler) UpdateProjectRepository(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	repoID := strings.TrimSpace(chi.URLParam(r, "repoId"))
	if projectID == "" || repoID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and repoId are required")
		return
	}

	var req ProjectRepositoryAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	project, err := h.projectService.GetProjectByProjectID(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Project not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load project")
		return
	}

	existing, err := h.store.GetProjectRepositoryByRepoID(store.GetProjectRepositoryByRepoIDParams{
		ProjectRowID: project.ID,
		RepoID:       repoID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Project repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load project repository")
		return
	}

	if req.Enabled == nil {
		current := existing.Enabled == 1
		req.Enabled = &current
	}
	if strings.TrimSpace(req.RepoID) == "" {
		req.RepoID = repoID
	}

	repo, issues, err := h.projectService.UpsertProjectRepository(r.Context(), projectID, projects.ProjectRepositoryInput{
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
		Enabled:          *req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update project repository")
		return
	}
	if len(issues) > 0 {
		writeProjectValidationError(w, issues)
		return
	}

	writeJSON(w, http.StatusOK, ProjectAPIResponse{
		Success:    true,
		Repository: projectAPIRepositoryPtr(mapProjectRepositoryToAPI(*repo)),
	})
}

func (h *APIHandler) SetProjectRepositoryEnabled(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(chi.URLParam(r, "projectId"))
	repoID := strings.TrimSpace(chi.URLParam(r, "repoId"))
	if projectID == "" || repoID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "projectId and repoId are required")
		return
	}

	var req ProjectRepositoryEnabledAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	repo, err := h.projectService.SetProjectRepositoryEnabled(r.Context(), projectID, repoID, req.Enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Project or repository not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update repository enabled state")
		return
	}

	writeJSON(w, http.StatusOK, ProjectAPIResponse{
		Success:    true,
		Repository: projectAPIRepositoryPtr(mapProjectRepositoryToAPI(*repo)),
	})
}

func writeProjectValidationError(w http.ResponseWriter, issues []projects.ProjectValidationIssue) {
	writeJSON(w, http.StatusBadRequest, RelayApiErrorShape{
		Error:   "VALIDATION_ERROR",
		Message: "Project configuration validation failed",
		Details: map[string]interface{}{
			"validation": issues,
		},
	})
}

func mapProjectToAPI(project store.Project) ProjectAPIProject {
	return ProjectAPIProject{
		ProjectID:           project.ProjectID,
		Name:                project.Name,
		Description:         project.Description,
		Status:              project.Status,
		DefaultRepositoryID: project.DefaultRepositoryID,
		CreatedAt:           parseAndFormatTime(project.CreatedAt),
		UpdatedAt:           parseAndFormatTime(project.UpdatedAt),
	}
}

func mapProjectRepositoriesToAPI(rows []store.ProjectRepository) []ProjectAPIRepository {
	items := make([]ProjectAPIRepository, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapProjectRepositoryToAPI(row))
	}
	return items
}

func mapProjectRepositoryToAPI(repo store.ProjectRepository) ProjectAPIRepository {
	return ProjectAPIRepository{
		RepoID:           repo.RepoID,
		Role:             repo.Role,
		LocalPath:        repo.LocalPath,
		RemoteLabel:      repo.RemoteLabel,
		RemoteURL:        repo.RemoteUrl,
		DefaultBranch:    repo.DefaultBranch,
		AllowedRoots:     decodeJSONStringArray(repo.AllowedRootsJson),
		IgnoredGlobs:     decodeJSONStringArray(repo.IgnoredGlobsJson),
		MaxFileSizeBytes: repo.MaxFileSizeBytes,
		IncludeUntracked: repo.IncludeUntracked == 1,
		Enabled:          repo.Enabled == 1,
		CreatedAt:        parseAndFormatTime(repo.CreatedAt),
		UpdatedAt:        parseAndFormatTime(repo.UpdatedAt),
	}
}

func decodeJSONStringArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	if values == nil {
		return []string{}
	}
	return values
}

func isUniqueConstraintError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func projectAPIProjectPtr(project ProjectAPIProject) *ProjectAPIProject {
	return &project
}

func projectAPIRepositoryPtr(repo ProjectAPIRepository) *ProjectAPIRepository {
	return &repo
}
