package agentrefs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type requiredSourceInput struct {
	path string
	role string
}

var workflowInputs = []requiredSourceInput{
	{"relay-contracts/contracts/intent_drift_review_contract.md", "intent drift review contract"},
	{"relay-contracts/contracts/planner_mcp_plan_attempt_contract.md", "planner MCP plan attempt contract"},
	{"relay-contracts/contracts/refactor_backlog_contract.md", "refactor backlog contract"},
	{"relay-contracts/policies/pipeline_lifecycle_policy.md", "pipeline lifecycle policy"},
	{"internal/app/plans/attempt_types.go", "plan attempt type model"},
	{"internal/app/plans/attempt_service.go", "plan attempt service"},
	{"internal/api/plans/attempt_handler.go", "plan attempt HTTP handler"},
	{"internal/mcp/plan_attempt_tools.go", "plan attempt MCP tools"},
	{"internal/refactors/types.go", "refactor backlog type model"},
	{"internal/mcp/refactor_backlog_tools.go", "refactor backlog MCP tools"},
	{"internal/app/plans/work_packets.go", "next-pass work packet model"},
}

func BuildWorkflowSurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	var sourceInputs []SourceInput
	for _, in := range workflowInputs {
		abs := filepath.Join(repoRoot, in.path)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return nil, fmt.Errorf("required workflow source input missing: %s", in.path)
		}
		hash, err := ComputeSHA256(abs)
		if err != nil {
			return nil, fmt.Errorf("hash workflow source input %s: %w", in.path, err)
		}
		sourceInputs = append(sourceInputs, SourceInput{
			Path:   in.path,
			SHA256: hash,
			Role:   in.role,
		})
	}

	facts := []Fact{
		{
			ID:    "workflow-plan-attempt-status-model",
			Label: FactLabelProven,
			Statement: "PlanAttemptStatus constants (draft, approved, submitted, voided, superseded) and " +
				"PlanAttemptReviewState constants model the complete plan attempt lifecycle in internal/app/plans/attempt_types.go. " +
				"ModelTier constants (economy, standard, high_assurance, auto_escalate) define compute tier selection.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_types.go"},
			},
		},
		{
			ID:    "workflow-intent-packet-lineage",
			Label: FactLabelProven,
			Statement: "Intent packet lineage is tracked through root_intent_packet_id and reviewed_intent_packet_id in " +
				"PlanIntentReviewPacket. IntentPacketEvidence captures the full chain: kind (original/revision), " +
				"content_hash, redaction_status, and source_artifact_path. PriorAttemptInfo and PriorReviewInfo " +
				"in the review packet connect intent thread history.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_types.go"},
			},
		},
		{
			ID:    "workflow-review-packet-retrieval-only",
			Label: FactLabelProven,
			Statement: "PlanIntentReviewPacket.RetrievalSemantics controls whether the review packet performs " +
				"a model call and mutates state. RetrievalOnly=true means no model call and no state mutation; " +
				"this is the safe read-only path for external reviewers.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_types.go"},
			},
		},
		{
			ID:    "workflow-drift-review-submit-boundary",
			Label: FactLabelProven,
			Statement: "DriftReviewInput is the bounded input for submitting an intent drift review. " +
				"It carries overall_alignment, recommended_action, approval_gate_status, and findings JSON. " +
				"SubmitIntentDriftReviewRequest wraps it with project_id and plan_attempt_id. " +
				"DriftReviewMode constants (disabled, manual, automatic, external) control drift behavior.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_types.go"},
			},
		},
		{
			ID:    "workflow-approval-submit-gates",
			Label: FactLabelDerived,
			Statement: "Approval gate status constants (not_required, ready, acknowledgement_required, " +
				"revision_required, blocked) gate both the ApprovePlanAttemptRequest and SubmitPlanAttemptRequest " +
				"boundaries. SubmitPlanAttemptRequest requires submission_confirmed and " +
				"reviewed_plan_json_artifact_sha256. These gates are enforced across attempt_types.go, " +
				"attempt_service.go, and attempt_handler.go.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_types.go"},
				{Kind: "source", Value: "internal/app/plans/attempt_service.go"},
				{Kind: "source", Value: "internal/api/plans/attempt_handler.go"},
			},
		},
		{
			ID:    "workflow-refactor-backlog-candidate-model",
			Label: FactLabelProven,
			Statement: "Refactor candidate statuses (ready, scheduled, scheduled_revision_required, completed, " +
				"completed_with_warnings, deferred, rejected, superseded) model the full candidate lifecycle " +
				"as defined by internal/refactors/types.go (runtime) and relay-contracts/contracts/refactor_backlog_contract.md (contract). " +
				"DiscoveryTaskInput and CandidateInput define the bounded creation surface. RiskLevel constants " +
				"(low, medium, high) classify candidate severity. CandidateScheduleInput records a passive scheduling reference. " +
				"Completion statuses (completed, completed_with_warnings, scheduled_revision_required) are derived from " +
				"scheduled pass audit outcomes per the contract.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/refactors/types.go"},
				{Kind: "contract", Value: "relay-contracts/contracts/refactor_backlog_contract.md"},
			},
		},
		{
			ID:    "workflow-refactor-mcp-safety-boundaries",
			Label: FactLabelDerived,
			Statement: "Refactor backlog MCP tools in internal/mcp/refactor_backlog_tools.go expose " +
				"discovery task and candidate CRUD operations. The safety boundary is enforced through " +
				"lifecycle validation in internal/refactors/types.go: terminal statuses block transitions, " +
				"secret-like values are rejected, cross-project references are blocked, and " +
				"not_pass_ready validation prevents premature candidate elevation.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/mcp/refactor_backlog_tools.go"},
				{Kind: "source", Value: "internal/refactors/types.go"},
			},
		},
		{
			ID:    "workflow-next-pass-work-blockers",
			Label: FactLabelProven,
			Statement: "NextPassWorkResponse reports business-state blockers through WorkBlocker entries. " +
				"Blocker codes in internal/app/plans/work_packets.go include unknown_plan, " +
				"plan_not_active, dependencies_incomplete, prior_pass_awaits_audit, active_run_exists, " +
				"revision_required_same_pass, and no_eligible_pass. Blockers have a Recoverable flag.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/work_packets.go"},
			},
		},
		{
			ID:    "workflow-work-packet-read-only",
			Label: FactLabelDerived,
			Statement: "NextPassWorkResponse and its nested types (WorkPlanSummary, WorkPassSummary, " +
				"WorkContextSummary) are read-only projections. The get_next_pass_work tool " +
				"(NextPassWorkTool constant) does not mutate state. WorkBlocker entries guide the caller " +
				"toward recoverable actions without automatic remediation.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/work_packets.go"},
			},
		},
		{
			ID:    "workflow-route-touchpoints",
			Label: FactLabelConvention,
			Statement: "Plan attempt HTTP handlers in internal/api/plans/attempt_handler.go serve as " +
				"the primary REST touchpoint for plan attempt creation, review packet retrieval, " +
				"drift review submission, approval, submission, revision, and voiding. The handler names " +
				"(HandleCreate, HandleGetReviewPacket, HandleSubmitDriftReview, HandleApprove, " +
				"HandleSubmit, HandleRevise, HandleVoid) follow the Handler naming convention and " +
				"are routed through MountRoutes in internal/api/plans/routes.go.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/api/plans/attempt_handler.go"},
				{Kind: "source", Value: "internal/api/plans/routes.go"},
			},
		},
		{
			ID:    "workflow-gap-contract-runtime-comparison",
			Label: FactLabelUnresolved,
			Statement: "Contract/runtime semantic comparison is hash-grounded but not fully parsed by this " +
				"generator; semantic mismatch review remains unresolved. Source inputs include hashed " +
				"relay-contracts contract and policy files alongside runtime Go sources, but the generator " +
				"does not parse contract semantic claims against runtime behavior. A future deterministic " +
				"comparison pass (PASS-006+) may resolve this gap.",
			Evidence: []Evidence{
				{Kind: "contract", Value: "relay-contracts/contracts/intent_drift_review_contract.md"},
				{Kind: "contract", Value: "relay-contracts/contracts/planner_mcp_plan_attempt_contract.md"},
				{Kind: "contract", Value: "relay-contracts/contracts/refactor_backlog_contract.md"},
				{Kind: "policy", Value: "relay-contracts/policies/pipeline_lifecycle_policy.md"},
			},
		},
		{
			ID:    "workflow-gap-untested-state-values",
			Label: FactLabelUnresolved,
			Statement: "The generator does not yet inspect tests for every state value; untested state-value " +
				"coverage remains unresolved. PlanAttemptStatus, PlanAttemptReviewState, " +
				"ApprovalGateStatus, DriftReviewMode, ModelTier, RefactorCandidateStatus, and " +
				"WorkBlocker codes are declared in runtime source files but not cross-referenced against " +
				"test coverage by this generator. PASS-006 owns test-coverage completeness.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_types.go"},
				{Kind: "source", Value: "internal/refactors/types.go"},
				{Kind: "source", Value: "internal/app/plans/work_packets.go"},
			},
		},
		{
			ID:    "workflow-gap-lifecycle-observed-writes",
			Label: FactLabelUnresolved,
			Statement: "The generator does not yet enumerate all direct lifecycle/status writes; " +
				"observed-write coverage remains unresolved. Plan attempt status transitions, refactor " +
				"candidate lifecycle operations, and work-packet state changes are implemented across " +
				"service and handler files but are not enumerated by this generator. " +
				"PASS-007 owns lifecycle-write audit coverage.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/plans/attempt_service.go"},
				{Kind: "source", Value: "internal/api/plans/attempt_handler.go"},
				{Kind: "source", Value: "internal/mcp/refactor_backlog_tools.go"},
			},
		},
		{
			ID:    "workflow-gap-transport-coverage",
			Label: FactLabelUnresolved,
			Statement: "The generator identifies plan attempt handler touchpoints but does not provide " +
				"complete HTTP route/API coverage; PASS-006 owns route/API completeness. MCP tool " +
				"registration and server wiring for plan attempt and refactor backlog tools are noted " +
				"but not exhaustively enumerated by this generator.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/api/plans/attempt_handler.go"},
				{Kind: "source", Value: "internal/api/plans/routes.go"},
				{Kind: "source", Value: "internal/mcp/plan_attempt_tools.go"},
				{Kind: "source", Value: "internal/mcp/refactor_backlog_tools.go"},
			},
		},
	}

	sort.Slice(facts, func(i, j int) bool {
		return facts[i].ID < facts[j].ID
	})

	labels := []FactLabel{
		FactLabelProven,
		FactLabelDerived,
		FactLabelConvention,
		FactLabelUnresolved,
		FactLabelConflict,
	}

	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "workflow-surfaces",
		Repo: RepoIdentity{
			ProjectID: "relay",
			RepoID:    "Paintersrp/relay",
			Branch:    "main",
		},
		GeneratedBy: GeneratorIdentity{
			Name:    "relay-agentrefs",
			Version: "0.1.0",
		},
		Rendering: RenderingContract{
			JSONPrimary:       true,
			MarkdownFromJSON:  true,
			DeterministicSort: true,
			NoTimestamps:      true,
			RelativePathsOnly: true,
		},
		SourceInputs: sourceInputs,
		FactLabels:   labels,
		Facts:        facts,
		References: []ReferenceEntry{
			{
				ID:          "intent-drift-review-contract",
				Kind:        "contract",
				Path:        "contracts/intent_drift_review_contract.md",
				Description: "Planner intent drift review contract from relay-contracts.",
			},
			{
				ID:          "pipeline-lifecycle-policy",
				Kind:        "policy",
				Path:        "policies/pipeline_lifecycle_policy.md",
				Description: "Pipeline lifecycle policy from relay-contracts.",
			},
			{
				ID:          "planner-mcp-plan-attempt-contract",
				Kind:        "contract",
				Path:        "contracts/planner_mcp_plan_attempt_contract.md",
				Description: "Planner MCP plan attempt contract from relay-contracts.",
			},
			{
				ID:          "refactor-backlog-contract",
				Kind:        "contract",
				Path:        "contracts/refactor_backlog_contract.md",
				Description: "Refactor backlog contract from relay-contracts.",
			},
		},
	}

	return doc, nil
}
