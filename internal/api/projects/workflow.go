package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type WorkflowProjectService interface {
	ListProjects(context.Context, workflowprojects.ListProjectsInput) ([]workflowstore.Project, error)
	GetProject(context.Context, workflowprojects.GetProjectInput) (workflowprojects.ProjectDetail, error)
	CreateProject(context.Context, workflowprojects.CreateProjectInput) (workflowstore.Project, error)
	UpdateProject(context.Context, workflowprojects.UpdateProjectInput) (workflowstore.Project, error)
	ArchiveProject(context.Context, string) (workflowstore.Project, error)
	RestoreProject(context.Context, string) (workflowstore.Project, error)
	AttachRepository(context.Context, string, string) (workflowstore.ProjectRepositoryTarget, error)
	DetachRepository(context.Context, string, string) error
	CreateNote(context.Context, workflowprojects.CreateNoteInput) (workflowstore.ProjectNote, error)
	UpdateNote(context.Context, workflowprojects.UpdateNoteInput) (workflowstore.ProjectNote, error)
	DeleteNote(context.Context, string, string) error
}

type WorkflowHandler struct {
	service WorkflowProjectService
}

type projectResponse struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type projectRepositoryResponse struct {
	RepoTarget string `json:"repoTarget"`
	CreatedAt  string `json:"createdAt"`
}

type projectNoteResponse struct {
	NoteID    string `json:"noteId"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type projectPlanResponse struct {
	PlanID      string `json:"planId"`
	FeatureSlug string `json:"featureSlug"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type createNoteRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type updateNoteRequest struct {
	Title  *string `json:"title"`
	Body   *string `json:"body"`
	Status *string `json:"status"`
}

func NewWorkflowHandler(service WorkflowProjectService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryLimit(r, "limit")
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
		return
	}
	values, err := h.service.ListProjects(r.Context(), workflowprojects.ListProjectsInput{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  limit,
	})
	if err != nil {
		writeProjectError(w, err)
		return
	}
	items := make([]projectResponse, 0, len(values))
	for _, value := range values {
		items = append(items, projectDTO(value))
	}
	shared.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	repositoryLimit, ok := queryLimit(r, "repositoryLimit")
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid repositoryLimit")
		return
	}
	noteLimit, ok := queryLimit(r, "noteLimit")
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid noteLimit")
		return
	}
	planLimit, ok := queryLimit(r, "planLimit")
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid planLimit")
		return
	}
	detail, err := h.service.GetProject(r.Context(), workflowprojects.GetProjectInput{
		ProjectID:       projectID(r),
		RepositoryLimit: repositoryLimit,
		NoteLimit:       noteLimit,
		PlanLimit:       planLimit,
	})
	if err != nil {
		writeProjectError(w, err)
		return
	}
	repositories := make([]projectRepositoryResponse, 0, len(detail.Repositories))
	for _, value := range detail.Repositories {
		repositories = append(repositories, projectRepositoryResponse{
			RepoTarget: value.RepoTarget,
			CreatedAt:  value.CreatedAt,
		})
	}
	notes := make([]projectNoteResponse, 0, len(detail.Notes))
	for _, value := range detail.Notes {
		notes = append(notes, projectNoteDTO(value))
	}
	plans := make([]projectPlanResponse, 0, len(detail.Plans))
	for _, value := range detail.Plans {
		plans = append(plans, projectPlanResponse{
			PlanID:      value.PlanID,
			FeatureSlug: value.FeatureSlug,
			Status:      value.Status,
			CreatedAt:   value.CreatedAt,
			UpdatedAt:   value.UpdatedAt,
		})
	}
	shared.JSON(w, http.StatusOK, map[string]any{
		"project":      projectDTO(detail.Project),
		"repositories": repositories,
		"notes":        notes,
		"plans":        plans,
	})
}

func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createProjectRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Project request")
		return
	}
	value, err := h.service.CreateProject(r.Context(), workflowprojects.CreateProjectInput{
		Name:        request.Name,
		Description: request.Description,
	})
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, projectDTO(value))
}

func (h *WorkflowHandler) Update(w http.ResponseWriter, r *http.Request) {
	var request updateProjectRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Project request")
		return
	}
	value, err := h.service.UpdateProject(r.Context(), workflowprojects.UpdateProjectInput{
		ProjectID:   projectID(r),
		Name:        request.Name,
		Description: request.Description,
	})
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, projectDTO(value))
}

func (h *WorkflowHandler) Archive(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.ArchiveProject(r.Context(), projectID(r))
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, projectDTO(value))
}

func (h *WorkflowHandler) Restore(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.RestoreProject(r.Context(), projectID(r))
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, projectDTO(value))
}

func (h *WorkflowHandler) AttachRepository(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.AttachRepository(
		r.Context(),
		projectID(r),
		strings.TrimSpace(chi.URLParam(r, "repoTarget")),
	)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, projectRepositoryResponse{
		RepoTarget: value.RepoTarget,
		CreatedAt:  value.CreatedAt,
	})
}

func (h *WorkflowHandler) DetachRepository(w http.ResponseWriter, r *http.Request) {
	err := h.service.DetachRepository(
		r.Context(),
		projectID(r),
		strings.TrimSpace(chi.URLParam(r, "repoTarget")),
	)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkflowHandler) CreateNote(w http.ResponseWriter, r *http.Request) {
	var request createNoteRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Project note request")
		return
	}
	value, err := h.service.CreateNote(r.Context(), workflowprojects.CreateNoteInput{
		ProjectID: projectID(r),
		Title:     request.Title,
		Body:      request.Body,
	})
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, projectNoteDTO(value))
}

func (h *WorkflowHandler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	var request updateNoteRequest
	if err := decodeStrict(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid Project note request")
		return
	}
	value, err := h.service.UpdateNote(r.Context(), workflowprojects.UpdateNoteInput{
		ProjectID: projectID(r),
		NoteID:    strings.TrimSpace(chi.URLParam(r, "noteID")),
		Title:     request.Title,
		Body:      request.Body,
		Status:    request.Status,
	})
	if err != nil {
		writeProjectError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, projectNoteDTO(value))
}

func (h *WorkflowHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	err := h.service.DeleteNote(
		r.Context(),
		projectID(r),
		strings.TrimSpace(chi.URLParam(r, "noteID")),
	)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func projectID(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "projectID"))
}

func projectDTO(value workflowstore.Project) projectResponse {
	return projectResponse{
		ProjectID:   value.ProjectID,
		Name:        value.Name,
		Description: value.Description,
		Status:      value.Status,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func projectNoteDTO(value workflowstore.ProjectNote) projectNoteResponse {
	return projectNoteResponse{
		NoteID:    value.NoteID,
		Title:     value.Title,
		Body:      value.Body,
		Status:    value.Status,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}

func queryLimit(r *http.Request, key string) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(value)
	return limit, err == nil && limit > 0
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

func writeProjectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Project, note, or repository attachment was not found")
	case errors.Is(err, workflowprojects.ErrInvalidProjectRequest):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "unique"),
		strings.Contains(strings.ToLower(err.Error()), "constraint"):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Project operation conflicts with current state")
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Project operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/projects", handler.List)
	r.Post("/projects", handler.Create)
	r.Get("/projects/{projectID}", handler.Get)
	r.Patch("/projects/{projectID}", handler.Update)
	r.Post("/projects/{projectID}/archive", handler.Archive)
	r.Post("/projects/{projectID}/restore", handler.Restore)
	r.Put("/projects/{projectID}/repositories/{repoTarget}", handler.AttachRepository)
	r.Delete("/projects/{projectID}/repositories/{repoTarget}", handler.DetachRepository)
	r.Post("/projects/{projectID}/notes", handler.CreateNote)
	r.Patch("/projects/{projectID}/notes/{noteID}", handler.UpdateNote)
	r.Delete("/projects/{projectID}/notes/{noteID}", handler.DeleteNote)
}
