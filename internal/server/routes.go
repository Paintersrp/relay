package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"relay/internal/api"
	artifactsapi "relay/internal/api/artifacts"
	auditsapi "relay/internal/api/audits"
	intakeapi "relay/internal/api/intake"
	plansapi "relay/internal/api/plans"
	projectsapi "relay/internal/api/projects"
	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	appplans "relay/internal/app/plans"
	appprojects "relay/internal/app/projects"
	"relay/internal/devreload"
	"relay/internal/events"
	"relay/internal/handlers"
	"relay/internal/mcp"
	"relay/internal/repos"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// webBaseURL returns the configured React workbench base URL.
// It reads RELAY_WEB_BASE_URL and falls back to http://localhost:3000.
// Trailing slashes are trimmed. The result is a fixed, server-configured
// value and never derived from user-controlled request input.
func webBaseURL() string {
	base := os.Getenv("RELAY_WEB_BASE_URL")
	if base == "" {
		base = "http://localhost:3000"
	}
	return strings.TrimRight(base, "/")
}

// webURL appends an internal workbench path to the configured base URL.
// path must begin with "/" and is a literal internal route; it is never
// derived from user input to prevent open-redirect vulnerabilities.
func webURL(path string) string {
	return webBaseURL() + path
}

// resolveRunStep maps a run status to the appropriate React workbench step path segment.
// The mapping uses canonical workflow statuses (see docs/api/frontend-api-contract.md).
// Unknown or empty statuses fall back to "intake".
func resolveRunStep(status string) string {
	switch status {
	// Intake step statuses
	case "draft", "needs_cleanup",
		"intake_received", "intake_needs_review",
		"validated", "needs_review",
		"intake_approved", "intake_rejected", "intake_blocked":
		return "intake"

	// Prepare step statuses
	case "approved_for_prepare",
		"packet_ready", "packet_validated", "packet_validation_failed",
		"repair_validated",
		"brief_ready_for_review", "brief_validation_failed":
		return "prepare"

	// Execute step statuses
	case "approved_for_executor",
		"executor_dispatched",
		"executor_running", "executor_done", "executor_blocked",
		"executor_error", "executor_cancelled",
		"agent_done", "agent_blocked", "agent_result_needs_review",
		"local_validation_running":
		return "execute"

	// Audit step statuses
	case "validation_passed", "validation_failed_accepted", "validation_failed",
		"audit_ready", "audit_ready_for_review",
		"revision_required",
		"accepted", "accepted_with_warnings",
		"completed",
		"audit_pending", "audit_generated", "audit_submitted",
		"audit_approved", "audit_approved_with_warnings",
		"audit_revision_requested", "audit_closed", "closed":
		return "audit"

	case "blocked":
		return "intake"

	default:
		return "intake"
	}
}

// mountProjectRefactorRoutes registers project-scoped refactor backlog routes
// against the legacy root API handler. PASS-002 preserves these route paths and
// their existing handlers; refactor backlog implementation is not migrated in
// this pass.
func mountProjectRefactorRoutes(r chi.Router, h *api.APIHandler) {
	r.Get("/projects/{projectId}/refactor/discovery-tasks", h.ListRefactorDiscoveryTasks)
	r.Post("/projects/{projectId}/refactor/discovery-tasks", h.CreateRefactorDiscoveryTask)
	r.Get("/projects/{projectId}/refactor/discovery-tasks/{taskId}", h.GetRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/update", h.UpdateRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/complete", h.CompleteRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/close", h.CloseRefactorDiscoveryTask)
	r.Post("/projects/{projectId}/refactor/discovery-tasks/{taskId}/supersede", h.SupersedeRefactorDiscoveryTask)

	r.Get("/projects/{projectId}/refactor/candidates", h.ListRefactorCandidates)
	r.Post("/projects/{projectId}/refactor/candidates", h.CreateRefactorCandidate)
	r.Get("/projects/{projectId}/refactor/candidates/{candidateId}", h.GetRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/update", h.UpdateRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/defer", h.DeferRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/reject", h.RejectRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/supersede", h.SupersedeRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/mark-scheduled", h.MarkScheduledRefactorCandidate)
	r.Get("/projects/{projectId}/refactor/candidates/{candidateId}/placement-suggestion", h.GetRefactorCandidatePlacementSuggestion)
	r.Post("/projects/{projectId}/refactor/candidates/{candidateId}/promote", h.PromoteRefactorCandidate)
	r.Post("/projects/{projectId}/refactor/plans/generate", h.GenerateRefactorOnlyPlan)
}

func BuildRoutes(s *store.Store, rs *repos.Service, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Expose ChatGPT-facing remote MCP endpoint.
	mcpDeps := mcp.NewDepsFromEnv(s, log)
	mcpSrv := mcp.NewServer(log, mcpDeps)
	mcpHandler := mcp.NewHTTPHandler(mcpSrv, log)
	r.Handle("/mcp", mcpHandler)

	eventHub := events.NewHub(log)

	// JSON API adapter routes
	apiH := api.NewAPIHandler(s, log, eventHub)
	projectH := projectsapi.NewHandler(appprojects.NewService(s))
	planSvc := appplans.NewService(s)
	planLifecycleSvc := appplans.NewRunLifecycleService(s)
	planWorkSvc := appplans.NewOrchestratorWorkService(s)
	planH := plansapi.NewHandler(planSvc, planLifecycleSvc, planWorkSvc, s)
	r.Route("/api", func(r chi.Router) {
		r.Use(shared.CORSMiddleware)
		runsapi.MountRoutes(r, apiH)
		artifactsapi.MountRoutes(r, apiH)
		intakeapi.MountRoutes(r, apiH)
		projectsapi.MountRoutes(r, projectH)
		mountProjectRefactorRoutes(r, apiH)
		plansapi.MountRoutes(r, planH)
		auditsapi.MountRoutes(r, apiH)
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"NOT_FOUND","message":"API route not found"}`))
		})
	})

	// Legacy handoff creation — backend creation logic preserved; success
	// redirect updated to React intake. Old templ/htmx workflow UI handlers,
	// templates, generated templ output, test files, and static workflow assets
	// have been physically removed. See Pass 14R2 handoff.
	handoffs := handlers.NewHandoffsHandler(s, log, eventHub)
	r.Post("/handoffs", handoffs.Create)

	// GET / → React /runs
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, webURL("/runs"), http.StatusFound)
	})

	// GET /handoffs/new → React /runs/new
	r.Get("/handoffs/new", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, webURL("/runs/new"), http.StatusFound)
	})

	// GET /runs/{id} → React /runs/{id}/{resolvedStep} based on run status
	r.Get("/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid run id", http.StatusBadRequest)
			return
		}
		run, err := s.GetRun(id)
		if err != nil {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		step := resolveRunStep(run.Status)
		http.Redirect(w, r, webURL(fmt.Sprintf("/runs/%d/%s", id, step)), http.StatusFound)
	})

	// GET /runs/{id}/agent-run-monitor → React /runs/{id}/execute
	r.Get("/runs/{id}/agent-run-monitor", func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		http.Redirect(w, r, webURL("/runs/"+idStr+"/execute"), http.StatusFound)
	})

	// Artifact raw view and download routes — preserved
	artifactsH := handlers.NewArtifactsHandler(s)
	r.Get("/runs/{id}/artifacts/{kind}", artifactsH.View)
	r.Get("/runs/{id}/artifacts/{kind}/download", artifactsH.Download)

	// Instruction routes — preserved
	instructionH := handlers.NewInstructionsHandler()
	r.Get("/instructions", instructionH.List)
	r.Get("/instructions/{kind}", instructionH.View)
	r.Get("/instructions/{kind}/download", instructionH.Download)

	// Repository settings routes — preserved
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
