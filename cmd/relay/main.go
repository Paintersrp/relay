package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"relay/internal/app/operations"
	"relay/internal/config"
	"relay/internal/executor"
	"relay/internal/server"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := config.LoadDotenvFiles(".", ".env", ".env.local"); err != nil {
		log.Warn("loading local env files", "error", err)
	}

	workflowDBPath := "data/workflow/relay-workflow.sqlite"
	if value := os.Getenv("RELAY_WORKFLOW_DB_PATH"); value != "" {
		workflowDBPath = value
	}
	workflowArtifactsDir := "data/workflow/artifacts"
	if value := os.Getenv("RELAY_WORKFLOW_ARTIFACTS_DIR"); value != "" {
		workflowArtifactsDir = value
	}
	workflowStore, err := workflowstore.Open(workflowDBPath, workflowArtifactsDir)
	if err != nil {
		log.Error("open workflow store", "error", err)
		os.Exit(1)
	}
	defer workflowStore.Close()

	sourceVaultDir := "data/workflow/source-vaults"
	if value := os.Getenv("RELAY_SOURCE_VAULT_DIR"); value != "" {
		sourceVaultDir = value
	}
	sourceVaults, err := sourcevault.Open(context.Background(), sourceVaultDir, workflowStore)
	if err != nil {
		log.Error("open and reconcile source vaults", "error", err)
		os.Exit(1)
	}
	authorityPublications, err := operations.NewAuthorityPublicationService(workflowStore, sourceVaults)
	if err != nil {
		log.Error("open operation packet authority publication service", "error", err)
		os.Exit(1)
	}
	if err := authorityPublications.Reconcile(context.Background()); err != nil {
		log.Error("reconcile operation packet authority publications", "error", err)
		os.Exit(1)
	}

	ownerInstanceID := executor.NewOwnerInstanceID()
	relayServer := server.NewWorkflow(workflowStore, log, ownerInstanceID, sourceVaults)

	port := "8080"
	if value := os.Getenv("PORT"); value != "" {
		port = value
	}
	log.Info("Relay server starting", "port", port)
	if err := http.ListenAndServe(":"+port, relayServer.Handler()); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
