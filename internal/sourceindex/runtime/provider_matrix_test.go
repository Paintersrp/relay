package sourceindexruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"relay/internal/app/operations"
	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	"relay/internal/sourceindex/supervisor"
	workflowstore "relay/internal/store/workflow"
)

// providerStore controls just the reads made by OpenSearchIndex; embedded
// runtimeStore supplies the unrelated Store methods.
type providerStore struct {
	*runtimeStore
	mu                    sync.Mutex
	row                   workflowstore.SourceIndexGeneration
	createErr, currentErr error
	active                bool
	activeErr             error
	create                func(context.Context) (workflowstore.SourceIndexGeneration, error)
	current               func(context.Context, string) (workflowstore.SourceIndexGeneration, error)
	authority             func(context.Context, sourceindex.GenerationIdentity) (bool, error)
}

func (s *providerStore) CreateOrResolveSourceIndexGeneration(ctx context.Context, _ workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	if s.create != nil {
		r, err := s.create(ctx)
		return r, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.row, false, s.createErr
}
func (s *providerStore) GetSourceIndexGeneration(ctx context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	if s.current != nil {
		return s.current(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.row, s.currentErr
}
func (s *providerStore) IsSourceIndexAuthorityActive(ctx context.Context, x sourceindex.GenerationIdentity) (bool, error) {
	if s.authority != nil {
		return s.authority(ctx, x)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.activeErr
}

type providerAuthority struct {
	resolve func(context.Context) (sourceindex.GenerationIdentity, error)
}

func (a providerAuthority) ResolveSourceIndexIdentity(ctx context.Context, _ workflowstore.OperationPacketVaultRelationship) (sourceindex.GenerationIdentity, error) {
	return a.resolve(ctx)
}
func (providerAuthority) AcquireSourceIndexLease(context.Context, sourceindex.GenerationIdentity) (supervisor.SourceLease, error) {
	return providerLease{}, nil
}

type providerLease struct{}

func (providerLease) RepositoryPath() string { return "" }
func (providerLease) Close() error           { return nil }

type providerBuilder struct {
	mu    sync.Mutex
	calls int
}

func (b *providerBuilder) BuildGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return workflowstore.SourceIndexGeneration{}, errors.New("provider invoked builder")
}
func (b *providerBuilder) count() int { b.mu.Lock(); defer b.mu.Unlock(); return b.calls }

type providerFixture struct {
	m        *Manager
	store    *providerStore
	identity sourceindex.GenerationIdentity
	id       string
	build    *providerBuilder
	reader   *controlledReader
	opens    int
	openErr  error
}

func newProviderFixture(t *testing.T, state workflowstore.SourceIndexGenerationState) *providerFixture {
	t.Helper()
	identity, id := lifecycleIdentity(t, "provider-"+string(state))
	store := &providerStore{runtimeStore: &runtimeStore{}, row: workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: state}, active: true}
	ctx, cancel := context.WithCancel(context.Background())
	build := &providerBuilder{}
	f := &providerFixture{store: store, identity: identity, id: id, build: build, reader: &controlledReader{query: func(context.Context) ([]reader.Candidate, error) { return nil, nil }}}
	f.m = &Manager{store: store, authority: providerAuthority{resolve: func(context.Context) (sourceindex.GenerationIdentity, error) { return identity, nil }}, config: Config{QueryTimeout: time.Second}, build: build, started: true, queued: map[string]bool{}, active: map[string]bool{}, builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 8), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}), logger: slog.Default()}
	t.Cleanup(cancel)
	old := openGenerationReader
	openGenerationReader = func(context.Context, reader.GenerationStore, reader.Config, sourceindex.GenerationIdentity) (generationReader, error) {
		f.opens++
		if f.openErr != nil {
			return nil, f.openErr
		}
		return f.reader, nil
	}
	t.Cleanup(func() { openGenerationReader = old })
	return f
}

func (f *providerFixture) open(t *testing.T, ctx context.Context) (sourcegatewayHandle, error) {
	t.Helper()
	h, err := f.m.OpenSearchIndex(ctx, operations.SourceReadAuthority{})
	return sourcegatewayHandle{h}, err
}

// sourcegatewayHandle keeps the matrix assertions independent of the concrete
// public handle type while still exercising the real provider method.
type sourcegatewayHandle struct{ h interface{ Close() error } }

func (h sourcegatewayHandle) nil() bool { return h.h == nil }
func (f *providerFixture) assertNoBuild(t *testing.T) {
	t.Helper()
	if n := f.build.count(); n != 0 {
		t.Fatalf("synchronous builder calls = %d", n)
	}
}
func (f *providerFixture) assertReleased(t *testing.T) {
	t.Helper()
	l := f.m.lock(f.id)
	if !l.mu.TryLock() {
		t.Fatal("generation read ownership was not released")
	}
	l.mu.Unlock()
}
func requireUnavailable(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, reader.ErrGenerationUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
}
func requireIntegrity(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, reader.ErrGenerationIntegrity) {
		t.Fatalf("error = %v, want integrity", err)
	}
}

func TestOpenSearchIndexProviderStateMatrix(t *testing.T) {
	t.Run("PROV-01 open before Start", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		f.m.started = false
		h, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if !h.nil() || f.opens != 0 {
			t.Fatal("provider opened a reader before start")
		}
		f.assertNoBuild(t)
		f.assertReleased(t)
	})
	t.Run("PROV-02 exact ready generation opens", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		h, err := f.open(t, context.Background())
		if err != nil || h.nil() || f.opens != 1 || len(f.m.wake) != 0 {
			t.Fatalf("handle=%v opens=%d wake=%d err=%v", h.nil(), f.opens, len(f.m.wake), err)
		}
		f.assertNoBuild(t)
		if err := h.h.Close(); err != nil || f.reader.closeCalls != 1 {
			t.Fatalf("close=%v calls=%d", err, f.reader.closeCalls)
		}
		f.assertReleased(t)
	})
	t.Run("PROV-03 pending queues once without building", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationPending)
		for range 2 {
			h, err := f.open(t, context.Background())
			requireUnavailable(t, err)
			if !h.nil() {
				t.Fatal("handle returned")
			}
		}
		if len(f.m.queue) != 1 || f.opens != 0 {
			t.Fatalf("queue=%d opens=%d", len(f.m.queue), f.opens)
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-04 building is unavailable without wake", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationBuilding)
		_, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if f.opens != 0 || len(f.m.wake) != 0 {
			t.Fatal("building opened reader or woke reconciliation")
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-05 transient failed wakes reconciliation", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationFailed)
		f.store.row.FailureCode = "cancelled"
		_, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if f.opens != 0 || len(f.m.wake) != 1 {
			t.Fatal("transient failure did not use production wake behavior")
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-06 deterministic failed does not queue or wake", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationFailed)
		f.store.row.FailureCode = "deterministic"
		_, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if len(f.m.queue) != 0 || len(f.m.wake) != 0 {
			t.Fatal("deterministic failure scheduled work")
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-07 retired wakes reconciliation", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationRetired)
		_, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if f.opens != 0 || len(f.m.wake) != 1 {
			t.Fatal("retired generation did not use production wake behavior")
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-08 inactive authority releases ownership", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		f.store.active = false
		h, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if !h.nil() || f.opens != 0 {
			t.Fatal("inactive authority opened reader")
		}
		f.assertReleased(t)
		f.assertNoBuild(t)
	})
	t.Run("PROV-09 identity collision is integrity", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		other, _ := lifecycleIdentity(t, "provider-collision")
		f.store.row.Identity = other
		_, err := f.open(t, context.Background())
		requireIntegrity(t, err)
		if f.opens != 0 {
			t.Fatal("collision opened reader")
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-10 malformed persisted generation is integrity", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		f.store.row.Identity.BuildOptionsSHA256 = "bad"
		_, err := f.open(t, context.Background())
		requireIntegrity(t, err)
		if f.opens != 0 {
			t.Fatal("malformed row opened reader")
		}
		f.assertNoBuild(t)
	})
}

func TestOpenSearchIndexProviderErrorsAndCancellation(t *testing.T) {
	t.Run("PROV-11 workflow store errors are preserved", func(t *testing.T) {
		for _, stage := range []string{"resolution", "current row", "authority"} {
			t.Run(stage, func(t *testing.T) {
				f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
				want := errors.New(stage)
				switch stage {
				case "resolution":
					f.store.createErr = want
				case "current row":
					f.store.currentErr = want
				default:
					f.store.activeErr = want
				}
				_, err := f.open(t, context.Background())
				if !errors.Is(err, want) {
					t.Fatalf("error=%v", err)
				}
				if stage != "resolution" {
					f.assertReleased(t)
				}
				f.assertNoBuild(t)
			})
		}
	})
	t.Run("PROV-12 SourceVault operational error is preserved", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		want := errors.New("vault operational")
		f.m.authority = providerAuthority{resolve: func(context.Context) (sourceindex.GenerationIdentity, error) {
			return sourceindex.GenerationIdentity{}, want
		}}
		_, err := f.open(t, context.Background())
		if !errors.Is(err, want) {
			t.Fatalf("error=%v", err)
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-13 caller cancellation is preserved at every provider stage", func(t *testing.T) {
		t.Run("before provider", func(t *testing.T) {
			f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := f.open(t, ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
			f.assertNoBuild(t)
		})
		for _, stage := range []string{"identity", "generation", "authority", "reader"} {
			t.Run("during "+stage, func(t *testing.T) {
				f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
				entered := make(chan struct{}, 1)
				block := func(ctx context.Context) error { entered <- struct{}{}; <-ctx.Done(); return ctx.Err() }
				switch stage {
				case "identity":
					f.m.authority = providerAuthority{resolve: func(ctx context.Context) (sourceindex.GenerationIdentity, error) {
						return sourceindex.GenerationIdentity{}, block(ctx)
					}}
				case "generation":
					f.store.create = func(ctx context.Context) (workflowstore.SourceIndexGeneration, error) {
						return workflowstore.SourceIndexGeneration{}, block(ctx)
					}
				case "authority":
					f.store.authority = func(ctx context.Context, _ sourceindex.GenerationIdentity) (bool, error) { return false, block(ctx) }
				case "reader":
					old := openGenerationReader
					openGenerationReader = func(ctx context.Context, _ reader.GenerationStore, _ reader.Config, _ sourceindex.GenerationIdentity) (generationReader, error) {
						return nil, block(ctx)
					}
					t.Cleanup(func() { openGenerationReader = old })
				}
				ctx, cancel := context.WithCancel(context.Background())
				result := make(chan error, 1)
				go func() { _, err := f.open(t, ctx); result <- err }()
				<-entered
				cancel()
				err := <-result
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v", err)
				}
				if stage == "authority" || stage == "reader" {
					f.assertReleased(t)
				}
				f.assertNoBuild(t)
			})
		}
	})
	t.Run("PROV-14 reader integrity wakes and releases", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		f.openErr = reader.ErrGenerationIntegrity
		h, err := f.open(t, context.Background())
		requireIntegrity(t, err)
		if !h.nil() || len(f.m.wake) != 1 {
			t.Fatal("integrity reader failure did not wake")
		}
		f.assertReleased(t)
		f.assertNoBuild(t)
	})
}

func TestOpenSearchIndexProviderCurrentRowAndShutdown(t *testing.T) {
	t.Run("PROV-15 stable stopping state releases ownership", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		old := beforeProviderFinalStateCheck
		beforeProviderFinalStateCheck = func() { f.m.mu.Lock(); f.m.stopping = true; f.m.mu.Unlock() }
		t.Cleanup(func() { beforeProviderFinalStateCheck = old })
		h, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if !h.nil() || f.reader.closeCalls != 1 {
			t.Fatal("stopping provider retained handle or reader")
		}
		f.assertReleased(t)
		f.assertNoBuild(t)
	})
	t.Run("PROV-16 completed shutdown is unavailable", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		f.m.stopping = true
		h, err := f.open(t, context.Background())
		requireUnavailable(t, err)
		if !h.nil() || f.opens != 0 {
			t.Fatal("shutdown provider opened reader")
		}
		f.assertNoBuild(t)
	})
	t.Run("PROV-18 current row state wins and PROV-20 releases ownership", func(t *testing.T) {
		for _, state := range []workflowstore.SourceIndexGenerationState{workflowstore.SourceIndexGenerationPending, workflowstore.SourceIndexGenerationBuilding, workflowstore.SourceIndexGenerationFailed, workflowstore.SourceIndexGenerationRetired} {
			t.Run(string(state), func(t *testing.T) {
				f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
				changed := f.store.row
				changed.State = state
				f.store.current = func(context.Context, string) (workflowstore.SourceIndexGeneration, error) { return changed, nil }
				_, err := f.open(t, context.Background())
				requireUnavailable(t, err)
				if f.opens != 0 {
					t.Fatal("changed row opened reader")
				}
				f.assertReleased(t)
				f.assertNoBuild(t)
			})
		}
	})
	t.Run("PROV-19 current row identity mismatch is integrity", func(t *testing.T) {
		f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
		changed := f.store.row
		changed.Identity, _ = lifecycleIdentity(t, "provider-current-mismatch")
		changed.GenerationID, _ = sourceindex.GenerationID(changed.Identity)
		f.store.current = func(context.Context, string) (workflowstore.SourceIndexGeneration, error) { return changed, nil }
		_, err := f.open(t, context.Background())
		requireIntegrity(t, err)
		if f.opens != 0 {
			t.Fatal("mismatched row opened reader")
		}
		f.assertReleased(t)
		f.assertNoBuild(t)
	})
	t.Run("PROV-20 current row and reader operational failures release ownership", func(t *testing.T) {
		for _, stage := range []string{"current row", "reader"} {
			t.Run(stage, func(t *testing.T) {
				f := newProviderFixture(t, workflowstore.SourceIndexGenerationReady)
				want := errors.New(stage)
				if stage == "current row" {
					f.store.currentErr = want
				} else {
					f.openErr = want
				}
				_, err := f.open(t, context.Background())
				if !errors.Is(err, want) {
					t.Fatalf("error=%v", err)
				}
				f.assertReleased(t)
				f.assertNoBuild(t)
			})
		}
	})
}
