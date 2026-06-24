package runs

import (
	rootapi "relay/internal/api"

	"github.com/go-chi/chi/v5"
)

// MountRoutes registers run and run-lifecycle routes on r against the existing
// *rootapi.APIHandler methods. PASS-001 route composition only.
func MountRoutes(r chi.Router, h *rootapi.APIHandler) {
	r.Get("/runs", h.ListRuns)
	r.Post("/dev/setup-smoke-validation-failure", h.SetupSmokeValidationFailure)
	r.Get("/runs/{id}", h.GetRun)
	r.Get("/runs/{id}/events", h.ListEvents)
	r.Post("/runs/{id}/approve-intake", h.ApproveIntake)
	r.Post("/runs/{id}/prepare", h.PrepareRun)
	r.Post("/runs/{id}/render-brief", h.RenderBrief)
	r.Post("/runs/{id}/approve-brief", h.ApproveBrief)
	r.Post("/runs/{id}/execute", h.ExecuteRun)
	r.Post("/runs/{id}/validate", h.ValidateRun)
	r.Post("/runs/{id}/validate/accept-failure", h.AcceptFailedValidation)
	r.Post("/runs/{id}/repair/validation", h.RepairValidation)
}
