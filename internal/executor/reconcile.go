package executor

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	var errs []error
	for _, exec := range active {
		if exec.OwnerInstanceID.Valid && exec.OwnerInstanceID.String == ownerInstanceID {
			continue
		}
		reason := TerminalReasonRestartOrphanReconcile
		message := ""
		verifiedAbsent := false
		identity, identityErr := processIdentityFromExecution(&exec)
		if identityErr == nil {
			running, probeErr := controller.IsRunning(identity)
			if probeErr != nil {
				message = "Executor process identity could not be verified during restart reconciliation: " + probeErr.Error()
			} else if running {
				markTerminationRequested(st, exec.ID, TerminalReasonRestartOrphanReconcile)
				result, termErr := controller.TerminateTree(identity, 2*time.Second)
				if termErr != nil {
					message = "Executor orphan termination failed during restart reconciliation: " + termErr.Error()
				} else if !result.VerifiedAbsent {
					message = "Executor orphan termination could not verify process absence during restart reconciliation"
				} else {
					message = "Executor orphan process terminated during restart reconciliation"
					verifiedAbsent = true
				}
			} else {
				message = "Executor process already absent during restart reconciliation"
				verifiedAbsent = true
			}
		} else {
			message = "Executor process identity missing during restart reconciliation"
		}

		if !verifiedAbsent {
			if failed := markTerminationFailed(st, exec.ID, message); failed == nil {
				message += "; failed to persist termination blocker"
			}
			createEvent(st, exec.RunID, "warn", message)
			errs = append(errs, fmt.Errorf("executor orphan termination unproven for execution %d: %s", exec.ID, message))
			continue
		}

		markTerminationVerified(st, exec.ID)
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
			errs = append(errs, fmt.Errorf("terminalize stale execution %d: %w", exec.ID, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(joinErrors(errs))
	}
	return nil
}

func joinErrors(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, "; ")
}
