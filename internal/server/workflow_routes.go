package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	artifactsapi "relay/internal/api/artifacts"
	auditsapi "relay/internal/api/audits"
	canonicalapi "relay/internal/api/canonical"
	featuresapi "relay/internal/api/features"
	packagesapi "relay/internal/api/packages"
	plansapi "relay/internal/api/plans"
	projectsapi "relay/internal/api/projects"
	repositoriesapi "relay/internal/api/repositories"
	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	ticketsapi "relay/internal/api/tickets"
	appaudits "relay/internal/app/audits"
	appfeatures "relay/internal/app/features"
	appoperations "relay/internal/app/operations"
	apppackages "relay/internal/app/packages"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowsubmissions "relay/internal/app/submissions"
	apptickets "relay/internal/app/tickets"
	appwayfinder "relay/internal/app/wayfinder"
	workflowapp "relay/internal/app/workflow"
	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildWorkflowRoutes(workflowStore *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults sourceVaultRuntime) (http.Handler, error) {
	handler, _, err := buildWorkflowRuntime(workflowStore, log, ownerInstanceID, sourceVaults, nil)
	if err != nil {
		return nil, err
	}
	return handler, nil
}

func buildWorkflowRuntime(workflowStore *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults sourceVaultRuntime, mcpHandlers []MCPHandler) (http.Handler, []MCPRouteDescriptor, error) {
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

	var sourceVaultRoot string
	if sourceVaults != nil {
		sourceVaultRoot = sourceVaults.Root()
	}
	readService, err := workflowapp.NewService(workflowStore, sourceVaultRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("construct workflow read service: %w", err)
	}
	submissionService, err := workflowsubmissions.NewService(workflowStore)
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
	if strings.TrimSpace(sourceVaultRoot) == "" {
		return nil, nil, fmt.Errorf("construct prototype execution: source-vault root is required")
	}
	prototypeExecution, err := executor.NewPrototypeExecution(workflowStore, ownerInstanceID, filepath.Join(sourceVaultRoot, "prototype-execution"))
	if err != nil {
		return nil, nil, fmt.Errorf("construct prototype execution: %w", err)
	}
	if err := featureAuthorityService.SetPrototypeExecutor(prototypeExecution); err != nil {
		return nil, nil, fmt.Errorf("bind prototype execution: %w", err)
	}
	prototypeCleanup, err := executor.NewPrototypeCleanup(workflowStore, filepath.Join(sourceVaultRoot, "prototype-execution"))
	if err != nil {
		return nil, nil, fmt.Errorf("construct prototype cleanup: %w", err)
	}
	if err := featureAuthorityService.SetPrototypeCleaner(prototypeCleanup); err != nil {
		return nil, nil, fmt.Errorf("bind prototype cleanup: %w", err)
	}
	executionService, err := executor.NewExecution(workflowStore, log, ownerInstanceID, sourceVaultReader)
	if err != nil {
		return nil, nil, fmt.Errorf("construct execution: %w", err)
	}
	ticketService, err := apptickets.NewService(workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct ticket service: %w", err)
	}
	// The guided Feature journey must observe the exact ticket Service instance
	// the auditor completion uses so its process-local brief review
	// continuation arms the distinct explicit approval. The Feature owner never
	// constructs a second ticket Service for guided reads or dispatches.
	if err := featureAuthorityService.SetGuidedTicketOwner(ticketService); err != nil {
		return nil, nil, fmt.Errorf("bind guided ticket owner: %w", err)
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
	if err := featureAuthorityService.SetGuidedPackageOwner(packageService); err != nil {
		return nil, nil, fmt.Errorf("bind guided package owner: %w", err)
	}
	if err := featureAuthorityService.SetGuidedAuditOwner(auditService); err != nil {
		return nil, nil, fmt.Errorf("bind guided audit owner: %w", err)
	}
	projectService, err := workflowprojects.NewService(workflowStore, featureAuthorityService)
	if err != nil {
		return nil, nil, fmt.Errorf("construct project service: %w", err)
	}
	packageWorkflowService, err := appoperations.NewPackageWorkflowService(packageService, executionService, workflowStore)
	if err != nil {
		return nil, nil, fmt.Errorf("construct package workflow service: %w", err)
	}

	repositoryHandler := repositoriesapi.NewWorkflowHandler(readService, log)
	projectHandler := projectsapi.NewWorkflowHandler(projectService)
	canonicalHandler := canonicalapi.NewWorkflowHandler(submissionService)
	planHandler := plansapi.NewWorkflowHandler(readService)
	runHandler := runsapi.NewWorkflowReadHandler(readService)
	executionHandler := runsapi.NewWorkflowExecutionHandler(executionService)
	artifactHandler := artifactsapi.NewWorkflowHandler(readService)
	auditHandler := auditsapi.NewWorkflowHandler(auditService)
	featureWorkspaceHandler := featuresapi.NewWorkspaceHandlerFromServices(wayfinderService, featureAuthorityService, featureCompletionWorkflowService)
	ticketHandler := ticketsapi.NewWorkflowHandlerFromServices(ticketWorkflowService, ticketReadService{service: ticketService, store: workflowStore})
	packageHandler := packagesapi.NewWorkflowHandler(packageWorkflowService)

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.RealIP)

	mcpRoutes, err := mcpRouteDescriptors(mcpHandlers)
	if err != nil {
		return nil, nil, fmt.Errorf("construct MCP route descriptors: %w", err)
	}

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

	startupResults, startupErr := prototypeCleanup.ReconcileStartup(context.Background(), 20)
	if startupErr != nil {
		log.Error("prototype startup reconciliation failed", "error", startupErr)
	}
	for _, result := range startupResults {
		if result.Reconciliation.ResultingRunState == "cleanup_required" {
			log.Warn("prototype cleanup remains required", "run_id", result.Run.PrototypeRunID)
		}
	}

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
