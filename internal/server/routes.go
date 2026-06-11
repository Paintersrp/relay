package server

import (
	"log/slog"
	"net/http"

	"relay/internal/devreload"
	"relay/internal/handlers"
	"relay/internal/repos"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildRoutes(s *store.Store, rs *repos.Service, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	dashboard := handlers.NewDashboardHandler(s)
	handoffs := handlers.NewHandoffsHandler(s, log)
	runs := handlers.NewRunsHandler(s, log)
	handoffs.SetRunsHandler(runs)
	artifactsH := handlers.NewArtifactsHandler(s)

	r.Get("/", dashboard.Get)
	r.Get("/handoffs/new", handoffs.NewForm)
	r.Post("/handoffs", handoffs.Create)

	r.Get("/runs/{id}", runs.Get)
	r.Post("/runs/{id}/actions", runs.Action)
	r.Get("/runs/{id}/agent-run-monitor", runs.AgentRunMonitor)

	r.Get("/runs/{id}/artifacts/{kind}/preview", artifactsH.Preview)
	r.Get("/runs/{id}/artifacts/{kind}", artifactsH.View)
	r.Get("/runs/{id}/artifacts/{kind}/download", artifactsH.Download)

	instructionH := handlers.NewInstructionsHandler()
	r.Get("/instructions", instructionH.List)
	r.Get("/instructions/{kind}", instructionH.View)
	r.Get("/instructions/{kind}/download", instructionH.Download)

	repoSettings := handlers.NewRepoSettingsHandler(s, rs, log)
	r.Get("/settings/repos", repoSettings.Get)
	r.Post("/settings/repos/roots", repoSettings.AddRoot)
	r.Post("/settings/repos/roots/{id}/toggle", repoSettings.ToggleRoot)
	r.Post("/settings/repos/roots/{id}/delete", repoSettings.DeleteRoot)
	r.Post("/settings/repos/scan", repoSettings.Scan)

	if devreload.Enabled() {
		reloader := devreload.New(log)
		if err := reloader.Watch("web/static"); err != nil {
			log.Warn("dev reload watcher failed", "error", err)
		}
		r.Get("/dev/reload", reloader.Handler)
	}

	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	return r
}
