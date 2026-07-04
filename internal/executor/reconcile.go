package executor

import (
	"fmt"
	"log/slog"
	"time"

	"relay/internal/events"
	"relay/internal/pipeline"
	"relay/internal/store"
)

func ReconcileActiveExecutions(st *store.Store, hub *events.Hub, log *slog.Logger, ownerInstanceID string, controller pipeline.ProcessController) error {
	if controller == nil {
		controller = pipeline.DefaultProcessController()
	}
	active, err := st.ListActiveAgentExecutions()
	if err != nil {
		return err
	}
	for _, exec := range active {
		if exec.OwnerInstanceID.Valid && exec.OwnerInstanceID.String == ownerInstanceID {
			continue
		}
		reason := TerminalReasonProcessLost
		message := "Executor process lost during Relay restart reconciliation"
		identity, identityErr := processIdentityFromExecution(&exec)
		if identityErr == nil {
			running, probeErr := controller.IsRunning(identity)
			if probeErr != nil {
				message = "Executor process identity could not be verified during restart reconciliation: " + probeErr.Error()
			} else if running {
				result, termErr := controller.TerminateTree(identity, 2*time.Second)
				if termErr != nil {
					message = "Executor orphan termination failed during restart reconciliation: " + termErr.Error()
				} else if !result.VerifiedAbsent {
					message = "Executor orphan termination could not verify process absence during restart reconciliation"
				} else {
					reason = TerminalReasonRestartOrphanReconcile
					message = "Executor orphan process terminated during restart reconciliation"
				}
			} else {
				reason = TerminalReasonRestartOrphanReconcile
				message = "Executor process already absent during restart reconciliation"
			}
		} else {
			message = "Executor process identity missing during restart reconciliation"
		}
		finished := executionTimestampNow()
		if _, _, err := terminalizeExecution(st, hub, log, exec.RunID, exec.ID, terminalExecutionInput{
			Status:          ExecutionStatusProcessLost,
			Reason:          reason,
			FinishedAt:      finished,
			TerminalizedAt:  finished,
			EventLevel:      "warn",
			EventMessage:    message,
			StepEventStatus: "process_lost",
			RunStatus:       StatusExecutorBlocked,
			RunEventStatus:  "blocked",
		}); err != nil {
			return fmt.Errorf("terminalize stale execution %d: %w", exec.ID, err)
		}
		if reason == TerminalReasonProcessLost {
			return fmt.Errorf("executor orphan termination unproven for execution %d: %s", exec.ID, message)
		}
	}
	return nil
}
