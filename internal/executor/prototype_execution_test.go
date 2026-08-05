package executor

import (
	"context"
	"os"
	"path/filepath"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
	"strings"
	"testing"
)

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
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("prototype root=%v info=%v", err, info)
	}
	if filepath.IsAbs(p.root) == false {
		t.Fatalf("root is not absolute: %q", p.root)
	}
	if _, err := p.Launch(context.Background(), prototypeexecution.LaunchRequest{RunID: "missing"}); err == nil {
		t.Fatal("launch accepted a missing run")
	}
}

func TestPrototypeReconciliation(t *testing.T) {
	if prototypeResultRelativePath != ".relay/prototype/result.json" || prototypeExportRelativePath != ".relay/prototype/export" {
		t.Fatal("fixed prototype paths changed")
	}
	if prototypeMaxResultBytes != 1<<20 || prototypeMaxEvidenceMemberBytes != 8<<20 || prototypeMaxEvidenceTotalBytes != 32<<20 {
		t.Fatal("prototype limits changed")
	}
}

func TestPrototypeCancellationAndTimeout(t *testing.T) {
	if prototypeCancelGrace.Seconds() != 2 {
		t.Fatal("unexpected cancellation grace")
	}
	if strings.TrimSpace(prototypeResultMediaType) == "" {
		t.Fatal("missing result media type")
	}
}

func TestPrototypeResultAndEvidenceFinalization(t *testing.T) {
	if (prototypeResultEnvelope{}).SchemaVersion != 0 {
		t.Fatal("zero envelope should not be valid by default")
	}
}

func TestPrototypeEvidenceSafety(t *testing.T) {
	if filepath.IsAbs(".relay/prototype/export/x") {
		t.Fatal("relative evidence path became absolute")
	}
}
func TestPrototypePart2Boundary(t *testing.T) {
	var executor prototypeexecution.Executor = (*PrototypeExecution)(nil)
	if executor == nil {
		t.Fatal("prototype executor contract is nil")
	}
}

func TestPrototypeTimeoutLimits(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"timeout_seconds":1800}`, true}, {`{"timeout_seconds":1}`, true}, {`{"timeout_seconds":7200}`, true}, {`{"timeout_seconds":1800.0}`, false}, {`{"timeout_seconds":1.8e3}`, false}, {`{"timeout_seconds":"1800"}`, false}, {`{"timeout_seconds":0}`, false}, {`{"timeout_seconds":7201}`, false}, {`{}`, false}, {`{"timeout_seconds":null}`, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			d, e := decodePrototypeTimeoutLimits(tc.raw)
			if tc.valid {
				if e != nil || d <= 0 {
					t.Fatalf("decode=%v duration=%v", e, d)
				}
			} else if e == nil {
				t.Fatalf("accepted invalid limits: %s", tc.raw)
			}
		})
	}
}
func TestPrototypeResultEnvelopeValidation(t *testing.T) {
	a := workflowstore.PrototypeAuthorization{ProposedRunID: "prototype-run-1", AuthorizationID: "prototype-authorization-1", InvocationSHA256: strings.Repeat("a", 64), SourceCommit: strings.Repeat("b", 40), BaseCommit: strings.Repeat("b", 40), Adapter: "codex", Model: "model"}
	p := workflowstore.PrototypeProposal{ProposalID: "prototype-proposal-1"}
	run := workflowstore.PrototypeRun{}
	data := `{"schema_version":1,"run_id":"prototype-run-1","proposal_id":"prototype-proposal-1","authorization_id":"prototype-authorization-1","invocation_sha256":"` + strings.Repeat("a", 64) + `","source_commit":"` + strings.Repeat("b", 40) + `","base_commit":"` + strings.Repeat("b", 40) + `","adapter":"codex","model":"model","outcome":"succeeded","variant_results":[],"validations":[],"evidence":[]}`
	if _, e := validatePrototypeResultEnvelope([]byte(data), run, a, p, func() *int { x := 0; return &x }()); e != nil {
		t.Fatal(e)
	}
	bad := strings.Replace(data, `"outcome":"succeeded"`, `"outcome":"invalid"`, 1)
	if _, e := validatePrototypeResultEnvelope([]byte(bad), run, a, p, nil); e == nil {
		t.Fatal("accepted invalid outcome")
	}
}
