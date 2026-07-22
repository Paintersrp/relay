package cutover

import "github.com/go-chi/chi/v5"

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/cutover/state", handler.State)
	r.Get("/cutover/activations/{activationID}/readiness", handler.Readiness)
	r.Get("/cutover/history", handler.History)
	r.Post("/cutover/prepare", handler.Prepare)
	r.Post("/cutover/activate", handler.Activate)
	r.Post("/cutover/rollback", handler.Rollback)
	r.Post("/cutover/roll-forward-evidence", handler.RollForwardEvidence)
}
