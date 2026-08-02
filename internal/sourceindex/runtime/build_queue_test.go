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

type queueStore struct {
	mu              sync.Mutex
	rows            map[string]workflowstore.SourceIndexGeneration
	active          map[string]bool
	authorityChecks map[string]int
	retires         map[string]int
	retries         map[string]int
	retired         map[string]chan struct{}
}

func newQueueStore(rows ...workflowstore.SourceIndexGeneration) *queueStore {
	s := &queueStore{
		rows:            make(map[string]workflowstore.SourceIndexGeneration, len(rows)),
		active:          make(map[string]bool, len(rows)),
		authorityChecks: map[string]int{},
		retires:         map[string]int{},
		retries:         map[string]int{},
		retired:         map[string]chan struct{}{},
	}
	for _, row := range rows {
		s.rows[row.GenerationID] = row
		s.active[row.GenerationID] = true
		s.retired[row.GenerationID] = make(chan struct{}, 1)
	}
	return s
}

func (s *queueStore) CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	return workflowstore.SourceIndexGeneration{}, false, nil
}
func (s *queueStore) GetSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return row, workflowstore.ErrSourceIndexGenerationNotFound
	}
	return row, nil
}
func (s *queueStore) GetSourceIndexGenerationByIdentity(_ context.Context, identity sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		return workflowstore.SourceIndexGeneration{}, err
	}
	return s.GetSourceIndexGeneration(context.Background(), id)
}
func (s *queueStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *queueStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	return nil, nil
}
func (s *queueStore) ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	return nil, nil
}
func (s *queueStore) IsSourceIndexAuthorityActive(_ context.Context, identity sourceindex.GenerationIdentity) (bool, error) {
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorityChecks[id]++
	return s.active[id], nil
}
func (s *queueStore) MarkSourceIndexGenerationReady(context.Context, workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *queueStore) MarkSourceIndexGenerationFailed(context.Context, workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *queueStore) RetrySourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retries[id]++
	row := s.rows[id]
	row.State = workflowstore.SourceIndexGenerationPending
	s.rows[id] = row
	return row, nil
}
func (s *queueStore) RetireSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retires[id]++
	row := s.rows[id]
	row.State = workflowstore.SourceIndexGenerationRetired
	s.rows[id] = row
	s.retired[id] <- struct{}{}
	return row, nil
}
func (s *queueStore) ReactivateSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.rows[id]
	row.State = workflowstore.SourceIndexGenerationPending
	s.rows[id] = row
	return row, nil
}
func (s *queueStore) setActive(id string, active bool) {
	s.mu.Lock()
	s.active[id] = active
	s.mu.Unlock()
}
func (s *queueStore) setState(id string, state workflowstore.SourceIndexGenerationState) {
	s.mu.Lock()
	row := s.rows[id]
	row.State = state
	s.rows[id] = row
	s.mu.Unlock()
}
func (s *queueStore) state(id string) workflowstore.SourceIndexGenerationState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[id].State
}
func (s *queueStore) lifecycleCounts(id string) (authority, retires, retries int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authorityChecks[id], s.retires[id], s.retries[id]
}

type controlledBuilder struct {
	mu                   sync.Mutex
	manager              *Manager
	store                *queueStore
	gates                map[string]chan struct{}
	entered              map[string]chan struct{}
	completed            map[string]chan struct{}
	cancelled            map[string]chan struct{}
	errors               map[string]error
	calls                map[string]int
	activeByGeneration   map[string]int
	maxByGeneration      map[string]int
	currentActive        int
	maximumActive        int
	entryOrder           []string
	completionOrder      []string
	receivedCancellation map[string]int
	ownershipEntries     int
}

func newControlledBuilder(store *queueStore) *controlledBuilder {
	return &controlledBuilder{
		store:                store,
		gates:                map[string]chan struct{}{},
		entered:              map[string]chan struct{}{},
		completed:            map[string]chan struct{}{},
		cancelled:            map[string]chan struct{}{},
		errors:               map[string]error{},
		calls:                map[string]int{},
		activeByGeneration:   map[string]int{},
		maxByGeneration:      map[string]int{},
		receivedCancellation: map[string]int{},
	}
}

func (b *controlledBuilder) block(id string) {
	b.mu.Lock()
	b.gates[id] = make(chan struct{})
	if b.entered[id] == nil {
		b.entered[id] = make(chan struct{}, 8)
		b.completed[id] = make(chan struct{}, 8)
		b.cancelled[id] = make(chan struct{}, 8)
	}
	b.mu.Unlock()
}
func (b *controlledBuilder) release(id string) {
	b.mu.Lock()
	gate := b.gates[id]
	b.mu.Unlock()
	close(gate)
}
func (b *controlledBuilder) injectError(id string, err error) {
	b.mu.Lock()
	b.errors[id] = err
	b.mu.Unlock()
}
func (b *controlledBuilder) BuildGeneration(ctx context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	b.mu.Lock()
	b.calls[id]++
	b.currentActive++
	b.activeByGeneration[id]++
	if b.currentActive > b.maximumActive {
		b.maximumActive = b.currentActive
	}
	if b.activeByGeneration[id] > b.maxByGeneration[id] {
		b.maxByGeneration[id] = b.activeByGeneration[id]
	}
	b.entryOrder = append(b.entryOrder, id)
	gate, entered, completed, cancelled := b.gates[id], b.entered[id], b.completed[id], b.cancelled[id]
	err := b.errors[id]
	b.mu.Unlock()

	b.manager.mu.Lock()
	if b.manager.builds[id].done != nil {
		b.mu.Lock()
		b.ownershipEntries++
		b.mu.Unlock()
	}
	b.manager.mu.Unlock()
	entered <- struct{}{}

	cancelledBuild := false
	select {
	case <-gate:
	case <-ctx.Done():
		cancelledBuild = true
		b.store.setState(id, workflowstore.SourceIndexGenerationFailed)
		cancelled <- struct{}{}
		err = ctx.Err()
	}
	if err != nil && !cancelledBuild {
		b.store.setState(id, workflowstore.SourceIndexGenerationFailed)
	}

	b.mu.Lock()
	b.currentActive--
	b.activeByGeneration[id]--
	b.completionOrder = append(b.completionOrder, id)
	if cancelledBuild {
		b.receivedCancellation[id]++
	}
	b.mu.Unlock()
	completed <- struct{}{}
	return workflowstore.SourceIndexGeneration{}, err
}

type buildSnapshot struct {
	calls, active, maximum, generationMaximum, cancellations int
	entries, completions, ownershipEntries                   int
}

func (b *controlledBuilder) snapshot(id string) buildSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return buildSnapshot{
		calls:             b.calls[id],
		active:            b.currentActive,
		maximum:           b.maximumActive,
		generationMaximum: b.maxByGeneration[id],
		cancellations:     b.receivedCancellation[id],
		entries:           len(b.entryOrder),
		completions:       len(b.completionOrder),
		ownershipEntries:  b.ownershipEntries,
	}
}

type queueHarness struct {
	m            *Manager
	store        *queueStore
	build        *controlledBuilder
	reservations int
}

func newQueueHarness(t *testing.T, parallelism int, rows ...workflowstore.SourceIndexGeneration) *queueHarness {
	t.Helper()
	store := newQueueStore(rows...)
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		store: store, config: Config{BuildParallelism: parallelism, IndexRoot: t.TempDir()},
		queued: map[string]bool{}, active: map[string]bool{}, builds: map[string]localBuild{},
		locks: map[string]*generationLock{}, queue: make(chan string, 32), wake: make(chan struct{}, 1),
		ctx: ctx, cancel: cancel, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	build := newControlledBuilder(store)
	build.manager = m
	m.build = build
	return &queueHarness{m: m, store: store, build: build}
}

func (h *queueHarness) enqueue(id string) {
	h.m.mu.Lock()
	before := h.m.queued[id]
	h.m.mu.Unlock()
	h.m.enqueue(id)
	h.m.mu.Lock()
	after := h.m.queued[id]
	h.m.mu.Unlock()
	if !before && after {
		h.reservations++
	}
}
func (h *queueHarness) startWorkers() {
	for range h.m.config.BuildParallelism {
		h.m.wg.Add(1)
		go h.m.worker()
	}
}
func (h *queueHarness) stopWorkers(t *testing.T) {
	t.Helper()
	h.m.cancel()
	done := make(chan struct{})
	go func() { h.m.wg.Wait(); close(done) }()
	waitSignal(t, done, "workers did not stop")
}
func (h *queueHarness) runNext(t *testing.T) <-chan struct{} {
	t.Helper()
	id := waitString(t, h.m.queue, "queue item was not available")
	done := make(chan struct{})
	go func() { h.m.runBuild(id); close(done) }()
	return done
}

type queueAccounting struct {
	reservations, itemsConsumed, builderCalls, currentActive, maximumActive int
	ownershipEntries, ownershipReleases                                     int
	reservationReleases                                                     int
}

func (h *queueHarness) accounting() queueAccounting {
	h.m.mu.Lock()
	queued, owned := len(h.m.queued), len(h.m.builds)
	h.m.mu.Unlock()
	h.build.mu.Lock()
	calls := 0
	for _, count := range h.build.calls {
		calls += count
	}
	active, maximum, ownershipEntries := h.build.currentActive, h.build.maximumActive, h.build.ownershipEntries
	h.build.mu.Unlock()
	return queueAccounting{
		reservations: h.reservations, itemsConsumed: h.reservations - len(h.m.queue), builderCalls: calls,
		currentActive: active, maximumActive: maximum, ownershipEntries: ownershipEntries,
		ownershipReleases: ownershipEntries - owned, reservationReleases: h.reservations - queued,
	}
}

func requireAccounting(t *testing.T, got, want queueAccounting) {
	t.Helper()
	if got != want {
		t.Fatalf("queue accounting = %+v, want %+v", got, want)
	}
}
func waitSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}
func waitString(t *testing.T, ch <-chan string, message string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal(message)
		return ""
	}
}
func pendingQueueRow(t *testing.T, vault string) workflowstore.SourceIndexGeneration {
	t.Helper()
	identity, id := lifecycleIdentity(t, vault)
	return workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationPending}
}

func TestBuildQueueReservationLifecycle(t *testing.T) {
	row := pendingQueueRow(t, "queue-reservation")
	h := newQueueHarness(t, 1, row)
	h.build.block(row.GenerationID)
	for range 8 {
		h.enqueue(row.GenerationID)
	}
	if len(h.m.queue) != 1 || h.reservations != 1 {
		t.Fatalf("queue items = %d, reservations = %d, want 1, 1", len(h.m.queue), h.reservations)
	}

	firstDone := h.runNext(t)
	waitSignal(t, h.build.entered[row.GenerationID], "first build did not enter")
	for range 8 {
		h.enqueue(row.GenerationID)
	}
	if got := h.build.snapshot(row.GenerationID); got.calls != 1 || len(h.m.queue) != 0 {
		t.Fatalf("active duplicate accounting = %+v, queue items = %d", got, len(h.m.queue))
	}
	h.build.release(row.GenerationID)
	waitSignal(t, firstDone, "first build did not finish")
	requireAccounting(t, h.accounting(), queueAccounting{reservations: 1, itemsConsumed: 1, builderCalls: 1, maximumActive: 1, ownershipEntries: 1, ownershipReleases: 1, reservationReleases: 1})

	h.store.setState(row.GenerationID, workflowstore.SourceIndexGenerationPending)
	h.build.block(row.GenerationID)
	h.enqueue(row.GenerationID)
	secondDone := h.runNext(t)
	waitSignal(t, h.build.entered[row.GenerationID], "second build did not enter")
	h.build.release(row.GenerationID)
	waitSignal(t, secondDone, "second build did not finish")
	got := h.build.snapshot(row.GenerationID)
	if got.calls != 2 || got.generationMaximum != 1 || got.entries != 2 || got.completions != 2 {
		t.Fatalf("build lifecycle = %+v", got)
	}
	requireAccounting(t, h.accounting(), queueAccounting{reservations: 2, itemsConsumed: 2, builderCalls: 2, maximumActive: 1, ownershipEntries: 2, ownershipReleases: 2, reservationReleases: 2})
}

func TestBuildQueueConfiguredParallelismAndDuplicateBuildExclusion(t *testing.T) {
	a := pendingQueueRow(t, "queue-parallel-a")
	b := pendingQueueRow(t, "queue-parallel-b")
	c := pendingQueueRow(t, "queue-parallel-c")
	h := newQueueHarness(t, 2, a, b, c)
	for _, row := range []workflowstore.SourceIndexGeneration{a, b, c} {
		h.build.block(row.GenerationID)
		h.enqueue(row.GenerationID)
	}
	h.startWorkers()
	waitSignal(t, h.build.entered[a.GenerationID], "first build did not enter")
	waitSignal(t, h.build.entered[b.GenerationID], "second build did not enter")
	select {
	case <-h.build.entered[c.GenerationID]:
		t.Fatal("third build exceeded configured parallelism")
	default:
	}
	for range 8 {
		h.enqueue(a.GenerationID)
	}
	if got := h.build.snapshot(a.GenerationID); got.calls != 1 || got.generationMaximum != 1 {
		t.Fatalf("duplicate active build = %+v", got)
	}

	h.build.release(a.GenerationID)
	waitSignal(t, h.build.completed[a.GenerationID], "first build did not complete")
	waitSignal(t, h.build.entered[c.GenerationID], "third build did not enter after capacity released")
	if got := h.build.snapshot(c.GenerationID); got.maximum != 2 || got.active != 2 {
		t.Fatalf("parallel build accounting = %+v", got)
	}
	h.build.release(b.GenerationID)
	h.build.release(c.GenerationID)
	waitSignal(t, h.build.completed[b.GenerationID], "second build did not complete")
	waitSignal(t, h.build.completed[c.GenerationID], "third build did not complete")
	h.stopWorkers(t)
	for _, row := range []workflowstore.SourceIndexGeneration{a, b, c} {
		if got := h.build.snapshot(row.GenerationID); got.calls != 1 || got.generationMaximum != 1 {
			t.Fatalf("generation %s build accounting = %+v", row.GenerationID, got)
		}
	}
	requireAccounting(t, h.accounting(), queueAccounting{reservations: 3, itemsConsumed: 3, builderCalls: 3, maximumActive: 2, ownershipEntries: 3, ownershipReleases: 3, reservationReleases: 3})
}

func TestBuildQueueRefreshesAuthorityAfterQueueWait(t *testing.T) {
	a := pendingQueueRow(t, "queue-authority-a")
	b := pendingQueueRow(t, "queue-authority-b")
	target := pendingQueueRow(t, "queue-authority-target")
	h := newQueueHarness(t, 2, a, b, target)
	for _, row := range []workflowstore.SourceIndexGeneration{a, b, target} {
		h.build.block(row.GenerationID)
		h.enqueue(row.GenerationID)
	}
	h.startWorkers()
	waitSignal(t, h.build.entered[a.GenerationID], "first capacity build did not enter")
	waitSignal(t, h.build.entered[b.GenerationID], "second capacity build did not enter")
	h.store.setActive(target.GenerationID, false)
	h.build.release(a.GenerationID)
	waitSignal(t, h.store.retired[target.GenerationID], "inactive queued generation was not retired")
	select {
	case <-h.build.entered[target.GenerationID]:
		t.Fatal("inactive queued generation reached builder")
	default:
	}
	h.build.release(b.GenerationID)
	waitSignal(t, h.build.completed[a.GenerationID], "first capacity build did not complete")
	waitSignal(t, h.build.completed[b.GenerationID], "second capacity build did not complete")
	h.stopWorkers(t)
	authority, retires, retries := h.store.lifecycleCounts(target.GenerationID)
	if authority == 0 || retires != 1 || retries != 0 || h.store.state(target.GenerationID) != workflowstore.SourceIndexGenerationRetired {
		t.Fatalf("inactive lifecycle: authority checks=%d retires=%d retries=%d state=%s", authority, retires, retries, h.store.state(target.GenerationID))
	}
	requireAccounting(t, h.accounting(), queueAccounting{reservations: 3, itemsConsumed: 3, builderCalls: 2, maximumActive: 2, ownershipEntries: 2, ownershipReleases: 2, reservationReleases: 3})
}

func TestBuildQueueFailureAndCancellationReleaseOwnership(t *testing.T) {
	t.Run("builder failure", func(t *testing.T) {
		row := pendingQueueRow(t, "queue-failure")
		h := newQueueHarness(t, 1, row)
		injected := errors.New("injected build failure")
		h.build.block(row.GenerationID)
		h.build.injectError(row.GenerationID, injected)
		h.enqueue(row.GenerationID)
		firstDone := h.runNext(t)
		waitSignal(t, h.build.entered[row.GenerationID], "failed build did not enter")
		h.build.release(row.GenerationID)
		waitSignal(t, firstDone, "failed build did not finish")
		if h.store.state(row.GenerationID) != workflowstore.SourceIndexGenerationFailed {
			t.Fatalf("failed build state = %s", h.store.state(row.GenerationID))
		}

		h.enqueue(row.GenerationID)
		secondDone := h.runNext(t)
		waitSignal(t, secondDone, "failed generation queue item did not finish")
		if got := h.build.snapshot(row.GenerationID); got.calls != 1 || got.active != 0 || got.generationMaximum != 1 {
			t.Fatalf("failure cleanup = %+v", got)
		}
		requireAccounting(t, h.accounting(), queueAccounting{reservations: 2, itemsConsumed: 2, builderCalls: 1, maximumActive: 1, ownershipEntries: 1, ownershipReleases: 1, reservationReleases: 2})
	})

	t.Run("manager context cancellation", func(t *testing.T) {
		row := pendingQueueRow(t, "queue-cancellation")
		h := newQueueHarness(t, 1, row)
		h.build.block(row.GenerationID)
		h.enqueue(row.GenerationID)
		h.startWorkers()
		waitSignal(t, h.build.entered[row.GenerationID], "cancelled build did not enter")
		h.m.cancel()
		waitSignal(t, h.build.cancelled[row.GenerationID], "builder did not receive cancellation")
		waitSignal(t, h.build.completed[row.GenerationID], "cancelled build did not complete")
		done := make(chan struct{})
		go func() { h.m.wg.Wait(); close(done) }()
		waitSignal(t, done, "cancelled worker did not stop")
		if got := h.build.snapshot(row.GenerationID); got.calls != 1 || got.active != 0 || got.cancellations != 1 || got.generationMaximum != 1 {
			t.Fatalf("cancellation cleanup = %+v", got)
		}
		if h.store.state(row.GenerationID) != workflowstore.SourceIndexGenerationFailed {
			t.Fatalf("cancelled build state = %s", h.store.state(row.GenerationID))
		}
		requireAccounting(t, h.accounting(), queueAccounting{reservations: 1, itemsConsumed: 1, builderCalls: 1, maximumActive: 1, ownershipEntries: 1, ownershipReleases: 1, reservationReleases: 1})
	})
}
