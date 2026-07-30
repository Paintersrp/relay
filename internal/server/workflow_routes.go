package server

import (
	"context"
	"fmt"
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
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildWorkflowRoutes(workflowStore *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults *sourcevault.Manager) (http.Handler, error) {
	handler, _, err := buildWorkflowRuntime(workflowStore, log, ownerInstanceID, sourceVaults, nil)
	if err != nil {
		return nil, err
	}
	return handler, nil
}

func buildWorkflowRuntime(workflowStore *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults *sourcevault.Manager, mcpHandlers []MCPHandler) (http.Handler, []MCPRouteDescriptor, error) {
	if workflowStore == nil {
		return nil, nil, fmt.Errorf("construct workflow runtime: workflow store is required")
	}
	if log == nil {
		log = slog.Default()
	}
	var sourceVaultReader apppackages.SourceVaultReader
	if sourceVaults != nil {
		sourceVaultReader = sourceVaults
	}

	readService, err := workflowapp.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct workflow read service: %w", err)
	}
	projectService, err := workflowprojects.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct project service: %w", err)
	}
	cutoverService, err := appcutover.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct cutover service: %w", err)
	}
	legacyGate := appcutover.NewLegacyGate(cutoverService)
	planMutationService, err := workflowplans.NewServiceWithGate(workflowStore, legacyGate)
	if err != nil {
		return nil, nil, fmt.Errorf("construct plan mutation service: %w", err)
	}
	submissionService, err := workflowsubmissions.NewServiceWithGate(workflowStore, legacyGate)
	if err != nil {
		return nil, nil, fmt.Errorf("construct submission service: %w", err)
	}
	auditService, err := appaudits.NewWorkflowAuditServiceWithSourceVaults(workflowStore, sourceVaultReader)
	if err != nil {
		return nil, nil, fmt.Errorf("construct audit service: %w", err)
	}
	wayfinderService, err := appwayfinder.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct wayfinder service: %w", err)
	}
	featureAuthorityService, err := appfeatures.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct feature authority service: %w", err)
	}
	executionService, err := executor.NewExecution(workflowStore, log, ownerInstanceID, sourceVaultReader)
	if err != nil {
		return nil, nil, fmt.Errorf("construct execution: %w", err)
	}
	ticketService, err := apptickets.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct ticket service: %w", err)
	}
	packetService, err := appoperations.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct packet service: %w", err)
	}
	ticketWorkflowService, err := appoperations.NewTicketWorkflowService(packetService, ticketService)
	if err != nil {
		return nil, nil, fmt.Errorf("construct ticket workflow service: %w", err)
	}
	featureCompletionWorkflowService, err := appoperations.NewFeatureCompletionWorkflowService(featureAuthorityService)
	if err != nil {
		return nil, nil, fmt.Errorf("construct feature completion workflow service: %w", err)
	}
	packageService, err := apppackages.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct package service: %w", err)
	}
	packageWorkflowService, err := appoperations.NewPackageWorkflowService(packageService, executionService, workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct package workflow service: %w", err)
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
	cutoverWorkflowService, err := appoperations.NewCutoverWorkflowService(packetService, cutoverService)
	if err != nil {
		return nil, nil, fmt.Errorf("construct cutover workflow service: %w", err)
	}
	cutoverHandler := cutoverapi.NewWorkflowHandler(cutoverService, cutoverWorkflowService)

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.RealIP)

	if err := mcp.ValidateCompiledSurfaceCatalog(); err != nil {
		return nil, nil, fmt.Errorf("validate MCP surface catalog: %w", err)
	}
	mcpRoutes, err := mcpRouteDescriptors(mcpHandlers)
	if err != nil {
		return nil, nil, fmt.Errorf("construct MCP route descriptors: %w", err)
	}

	mcpServer := mcp.NewServer(log, mcp.NewWorkflowDepsFromEnv(workflowStore, log, sourceVaultReader))
	router.Handle("/mcp", newCutoverAggregateHandler(cutoverService, mcp.NewHTTPHandler(mcpServer, log)))
	for _, current := range mcpHandlers {
		router.Handle(current.Path, current.Handler)
	}

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

	return router, mcpRoutes, nil
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

type legacyAdmissionState interface {
	IsLegacyAdmissionClosed(context.Context) (bool, error)
}

type cutoverAggregateHandler struct {
	state legacyAdmissionState
	next  http.Handler
}

func newCutoverAggregateHandler(state legacyAdmissionState, next http.Handler) http.Handler {
	return cutoverAggregateHandler{state: state, next: next}
}

func (handler cutoverAggregateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	closed, err := handler.state.IsLegacyAdmissionClosed(r.Context())
	if err != nil {
		shared.Error(w, http.StatusServiceUnavailable, "CUTOVER_STATE_UNAVAILABLE", "Cutover admission state is unavailable")
		return
	}
	if closed {
		shared.Error(w, http.StatusConflict, "LEGACY_ADMISSION_CLOSED", "Legacy MCP admission is closed")
		return
	}
	handler.next.ServeHTTP(w, r)
}
