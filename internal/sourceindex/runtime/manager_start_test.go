package sourceindexruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"relay/internal/sourceindex"
	workflowstore "relay/internal/store/workflow"
)

type startEvents struct {
	mu     sync.Mutex
	counts map[string]int
	order  []string
	signal chan struct{}
}

func newStartEvents() *startEvents {
	return &startEvents{counts: make(map[string]int), signal: make(chan struct{}, 64)}
}

func (e *startEvents) record(name string) {
	e.mu.Lock()
	e.counts[name]++
	e.order = append(e.order, name)
	e.mu.Unlock()
	select {
	case e.signal <- struct{}{}:
	default:
	}
}

func (e *startEvents) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counts[name]
}

func (e *startEvents) position(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, event := range e.order {
		if event == name {
			return i
		}
	}
	return -1
}

type controlledStartStore struct {
	events  *startEvents
	release chan struct{}
	once    sync.Once
	err     error
}

func (s *controlledStartStore) releaseReconciliation() {
	s.once.Do(func() { close(s.release) })
}

func (s *controlledStartStore) CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	return workflowstore.SourceIndexGeneration{}, false, nil
}

func (s *controlledStartStore) GetSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, workflowstore.ErrSourceIndexGenerationNotFound
}

func (s *controlledStartStore) GetSourceIndexGenerationByIdentity(context.Context, sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, workflowstore.ErrSourceIndexGenerationNotFound
}

func (s *controlledStartStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

func (s *controlledStartStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	s.events.record("generation-list")
	s.events.record("reconcile-complete")
	return nil, nil
}

func (s *controlledStartStore) ListActiveSourceIndexAuthorities(ctx context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	s.events.record("authority-list")
	s.events.record("reconcile-enter")
	<-s.release
	if err := ctx.Err(); err != nil {
		s.events.record("reconcile-complete")
		return nil, err
	}
	if s.err != nil {
		s.events.record("reconcile-complete")
		return nil, s.err
	}
	return nil, nil
}

func (s *controlledStartStore) IsSourceIndexAuthorityActive(context.Context, sourceindex.GenerationIdentity) (bool, error) {
	return false, nil
}

func (s *controlledStartStore) MarkSourceIndexGenerationReady(context.Context, workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

func (s *controlledStartStore) MarkSourceIndexGenerationFailed(context.Context, workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

func (s *controlledStartStore) RetrySourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

func (s *controlledStartStore) RetireSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

func (s *controlledStartStore) ReactivateSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

type startHarness struct {
	m      *Manager
	store  *controlledStartStore
	events *startEvents
}

func newStartHarness(t *testing.T, parallelism int) *startHarness {
	t.Helper()
	events := newStartEvents()
	store := &controlledStartStore{events: events, release: make(chan struct{})}
	m := &Manager{
		store:  store,
		config: Config{BuildParallelism: parallelism},
		queued: make(map[string]bool),
		active: make(map[string]bool),
		builds: make(map[string]localBuild),
		locks:  make(map[string]*generationLock),
		queue:  make(chan string, 8),
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	previousWorkerStarted := workerStarted
	previousPeriodicStarted := periodicStarted
	workerStarted = func() { events.record("worker-start") }
	periodicStarted = func() { events.record("periodic-start") }
	h := &startHarness{m: m, store: store, events: events}
	go func() {
		<-m.done
		events.record("done-close")
	}()
	t.Cleanup(func() {
		store.releaseReconciliation()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
		workerStarted = previousWorkerStarted
		periodicStarted = previousPeriodicStarted
	})
	return h
}

func (h *startHarness) start(ctx context.Context) error {
	h.events.record("start-call")
	err := h.m.Start(ctx)
	h.events.record("start-return")
	return err
}

func (h *startHarness) shutdown(ctx context.Context) error {
	h.events.record("shutdown-call")
	err := h.m.Shutdown(ctx)
	h.events.record("shutdown-return")
	return err
}

func awaitStartCount(t *testing.T, events *startEvents, name string, want int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for events.count(name) < want {
		select {
		case <-events.signal:
		case <-deadline.C:
			t.Fatalf("%s count = %d, want %d", name, events.count(name), want)
		}
	}
	if got := events.count(name); got != want {
		t.Fatalf("%s count = %d, want exactly %d", name, got, want)
	}
}

func awaitStartResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle call did not return")
		return nil
	}
}

func requireStartCounts(t *testing.T, events *startEvents, counts map[string]int) {
	t.Helper()
	for name, want := range counts {
		if got := events.count(name); got != want {
			t.Errorf("%s count = %d, want %d", name, got, want)
		}
	}
}

func TestManagerStartFirstInitializesContext(t *testing.T) {
	h := newStartHarness(t, 1)
	parent, cancelParent := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- h.start(parent) }()

	awaitStartCount(t, h.events, "reconcile-enter", 1)
	h.m.mu.Lock()
	started, managerCtx, managerCancel := h.m.started, h.m.ctx, h.m.cancel
	h.m.mu.Unlock()
	if !started || managerCtx == nil || managerCancel == nil {
		t.Fatalf("first Start did not initialize lifecycle: started=%v ctx=%v cancel=%v", started, managerCtx != nil, managerCancel != nil)
	}
	if got := h.events.count("start-return"); got != 0 {
		t.Fatalf("Start returned before startup reconciliation completed: %d returns", got)
	}

	h.store.releaseReconciliation()
	if err := awaitStartResult(t, result); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	awaitStartCount(t, h.events, "worker-start", 1)
	awaitStartCount(t, h.events, "periodic-start", 1)
	cancelParent()
	select {
	case <-managerCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("parent cancellation did not reach manager context")
	}
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	awaitStartCount(t, h.events, "done-close", 1)
	requireStartCounts(t, h.events, map[string]int{"start-call": 1, "start-return": 1, "authority-list": 1, "generation-list": 1, "reconcile-complete": 1, "shutdown-return": 1, "done-close": 1})
}

func TestManagerStartReconciliationPrecedesWorkers(t *testing.T) {
	h := newStartHarness(t, 2)
	result := make(chan error, 1)
	go func() { result <- h.start(context.Background()) }()

	awaitStartCount(t, h.events, "reconcile-enter", 1)
	requireStartCounts(t, h.events, map[string]int{"worker-start": 0, "periodic-start": 0, "start-return": 0, "reconcile-complete": 0})
	h.store.releaseReconciliation()
	if err := awaitStartResult(t, result); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	awaitStartCount(t, h.events, "worker-start", 2)
	awaitStartCount(t, h.events, "periodic-start", 1)
	completed := h.events.position("reconcile-complete")
	if worker, periodic := h.events.position("worker-start"), h.events.position("periodic-start"); completed < 0 || worker <= completed || periodic <= completed {
		t.Fatalf("launch ordering = reconcile %d, worker %d, periodic %d", completed, worker, periodic)
	}
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	awaitStartCount(t, h.events, "done-close", 1)
	requireStartCounts(t, h.events, map[string]int{"authority-list": 1, "generation-list": 1, "worker-start": 2, "periodic-start": 1, "start-return": 1, "shutdown-return": 1, "done-close": 1})
}

func TestManagerStartFailureStopsWithoutWorkers(t *testing.T) {
	h := newStartHarness(t, 3)
	sentinel := errors.New("startup reconciliation failed")
	h.store.err = sentinel
	h.store.releaseReconciliation()
	if err := h.start(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("Start() error = %v, want sentinel", err)
	}

	h.m.mu.Lock()
	stopping, managerCtx := h.m.stopping, h.m.ctx
	h.m.mu.Unlock()
	if !stopping {
		t.Fatal("manager is not stopping after startup failure")
	}
	select {
	case <-managerCtx.Done():
	default:
		t.Fatal("manager context remains active after startup failure")
	}
	awaitStartCount(t, h.events, "done-close", 1)
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() after failed Start = %v", err)
	}
	requireStartCounts(t, h.events, map[string]int{"start-call": 1, "start-return": 1, "authority-list": 1, "generation-list": 0, "reconcile-complete": 1, "worker-start": 0, "periodic-start": 0, "shutdown-return": 1, "done-close": 1})
}

func TestManagerStartShutdownDuringStartupReconciliation(t *testing.T) {
	h := newStartHarness(t, 3)
	startResult := make(chan error, 1)
	shutdownResult := make(chan error, 1)
	go func() { startResult <- h.start(context.Background()) }()
	awaitStartCount(t, h.events, "reconcile-enter", 1)
	go func() { shutdownResult <- h.shutdown(context.Background()) }()

	h.m.mu.Lock()
	managerCtx := h.m.ctx
	h.m.mu.Unlock()
	select {
	case <-managerCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not cancel manager context")
	}
	requireStartCounts(t, h.events, map[string]int{"worker-start": 0, "periodic-start": 0})
	h.store.releaseReconciliation()
	if err := awaitStartResult(t, startResult); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want context cancellation", err)
	}
	if err := awaitStartResult(t, shutdownResult); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	awaitStartCount(t, h.events, "done-close", 1)
	requireStartCounts(t, h.events, map[string]int{"start-call": 1, "start-return": 1, "shutdown-call": 1, "shutdown-return": 1, "authority-list": 1, "generation-list": 0, "reconcile-complete": 1, "worker-start": 0, "periodic-start": 0, "done-close": 1})
}

func TestManagerStartLaunchesExactWorkersAndRejectsRepeatedStart(t *testing.T) {
	h := newStartHarness(t, 3)
	h.store.releaseReconciliation()
	if err := h.start(context.Background()); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	awaitStartCount(t, h.events, "worker-start", 3)
	awaitStartCount(t, h.events, "periodic-start", 1)

	h.m.mu.Lock()
	originalCtx := h.m.ctx
	h.m.mu.Unlock()
	if err := h.start(context.Background()); err == nil {
		t.Fatal("second Start() succeeded")
	}
	select {
	case <-originalCtx.Done():
		t.Fatal("repeated Start cancelled the original manager context")
	default:
	}
	requireStartCounts(t, h.events, map[string]int{"start-call": 2, "start-return": 2, "authority-list": 1, "generation-list": 1, "worker-start": 3, "periodic-start": 1})

	h.m.wakeReconciliation()
	awaitStartCount(t, h.events, "authority-list", 2)
	awaitStartCount(t, h.events, "generation-list", 2)
	requireStartCounts(t, h.events, map[string]int{"worker-start": 3, "periodic-start": 1})
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	awaitStartCount(t, h.events, "done-close", 1)
	requireStartCounts(t, h.events, map[string]int{"shutdown-return": 1, "done-close": 1})
}

func TestManagerStartParentCancellationTerminatesWorkers(t *testing.T) {
	h := newStartHarness(t, 3)
	h.store.releaseReconciliation()
	parent, cancelParent := context.WithCancel(context.Background())
	if err := h.start(parent); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	awaitStartCount(t, h.events, "worker-start", 3)
	awaitStartCount(t, h.events, "periodic-start", 1)

	cancelParent()
	select {
	case <-h.m.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("parent cancellation did not cancel manager context")
	}
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	awaitStartCount(t, h.events, "done-close", 1)
	before := map[string]int{"worker-start": h.events.count("worker-start"), "periodic-start": h.events.count("periodic-start"), "done-close": h.events.count("done-close")}
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	requireStartCounts(t, h.events, before)
	requireStartCounts(t, h.events, map[string]int{"start-call": 1, "start-return": 1, "authority-list": 1, "generation-list": 1, "worker-start": 3, "periodic-start": 1, "shutdown-call": 2, "shutdown-return": 2, "done-close": 1})
}
