package audits

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowAuditPackageRequiredRejectsNonPackageRunWithoutEffects(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(ctx, "relay", repoPath); err != nil {
		t.Fatal(err)
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := runs.CreateRun(ctx, workflowruns.CreateRunInput{
		FeatureSlug: "package-required", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		CanonicalJSON:    packageEvidenceExecutionSpec("package-required", "main", strings.Repeat("a", 40)),
		RenderedMarkdown: []byte("# Execution Brief\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionRun(ctx, created.Run.RunID, "setup_ready", "executing")
		if err != nil {
			return err
		}
		_, err = tx.TransitionRun(ctx, created.Run.RunID, "executing", workflowstore.RunStatusValidating)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	runBefore, err := store.GetRunByRunID(ctx, created.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := nonPackageAuditState(t, store, runBefore.ID)
	loaderCalled := false
	service, err := newWorkflowAuditService(store, func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error) {
		t.Fatal("non-package audit invoked repository inspection")
		return workflowrepos.AuditCommitEvidence{}, nil
	}, func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
		loaderCalled = true
		t.Fatal("non-package audit loaded package evidence")
		return WorkflowPackageExecutionEvidence{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("c", 40)
	decisionInput := RecordWorkflowAuditDecisionInput{
		RunID: created.Run.RunID, AuditPacketID: "packet", PacketSHA256: strings.Repeat("d", 64),
		AuditedCommit: commit, Decision: workflowstore.AuditDecisionAccepted, Rationale: "accepted", OperatorConfirmed: true,
	}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "Prepare", call: func() error {
			_, err := service.Prepare(ctx, PrepareWorkflowAuditInput{RunID: created.Run.RunID, AuditedCommit: commit})
			return err
		}},
		{name: "GetStatus", call: func() error { _, err := service.GetStatus(ctx, created.Run.RunID); return err }},
		{name: "GetCurrentPacket", call: func() error { _, err := service.GetCurrentPacket(ctx, created.Run.RunID); return err }},
		{name: "GetCurrentArtifact", call: func() error {
			_, err := service.GetCurrentArtifact(ctx, GetWorkflowAuditArtifactInput{RunID: created.Run.RunID, ArtifactReference: "assignment"})
			return err
		}},
		{name: "RecordDecision", call: func() error { _, err := service.RecordDecision(ctx, decisionInput); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, ErrWorkflowAuditPackageRequired) {
				t.Fatalf("error = %v, want ErrWorkflowAuditPackageRequired", err)
			}
			if got := nonPackageAuditState(t, store, runBefore.ID); got != stateBefore {
				t.Fatalf("audit state changed: before=%+v after=%+v", stateBefore, got)
			}
		})
	}
	if loaderCalled {
		t.Fatal("non-package audit loaded package evidence")
	}
}

type nonPackageAuditSnapshot struct {
	run, plan, pass, packets, artifacts, decisions, ticketDecisions, satisfactions, seeds, seedFindings, reopenings, seedReopenings int64
	artifactEntries                                                                                                                 int
}

func nonPackageAuditState(t *testing.T, store *workflowstore.Store, runID int64) nonPackageAuditSnapshot {
	t.Helper()
	state := nonPackageAuditSnapshot{}
	queries := []struct {
		query string
		out   *int64
	}{
		{`SELECT COUNT(*) FROM runs WHERE id = ?`, &state.run},
		{`SELECT COUNT(*) FROM plans`, &state.plan},
		{`SELECT COUNT(*) FROM plan_passes`, &state.pass},
		{`SELECT COUNT(*) FROM audit_packets WHERE run_row_id = ?`, &state.packets},
		{`SELECT COUNT(*) FROM artifacts WHERE run_row_id = ?`, &state.artifacts},
		{`SELECT COUNT(*) FROM audit_decisions WHERE run_row_id = ?`, &state.decisions},
		{`SELECT COUNT(*) FROM audit_ticket_revision_decisions`, &state.ticketDecisions},
		{`SELECT COUNT(*) FROM delivery_ticket_revision_satisfactions`, &state.satisfactions},
		{`SELECT COUNT(*) FROM audit_remediation_seeds`, &state.seeds},
		{`SELECT COUNT(*) FROM audit_remediation_seed_findings`, &state.seedFindings},
		{`SELECT COUNT(*) FROM feature_workspace_completion_reopenings`, &state.reopenings},
		{`SELECT COUNT(*) FROM audit_remediation_seed_reopenings`, &state.seedReopenings},
	}
	for index, item := range queries {
		var err error
		if index == 0 || index == 3 || index == 4 || index == 5 {
			err = store.DB().QueryRow(item.query, runID).Scan(item.out)
		} else {
			err = store.DB().QueryRow(item.query).Scan(item.out)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(store.ArtifactStore().Root())
	if err != nil {
		t.Fatal(err)
	}
	state.artifactEntries = len(entries)
	return state
}
