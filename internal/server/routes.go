package server

import (
	"log/slog"
	"net/http"

	"relay/internal/handlers"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildRoutes(s *store.Store, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	dashboard := handlers.NewDashboardHandler(s)
	handoffs := handlers.NewHandoffsHandler(s, log)
	runs := handlers.NewRunsHandler(s, log)
	artifactsH := handlers.NewArtifactsHandler(s)

	r.Get("/", dashboard.Get)
	r.Get("/handoffs/new", handoffs.NewForm)
	r.Post("/handoffs", handoffs.Create)

	r.Get("/runs/{id}", runs.Get)
	r.Post("/runs/{id}/actions", runs.Action)

	r.Get("/runs/{id}/artifacts/{kind}", artifactsH.View)
	r.Get("/runs/{id}/artifacts/{kind}/download", artifactsH.Download)

	// serve static assets
	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	return r
}
