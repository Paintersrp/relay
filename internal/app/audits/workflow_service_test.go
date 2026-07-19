package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowpackages "relay/internal/app/packages"
	workflowplans "relay/internal/app/plans/workflow"
	workflowruns "relay/internal/app/runs/workflow"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/artifactschema"
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

func auditFixtureExecutionSpec(featureSlug, branch, baseCommit string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"2.0","feature_slug":%q,"repo_target":"relay","branch":%q,"base_commit":%q,"goal":"Exercise the audit packet workflow.","context":"Audit packet test fixture.","scope":{"in_scope":["Exercise audit packet generation."],"out_of_scope":["No unrelated behavior."]},"steps":[{"number":1,"goal":"Apply one representative source change.","substeps":[{"number":1,"instruction":"Apply the representative source change.","files":[{"path":"internal/a.go","operation":"modify","purpose":"Provide representative changed source.","implementation":{"changes":[{"kind":"replace","old_text":"before\n","new_text":"after\n","expected_occurrences":1}]}}],"completion_criteria":["The representative source change is declared."]}],"completion_criteria":["The representative step is complete."]}],"validation":{"commands":[{"command":"go test ./internal/audits","expected":"The focused audit tests pass."}]},"completion_criteria":["The audit packet fixture is complete."]}`, featureSlug, branch, baseCommit))
}

func cloneAuditPacket(t *testing.T, packet map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func requireAuditPacketValidity(t *testing.T, packet map[string]any, want bool) {
	t.Helper()
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := artifactschema.Validate(artifactschema.KindAuditPacket, raw)
	if err != nil {
		t.Fatal(err)
	}
	if valid != want {
		t.Fatalf("packet validity = %v, want %v: %s", valid, want, raw)
	}
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
		CanonicalJSON:    auditFixtureExecutionSpec("audit-test", "feat/simplification", strings.Repeat("a", 40)),
		RenderedMarkdown: []byte("# Executor Brief\n\nExact task.\n"),
		PlanID:           planID,
		PassNumber:       passNumber,
	})
	if err != nil {
		t.Fatal(err)
	}
	runArtifacts, err := store.ListArtifactsByRun(context.Background(), createdRun.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var effectiveBrief workflowstore.Artifact
	for _, artifact := range runArtifacts {
		if artifact.Kind == "executor_brief" {
			effectiveBrief = artifact
		}
	}
	if effectiveBrief.ArtifactID == "" {
		t.Fatal("executor brief artifact is missing")
	}
	runtimeJSON, err := json.Marshal(map[string]any{
		"normalized_status":           "done",
		"termination_verified":        true,
		"effective_brief_artifact_id": effectiveBrief.ArtifactID,
		"effective_brief_sha256":      effectiveBrief.SHA256,
		"effective_brief_mode":        "full",
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
	if _, err := runs.MarkExecutionAttemptRunning(context.Background(), begun.Attempt.AttemptID, string(runtimeJSON)); err != nil {
		t.Fatal(err)
	}
	finished, err := runs.FinishExecutionAttempt(context.Background(), workflowruns.FinishExecutionAttemptInput{
		AttemptID:  begun.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: string(runtimeJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.run = finished.Run
	stageAttemptEvidence(t, store, finished.Attempt, effectiveBrief)
	return fixture
}

func stageAttemptEvidence(t *testing.T, store *workflowstore.Store, attempt workflowstore.ExecutionAttempt, effectiveBrief workflowstore.Artifact) {
	t.Helper()
	batch, err := store.ArtifactStore().Begin("attempt-test/" + attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"normalized_status":           "done",
		"termination_verified":        true,
		"effective_brief_artifact_id": effectiveBrief.ArtifactID,
		"effective_brief_sha256":      effectiveBrief.SHA256,
		"effective_brief_mode":        "full",
	})
	if err != nil {
		t.Fatal(err)
	}
	executionEvidence, err := batch.Stage("execution_evidence", "execution-evidence.json", "application/json", payload)
	if err != nil {
		t.Fatal(err)
	}
	executorResult, err := batch.Stage("executor_result", "executor-result.txt", "text/plain", []byte("STATUS: DONE\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifactBatch(context.Background(), batch, func(tx *workflowstore.Tx) error {
		for _, staged := range []workflowartifacts.File{executionEvidence, executorResult} {
			if _, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
				ArtifactID:            workflowstore.NewArtifactID(),
				OwnerType:             workflowstore.ArtifactOwnerExecutionAttempt,
				ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true},
				Kind:                  staged.Kind,
				RelativePath:          staged.RelativePath,
				MediaType:             staged.MediaType,
				SHA256:                staged.SHA256,
				SizeBytes:             staged.SizeBytes,
			}); err != nil {
				return err
			}
		}
		return nil
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
		BaseCommit: fixture.run.BaseCommit, CanonicalJSON: auditFixtureExecutionSpec("applier-only", fixture.run.Branch, fixture.run.BaseCommit),
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
	if packet.SchemaVersion != "2.0" {
		t.Fatalf("schema_version = %q", packet.SchemaVersion)
	}
}

func TestWorkflowAuditApplierOnlyPacketExcludesUnrelatedRunArtifacts(t *testing.T) {
	fixture := newAuditFixture(t, false)
	created, err := fixture.runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug: "applier-only-filtered", RepoTarget: fixture.run.RepoTarget, Branch: fixture.run.Branch,
		BaseCommit: fixture.run.BaseCommit, CanonicalJSON: auditFixtureExecutionSpec("applier-only-filtered", fixture.run.Branch, fixture.run.BaseCommit),
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
		if p["schema_version"] != "2.0" {
			t.Fatalf("schema_version = %v", p["schema_version"])
		}
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
		execution := p["execution"].(map[string]any)
		executor := execution["executor"].(map[string]any)
		result := executor["result"].(map[string]any)
		for _, forbidden := range []string{"effective_brief_artifact_id", "effective_brief_sha256", "effective_brief_mode"} {
			if _, exists := result[forbidden]; exists {
				t.Fatalf("nested result contains %s: %v", forbidden, result)
			}
		}
		for _, required := range []string{"effective_brief_artifact_reference", "effective_brief_sha256", "effective_brief_mode"} {
			if _, exists := executor[required]; !exists {
				t.Fatalf("outer executor evidence omitted %s: %v", required, executor)
			}
		}
	}

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
			if _, hasExitCode := entry["exit_code"]; hasExitCode {
				t.Fatalf("validation entry contains invented exit_code: %v", entry)
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
		CanonicalJSON:    auditFixtureExecutionSpec("remediation-run", fixture.run.Branch, fixture.run.BaseCommit),
		RenderedMarkdown: []byte("# Remediation brief\n"),
		RemediatesRunID:  fixture.run.RunID,
	})
	if err != nil {
		t.Fatal(err)
	}
	runArtifacts, err := fixture.store.ListArtifactsByRun(context.Background(), remediationRun.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var effectiveBrief workflowstore.Artifact
	for _, artifact := range runArtifacts {
		if artifact.Kind == "executor_brief" {
			effectiveBrief = artifact
		}
	}
	if effectiveBrief.ArtifactID == "" {
		t.Fatal("remediation executor brief artifact is missing")
	}
	runtimeJSON, err := json.Marshal(map[string]any{
		"normalized_status":           "done",
		"termination_verified":        true,
		"effective_brief_artifact_id": effectiveBrief.ArtifactID,
		"effective_brief_sha256":      effectiveBrief.SHA256,
		"effective_brief_mode":        "full",
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
	if _, err := runs.MarkExecutionAttemptRunning(context.Background(), begun.Attempt.AttemptID, string(runtimeJSON)); err != nil {
		t.Fatal(err)
	}
	finished, err := runs.FinishExecutionAttempt(context.Background(), workflowruns.FinishExecutionAttemptInput{
		AttemptID:  begun.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: string(runtimeJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	stageAttemptEvidence(t, fixture.store, finished.Attempt, effectiveBrief)

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

func TestWorkflowAuditHybridPacketConformsToCurrentSchema(t *testing.T) {
	fixture := newAuditFixture(t, false)
	stageApplierEvidence(t, fixture.store, fixture.run, "completed", []string{"1.1.file.1.change.1"})
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
	if packet.SchemaVersion != "2.0" ||
		packet.Execution.ActorKind != workflowstore.ImplementationActorHybrid ||
		packet.Execution.Applier == nil ||
		packet.Execution.Executor == nil {
		t.Fatalf("hybrid packet = %+v", packet)
	}
}

func TestWorkflowAuditPacketValidatorEnforcesClosedCurrentAuthority(t *testing.T) {
	fixture := newAuditFixture(t, true)
	if _, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	}); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var packet map[string]any
	if err := json.Unmarshal(current.PacketBytes, &packet); err != nil {
		t.Fatal(err)
	}
	requireAuditPacketValidity(t, packet, true)

	metadataVariants := []struct {
		name   string
		value  any
		absent bool
	}{
		{name: "absent", absent: true},
		{name: "null", value: nil},
		{name: "boolean", value: true},
		{name: "number", value: 7},
		{name: "object", value: map[string]any{"version": "2.0"}},
		{name: "array", value: []any{"2.0"}},
		{name: "malformed_string", value: "not-a-version"},
		{name: "stale", value: "1.0"},
		{name: "unsupported", value: "3.7"},
		{name: "future", value: "999.0"},
	}
	for _, variant := range metadataVariants {
		t.Run("metadata_"+variant.name, func(t *testing.T) {
			clone := cloneAuditPacket(t, packet)
			if variant.absent {
				delete(clone, "schema_version")
			} else {
				clone["schema_version"] = variant.value
			}
			requireAuditPacketValidity(t, clone, true)
		})
	}

	t.Run("nested_effective_brief_duplication", func(t *testing.T) {
		clone := cloneAuditPacket(t, packet)
		execution := clone["execution"].(map[string]any)
		executor := execution["executor"].(map[string]any)
		result := executor["result"].(map[string]any)
		result["effective_brief_mode"] = "full"
		requireAuditPacketValidity(t, clone, false)
	})

	t.Run("lexically_invalid_machine_string", func(t *testing.T) {
		clone := cloneAuditPacket(t, packet)
		clone["repository"].(map[string]any)["branch"] = " main"
		requireAuditPacketValidity(t, clone, false)
	})

	t.Run("historical_packet_shape_relabelled_current", func(t *testing.T) {
		clone := cloneAuditPacket(t, packet)
		clone["schema_version"] = "2.0"
		clone["execution"] = map[string]any{
			"status":                      "succeeded",
			"committed_sha":               fixture.head,
			"completion_summary":          "Execution completed.",
			"blockers_or_incomplete_work": []any{},
			"reported_changed_files":      []any{"internal/a.go"},
		}
		requireAuditPacketValidity(t, clone, false)
	})
}

func TestWorkflowAuditPacketSchemaFailureRollsBackAndPreservesCurrentPacket(t *testing.T) {
	fixture := newAuditFixture(t, false)
	first, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: fixture.head,
	})
	if err != nil {
		t.Fatal(err)
	}

	newHead := strings.Repeat("c", 40)
	fixture.service.inspector = func(_ context.Context, _ string, branch, base, audited string) (workflowrepos.AuditCommitEvidence, error) {
		return workflowrepos.AuditCommitEvidence{
			Branch:        branch,
			BaseCommit:    base,
			AuditedCommit: audited,
			ChangedFiles:  []string{"internal/a.go"},
			NameStatus:    "M\tinternal/a.go",
			DiffStat:      "1 file changed",
			CommitLog:     audited + "\tDev\t2026-07-06T00:00:00Z\tchange",
			Diff:          "diff --git a/internal/a.go b/internal/a.go\n+change\n",
		}, nil
	}
	validatorCalls := 0
	fixture.service.packetValidator = func([]byte) (bool, error) {
		validatorCalls++
		return false, nil
	}
	_, err = fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: newHead,
	})
	if !errors.Is(err, ErrWorkflowAuditPacketSchemaInvalid) {
		t.Fatalf("error = %v", err)
	}
	if validatorCalls != 1 {
		t.Fatalf("packet validator calls = %d, want 1", validatorCalls)
	}

	var currentCount int
	if err := fixture.store.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_packets WHERE run_row_id = ? AND status = ? AND audit_packet_id = ?`,
		fixture.run.ID,
		workflowstore.AuditPacketStatusCurrent,
		first.Packet.AuditPacketID,
	).Scan(&currentCount); err != nil {
		t.Fatal(err)
	}
	if currentCount != 1 {
		t.Fatalf("current packet count = %d, want 1", currentCount)
	}
	var packetRows int
	if err := fixture.store.DB().QueryRow(
		`SELECT COUNT(*) FROM audit_packets WHERE run_row_id = ?`,
		fixture.run.ID,
	).Scan(&packetRows); err != nil {
		t.Fatal(err)
	}
	if packetRows != 1 {
		t.Fatalf("packet row count = %d, want original row only", packetRows)
	}

	var packetArtifacts int
	if err := fixture.store.DB().QueryRow(
		`SELECT COUNT(*) FROM artifacts WHERE run_row_id = ? AND kind IN ('audit_packet', 'unified_diff')`,
		fixture.run.ID,
	).Scan(&packetArtifacts); err != nil {
		t.Fatal(err)
	}
	if packetArtifacts != 2 {
		t.Fatalf("packet artifact count = %d, want original pair only", packetArtifacts)
	}

	run, err := fixture.store.GetRunByRunID(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != workflowstore.RunStatusAuditReady {
		t.Fatalf("run status = %q, want audit_ready", run.Status)
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
	attempt, found, err := fixture.store.GetLatestSucceededExecutionAttemptOptional(context.Background(), fixture.run.ID)
	if err != nil || !found {
		t.Fatalf("selected attempt = %+v, found = %v, err = %v", attempt, found, err)
	}
	var currentResult WorkflowAuditAttemptResult
	if err := json.Unmarshal([]byte(attempt.ResultJSON), &currentResult); err != nil {
		t.Fatal(err)
	}
	unsafeResult, err := json.Marshal(map[string]any{
		"owner_instance_id":           "owner-private",
		"process_identity":            "pid-private",
		"command_preview":             `C:\Users\operator\relay ` + secret,
		"exit_code":                   0,
		"termination_verified":        true,
		"normalized_status":           "done",
		"error":                       "safe " + secret,
		"blocker_text":                "",
		"effective_brief_artifact_id": currentResult.EffectiveBriefArtifactID,
		"effective_brief_sha256":      currentResult.EffectiveBriefSHA256,
		"effective_brief_mode":        currentResult.EffectiveBriefMode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec(
		`UPDATE execution_attempts SET result_json = ? WHERE run_row_id = ?`,
		string(unsafeResult),
		fixture.run.ID,
	); err != nil {
		t.Fatal(err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range artifacts {
		if artifact.Kind != "executor_result" {
			continue
		}
		path, pathErr := workflowArtifactPath(fixture.store, artifact)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		if err := os.WriteFile(path, []byte(`C:\Users\operator\relay /home/operator/relay https://example.invalid/file?token=signed-value`), 0o600); err != nil {
			t.Fatal(err)
		}
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
	// Use non-authoritative raw Executor output for bounded readback mutation tests.
	var ref WorkflowAuditPacketArtifact
	for _, artifact := range packet.Artifacts {
		if artifact.ArtifactType == "executor_result" {
			ref = artifact
			break
		}
	}
	if ref.ArtifactReference == "" {
		t.Fatal("packet has no executor_result evidence reference")
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

func newTicketPackageAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
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
	const baseCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const treeOID = "cccccccccccccccccccccccccccccccccccccccc"
	var selection workflowstore.DeliveryTicketSelection
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
			RepoTarget: "relay", LocalPath: repoPath, ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		}); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{
			VaultID: "vault-ticket-audit", RepoTarget: "relay", RelativePath: "repositories/relay.git",
		})
		if err != nil {
			return err
		}
		closure, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: "closure-ticket-audit", CommitOID: baseCommit, TreeOID: treeOID,
			RefName: "refs/relay/closures/closure-ticket-audit", StartedAt: "2026-07-18T00:00:00.000000000Z",
		})
		if err != nil {
			return err
		}
		ready, err := tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: closure.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: "2026-07-18T00:00:01.000000000Z",
		})
		if err != nil {
			return err
		}
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: "project-ticket-audit", Name: "Ticket audit"})
		if err != nil {
			return err
		}
		workspace, err := tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{
			WorkspaceID: "workspace-ticket-audit", ProjectRowID: project.ID, FeatureSlug: "ticket-audit",
		})
		if err != nil {
			return err
		}
		authority, err := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-ticket-audit", WorkspaceRowID: workspace.ID, RevisionNumber: 1,
			SourceClosureRowID: sql.NullInt64{Int64: ready.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		workspace, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, authority.ID, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return err
		}
		ticket, err := tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{
			TicketID: "P6-T2", WorkspaceRowID: workspace.ID, ExternalPriority: 49,
		})
		if err != nil {
			return err
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main", BaseCommit: baseCommit,
			SourceClosureRowID: ready.ID, SourcePath: "tickets/P6-T2.delivery-ticket.json",
			Goal: "Bind exact ticket audit package evidence.", Context: "The audit packet must retain exact package provenance.",
			TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID); err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID: "approval-ticket-audit", RevisionRowID: revision.ID, ApprovalKind: "delivery", ApprovalState: "approved",
			Rationale: "Approve the exact ticket audit package.", SourceClosureRowID: ready.ID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: authority.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selection, err = tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID: "selection-ticket-audit", WorkspaceRowID: workspace.ID, State: "active",
			Rationale: "Reserve the exact approved audit ticket.", SourceClosureRowID: sql.NullInt64{Int64: ready.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: revision.ID, ApprovalRowID: approval.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	packageService, err := workflowpackages.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	brief := []byte("# Ticket Design Brief\n\n## Ticket Identity\n\n## Context\n\n## Design\n\n## Implementation Notes\n\n## Validation\n")
	spec := auditFixtureExecutionSpec("ticket-audit", "main", baseCommit)
	prepared, err := packageService.Prepare(ctx, workflowpackages.PrepareInput{
		SelectionID: selection.SelectionID,
		TicketDesignBriefs: []workflowpackages.ArtifactInput{{
			DisplayName: "ticket-audit.ticket-P6-T2.r1.design-brief.md", ExpectedSHA256: sha256HexBytes(brief), Bytes: brief,
		}},
		ExecutionSpec: workflowpackages.ArtifactInput{
			DisplayName: "ticket-audit.execution-spec.json", ExpectedSHA256: sha256HexBytes(spec), Bytes: spec,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := packageService.Approve(ctx, workflowpackages.ApproveInput{PackageID: prepared.Package.PackageID})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	var effectiveBrief workflowstore.Artifact
	for _, artifact := range approved.RunArtifacts {
		if artifact.Kind == "executor_brief" {
			effectiveBrief = artifact
		}
	}
	if effectiveBrief.ID == 0 {
		t.Fatal("package Run has no executor brief")
	}
	runtimeJSON, err := json.Marshal(map[string]any{
		"normalized_status": "done", "termination_verified": true,
		"effective_brief_artifact_id": effectiveBrief.ArtifactID, "effective_brief_sha256": effectiveBrief.SHA256,
		"effective_brief_mode": "full",
	})
	if err != nil {
		t.Fatal(err)
	}
	begun, err := runs.BeginExecutionAttempt(ctx, workflowruns.BeginExecutionAttemptInput{RunID: approved.Run.RunID, Adapter: "codex", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkExecutionAttemptRunning(ctx, begun.Attempt.AttemptID, string(runtimeJSON)); err != nil {
		t.Fatal(err)
	}
	finished, err := runs.FinishExecutionAttempt(ctx, workflowruns.FinishExecutionAttemptInput{
		AttemptID: begun.Attempt.AttemptID, Status: workflowstore.AttemptStatusSucceeded, ResultJSON: string(runtimeJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	stageAttemptEvidence(t, store, finished.Attempt, effectiveBrief)
	fixture := &auditFixture{store: store, runs: runs, run: finished.Run, head: strings.Repeat("b", 40)}
	fixture.service, err = NewWorkflowAuditServiceWithInspector(store, func(_ context.Context, _ string, branch, base, audited string) (workflowrepos.AuditCommitEvidence, error) {
		if audited != fixture.head {
			return workflowrepos.AuditCommitEvidence{}, errors.New("head_mismatch")
		}
		return workflowrepos.AuditCommitEvidence{
			Branch: branch, BaseCommit: base, AuditedCommit: audited, ChangedFiles: []string{"internal/a.go"},
			NameStatus: "M\tinternal/a.go", DiffStat: "1 file changed", CommitLog: audited + "\tDev\t2026-07-18T00:00:00Z\tchange",
			Diff: "diff --git a/internal/a.go b/internal/a.go\n+change\n",
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestWorkflowAuditTicketPackageEvidenceIsBoundedAndFresh(t *testing.T) {
	fixture := newTicketPackageAuditFixture(t)
	prepared, err := fixture.service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head})
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
	packageEvidence, found, err := workflowAuditTicketPackageArtifact(packet)
	if err != nil || !found {
		t.Fatalf("ticket package evidence = %#v, found=%t, err=%v", packageEvidence, found, err)
	}
	for _, kind := range []string{workflowAuditArtifactTypeTicketPackageEvidence, workflowAuditArtifactTypeApprovedExecutionSpec, workflowAuditArtifactTypeTicketDesignBrief} {
		matched := false
		for _, artifact := range packet.Artifacts {
			matched = matched || artifact.ArtifactType == kind
		}
		if !matched {
			t.Fatalf("packet has no %s artifact", kind)
		}
	}
	read, err := fixture.service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
		RunID: fixture.run.RunID, ArtifactReference: packageEvidence.ArtifactReference, MaxBytes: MaxWorkflowAuditReadBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	var evidence WorkflowAuditTicketPackageEvidence
	if err := json.Unmarshal(read.Content, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Package.ExecutionSpec.ArtifactReference == "" || len(evidence.Tickets) != 1 ||
		evidence.Tickets[0].TicketID != "P6-T2" || evidence.Tickets[0].DesignBrief.ArtifactReference == "" ||
		evidence.Package.PackageRowID != fixture.run.ExecutionPackageRowID.Int64 || prepared.Packet.PacketSHA256 != current.Packet.PacketSHA256 {
		t.Fatalf("ticket package evidence = %#v", evidence)
	}

	lease, err := fixture.runs.AcquireRunMutationLease(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runs.MarkRunMutationLeaseUncertain(context.Background(), fixture.run.RunID, lease.LeaseID, "test uncertainty"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.GetCurrentPacket(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowAuditPacketStale) {
		t.Fatalf("uncertain lease current packet error = %v", err)
	}
}

func TestWorkflowAuditTicketPackageStalesWhenCurrentTicketRevisionChanges(t *testing.T) {
	ctx := context.Background()
	fixture := newTicketPackageAuditFixture(t)
	if _, err := fixture.service.Prepare(ctx, PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: fixture.head}); err != nil {
		t.Fatal(err)
	}
	pkg, err := getWorkflowAuditExecutionPackageByRowID(ctx, fixture.store, fixture.run.ExecutionPackageRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	members, err := fixture.store.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("package members = %#v, err=%v", members, err)
	}
	revision, err := fixture.store.GetDeliveryTicketRevisionByRowID(ctx, members[0].RevisionRowID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := fixture.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		replacement, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: revision.RevisionNumber + 1,
			ReplacesRevisionRowID: sql.NullInt64{Int64: revision.ID, Valid: true}, RepoTarget: revision.RepoTarget,
			Branch: revision.Branch, BaseCommit: revision.BaseCommit, SourceClosureRowID: revision.SourceClosureRowID,
			SourcePath: revision.SourcePath, Goal: revision.Goal, Context: revision.Context,
			TransitionApplicability: revision.TransitionApplicability,
		})
		if err != nil {
			return err
		}
		_, err = tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, replacement.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.GetCurrentPacket(ctx, fixture.run.RunID); !errors.Is(err, ErrWorkflowAuditPacketStale) {
		t.Fatalf("replaced ticket revision current packet error = %v", err)
	}
}

var _ = json.Valid
