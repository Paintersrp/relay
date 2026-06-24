package intake

import "github.com/go-chi/chi/v5"

// MountRoutes registers the planner-handoff intake route on r against the
// feature intake Handler.
func MountRoutes(r chi.Router, h *Handler) {
	r.Post("/intake/planner-handoff", h.IntakePlannerHandoff)
}
