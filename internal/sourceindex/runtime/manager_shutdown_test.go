package sourceindexruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type shutdownEvents struct {
	mu     sync.Mutex
	counts map[string]int
	signal chan struct{}
}

func newShutdownEvents() *shutdownEvents {
	return &shutdownEvents{counts: make(map[string]int), signal: make(chan struct{}, 32)}
}

func (e *shutdownEvents) record(name string) {
	e.mu.Lock()
	e.counts[name]++
	e.mu.Unlock()
	select {
	case e.signal <- struct{}{}:
	default:
	}
}

func (e *shutdownEvents) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counts[name]
}

func (e *shutdownEvents) await(t *testing.T, name string, want int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for e.count(name) < want {
		select {
		case <-e.signal:
		case <-deadline.C:
			t.Fatalf("%s count = %d, want %d", name, e.count(name), want)
		}
	}
	if got := e.count(name); got != want {
		t.Fatalf("%s count = %d, want exactly %d", name, got, want)
	}
}

type shutdownHarness struct {
	m             *Manager
	events        *shutdownEvents
	workerRelease chan struct{}
	releaseOnce   sync.Once
}

func newShutdownHarness(t *testing.T, parallelism int, blockWorkers bool) *shutdownHarness {
	t.Helper()
	events := newShutdownEvents()
	store := &controlledStartStore{events: newStartEvents(), release: make(chan struct{})}
	store.releaseReconciliation()
	h := &shutdownHarness{
		events:        events,
		workerRelease: make(chan struct{}),
		m: &Manager{
			store:  store,
			config: Config{BuildParallelism: parallelism},
			queued: make(map[string]bool),
			active: make(map[string]bool),
			builds: make(map[string]localBuild),
			locks:  make(map[string]*generationLock),
			queue:  make(chan string, parallelism),
			wake:   make(chan struct{}, 1),
			done:   make(chan struct{}),
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}
	previousWorkerStarted, previousPeriodicStarted, previousWorkerExited := workerStarted, periodicStarted, workerExited
	workerStarted = func() {
		events.record("worker-entry")
		if blockWorkers {
			<-h.workerRelease
		}
	}
	periodicStarted = func() { events.record("periodic-entry") }
	workerExited = func(periodic bool) {
		if periodic {
			events.record("periodic-exit")
			return
		}
		events.record("worker-exit")
	}
	go func() {
		<-h.m.done
		events.record("done-close")
	}()
	t.Cleanup(func() {
		h.releaseWorkers()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.m.Shutdown(ctx)
		workerStarted, periodicStarted, workerExited = previousWorkerStarted, previousPeriodicStarted, previousWorkerExited
	})
	return h
}

func (h *shutdownHarness) releaseWorkers() {
	h.releaseOnce.Do(func() { close(h.workerRelease) })
}

func (h *shutdownHarness) start(t *testing.T) {
	t.Helper()
	if err := h.m.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	h.events.await(t, "worker-entry", h.m.config.BuildParallelism)
	h.events.await(t, "periodic-entry", 1)
	go func() {
		<-h.m.ctx.Done()
		h.events.record("manager-cancel")
	}()
}

func (h *shutdownHarness) shutdown(ctx context.Context) error {
	h.events.record("shutdown-call")
	err := h.m.Shutdown(ctx)
	h.events.record("shutdown-return")
	return err
}

func (h *shutdownHarness) installBuilds() map[string]*int {
	counters := map[string]*int{"first": new(int), "second": new(int), "unregistered": new(int)}
	h.m.mu.Lock()
	for _, id := range []string{"first", "second"} {
		id := id
		h.m.builds[id] = localBuild{cancel: func() {
			*h.buildCounter(counters, id)++
			h.events.record("build-cancel-" + id)
		}, done: make(chan struct{})}
	}
	h.m.mu.Unlock()
	return counters
}

func (h *shutdownHarness) buildCounter(counters map[string]*int, id string) *int {
	return counters[id]
}

func (h *shutdownHarness) awaitStopped(t *testing.T) {
	t.Helper()
	h.events.await(t, "worker-exit", h.m.config.BuildParallelism)
	h.events.await(t, "periodic-exit", 1)
	h.events.await(t, "done-close", 1)
}

func requireShutdownOpen(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s closed unexpectedly", description)
	default:
	}
}

func TestManagerShutdownBeforeStartLeavesManagerStartable(t *testing.T) {
	h := newShutdownHarness(t, 2, false)
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() before Start error = %v", err)
	}
	if h.m.ctx != nil || h.m.cancel != nil {
		t.Fatal("Shutdown() before Start initialized or cancelled a manager context")
	}
	if got := h.events.count("worker-entry"); got != 0 {
		t.Fatalf("worker entries before Start = %d, want 0", got)
	}
	if got := h.events.count("build-cancel-first") + h.events.count("build-cancel-second"); got != 0 {
		t.Fatalf("local build cancellations before Start = %d, want 0", got)
	}
	requireShutdownOpen(t, h.m.done, "manager completion")
	h.start(t)
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() after Start error = %v", err)
	}
	h.awaitStopped(t)
}

func TestManagerShutdownCancelsBuildsAndWaitsForWorkers(t *testing.T) {
	h := newShutdownHarness(t, 2, true)
	h.start(t)
	counters := h.installBuilds()
	done := h.m.done
	result := make(chan error, 1)
	go func() { result <- h.shutdown(context.Background()) }()
	h.events.await(t, "build-cancel-first", 1)
	h.events.await(t, "build-cancel-second", 1)
	h.events.await(t, "manager-cancel", 1)
	h.m.mu.Lock()
	stopping, managerCtx := h.m.stopping, h.m.ctx
	h.m.mu.Unlock()
	if !stopping {
		t.Fatal("manager is not stopping")
	}
	select {
	case <-managerCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("manager context was not cancelled")
	}
	if *counters["first"] != 1 || *counters["second"] != 1 || *counters["unregistered"] != 0 {
		t.Fatalf("build cancellation counts = first:%d second:%d unregistered:%d", *counters["first"], *counters["second"], *counters["unregistered"])
	}
	select {
	case err := <-result:
		t.Fatalf("Shutdown() returned while worker remained blocked: %v", err)
	default:
	}
	requireShutdownOpen(t, done, "manager completion")
	h.releaseWorkers()
	if err := <-result; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	h.awaitStopped(t)
	if h.m.done != done {
		t.Fatal("Shutdown replaced the manager completion channel")
	}
}

func TestManagerConcurrentAndRepeatedShutdown(t *testing.T) {
	h := newShutdownHarness(t, 2, true)
	h.start(t)
	counters := h.installBuilds()
	done := h.m.done
	const callers = 4
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- h.shutdown(context.Background())
		}()
	}
	close(start)
	h.events.await(t, "build-cancel-first", 1)
	h.events.await(t, "build-cancel-second", 1)
	h.events.await(t, "manager-cancel", 1)
	h.releaseWorkers()
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Shutdown() error = %v", err)
		}
	}
	h.awaitStopped(t)
	for range 3 {
		if err := h.shutdown(context.Background()); err != nil {
			t.Fatalf("repeated Shutdown() error = %v", err)
		}
	}
	if *counters["first"] != 1 || *counters["second"] != 1 {
		t.Fatalf("build cancellation counts = first:%d second:%d", *counters["first"], *counters["second"])
	}
	if h.m.done != done || h.events.count("manager-cancel") != 1 || h.events.count("done-close") != 1 || h.events.count("worker-exit") != 2 || h.events.count("periodic-exit") != 1 {
		t.Fatal("repeated Shutdown mutated the stopped lifecycle")
	}
}

func TestManagerShutdownTimeoutThenLaterCompletion(t *testing.T) {
	h := newShutdownHarness(t, 2, true)
	h.start(t)
	counters := h.installBuilds()
	done := h.m.done
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := h.shutdown(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown() error = %v, want context cancellation", err)
	}
	h.events.await(t, "build-cancel-first", 1)
	h.events.await(t, "build-cancel-second", 1)
	h.events.await(t, "manager-cancel", 1)
	h.m.mu.Lock()
	stopping, managerCtx := h.m.stopping, h.m.ctx
	h.m.mu.Unlock()
	if !stopping {
		t.Fatal("manager is not stopping after timed-out Shutdown")
	}
	select {
	case <-managerCtx.Done():
	default:
		t.Fatal("manager context remains active after timed-out Shutdown")
	}
	requireShutdownOpen(t, done, "manager completion")
	h.releaseWorkers()
	if err := h.shutdown(context.Background()); err != nil {
		t.Fatalf("later Shutdown() error = %v", err)
	}
	h.awaitStopped(t)
	if *counters["first"] != 1 || *counters["second"] != 1 || h.events.count("manager-cancel") != 1 || h.m.done != done {
		t.Fatalf("later Shutdown changed cancellation or completion: first=%d second=%d completionReplaced=%v", *counters["first"], *counters["second"], h.m.done != done)
	}
}
