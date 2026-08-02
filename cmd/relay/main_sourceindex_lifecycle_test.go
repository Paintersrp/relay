package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"relay/internal/app/mcpcomposition"
	"relay/internal/app/operations"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/server"
	"relay/internal/sourcegateway"
	"relay/internal/sourceindex"
	sourceindexruntime "relay/internal/sourceindex/runtime"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type lifecycleCounts struct {
	runtimeConstructor atomic.Int32
	runtimeStart       atomic.Int32
	runtimeShutdown    atomic.Int32
	listener           atomic.Int32
	composition        atomic.Int32
	handlerComposition atomic.Int32
	serverConstruction atomic.Int32
	serverStart        atomic.Int32
	mcpStart           atomic.Int32
	httpShutdown       atomic.Int32
	mcpShutdown        atomic.Int32
	readiness          atomic.Int32
	shutdownWindows    atomic.Int32
}

type lifecycleWant struct {
	runtimeConstructor int32
	runtimeStart       int32
	runtimeShutdown    int32
	listener           int32
	composition        int32
	handlerComposition int32
	serverConstruction int32
	serverStart        int32
	mcpStart           int32
	httpShutdown       int32
	mcpShutdown        int32
	readiness          int32
}

func (c *lifecycleCounts) assert(t *testing.T, want lifecycleWant) {
	t.Helper()
	got := lifecycleWant{
		runtimeConstructor: c.runtimeConstructor.Load(), runtimeStart: c.runtimeStart.Load(), runtimeShutdown: c.runtimeShutdown.Load(),
		listener: c.listener.Load(), composition: c.composition.Load(), handlerComposition: c.handlerComposition.Load(),
		serverConstruction: c.serverConstruction.Load(), serverStart: c.serverStart.Load(), mcpStart: c.mcpStart.Load(),
		httpShutdown: c.httpShutdown.Load(), mcpShutdown: c.mcpShutdown.Load(), readiness: c.readiness.Load(),
	}
	if got != want {
		t.Fatalf("lifecycle counts = %+v, want %+v", got, want)
	}
}

type fakeSourceIndexRuntime struct {
	counts          *lifecycleCounts
	startErr        error
	shutdownErr     error
	waitForShutdown bool
	startEntered    chan struct{}
	startRelease    chan struct{}
	shutdownEntered chan context.Context
	blockBuild      bool
	buildActive     atomic.Bool
	buildRelease    chan struct{}
	buildDone       chan struct{}
	buildOnce       sync.Once
	contextsMu      sync.Mutex
	contexts        []context.Context
}

func newFakeSourceIndexRuntime(counts *lifecycleCounts) *fakeSourceIndexRuntime {
	return &fakeSourceIndexRuntime{counts: counts, startEntered: make(chan struct{}, 1), shutdownEntered: make(chan context.Context, 1)}
}

func (f *fakeSourceIndexRuntime) Start(ctx context.Context) error {
	f.counts.runtimeStart.Add(1)
	f.startEntered <- struct{}{}
	if f.startRelease != nil {
		select {
		case <-f.startRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.startErr != nil {
		return f.startErr
	}
	if f.blockBuild {
		f.buildRelease = make(chan struct{})
		f.buildDone = make(chan struct{})
		f.buildActive.Store(true)
		go func() {
			<-f.buildRelease
			f.buildActive.Store(false)
			close(f.buildDone)
		}()
	}
	return nil
}

func (f *fakeSourceIndexRuntime) Shutdown(ctx context.Context) error {
	f.counts.runtimeShutdown.Add(1)
	f.contextsMu.Lock()
	f.contexts = append(f.contexts, ctx)
	f.contextsMu.Unlock()
	f.shutdownEntered <- ctx
	if f.blockBuild {
		f.buildOnce.Do(func() { close(f.buildRelease) })
		<-f.buildDone
	}
	if f.waitForShutdown {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.shutdownErr
}

func (*fakeSourceIndexRuntime) SetLogger(*slog.Logger) {}

func (*fakeSourceIndexRuntime) OpenSearchIndex(context.Context, operations.SourceReadAuthority) (sourcegateway.SearchIndexHandle, error) {
	return nil, errors.New("fake search index is not used by command lifecycle tests")
}

type fakeRelayLifecycle struct {
	counts          *lifecycleCounts
	startErr        error
	shutdownErr     error
	waitForShutdown bool
	shutdownEntered chan context.Context
	contextsMu      sync.Mutex
	contexts        []context.Context
}

func (f *fakeRelayLifecycle) Handler() http.Handler { return http.NotFoundHandler() }

func (*fakeRelayLifecycle) PrepareMCPIngress(string) (server.MCPIngressSummary, error) {
	return server.MCPIngressSummary{}, nil
}

func (f *fakeRelayLifecycle) StartMCPIngress(context.Context) error {
	f.counts.mcpStart.Add(1)
	return f.startErr
}

func (f *fakeRelayLifecycle) ShutdownMCPIngress(ctx context.Context) error {
	f.counts.mcpShutdown.Add(1)
	f.contextsMu.Lock()
	f.contexts = append(f.contexts, ctx)
	f.contextsMu.Unlock()
	f.shutdownEntered <- ctx
	if f.waitForShutdown {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.shutdownErr
}

type fakeHTTPLifecycle struct {
	counts          *lifecycleCounts
	serveDone       chan struct{}
	serveOnce       sync.Once
	shutdownErr     error
	waitForShutdown bool
	shutdownEntered chan context.Context
	contextsMu      sync.Mutex
	contexts        []context.Context
}

func (f *fakeHTTPLifecycle) Serve(net.Listener) error {
	f.counts.serverStart.Add(1)
	<-f.serveDone
	return http.ErrServerClosed
}

func (f *fakeHTTPLifecycle) Shutdown(ctx context.Context) error {
	f.counts.httpShutdown.Add(1)
	f.contextsMu.Lock()
	f.contexts = append(f.contexts, ctx)
	f.contextsMu.Unlock()
	f.shutdownEntered <- ctx
	defer f.stopServing()
	if f.waitForShutdown {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.shutdownErr
}

func (f *fakeHTTPLifecycle) Close() error {
	f.stopServing()
	return nil
}

func (f *fakeHTTPLifecycle) stopServing() { f.serveOnce.Do(func() { close(f.serveDone) }) }

type fakeTCPListener struct{}

func (*fakeTCPListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (*fakeTCPListener) Close() error              { return nil }
func (*fakeTCPListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18463} }

type lifecycleHarness struct {
	counts              lifecycleCounts
	runtime             *fakeSourceIndexRuntime
	relay               *fakeRelayLifecycle
	http                *fakeHTTPLifecycle
	constructorErr      error
	compositionErr      error
	handlerErr          error
	listenerErr         error
	shutdownContext     func() (context.Context, context.CancelFunc)
	originalComposition func(*workflowstore.Store, *sourcevault.Manager, *operations.AuthorityPublicationService, []byte, fileacquisition.FetchOne, ...sourcegateway.Option) (mcpcomposition.Services, error)
	originalHandlers    func(*workflowstore.Store, mcpcomposition.Services, *slog.Logger) ([]server.MCPHandler, error)
}

func installLifecycleHarness(t *testing.T, enabled bool) *lifecycleHarness {
	t.Helper()
	root := t.TempDir()
	t.Setenv("RELAY_WORKFLOW_DB_PATH", filepath.Join(root, "workflow.sqlite"))
	t.Setenv("RELAY_WORKFLOW_ARTIFACTS_DIR", filepath.Join(root, "artifacts"))
	t.Setenv("RELAY_SOURCE_VAULT_DIR", filepath.Join(root, "source-vaults"))
	t.Setenv("RELAY_SOURCE_CURSOR_HMAC_KEY", strings.Repeat("k", 32))
	t.Setenv("PORT", "0")

	h := &lifecycleHarness{}
	h.runtime = newFakeSourceIndexRuntime(&h.counts)
	h.relay = &fakeRelayLifecycle{counts: &h.counts, shutdownEntered: make(chan context.Context, 1)}
	h.http = &fakeHTTPLifecycle{counts: &h.counts, serveDone: make(chan struct{}), shutdownEntered: make(chan context.Context, 1)}
	h.originalComposition = composeRetainedSourcePolicy
	h.originalHandlers = buildMCPHandlers
	h.shutdownContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }

	originalLoadConfig := loadSourceIndexConfig
	originalRuntime := newSourceIndexRuntime
	originalCompose := composeRetainedSourcePolicy
	originalBuildHandlers := buildMCPHandlers
	originalWorkflowServer := newWorkflowServer
	originalListen := listen
	originalHTTPServer := newHTTPServer
	originalShutdownContext := newShutdownContext
	t.Cleanup(func() {
		loadSourceIndexConfig = originalLoadConfig
		newSourceIndexRuntime = originalRuntime
		composeRetainedSourcePolicy = originalCompose
		buildMCPHandlers = originalBuildHandlers
		newWorkflowServer = originalWorkflowServer
		listen = originalListen
		newHTTPServer = originalHTTPServer
		newShutdownContext = originalShutdownContext
	})

	loadSourceIndexConfig = func(sourceindex.ProtectedStorage) (sourceindexruntime.Config, error) {
		return sourceindexruntime.Config{Enabled: enabled}, nil
	}
	newSourceIndexRuntime = func(*workflowstore.Store, *sourcevault.Manager, sourceindexruntime.Config) (sourceIndexRuntime, error) {
		h.counts.runtimeConstructor.Add(1)
		if h.constructorErr != nil {
			return nil, h.constructorErr
		}
		return h.runtime, nil
	}
	composeRetainedSourcePolicy = func(store *workflowstore.Store, vaults *sourcevault.Manager, publications *operations.AuthorityPublicationService, cursorKey []byte, fetcher fileacquisition.FetchOne, options ...sourcegateway.Option) (mcpcomposition.Services, error) {
		h.counts.composition.Add(1)
		if h.compositionErr != nil {
			return mcpcomposition.Services{}, h.compositionErr
		}
		return h.originalComposition(store, vaults, publications, cursorKey, fetcher, options...)
	}
	buildMCPHandlers = func(store *workflowstore.Store, policy mcpcomposition.Services, log *slog.Logger) ([]server.MCPHandler, error) {
		h.counts.handlerComposition.Add(1)
		if h.handlerErr != nil {
			return nil, h.handlerErr
		}
		return h.originalHandlers(store, policy, log)
	}
	newWorkflowServer = func(*workflowstore.Store, *slog.Logger, string, *sourcevault.Manager, []server.MCPHandler) (relayLifecycle, error) {
		h.counts.serverConstruction.Add(1)
		return h.relay, nil
	}
	listen = func(string, string) (net.Listener, error) {
		h.counts.listener.Add(1)
		if h.listenerErr != nil {
			return nil, h.listenerErr
		}
		return &fakeTCPListener{}, nil
	}
	newHTTPServer = func(http.Handler) httpLifecycle { return h.http }
	newShutdownContext = func() (context.Context, context.CancelFunc) {
		h.counts.shutdownWindows.Add(1)
		return h.shutdownContext()
	}
	return h
}

func (h *lifecycleHarness) run(t *testing.T, ctx context.Context, ready chan<- runtimeReady) error {
	t.Helper()
	return run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), ready)
}

func awaitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(60 * time.Second):
		t.Fatal("command lifecycle did not terminate")
		return nil
	}
}

func awaitShutdownEntry(t *testing.T, entered <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-entered:
		return ctx
	case <-time.After(60 * time.Second):
		t.Fatal("shutdown participant did not enter")
		return nil
	}
}

func runUntilReady(t *testing.T, h *lifecycleHarness) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan runtimeReady, 1)
	result := make(chan error, 1)
	go func() { result <- h.run(t, ctx, ready) }()
	select {
	case <-ready:
		h.counts.readiness.Add(1)
	case err := <-result:
		t.Fatalf("Relay stopped before readiness: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("Relay did not become ready")
	}
	return cancel, result
}

func TestSourceIndexCMD01DisabledConstructsNoRuntime(t *testing.T) {
	h := installLifecycleHarness(t, false)
	cancel, result := runUntilReady(t, h)
	cancel()
	if err := awaitResult(t, result); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	h.counts.assert(t, lifecycleWant{listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
}

func TestSourceIndexCMD02ConstructionFailureStopsStartup(t *testing.T) {
	h := installLifecycleHarness(t, true)
	sentinel := errors.New("runtime construction failed")
	h.constructorErr = sentinel
	ready := make(chan runtimeReady, 1)
	err := h.run(t, context.Background(), ready)
	if !errors.Is(err, sentinel) {
		t.Fatalf("run() error = %v, want construction sentinel", err)
	}
	h.counts.readiness.Store(int32(len(ready)))
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1})
}

func TestSourceIndexCMD03StartFailureDoesNotTransferShutdownOwnership(t *testing.T) {
	h := installLifecycleHarness(t, true)
	sentinel := errors.New("runtime start failed")
	h.runtime.startErr = sentinel
	ready := make(chan runtimeReady, 1)
	err := h.run(t, context.Background(), ready)
	if !errors.Is(err, sentinel) {
		t.Fatalf("run() error = %v, want start sentinel", err)
	}
	h.counts.readiness.Store(int32(len(ready)))
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1})
}

func TestSourceIndexCMD04PostStartFailureRunsDeferredShutdownOnce(t *testing.T) {
	h := installLifecycleHarness(t, true)
	sentinel := errors.New("handler composition failed")
	h.handlerErr = sentinel
	err := h.run(t, context.Background(), make(chan runtimeReady, 1))
	if !errors.Is(err, sentinel) {
		t.Fatalf("run() error = %v, want handler sentinel", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, composition: 1, handlerComposition: 1})
}

func TestSourceIndexCMD05ListenerFailureShutsRuntimeDownOnce(t *testing.T) {
	h := installLifecycleHarness(t, true)
	sentinel := errors.New("listener bind failed")
	h.listenerErr = sentinel
	err := h.run(t, context.Background(), make(chan runtimeReady, 1))
	if !errors.Is(err, sentinel) {
		t.Fatalf("run() error = %v, want listener sentinel", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1})
}

func TestSourceIndexCMD06CompositionFailureShutsRuntimeDownOnce(t *testing.T) {
	h := installLifecycleHarness(t, true)
	sentinel := errors.New("application composition failed")
	h.compositionErr = sentinel
	err := h.run(t, context.Background(), make(chan runtimeReady, 1))
	if !errors.Is(err, sentinel) {
		t.Fatalf("run() error = %v, want composition sentinel", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, composition: 1})
}

func TestRuntimeLifecycleCMD07NormalShutdownOwnsRuntimeOnce(t *testing.T) {
	h := installLifecycleHarness(t, true)
	cancel, result := runUntilReady(t, h)
	cancel()
	if err := awaitResult(t, result); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
}

func TestShutdownRuntimeCMD08RuntimeErrorIsReturnedAfterAllShutdowns(t *testing.T) {
	h := installLifecycleHarness(t, true)
	sentinel := errors.New("runtime shutdown failed")
	h.runtime.shutdownErr = sentinel
	cancel, result := runUntilReady(t, h)
	cancel()
	err := awaitResult(t, result)
	if !errors.Is(err, sentinel) {
		t.Fatalf("run() error = %v, want shutdown sentinel", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
}

func TestShutdownRuntimeCMD09RuntimeTimeoutIsReturnedWithoutRetry(t *testing.T) {
	h := installLifecycleHarness(t, true)
	h.runtime.waitForShutdown = true
	var shutdownWindow context.Context
	h.shutdownContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		shutdownWindow = ctx
		return ctx, cancel
	}
	cancel, result := runUntilReady(t, h)
	cancel()
	err := awaitResult(t, result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run() error = %v, want deadline exceeded", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
	if h.counts.shutdownWindows.Load() != 1 {
		t.Fatalf("shutdown windows = %d, want 1", h.counts.shutdownWindows.Load())
	}
	h.runtime.contextsMu.Lock()
	defer h.runtime.contextsMu.Unlock()
	if len(h.runtime.contexts) != 1 || h.runtime.contexts[0] != shutdownWindow {
		t.Fatalf("runtime shutdown did not use the coordinated timeout context")
	}
}

func TestShutdownRuntimeCMD10TimeoutDoesNotRunDeferredShutdown(t *testing.T) {
	h := installLifecycleHarness(t, true)
	h.runtime.waitForShutdown = true
	h.http.waitForShutdown = true
	h.relay.waitForShutdown = true
	h.shutdownContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		return ctx, cancel
	}
	cancel, result := runUntilReady(t, h)
	cancel()
	if err := awaitResult(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run() error = %v, want deadline exceeded", err)
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
	if h.counts.shutdownWindows.Load() != 1 {
		t.Fatalf("shutdown windows = %d, want 1", h.counts.shutdownWindows.Load())
	}
}

func TestShutdownRuntimeCMD11ParticipantsShareCancellationWindow(t *testing.T) {
	h := installLifecycleHarness(t, true)
	h.runtime.waitForShutdown = true
	h.http.waitForShutdown = true
	h.relay.waitForShutdown = true
	shared, cancelWindow := context.WithCancel(context.Background())
	h.shutdownContext = func() (context.Context, context.CancelFunc) { return shared, cancelWindow }
	cancelRun, result := runUntilReady(t, h)
	cancelRun()
	runtimeContext := awaitShutdownEntry(t, h.runtime.shutdownEntered)
	httpContext := awaitShutdownEntry(t, h.http.shutdownEntered)
	mcpContext := awaitShutdownEntry(t, h.relay.shutdownEntered)
	if runtimeContext != shared || httpContext != shared || mcpContext != shared {
		t.Fatalf("shutdown participants did not receive the shared context")
	}
	cancelWindow()
	if err := awaitResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("run() error = %v, want shared cancellation", err)
	}
	for name, observed := range map[string]context.Context{"runtime": runtimeContext, "HTTP": httpContext, "MCP": mcpContext} {
		if !errors.Is(observed.Err(), context.Canceled) {
			t.Fatalf("%s shutdown did not observe shared cancellation", name)
		}
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
	if h.counts.shutdownWindows.Load() != 1 {
		t.Fatalf("shutdown windows = %d, want 1", h.counts.shutdownWindows.Load())
	}
}

func TestReadinessIndexCMD12BuildMayRemainBlocked(t *testing.T) {
	h := installLifecycleHarness(t, true)
	h.runtime.blockBuild = true
	cancel, result := runUntilReady(t, h)
	if !h.runtime.buildActive.Load() {
		t.Fatal("source-index build completed before readiness")
	}
	cancel()
	if err := awaitResult(t, result); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if h.runtime.buildActive.Load() {
		t.Fatal("source-index build remained active after shutdown")
	}
	h.counts.assert(t, lifecycleWant{runtimeConstructor: 1, runtimeStart: 1, runtimeShutdown: 1, listener: 1, composition: 1, handlerComposition: 1, serverConstruction: 1, serverStart: 1, mcpStart: 1, httpShutdown: 1, mcpShutdown: 1, readiness: 1})
}

var _ sourceIndexRuntime = (*fakeSourceIndexRuntime)(nil)
var _ relayLifecycle = (*fakeRelayLifecycle)(nil)
var _ httpLifecycle = (*fakeHTTPLifecycle)(nil)
