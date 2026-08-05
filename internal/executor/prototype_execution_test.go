package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if err != nil { t.Fatal(err) }
	defer store.Close()
	root := filepath.Join(t.TempDir(), "prototype")
	p, err := NewPrototypeExecution(store, "owner-test", root)
	if err != nil { t.Fatal(err) }
	if _, err := p.Launch(context.Background(), prototypeexecution.LaunchRequest{RunID: "missing", ExpectedRunVersion: 1, MutationIdentity: "launch"}); err == nil {
		t.Fatal("launch accepted a missing durable run")
	}
	if _, err := os.Stat(root); err != nil { t.Fatalf("runtime root was not created: %v", err) }
	if _, err := p.Launch(context.Background(), prototypeexecution.LaunchRequest{RunID: "", ExpectedRunVersion: 1, MutationIdentity: "launch"}); !errors.Is(err, prototypeexecution.ErrInvocation) {
		t.Fatalf("invalid launch=%v", err)
	}
}

func TestPrototypeReconciliation(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	p, err := NewPrototypeExecution(store, "owner", filepath.Join(t.TempDir(), "runtime"))
	if err != nil { t.Fatal(err) }
	before, err := store.DB().QueryContext(context.Background(), `SELECT prototype_run_id FROM feature_workspace_prototype_runs`)
	if err != nil { t.Fatal(err) }; before.Close()
	if _, err := p.Reconcile(context.Background(), prototypeexecution.OperationRequest{RunID: "missing", MutationIdentity: "reconcile"}); err == nil { t.Fatal("reconciliation accepted unknown run") }
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_prototype_evidence_import_batches`).Scan(&count); err != nil || count != 0 { t.Fatalf("reconciliation created artifacts for unknown run: %d %v", count, err) }
}

func TestPrototypeCancellationAndTimeout(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil { t.Fatal(err) }; defer store.Close()
	p, err := NewPrototypeExecution(store, "owner", filepath.Join(t.TempDir(), "runtime"))
	if err != nil { t.Fatal(err) }
	if _, err := p.Cancel(context.Background(), prototypeexecution.OperationRequest{RunID: "run", MutationIdentity: ""}); !errors.Is(err, prototypeexecution.ErrCancellation) { t.Fatalf("missing cancellation identity=%v", err) }
	if _, err := p.SettleTimeout(context.Background(), prototypeexecution.OperationRequest{RunID: "run", MutationIdentity: ""}); !errors.Is(err, prototypeexecution.ErrTimeout) { t.Fatalf("missing timeout identity=%v", err) }
	if _, err := p.Cancel(context.Background(), prototypeexecution.OperationRequest{RunID: "missing", MutationIdentity: "cancel"}); err == nil { t.Fatal("cancel accepted missing durable run") }
}

func TestPrototypeResultAndEvidenceFinalization(t *testing.T) {
	run, authorization, proposal, data := prototypeEnvelopeFixture()
	zero := 0
	if _, err := validatePrototypeResultEnvelope(data, run, authorization, proposal, &zero); err != nil { t.Fatal(err) }
	failed := strings.Replace(string(data), `"outcome":"succeeded"`, `"outcome":"failed"`, 1)
	if _, err := validatePrototypeResultEnvelope([]byte(failed), run, authorization, proposal, &zero); !errors.Is(err, prototypeexecution.ErrResultInvalid) { t.Fatalf("zero-exit mismatch=%v", err) }
	nonzero := 1
	if _, err := validatePrototypeResultEnvelope(data, run, authorization, proposal, &nonzero); !errors.Is(err, prototypeexecution.ErrResultInvalid) { t.Fatalf("nonzero success=%v", err) }
}

func TestPrototypeEvidenceSafety(t *testing.T) {
	worktree := t.TempDir()
	export := filepath.Join(worktree, ".relay", "prototype", "export")
	if err := os.MkdirAll(export, 0700); err != nil { t.Fatal(err) }
	content := []byte("safe evidence\n")
	file := filepath.Join(export, "report.txt")
	if err := os.WriteFile(file, content, 0600); err != nil { t.Fatal(err) }
	digest := sha256.Sum256(content)
	valid := prototypeEvidenceCandidate{SemanticRole: "report", RelativePath: ".relay/prototype/export/report.txt", SHA256: hex.EncodeToString(digest[:]), MediaType: "text/plain"}
	if got, err := prototypeCandidatePath(worktree, valid); err != nil || got != file { t.Fatalf("valid evidence=%q %v", got, err) }
	for _, candidate := range []prototypeEvidenceCandidate{
		{SemanticRole: "absolute", RelativePath: file, SHA256: valid.SHA256, MediaType: "text/plain"},
		{SemanticRole: "traversal", RelativePath: ".relay/prototype/export/../secret", SHA256: valid.SHA256, MediaType: "text/plain"},
		{SemanticRole: "directory", RelativePath: ".relay/prototype/export", SHA256: valid.SHA256, MediaType: "text/plain"},
		{SemanticRole: "secret", RelativePath: ".relay/prototype/export/secret.txt", SHA256: valid.SHA256, MediaType: "text/plain"},
	} {
		if candidate.SemanticRole == "secret" { if err := os.WriteFile(filepath.Join(export, "secret.txt"), []byte("OPENAI_API_KEY=value"), 0600); err != nil { t.Fatal(err) } }
		if _, err := prototypeCandidatePath(worktree, candidate); !errors.Is(err, prototypeexecution.ErrEvidenceUnsafe) { t.Fatalf("%s accepted: %v", candidate.SemanticRole, err) }
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, content, 0600); err != nil { t.Fatal(err) }
	if err := os.Symlink(outside, filepath.Join(export, "escape.txt")); err == nil {
		candidate := valid; candidate.RelativePath = ".relay/prototype/export/escape.txt"
		if _, err := prototypeCandidatePath(worktree, candidate); !errors.Is(err, prototypeexecution.ErrEvidenceUnsafe) { t.Fatalf("symlink escape accepted: %v", err) }
	}
}

func TestPrototypePart2Boundary(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil { t.Fatal(err) }; defer store.Close()
	p, err := NewPrototypeExecution(store, "owner", filepath.Join(t.TempDir(), "runtime"))
	if err != nil { t.Fatal(err) }
	if _, err := p.Reconcile(context.Background(), prototypeexecution.OperationRequest{RunID: "missing", MutationIdentity: "reconcile"}); err == nil { t.Fatal("missing run reconciliation succeeded") }
	for _, table := range []string{"repository_targets", "repository_branch_mutation_leases", "feature_workspace_prototype_result_members"} {
		var count int; if err := store.DB().QueryRow(`SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 { t.Fatalf("Part 2 boundary %s count=%d err=%v", table, count, err) }
	}
}

func TestPrototypeTimeoutLimits(t *testing.T) {
	for _, tc := range []struct { name, raw string; want time.Duration; valid bool }{
		{"minimum", `{"timeout_seconds":1}`, time.Second, true}, {"maximum", `{"timeout_seconds":7200}`, 7200*time.Second, true}, {"zero", `{"timeout_seconds":0}`, 0, false}, {"above maximum", `{"timeout_seconds":7201}`, 0, false}, {"decimal", `{"timeout_seconds":1800.0}`, 0, false}, {"exponent", `{"timeout_seconds":1.8e3}`, 0, false}, {"quoted", `{"timeout_seconds":"1800"}`, 0, false}, {"null", `{"timeout_seconds":null}`, 0, false}, {"missing", `{}`, 0, false},
	} { t.Run(tc.name, func(t *testing.T) { got, err := decodePrototypeTimeoutLimits(tc.raw); if tc.valid { if err != nil || got != tc.want { t.Fatalf("decode=%v duration=%v", err, got) } } else if !errors.Is(err, prototypeexecution.ErrLimitsInvalid) { t.Fatalf("accepted invalid limit: %v", err) } }) }
}

func TestPrototypeResultEnvelopeValidation(t *testing.T) {
	run, authorization, proposal, data := prototypeEnvelopeFixture(); zero := 0
	if _, err := validatePrototypeResultEnvelope(data, run, authorization, proposal, &zero); err != nil { t.Fatal(err) }
	for _, tc := range []struct{name, from, to string}{
		{"run", `prototype-run-1`, `wrong-run`}, {"proposal", `prototype-proposal-1`, `wrong-proposal`}, {"authorization", `prototype-authorization-1`, `wrong-authorization`}, {"digest", strings.Repeat("a",64), strings.Repeat("c",64)}, {"source", strings.Repeat("b",40), strings.Repeat("d",40)}, {"adapter", `codex`, `other`}, {"model", `model`, `other-model`}, {"outcome", `succeeded`, `unknown`},
	} { t.Run(tc.name, func(t *testing.T) { bad := []byte(strings.Replace(string(data), tc.from, tc.to, 1)); if _, err := validatePrototypeResultEnvelope(bad, run, authorization, proposal, &zero); !errors.Is(err, prototypeexecution.ErrResultInvalid) { t.Fatalf("accepted %s mismatch: %v", tc.name, err) } }) }
	if _, err := validatePrototypeResultEnvelope(append(data, []byte(` {}`)...), run, authorization, proposal, &zero); !errors.Is(err, prototypeexecution.ErrResultInvalid) { t.Fatalf("trailing json=%v", err) }
}
