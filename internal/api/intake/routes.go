package intake

import (
	rootapi "relay/internal/api"

	"github.com/go-chi/chi/v5"
)

// MountRoutes registers planner-handoff intake routes on r against the existing
// *rootapi.APIHandler methods. PASS-001 route composition only.
func MountRoutes(r chi.Router, h *rootapi.APIHandler) {
	r.Post("/intake/planner-handoff", h.IntakePlannerHandoff)
}
