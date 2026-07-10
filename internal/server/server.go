package server

import (
	"log/slog"
	"net/http"

	workflowstore "relay/internal/store/workflow"
)

type Server struct {
	log *slog.Logger
	mux http.Handler
}

func NewWorkflow(store *workflowstore.Store, log *slog.Logger, ownerInstanceID string) *Server {
	return &Server{
		log: log,
		mux: BuildWorkflowRoutes(store, log, ownerInstanceID),
	}
}

func (s *Server) Handler() http.Handler {
	return s.mux
}
