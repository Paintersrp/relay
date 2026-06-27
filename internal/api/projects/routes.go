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

	r.Get("/projects/{projectId}/plan-seeds", h.ListPlanSeeds)
	r.Post("/projects/{projectId}/plan-seeds", h.CreatePlanSeed)
	r.Get("/projects/{projectId}/plan-seeds/{seedId}", h.GetPlanSeed)
	r.Get("/projects/{projectId}/plan-seeds/{seedId}/planning-context", h.GetPlanSeedPlanningContext)
	r.Post("/projects/{projectId}/plan-seeds/{seedId}/plan-attempts", h.CreatePlanAttemptFromSeed)
	r.Post("/projects/{projectId}/plan-seeds/{seedId}/update", h.UpdatePlanSeed)
	r.Post("/projects/{projectId}/plan-seeds/{seedId}/defer", h.DeferPlanSeed)
	r.Post("/projects/{projectId}/plan-seeds/{seedId}/reject", h.RejectPlanSeed)
}
