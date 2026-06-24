package projects

import "github.com/go-chi/chi/v5"

// MountRoutes registers the project and project-repository JSON API routes on r
// against the project feature Handler. Project-scoped refactor routes remain on
// the legacy root API handler and are mounted by internal/server.
func MountRoutes(r chi.Router, h *Handler) {
	r.Get("/projects", h.ListProjects)
	r.Post("/projects", h.CreateProject)
	r.Get("/projects/{projectId}", h.GetProject)
	r.Post("/projects/{projectId}/repositories", h.UpsertProjectRepository)
	r.Post("/projects/{projectId}/repositories/{repoId}/update", h.UpdateProjectRepository)
	r.Post("/projects/{projectId}/repositories/{repoId}/set-enabled", h.SetProjectRepositoryEnabled)
}
