package audits

import "github.com/go-chi/chi/v5"

func MountWorkflowRoutes(r chi.Router, h *WorkflowHandler) {
	r.Post("/runs/{runID}/audit/prepare", h.Prepare)
	r.Get("/runs/{runID}/audit/status", h.Status)
	r.Get("/runs/{runID}/audit/packet", h.Packet)
	r.Post("/runs/{runID}/audit/decision", h.RecordDecision)
}
