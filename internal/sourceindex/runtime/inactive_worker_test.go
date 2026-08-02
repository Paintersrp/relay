//go:build linux

package sourceindexruntime

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"relay/internal/sourceindex"
	workflowstore "relay/internal/store/workflow"
)

type inactiveWorkerStore struct {
	row        workflowstore.SourceIndexGeneration
	active     bool
	retireCall chan struct{}
}

func (s *inactiveWorkerStore) CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	return s.row, false, nil
}
func (s *inactiveWorkerStore) GetSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return s.row, nil
}
func (s *inactiveWorkerStore) GetSourceIndexGenerationByIdentity(context.Context, sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	return s.row, nil
}
func (s *inactiveWorkerStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, errors.New("builder must not be called")
}
func (s *inactiveWorkerStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	return []workflowstore.SourceIndexGeneration{s.row}, nil
}
func (s *inactiveWorkerStore) ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	return nil, nil
}
func (s *inactiveWorkerStore) IsSourceIndexAuthorityActive(context.Context, sourceindex.GenerationIdentity) (bool, error) {
	return s.active, nil
}
func (s *inactiveWorkerStore) MarkSourceIndexGenerationReady(context.Context, workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, errors.New("ready mutation must not be called")
}
func (s *inactiveWorkerStore) MarkSourceIndexGenerationFailed(context.Context, workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, errors.New("failure mutation must not be called")
}
func (s *inactiveWorkerStore) RetrySourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, errors.New("retry mutation must not be called")
}
func (s *inactiveWorkerStore) ReactivateSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	return workflowstore.SourceIndexGeneration{}, errors.New("reactivation must not be called")
}
func (s *inactiveWorkerStore) RetireSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	s.row.State = workflowstore.SourceIndexGenerationRetired
	select {
	case s.retireCall <- struct{}{}:
	default:
	}
	return s.row, nil
}

type forbiddenBuild struct{ called chan struct{} }

func (b *forbiddenBuild) BuildGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	b.called <- struct{}{}
	return workflowstore.SourceIndexGeneration{}, errors.New("builder called")
}

func TestInactiveQueuedWorkerDoesNotBuildOrDeadlock(t *testing.T) {
	options, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity("vault", "0123456789abcdef0123456789abcdef01234567", "89abcdef0123456789abcdef0123456789abcdef", options)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	store := &inactiveWorkerStore{row: workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationPending}, active: false, retireCall: make(chan struct{}, 1)}
	builder := &forbiddenBuild{called: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{store: store, build: builder, config: Config{IndexRoot: filepath.Join(t.TempDir(), "index")}, queued: map[string]bool{id: true}, active: map[string]bool{id: true}, builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 1), wake: make(chan struct{}, 1), ctx: ctx, logger: nil}
	if m.logger == nil {
		m.logger = slog.Default()
	}

	finished := make(chan struct{})
	go func() {
		m.runBuild(id)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("inactive worker deadlocked")
	}
	select {
	case <-builder.called:
		t.Fatal("inactive worker called builder")
	default:
	}
	select {
	case <-store.retireCall:
	default:
		t.Fatal("inactive worker did not retire generation")
	}
	if store.row.State != workflowstore.SourceIndexGenerationRetired {
		t.Fatalf("state=%s, want retired", store.row.State)
	}
}
