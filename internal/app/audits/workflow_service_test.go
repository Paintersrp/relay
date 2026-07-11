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
	store        *workflowstore.Store
	runs         *workflowruns.Service
	service      *WorkflowAuditService
	run          workflowstore.Run
	plan         workflowstore.Plan
	pass         workflowstore.PlanPass
	head         string
	inspectErr   error
	inspectCalls int
	inspectHook  func(int)
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
		fixture.inspectCalls++
		if fixture.inspectHook != nil {
			fixture.inspectHook(fixture.inspectCalls)
		}
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
		var project workflowstore.Project
		if err := store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
			var err error
			project, err = tx.CreateProject(context.Background(), workflowstore.CreateProjectParams{
				ProjectID: "project-audit-tests",
				Name:      "Audit tests",
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
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
			ProjectID:        project.ProjectID,
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

func stageApplierEvidence(t *testing.T, store *workflowstore.Store, run workflowstore.Run, outcome string, residuals []string) {
	t.Helper()
	batch, err := store.ArtifactStore().Begin("applier-test/" + run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	resultBody, err := json.Marshal(map[string]any{
		"outcome":             outcome,
		"actor_kind":          "applier",
		"changed_files":       []string{"internal/a.go"},
		"residual_operations": residuals,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := batch.Stage("applier_result_json", "applier-result.json", "application/json", append(resultBody, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := batch.Stage("applier_ledger_json", "applier-ledger.json", "application/json", []byte(`{"operations":[]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifactBatch(context.Background(), batch, func(tx *workflowstore.Tx) error {
		for _, staged := range []struct {
			kind, path, media, sha string
			size                   int64
		}{
			{result.Kind, result.RelativePath, result.MediaType, result.SHA256, result.SizeBytes},
			{ledger.Kind, ledger.RelativePath, ledger.MediaType, ledger.SHA256, ledger.SizeBytes},
		} {
			if _, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
				ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun,
				RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: staged.kind,
				RelativePath: staged.path, MediaType: staged.media, SHA256: staged.sha, SizeBytes: staged.size,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func stageUnrelatedRunArtifact(t *testing.T, store *workflowstore.Store, run workflowstore.Run) workflowstore.Artifact {
	t.Helper()
	batch, err := store.ArtifactStore().Begin("unrelated-run-test/" + run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := batch.Stage("unrelated_run_evidence", "unrelated-run-evidence.txt", "text/plain", []byte("not implementation evidence\n"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact workflowstore.Artifact
	if err := store.CommitArtifactBatch(context.Background(), batch, func(tx *workflowstore.Tx) error {
		var err error
		artifact, err = tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
			ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun,
			RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: staged.Kind,
			RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func TestWorkflowAuditApplierOnlyPacketUsesRunEvidenceWithoutExecutorAttempt(t *testing.T) {
	fixture := newAuditFixture(t, false)
	created, err := fixture.runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug: "applier-only", RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch,
		BaseCommit: fixture.run.BaseCommit, CanonicalJSON: []byte(`{"schema_version":"1.0","feature_slug":"applier-only"}`),
		RenderedMarkdown: []byte("# Applier brief\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	stageApplierEvidence(t, fixture.store, created.Run, "completed", nil)
	if _, err := fixture.runs.RecordApplierCompleted(context.Background(), created.Run.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runs.RecordValidationResult(context.Background(), created.Run.RunID, true); err != nil {
		t.Fatal(err)
	}
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: created.Run.RunID, AuditedCommit: fixture.head})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Packet.ImplementationActorKind != workflowstore.ImplementationActorApplier || prepared.Packet.ExecutionAttemptRowID.Valid {
		t.Fatalf("packet = %+v", prepared.Packet)
	}
	var packet WorkflowAuditPacket
	if err := json.Unmarshal(func() []byte {
		current, err := fixture.service.GetCurrentPacket(context.Background(), created.Run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		return current.PacketBytes
	}(), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Execution.ActorKind != workflowstore.ImplementationActorApplier || packet.Execution.Applier == nil || packet.Execution.Executor != nil {
		t.Fatalf("execution evidence = %+v", packet.Execution)
	}
}

func TestWorkflowAuditApplierOnlyPacketExcludesUnrelatedRunArtifacts(t *testing.T) {
	fixture := newAuditFixture(t, false)
	created, err := fixture.runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug: "applier-only-filtered", RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch,
		BaseCommit: fixture.run.BaseCommit, CanonicalJSON: []byte(`{"schema_version":"1.0","feature_slug":"applier-only-filtered"}`),
		RenderedMarkdown: []byte("# Applier brief\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	stageApplierEvidence(t, fixture.store, created.Run, "completed", nil)
	unrelated := stageUnrelatedRunArtifact(t, fixture.store, created.Run)
	if _, err := fixture.runs.RecordApplierCompleted(context.Background(), created.Run.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runs.RecordValidationResult(context.Background(), created.Run.RunID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: created.Run.RunID, AuditedCommit: fixture.head}); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), created.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packet WorkflowAuditPacket
	if err := json.Unmarshal(current.PacketBytes, &packet); err != nil {
		t.Fatal(err)
	}
	for _, declared := range packet.Artifacts {
		if declared.ArtifactReference == unrelated.ArtifactID {
			t.Fatalf("unrelated run artifact was declared in audit packet: %+v", declared)
		}
	}
	_, err = fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
		RunID: created.Run.RunID, ArtifactReference: unrelated.ArtifactID, MaxBytes: 12000,
	})
	if !errors.Is(err, ErrWorkflowAuditArtifactReference) {
		t.Fatalf("unrelated run artifact readback error = %v, want %v", err, ErrWorkflowAuditArtifactReference)
	}
}

func TestWorkflowAuditPacketConformsToSchemaContract(t *testing.T) {
	// 1. Managed run packet
	fixtureManaged := newAuditFixture(t, true)
	_, err := fixtureManaged.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixtureManaged.run.RunID, AuditedCommit: fixtureManaged.head,
	})
	if err != nil {
		t.Fatal(err)
	}
	currentManaged, err := fixtureManaged.service.GetCurrentPacket(context.Background(), fixtureManaged.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packetManaged map[string]any
	if err := json.Unmarshal(currentManaged.PacketBytes, &packetManaged); err != nil {
		t.Fatal(err)
	}

	// 2. Unassociated non-remediation run packet
	fixtureUnassociated := newAuditFixture(t, false)
	_, err = fixtureUnassociated.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixtureUnassociated.run.RunID, AuditedCommit: fixtureUnassociated.head,
	})
	if err != nil {
		t.Fatal(err)
	}
	currentUnassociated, err := fixtureUnassociated.service.GetCurrentPacket(context.Background(), fixtureUnassociated.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packetUnassociated map[string]any
	if err := json.Unmarshal(currentUnassociated.PacketBytes, &packetUnassociated); err != nil {
		t.Fatal(err)
	}

	// Assert root keys for both
	for _, p := range []map[string]any{packetManaged, packetUnassociated} {
		for _, requiredKey := range []string{
			"schema_version", "run", "repository", "authority", "execution",
			"changed_files", "relevant_source_paths", "validation", "artifacts",
		} {
			if _, exists := p[requiredKey]; !exists {
				t.Fatalf("missing root key %s", requiredKey)
			}
		}
		// remediation_context must be absent for non-remediation
		if _, exists := p["remediation_context"]; exists {
			t.Fatal("remediation_context exists in non-remediation packet")
		}
		// Check that forbidden root keys are absent
		for _, forbidden := range []string{"audit_packet_id", "selected_pass", "attempt", "validation_evidence", "commit", "blockers"} {
			if _, exists := p[forbidden]; exists {
				t.Fatalf("forbidden root key %s exists in packet", forbidden)
			}
		}
	}

	// Check managed packet run keys
	runManaged, ok := packetManaged["run"].(map[string]any)
	if !ok {
		t.Fatal("run is not a map in managed packet")
	}
	if _, ok := runManaged["run_id"].(float64); !ok {
		t.Fatalf("run.run_id is not a number: %v", runManaged["run_id"])
	}
	if _, ok := runManaged["plan_id"].(float64); !ok {
		t.Fatalf("run.plan_id is not a number: %v", runManaged["plan_id"])
	}
	if _, ok := runManaged["pass_id"].(float64); !ok {
		t.Fatalf("run.pass_id is not a number: %v", runManaged["pass_id"])
	}
	if _, exists := runManaged["user_intent"]; exists {
		t.Fatal("run.user_intent exists in managed packet")
	}

	// Check unassociated packet run keys
	runUnassociated, ok := packetUnassociated["run"].(map[string]any)
	if !ok {
		t.Fatal("run is not a map in unassociated packet")
	}
	if _, ok := runUnassociated["run_id"].(float64); !ok {
		t.Fatalf("run.run_id is not a number: %v", runUnassociated["run_id"])
	}
	if _, exists := runUnassociated["plan_id"]; exists {
		t.Fatal("run.plan_id exists in unassociated packet")
	}
	if _, exists := runUnassociated["pass_id"]; exists {
		t.Fatal("run.pass_id exists in unassociated packet")
	}
	userIntent, ok := runUnassociated["user_intent"].(string)
	if !ok || strings.TrimSpace(userIntent) == "" {
		t.Fatalf("run.user_intent is not a nonblank string: %v", runUnassociated["user_intent"])
	}

	// Check authority filenames & content
	for _, p := range []map[string]any{packetManaged, packetUnassociated} {
		auth, ok := p["authority"].(map[string]any)
		if !ok {
			t.Fatal("authority is not a map")
		}
		spec, ok := auth["execution_spec"].(map[string]any)
		if !ok {
			t.Fatal("authority.execution_spec is not a map")
		}
		specFilename, _ := spec["filename"].(string)
		if !strings.HasSuffix(specFilename, ".execution-spec.json") || specFilename == "execution-spec.json" {
			t.Fatalf("execution_spec.filename is invalid: %s", specFilename)
		}
		if _, ok := spec["content"].(map[string]any); !ok {
			t.Fatalf("execution_spec.content is not a map: %v", spec["content"])
		}

		brief, ok := auth["executor_brief"].(map[string]any)
		if !ok {
			t.Fatal("authority.executor_brief is not a map")
		}
		briefFilename, _ := brief["filename"].(string)
		if !strings.HasSuffix(briefFilename, ".executor-brief.md") || briefFilename == "executor-brief.md" {
			t.Fatalf("executor_brief.filename is invalid: %s", briefFilename)
		}
		briefContent, _ := brief["content"].(string)
		if strings.TrimSpace(briefContent) == "" {
			t.Fatal("executor_brief.content is blank")
		}
	}

	// Check managed_context
	managedCtx, ok := packetManaged["authority"].(map[string]any)["managed_context"].(map[string]any)
	if !ok {
		t.Fatal("managed_context is missing or not a map in managed packet")
	}
	expectedManagedCtxKeys := map[string]bool{
		"plan_goal": true, "plan_context": true, "plan_scope": true,
		"repository_target": true, "selected_pass": true, "plan_completion_criteria": true,
	}
	if len(managedCtx) != len(expectedManagedCtxKeys) {
		t.Fatalf("managed_context has unexpected number of keys: %v", managedCtx)
	}
	for k := range expectedManagedCtxKeys {
		if _, exists := managedCtx[k]; !exists {
			t.Fatalf("managed_context missing key %s", k)
		}
	}
	repoTargetObj, ok := managedCtx["repository_target"].(map[string]any)
	if !ok {
		t.Fatal("repository_target is not a map")
	}
	for _, k := range []string{"repo_target", "branch", "planning_base_commit"} {
		if _, exists := repoTargetObj[k]; !exists {
			t.Fatalf("repository_target missing key %s", k)
		}
	}

	// Check execution shape
	for _, p := range []map[string]any{packetManaged, packetUnassociated} {
		exec, ok := p["execution"].(map[string]any)
		if !ok {
			t.Fatal("execution is not a map")
		}
		expectedExecKeys := map[string]bool{
			"actor_kind": true, "executor": true,
			"status": true, "committed_sha": true, "completion_summary": true,
			"blockers_or_incomplete_work": true, "reported_changed_files": true,
		}
		if len(exec) != len(expectedExecKeys) {
			t.Fatalf("execution has unexpected keys: %v", exec)
		}
		for k := range expectedExecKeys {
			if _, exists := exec[k]; !exists {
				t.Fatalf("execution missing key %s", k)
			}
		}
	}

	// Check validation entries
	for _, p := range []map[string]any{packetManaged, packetUnassociated} {
		vals, ok := p["validation"].([]any)
		if !ok {
			t.Fatal("validation is not an array")
		}
		for _, valEntry := range vals {
			entry, ok := valEntry.(map[string]any)
			if !ok {
				t.Fatal("validation entry is not a map")
			}
			for _, k := range []string{"command", "expected", "status", "concise_result"} {
				if _, exists := entry[k]; !exists {
					t.Fatalf("validation entry missing key %s", k)
				}
			}
			status, _ := entry["status"].(string)
			if status != "passed" && status != "failed" && status != "not_run" {
				t.Fatalf("invalid validation status: %s", status)
			}
			exitCode, hasExitCode := entry["exit_code"]
			if status == "passed" || status == "failed" {
				if !hasExitCode {
					t.Fatal("passed/failed validation entry lacks exit_code")
				}
				if _, ok := exitCode.(float64); !ok {
					t.Fatalf("exit_code is not a number: %v", exitCode)
				}
			} else {
				if hasExitCode {
					t.Fatal("not_run validation entry contains exit_code")
				}
			}
			if artRef, exists := entry["artifact_reference"]; exists {
				artRefStr, _ := artRef.(string)
				found := false
				for _, artEntryRaw := range p["artifacts"].([]any) {
					artEntry, ok := artEntryRaw.(map[string]any)
					if ok && artEntry["artifact_reference"] == artRefStr {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("validation artifact_reference %s not found in artifacts list", artRefStr)
				}
			}
		}
	}

	// Check artifacts shape
	for _, p := range []map[string]any{packetManaged, packetUnassociated} {
		arts, ok := p["artifacts"].([]any)
		if !ok {
			t.Fatal("artifacts is not an array")
		}
		hasUnifiedDiff := false
		for _, artEntryRaw := range arts {
			artEntry, ok := artEntryRaw.(map[string]any)
			if !ok {
				t.Fatal("artifact entry is not a map")
			}
			expectedArtKeys := map[string]bool{
				"artifact_reference": true, "artifact_type": true,
				"sha256": true, "description": true,
			}
			if len(artEntry) != len(expectedArtKeys) {
				t.Fatalf("artifact entry has unexpected keys: %v", artEntry)
			}
			for k := range expectedArtKeys {
				if _, exists := artEntry[k]; !exists {
					t.Fatalf("artifact entry missing key %s", k)
				}
			}
			if artEntry["artifact_type"] == "unified_diff" {
				hasUnifiedDiff = true
			}
		}
		if !hasUnifiedDiff {
			t.Fatal("artifacts list missing unified_diff type")
		}
	}
}

func TestWorkflowAuditRemediationPacketConformsToSchemaContract(t *testing.T) {
	fixture := newAuditFixture(t, false)
	preparedOrig, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID:             fixture.run.RunID,
		AuditPacketID:     preparedOrig.Packet.AuditPacketID,
		PacketSHA256:      preparedOrig.Packet.PacketSHA256,
		AuditedCommit:     fixture.head,
		Decision:          workflowstore.AuditDecisionNeedsRevision,
		Rationale:         "needs revision",
		OperatorConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	runs := fixture.runs
	remediationRun, err := runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      "remediation-run",
		RepoTarget:       fixture.run.RepoTarget,
		Branch:           fixture.run.Branch,
		BaseCommit:       fixture.run.BaseCommit,
		CanonicalJSON:    []byte(`{"schema_version":"1.0","feature_slug":"remediation-run"}`),
		RenderedMarkdown: []byte("# Remediation brief\n"),
		RemediatesRunID:  fixture.run.RunID,
	})
	if err != nil {
		t.Fatal(err)
	}
	begun, err := runs.BeginExecutionAttempt(context.Background(), workflowruns.BeginExecutionAttemptInput{
		RunID: remediationRun.Run.RunID, Adapter: "codex", Model: "test-model",
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
	stageAttemptEvidence(t, fixture.store, finished.Attempt)

	_, err = fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: finished.Run.RunID, AuditedCommit: fixture.head,
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), finished.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packet map[string]any
	if err := json.Unmarshal(current.PacketBytes, &packet); err != nil {
		t.Fatal(err)
	}

	remedCtx, ok := packet["remediation_context"].(map[string]any)
	if !ok {
		t.Fatal("remediation_context is missing or not a map in remediation packet")
	}
	remediatedRunID, ok := remedCtx["remediated_run_id"].(float64)
	if !ok {
		t.Fatalf("remediation_context.remediated_run_id is not a number: %v", remedCtx["remediated_run_id"])
	}
	if int64(remediatedRunID) != fixture.run.ID {
		t.Fatalf("expected remediated_run_id to be %d, got %v", fixture.run.ID, remediatedRunID)
	}
	findings, ok := remedCtx["material_findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("remediation_context.material_findings is empty or invalid: %v", remedCtx["material_findings"])
	}
	for _, fRaw := range findings {
		f, ok := fRaw.(map[string]any)
		if !ok {
			t.Fatal("material finding is not a map")
		}
		for _, k := range []string{"source", "summary", "evidence", "required_remediation"} {
			val, exists := f[k].(string)
			if !exists || strings.TrimSpace(val) == "" {
				t.Fatalf("material finding key %s is missing or blank", k)
			}
		}
	}
	if _, exists := remedCtx["remediates_run_id"]; exists {
		t.Fatal("remediation_context contains obsolete key remediates_run_id")
	}

	runObj, ok := packet["run"].(map[string]any)
	if !ok {
		t.Fatal("run is not a map")
	}
	remRunID, ok := runObj["remediates_run_id"].(float64)
	if !ok || int64(remRunID) != fixture.run.ID {
		t.Fatalf("run.remediates_run_id is invalid: %v", runObj["remediates_run_id"])
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

func TestWorkflowAuditPacketBecomesStaleAfterRepositoryChange(t *testing.T) {
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

func TestWorkflowAuditPacketExcludesRuntimeAndEvidenceBodies(t *testing.T) {
	fixture := newAuditFixture(t, false)
	secret := "audit-packet-secret"
	t.Setenv("OPENAI_API_KEY", secret)
	unsafeResult := `{
		"owner_instance_id":"owner-private",
		"process_identity":"pid-private",
		"command_preview":"C:\\Users\\operator\\relay ` + secret + `",
		"exit_code":0,
		"termination_verified":true,
		"normalized_status":"done",
		"error":"safe ` + secret + `",
		"blocker_text":""
	}`
	if _, err := fixture.store.DB().Exec(
		`UPDATE execution_attempts SET result_json = ? WHERE run_row_id = ?`,
		unsafeResult,
		fixture.run.ID,
	); err != nil {
		t.Fatal(err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), fixture.run.ID)
	if err == nil && len(artifacts) > 0 {
		path, pathErr := workflowArtifactPath(fixture.store, artifacts[0])
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		_ = os.WriteFile(path, []byte(`C:\\Users\\operator\\relay /home/operator/relay https://example.invalid/file?token=signed-value`), 0o600)
	}

	if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	text := string(current.PacketBytes)
	for _, forbidden := range []string{
		"owner-private",
		"pid-private",
		"command_preview",
		`C:\\Users\\operator\\relay`,
		"/home/operator/relay",
		"signed-value",
		secret,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("packet leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("packet did not preserve configured-secret redaction: %s", text)
	}
}

func TestWorkflowAuditAuthoritativeArtifactTamperingBlocks(t *testing.T) {
	tests := []struct {
		name    string
		managed bool
		kind    string
		owner   string
	}{
		{name: "execution spec", kind: "execution_spec", owner: "run"},
		{name: "executor brief", kind: "executor_brief", owner: "run"},
		{name: "canonical plan", managed: true, kind: "canonical_plan", owner: "plan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAuditFixture(t, tt.managed)
			var artifacts []workflowstore.Artifact
			var err error
			if tt.owner == "plan" {
				artifacts, err = fixture.store.ListArtifactsByPlan(context.Background(), fixture.plan.ID)
			} else {
				artifacts, err = fixture.store.ListArtifactsByRun(context.Background(), fixture.run.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := requireArtifactKind(artifacts, tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			path, err := workflowArtifactPath(fixture.store, artifact)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) == 0 {
				t.Fatal("authoritative artifact is empty")
			}
			data[0] ^= 0x01
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
				RunID: fixture.run.RunID, AuditedCommit: fixture.head,
			}); err == nil {
				t.Fatal("tampered authoritative artifact was accepted")
			}
		})
	}
}

func TestWorkflowAuditDecisionReverifiesPacketArtifactInsideTransaction(t *testing.T) {
	fixture := newAuditFixture(t, false)
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := fixture.store.GetArtifactByRowID(context.Background(), prepared.Packet.ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	path, err := workflowArtifactPath(fixture.store, artifact)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) == 0 {
		t.Fatal("audit packet artifact is empty")
	}

	fixture.inspectCalls = 0
	fixture.inspectHook = func(call int) {
		if call != 2 {
			return
		}
		tampered := append([]byte(nil), original...)
		tampered[0] ^= 0x01
		if err := os.WriteFile(path, tampered, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	_, err = fixture.service.RecordDecision(context.Background(), RecordWorkflowAuditDecisionInput{
		RunID:             fixture.run.RunID,
		AuditPacketID:     prepared.Packet.AuditPacketID,
		PacketSHA256:      prepared.Packet.PacketSHA256,
		AuditedCommit:     fixture.head,
		Decision:          workflowstore.AuditDecisionAccepted,
		Rationale:         "must not persist",
		OperatorConfirmed: true,
	})
	if !errors.Is(err, ErrWorkflowAuditPacketStale) {
		t.Fatalf("error = %v", err)
	}
	run, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != workflowstore.RunStatusAuditReady {
		t.Fatalf("tampered packet changed Run status to %q", run.Status)
	}
	var decisions int
	if err := fixture.store.DB().QueryRow(`SELECT COUNT(*) FROM audit_decisions WHERE run_row_id = ?`, run.ID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if decisions != 0 {
		t.Fatalf("tampered packet created %d decisions", decisions)
	}
	var decisionArtifacts int
	if err := fixture.store.DB().QueryRow(
		`SELECT COUNT(*) FROM artifacts WHERE run_row_id = ? AND kind = 'audit_decision'`,
		run.ID,
	).Scan(&decisionArtifacts); err != nil {
		t.Fatal(err)
	}
	if decisionArtifacts != 0 {
		t.Fatalf("tampered packet created %d decision artifacts", decisionArtifacts)
	}
}

func workflowAuditEvidenceReference(t *testing.T, fixture *auditFixture) (workflowstore.Artifact, WorkflowAuditPacket) {
	t.Helper()
	if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packet WorkflowAuditPacket
	if err := json.Unmarshal(current.PacketBytes, &packet); err != nil {
		t.Fatal(err)
	}
	if len(packet.Artifacts) == 0 {
		t.Fatal("packet has no artifact references")
	}
	// Find a non-unified_diff artifact (evidence) to use for readback tests.
	var ref WorkflowAuditPacketArtifact
	for _, a := range packet.Artifacts {
		if a.ArtifactType != "unified_diff" {
			ref = a
			break
		}
	}
	if ref.ArtifactReference == "" {
		t.Fatal("packet has no non-unified_diff evidence reference")
	}
	artifact, err := fixture.store.GetArtifactByArtifactID(
		context.Background(),
		ref.ArtifactReference,
	)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, packet
}

func TestWorkflowAuditArtifactReadbackRequiresCurrentPacketReferenceOwnershipAndIntegrity(t *testing.T) {
	t.Run("bounded declared reference", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		artifact, _ := workflowAuditEvidenceReference(t, fixture)
		result, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
			RunID: fixture.run.RunID, ArtifactReference: artifact.ArtifactID, MaxBytes: 8,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Artifact.ArtifactID != artifact.ArtifactID ||
			result.Packet.AuditPacketID == "" ||
			!result.Truncated ||
			len(result.Content) > 8 {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("arbitrary and undeclared references", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		_, _ = workflowAuditEvidenceReference(t, fixture)
		for _, invalid := range []string{"/tmp/arbitrary", "../artifact", "artifact-missing"} {
			if _, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
				RunID: fixture.run.RunID, ArtifactReference: invalid, MaxBytes: 8,
			}); !errors.Is(err, ErrWorkflowAuditArtifactReference) {
				t.Fatalf("reference %q error = %v", invalid, err)
			}
		}
	})

	t.Run("packet reference cannot cross execution attempts", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		artifact, _ := workflowAuditEvidenceReference(t, fixture)
		otherRun, err := fixture.runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
			FeatureSlug:      "audit-readback-other",
			RepoTarget:       fixture.run.RepoTarget,
			Branch:           fixture.run.Branch,
			BaseCommit:       fixture.run.BaseCommit,
			CanonicalJSON:    []byte(`{"schema_version":"1.0"}`),
			RenderedMarkdown: []byte("# Other brief\n"),
		})
		if err != nil {
			t.Fatal(err)
		}
		otherAttempt, err := fixture.runs.BeginExecutionAttempt(context.Background(), workflowruns.BeginExecutionAttemptInput{
			RunID: otherRun.Run.RunID, Adapter: "codex", Model: "other-model",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.store.DB().Exec(
			`UPDATE artifacts SET execution_attempt_row_id = ? WHERE artifact_id = ?`,
			otherAttempt.Attempt.ID,
			artifact.ArtifactID,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
			RunID: fixture.run.RunID, ArtifactReference: artifact.ArtifactID, MaxBytes: 8,
		}); !errors.Is(err, ErrWorkflowAuditArtifactReference) && !errors.Is(err, ErrWorkflowAuditArtifactOwnership) {
			t.Fatalf("cross-attempt reference error = %v", err)
		}
	})

	t.Run("dangling packet reference", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		artifact, _ := workflowAuditEvidenceReference(t, fixture)
		if _, err := fixture.store.DB().Exec(
			`DELETE FROM artifacts WHERE artifact_id = ?`,
			artifact.ArtifactID,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
			RunID: fixture.run.RunID, ArtifactReference: artifact.ArtifactID, MaxBytes: 8,
		}); !errors.Is(err, ErrWorkflowAuditArtifactReference) {
			t.Fatalf("dangling reference error = %v", err)
		}
	})

	t.Run("tampered artifact", func(t *testing.T) {
		fixture := newAuditFixture(t, false)
		artifact, _ := workflowAuditEvidenceReference(t, fixture)
		path, err := workflowArtifactPath(fixture.store, artifact)
		if err != nil {
			t.Fatal(err)
		}
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(original) == 0 {
			t.Fatal("evidence artifact is empty")
		}
		tampered := append([]byte(nil), original...)
		tampered[0] ^= 0x01
		if err := os.WriteFile(path, tampered, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
			RunID: fixture.run.RunID, ArtifactReference: artifact.ArtifactID, MaxBytes: 8,
		}); !errors.Is(err, ErrWorkflowAuditArtifactIntegrity) {
			t.Fatalf("tampered artifact error = %v", err)
		}
	})
}

func TestWorkflowAuditArtifactReadbackUsesPacketDeclaredArtifacts(t *testing.T) {
	fixture := newAuditFixture(t, false)
	_, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head})
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packet WorkflowAuditPacket
	if err := json.Unmarshal(current.PacketBytes, &packet); err != nil {
		t.Fatal(err)
	}
	var diffRef string
	for _, a := range packet.Artifacts {
		if a.ArtifactType == "unified_diff" {
			diffRef = a.ArtifactReference
			break
		}
	}
	if diffRef == "" {
		t.Fatal("no unified_diff artifact reference in packet")
	}

	result, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{RunID: fixture.run.RunID, ArtifactReference: diffRef, MaxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact.Kind != "unified_diff" || !strings.Contains(string(result.Content), "diff --git") {
		t.Fatalf("artifact readback = %+v content=%q", result.Artifact, string(result.Content))
	}
	if _, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{RunID: fixture.run.RunID, ArtifactReference: "../secret"}); !errors.Is(err, ErrWorkflowAuditArtifactReference) {
		t.Fatalf("undeclared path-like reference error = %v", err)
	}
}

var _ = json.Valid
