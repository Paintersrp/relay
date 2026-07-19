package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	artifactsapi "relay/internal/api/artifacts"
	auditsapi "relay/internal/api/audits"
	canonicalapi "relay/internal/api/canonical"
	cutoverapi "relay/internal/api/cutover"
	featuresapi "relay/internal/api/features"
	packagesapi "relay/internal/api/packages"
	plansapi "relay/internal/api/plans"
	projectsapi "relay/internal/api/projects"
	repositoriesapi "relay/internal/api/repositories"
	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	ticketsapi "relay/internal/api/tickets"
	appaudits "relay/internal/app/audits"
	appcutover "relay/internal/app/cutover"
	appfeatures "relay/internal/app/features"
	appoperations "relay/internal/app/operations"
	apppackages "relay/internal/app/packages"
	workflowplans "relay/internal/app/plans/workflow"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowsubmissions "relay/internal/app/submissions"
	apptickets "relay/internal/app/tickets"
	appwayfinder "relay/internal/app/wayfinder"
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
	wayfinderService, err := appwayfinder.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	featureAuthorityService, err := appfeatures.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	executionService := executor.NewWorkflowExecutionService(workflowStore, log, ownerInstanceID)
	ticketService, err := apptickets.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	packetService, err := appoperations.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	ticketWorkflowService, err := appoperations.NewTicketWorkflowService(packetService, ticketService)
	if err != nil {
		panic(err)
	}
	featureCompletionWorkflowService, err := appoperations.NewFeatureCompletionWorkflowService(packetService, featureAuthorityService)
	if err != nil {
		panic(err)
	}
	packageService, err := apppackages.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	packageWorkflowService, err := appoperations.NewPackageWorkflowService(packetService, packageService, executionService, workflowStore)
	if err != nil {
		panic(err)
	}

	repositoryHandler := repositoriesapi.NewWorkflowHandler(readService, log)
	projectHandler := projectsapi.NewWorkflowHandler(projectService)
	canonicalHandler := canonicalapi.NewWorkflowHandler(submissionService, planMutationService)
	planHandler := plansapi.NewWorkflowHandler(readService)
	runHandler := runsapi.NewWorkflowReadHandler(readService)
	executionHandler := runsapi.NewWorkflowExecutionHandler(executionService)
	artifactHandler := artifactsapi.NewWorkflowHandler(readService)
	auditHandler := auditsapi.NewWorkflowHandler(auditService)
	featureWorkspaceHandler := featuresapi.NewWorkspaceHandlerFromServices(wayfinderService, featureAuthorityService, featureCompletionWorkflowService)
	ticketHandler := ticketsapi.NewWorkflowHandlerFromServices(ticketWorkflowService, ticketReadService{service: ticketService, store: workflowStore})
	packageHandler := packagesapi.NewWorkflowHandler(packageWorkflowService)
	cutoverService, err := appcutover.NewService(workflowStore)
	if err != nil {
		panic(err)
	}
	cutoverWorkflowService, err := appoperations.NewCutoverWorkflowService(packetService, cutoverService)
	if err != nil {
		panic(err)
	}
	cutoverHandler := cutoverapi.NewWorkflowHandler(cutoverService, cutoverWorkflowService)

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
		featuresapi.MountWorkspaceRoutes(api, featureWorkspaceHandler)
		ticketsapi.MountWorkflowRoutes(api, ticketHandler)
		packagesapi.MountWorkflowRoutes(api, packageHandler)
		cutoverapi.MountWorkflowRoutes(api, cutoverHandler)
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

type ticketReadService struct {
	service *apptickets.Service
	store   *workflowstore.Store
}

func (s ticketReadService) Read(ctx context.Context, ticketID string) (apptickets.TicketDetail, error) {
	return s.service.Read(ctx, ticketID)
}

func (s ticketReadService) ListHistory(ctx context.Context, ticketID string) ([]ticketsapi.RevisionHistory, error) {
	ticket, err := s.store.GetDeliveryTicketByTicketID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	values, err := s.store.ListDeliveryTicketRevisions(ctx, ticket.ID)
	if err != nil {
		return nil, err
	}
	result := make([]ticketsapi.RevisionHistory, 0, len(values))
	for _, value := range values {
		result = append(result, ticketsapi.RevisionHistory{RowID: value.ID, RevisionNumber: value.RevisionNumber, ReplacesRevisionRowID: value.ReplacesRevisionRowID, SourceClosureRowID: value.SourceClosureRowID, CreatedAt: value.CreatedAt, Goal: value.Goal, CancellationReason: value.CancellationReason})
	}
	return result, nil
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
