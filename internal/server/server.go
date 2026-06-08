package server

import (
	"log/slog"
	"net/http"

	"relay/internal/store"
)

type Server struct {
	store *store.Store
	log   *slog.Logger
	mux   http.Handler
}

func New(s *store.Store, log *slog.Logger) *Server {
	mux := BuildRoutes(s, log)
	return &Server{
		store: s,
		log:   log,
		mux:   mux,
	}
}

func (s *Server) Handler() http.Handler {
	return s.mux
}
