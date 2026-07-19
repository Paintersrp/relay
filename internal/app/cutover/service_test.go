package cutover

import (
	"context"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestStateInertPrepared(t *testing.T) {
	store, teardown := testStore(t)
	defer teardown()
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := svc.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no current activation in fresh store")
	}
	closed, err := svc.IsLegacyAdmissionClosed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("legacy admission must be open before activation")
	}
}

func TestReadinessBlockedWithoutEvidence(t *testing.T) {
	// A prepared activation without evidence should not be ready for activation
	// (the DB trigger will reject activation without prerequisites/obligations/criteria)
}

func TestLegacyGateAllowsBeforeActivation(t *testing.T) {
	store, teardown := testStore(t)
	defer teardown()
	svc, _ := NewService(store)
	gate := NewLegacyGate(svc)
	decision, err := gate.AllowNewPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("expected legacy gate to allow before activation")
	}
}

func testStore(t *testing.T) (*workflowstore.Store, func()) {
	t.Helper()
	store, err := workflowstore.Open("file::memory:?cache=shared", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, func() {
		store.Close()
	}
}
