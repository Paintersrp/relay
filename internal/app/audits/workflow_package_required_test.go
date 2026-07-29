package audits

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	var projectID, planID, passID int64
	if err := store.DB().QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-package-required', 'Package required') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-package-required', 'package-required', ?) RETURNING id`, projectID, strings.Repeat("e", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit) VALUES (?, 1, 'relay', 'main', ?)`, planID, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target) VALUES ('pass-package-required', ?, 1, 'Package required pass', 'relay') RETURNING id`, planID).Scan(&passID); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.TransitionPlanPass(ctx, "pass-package-required", workflowstore.PassStatusPlanned, workflowstore.PassStatusInProgress)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE runs SET plan_row_id = ?, plan_pass_row_id = ? WHERE id = ?`, planID, passID, created.Run.ID); err != nil {
		t.Fatal(err)
	}
	created.Run, err = store.GetRunByRunID(ctx, created.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	runBefore, err := store.GetRunByRunID(ctx, created.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
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
			stateBefore := nonPackageAuditState(t, store, runBefore.RunID)
			if err := test.call(); !errors.Is(err, ErrWorkflowAuditPackageRequired) {
				t.Fatalf("error = %v, want ErrWorkflowAuditPackageRequired", err)
			}
			if got := nonPackageAuditState(t, store, runBefore.RunID); !reflect.DeepEqual(got, stateBefore) {
				t.Fatalf("audit state changed: before=%+v after=%+v", stateBefore, got)
			}
		})
	}
	if loaderCalled {
		t.Fatal("non-package audit loaded package evidence")
	}
}

type nonPackageAuditSnapshot struct {
	run                                                                                                            workflowstore.Run
	plan                                                                                                           workflowstore.Plan
	pass                                                                                                           workflowstore.PlanPass
	packets, artifacts, decisions, ticketDecisions, satisfactions, seeds, seedFindings, reopenings, seedReopenings int64
	auditPacketDirectories, auditDecisionDirectories, stagingDirectories                                           []string
}

func nonPackageAuditState(t *testing.T, store *workflowstore.Store, runID string) nonPackageAuditSnapshot {
	t.Helper()
	state := nonPackageAuditSnapshot{}
	var err error
	state.run, err = store.GetRunByRunID(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.run.PlanRowID.Valid || !state.run.PlanPassRowID.Valid {
		t.Fatal("non-package fixture is not attached to a plan pass")
	}
	state.plan, err = store.GetPlanByRowID(context.Background(), state.run.PlanRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	state.pass, err = store.GetPlanPassByRowID(context.Background(), state.run.PlanPassRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	queries := []struct {
		query string
		out   *int64
	}{
		{`SELECT COUNT(*) FROM runs WHERE id = ?`, new(int64)},
		{`SELECT COUNT(*) FROM plans`, new(int64)},
		{`SELECT COUNT(*) FROM plan_passes`, new(int64)},
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
			err = store.DB().QueryRow(item.query, state.run.ID).Scan(item.out)
		} else {
			err = store.DB().QueryRow(item.query).Scan(item.out)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	state.auditPacketDirectories = nonPackageAuditDirectories(t, store, "audit-packets")
	state.auditDecisionDirectories = nonPackageAuditDirectories(t, store, "audit-decisions")
	state.stagingDirectories = nonPackageAuditDirectories(t, store, "staging")
	return state
}

func nonPackageAuditDirectories(t *testing.T, store *workflowstore.Store, relative string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.ArtifactStore().Root(), relative))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	return result
}
