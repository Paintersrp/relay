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
	"path/filepath"
	"syscall"
	"time"

	"relay/internal/app/mcpcomposition"
	"relay/internal/app/operations"
	mcpbootstrap "relay/internal/bootstrap/mcp"
	"relay/internal/config"
	"relay/internal/executor"
	"relay/internal/mcp"
	"relay/internal/server"
	"relay/internal/sourcegateway"
	"relay/internal/sourceindex"
	sourceindexruntime "relay/internal/sourceindex/runtime"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type runtimeReady struct {
	MainURL    string
	MCPIngress server.MCPIngressSummary
}

type sourceIndexRuntime interface {
	sourcegateway.SearchIndexProvider
	Start(context.Context) error
	Shutdown(context.Context) error
	SetLogger(*slog.Logger)
}

type relayLifecycle interface {
	Handler() http.Handler
	PrepareMCPIngress(string) (server.MCPIngressSummary, error)
	StartMCPIngress(context.Context) error
	ShutdownMCPIngress(context.Context) error
}

type httpLifecycle interface {
	Serve(net.Listener) error
	Shutdown(context.Context) error
	Close() error
}

var newWorkflowServer = func(store *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults *sourcevault.Manager, mcpHandlers []server.MCPHandler) (relayLifecycle, error) {
	return server.NewWorkflow(store, log, ownerInstanceID, sourceVaults, mcpHandlers)
}

var listen = net.Listen

var loadSourceIndexConfig = sourceindexruntime.LoadConfig

var composeRetainedSourcePolicy = mcpcomposition.New

var buildMCPHandlers = mcpbootstrap.BuildHandlers

var newHTTPServer = func(handler http.Handler) httpLifecycle {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
}

var newShutdownContext = func() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 6*time.Second)
}

var newSourceIndexRuntime = func(store *workflowstore.Store, authority *sourcevault.Manager, indexConfig sourceindexruntime.Config) (sourceIndexRuntime, error) {
	return sourceindexruntime.New(store, authority, indexConfig)
}

func constructRelayServer(store *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults *sourcevault.Manager, mcpHandlers []server.MCPHandler) (relayLifecycle, error) {
	relayServer, err := newWorkflowServer(store, log, ownerInstanceID, sourceVaults, mcpHandlers)
	if err != nil {
		return nil, fmt.Errorf("construct Relay server: %w", err)
	}
	return relayServer, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, nil); err != nil {
		log.Error("Relay server stopped", "error_class", "runtime_failure", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, ready chan<- runtimeReady) (returnErr error) {
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
	sourceVaultDir, sourceVaultExplicit, err := config.ResolveSourceVaultDir()
	if err != nil {
		return fmt.Errorf("resolve source vault storage: %w", err)
	}
	legacyMigration, err := migrateLegacySourceVault(sourceVaultDir, sourceVaultExplicit)
	if err != nil {
		return err
	}
	sourceVaults, err := sourcevault.Open(ctx, sourceVaultDir, workflowStore)
	if err != nil {
		return fmt.Errorf("open and reconcile source vaults: %w", err)
	}
	if err := legacyMigration.reconciled(); err != nil {
		return fmt.Errorf("record source-vault migration reconciliation: %w", err)
	}
	if err := legacyMigration.cleanup(); err != nil {
		log.Warn("remove migrated legacy source-vault storage", "legacy_path", legacyMigration.legacy, "destination_path", legacyMigration.destination, "error", err)
	}
	authorityPublications, err := operations.NewAuthorityPublicationService(workflowStore, sourceVaults)
	if err != nil {
		return fmt.Errorf("open operation packet authority publication service: %w", err)
	}
	if err := authorityPublications.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile operation packet authority publications: %w", err)
	}
	cursorKey, err := mcpbootstrap.SourceCursorKeyFromEnv()
	if err != nil {
		return err
	}
	abs := func(path string) (string, error) {
		value, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		return filepath.Clean(value), nil
	}
	vaultPath, err := abs(sourceVaultDir)
	if err != nil {
		return fmt.Errorf("resolve source vault storage: %w", err)
	}
	artifactPath, err := abs(workflowArtifactsDir)
	if err != nil {
		return fmt.Errorf("resolve workflow artifact storage: %w", err)
	}
	databasePath, err := abs(workflowDBPath)
	if err != nil {
		return fmt.Errorf("resolve workflow database storage: %w", err)
	}
	indexConfig, err := loadSourceIndexConfig(sourceindex.ProtectedStorage{SourceVaultRoot: vaultPath, WorkflowArtifactsRoot: artifactPath, WorkflowDatabasePath: databasePath})
	if err != nil {
		return fmt.Errorf("load source-index runtime configuration: %w", err)
	}
	var indexRuntime sourceIndexRuntime
	var sourceOptions []sourcegateway.Option
	ownsRuntimeShutdown := false
	if indexConfig.Enabled {
		indexRuntime, err = newSourceIndexRuntime(workflowStore, sourceVaults, indexConfig)
		if err != nil {
			return fmt.Errorf("construct source-index runtime: %w", err)
		}
		indexRuntime.SetLogger(log)
		if err = indexRuntime.Start(ctx); err != nil {
			return fmt.Errorf("start source-index runtime: %w", err)
		}
		ownsRuntimeShutdown = true
		defer func() {
			if !ownsRuntimeShutdown {
				return
			}
			shutdown, cancel := newShutdownContext()
			defer cancel()
			returnErr = errors.Join(returnErr, indexRuntime.Shutdown(shutdown))
		}()
		sourceOptions = append(sourceOptions, sourcegateway.WithSearchIndexProvider(indexRuntime))
	}
	policy, err := composeRetainedSourcePolicy(workflowStore, sourceVaults, authorityPublications, cursorKey, mcp.NewHTTPSFileParameterFetcher(), sourceOptions...)
	if err != nil {
		return fmt.Errorf("compose retained source policy: %w", err)
	}
	mcpHandlers, err := buildMCPHandlers(workflowStore, policy, log)
	if err != nil {
		return fmt.Errorf("compose published MCP handlers: %w", err)
	}
	ownerInstanceID := executor.NewOwnerInstanceID()
	relayServer, err := constructRelayServer(workflowStore, log, ownerInstanceID, sourceVaults, mcpHandlers)
	if err != nil {
		return err
	}
	port := environmentOrDefault("PORT", "8080")
	listener, err := listen("tcp", ":"+port)
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
	httpServer := newHTTPServer(relayServer.Handler())
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
	shutdownContext, shutdownCancel := newShutdownContext()
	defer shutdownCancel()
	ingressResult := make(chan error, 1)
	mainResult := make(chan error, 1)
	indexResult := make(chan error, 1)
	go func() { ingressResult <- relayServer.ShutdownMCPIngress(shutdownContext) }()
	go func() { mainResult <- httpServer.Shutdown(shutdownContext) }()
	if indexRuntime != nil {
		ownsRuntimeShutdown = false
		go func() { indexResult <- indexRuntime.Shutdown(shutdownContext) }()
	} else {
		indexResult <- nil
	}
	shutdownErr := errors.Join(<-ingressResult, <-mainResult, <-indexResult)
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
