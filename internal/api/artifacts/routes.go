package artifacts

import "github.com/go-chi/chi/v5"

// MountRoutes registers run artifact routes on r against the feature artifact
// Handler.
func MountRoutes(r chi.Router, h *Handler) {
	r.Get("/runs/{id}/artifacts", h.ListArtifacts)
	r.Get("/runs/{id}/artifacts/{kind}", h.GetArtifactContent)
}
