package sourceindexruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

type ownershipStore struct {
	mu          sync.Mutex
	row         workflowstore.SourceIndexGeneration
	active      bool
	reads       int
	blockRead   int
	readEntered chan struct{}
	readRelease chan struct{}
	ready       int
	failed      int
	retries     int
	retires     int
	reactivates int
}

func (s *ownershipStore) CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	return s.row, false, nil
}

func (s *ownershipStore) GetSourceIndexGeneration(ctx context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	s.reads++
	read := s.reads
	block := read == s.blockRead
	entered, release := s.readEntered, s.readRelease
	s.mu.Unlock()
	if block {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return workflowstore.SourceIndexGeneration{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.row.GenerationID {
		return workflowstore.SourceIndexGeneration{}, workflowstore.ErrSourceIndexGenerationNotFound
	}
	return s.row, nil
}

func (s *ownershipStore) GetSourceIndexGenerationByIdentity(ctx context.Context, identity sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		return workflowstore.SourceIndexGeneration{}, err
	}
	return s.GetSourceIndexGeneration(ctx, id)
}

func (s *ownershipStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, nil
}

func (*ownershipStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	return nil, nil
}

func (*ownershipStore) ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	return nil, nil
}

func (s *ownershipStore) IsSourceIndexAuthorityActive(context.Context, sourceindex.GenerationIdentity) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, nil
}

func (s *ownershipStore) MarkSourceIndexGenerationReady(_ context.Context, p workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready++
	s.row.State = workflowstore.SourceIndexGenerationReady
	s.row.GenerationManifestSHA256 = p.GenerationManifestSHA256
	s.row.CoverageManifestSHA256 = p.CoverageManifestSHA256
	s.row.ArtifactManifestSHA256 = p.ArtifactManifestSHA256
	return s.row, nil
}

func (s *ownershipStore) MarkSourceIndexGenerationFailed(_ context.Context, p workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed++
	s.row.State = workflowstore.SourceIndexGenerationFailed
	s.row.FailureCode = p.FailureCode
	return s.row, nil
}

func (s *ownershipStore) RetrySourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retries++
	return s.row, nil
}

func (s *ownershipStore) RetireSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retires++
	return s.row, nil
}

func (s *ownershipStore) ReactivateSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reactivates++
	return s.row, nil
}

func (s *ownershipStore) replace(row workflowstore.SourceIndexGeneration) {
	s.mu.Lock()
	s.row = row
	s.mu.Unlock()
}

type ownershipEvents struct {
	mu              sync.Mutex
	order           []string
	counts          map[localBuildEvent]int
	writeReleased   chan struct{}
	continueCleanup chan struct{}
	waitStarted     chan struct{}
	writeOnce       sync.Once
	waitOnce        sync.Once
}

func newOwnershipEvents(blockCleanup bool) *ownershipEvents {
	e := &ownershipEvents{counts: map[localBuildEvent]int{}, writeReleased: make(chan struct{}), waitStarted: make(chan struct{})}
	if blockCleanup {
		e.continueCleanup = make(chan struct{})
	}
	return e
}

func (e *ownershipEvents) record(name string) {
	e.mu.Lock()
	e.order = append(e.order, name)
	e.mu.Unlock()
}

func (e *ownershipEvents) hook(_ string, event localBuildEvent) {
	e.mu.Lock()
	e.counts[event]++
	e.order = append(e.order, map[localBuildEvent]string{
		localBuildRegistered:    "registered",
		localBuildWriteReleased: "write_released",
		localBuildRemoved:       "removed",
		localBuildDoneClosed:    "done_closed",
		localBuildWaitStarted:   "wait_started",
	}[event])
	e.mu.Unlock()
	if event == localBuildWriteReleased {
		e.writeOnce.Do(func() { close(e.writeReleased) })
		if e.continueCleanup != nil {
			<-e.continueCleanup
		}
	}
	if event == localBuildWaitStarted {
		e.waitOnce.Do(func() { close(e.waitStarted) })
	}
}

type ownershipBuilder struct {
	manager       *Manager
	store         *ownershipStore
	events        *ownershipEvents
	entered       chan struct{}
	release       chan struct{}
	result        workflowstore.SourceIndexGeneration
	err           error
	mu            sync.Mutex
	calls         int
	ownersAtEntry int
	doneAtEntry   chan struct{}
	cancelSet     bool
	lockHeld      bool
	cancelled     bool
}

func (b *ownershipBuilder) BuildGeneration(ctx context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	b.manager.mu.Lock()
	local, owned := b.manager.builds[id]
	owners := len(b.manager.builds)
	b.manager.mu.Unlock()
	lockHeld := !b.manager.lock(id).mu.TryLock()
	if !lockHeld {
		b.manager.lock(id).mu.Unlock()
	}
	b.mu.Lock()
	b.calls++
	b.ownersAtEntry = owners
	b.doneAtEntry = local.done
	b.cancelSet = owned && local.cancel != nil
	b.lockHeld = lockHeld
	b.mu.Unlock()
	b.events.record("builder_entered")
	close(b.entered)
	select {
	case <-b.release:
	case <-ctx.Done():
		b.mu.Lock()
		b.cancelled = true
		b.mu.Unlock()
		b.err = ctx.Err()
	}
	if b.result.GenerationID != "" {
		b.store.replace(b.result)
	}
	b.events.record("builder_returned")
	return b.result, b.err
}

func newOwnershipManager(t *testing.T, row workflowstore.SourceIndexGeneration, events *ownershipEvents) (*Manager, *ownershipStore, *ownershipBuilder) {
	t.Helper()
	store := &ownershipStore{row: row, active: true}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		store: store, config: Config{IndexRoot: t.TempDir()}, queued: map[string]bool{}, active: map[string]bool{},
		builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 8),
		wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)), localBuildEvent: events.hook,
	}
	b := &ownershipBuilder{manager: m, store: store, events: events, entered: make(chan struct{}), release: make(chan struct{})}
	m.build = b
	t.Cleanup(cancel)
	return m, store, b
}

func requireOpen(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(message)
	default:
	}
}

func TestLocalBuildOwnershipLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result workflowstore.SourceIndexGenerationState
		err    error
		cancel bool
	}{
		{name: "success", result: workflowstore.SourceIndexGenerationReady},
		{name: "builder error", result: workflowstore.SourceIndexGenerationFailed, err: errors.New("controlled build failure")},
		{name: "cancellation", result: workflowstore.SourceIndexGenerationFailed, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity, id := lifecycleIdentity(t, "local-build-ownership-"+tc.name)
			pending := workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationPending}
			final := pending
			final.State = tc.result
			events := newOwnershipEvents(true)
			m, _, build := newOwnershipManager(t, pending, events)
			build.result, build.err = final, tc.err

			for range 8 {
				m.enqueue(id)
			}
			if got := <-m.queue; got != id {
				t.Fatalf("queued generation = %q, want %q", got, id)
			}
			runDone := make(chan struct{})
			go func() { m.runBuild(id); close(runDone) }()
			waitSignal(t, build.entered, "builder did not enter")

			build.mu.Lock()
			calls, owners, doneAtEntry, cancelSet, lockHeld := build.calls, build.ownersAtEntry, build.doneAtEntry, build.cancelSet, build.lockHeld
			build.mu.Unlock()
			if calls != 1 || owners != 1 || doneAtEntry == nil || !cancelSet || !lockHeld {
				t.Fatalf("builder entry calls=%d owners=%d done=%v cancel=%v lockHeld=%v", calls, owners, doneAtEntry != nil, cancelSet, lockHeld)
			}
			requireOpen(t, doneAtEntry, "localBuild.done closed while builder held generation ownership")
			for range 8 {
				m.enqueue(id)
			}
			if len(m.queue) != 0 {
				t.Fatalf("repeated queue pressure added %d duplicate items", len(m.queue))
			}

			writerAttempted := make(chan struct{})
			writerAcquired := make(chan struct{})
			go func() {
				close(writerAttempted)
				l := m.lock(id)
				l.mu.Lock()
				events.record("writer_acquired")
				close(writerAcquired)
				l.mu.Unlock()
			}()
			waitSignal(t, writerAttempted, "writer did not attempt generation ownership")
			requireOpen(t, writerAcquired, "writer acquired while builder held generation ownership")
			requireOpen(t, doneAtEntry, "localBuild.done closed before builder completion")

			if tc.cancel {
				m.mu.Lock()
				m.builds[id].cancel()
				m.mu.Unlock()
			} else {
				close(build.release)
			}
			waitSignal(t, events.writeReleased, "generation write ownership was not released")
			waitSignal(t, writerAcquired, "waiting writer did not acquire after build release")
			requireOpen(t, doneAtEntry, "localBuild.done closed before post-build cleanup")
			m.mu.Lock()
			_, stillOwned := m.builds[id]
			m.mu.Unlock()
			if !stillOwned {
				t.Fatal("local build ownership was removed before the write-release boundary completed")
			}
			close(events.continueCleanup)
			waitSignal(t, runDone, "runBuild did not finish")
			waitSignal(t, doneAtEntry, "localBuild.done did not close")

			m.mu.Lock()
			_, owned := m.builds[id]
			queued := m.queued[id]
			m.mu.Unlock()
			if owned || queued {
				t.Fatalf("completion left ownership=%v queued=%v", owned, queued)
			}
			events.mu.Lock()
			counts := events.counts
			order := append([]string(nil), events.order...)
			events.mu.Unlock()
			for _, event := range []localBuildEvent{localBuildRegistered, localBuildWriteReleased, localBuildRemoved, localBuildDoneClosed} {
				if counts[event] != 1 {
					t.Fatalf("event %d count = %d, want 1; order=%v", event, counts[event], order)
				}
			}
			positions := map[string]int{}
			for i, name := range order {
				positions[name] = i
			}
			want := []string{"registered", "builder_entered", "builder_returned", "write_released", "removed", "done_closed"}
			for i := 1; i < len(want); i++ {
				if positions[want[i-1]] >= positions[want[i]] {
					t.Fatalf("ownership order = %v, want %v in order", order, want)
				}
			}
			if positions["writer_acquired"] <= positions["builder_returned"] || positions["writer_acquired"] >= positions["removed"] {
				t.Fatalf("writer acquisition order = %v", order)
			}

			m.runBuild(id)
			build.mu.Lock()
			calls = build.calls
			build.mu.Unlock()
			if calls != 1 {
				t.Fatalf("completed generation builder calls = %d, want 1", calls)
			}
		})
	}
}

func TestReconcileWaitsForLocalBuildAndRereadsCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		state      workflowstore.SourceIndexGenerationState
		verifyWant int
	}{
		{name: "ready completion", state: workflowstore.SourceIndexGenerationReady, verifyWant: 1},
		{name: "failed completion", state: workflowstore.SourceIndexGenerationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity, id := lifecycleIdentity(t, "reconcile-local-"+tc.name)
			pending := workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationPending}
			stale := pending
			stale.State = workflowstore.SourceIndexGenerationBuilding
			stale.AttemptCount = 1
			stale.FailureCode = "stale"
			stale.GenerationManifestSHA256 = "stale-generation"
			stale.CoverageManifestSHA256 = "stale-coverage"
			stale.ArtifactManifestSHA256 = "stale-artifact"
			current := pending
			current.State = tc.state
			current.AttemptCount = 7
			current.FailureCode = "deterministic"
			current.GenerationManifestSHA256 = "current-generation"
			current.CoverageManifestSHA256 = "current-coverage"
			current.ArtifactManifestSHA256 = "current-artifact"

			events := newOwnershipEvents(true)
			m, store, build := newOwnershipManager(t, pending, events)
			build.result = current
			runDone := make(chan struct{})
			go func() { m.runBuild(id); close(runDone) }()
			waitSignal(t, build.entered, "builder did not enter")
			m.mu.Lock()
			local := m.builds[id]
			m.mu.Unlock()

			var verifyMu sync.Mutex
			verifyCalls := 0
			oldVerify := verifyPublishedGeneration
			verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
				verifyMu.Lock()
				verifyCalls++
				verifyMu.Unlock()
				return reader.Descriptor{GenerationManifestSHA256: "current-generation", CoverageManifestSHA256: "current-coverage", ArtifactManifestSHA256: "current-artifact"}, nil
			}
			t.Cleanup(func() { verifyPublishedGeneration = oldVerify })

			reconcileDone := make(chan struct{})
			var reconcileErr error
			go func() {
				reconcileErr = m.reconcileGeneration(context.Background(), stale, true, false)
				close(reconcileDone)
			}()
			waitSignal(t, events.waitStarted, "reconciliation did not detect local ownership")
			requireOpen(t, reconcileDone, "reconciliation completed while local build was active")
			requireOpen(t, local.done, "localBuild.done closed while builder was blocked")
			verifyMu.Lock()
			beforeVerify := verifyCalls
			verifyMu.Unlock()
			store.mu.Lock()
			beforeReads := store.reads
			beforeLifecycle := store.ready + store.failed + store.retries + store.retires + store.reactivates
			store.mu.Unlock()
			build.mu.Lock()
			beforeBuilds := build.calls
			build.mu.Unlock()
			if beforeVerify != 0 || beforeLifecycle != 0 || beforeBuilds != 1 || beforeReads != 2 || len(m.queue) != 0 {
				t.Fatalf("active local work verify=%d lifecycle=%d builds=%d reads=%d queue=%d", beforeVerify, beforeLifecycle, beforeBuilds, beforeReads, len(m.queue))
			}

			store.replace(current)
			store.mu.Lock()
			store.blockRead = 3
			store.readEntered = make(chan struct{})
			store.readRelease = make(chan struct{})
			readEntered, readRelease := store.readEntered, store.readRelease
			store.mu.Unlock()
			close(build.release)
			waitSignal(t, events.writeReleased, "build did not release generation ownership")

			writerAcquired := make(chan struct{})
			writerRelease := make(chan struct{})
			go func() {
				l := m.lock(id)
				l.mu.Lock()
				close(writerAcquired)
				<-writerRelease
				l.mu.Unlock()
			}()
			waitSignal(t, writerAcquired, "independent writer could not acquire while reconciliation waited")
			requireOpen(t, local.done, "localBuild.done closed before ownership removal")
			close(events.continueCleanup)
			waitSignal(t, local.done, "localBuild.done did not close")
			waitSignal(t, runDone, "runBuild did not finish")
			requireOpen(t, readEntered, "reconciliation reread before reacquiring generation ownership")
			close(writerRelease)
			waitSignal(t, readEntered, "reconciliation did not reread after localBuild.done closed")
			if m.lock(id).mu.TryLock() {
				m.lock(id).mu.Unlock()
				t.Fatal("reconciliation reread without generation write ownership")
			}
			close(readRelease)
			waitSignal(t, reconcileDone, "reconciliation did not finish after current-row reread")
			if reconcileErr != nil {
				t.Fatal(reconcileErr)
			}

			verifyMu.Lock()
			gotVerify := verifyCalls
			verifyMu.Unlock()
			store.mu.Lock()
			reads := store.reads
			lifecycle := store.ready + store.failed + store.retries + store.retires + store.reactivates
			store.mu.Unlock()
			build.mu.Lock()
			calls := build.calls
			build.mu.Unlock()
			if reads != 3 || gotVerify != tc.verifyWant || lifecycle != 0 || calls != 1 || len(m.queue) != 0 {
				t.Fatalf("completion reads=%d verify=%d lifecycle=%d builds=%d queue=%d", reads, gotVerify, lifecycle, calls, len(m.queue))
			}
		})
	}
}
