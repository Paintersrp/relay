package executor

import "fmt"

// ExecutionMode is the mechanically derived runtime execution disposition for
// one package-linked Run. It is derived from the durable Deterministic Outcome
// and is explicitly non-authoritative: it never carries semantic
// implementation authority of its own, and the approved Delivery Ticket remains
// the sole semantic authority for an adaptive Executor.
type ExecutionMode string

const (
	// ExecutionModeAbsent means no Deterministic Operations artifact was
	// approved, so the complete Delivery Ticket is implemented adaptively.
	ExecutionModeAbsent ExecutionMode = "absent"
	// ExecutionModePreflightFailed means the deterministic preflight failed, so
	// the complete Delivery Ticket is implemented adaptively from the unchanged
	// worktree. The failure record is source-state evidence only.
	ExecutionModePreflightFailed ExecutionMode = "preflight_failed"
	// ExecutionModePartialApplied means deterministic application applied
	// partially, so the adaptive Executor preserves Relay-applied work and
	// completes the remaining Delivery Ticket obligations.
	ExecutionModePartialApplied ExecutionMode = "partial_applied"
	// ExecutionModeCompleteApplied means deterministic application covered the
	// approved operations completely, so no adaptive Executor is dispatched.
	ExecutionModeCompleteApplied ExecutionMode = "complete_applied"
)

// executionModeFromOutcome mechanically derives the runtime ExecutionMode from
// the durable Deterministic Outcome summary. The result is non-authoritative:
// it only mirrors what the verified outcome already states.
func executionModeFromOutcome(outcome DeterministicOutcomeSummary) (ExecutionMode, error) {
	switch outcome.Status {
	case string(DeterministicPreflightNotPresent):
		if outcome.Coverage != "" {
			return "", fmt.Errorf("not_present deterministic outcome has coverage %q", outcome.Coverage)
		}
		return ExecutionModeAbsent, nil
	case string(DeterministicPreflightFailed):
		return ExecutionModePreflightFailed, nil
	case "applied":
		switch outcome.Coverage {
		case "partial":
			return ExecutionModePartialApplied, nil
		case "complete":
			return ExecutionModeCompleteApplied, nil
		}
		return "", fmt.Errorf("applied deterministic outcome has unsupported coverage %q", outcome.Coverage)
	default:
		return "", fmt.Errorf("unsupported deterministic outcome status %q", outcome.Status)
	}
}

// adaptiveDispatchRequired reports whether the mode still requires an adaptive
// Executor dispatch. Deterministic completion is the only mode that suppresses
// adaptive dispatch, and it does so only when the existing coverage validation
// records the outcome as complete.
func adaptiveDispatchRequired(mode ExecutionMode) bool {
	return mode != ExecutionModeCompleteApplied
}

// adaptiveSourceMutationStarted maps a mode to whether a repository mutation
// lease already exists because deterministic application mutated source state.
func adaptiveSourceMutationStarted(mode ExecutionMode) (bool, bool) {
	switch mode {
	case ExecutionModeAbsent, ExecutionModePreflightFailed:
		return false, true
	case ExecutionModePartialApplied:
		return true, true
	default:
		return false, false
	}
}

// workflowrunsModeString maps an ExecutionMode to the canonical
// execution_assignment_mode wire value. The executor persists exactly this
// value in the attempt ResultJSON and the execution_evidence artifact, and the
// workflowruns BeginPreparedAdaptiveExecution admission contract requires the
// same value in the pre-launch runtime record. Deterministic completion is not
// an adaptive wire mode. The runs package owns that contract; the executor
// adapts to it at the boundary.
func workflowrunsModeString(mode ExecutionMode) (string, bool) {
	switch mode {
	case ExecutionModeAbsent:
		return "adaptive_no_operations", true
	case ExecutionModePreflightFailed:
		return "adaptive_preflight_failed", true
	case ExecutionModePartialApplied:
		return "adaptive_after_partial_application", true
	default:
		return "", false
	}
}
