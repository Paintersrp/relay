package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	appruns "relay/internal/app/runs"
	"relay/internal/config"
	"relay/internal/events"
	"relay/internal/executor"
	"relay/internal/repos"
	"relay/internal/server"
	"relay/internal/store"
	workflowstore "relay/internal/store/workflow"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := config.LoadDotenvFiles(".", ".env", ".env.local"); err != nil {
		log.Warn("loading local env files", "error", err)
	}

	dbPath := "data/relay.sqlite"
	if p := os.Getenv("RELAY_DB_PATH"); p != "" {
		dbPath = p
	}

	s, err := store.Open(dbPath, log)
	if err != nil {
		log.Error("open store", "error", err)
		os.Exit(1)
	}
	defer s.Close()

	workflowDBPath := "data/workflow/relay-workflow.sqlite"
	if p := os.Getenv("RELAY_WORKFLOW_DB_PATH"); p != "" {
		workflowDBPath = p
	}
	workflowArtifactsDir := "data/workflow/artifacts"
	if p := os.Getenv("RELAY_WORKFLOW_ARTIFACTS_DIR"); p != "" {
		workflowArtifactsDir = p
	}
	workflowStore, err := workflowstore.Open(workflowDBPath, workflowArtifactsDir)
	if err != nil {
		log.Error("open workflow store", "error", err)
		os.Exit(1)
	}
	defer workflowStore.Close()

	repoService := repos.NewService(s, log)
	eventHub := events.NewHub(log)
	ownerInstanceID := executor.NewOwnerInstanceID()
	runService := appruns.NewService(s, log, eventHub)
	if err := runService.ReconcileExecutorOwnership(ownerInstanceID); err != nil {
		log.Error("reconcile executor ownership", "error", err)
		os.Exit(1)
	}

	if err := s.EnsureDefaultRepoRoots([]string{"D:/Code"}); err != nil {
		log.Warn("ensure default repo roots", "error", err)
	}

	go func() {
		summary := repoService.ScanEnabledRoots(context.Background())
		log.Info("repo discovery completed",
			"roots_scanned", summary.RootsScanned,
			"repos_found", summary.ReposFound,
			"repos_saved", summary.ReposSaved,
			"warnings", summary.Warnings,
		)
	}()

	srv := server.NewWithEventsAndWorkflow(s, workflowStore, repoService, log, eventHub, ownerInstanceID)

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	log.Info("Relay server starting", "port", port)
	if err := http.ListenAndServe(":"+port, srv.Handler()); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
