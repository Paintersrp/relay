package server

import (
	"log/slog"
	"net/http"

	"relay/internal/events"
	"relay/internal/repos"
	"relay/internal/store"
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

func (s *Server) Handler() http.Handler {
	return s.mux
}
