package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowplans "relay/internal/app/plans/workflow"
	workflowruns "relay/internal/app/runs/workflow"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type auditFixture struct {
	store      *workflowstore.Store
	runs       *workflowruns.Service
	service    *WorkflowAuditService
	run        workflowstore.Run
	plan       workflowstore.Plan
	pass       workflowstore.PlanPass
	head       string
	inspectErr error
}

func newAuditFixture(t *testing.T, managed bool) *auditFixture {
	t.Helper()
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
	if _, err := registry.Register(context.Background(), "relay", repoPath); err != nil {
		t.Fatal(err)
	}
	fixture := &auditFixture{store: store, head: strings.Repeat("b", 40)}
	inspector := func(_ context.Context, _ string, branch, base, audited string) (workflowrepos.AuditCommitEvidence, error) {
		if fixture.inspectErr != nil {
			return workflowrepos.AuditCommitEvidence{}, fixture.inspectErr
		}
		if audited != fixture.head {
			return workflowrepos.AuditCommitEvidence{}, errors.New("head_mismatch")
		}
		return workflowrepos.AuditCommitEvidence{
			Branch: branch, BaseCommit: base, AuditedCommit: audited,
			ChangedFiles: []string{"internal/a.go"},
			NameStatus:   "M\tinternal/a.go",
			DiffStat:     "1 file changed",
			CommitLog:    audited + "\tDev\t2026-07-06T00:00:00Z\tchange",
			Diff:         "diff --git a/internal/a.go b/internal/a.go\n+change\n",
		}, nil
	}
	service, err := NewWorkflowAuditServiceWithInspector(store, inspector)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service = service
	runs, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.runs = runs

	planID := ""
	passNumber := int64(0)
	if managed {
		planService, err := workflowplans.NewService(store)
		if err != nil {
			t.Fatal(err)
		}
		planJSON := []byte(`{
  "schema_version":"1.0",
  "feature_slug":"audit-test",
  "goal":"Audit test Plan.",
  "context":"Audit test context.",
  "scope":{"in_scope":["Test audit."],"out_of_scope":["No extra work."]},
  "repo_targets":[{"repo_target":"relay","branch":"feat/simplification","planning_base_commit":"` + strings.Repeat("a", 40) + `"}],
  "passes":[{"number":1,"name":"Audit pass","repo_target":"relay","goal":"Test audit.","context":"Selected pass authority.","scope":{"in_scope":["Audit."],"out_of_scope":["No extra."]},"depends_on":[],"outcomes":["Audited."],"source_targets":[{"path":"internal/a.go","purpose":"Test."}],"validation_intent":["Audit."],"completion_criteria":["Done."]}],
  "completion_criteria":["Complete."]
}`)
		created, err := planService.CreatePlan(context.Background(), workflowplans.CreatePlanInput{
			FeatureSlug:      "audit-test",
			CanonicalJSON:    planJSON,
			RenderedMarkdown: []byte("# Plan\n"),
			Repositories: []workflowplans.RepositoryTargetInput{{
				RepoTarget: "relay", Branch: "feat/simplification", PlanningBaseCommit: strings.Repeat("a", 40),
			}},
			Passes: []workflowplans.PassInput{{Number: 1, Name: "Audit pass", RepoTarget: "relay"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fixture.plan = created.Plan
		fixture.pass = created.Passes[0]
		planID = created.Plan.PlanID
		passNumber = 1
	}
	createdRun, err := runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      "audit-test",
		RepoTarget:       "relay",
		Branch:           "feat/simplification",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    []byte(`{"schema_version":"1.0","feature_slug":"audit-test"}`),
		RenderedMarkdown: []byte("# Executor Brief\n\nExact task.\n"),
		PlanID:           planID,
		PassNumber:       passNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	begun, err := runs.BeginExecutionAttempt(context.Background(), workflowruns.BeginExecutionAttemptInput{
		RunID: createdRun.Run.RunID, Adapter: "codex", Model: "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkExecutionAttemptRunning(context.Background(), begun.Attempt.AttemptID, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
	finished, err := runs.FinishExecutionAttempt(context.Background(), workflowruns.FinishExecutionAttemptInput{
		AttemptID:  begun.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: `{"normalized_status":"done"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.run = finished.Run
	stageAttemptEvidence(t, store, finished.Attempt)
	return fixture
}

func stageAttemptEvidence(t *testing.T, store *workflowstore.Store, attempt workflowstore.ExecutionAttempt) {
	t.Helper()
	batch, err := store.ArtifactStore().Begin("attempt-test/" + attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := batch.Stage("execution_evidence", "execution-evidence.json", "application/json", []byte(`{"validated":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifactBatch(context.Background(), batch, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
			ArtifactID:            workflowstore.NewArtifactID(),
			OwnerType:             workflowstore.ArtifactOwnerExecutionAttempt,
			ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true},
			Kind:                  staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType,
			SHA256: staged.SHA256, SizeBytes: staged.SizeBytes,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowAuditPacketContainsCompleteAuthority(t *testing.T) {
	fixture := newAuditFixture(t, true)
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Run.Status != workflowstore.RunStatusAuditReady || prepared.Packet.Status != workflowstore.AuditPacketStatusCurrent {
		t.Fatalf("prepared = %+v", prepared)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	text := string(current.PacketBytes)
	for _, required := range []string{
		`"execution_spec"`, `"executor_brief"`, `"selected_pass"`,
		`"attempt_id"`, `"adapter": "codex"`, `"model": "test-model"`,
		`"validation_evidence"`, `"audited_commit"`, `"changed_files"`, `"diff"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("packet missing %s: %s", required, text)
		}
	}
	if current.Packet.PacketSHA256 != current.Artifact.SHA256 {
		t.Fatal("packet identity does not match artifact SHA-256")
	}
}

func TestWorkflowAuditAcceptedDecisionCompletesRunPassAndPlan(t *testing.T) {
	fixture := newAuditFixture(t, true)
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID:             fixture.run.RunID,
		AuditPacketID:     prepared.Packet.AuditPacketID,
		PacketSHA256:      prepared.Packet.PacketSHA256,
		AuditedCommit:     fixture.head,
		Decision:          workflowstore.AuditDecisionAccepted,
		Rationale:         "accepted",
		OperatorConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != workflowstore.RunStatusCompleted || result.Pass == nil || result.Pass.Status != workflowstore.PassStatusCompleted || result.Plan == nil || result.Plan.Status != workflowstore.PlanStatusCompleted {
		t.Fatalf("result = %+v", result)
	}
	if _, err := fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: prepared.Packet.AuditPacketID,
		PacketSHA256: prepared.Packet.PacketSHA256, AuditedCommit: fixture.head,
		Decision: workflowstore.AuditDecisionAccepted, OperatorConfirmed: true,
	}); err == nil {
		t.Fatal("second decision was accepted")
	}
}

func TestWorkflowAuditNeedsRevisionPreservesManagedPass(t *testing.T) {
	fixture := newAuditFixture(t, true)
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: prepared.Packet.AuditPacketID,
		PacketSHA256: prepared.Packet.PacketSHA256, AuditedCommit: fixture.head,
		Decision:  workflowstore.AuditDecisionNeedsRevision,
		Rationale: "fix the finding", OperatorConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pass, err := fixture.store.GetPlanPassByRowID(context.Background(), fixture.run.PlanPassRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := fixture.store.GetPlanByRowID(context.Background(), fixture.run.PlanRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != workflowstore.RunStatusNeedsRevision || pass.Status != workflowstore.PassStatusInProgress || plan.Status != workflowstore.PlanStatusActive {
		t.Fatalf("run=%s pass=%s plan=%s", result.Run.Status, pass.Status, plan.Status)
	}
}

func TestWorkflowAuditPacketBecomesStaleAfterRepositoryChangeOrLaterAttempt(t *testing.T) {
	t.Run("repository change", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head}); err != nil {
			t.Fatal(err)
		}
		fixture.inspectErr = errors.New("head_mismatch")
		if _, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowAuditPacketStale) {
			t.Fatalf("error = %v", err)
		}
		latest, err := fixture.store.GetLatestAuditPacketByRun(context.Background(), fixture.run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if latest.Status != workflowstore.AuditPacketStatusStale {
			t.Fatalf("status = %q", latest.Status)
		}
	})

	t.Run("later attempt", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head}); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.runs.BeginExecutionAttempt(context.Background(), workflowruns.BeginExecutionAttemptInput{
			RunID: fixture.run.RunID, Adapter: "codex", Model: "retry-model",
		}); err != nil {
			t.Fatal(err)
		}
		latest, err := fixture.store.GetLatestAuditPacketByRun(context.Background(), fixture.run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if latest.Status != workflowstore.AuditPacketStatusStale || latest.StaleReason != "later_execution_attempt" {
			t.Fatalf("packet = %+v", latest)
		}
	})
}

func TestWorkflowAuditDecisionRequiresExplicitConfirmationAndExactPacket(t *testing.T) {
	fixture := newAuditFixture(t, false)
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: prepared.Packet.AuditPacketID,
		PacketSHA256: prepared.Packet.PacketSHA256, AuditedCommit: fixture.head,
		Decision: workflowstore.AuditDecisionAccepted,
	})
	if !errors.Is(err, ErrWorkflowAuditConfirmation) {
		t.Fatalf("error = %v", err)
	}
	_, err = fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID: fixture.run.RunID, AuditPacketID: prepared.Packet.AuditPacketID,
		PacketSHA256: strings.Repeat("0", 64), AuditedCommit: fixture.head,
		Decision: workflowstore.AuditDecisionAccepted, OperatorConfirmed: true,
	})
	if !errors.Is(err, ErrWorkflowAuditPacketStale) {
		t.Fatalf("error = %v", err)
	}
}

var _ = json.Valid
