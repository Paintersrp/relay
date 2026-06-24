package projects

import (
	rootapi "relay/internal/api"

	"github.com/go-chi/chi/v5"
)

// MountRoutes registers project, repository, and project-scoped refactor routes
// on r against the existing *rootapi.APIHandler methods. PASS-001 route
// composition only.
func MountRoutes(r chi.Router, h *rootapi.APIHandler) {
	r.Get("/projects", h.ListProjects)
	r.Post("/projects", h.CreateProject)
	r.Get("/projects/{projectId}", h.GetProject)
	r.Post("/projects/{projectId}/repositories", h.UpsertProjectRepository)
	r.Post("/projects/{projectId}/repositories/{repoId}/update", h.UpdateProjectRepository)
	r.Post("/projects/{projectId}/repositories/{repoId}/set-enabled", h.SetProjectRepositoryEnabled)

	r.Get("/projects/{projectId}/refactor/discovery-tasks", h.ListRefactorDiscoveryTasks)
	r.Post("/projects/{projectId}/refactor/discovery-tasks", h.CreateRefactorDiscoveryTask)
	r.Get("/projects/{projectId}/refactor/discovery-tasks/{taskId}", h.GetRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/update", h.UpdateRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/complete", h.CompleteRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/close", h.CloseRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/supersede", h.SupersedeRefactorDiscoveryTask)

	r.Get("/projects/{projectId}/refactor/candidates", h.ListRefactorCandidates)
	r.Post("/projects/{projectId}/refactor/candidates", h.CreateRefactorCandidate)
	r.Get("/projects/{projectId}/refactor/candidates/{candidateId}", h.GetRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/update", h.UpdateRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/defer", h.DeferRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/reject", h.RejectRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/supersede", h.SupersedeRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/mark-scheduled", h.MarkScheduledRefactorCandidate)
	r.Get("/projects/{projectId}/refactor/candidates/{candidateId}/placement-suggestion", h.GetRefactorCandidatePlacementSuggestion)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/promote", h.PromoteRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/plans/generate", h.GenerateRefactorOnlyPlan)
}
