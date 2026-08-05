package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"relay/internal/pipeline"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
	"time"
)

const (
	prototypeResultRelativePath           = ".relay/prototype/result.json"
	prototypeExportRelativePath           = ".relay/prototype/export"
	prototypeResultMediaType              = "application/vnd.relay.prototype-result+json"
	prototypeMaxResultBytes         int64 = 1 << 20
	prototypeMaxEvidenceMemberBytes int64 = 8 << 20
	prototypeMaxEvidenceTotalBytes  int64 = 32 << 20
	prototypeCancelGrace                  = 2 * time.Second
)

type PrototypeExecution struct {
	store                 *workflowstore.Store
	ownerInstanceID, root string
	controller            pipeline.ProcessController
	runner                WorkflowCommandRunner
	clock                 func() time.Time
}

func NewPrototypeExecution(store *workflowstore.Store, ownerInstanceID, root string) (*PrototypeExecution, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if root == "" {
		return nil, fmt.Errorf("prototype root is required")
	}
	a, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if st, e := os.Stat(a); e == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return nil, fmt.Errorf("prototype root is not a directory")
		}
	} else if os.IsNotExist(e) {
		if e = os.MkdirAll(a, 0700); e != nil {
			return nil, e
		}
	} else {
		return nil, e
	}
	return &PrototypeExecution{store: store, ownerInstanceID: ownerInstanceID, root: a, controller: pipeline.DefaultProcessController(), clock: time.Now}, nil
}
func (p *PrototypeExecution) load(ctx context.Context, inRun string) (prototypeexecution.Result, error) {
	r, e := p.store.GetPrototypeRun(ctx, inRun)
	if e != nil {
		return prototypeexecution.Result{}, e
	}
	out := prototypeexecution.Result{Run: r}
	if v, x := p.store.GetPrototypeRuntimeByRunID(ctx, inRun); x == nil {
		out.Runtime = &v
	}
	if v, x := p.store.GetPrototypeTargetByRunID(ctx, inRun); x == nil {
		out.Target = &v
	}
	if v, x := p.store.GetPrototypeLeaseByRunID(ctx, inRun); x == nil {
		out.Lease = &v
	}
	out.EvidenceBatches, _ = p.store.ListPrototypeEvidenceBatches(ctx, inRun)
	if v, x := p.store.GetPrototypeResultByRunID(ctx, inRun); x == nil {
		out.FinalResult = &v
	}
	out.Evidence, _ = p.store.ListPrototypeEvidenceMembers(ctx, inRun)
	return out, nil
}
func (p *PrototypeExecution) Launch(ctx context.Context, in prototypeexecution.LaunchRequest) (prototypeexecution.Result, error) {
	return p.load(ctx, in.RunID)
}
func (p *PrototypeExecution) Reconcile(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return p.load(ctx, in.RunID)
}
func (p *PrototypeExecution) Cancel(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return p.load(ctx, in.RunID)
}
func (p *PrototypeExecution) SettleTimeout(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return p.load(ctx, in.RunID)
}

var _ prototypeexecution.Executor = (*PrototypeExecution)(nil)
var _ = errors.Is

type prototypeResultEnvelope struct { SchemaVersion int `json:"schema_version"`; RunID string `json:"run_id"`; ProposalID string `json:"proposal_id"`; AuthorizationID string `json:"authorization_id"`; InvocationSHA256 string `json:"invocation_sha256"`; SourceCommit string `json:"source_commit"`; BaseCommit string `json:"base_commit"`; Adapter string `json:"adapter"`; Model string `json:"model"`; Outcome string `json:"outcome"`; VariantResults []prototypeVariantResult `json:"variant_results"`; Validations []prototypeValidationResult `json:"validations"`; Evidence []prototypeEvidenceCandidate `json:"evidence"`; TemporaryCommit string `json:"temporary_commit,omitempty"` }
type prototypeVariantResult struct { Variant string `json:"variant"`; Status string `json:"status"`; Summary string `json:"summary"` }
type prototypeValidationResult struct { Command string `json:"command"`; ExitCode int `json:"exit_code"`; Summary string `json:"summary"` }
type prototypeEvidenceCandidate struct { SemanticRole string `json:"semantic_role"`; RelativePath string `json:"relative_path"`; SHA256 string `json:"sha256"`; MediaType string `json:"media_type"`; Required bool `json:"required"` }
