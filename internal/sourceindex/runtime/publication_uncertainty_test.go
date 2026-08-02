package sourceindexruntime

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

type publicationStore struct {
	row           workflowstore.SourceIndexGeneration
	active        bool
	events        []string
	ready         int
	failed        int
	retries       int
	retires       int
	reactivates   int
	readyErr      error
	failedErr     error
	retryErr      error
	retireErr     error
	reactivateErr error
}

func (s *publicationStore) CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	return s.row, false, nil
}

func (s *publicationStore) GetSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	if id != s.row.GenerationID {
		return workflowstore.SourceIndexGeneration{}, workflowstore.ErrSourceIndexGenerationNotFound
	}
	return s.row, nil
}

func (s *publicationStore) GetSourceIndexGenerationByIdentity(context.Context, sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	return s.row, nil
}

func (s *publicationStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.events = append(s.events, "build")
	return s.row, nil
}

func (s *publicationStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	return []workflowstore.SourceIndexGeneration{s.row}, nil
}

func (*publicationStore) ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	return nil, nil
}

func (s *publicationStore) IsSourceIndexAuthorityActive(context.Context, sourceindex.GenerationIdentity) (bool, error) {
	return s.active, nil
}

func (s *publicationStore) MarkSourceIndexGenerationReady(_ context.Context, p workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	s.ready++
	s.events = append(s.events, "ready")
	if s.readyErr != nil {
		return workflowstore.SourceIndexGeneration{}, s.readyErr
	}
	s.row.State = workflowstore.SourceIndexGenerationReady
	s.row.GenerationManifestSHA256 = p.GenerationManifestSHA256
	s.row.CoverageManifestSHA256 = p.CoverageManifestSHA256
	s.row.ArtifactManifestSHA256 = p.ArtifactManifestSHA256
	return s.row, nil
}

func (s *publicationStore) MarkSourceIndexGenerationFailed(_ context.Context, p workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	s.failed++
	s.events = append(s.events, "failed")
	if s.failedErr != nil {
		return workflowstore.SourceIndexGeneration{}, s.failedErr
	}
	s.row.State = workflowstore.SourceIndexGenerationFailed
	s.row.FailureCode = p.FailureCode
	s.row.FailureMessage = p.FailureMessage
	return s.row, nil
}

func (s *publicationStore) RetrySourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.retries++
	s.events = append(s.events, "retry")
	if s.retryErr != nil {
		return workflowstore.SourceIndexGeneration{}, s.retryErr
	}
	s.row.State = workflowstore.SourceIndexGenerationPending
	return s.row, nil
}

func (s *publicationStore) RetireSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.retires++
	s.events = append(s.events, "retire")
	if s.retireErr != nil {
		return workflowstore.SourceIndexGeneration{}, s.retireErr
	}
	s.row.State = workflowstore.SourceIndexGenerationRetired
	return s.row, nil
}

func (s *publicationStore) ReactivateSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.reactivates++
	s.events = append(s.events, "reactivate")
	if s.reactivateErr != nil {
		return workflowstore.SourceIndexGeneration{}, s.reactivateErr
	}
	s.row.State = workflowstore.SourceIndexGenerationPending
	return s.row, nil
}

type publicationBuilder struct{ calls int }

func (b *publicationBuilder) BuildGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	b.calls++
	return workflowstore.SourceIndexGeneration{}, errors.New("publication recovery invoked builder")
}

type publicationHarness struct {
	store             *publicationStore
	manager           *Manager
	builder           *publicationBuilder
	descriptor        reader.Descriptor
	verifyErr         error
	verifyCalls       int
	finalCleanup      int
	attemptCleanup    int
	finalCleanupErr   error
	attemptCleanupErr error
}

func newPublicationHarness(t *testing.T, active bool) *publicationHarness {
	t.Helper()
	identity, id := lifecycleIdentity(t, "publication-uncertainty")
	row := workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationBuilding, AttemptCount: 1}
	store := &publicationStore{row: row, active: active}
	build := &publicationBuilder{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &publicationHarness{
		store:   store,
		builder: build,
		descriptor: reader.Descriptor{
			GenerationID:             id,
			Identity:                 identity,
			GenerationManifestSHA256: "verified-generation-digest",
			CoverageManifestSHA256:   "verified-coverage-digest",
			ArtifactManifestSHA256:   "verified-artifact-digest",
		},
	}
	h.manager = &Manager{
		store: store, build: build, config: Config{}, queued: map[string]bool{}, active: map[string]bool{},
		builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 4),
		wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}), logger: slog.Default(),
	}
	oldVerify, oldFinal, oldAttempts := verifyPublishedGeneration, removeOwnedGeneration, removeAllOwnedGenerationAttempts
	verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
		h.verifyCalls++
		h.store.events = append(h.store.events, "verify")
		return h.descriptor, h.verifyErr
	}
	removeOwnedGeneration = func(string, string) error {
		h.finalCleanup++
		h.store.events = append(h.store.events, "remove-final")
		return h.finalCleanupErr
	}
	removeAllOwnedGenerationAttempts = func(string, string) error {
		h.attemptCleanup++
		h.store.events = append(h.store.events, "remove-attempts")
		return h.attemptCleanupErr
	}
	t.Cleanup(func() {
		verifyPublishedGeneration = oldVerify
		removeOwnedGeneration = oldFinal
		removeAllOwnedGenerationAttempts = oldAttempts
	})
	return h
}

func (h *publicationHarness) reconcile() error {
	return h.manager.reconcileGeneration(context.Background(), h.store.row, h.store.active, false)
}

func (h *publicationHarness) reservations() int {
	return len(h.manager.queue)
}

func (h *publicationHarness) requireNoBuilder(t *testing.T) {
	t.Helper()
	if h.builder.calls != 0 {
		t.Fatalf("builder calls = %d, want 0", h.builder.calls)
	}
}

func TestPublishedBuildingActivePublicationIsAdopted(t *testing.T) {
	h := newPublicationHarness(t, true)
	if err := h.reconcile(); err != nil {
		t.Fatal(err)
	}
	if h.store.row.State != workflowstore.SourceIndexGenerationReady {
		t.Fatalf("state = %s, want ready", h.store.row.State)
	}
	if h.store.row.GenerationManifestSHA256 != h.descriptor.GenerationManifestSHA256 ||
		h.store.row.CoverageManifestSHA256 != h.descriptor.CoverageManifestSHA256 ||
		h.store.row.ArtifactManifestSHA256 != h.descriptor.ArtifactManifestSHA256 {
		t.Fatalf("persisted digests = %q/%q/%q", h.store.row.GenerationManifestSHA256, h.store.row.CoverageManifestSHA256, h.store.row.ArtifactManifestSHA256)
	}
	if h.verifyCalls != 1 || h.store.ready != 1 || h.store.failed != 0 || h.store.retries != 0 || h.reservations() != 0 || h.finalCleanup != 0 || h.attemptCleanup != 0 {
		t.Fatalf("verify=%d ready=%d failed=%d retry=%d reservations=%d cleanup=%d/%d", h.verifyCalls, h.store.ready, h.store.failed, h.store.retries, h.reservations(), h.finalCleanup, h.attemptCleanup)
	}
	h.requireNoBuilder(t)
}

func TestPublishedBuildingInactivePublicationIsAdoptedThenRetired(t *testing.T) {
	h := newPublicationHarness(t, false)
	if err := h.reconcile(); err != nil {
		t.Fatal(err)
	}
	wantEvents := []string{"verify", "ready", "retire", "remove-final", "remove-attempts"}
	if !reflect.DeepEqual(h.store.events, wantEvents) {
		t.Fatalf("events = %v, want %v", h.store.events, wantEvents)
	}
	if h.store.row.State != workflowstore.SourceIndexGenerationRetired || h.store.ready != 1 || h.store.retires != 1 || h.store.retries != 0 || h.reservations() != 0 {
		t.Fatalf("state=%s ready=%d retire=%d retry=%d reservations=%d", h.store.row.State, h.store.ready, h.store.retires, h.store.retries, h.reservations())
	}
	h.requireNoBuilder(t)
}

func TestPublishedBuildingOperationalVerificationErrorPreservesPublication(t *testing.T) {
	h := newPublicationHarness(t, true)
	want := errors.New("verification storage unavailable")
	h.verifyErr = want
	err := h.reconcile()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want errors.Is sentinel", err)
	}
	if h.store.row.State != workflowstore.SourceIndexGenerationBuilding || h.finalCleanup != 0 || h.attemptCleanup != 0 || h.store.failed != 0 || h.store.retries != 0 || h.reservations() != 0 {
		t.Fatalf("state=%s cleanup=%d/%d failed=%d retry=%d reservations=%d", h.store.row.State, h.finalCleanup, h.attemptCleanup, h.store.failed, h.store.retries, h.reservations())
	}
	h.requireNoBuilder(t)
}

func TestPublishedBuildingIntegrityFailureCleansAndQueuesRetry(t *testing.T) {
	h := newPublicationHarness(t, true)
	h.verifyErr = reader.ErrGenerationIntegrity
	if err := h.reconcile(); err != nil {
		t.Fatal(err)
	}
	wantEvents := []string{"verify", "remove-final", "remove-attempts", "failed", "retry"}
	if !reflect.DeepEqual(h.store.events, wantEvents) {
		t.Fatalf("events = %v, want %v", h.store.events, wantEvents)
	}
	if h.store.row.State != workflowstore.SourceIndexGenerationPending || h.store.failed != 1 || h.store.retries != 1 || h.reservations() != 1 {
		t.Fatalf("state=%s failed=%d retry=%d reservations=%d", h.store.row.State, h.store.failed, h.store.retries, h.reservations())
	}
	h.requireNoBuilder(t)
}

func TestPublishedBuildingMutationFailuresPreserveClassification(t *testing.T) {
	t.Run("ready mutation", func(t *testing.T) {
		h := newPublicationHarness(t, true)
		want := errors.New("ready persistence failed")
		h.store.readyErr = want
		if err := h.reconcile(); !errors.Is(err, want) {
			t.Fatalf("error = %v, want errors.Is sentinel", err)
		}
		if h.store.row.State != workflowstore.SourceIndexGenerationBuilding || h.store.failed != 0 || h.store.retries != 0 || h.store.retires != 0 || h.store.reactivates != 0 || h.finalCleanup != 0 || h.attemptCleanup != 0 || h.reservations() != 0 {
			t.Fatalf("state=%s failed=%d retry=%d retire=%d reactivate=%d cleanup=%d/%d reservations=%d", h.store.row.State, h.store.failed, h.store.retries, h.store.retires, h.store.reactivates, h.finalCleanup, h.attemptCleanup, h.reservations())
		}
		h.requireNoBuilder(t)
	})

	t.Run("retirement mutation", func(t *testing.T) {
		h := newPublicationHarness(t, false)
		want := errors.New("retirement persistence failed")
		h.store.retireErr = want
		if err := h.reconcile(); !errors.Is(err, want) {
			t.Fatalf("error = %v, want errors.Is sentinel", err)
		}
		if h.store.row.State != workflowstore.SourceIndexGenerationReady || h.store.ready != 1 || h.store.failed != 0 || h.store.retries != 0 || h.store.retires != 1 || h.store.reactivates != 0 || h.finalCleanup != 0 || h.attemptCleanup != 0 || h.reservations() != 0 {
			t.Fatalf("state=%s ready=%d failed=%d retry=%d retire=%d reactivate=%d cleanup=%d/%d reservations=%d", h.store.row.State, h.store.ready, h.store.failed, h.store.retries, h.store.retires, h.store.reactivates, h.finalCleanup, h.attemptCleanup, h.reservations())
		}
		h.requireNoBuilder(t)
	})

	t.Run("cleanup after retirement", func(t *testing.T) {
		h := newPublicationHarness(t, false)
		want := errors.New("final cleanup failed")
		h.finalCleanupErr = want
		if err := h.reconcile(); !errors.Is(err, want) {
			t.Fatalf("error = %v, want errors.Is sentinel", err)
		}
		if h.store.row.State != workflowstore.SourceIndexGenerationRetired || h.finalCleanup != 1 || h.store.reactivates != 0 || h.store.retries != 0 || h.reservations() != 0 || h.attemptCleanup != 0 {
			t.Fatalf("state=%s final-cleanup=%d reactivate=%d retry=%d reservations=%d attempt-cleanup=%d", h.store.row.State, h.finalCleanup, h.store.reactivates, h.store.retries, h.reservations(), h.attemptCleanup)
		}
		h.requireNoBuilder(t)
	})
}
