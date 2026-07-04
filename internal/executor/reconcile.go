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
		if err := reconcileActiveExecution(st, hub, log, controller, exec); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.New(joinErrors(errs))
	}
	return nil
}

func reconcileActiveExecution(st *store.Store, hub *events.Hub, log *slog.Logger, controller pipeline.ProcessController, exec store.AgentExecution) error {
	reason := TerminalReasonRestartOrphanReconcile
	message := ""
	verifiedAbsent := false
	identity, identityErr := processIdentityFromExecution(&exec)
	if identityErr == nil {
		owned, openErr := controller.OpenOwned(identity)
		if openErr != nil {
			message = "Executor process ownership could not be reopened during restart reconciliation: " + openErr.Error()
		} else {
			running, probeErr := owned.TreeRunning()
			if probeErr != nil {
				message = "Executor process identity could not be verified during restart reconciliation: " + probeErr.Error()
			} else if running {
				markTerminationRequested(st, exec.ID, TerminalReasonRestartOrphanReconcile)
				result, termErr := owned.Terminate(2 * time.Second)
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
			if releaseErr := owned.Release(); releaseErr != nil {
				message = "Executor process ownership release failed during restart reconciliation: " + releaseErr.Error()
				verifiedAbsent = false
			}
		}
	} else {
		message = "Executor process identity missing during restart reconciliation"
	}

	if !verifiedAbsent {
		if failed := markTerminationFailed(st, exec.ID, message); failed == nil {
			message += "; failed to persist termination blocker"
		}
		createEvent(st, exec.RunID, "warn", message)
		return fmt.Errorf("executor orphan termination unproven for execution %d: %s", exec.ID, message)
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
		return fmt.Errorf("terminalize stale execution %d: %w", exec.ID, err)
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
