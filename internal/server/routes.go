package server

import (
	"log/slog"
	"net/http"

	"relay/internal/api"
	"relay/internal/devreload"
	"relay/internal/events"
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

	// JSON API adapter routes
	apiH := api.NewAPIHandler(s, log)
	r.Route("/api", func(r chi.Router) {
		r.Use(api.CORSMiddleware)
		r.Get("/runs", apiH.ListRuns)
		r.Get("/runs/{id}", apiH.GetRun)
		r.Get("/runs/{id}/artifacts", apiH.ListArtifacts)
		r.Get("/runs/{id}/events", apiH.ListEvents)
		r.Post("/intake/planner-handoff", apiH.IntakePlannerHandoff)
		r.Post("/runs/{id}/approve-intake", apiH.ApproveIntake)
		r.Post("/runs/{id}/prepare", apiH.PrepareRun)
		r.Post("/runs/{id}/render-brief", apiH.RenderBrief)
		r.Post("/runs/{id}/approve-brief", apiH.ApproveBrief)
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"NOT_FOUND","message":"API route not found"}`))
		})
	})

	eventHub := events.NewHub(log)
	dashboard := handlers.NewDashboardHandler(s)
	handoffs := handlers.NewHandoffsHandler(s, log, eventHub)
	runs := handlers.NewRunsHandler(s, log, eventHub)
	handoffs.SetRunsHandler(runs)
	artifactsH := handlers.NewArtifactsHandler(s)

	r.Get("/", dashboard.Get)
	r.Get("/handoffs/new", handoffs.NewForm)
	r.Post("/handoffs", handoffs.Create)

	r.Get("/runs/{id}", runs.Get)
	r.Get("/runs/{id}/events", runs.Events)
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
