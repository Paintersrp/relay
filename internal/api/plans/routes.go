// Package plans is the plan feature HTTP transport adapter.
//
// It owns plan and project-scoped plan work API DTOs, request/response mappers,
// HTTP handlers, and the plan route mounter. It delegates all business behavior
// to relay/internal/app/plans and must not import root relay/internal/api or
// perform plan persistence queries directly.
package plans

import "github.com/go-chi/chi/v5"

// MountRoutes registers plan and project-scoped plan work routes on r against
// the feature-local Handler.
func MountRoutes(r chi.Router, h *Handler) {
	r.Post("/plans/validate", h.ValidatePlan)
	r.Post("/plans", h.SubmitPlan)
	r.Get("/plans", h.ListPlans)
	r.Get("/plans/{planId}", h.GetPlan)
	r.Get("/plans/{planId}/passes/{passId}", h.GetPlanPass)

	r.Get("/projects/{projectId}/plans/{planId}/next-pass-work", h.GetNextPassWork)
	r.Get("/projects/{projectId}/plans/{planId}/next-audit-work", h.GetNextAuditWork)
}
