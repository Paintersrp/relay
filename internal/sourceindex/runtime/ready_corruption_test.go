package sourceindexruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	workflowstore "relay/internal/store/workflow"
)

type reconciliationBuilder struct{ calls int }

func (b *reconciliationBuilder) BuildGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error) {
	b.calls++
	return workflowstore.SourceIndexGeneration{}, errors.New("builder must not be called synchronously")
}

type readyAccounting struct {
	verification, finalCleanup, attemptCleanup int
	failed, retries, retirements, reservations int
	builderCalls                               int
}

func readyRow(t *testing.T, name string) (workflowstore.SourceIndexGeneration, string) {
	t.Helper()
	identity, id := lifecycleIdentity(t, name)
	return workflowstore.SourceIndexGeneration{
		GenerationID:             id,
		Identity:                 identity,
		State:                    workflowstore.SourceIndexGenerationReady,
		AttemptCount:             1,
		GenerationManifestSHA256: "generation-digest",
		CoverageManifestSHA256:   "coverage-digest",
		ArtifactManifestSHA256:   "artifact-digest",
	}, id
}

func readyManager(store *runtimeStore, root string, build *reconciliationBuilder) *Manager {
	m := matrixManager(store)
	m.config.IndexRoot = root
	m.build = build
	return m
}

func countEvent(events []string, want string) int {
	count := 0
	for _, event := range events {
		if event == want {
			count++
		}
	}
	return count
}

func snapshotReadyAccounting(m *Manager, store *runtimeStore, build *reconciliationBuilder, verification, finalCleanup, attemptCleanup int) readyAccounting {
	return readyAccounting{
		verification:   verification,
		finalCleanup:   finalCleanup,
		attemptCleanup: attemptCleanup,
		failed:         countEvent(store.events, "failed"),
		retries:        store.retryCalls,
		retirements:    countEvent(store.events, "retire"),
		reservations:   len(m.queued),
		builderCalls:   build.calls,
	}
}

func installReadyCleanup(t *testing.T, finalCleanup, attemptCleanup *int, finalErr, attemptErr error) {
	t.Helper()
	oldFinal, oldAttempts := removeOwnedGeneration, removeAllOwnedGenerationAttempts
	removeOwnedGeneration = func(string, string) error {
		*finalCleanup++
		return finalErr
	}
	removeAllOwnedGenerationAttempts = func(string, string) error {
		*attemptCleanup++
		return attemptErr
	}
	t.Cleanup(func() {
		removeOwnedGeneration = oldFinal
		removeAllOwnedGenerationAttempts = oldAttempts
	})
}

func TestReadyGenerationCorruptionRecovery(t *testing.T) {
	t.Run("READY-01 valid ready generation remains unchanged", func(t *testing.T) {
		row, id := readyRow(t, "ready-valid")
		store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
		build := &reconciliationBuilder{}
		m := readyManager(store, t.TempDir(), build)
		verification, finalCleanup, attemptCleanup := 0, 0, 0
		installReadyCleanup(t, &finalCleanup, &attemptCleanup, nil, nil)
		oldVerify := verifyPublishedGeneration
		verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
			verification++
			return reader.Descriptor{
				GenerationManifestSHA256: row.GenerationManifestSHA256,
				CoverageManifestSHA256:   row.CoverageManifestSHA256,
				ArtifactManifestSHA256:   row.ArtifactManifestSHA256,
			}, nil
		}
		t.Cleanup(func() { verifyPublishedGeneration = oldVerify })

		if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
			t.Fatal(err)
		}
		got, _ := store.GetSourceIndexGeneration(context.Background(), id)
		if !reflect.DeepEqual(got, row) {
			t.Fatalf("ready row changed: got %+v, want %+v", got, row)
		}
		want := readyAccounting{verification: 1}
		if got := snapshotReadyAccounting(m, store, build, verification, finalCleanup, attemptCleanup); got != want {
			t.Fatalf("accounting = %+v, want %+v", got, want)
		}
	})

	for _, tc := range []struct {
		name        string
		missingName string
		digest      bool
	}{
		{name: "READY-02 missing final generation", missingName: "final generation"},
		{name: "READY-03 missing generation manifest", missingName: sourceindex.GenerationManifestFileName},
		{name: "READY-04 missing coverage manifest", missingName: sourceindex.CoverageManifestFileName},
		{name: "READY-05 missing artifact manifest", missingName: sourceindex.ArtifactManifestFileName},
		{name: "READY-06 persisted digest mismatch", digest: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, id := readyRow(t, tc.name)
			root := t.TempDir()
			generation := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
			if err := os.MkdirAll(generation, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{sourceindex.GenerationManifestFileName, sourceindex.CoverageManifestFileName, sourceindex.ArtifactManifestFileName} {
				if err := os.WriteFile(filepath.Join(generation, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			missingPath := ""
			if tc.missingName == "final generation" {
				missingPath = generation
				if err := os.RemoveAll(generation); err != nil {
					t.Fatal(err)
				}
			} else if tc.missingName != "" {
				missingPath = filepath.Join(generation, tc.missingName)
				if err := os.Remove(missingPath); err != nil {
					t.Fatal(err)
				}
			}

			store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			build := &reconciliationBuilder{}
			m := readyManager(store, root, build)
			verification, finalCleanup, attemptCleanup := 0, 0, 0
			installReadyCleanup(t, &finalCleanup, &attemptCleanup, nil, nil)
			oldVerify := verifyPublishedGeneration
			verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
				verification++
				if tc.digest {
					for _, name := range []string{sourceindex.GenerationManifestFileName, sourceindex.CoverageManifestFileName, sourceindex.ArtifactManifestFileName} {
						if _, err := os.Stat(filepath.Join(generation, name)); err != nil {
							t.Fatalf("digest fixture missing %s: %v", name, err)
						}
					}
					return reader.Descriptor{GenerationManifestSHA256: "different-generation", CoverageManifestSHA256: row.CoverageManifestSHA256, ArtifactManifestSHA256: row.ArtifactManifestSHA256}, nil
				}
				if _, err := os.Stat(missingPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("fixture %q is not missing: %v", tc.missingName, err)
				}
				return reader.Descriptor{}, fmt.Errorf("%w: %s", reader.ErrGenerationIntegrity, tc.missingName)
			}
			t.Cleanup(func() { verifyPublishedGeneration = oldVerify })

			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetSourceIndexGeneration(context.Background(), id)
			if got.State != workflowstore.SourceIndexGenerationPending {
				t.Fatalf("state = %s, want pending", got.State)
			}
			want := readyAccounting{verification: 1, finalCleanup: 1, attemptCleanup: 1, retirements: 1, reservations: 1}
			if got := snapshotReadyAccounting(m, store, build, verification, finalCleanup, attemptCleanup); got != want {
				t.Fatalf("accounting = %+v, want %+v", got, want)
			}
		})
	}

	t.Run("READY-07 operational verification failure preserves ready generation", func(t *testing.T) {
		row, id := readyRow(t, "ready-operational-error")
		store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
		build := &reconciliationBuilder{}
		m := readyManager(store, t.TempDir(), build)
		verification, finalCleanup, attemptCleanup := 0, 0, 0
		installReadyCleanup(t, &finalCleanup, &attemptCleanup, nil, nil)
		wantErr := errors.New("verification storage unavailable")
		oldVerify := verifyPublishedGeneration
		verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
			verification++
			return reader.Descriptor{}, wantErr
		}
		t.Cleanup(func() { verifyPublishedGeneration = oldVerify })

		if err := m.reconcileGeneration(context.Background(), row, true, false); !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
		got, _ := store.GetSourceIndexGeneration(context.Background(), id)
		if !reflect.DeepEqual(got, row) {
			t.Fatalf("ready row changed: got %+v, want %+v", got, row)
		}
		want := readyAccounting{verification: 1}
		if got := snapshotReadyAccounting(m, store, build, verification, finalCleanup, attemptCleanup); got != want {
			t.Fatalf("accounting = %+v, want %+v", got, want)
		}
	})
}

func TestCorruptReadyGenerationCleanupOrdering(t *testing.T) {
	row, id := readyRow(t, "ready-cleanup-order")
	store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
	build := &reconciliationBuilder{}
	m := readyManager(store, t.TempDir(), build)
	order := []string{}
	store.eventHook = func(event string) { order = append(order, event) }
	oldVerify, oldFinal, oldAttempts := verifyPublishedGeneration, removeOwnedGeneration, removeAllOwnedGenerationAttempts
	verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
		if len(order) != 0 || len(m.queued) != 0 {
			t.Fatalf("verification did not run first: %v", order)
		}
		order = append(order, "verify")
		return reader.Descriptor{}, reader.ErrGenerationIntegrity
	}
	removeOwnedGeneration = func(string, string) error {
		if !reflect.DeepEqual(order, []string{"verify"}) || len(m.queued) != 0 {
			t.Fatalf("final cleanup order = %v", order)
		}
		order = append(order, "remove final generation")
		return nil
	}
	removeAllOwnedGenerationAttempts = func(string, string) error {
		if !reflect.DeepEqual(order, []string{"verify", "remove final generation"}) || len(m.queued) != 0 {
			t.Fatalf("attempt cleanup order = %v", order)
		}
		order = append(order, "remove attempts")
		return nil
	}
	t.Cleanup(func() {
		verifyPublishedGeneration = oldVerify
		removeOwnedGeneration = oldFinal
		removeAllOwnedGenerationAttempts = oldAttempts
	})

	if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
		t.Fatal(err)
	}
	if len(m.queued) == 1 {
		order = append(order, "enqueue")
	}
	want := []string{"verify", "remove final generation", "remove attempts", "retire", "reactivate", "enqueue"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if build.calls != 0 {
		t.Fatalf("builder calls = %d, want 0", build.calls)
	}
}

func TestCorruptReadyGenerationCleanupFailure(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		finalErr, attemptErr   error
		wantFinal, wantAttempt int
	}{
		{name: "READY-09 final-generation cleanup failure", finalErr: errors.New("final cleanup failed"), wantFinal: 1},
		{name: "READY-09 attempt cleanup failure", attemptErr: errors.New("attempt cleanup failed"), wantFinal: 1, wantAttempt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, id := readyRow(t, tc.name)
			store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			build := &reconciliationBuilder{}
			m := readyManager(store, t.TempDir(), build)
			verification, finalCleanup, attemptCleanup := 0, 0, 0
			installReadyCleanup(t, &finalCleanup, &attemptCleanup, tc.finalErr, tc.attemptErr)
			oldVerify := verifyPublishedGeneration
			verifyPublishedGeneration = func(context.Context, reader.Config, sourceindex.GenerationIdentity) (reader.Descriptor, error) {
				verification++
				return reader.Descriptor{}, reader.ErrGenerationIntegrity
			}
			t.Cleanup(func() { verifyPublishedGeneration = oldVerify })
			wantErr := tc.finalErr
			if wantErr == nil {
				wantErr = tc.attemptErr
			}

			if err := m.reconcileGeneration(context.Background(), row, true, false); !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			got, _ := store.GetSourceIndexGeneration(context.Background(), id)
			if !reflect.DeepEqual(got, row) {
				t.Fatalf("row changed after cleanup failure: got %+v, want %+v", got, row)
			}
			want := readyAccounting{verification: 1, finalCleanup: tc.wantFinal, attemptCleanup: tc.wantAttempt}
			if got := snapshotReadyAccounting(m, store, build, verification, finalCleanup, attemptCleanup); got != want {
				t.Fatalf("accounting = %+v, want %+v", got, want)
			}
		})
	}
}
