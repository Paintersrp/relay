package audits

import (
	"github.com/go-chi/chi/v5"
)

// MountRoutes registers local audit and run audit routes on r against the
// audit feature handler.
func MountRoutes(r chi.Router, h *Handler) {
	r.Post("/audits/local", h.CreateLocalAudit)
	r.Get("/audits/local/{auditId}", h.GetLocalAudit)
	r.Get("/projects/{projectId}/audits", h.ListProjectLocalAudits)

	r.Get("/runs/{id}/audit/status", h.GetAuditStatus)
	r.Post("/runs/{id}/audit", h.GenerateAudit)
	r.Post("/runs/{id}/audit/submit", h.SubmitAuditPacket)
	r.Post("/runs/{id}/audit/approve", h.ApproveAudit)
	r.Post("/runs/{id}/audit/request-revision", h.RequestAuditRevision)
	r.Post("/runs/{id}/audit/prepare-commit-message", h.PrepareCommitMessage)
	r.Post("/runs/{id}/audit/close", h.CloseRun)
}

func MountWorkflowRoutes(r chi.Router, h *WorkflowHandler) {
	r.Post("/workflow/runs/{runID}/audit/prepare", h.Prepare)
	r.Get("/workflow/runs/{runID}/audit/status", h.Status)
}
