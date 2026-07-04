package store

import (
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"
)

func newOwnershipTestStore(t *testing.T) *Store {
	t.Helper()
	path := t.TempDir() + "/relay.sqlite"
	st, err := Open(path, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createOwnershipTestRun(t *testing.T, st *Store) int64 {
	t.Helper()
	repo, err := st.CreateRepo("repo", t.TempDir())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := st.CreateRun(repo.ID, "run", "approved_for_executor", "gpt-4o", "gpt-4o", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run.ID
}

func TestAgentExecutionOwnershipCancellationAndCAS(t *testing.T) {
	st := newOwnershipTestStore(t)
	runID := createOwnershipTestRun(t, st)

	exec, err := st.CreateOwnedAgentExecution(runID, "test", "starting", "preview", "local_process", "owner-a", "token-a")
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	if exec.OwnerInstanceID.String != "owner-a" || exec.OwnershipToken.String != "token-a" {
		t.Fatalf("ownership fields not persisted: %+v", exec)
	}

	active, err := st.GetActiveAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active == nil || active.ID != exec.ID {
		t.Fatalf("expected active execution %d, got %+v", exec.ID, active)
	}

	registered, won, err := st.RegisterAgentExecutionProcess(exec.ID, AgentProcessIdentityUpdate{
		ProcessID:        1234,
		ProcessGroupID:   1234,
		ProcessIdentity:  `{"pid":1234,"group_id":1234,"started_at":"fingerprint","platform":"test"}`,
		ProcessStartedAt: "fingerprint",
		StartedAt:        "2026-07-04T00:00:00Z",
		OwnershipToken:   "token-a",
	})
	if err != nil || !won {
		t.Fatalf("register process won=%v err=%v", won, err)
	}
	if registered.Status != "running" || !registered.ProcessIdentity.Valid {
		t.Fatalf("process identity not registered: %+v", registered)
	}

	first, initiated, err := st.RequestAgentExecutionCancellation(exec.ID, "2026-07-04T00:00:01Z")
	if err != nil || !initiated {
		t.Fatalf("first cancellation initiated=%v err=%v", initiated, err)
	}
	second, initiated, err := st.RequestAgentExecutionCancellation(exec.ID, "2026-07-04T00:00:02Z")
	if err != nil || initiated {
		t.Fatalf("second cancellation initiated=%v err=%v", initiated, err)
	}
	if first.CancellationRequestedAt.String != second.CancellationRequestedAt.String {
		t.Fatalf("cancellation timestamp changed: first=%s second=%s", first.CancellationRequestedAt.String, second.CancellationRequestedAt.String)
	}

	exitCode := int64(0)
	var wg sync.WaitGroup
	winners := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, won, err := st.TerminalizeAgentExecutionCAS(exec.ID, AgentExecutionTerminalUpdate{
				Status:         "canceled",
				ExitCode:       &exitCode,
				FinishedAt:     "2026-07-04T00:00:03Z",
				TerminalReason: "operator_cancel_requested",
				TerminalizedAt: "2026-07-04T00:00:03Z",
			})
			if err != nil && err != sql.ErrNoRows {
				t.Errorf("terminalize: %v", err)
			}
			winners <- won
		}()
	}
	wg.Wait()
	close(winners)
	count := 0
	for won := range winners {
		if won {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one CAS winner, got %d", count)
	}

	list, err := st.ListActiveAgentExecutions()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no active executions after terminalization, got %d", len(list))
	}
}
