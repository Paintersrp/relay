package plans

import (
	rootapi "relay/internal/api"

	"github.com/go-chi/chi/v5"
)

// MountRoutes registers plan and project-scoped plan work routes on r against
// the existing *rootapi.APIHandler methods. PASS-001 route composition only.
func MountRoutes(r chi.Router, h *rootapi.APIHandler) {
	r.Post("/plans/validate", h.ValidatePlan)
	r.Post("/plans", h.SubmitPlan)
	r.Get("/plans", h.ListPlans)
	r.Get("/plans/{planId}", h.GetPlan)
	r.Get("/plans/{planId}/passes/{passId}", h.GetPlanPass)

	r.Get("/projects/{projectId}/plans/{planId}/next-pass-work", h.GetNextPassWork)
	r.Get("/projects/{projectId}/plans/{planId}/next-audit-work", h.GetNextAuditWork)
}
