package workflowruns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowplans "relay/internal/app/plans/workflow"
	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

type sequenceIDs struct {
	runIDs        []string
	attemptIDs    []string
	artifactIDs   []string
	decisionIDs   []string
	runIndex      int
	attemptIndex  int
	artifactIndex int
	decisionIndex int
}

func (ids *sequenceIDs) RunID() string {
	value := ids.runIDs[ids.runIndex]
	ids.runIndex++
	return value
}

func (ids *sequenceIDs) ExecutionAttemptID() string {
	value := ids.attemptIDs[ids.attemptIndex]
	ids.attemptIndex++
	return value
}

func (ids *sequenceIDs) ArtifactID() string {
	value := ids.artifactIDs[ids.artifactIndex]
	ids.artifactIndex++
	return value
}

func (ids *sequenceIDs) AuditDecisionID() string {
	value := ids.decisionIDs[ids.decisionIndex]
	ids.decisionIndex++
	return value
}

type planIDs struct {
	passIndex     int
	artifactIndex int
}

func (ids *planIDs) PlanID() string { return "plan-run-tests" }
func (ids *planIDs) PassID() string {
	ids.passIndex++
	return fmt.Sprintf("pass-run-tests-%d", ids.passIndex)
}
func (ids *planIDs) ArtifactID() string {
	ids.artifactIndex++
	return fmt.Sprintf("artifact-plan-run-tests-%d", ids.artifactIndex)
}

func TestCreateManagedAndStandaloneRuns(t *testing.T) {
	ctx := context.Background()
	store, root := openRunTestStore(t)
	registerRunTestRepo(t, ctx, store, "relay")
	plan := createRunTestPlan(t, ctx, store)
	service := newRunTestService(t, store, &sequenceIDs{
		runIDs:      []string{"run-managed", "run-standalone"},
		artifactIDs: []string{"artifact-run-1", "artifact-run-2", "artifact-run-3", "artifact-run-4"},
	})

	canonical := []byte("{\"feature_slug\":\"feature\"}\n")
	brief := []byte("# Executor Brief\n")
	managed, err := service.CreateRun(ctx, CreateRunInput{
		FeatureSlug:      "feature",
		RepoTarget:       "relay",
		Branch:           "feat/simplification",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    canonical,
		RenderedMarkdown: brief,
		PlanID:           plan.Plan.PlanID,
		PassNumber:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if managed.Run.Status != workflowstore.RunStatusSetupReady || !managed.Run.PlanRowID.Valid || !managed.Run.PlanPassRowID.Valid {
		t.Fatalf("unexpected managed run: %+v", managed.Run)
	}
	pass, err := store.GetPlanPassByPlanAndNumber(ctx, plan.Plan.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if pass.Status != workflowstore.PassStatusInProgress {
		t.Fatalf("managed pass status = %q", pass.Status)
	}
	for _, expected := range []struct {
		path string
		data []byte
	}{
		{path: filepath.Join(root, "artifacts", "runs", "run-managed", "feature.execution-spec.json"), data: canonical},
		{path: filepath.Join(root, "artifacts", "runs", "run-managed", "feature.executor-brief.md"), data: brief},
	} {
		data, err := os.ReadFile(expected.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(expected.data) {
			t.Fatalf("artifact %s changed: got %q want %q", expected.path, data, expected.data)
		}
	}

	standalone, err := service.CreateRun(ctx, CreateRunInput{
		FeatureSlug:      "feature",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("b", 40),
		CanonicalJSON:    canonical,
		RenderedMarkdown: brief,
	})
	if err != nil {
		t.Fatal(err)
	}
	if standalone.Run.PlanRowID.Valid || standalone.Run.PlanPassRowID.Valid {
		t.Fatalf("standalone run acquired managed association: %+v", standalone.Run)
	}
}

func TestCreateRunRejectsMismatchedAssociationAndInvalidRemediation(t *testing.T) {
	ctx := context.Background()
	store, _ := openRunTestStore(t)
	registerRunTestRepo(t, ctx, store, "relay")
	registerRunTestRepo(t, ctx, store, "other")
	plan := createRunTestPlan(t, ctx, store)
	service := newRunTestService(t, store, &sequenceIDs{
		runIDs:      []string{"run-mismatch", "run-original", "run-remediation", "run-invalid-remediation"},
		attemptIDs:  []string{"attempt-original"},
		artifactIDs: []string{"artifact-1", "artifact-2", "artifact-3", "artifact-4", "artifact-5", "artifact-6", "artifact-7", "artifact-8"},
	})

	baseInput := CreateRunInput{
		FeatureSlug:      "feature",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Brief\n"),
		PlanID:           plan.Plan.PlanID,
		PassNumber:       1,
	}
	mismatch := baseInput
	mismatch.RepoTarget = "other"
	if _, err := service.CreateRun(ctx, mismatch); err == nil {
		t.Fatal("mismatched Plan/pass/repository association was accepted")
	}
	pass, err := store.GetPlanPassByPlanAndNumber(ctx, plan.Plan.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if pass.Status != workflowstore.PassStatusPlanned {
		t.Fatalf("failed association changed pass status to %q", pass.Status)
	}

	original, err := service.CreateRun(ctx, baseInput)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := service.BeginExecutionAttempt(ctx, BeginExecutionAttemptInput{
		RunID:   original.Run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MarkExecutionAttemptRunning(ctx, attempt.Attempt.AttemptID, `{"running":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FinishExecutionAttempt(ctx, FinishExecutionAttemptInput{
		AttemptID:  attempt.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: `{"ok":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	originalRun, err := service.RecordValidationResult(ctx, original.Run.RunID, false)
	if err != nil {
		t.Fatal(err)
	}
	if originalRun.Status != workflowstore.RunStatusNeedsRevision {
		t.Fatalf("original run status = %q", originalRun.Status)
	}

	remediation := baseInput
	remediation.RemediatesRunID = original.Run.RunID
	created, err := service.CreateRun(ctx, remediation)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Run.RemediatesRunRowID.Valid || created.Run.RemediatesRunRowID.Int64 != original.Run.ID {
		t.Fatalf("unexpected remediation relation: %+v", created.Run)
	}

	invalid := baseInput
	invalid.RemediatesRunID = created.Run.RunID
	if _, err := service.CreateRun(ctx, invalid); err == nil {
		t.Fatal("run not in needs_revision was accepted as remediation source")
	}
}

func TestFailedAndTimedOutAttemptsCanEnterValidationAndRemediation(t *testing.T) {
	cases := []struct {
		name          string
		suffix        string
		attemptStatus string
	}{
		{name: "failed", suffix: "failed", attemptStatus: workflowstore.AttemptStatusFailed},
		{name: "timed out", suffix: "timed-out", attemptStatus: workflowstore.AttemptStatusTimedOut},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, _ := openRunTestStore(t)
			registerRunTestRepo(t, ctx, store, "relay")
			plan := createRunTestPlan(t, ctx, store)
			service := newRunTestService(t, store, &sequenceIDs{
				runIDs: []string{
					"run-" + tc.suffix + "-original",
					"run-" + tc.suffix + "-remediation",
				},
				attemptIDs: []string{"attempt-" + tc.suffix},
				artifactIDs: []string{
					"artifact-" + tc.suffix + "-1",
					"artifact-" + tc.suffix + "-2",
					"artifact-" + tc.suffix + "-3",
					"artifact-" + tc.suffix + "-4",
				},
			})

			baseInput := CreateRunInput{
				FeatureSlug:      "feature",
				RepoTarget:       "relay",
				Branch:           "main",
				BaseCommit:       strings.Repeat("a", 40),
				CanonicalJSON:    []byte("{}\n"),
				RenderedMarkdown: []byte("# Brief\n"),
				PlanID:           plan.Plan.PlanID,
				PassNumber:       1,
			}
			original, err := service.CreateRun(ctx, baseInput)
			if err != nil {
				t.Fatal(err)
			}
			begun, err := service.BeginExecutionAttempt(ctx, BeginExecutionAttemptInput{
				RunID:   original.Run.RunID,
				Adapter: "opencode_go",
				Model:   "test-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			finished, err := service.FinishExecutionAttempt(ctx, FinishExecutionAttemptInput{
				AttemptID:  begun.Attempt.AttemptID,
				Status:     tc.attemptStatus,
				ResultJSON: fmt.Sprintf(`{"status":%q}`, tc.attemptStatus),
			})
			if err != nil {
				t.Fatal(err)
			}
			if finished.Run.Status != workflowstore.RunStatusExecutionFailed {
				t.Fatalf("run status after %s attempt = %q", tc.attemptStatus, finished.Run.Status)
			}

			needsRevision, err := service.RecordValidationResult(ctx, original.Run.RunID, false)
			if err != nil {
				t.Fatal(err)
			}
			if needsRevision.Status != workflowstore.RunStatusNeedsRevision {
				t.Fatalf("validated run status = %q", needsRevision.Status)
			}

			remediationInput := baseInput
			remediationInput.RemediatesRunID = original.Run.RunID
			remediation, err := service.CreateRun(ctx, remediationInput)
			if err != nil {
				t.Fatal(err)
			}
			if !remediation.Run.RemediatesRunRowID.Valid || remediation.Run.RemediatesRunRowID.Int64 != original.Run.ID {
				t.Fatalf("unexpected remediation relation: %+v", remediation.Run)
			}
			if remediation.Run.PlanRowID != original.Run.PlanRowID || remediation.Run.PlanPassRowID != original.Run.PlanPassRowID {
				t.Fatalf("remediation association changed: original=%+v remediation=%+v", original.Run, remediation.Run)
			}
			pass, err := store.GetPlanPassByPlanAndNumber(ctx, plan.Plan.ID, 1)
			if err != nil {
				t.Fatal(err)
			}
			if pass.Status != workflowstore.PassStatusInProgress {
				t.Fatalf("managed pass status = %q", pass.Status)
			}
		})
	}
}

func TestExecutionAttemptValidationAndAuditDecisionCompleteManagedWorkflow(t *testing.T) {
	ctx := context.Background()
	store, _ := openRunTestStore(t)
	registerRunTestRepo(t, ctx, store, "relay")
	plan := createRunTestPlan(t, ctx, store)
	service := newRunTestService(t, store, &sequenceIDs{
		runIDs:      []string{"run-lifecycle"},
		attemptIDs:  []string{"attempt-lifecycle"},
		artifactIDs: []string{"artifact-run-spec", "artifact-run-brief"},
		decisionIDs: []string{"audit-decision-lifecycle"},
	})

	created, err := service.CreateRun(ctx, CreateRunInput{
		FeatureSlug:      "feature",
		RepoTarget:       "relay",
		Branch:           "main",
		BaseCommit:       strings.Repeat("a", 40),
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Brief\n"),
		PlanID:           plan.Plan.PlanID,
		PassNumber:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	begun, err := service.BeginExecutionAttempt(ctx, BeginExecutionAttemptInput{
		RunID:   created.Run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if begun.Run.Status != workflowstore.RunStatusExecuting || begun.Attempt.Status != workflowstore.AttemptStatusPending {
		t.Fatalf("unexpected begin result: %+v", begun)
	}
	if _, err := service.MarkExecutionAttemptRunning(ctx, begun.Attempt.AttemptID, `{"running":true}`); err != nil {
		t.Fatal(err)
	}
	finished, err := service.FinishExecutionAttempt(ctx, FinishExecutionAttemptInput{
		AttemptID:  begun.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: `{"exit_code":0}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Run.Status != workflowstore.RunStatusValidating || finished.Attempt.Status != workflowstore.AttemptStatusSucceeded {
		t.Fatalf("unexpected finish result: %+v", finished)
	}
	validated, err := service.RecordValidationResult(ctx, created.Run.RunID, true)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Status != workflowstore.RunStatusAuditReady {
		t.Fatalf("validated run status = %q", validated.Status)
	}

	passBeforeAudit, err := store.GetPlanPassByPlanAndNumber(ctx, plan.Plan.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if passBeforeAudit.Status != workflowstore.PassStatusInProgress {
		t.Fatalf("managed pass completed before accepted audit: %q", passBeforeAudit.Status)
	}
	planBeforeAudit, err := store.GetPlanByPlanID(ctx, plan.Plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if planBeforeAudit.Status != workflowstore.PlanStatusActive {
		t.Fatalf("managed Plan completed before accepted audit: %q", planBeforeAudit.Status)
	}

	packet := persistAuditPacket(t, ctx, store, validated)
	decision, err := service.RecordAuditDecision(ctx, RecordAuditDecisionInput{
		RunID:                 validated.RunID,
		AuditPacketArtifactID: packet.ArtifactID,
		AuditedCommit:         strings.Repeat("b", 40),
		PacketSHA256:          packet.SHA256,
		Decision:              workflowstore.AuditDecisionAccepted,
		Rationale:             "Accepted by test.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Run.Status != workflowstore.RunStatusCompleted || decision.Pass == nil || decision.Pass.Status != workflowstore.PassStatusCompleted {
		t.Fatalf("unexpected audit closeout: %+v", decision)
	}
	if decision.Plan == nil || decision.Plan.Status != workflowstore.PlanStatusCompleted {
		t.Fatalf("managed Plan was not completed: %+v", decision.Plan)
	}
}

func TestCreateRunFailuresLeaveNoRunArtifactsOrPassTransition(t *testing.T) {
	ctx := context.Background()

	t.Run("database metadata failure", func(t *testing.T) {
		store, root := openRunTestStore(t)
		registerRunTestRepo(t, ctx, store, "relay")
		plan := createRunTestPlan(t, ctx, store)
		service := newRunTestService(t, store, &sequenceIDs{
			runIDs:      []string{"run-db-failure"},
			artifactIDs: []string{"duplicate-artifact", "duplicate-artifact"},
		})
		_, err := service.CreateRun(ctx, CreateRunInput{
			FeatureSlug:      "feature",
			RepoTarget:       "relay",
			Branch:           "main",
			BaseCommit:       strings.Repeat("a", 40),
			CanonicalJSON:    []byte("{}\n"),
			RenderedMarkdown: []byte("# Brief\n"),
			PlanID:           plan.Plan.PlanID,
			PassNumber:       1,
		})
		if err == nil {
			t.Fatal("expected duplicate artifact ID failure")
		}
		assertRunTableCount(t, store.DB(), "runs", 0)
		pass, err := store.GetPlanPassByPlanAndNumber(ctx, plan.Plan.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if pass.Status != workflowstore.PassStatusPlanned {
			t.Fatalf("failed transaction changed pass status to %q", pass.Status)
		}
		assertNoRunFiles(t, filepath.Join(root, "artifacts", "runs"))
	})

	t.Run("artifact promotion failure", func(t *testing.T) {
		store, root := openRunTestStore(t)
		registerRunTestRepo(t, ctx, store, "relay")
		plan := createRunTestPlan(t, ctx, store)
		service := newRunTestService(t, store, &sequenceIDs{
			runIDs:      []string{"run-promotion-failure"},
			artifactIDs: []string{"artifact-one", "artifact-two"},
		})
		if err := os.WriteFile(filepath.Join(root, "artifacts", "runs"), []byte("block directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := service.CreateRun(ctx, CreateRunInput{
			FeatureSlug:      "feature",
			RepoTarget:       "relay",
			Branch:           "main",
			BaseCommit:       strings.Repeat("a", 40),
			CanonicalJSON:    []byte("{}\n"),
			RenderedMarkdown: []byte("# Brief\n"),
			PlanID:           plan.Plan.PlanID,
			PassNumber:       1,
		})
		if err == nil {
			t.Fatal("expected promotion failure")
		}
		assertRunTableCount(t, store.DB(), "runs", 0)
		pass, err := store.GetPlanPassByPlanAndNumber(ctx, plan.Plan.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if pass.Status != workflowstore.PassStatusPlanned {
			t.Fatalf("failed promotion changed pass status to %q", pass.Status)
		}
	})
}

func openRunTestStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
}

func registerRunTestRepo(t *testing.T, ctx context.Context, store *workflowstore.Store, key string) {
	t.Helper()
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, key, t.TempDir())
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func createRunTestPlan(t *testing.T, ctx context.Context, store *workflowstore.Store) workflowplans.CreatePlanResult {
	t.Helper()
	service, err := workflowplans.NewServiceWithIDs(store, &planIDs{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.CreatePlan(ctx, workflowplans.CreatePlanInput{
		FeatureSlug:      "feature",
		CanonicalJSON:    []byte("{}\n"),
		RenderedMarkdown: []byte("# Plan\n"),
		Repositories: []workflowplans.RepositoryTargetInput{
			{
				RepoTarget:         "relay",
				Branch:             "main",
				PlanningBaseCommit: strings.Repeat("a", 40),
			}},
		Passes: []workflowplans.PassInput{
			{Number: 1, Name: "Pass", RepoTarget: "relay"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func newRunTestService(t *testing.T, store *workflowstore.Store, ids *sequenceIDs) *Service {
	t.Helper()
	service, err := NewServiceWithIDs(store, ids)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func persistAuditPacket(t *testing.T, ctx context.Context, store *workflowstore.Store, run workflowstore.Run) workflowstore.Artifact {
	t.Helper()
	batch, err := store.ArtifactStore().Begin("runs/" + run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("audit_packet", "feature.audit-packet.json", "application/json", []byte("{\"audit\":true}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact workflowstore.Artifact
	if err := store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		var createErr error
		artifact, createErr = tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:   "artifact-audit-packet",
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: run.ID, Valid: true},
			Kind:         file.Kind,
			RelativePath: file.RelativePath,
			MediaType:    file.MediaType,
			SHA256:       file.SHA256,
			SizeBytes:    file.SizeBytes,
		})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func assertRunTableCount(t *testing.T, db *sql.DB, table string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s count = %d, want %d", table, got, want)
	}
}

func assertNoRunFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			return fmt.Errorf("unexpected durable file %s", path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

var _ = workflowartifacts.File{}
