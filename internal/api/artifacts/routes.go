package artifacts

import (
	rootapi "relay/internal/api"

	"github.com/go-chi/chi/v5"
)

// MountRoutes registers run artifact routes on r against the existing
// *rootapi.APIHandler methods. PASS-001 route composition only.
func MountRoutes(r chi.Router, h *rootapi.APIHandler) {
	r.Get("/runs/{id}/artifacts", h.ListArtifacts)
	r.Get("/runs/{id}/artifacts/{kind}", h.GetArtifactContent)
}
