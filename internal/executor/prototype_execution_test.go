package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/pipeline"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

func prototypeEnvelopeFixture() (workflowstore.PrototypeRun, workflowstore.PrototypeAuthorization, workflowstore.PrototypeProposal, []byte) {
	a := workflowstore.PrototypeAuthorization{ProposedRunID: "prototype-run-1", AuthorizationID: "prototype-authorization-1", InvocationSHA256: strings.Repeat("a", 64), SourceCommit: strings.Repeat("b", 40), BaseCommit: strings.Repeat("b", 40), Adapter: "codex", Model: "model"}
	p := workflowstore.PrototypeProposal{ProposalID: "prototype-proposal-1"}
	data := []byte(`{"schema_version":1,"run_id":"prototype-run-1","proposal_id":"prototype-proposal-1","authorization_id":"prototype-authorization-1","invocation_sha256":"` + a.InvocationSHA256 + `","source_commit":"` + a.SourceCommit + `","base_commit":"` + a.BaseCommit + `","adapter":"codex","model":"model","outcome":"succeeded","variant_results":[],"validations":[],"evidence":[]}`)
	return workflowstore.PrototypeRun{}, a, p, data
}

func TestPrototypeLaunchProtocol(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := filepath.Join(t.TempDir(), "prototype")
	p, err := NewPrototypeExecution(store, "owner-test", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Launch(context.Background(), prototypeexecution.LaunchRequest{RunID: "missing", ExpectedRunVersion: 1, MutationIdentity: "launch"}); err == nil {
		t.Fatal("launch accepted a missing durable run")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("runtime root was not created: %v", err)
	}
	if _, err := p.Launch(context.Background(), prototypeexecution.LaunchRequest{RunID: "", ExpectedRunVersion: 1, MutationIdentity: "launch"}); !errors.Is(err, prototypeexecution.ErrInvocation) {
		t.Fatalf("invalid launch=%v", err)
	}
}

func TestPrototypeReconciliation(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, err := NewPrototypeExecution(store, "owner", filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.DB().QueryContext(context.Background(), `SELECT prototype_run_id FROM feature_workspace_prototype_runs`)
	if err != nil {
		t.Fatal(err)
	}
	before.Close()
	if _, err := p.Reconcile(context.Background(), prototypeexecution.OperationRequest{RunID: "missing", MutationIdentity: "reconcile"}); err == nil {
		t.Fatal("reconciliation accepted unknown run")
	}
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_prototype_evidence_import_batches`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reconciliation created artifacts for unknown run: %d %v", count, err)
	}
}

func TestPrototypeCancellationAndTimeout(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, err := NewPrototypeExecution(store, "owner", filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Cancel(context.Background(), prototypeexecution.OperationRequest{RunID: "run", MutationIdentity: ""}); !errors.Is(err, prototypeexecution.ErrCancellation) {
		t.Fatalf("missing cancellation identity=%v", err)
	}
	if _, err := p.SettleTimeout(context.Background(), prototypeexecution.OperationRequest{RunID: "run", MutationIdentity: ""}); !errors.Is(err, prototypeexecution.ErrTimeout) {
		t.Fatalf("missing timeout identity=%v", err)
	}
	if _, err := p.Cancel(context.Background(), prototypeexecution.OperationRequest{RunID: "missing", MutationIdentity: "cancel"}); err == nil {
		t.Fatal("cancel accepted missing durable run")
	}
}

func TestPrototypeResultAndEvidenceFinalization(t *testing.T) {
	run, authorization, proposal, data := prototypeEnvelopeFixture()
	zero := 0
	if _, err := validatePrototypeResultEnvelope(data, run, authorization, proposal, &zero); err != nil {
		t.Fatal(err)
	}
	failed := strings.Replace(string(data), `"outcome":"succeeded"`, `"outcome":"failed"`, 1)
	if _, err := validatePrototypeResultEnvelope([]byte(failed), run, authorization, proposal, &zero); !errors.Is(err, prototypeexecution.ErrResultInvalid) {
		t.Fatalf("zero-exit mismatch=%v", err)
	}
	nonzero := 1
	if _, err := validatePrototypeResultEnvelope(data, run, authorization, proposal, &nonzero); !errors.Is(err, prototypeexecution.ErrResultInvalid) {
		t.Fatalf("nonzero success=%v", err)
	}
}

func TestPrototypeEvidenceSafety(t *testing.T) {
	worktree := t.TempDir()
	export := filepath.Join(worktree, ".relay", "prototype", "export")
	if err := os.MkdirAll(export, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("safe evidence\n")
	file := filepath.Join(export, "report.txt")
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	valid := prototypeEvidenceCandidate{SemanticRole: "report", RelativePath: ".relay/prototype/export/report.txt", SHA256: hex.EncodeToString(digest[:]), MediaType: "text/plain"}
	if got, err := prototypeCandidatePath(worktree, valid); err != nil || got != file {
		t.Fatalf("valid evidence=%q %v", got, err)
	}
	for _, candidate := range []prototypeEvidenceCandidate{
		{SemanticRole: "absolute", RelativePath: file, SHA256: valid.SHA256, MediaType: "text/plain"},
		{SemanticRole: "traversal", RelativePath: ".relay/prototype/export/../secret", SHA256: valid.SHA256, MediaType: "text/plain"},
		{SemanticRole: "directory", RelativePath: ".relay/prototype/export", SHA256: valid.SHA256, MediaType: "text/plain"},
		{SemanticRole: "secret", RelativePath: ".relay/prototype/export/secret.txt", SHA256: valid.SHA256, MediaType: "text/plain"},
	} {
		if candidate.SemanticRole == "secret" {
			if err := os.WriteFile(filepath.Join(export, "secret.txt"), []byte("OPENAI_API_KEY=value"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := prototypeCandidatePath(worktree, candidate); !errors.Is(err, prototypeexecution.ErrEvidenceUnsafe) {
			t.Fatalf("%s accepted: %v", candidate.SemanticRole, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(export, "escape.txt")); err == nil {
		candidate := valid
		candidate.RelativePath = ".relay/prototype/export/escape.txt"
		if _, err := prototypeCandidatePath(worktree, candidate); !errors.Is(err, prototypeexecution.ErrEvidenceUnsafe) {
			t.Fatalf("symlink escape accepted: %v", err)
		}
	}
}

func TestPrototypePart2Boundary(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, err := NewPrototypeExecution(store, "owner", filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Reconcile(context.Background(), prototypeexecution.OperationRequest{RunID: "missing", MutationIdentity: "reconcile"}); err == nil {
		t.Fatal("missing run reconciliation succeeded")
	}
	for _, table := range []string{"repository_targets", "repository_branch_mutation_leases", "feature_workspace_prototype_result_members"} {
		var count int
		if err := store.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("Part 2 boundary %s count=%d err=%v", table, count, err)
		}
	}
}

func TestPrototypeTimeoutLimits(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      time.Duration
		valid     bool
	}{
		{"minimum", `{"timeout_seconds":1}`, time.Second, true}, {"maximum", `{"timeout_seconds":7200}`, 7200 * time.Second, true}, {"zero", `{"timeout_seconds":0}`, 0, false}, {"above maximum", `{"timeout_seconds":7201}`, 0, false}, {"decimal", `{"timeout_seconds":1800.0}`, 0, false}, {"exponent", `{"timeout_seconds":1.8e3}`, 0, false}, {"quoted", `{"timeout_seconds":"1800"}`, 0, false}, {"null", `{"timeout_seconds":null}`, 0, false}, {"missing", `{}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodePrototypeTimeoutLimits(tc.raw)
			if tc.valid {
				if err != nil || got != tc.want {
					t.Fatalf("decode=%v duration=%v", err, got)
				}
			} else if !errors.Is(err, prototypeexecution.ErrLimitsInvalid) {
				t.Fatalf("accepted invalid limit: %v", err)
			}
		})
	}
}

func TestPrototypeResultEnvelopeValidation(t *testing.T) {
	run, authorization, proposal, data := prototypeEnvelopeFixture()
	zero := 0
	if _, err := validatePrototypeResultEnvelope(data, run, authorization, proposal, &zero); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"run", `prototype-run-1`, `wrong-run`}, {"proposal", `prototype-proposal-1`, `wrong-proposal`}, {"authorization", `prototype-authorization-1`, `wrong-authorization`}, {"digest", strings.Repeat("a", 64), strings.Repeat("c", 64)}, {"source", strings.Repeat("b", 40), strings.Repeat("d", 40)}, {"adapter", `codex`, `other`}, {"model", `model`, `other-model`}, {"outcome", `succeeded`, `unknown`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := []byte(strings.Replace(string(data), tc.from, tc.to, 1))
			if _, err := validatePrototypeResultEnvelope(bad, run, authorization, proposal, &zero); !errors.Is(err, prototypeexecution.ErrResultInvalid) {
				t.Fatalf("accepted %s mismatch: %v", tc.name, err)
			}
		})
	}
	if _, err := validatePrototypeResultEnvelope(append(data, []byte(` {}`)...), run, authorization, proposal, &zero); !errors.Is(err, prototypeexecution.ErrResultInvalid) {
		t.Fatalf("trailing json=%v", err)
	}
}

type prototypeRegressionProcessController struct {
	openCalls int
}

func (c *prototypeRegressionProcessController) StartOwned(context.Context, pipeline.CommandSpec) (pipeline.OwnedProcess, error) {
	return nil, errors.New("unexpected process start")
}

func (c *prototypeRegressionProcessController) OpenOwned(pipeline.ProcessIdentity) (pipeline.OwnedProcess, error) {
	c.openCalls++
	return nil, errors.New("unexpected process ownership lookup")
}

func prototypeRegressionFixture(t *testing.T, running bool, obligations string) (*PrototypeExecution, *workflowstore.Store, workflowstore.PrototypeRun, workflowstore.PrototypeAuthorization, workflowstore.PrototypeProposal, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	db := store.DB()
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'prototype_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range triggers {
		if _, err := db.Exec(`DROP TRIGGER ` + name); err != nil {
			t.Fatal(err)
		}
	}
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const tree = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const proposalSHA = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	const invocationSHA = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if _, err := db.Exec(`INSERT INTO feature_workspaces(id,workspace_id,project_row_id,feature_slug) VALUES(1,'workspace-prototype-regression',1,'prototype-regression'); INSERT INTO feature_workspace_discovery_tickets(id,discovery_ticket_id,workspace_row_id,ticket_key,subject) VALUES(1,'discovery-prototype-regression',1,'PROTOTYPE-REGRESSION','prototype regression')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_prototype_proposals(id,proposal_id,workspace_row_id,work_item_row_id,discovery_revision_row_id,artifact_row_id,proposal_sha256,proposal_size_bytes,proposal_media_type) VALUES(1,'prototype-proposal-regression',1,1,1,1,?,0,'application/json')`, proposalSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_prototype_authorizations(id,authorization_id,proposal_row_id,proposed_run_id,workspace_row_id,workspace_version,work_item_row_id,work_item_version,discovery_revision_row_id,proposal_sha256,source_closure_row_id,source_commit,source_tree,repo_target,base_commit,adapter,model,variants_json,evidence_obligations_json,limits_json,invocation_artifact_row_id,invocation_sha256,invocation_size_bytes,invocation_media_type) VALUES(1,'prototype-authorization-regression',1,'prototype-run-regression',1,1,1,1,1,?,1,?,?, 'prototype-repo',?,'adapter','model','[]',?,'{}',1,?,0,'application/json')`, proposalSHA, commit, tree, commit, obligations, invocationSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_prototype_runs(id,prototype_run_id,authorization_row_id,workspace_row_id,work_item_row_id,lifecycle_state,version) VALUES(1,'prototype-run-regression',1,1,1,'approved',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}

	worktree := filepath.Join(root, "worktree")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := workflowstore.PrototypeRuntime{RuntimeID: "prototype-runtime-regression", AuthorizedCommit: commit, AuthorizedTree: tree, RuntimeRootPath: filepath.Join(root, "runtime"), WorktreePath: worktree, EphemeralTargetKey: "prototype:regression", LeaseToken: "prototype-lease-regression", BackgroundContextID: "prototype-context-regression", DeadlineAt: "2026-01-01T00:00:00Z"}
	target := workflowstore.PrototypeTarget{TargetID: "prototype-target-regression", TargetKey: runtime.EphemeralTargetKey, WorktreePath: runtime.WorktreePath, AuthorizedCommit: commit, AuthorizedTree: tree}
	lease := workflowstore.PrototypeLease{LeaseToken: runtime.LeaseToken, EphemeralTargetKey: runtime.EphemeralTargetKey, OwnerInstanceID: "prototype-owner-regression"}
	var run workflowstore.PrototypeRun
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var txErr error
		run, runtime, target, lease, txErr = tx.ReservePrototypeRuntime(ctx, "prototype-run-regression", 1, runtime, target, lease)
		return txErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, _, txErr := tx.MarkPrototypePreparationReady(ctx, run.PrototypeRunID, run.Version)
		return txErr
	}); err != nil {
		t.Fatal(err)
	}
	run, err = store.GetPrototypeRun(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var txErr error
		run, _, txErr = tx.ClaimPrototypeLaunch(ctx, run.PrototypeRunID, run.Version, "prototype-launch-claim-regression", "test")
		return txErr
	}); err != nil {
		t.Fatal(err)
	}
	if running {
		identity := `{"pid":42,"started_at":"2026-01-01T00:00:00Z","platform":"linux"}`
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			var txErr error
			run, runtime, txErr = tx.PersistPrototypeProcessIdentity(ctx, run.PrototypeRunID, run.Version, identity, "2026-01-01T00:00:00Z")
			return txErr
		}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewPrototypeExecution(store, "prototype-owner-regression", filepath.Join(root, "prototype-runtime-root"))
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := store.GetPrototypeAuthorization(ctx, "prototype-authorization-regression")
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := store.GetPrototypeProposal(ctx, "prototype-proposal-regression")
	if err != nil {
		t.Fatal(err)
	}
	return p, store, run, authorization, proposal, worktree
}

func prototypeCleanupStatus(t *testing.T, store *workflowstore.Store, runID, kind string) string {
	t.Helper()
	var status string
	if err := store.DB().QueryRow(`SELECT c.status FROM feature_workspace_prototype_cleanup_obligations c JOIN feature_workspace_prototype_runs r ON r.id=c.run_row_id WHERE r.prototype_run_id=? AND c.obligation_kind=?`, runID, kind).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestPrototypeUnverifiablePersistedOwnershipRequiresCleanup(t *testing.T) {
	ctx := context.Background()
	p, store, run, _, _, _ := prototypeRegressionFixture(t, true, `[]`)
	const persistedIdentity = "not-decodable-process-identity"
	if _, err := store.DB().Exec(`UPDATE feature_workspace_prototype_runtimes SET process_identity=? WHERE run_row_id=?`, persistedIdentity, run.ID); err != nil {
		t.Fatal(err)
	}

	result, err := p.Reconcile(ctx, prototypeexecution.OperationRequest{RunID: run.PrototypeRunID, MutationIdentity: "reconcile-unverifiable"})
	if !errors.Is(err, prototypeexecution.ErrCleanupRequired) {
		t.Fatalf("reconcile error = %v, want cleanup required", err)
	}
	if result.Run.LifecycleState != "cleanup_required" {
		t.Fatalf("result lifecycle = %q, want cleanup_required", result.Run.LifecycleState)
	}
	runtime, err := store.GetPrototypeRuntimeByRunID(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.LaunchPhase != "ownership_unresolved" || !runtime.ProcessIdentity.Valid || runtime.ProcessIdentity.String != persistedIdentity {
		t.Fatalf("runtime = %#v, want retained identity and ownership_unresolved", runtime)
	}
	if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, "process_ownership"); got != "pending" {
		t.Fatalf("process ownership cleanup = %q, want pending", got)
	}
}

func TestPrototypeClaimedWithoutIdentityCancellationRequiresCleanup(t *testing.T) {
	ctx := context.Background()
	p, store, run, _, _, _ := prototypeRegressionFixture(t, false, `[]`)
	controller := &prototypeRegressionProcessController{}
	p.controller = controller

	result, err := p.Cancel(ctx, prototypeexecution.OperationRequest{RunID: run.PrototypeRunID, MutationIdentity: "cancel-claimed-without-identity"})
	if !errors.Is(err, prototypeexecution.ErrCleanupRequired) {
		t.Fatalf("cancel error = %v, want cleanup required", err)
	}
	if result.Run.LifecycleState != "cleanup_required" {
		t.Fatalf("result lifecycle = %q, want cleanup_required", result.Run.LifecycleState)
	}
	runtime, err := store.GetPrototypeRuntimeByRunID(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.CancelIdentity.Valid || runtime.CancelIdentity.String != "cancel-claimed-without-identity" || runtime.LaunchPhase != "ownership_unresolved" {
		t.Fatalf("runtime = %#v, want persisted cancellation identity and ownership_unresolved", runtime)
	}
	if controller.openCalls != 0 {
		t.Fatalf("process ownership lookup count = %d, want no termination attempt", controller.openCalls)
	}
	if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, "process_ownership"); got != "pending" {
		t.Fatalf("process ownership cleanup = %q, want pending", got)
	}
	rows, err := store.DB().Query(`SELECT from_state || '>' || to_state FROM feature_workspace_prototype_lifecycle_transitions WHERE run_row_id=? ORDER BY id`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var transitions []string
	for rows.Next() {
		var transition string
		if err := rows.Scan(&transition); err != nil {
			t.Fatal(err)
		}
		transitions = append(transitions, transition)
	}
	want := []string{"approved>preparing", "preparing>launch_uncertain", "launch_uncertain>cleanup_required"}
	if strings.Join(transitions, ",") != strings.Join(want, ",") {
		t.Fatalf("transitions = %#v, want %#v", transitions, want)
	}
}

func TestPrototypePartialEvidenceSettlementStaysPending(t *testing.T) {
	ctx := context.Background()
	p, store, run, _, _, _ := prototypeRegressionFixture(t, true, `[]`)
	zero := 0

	result, err := p.settleObservedOutcome(ctx, run.PrototypeRunID, run.Version, "runner_success", "partial-evidence", "succeeded", &zero)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.LifecycleState == "succeeded" {
		t.Fatal("missing result envelope settled as succeeded")
	}
	batches, err := store.ListPrototypeEvidenceBatches(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || batches[0].Completeness != "partial" || batches[0].EnvelopeStatus != "missing" {
		t.Fatalf("evidence batches = %#v, want one missing partial batch", batches)
	}
	var results int
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_prototype_results WHERE run_row_id=?`, run.ID).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if results != 0 {
		t.Fatalf("final results = %d, want none for partial evidence", results)
	}
	if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, "evidence_settlement"); got != "pending" {
		t.Fatalf("evidence settlement cleanup = %q, want pending", got)
	}
}

func TestPrototypeCompleteEvidenceSettlementPersistsCompatibilityMembersWithoutReplayDuplicates(t *testing.T) {
	ctx := context.Background()
	p, store, run, authorization, proposal, worktree := prototypeRegressionFixture(t, true, `["report"]`)
	export := filepath.Join(worktree, ".relay", "prototype", "export")
	if err := os.MkdirAll(export, 0o700); err != nil {
		t.Fatal(err)
	}
	evidence := []byte("complete evidence\n")
	if err := os.WriteFile(filepath.Join(export, "report.txt"), evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(evidence)
	envelope, err := json.Marshal(prototypeResultEnvelope{SchemaVersion: 1, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, SourceCommit: authorization.SourceCommit, BaseCommit: authorization.BaseCommit, Adapter: authorization.Adapter, Model: authorization.Model, Outcome: "succeeded", Evidence: []prototypeEvidenceCandidate{{SemanticRole: "report", RelativePath: ".relay/prototype/export/report.txt", SHA256: hex.EncodeToString(digest[:]), MediaType: "text/plain", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(worktree, ".relay", "prototype", "result.json")
	if err := os.WriteFile(resultPath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	zero := 0
	if _, err := p.settleObservedOutcome(ctx, run.PrototypeRunID, run.Version, "runner_success", "complete-evidence", "succeeded", &zero); err != nil {
		t.Fatal(err)
	}
	if _, err := p.settleObservedOutcome(ctx, run.PrototypeRunID, run.Version, "runner_success", "complete-evidence", "succeeded", &zero); err != nil {
		t.Fatal(err)
	}

	batches, err := store.ListPrototypeEvidenceBatches(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || batches[0].Completeness != "complete" || batches[0].EnvelopeStatus != "valid" {
		t.Fatalf("evidence batches = %#v, want one valid complete batch", batches)
	}
	if _, err := store.GetPrototypeResultByRunID(ctx, run.PrototypeRunID); err != nil {
		t.Fatalf("complete evidence did not create final result: %v", err)
	}
	if got := prototypeCleanupStatus(t, store, run.PrototypeRunID, "evidence_settlement"); got != "complete" {
		t.Fatalf("evidence settlement cleanup = %q, want complete", got)
	}
	members, err := store.ListPrototypeResultMembers(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].Sequence != 1 || members[0].MemberKind != "result_envelope" || members[1].Sequence != 2 || members[1].MemberKind != "evidence:report" {
		t.Fatalf("compatibility result members = %#v, want deterministic envelope then evidence", members)
	}
	var resultCount, memberCount int
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_prototype_results WHERE run_row_id=?`, run.ID).Scan(&resultCount); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_prototype_result_members WHERE run_row_id=?`, run.ID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if resultCount != 1 || memberCount != 2 {
		t.Fatalf("replay created rows: results=%d members=%d, want 1 and 2", resultCount, memberCount)
	}
}
