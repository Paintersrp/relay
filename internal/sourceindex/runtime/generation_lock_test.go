package sourceindexruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"relay/internal/app/operations"
	"relay/internal/sourcegateway"
	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

type generationLockAccounting struct {
	mu              sync.Mutex
	readerOpens     int
	readerCloses    int
	ownership       int
	writerAcquires  int
	finalCleanups   int
	attemptCleanups int
}

func (a *generationLockAccounting) add(field *int) {
	a.mu.Lock()
	*field++
	a.mu.Unlock()
}

func (a *generationLockAccounting) snapshot() generationLockAccounting {
	a.mu.Lock()
	defer a.mu.Unlock()
	return generationLockAccounting{
		readerOpens:     a.readerOpens,
		readerCloses:    a.readerCloses,
		ownership:       a.ownership,
		writerAcquires:  a.writerAcquires,
		finalCleanups:   a.finalCleanups,
		attemptCleanups: a.attemptCleanups,
	}
}

func (a *generationLockAccounting) require(t *testing.T, opens, closes, ownership, writers, final, attempts int) {
	t.Helper()
	got := a.snapshot()
	if got.readerOpens != opens || got.readerCloses != closes || got.ownership != ownership || got.writerAcquires != writers || got.finalCleanups != final || got.attemptCleanups != attempts {
		t.Fatalf("accounting opens=%d closes=%d ownership=%d writers=%d cleanup=%d/%d, want %d/%d/%d/%d/%d/%d", got.readerOpens, got.readerCloses, got.ownership, got.writerAcquires, got.finalCleanups, got.attemptCleanups, opens, closes, ownership, writers, final, attempts)
	}
}

type generationLockReader struct {
	accounting *generationLockAccounting
	path       []byte
}

func (*generationLockReader) Descriptor() reader.Descriptor { return reader.Descriptor{} }
func (r *generationLockReader) FallbackCandidates() []reader.Candidate {
	return []reader.Candidate{{Path: append([]byte(nil), r.path...)}}
}
func (r *generationLockReader) IndexedTextCandidates(context.Context, string) ([]reader.Candidate, error) {
	return []reader.Candidate{{Path: append([]byte(nil), r.path...)}}, nil
}
func (r *generationLockReader) Close() error {
	r.accounting.add(&r.accounting.readerCloses)
	return nil
}

type generationLockFixture struct {
	m          *Manager
	store      *runtimeStore
	row        workflowstore.SourceIndexGeneration
	accounting *generationLockAccounting
}

func newGenerationLockFixture(t *testing.T, name string) *generationLockFixture {
	t.Helper()
	identity, id := lifecycleIdentity(t, "generation-lock-"+name)
	row := workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationReady}
	store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
	ctx, cancel := context.WithCancel(context.Background())
	accounting := &generationLockAccounting{}
	m := &Manager{
		store: store,
		authority: providerAuthority{resolve: func(context.Context) (sourceindex.GenerationIdentity, error) {
			return identity, nil
		}},
		config:  Config{QueryTimeout: time.Second},
		started: true,
		queued:  map[string]bool{},
		active:  map[string]bool{},
		builds:  map[string]localBuild{},
		locks:   map[string]*generationLock{},
		queue:   make(chan string, 4),
		wake:    make(chan struct{}, 1),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		logger:  slog.Default(),
	}
	oldOpen := openGenerationReader
	openGenerationReader = func(context.Context, reader.GenerationStore, reader.Config, sourceindex.GenerationIdentity) (generationReader, error) {
		accounting.mu.Lock()
		accounting.readerOpens++
		n := accounting.readerOpens
		accounting.mu.Unlock()
		return &generationLockReader{accounting: accounting, path: []byte(fmt.Sprintf("reader-%d", n))}, nil
	}
	t.Cleanup(func() {
		openGenerationReader = oldOpen
		cancel()
	})
	return &generationLockFixture{m: m, store: store, row: row, accounting: accounting}
}

func (f *generationLockFixture) open(t *testing.T) sourcegateway.SearchIndexHandle {
	t.Helper()
	h, err := f.m.OpenSearchIndex(context.Background(), operations.SourceReadAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("OpenSearchIndex returned a nil handle")
	}
	concrete, ok := h.(*handle)
	if !ok {
		t.Fatalf("handle type = %T", h)
	}
	release := concrete.release
	concrete.release = func() {
		f.accounting.add(&f.accounting.ownership)
		release()
	}
	return h
}

func requireGenerationLockUsable(t *testing.T, h sourcegateway.SearchIndexHandle) {
	t.Helper()
	got, err := h.IndexedTextCandidates(context.Background(), "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Path) == 0 {
		t.Fatalf("query candidates = %+v", got)
	}
	if got := h.FallbackCandidates(); len(got) != 1 || len(got[0].Path) == 0 {
		t.Fatalf("fallback candidates = %+v", got)
	}
}

func requireNotSignaled(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(message)
	default:
	}
}

func awaitGenerationLockSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

func TestGenerationReadWriteLockExclusion(t *testing.T) {
	f := newGenerationLockFixture(t, "read-write")
	first := f.open(t)
	second := f.open(t)
	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})
	requireGenerationLockUsable(t, first)
	requireGenerationLockUsable(t, second)
	f.accounting.require(t, 2, 0, 0, 0, 0, 0)

	attempting := make(chan struct{})
	acquired := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(attempting)
		l := f.m.lock(f.row.GenerationID)
		l.mu.Lock()
		f.accounting.add(&f.accounting.writerAcquires)
		close(acquired)
		l.mu.Unlock()
		close(writerDone)
	}()
	awaitGenerationLockSignal(t, attempting, "writer did not attempt generation lock acquisition")
	requireNotSignaled(t, acquired, "writer acquired while both provider handles were open")

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	requireNotSignaled(t, acquired, "closing the first handle released the second handle's ownership")
	requireGenerationLockUsable(t, second)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	requireNotSignaled(t, acquired, "repeated first-handle close changed generation ownership")
	f.accounting.require(t, 2, 1, 1, 0, 0, 0)

	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	awaitGenerationLockSignal(t, acquired, "writer did not acquire after the final handle closed")
	awaitGenerationLockSignal(t, writerDone, "writer did not release the generation lock")
	f.accounting.require(t, 2, 2, 2, 1, 0, 0)
}

func TestGenerationWaitingWriterIsNotBypassedByReader(t *testing.T) {
	f := newGenerationLockFixture(t, "writer-preference")
	first := f.open(t)
	requireGenerationLockUsable(t, first)

	attempting := make(chan struct{})
	writerAcquired := make(chan struct{})
	writerRelease := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(attempting)
		l := f.m.lock(f.row.GenerationID)
		l.mu.Lock()
		f.accounting.add(&f.accounting.writerAcquires)
		close(writerAcquired)
		<-writerRelease
		l.mu.Unlock()
		close(writerDone)
	}()
	awaitGenerationLockSignal(t, attempting, "writer did not begin waiting")

	secondOpening := make(chan struct{})
	secondDone := make(chan struct{})
	var second sourcegateway.SearchIndexHandle
	var secondErr error
	go func() {
		close(secondOpening)
		second, secondErr = f.m.OpenSearchIndex(context.Background(), operations.SourceReadAuthority{})
		close(secondDone)
	}()
	awaitGenerationLockSignal(t, secondOpening, "second provider open did not begin")
	requireNotSignaled(t, secondDone, "second provider open bypassed the waiting writer while the first reader was open")

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writerAcquired:
	case <-secondDone:
		t.Fatal("second provider open obtained read ownership before the waiting writer")
	case <-time.After(5 * time.Second):
		t.Fatal("waiting writer did not acquire after the first handle closed")
	}
	requireNotSignaled(t, secondDone, "second provider open completed while the writer held ownership")
	close(writerRelease)
	awaitGenerationLockSignal(t, writerDone, "writer did not release ownership")
	awaitGenerationLockSignal(t, secondDone, "second provider open did not complete after writer release")
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	if second == nil {
		t.Fatal("second provider open returned a nil handle")
	}
	concrete := second.(*handle)
	release := concrete.release
	concrete.release = func() {
		f.accounting.add(&f.accounting.ownership)
		release()
	}
	requireGenerationLockUsable(t, second)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	f.accounting.require(t, 2, 2, 2, 1, 0, 0)
}

func installGenerationCleanupAccounting(t *testing.T, f *generationLockFixture) (<-chan struct{}, <-chan struct{}) {
	t.Helper()
	finalEntered := make(chan struct{}, 1)
	attemptEntered := make(chan struct{}, 1)
	oldFinal, oldAttempts := removeOwnedGeneration, removeAllOwnedGenerationAttempts
	removeOwnedGeneration = func(string, string) error {
		if f.m.lock(f.row.GenerationID).mu.TryRLock() {
			f.m.lock(f.row.GenerationID).mu.RUnlock()
			t.Error("final cleanup began without generation write ownership")
		}
		f.accounting.add(&f.accounting.writerAcquires)
		f.accounting.add(&f.accounting.finalCleanups)
		finalEntered <- struct{}{}
		return nil
	}
	removeAllOwnedGenerationAttempts = func(string, string) error {
		f.accounting.add(&f.accounting.attemptCleanups)
		attemptEntered <- struct{}{}
		return nil
	}
	t.Cleanup(func() {
		removeOwnedGeneration = oldFinal
		removeAllOwnedGenerationAttempts = oldAttempts
	})
	return finalEntered, attemptEntered
}

func TestGenerationRetirementCleanupWaitsForReader(t *testing.T) {
	f := newGenerationLockFixture(t, "retirement")
	finalEntered, attemptEntered := installGenerationCleanupAccounting(t, f)
	h := f.open(t)
	requireGenerationLockUsable(t, h)
	f.store.mu.Lock()
	f.store.active[f.row.GenerationID] = false
	f.store.mu.Unlock()

	reconcileStarted := make(chan struct{})
	reconcileDone := make(chan struct{})
	var reconcileErr error
	go func() {
		close(reconcileStarted)
		reconcileErr = f.m.reconcileGeneration(context.Background(), f.row, false, false)
		close(reconcileDone)
	}()
	awaitGenerationLockSignal(t, reconcileStarted, "retirement reconciliation did not start")
	requireNotSignaled(t, finalEntered, "final cleanup began while retirement reader was open")
	requireNotSignaled(t, attemptEntered, "attempt cleanup began while retirement reader was open")
	f.accounting.require(t, 1, 0, 0, 0, 0, 0)

	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	awaitGenerationLockSignal(t, reconcileDone, "retirement reconciliation did not finish after reader close")
	if reconcileErr != nil {
		t.Fatal(reconcileErr)
	}
	awaitGenerationLockSignal(t, finalEntered, "retirement final cleanup did not begin")
	awaitGenerationLockSignal(t, attemptEntered, "retirement attempt cleanup did not begin")
	got, err := f.store.GetSourceIndexGeneration(context.Background(), f.row.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != workflowstore.SourceIndexGenerationRetired {
		t.Fatalf("retirement state = %s", got.State)
	}
	f.accounting.require(t, 1, 1, 1, 1, 1, 1)
}

func TestGenerationCorruptionCleanupWaitsForReader(t *testing.T) {
	f := newGenerationLockFixture(t, "corruption")
	finalEntered, attemptEntered := installGenerationCleanupAccounting(t, f)
	oldVerify := verifyPublishedGeneration
	verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
		return reader.Descriptor{}, reader.ErrGenerationIntegrity
	}
	t.Cleanup(func() { verifyPublishedGeneration = oldVerify })
	h := f.open(t)
	requireGenerationLockUsable(t, h)

	reconcileStarted := make(chan struct{})
	reconcileDone := make(chan struct{})
	var reconcileErr error
	go func() {
		close(reconcileStarted)
		reconcileErr = f.m.reconcileGeneration(context.Background(), f.row, true, false)
		close(reconcileDone)
	}()
	awaitGenerationLockSignal(t, reconcileStarted, "corruption reconciliation did not start")
	requireNotSignaled(t, finalEntered, "final cleanup began while corruption reader was open")
	requireNotSignaled(t, attemptEntered, "attempt cleanup began while corruption reader was open")
	f.accounting.require(t, 1, 0, 0, 0, 0, 0)

	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	awaitGenerationLockSignal(t, reconcileDone, "corruption reconciliation did not finish after reader close")
	if reconcileErr != nil {
		t.Fatal(reconcileErr)
	}
	awaitGenerationLockSignal(t, finalEntered, "corruption final cleanup did not begin")
	awaitGenerationLockSignal(t, attemptEntered, "corruption attempt cleanup did not begin")
	got, err := f.store.GetSourceIndexGeneration(context.Background(), f.row.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != workflowstore.SourceIndexGenerationPending || len(f.m.queue) != 1 {
		t.Fatalf("corruption recovery state=%s queue=%d", got.State, len(f.m.queue))
	}
	f.accounting.require(t, 1, 1, 1, 1, 1, 1)
}
