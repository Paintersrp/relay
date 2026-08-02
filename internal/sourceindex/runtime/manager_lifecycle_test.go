package sourceindexruntime

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"relay/internal/sourceindex"
	workflowstore "relay/internal/store/workflow"
)

// lifecycleStore is deliberately in-memory: these tests prove manager lock and
// queue ownership, not workflow-store persistence.
type lifecycleStore struct {
	mu     sync.Mutex
	rows   map[string]workflowstore.SourceIndexGeneration
	active bool
}

func (s *lifecycleStore) CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	return workflowstore.SourceIndexGeneration{}, false, nil
}
func (s *lifecycleStore) GetSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[id], nil
}
func (s *lifecycleStore) GetSourceIndexGenerationByIdentity(context.Context, sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, workflowstore.ErrSourceIndexGenerationNotFound
}
func (s *lifecycleStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *lifecycleStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	return nil, nil
}
func (s *lifecycleStore) ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	return nil, nil
}
func (s *lifecycleStore) IsSourceIndexAuthorityActive(context.Context, sourceindex.GenerationIdentity) (bool, error) {
	return s.active, nil
}
func (s *lifecycleStore) MarkSourceIndexGenerationReady(context.Context, workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *lifecycleStore) MarkSourceIndexGenerationFailed(context.Context, workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *lifecycleStore) RetrySourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *lifecycleStore) RetireSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}
func (s *lifecycleStore) ReactivateSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

type blockingBuilder struct {
	started chan string
	release chan struct{}
	store   *lifecycleStore
}

func (b *blockingBuilder) BuildGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	b.started <- id
	<-b.release
	b.store.mu.Lock()
	row := b.store.rows[id]
	row.State = workflowstore.SourceIndexGenerationReady
	b.store.rows[id] = row
	b.store.mu.Unlock()
	return row, nil
}

func lifecycleIdentity(t *testing.T, vault string) (sourceindex.GenerationIdentity, string) {
	t.Helper()
	digest, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity(vault, "0123456789abcdef0123456789abcdef01234567", "89abcdef0123456789abcdef0123456789abcdef", digest)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return identity, id
}

func newLifecycleManager(store *lifecycleStore, build builder) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{store: store, build: build, config: Config{}, queued: map[string]bool{}, active: map[string]bool{}, builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 4), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, logger: slog.Default()}
}

func TestLocalBuildDoneClosesAfterGenerationWriteLock(t *testing.T) {
	identity, id := lifecycleIdentity(t, "vault")
	store := &lifecycleStore{rows: map[string]workflowstore.SourceIndexGeneration{id: {GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationPending}}, active: true}
	build := &blockingBuilder{started: make(chan string, 1), release: make(chan struct{}), store: store}
	m := newLifecycleManager(store, build)
	finished := make(chan struct{})
	go func() { m.runBuild(id); close(finished) }()
	<-build.started
	m.mu.Lock()
	local := m.builds[id]
	m.mu.Unlock()
	if local.done == nil {
		t.Fatal("local build was not registered")
	}
	close(build.release)
	select {
	case <-local.done:
		// Acquiring the write lock after done verifies reconciliation can take
		// ownership only after the local lifecycle has completed.
		lock := m.lock(id)
		lock.mu.Lock()
		lock.mu.Unlock()
	case <-time.After(time.Second):
		t.Fatal("build did not complete")
	}
	<-finished
}

func TestDuplicateEnqueueCollapsesAndDistinctGenerationsBuildConcurrently(t *testing.T) {
	identityA, idA := lifecycleIdentity(t, "vault-a")
	identityB, idB := lifecycleIdentity(t, "vault-b")
	store := &lifecycleStore{rows: map[string]workflowstore.SourceIndexGeneration{
		idA: {GenerationID: idA, Identity: identityA, State: workflowstore.SourceIndexGenerationPending},
		idB: {GenerationID: idB, Identity: identityB, State: workflowstore.SourceIndexGenerationPending},
	}, active: true}
	build := &blockingBuilder{started: make(chan string, 2), release: make(chan struct{}), store: store}
	m := newLifecycleManager(store, build)
	m.enqueue(idA)
	m.enqueue(idA)
	m.enqueue(idB)
	if got := len(m.queue); got != 2 {
		t.Fatalf("queued entries = %d, want 2", got)
	}
	finished := make(chan struct{}, 2)
	go func() { m.runBuild(<-m.queue); finished <- struct{}{} }()
	go func() { m.runBuild(<-m.queue); finished <- struct{}{} }()
	first, second := <-build.started, <-build.started
	if first == second {
		t.Fatalf("same generation built twice: %q", first)
	}
	close(build.release)
	<-finished
	<-finished
}
