package sourceindexruntime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

var retryableFailureCodes = []string{"cancelled", "source_unavailable", "indexer_start_failed", "publication_failed"}

func failedRow(t *testing.T, name, code string, attempts int64) (workflowstore.SourceIndexGeneration, string) {
	t.Helper()
	identity, id := lifecycleIdentity(t, name)
	return workflowstore.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflowstore.SourceIndexGenerationFailed, AttemptCount: attempts, FailureCode: code, FailureMessage: "failed"}, id
}

func TestRetryGenerationPolicy(t *testing.T) {
	for _, code := range retryableFailureCodes {
		t.Run("RETRY-01 transient failure retries "+code, func(t *testing.T) {
			row, id := failedRow(t, "retry-below-limit-"+code, code, 2)
			store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			build := &reconciliationBuilder{}
			m := readyManager(store, t.TempDir(), build)

			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetSourceIndexGeneration(context.Background(), id)
			if got.State != workflowstore.SourceIndexGenerationPending || store.retryCalls != 1 || len(m.queued) != 1 || len(m.queue) != 1 || build.calls != 0 {
				t.Fatalf("row=%+v retry=%d queued=%d items=%d builder=%d", got, store.retryCalls, len(m.queued), len(m.queue), build.calls)
			}
		})
	}

	for _, tc := range []struct{ name, code string }{
		{name: "RETRY-02 deterministic failure does not retry", code: "invalid_request"},
		{name: "RETRY-03 unknown failure does not retry", code: "future_failure_code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, id := failedRow(t, tc.name, tc.code, 1)
			store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			build := &reconciliationBuilder{}
			m := readyManager(store, t.TempDir(), build)

			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetSourceIndexGeneration(context.Background(), id)
			if !reflect.DeepEqual(got, row) || store.retryCalls != 0 || len(m.queued) != 0 || build.calls != 0 {
				t.Fatalf("row=%+v retry=%d queued=%d builder=%d", got, store.retryCalls, len(m.queued), build.calls)
			}
		})
	}
}

func TestTransientFailureRetryPolicyAttemptLimit(t *testing.T) {
	for _, code := range retryableFailureCodes {
		t.Run("RETRY-04 attempt limit "+code, func(t *testing.T) {
			row, id := failedRow(t, "retry-at-limit-"+code, code, 3)
			store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
			build := &reconciliationBuilder{}
			m := readyManager(store, t.TempDir(), build)
			if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetSourceIndexGeneration(context.Background(), id)
			if !reflect.DeepEqual(got, row) || store.retryCalls != 0 || len(m.queued) != 0 || build.calls != 0 {
				t.Fatalf("row=%+v retry=%d queued=%d builder=%d", got, store.retryCalls, len(m.queued), build.calls)
			}
		})
	}

	t.Run("RETRY-04 above attempt limit", func(t *testing.T) {
		row, id := failedRow(t, "retry-above-limit", "cancelled", 4)
		store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
		build := &reconciliationBuilder{}
		m := readyManager(store, t.TempDir(), build)
		if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
			t.Fatal(err)
		}
		got, _ := store.GetSourceIndexGeneration(context.Background(), id)
		if !reflect.DeepEqual(got, row) || store.retryCalls != 0 || len(m.queued) != 0 || build.calls != 0 {
			t.Fatalf("row=%+v retry=%d queued=%d builder=%d", got, store.retryCalls, len(m.queued), build.calls)
		}
	})
}

func TestRetryGenerationPolicyStoreFailure(t *testing.T) {
	row, id := failedRow(t, "retry-store-failure", "cancelled", 2)
	wantErr := errors.New("retry store failed")
	store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}, retryErr: wantErr}
	build := &reconciliationBuilder{}
	m := readyManager(store, t.TempDir(), build)

	if err := m.reconcileGeneration(context.Background(), row, true, false); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if store.retryCalls != 1 || len(m.queued) != 0 || len(m.queue) != 0 || build.calls != 0 {
		t.Fatalf("retry=%d queued=%d items=%d builder=%d", store.retryCalls, len(m.queued), len(m.queue), build.calls)
	}
	got, _ := store.GetSourceIndexGeneration(context.Background(), id)
	if !reflect.DeepEqual(got, row) {
		t.Fatalf("row changed: got %+v, want %+v", got, row)
	}
}

func TestRetryGenerationPolicyQueuesExactlyOnce(t *testing.T) {
	row, id := failedRow(t, "retry-queue-once", "publication_failed", 2)
	store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: true}}
	build := &reconciliationBuilder{}
	m := readyManager(store, t.TempDir(), build)

	for range 5 {
		if err := m.reconcileGeneration(context.Background(), row, true, false); err != nil {
			t.Fatal(err)
		}
	}
	if store.retryCalls != 1 || len(m.queued) != 1 || len(m.queue) != 1 || !m.queued[id] || build.calls != 0 {
		t.Fatalf("retry=%d queued=%d items=%d reserved=%v builder=%d", store.retryCalls, len(m.queued), len(m.queue), m.queued[id], build.calls)
	}
}

func TestRetryGenerationPolicyInactiveGenerationRetires(t *testing.T) {
	row, id := failedRow(t, "retry-inactive", "cancelled", 2)
	store := &runtimeStore{rows: map[string]workflowstore.SourceIndexGeneration{id: row}, active: map[string]bool{id: false}}
	build := &reconciliationBuilder{}
	m := readyManager(store, t.TempDir(), build)
	order := []string{}
	store.eventHook = func(event string) { order = append(order, event) }
	oldFinal, oldAttempts := removeOwnedGeneration, removeAllOwnedGenerationAttempts
	removeOwnedGeneration = func(string, string) error {
		order = append(order, "remove final generation")
		return nil
	}
	removeAllOwnedGenerationAttempts = func(string, string) error {
		order = append(order, "remove attempts")
		return nil
	}
	t.Cleanup(func() {
		removeOwnedGeneration = oldFinal
		removeAllOwnedGenerationAttempts = oldAttempts
	})

	if err := m.reconcileGeneration(context.Background(), row, false, false); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetSourceIndexGeneration(context.Background(), id)
	wantOrder := []string{"retire", "remove final generation", "remove attempts"}
	if got.State != workflowstore.SourceIndexGenerationRetired || !reflect.DeepEqual(order, wantOrder) || countEvent(store.events, "retire") != 1 || store.retryCalls != 0 || len(m.queued) != 0 || build.calls != 0 {
		t.Fatalf("row=%+v order=%v events=%v retry=%d queued=%d builder=%d", got, order, store.events, store.retryCalls, len(m.queued), build.calls)
	}
}
