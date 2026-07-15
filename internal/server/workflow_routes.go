package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	artifactsapi "relay/internal/api/artifacts"
	auditsapi "relay/internal/api/audits"
	canonicalapi "relay/internal/api/canonical"
	plansapi "relay/internal/api/plans"
	projectsapi "relay/internal/api/projects"
	repositoriesapi "relay/internal/api/repositories"
	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	appaudits "relay/internal/app/audits"
	workflowplans "relay/internal/app/plans/workflow"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowsubmissions "relay/internal/app/submissions"
	workflowapp "relay/internal/app/workflow"
	"relay/internal/executor"
	"relay/internal/mcp"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildWorkflowRoutes(workflowStore *workflowstore.Store, log *slog.Logger, ownerInstanceID string) http.Handler {
	if workflowStore == nil {
		panic("workflow store is required")
	}
	if log == nil {
		log = slog.Default()
	}

	readService, err := workflowapp.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	projectService, err := workflowprojects.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	planMutationService, err := workflowplans.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	submissionService, err := workflowsubmissions.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	auditService, err := appaudits.NewWorkflowAuditService(workflowStore)
	if err != nil {
		panic(err)
	}
	executionService := executor.NewWorkflowExecutionService(workflowStore, log, ownerInstanceID)

	repositoryHandler := repositoriesapi.NewWorkflowHandler(readService, log)
	projectHandler := projectsapi.NewWorkflowHandler(projectService)
	canonicalHandler := canonicalapi.NewWorkflowHandler(submissionService, planMutationService)
	planHandler := plansapi.NewWorkflowHandler(readService)
	runHandler := runsapi.NewWorkflowReadHandler(readService)
	executionHandler := runsapi.NewWorkflowExecutionHandler(executionService)
	artifactHandler := artifactsapi.NewWorkflowHandler(readService)
	auditHandler := auditsapi.NewWorkflowHandler(auditService)

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.RealIP)

	if err := mcp.ValidateCompiledSurfaceCatalog(); err != nil {
		panic(err)
	}

	mcpServer := mcp.NewServer(log, mcp.NewWorkflowDepsFromEnv(workflowStore, log))
	router.Handle("/mcp", mcp.NewHTTPHandler(mcpServer, log))

	router.Route("/api", func(api chi.Router) {
		api.Use(shared.CORSMiddleware)
		repositoriesapi.MountWorkflowRoutes(api, repositoryHandler)
		projectsapi.MountWorkflowRoutes(api, projectHandler)
		canonicalapi.MountWorkflowRoutes(api, canonicalHandler)
		plansapi.MountWorkflowRoutes(api, planHandler)
		runsapi.MountWorkflowReadRoutes(api, runHandler)
		runsapi.MountWorkflowExecutionRoutes(api, executionHandler)
		artifactsapi.MountWorkflowRoutes(api, artifactHandler)
		auditsapi.MountWorkflowRoutes(api, auditHandler)
		api.HandleFunc("/*", workflowJSONNotFound)
	})

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, workflowWebURL("/runs"), http.StatusFound)
	})
	router.Get("/runs/{runID}", func(w http.ResponseWriter, r *http.Request) {
		runID := strings.TrimSpace(chi.URLParam(r, "runID"))
		detail, err := readService.GetRun(r.Context(), runID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		stage, err := resolveWorkflowRunStage(detail.Summary.Run.Status)
		if err != nil {
			http.Error(w, "Run state cannot be routed", http.StatusConflict)
			return
		}
		http.Redirect(
			w,
			r,
			workflowWebURL("/runs/"+url.PathEscape(runID)+"/"+stage),
			http.StatusFound,
		)
	})

	return router
}

func resolveWorkflowRunStage(status string) (string, error) {
	return workflowapp.ResolveRunStage(status)
}

func workflowWebURL(path string) string {
	base := strings.TrimSpace(os.Getenv("RELAY_WEB_BASE_URL"))
	if base == "" {
		base = "http://localhost:3000"
	}
	return strings.TrimRight(base, "/") + path
}

func workflowJSONNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"NOT_FOUND","message":"API route not found"}`))
}
