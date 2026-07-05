package server

import (
	"log/slog"
	"net/http"

	"relay/internal/events"
	"relay/internal/repos"
	"relay/internal/store"
	workflowstore "relay/internal/store/workflow"
)

type Server struct {
	store       *store.Store
	repoService *repos.Service
	log         *slog.Logger
	mux         http.Handler
}

func New(s *store.Store, rs *repos.Service, log *slog.Logger) *Server {
	mux := BuildRoutes(s, rs, log)
	return &Server{
		store:       s,
		repoService: rs,
		log:         log,
		mux:         mux,
	}
}

func NewWithEvents(s *store.Store, rs *repos.Service, log *slog.Logger, eventHub *events.Hub, ownerInstanceID string) *Server {
	mux := BuildRoutesWithRuntime(s, rs, log, eventHub, ownerInstanceID)
	return &Server{
		store:       s,
		repoService: rs,
		log:         log,
		mux:         mux,
	}
}

func NewWithEventsAndWorkflow(s *store.Store, workflowStore *workflowstore.Store, rs *repos.Service, log *slog.Logger, eventHub *events.Hub, ownerInstanceID string) *Server {
	mux := BuildRoutesWithWorkflowRuntime(s, workflowStore, rs, log, eventHub, ownerInstanceID)
	return &Server{
		store:       s,
		repoService: rs,
		log:         log,
		mux:         mux,
	}
}

func (s *Server) Handler() http.Handler {
	return s.mux
}
