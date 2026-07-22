package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"relay/internal/app/operations"
	"relay/internal/config"
	"relay/internal/executor"
	"relay/internal/server"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type runtimeReady struct {
	MainURL    string
	MCPIngress server.MCPIngressSummary
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, nil); err != nil {
		log.Error("Relay server stopped", "error_class", "runtime_failure")
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, ready chan<- runtimeReady) error {
	if log == nil {
		log = slog.Default()
	}
	if err := config.LoadDotenvFiles(".", ".env", ".env.local"); err != nil {
		log.Warn("loading local env files", "error_class", "configuration_warning")
	}
	workflowDBPath := environmentOrDefault("RELAY_WORKFLOW_DB_PATH", "data/workflow/relay-workflow.sqlite")
	workflowArtifactsDir := environmentOrDefault("RELAY_WORKFLOW_ARTIFACTS_DIR", "data/workflow/artifacts")
	workflowStore, err := workflowstore.Open(workflowDBPath, workflowArtifactsDir)
	if err != nil {
		return fmt.Errorf("open workflow store: %w", err)
	}
	defer workflowStore.Close()
	sourceVaultDir := environmentOrDefault("RELAY_SOURCE_VAULT_DIR", "data/workflow/source-vaults")
	sourceVaults, err := sourcevault.Open(ctx, sourceVaultDir, workflowStore)
	if err != nil {
		return fmt.Errorf("open and reconcile source vaults: %w", err)
	}
	authorityPublications, err := operations.NewAuthorityPublicationService(workflowStore, sourceVaults)
	if err != nil {
		return fmt.Errorf("open operation packet authority publication service: %w", err)
	}
	if err := authorityPublications.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile operation packet authority publications: %w", err)
	}
	ownerInstanceID := executor.NewOwnerInstanceID()
	relayServer := server.NewWorkflow(workflowStore, log, ownerInstanceID, sourceVaults)
	port := environmentOrDefault("PORT", "8080")
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("bind Relay listener: %w", err)
	}
	tcpAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok || tcpAddress.Port < 1 {
		_ = listener.Close()
		return fmt.Errorf("Relay listener did not resolve a TCP port")
	}
	defaultUpstreamBase := fmt.Sprintf("http://127.0.0.1:%d", tcpAddress.Port)
	ingressSummary, err := relayServer.PrepareMCPIngress(defaultUpstreamBase)
	if err != nil {
		_ = listener.Close()
		return err
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	httpServer := &http.Server{Handler: relayServer.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	serveResult := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveResult <- err
	}()
	if err := relayServer.StartMCPIngress(runContext); err != nil {
		cancel()
		_ = httpServer.Close()
		return err
	}
	for _, mapping := range ingressSummary.Mappings {
		log.Info("Relay MCP private ingress starting", "mapping_id", mapping.MappingID, "route_path", mapping.RoutePath, "listener_address", mapping.ListenerAddress)
	}
	log.Info("Relay MCP upstream bearer configuration", "upstream_bearer_configured", ingressSummary.UpstreamBearerConfigured)
	log.Info("Relay server starting", "port", tcpAddress.Port)
	if ready != nil {
		ready <- runtimeReady{MainURL: defaultUpstreamBase, MCPIngress: ingressSummary}
	}
	var runtimeErr error
	select {
	case <-ctx.Done():
	case runtimeErr = <-serveResult:
		cancel()
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer shutdownCancel()
	ingressResult := make(chan error, 1)
	mainResult := make(chan error, 1)
	go func() { ingressResult <- relayServer.ShutdownMCPIngress(shutdownContext) }()
	go func() { mainResult <- httpServer.Shutdown(shutdownContext) }()
	shutdownErr := errors.Join(<-ingressResult, <-mainResult)
	if runtimeErr == nil {
		select {
		case runtimeErr = <-serveResult:
		default:
		}
	}
	return errors.Join(runtimeErr, shutdownErr)
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
