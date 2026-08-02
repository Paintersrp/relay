package sourceindexruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

type runtimeStore struct {
	mu     sync.Mutex
	rows   map[string]workflowstore.SourceIndexGeneration
	active map[string]bool
	events []string
	err    error
	activeErr error
	activeEntered chan struct{}
	activeRelease chan struct{}
}

func (s *runtimeStore) event(v string) { s.events = append(s.events, v) }
func (s *runtimeStore) CreateOrResolveSourceIndexGeneration(_ context.Context, p workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return workflowstore.SourceIndexGeneration{}, false, s.err
	}
	id, err := sourceindex.GenerationID(p.Identity)
	if err != nil {
		return workflowstore.SourceIndexGeneration{}, false, err
	}
	r, ok := s.rows[id]
	if !ok {
		r = workflowstore.SourceIndexGeneration{GenerationID: id, Identity: p.Identity, State: workflowstore.SourceIndexGenerationPending}
		s.rows[id] = r
	}
	return r, !ok, nil
}
func (s *runtimeStore) GetSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return r, workflowstore.ErrSourceIndexGenerationNotFound
	}
	return r, nil
}
func (s *runtimeStore) GetSourceIndexGenerationByIdentity(_ context.Context, x sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error) {
	id, e := sourceindex.GenerationID(x)
	if e != nil {
		return workflowstore.SourceIndexGeneration{}, e
	}
	return s.GetSourceIndexGeneration(context.Background(), id)
}
func (s *runtimeStore) BeginSourceIndexGenerationBuild(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[id]
	r.State = workflowstore.SourceIndexGenerationBuilding
	r.AttemptCount++
	s.rows[id] = r
	s.event("build")
	return r, nil
}
func (s *runtimeStore) ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]workflowstore.SourceIndexGeneration, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}
func (*runtimeStore) ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error) {
	return nil, nil
}
func (s *runtimeStore) IsSourceIndexAuthorityActive(_ context.Context, x sourceindex.GenerationIdentity) (bool, error) {
	if s.activeEntered != nil { select { case s.activeEntered <- struct{}{}: default: } }
	if s.activeRelease != nil { <-s.activeRelease }
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeErr != nil { return false, s.activeErr }
	id, e := sourceindex.GenerationID(x)
	if e != nil {
		return false, e
	}
	return s.active[id], nil
}
func (s *runtimeStore) MarkSourceIndexGenerationReady(_ context.Context, p workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[p.GenerationID]
	r.State = workflowstore.SourceIndexGenerationReady
	r.GenerationManifestSHA256 = p.GenerationManifestSHA256
	r.CoverageManifestSHA256 = p.CoverageManifestSHA256
	r.ArtifactManifestSHA256 = p.ArtifactManifestSHA256
	s.rows[p.GenerationID] = r
	s.event("ready")
	return r, nil
}
func (s *runtimeStore) MarkSourceIndexGenerationFailed(_ context.Context, p workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[p.GenerationID]
	r.State = workflowstore.SourceIndexGenerationFailed
	r.FailureCode = p.FailureCode
	r.FailureMessage = p.FailureMessage
	s.rows[p.GenerationID] = r
	s.event("failed")
	return r, nil
}
func (s *runtimeStore) RetrySourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[id]
	r.State = workflowstore.SourceIndexGenerationPending
	r.FailureCode = ""
	s.rows[id] = r
	s.event("retry")
	return r, nil
}
func (s *runtimeStore) RetireSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[id]
	r.State = workflowstore.SourceIndexGenerationRetired
	s.rows[id] = r
	s.event("retire")
	return r, nil
}
func (s *runtimeStore) ReactivateSourceIndexGeneration(_ context.Context, id string) (workflowstore.SourceIndexGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[id]
	r.State = workflowstore.SourceIndexGenerationPending
	s.rows[id] = r
	s.event("reactivate")
	return r, nil
}

func matrixManager(s *runtimeStore) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{store: s, config: Config{}, queued: map[string]bool{}, active: map[string]bool{}, builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 20), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}), logger: slog.Default()}
}
func matrixRow(t *testing.T, state workflowstore.SourceIndexGenerationState) (workflowstore.SourceIndexGeneration, string) {
	t.Helper()
	x, id := lifecycleIdentity(t, "matrix-"+string(state))
	return workflowstore.SourceIndexGeneration{GenerationID: id, Identity: x, State: state}, id
}
func seamCleanup(t *testing.T, final, attempts *int, finalErr error) {
	t.Helper()
	oldF, oldA := removeOwnedGeneration, removeAllOwnedGenerationAttempts
	removeOwnedGeneration = func(string, string) error { *final++; return finalErr }
	removeAllOwnedGenerationAttempts = func(string, string) error { *attempts++; return nil }
	t.Cleanup(func() { removeOwnedGeneration, removeAllOwnedGenerationAttempts = oldF, oldA })
}

func TestUnownedBuildingRecoveryMatrix(t *testing.T) {
	for _, tc := range []struct {
		name          string
		active, valid bool
		want          workflowstore.SourceIndexGenerationState
		queued        int
	}{
		{"REC-01 active valid publication becomes ready", true, true, workflowstore.SourceIndexGenerationReady, 0},
		{"REC-02 active invalid publication retries pending", true, false, workflowstore.SourceIndexGenerationPending, 1},
		{"REC-03 inactive valid publication retires", false, true, workflowstore.SourceIndexGenerationRetired, 0},
		{"REC-04 inactive invalid publication retires", false, false, workflowstore.SourceIndexGenerationRetired, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, id := matrixRow(t, workflowstore.SourceIndexGenerationBuilding)
			s := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: tc.active}}
			m := matrixManager(s)
			f, a := 0, 0
			seamCleanup(t, &f, &a, nil)
			old := verifyPublishedGeneration
			verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
				if !tc.valid {
					return reader.Descriptor{}, errors.New("invalid")
				}
				return reader.Descriptor{GenerationManifestSHA256: "g", CoverageManifestSHA256: "c", ArtifactManifestSHA256: "a"}, nil
			}
			t.Cleanup(func() { verifyPublishedGeneration = old })
			if err := m.reconcileGeneration(context.Background(), row, tc.active, false); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetSourceIndexGeneration(context.Background(), id)
			if got.State != tc.want || len(m.queue) != tc.queued {
				t.Fatalf("state=%s queue=%d", got.State, len(m.queue))
			}
			if got.State == workflowstore.SourceIndexGenerationBuilding {
				t.Fatal("REC-09 successful recovery left unowned building")
			}
			if tc.valid && got.GenerationManifestSHA256 != "g" {
				t.Fatal("verified digests not persisted")
			}
		})
	}
}

func TestReadyIntegrityAndCleanupPolicy(t *testing.T) {
	t.Run("READY-01 valid ready generation remains unchanged", func(t *testing.T) {
		row, id := matrixRow(t, workflowstore.SourceIndexGenerationReady)
		row.GenerationManifestSHA256 = "g"
		row.CoverageManifestSHA256 = "c"
		row.ArtifactManifestSHA256 = "a"
		s := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
		m := matrixManager(s)
		old := verifyPublishedGeneration
		verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
			return reader.Descriptor{GenerationManifestSHA256: "g", CoverageManifestSHA256: "c", ArtifactManifestSHA256: "a"}, nil
		}
		t.Cleanup(func() { verifyPublishedGeneration = old })
		if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
			t.Fatal(err)
		}
		if len(s.events) != 0 {
			t.Fatalf("mutations=%v", s.events)
		}
	})
	for _, name := range []string{"READY-02 missing final directory", "READY-03 corrupt generation manifest", "READY-04 corrupt coverage manifest", "READY-05 corrupt artifact manifest", "READY-06 corrupt shard"} {
		t.Run(name, func(t *testing.T) {
			row, id := matrixRow(t, workflowstore.SourceIndexGenerationReady)
			s := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			m := matrixManager(s)
			f, a := 0, 0
			seamCleanup(t, &f, &a, nil)
			old := verifyPublishedGeneration
			verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
				return reader.Descriptor{}, reader.ErrGenerationIntegrity
			}
			t.Cleanup(func() { verifyPublishedGeneration = old })
			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetSourceIndexGeneration(context.Background(), id)
			if got.State != workflowstore.SourceIndexGenerationPending || f != 1 || a != 1 || len(m.queue) != 1 {
				t.Fatalf("state=%s cleanup=%d/%d queue=%d", got.State, f, a, len(m.queue))
			}
		})
	}
	t.Run("READY-07 through READY-09 cleanup failure stops reactivation and queue", func(t *testing.T) {
		row, id := matrixRow(t, workflowstore.SourceIndexGenerationReady)
		s := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
		m := matrixManager(s)
		f, a := 0, 0
		want := errors.New("cleanup")
		seamCleanup(t, &f, &a, want)
		old := verifyPublishedGeneration
		verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
			return reader.Descriptor{}, reader.ErrGenerationIntegrity
		}
		t.Cleanup(func() { verifyPublishedGeneration = old })
		if err := m.reconcileGeneration(context.Background(), row, true, false); !errors.Is(err, want) {
			t.Fatalf("error=%v", err)
		}
		got, _ := s.GetSourceIndexGeneration(context.Background(), id)
		if got.State != workflowstore.SourceIndexGenerationRetired || len(m.queue) != 0 {
			t.Fatalf("state=%s queue=%d", got.State, len(m.queue))
		}
	})
}

func TestRetryPolicyMatrix(t *testing.T) {
	for _, code := range []string{"cancelled", "source_unavailable", "indexer_start_failed", "publication_failed"} {
		t.Run("RETRY retryable "+code, func(t *testing.T) {
			row, id := matrixRow(t, workflowstore.SourceIndexGenerationFailed)
			row.FailureCode = code
			row.AttemptCount = 2
			s := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			m := matrixManager(s)
			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetSourceIndexGeneration(context.Background(), id)
			if got.State != workflowstore.SourceIndexGenerationPending || got.AttemptCount != 2 || len(m.queue) != 1 {
				t.Fatalf("row=%+v queue=%d", got, len(m.queue))
			}
		})
	}
	for _, tc := range []struct {
		name, code string
		attempt    int64
	}{{"RETRY-05 attempt three remains failed", "cancelled", 3}, {"RETRY-06 deterministic failure remains failed", "deterministic", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			row, id := matrixRow(t, workflowstore.SourceIndexGenerationFailed)
			row.FailureCode = tc.code
			row.AttemptCount = tc.attempt
			s := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			m := matrixManager(s)
			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetSourceIndexGeneration(context.Background(), id)
			if got.State != workflowstore.SourceIndexGenerationFailed || len(m.queue) != 0 {
				t.Fatalf("state=%s queue=%d", got.State, len(m.queue))
			}
		})
	}
}
