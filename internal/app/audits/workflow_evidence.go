package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

type WorkflowImplementationEvidence struct {
	ActorKind string
	Applier   *WorkflowApplierImplementationEvidence
	Executor  *WorkflowExecutorImplementationEvidence
	Artifacts []workflowstore.Artifact
}

type WorkflowApplierImplementationEvidence struct {
	ImplementationResultArtifact workflowstore.Artifact
	LedgerArtifact               workflowstore.Artifact
	Result                       WorkflowApplierImplementationResult
}

type WorkflowApplierImplementationResult struct {
	Outcome               string   `json:"outcome"`
	ActorKind             string   `json:"actor_kind"`
	ChangedFiles          []string `json:"changed_files"`
	ResidualOperationIDs  []string `json:"residual_operation_ids"`
	ResidualOperations    []string `json:"residual_operations"`
	BlockedOperations     []string `json:"blocked_operations"`
	ModelExecutorRequired bool     `json:"model_executor_required"`
	FailureClass          string   `json:"failure_class"`
	FailureReason         string   `json:"failure_reason"`
}

type WorkflowExecutorImplementationEvidence struct {
	Attempt   workflowstore.ExecutionAttempt
	Artifacts []workflowstore.Artifact
}

type workflowImplementationEvidenceReader interface {
	ListArtifactsByRun(context.Context, int64) ([]workflowstore.Artifact, error)
	ListArtifactsByExecutionAttempt(context.Context, int64) ([]workflowstore.Artifact, error)
	GetLatestSucceededExecutionAttemptOptional(context.Context, int64) (workflowstore.ExecutionAttempt, bool, error)
}

func resolveWorkflowImplementationEvidence(ctx context.Context, store *workflowstore.Store, run workflowstore.Run) (WorkflowImplementationEvidence, error) {
	return resolveWorkflowImplementationEvidenceWithReader(ctx, store, store, run)
}

func resolveWorkflowImplementationEvidenceWithReader(ctx context.Context, artifactStore *workflowstore.Store, reader workflowImplementationEvidenceReader, run workflowstore.Run) (WorkflowImplementationEvidence, error) {
	runArtifacts, err := reader.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return WorkflowImplementationEvidence{}, err
	}
	applier, hasApplier, err := resolveApplierImplementationEvidence(artifactStore, runArtifacts)
	if err != nil {
		return WorkflowImplementationEvidence{}, err
	}
	applierArtifacts := workflowApplierImplementationArtifacts(runArtifacts)
	attempt, hasAttempt, err := reader.GetLatestSucceededExecutionAttemptOptional(ctx, run.ID)
	if err != nil {
		return WorkflowImplementationEvidence{}, err
	}
	var executor *WorkflowExecutorImplementationEvidence
	if hasAttempt {
		attemptArtifacts, err := reader.ListArtifactsByExecutionAttempt(ctx, attempt.ID)
		if err != nil {
			return WorkflowImplementationEvidence{}, err
		}
		executor = &WorkflowExecutorImplementationEvidence{Attempt: attempt, Artifacts: attemptArtifacts}
	}
	if hasApplier && applier.Result.Outcome == "blocked" {
		return WorkflowImplementationEvidence{}, fmt.Errorf("applier evidence is blocked and cannot be audited as implementation success")
	}
	if hasApplier && (len(applier.Result.ResidualOperationIDs) > 0 || len(applier.Result.ResidualOperations) > 0 || applier.Result.ModelExecutorRequired) && !hasAttempt {
		return WorkflowImplementationEvidence{}, fmt.Errorf("applier residual work has no succeeded Executor attempt")
	}
	switch {
	case hasApplier && hasAttempt:
		artifacts := append(applierArtifacts, executor.Artifacts...)
		return WorkflowImplementationEvidence{ActorKind: workflowstore.ImplementationActorHybrid, Applier: applier, Executor: executor, Artifacts: artifacts}, nil
	case hasApplier:
		return WorkflowImplementationEvidence{ActorKind: workflowstore.ImplementationActorApplier, Applier: applier, Artifacts: applierArtifacts}, nil
	case hasAttempt:
		return WorkflowImplementationEvidence{ActorKind: workflowstore.ImplementationActorExecutor, Executor: executor, Artifacts: executor.Artifacts}, nil
	default:
		return WorkflowImplementationEvidence{}, fmt.Errorf("no terminal implementation evidence is available for audit")
	}
}

func workflowApplierImplementationArtifacts(artifacts []workflowstore.Artifact) []workflowstore.Artifact {
	out := make([]workflowstore.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		switch artifact.Kind {
		case "applier_implementation_result_json",
			"applier_result_json",
			"applier_ledger_json",
			"applier_changed_files_json",
			"applier_failure_packet_json":
			out = append(out, artifact)
		}
	}
	return out
}

func resolveApplierImplementationEvidence(store *workflowstore.Store, artifacts []workflowstore.Artifact) (*WorkflowApplierImplementationEvidence, bool, error) {
	var resultArtifact workflowstore.Artifact
	var ledgerArtifact workflowstore.Artifact
	for _, artifact := range artifacts {
		switch artifact.Kind {
		case "applier_implementation_result_json", "applier_result_json":
			if resultArtifact.ID != 0 {
				return nil, false, fmt.Errorf("multiple applier implementation result artifacts")
			}
			resultArtifact = artifact
		case "applier_ledger_json":
			if ledgerArtifact.ID != 0 {
				return nil, false, fmt.Errorf("multiple applier ledger artifacts")
			}
			ledgerArtifact = artifact
		}
	}
	if resultArtifact.ID == 0 && ledgerArtifact.ID == 0 {
		return nil, false, nil
	}
	if resultArtifact.ID == 0 || ledgerArtifact.ID == 0 {
		return nil, false, fmt.Errorf("incomplete applier implementation evidence")
	}
	data, err := readWorkflowArtifact(store, resultArtifact, MaxWorkflowAuditEvidenceBytes)
	if err != nil {
		return nil, false, fmt.Errorf("read applier implementation result: %w", err)
	}
	var result WorkflowApplierImplementationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("decode applier implementation result: %w", err)
	}
	if strings.TrimSpace(result.ActorKind) != "" && result.ActorKind != workflowstore.ImplementationActorApplier && result.ActorKind != workflowstore.ImplementationActorHybrid {
		return nil, false, fmt.Errorf("unsupported applier actor kind %q", result.ActorKind)
	}
	return &WorkflowApplierImplementationEvidence{ImplementationResultArtifact: resultArtifact, LedgerArtifact: ledgerArtifact, Result: result}, true, nil
}

func implementationExecutionAttemptRowID(evidence WorkflowImplementationEvidence) sql.NullInt64 {
	if evidence.Executor == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: evidence.Executor.Attempt.ID, Valid: true}
}
