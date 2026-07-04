package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"relay/internal/app/plans"
	"relay/internal/events"
	"relay/internal/pipeline"
	"relay/internal/store"
)

const (
	ExecutionStatusStarting           = "starting"
	ExecutionStatusRunning            = "running"
	ExecutionStatusCancelRequested    = "cancel_requested"
	ExecutionStatusSucceeded          = "succeeded"
	ExecutionStatusCompletedLegacy    = "completed"
	ExecutionStatusFailed             = "failed"
	ExecutionStatusTimedOut           = "timed_out"
	ExecutionStatusCanceled           = "canceled"
	ExecutionStatusProcessLost        = "process_lost"
	ExecutionStatusTerminationPending = "termination_pending"

	TerminalReasonSucceeded              = "executor_completed"
	TerminalReasonFailed                 = "executor_failed"
	TerminalReasonTimedOut               = "executor_timed_out"
	TerminalReasonCanceled               = "operator_cancel_requested"
	TerminalReasonProcessLost            = "process_lost"
	TerminalReasonRestartOrphanReconcile = "relay_restart_orphan_reconciled"
)

type runtimeHandle struct {
	execID         int64
	runID          int64
	ownershipToken string
	cancel         context.CancelFunc
	controller     pipeline.ProcessController
	launchDone     <-chan struct{}
}

type runtimeRegistry struct {
	mu      sync.Mutex
	handles map[int64]runtimeHandle
}

func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{handles: make(map[int64]runtimeHandle)}
}

func (r *runtimeRegistry) put(h runtimeHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handles[h.execID] = h
}

func (r *runtimeRegistry) get(execID int64, token string) (runtimeHandle, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.handles[execID]
	if !ok || h.ownershipToken != token {
		return runtimeHandle{}, false
	}
	return h, true
}

func (r *runtimeRegistry) delete(execID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handles, execID)
}

var globalRuntimeRegistry = newRuntimeRegistry()

func NewOwnerInstanceID() string {
	return "relay-" + uuid.NewString()
}

func newOwnershipToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(b[:])
}

func terminalStatus(status string) bool {
	switch status {
	case ExecutionStatusSucceeded, ExecutionStatusCompletedLegacy, ExecutionStatusFailed, ExecutionStatusTimedOut, ExecutionStatusCanceled, ExecutionStatusProcessLost:
		return true
	default:
		return false
	}
}

type terminalExecutionInput struct {
	Status                  string
	Reason                  string
	ExitCode                *int64
	StartedAt               string
	FinishedAt              string
	StdoutPath              string
	StderrPath              string
	CombinedPath            string
	ResultPath              string
	Error                   string
	CancellationCompletedAt string
	TerminalizedAt          string
	EventLevel              string
	EventMessage            string
	StepEventStatus         string
	RunStatus               string
	RunEventStatus          string
}

func terminalizeExecution(st *store.Store, hub *events.Hub, log *slog.Logger, runID, execID int64, in terminalExecutionInput) (*store.AgentExecution, bool, error) {
	if in.FinishedAt == "" {
		in.FinishedAt = executionTimestampNow()
	}
	if in.TerminalizedAt == "" {
		in.TerminalizedAt = in.FinishedAt
	}
	exec, won, err := st.FinalizeAgentExecutionCAS(runID, execID, store.AgentExecutionTerminalUpdate{
		Status:                  in.Status,
		ExitCode:                in.ExitCode,
		StartedAt:               in.StartedAt,
		FinishedAt:              in.FinishedAt,
		StdoutPath:              in.StdoutPath,
		StderrPath:              in.StderrPath,
		CombinedPath:            in.CombinedPath,
		ResultPath:              in.ResultPath,
		Error:                   in.Error,
		CancellationCompletedAt: in.CancellationCompletedAt,
		TerminalReason:          in.Reason,
		TerminalizedAt:          in.TerminalizedAt,
	}, in.RunStatus, in.EventLevel, in.EventMessage)
	if err != nil {
		return nil, false, err
	}
	if !won {
		return exec, false, nil
	}
	if in.StepEventStatus != "" {
		publishRunEvent(hub, runID, events.KindStepAgent, "executor", in.StepEventStatus)
	}
	if in.RunEventStatus != "" {
		publishRunEvent(hub, runID, events.KindRunSummary, "executor", in.RunEventStatus)
	}
	if in.RunStatus != "" {
		if updatedRun, err := st.GetRun(runID); err == nil && updatedRun != nil {
			if err := plans.NewRunLifecycleService(st).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
				return exec, true, err
			}
		}
	}
	if log != nil {
		log.Info("executor: terminalized execution", "run_id", runID, "exec_id", execID, "status", in.Status, "reason", in.Reason)
	}
	return exec, true, nil
}

func markTerminationRequested(st *store.Store, execID int64, reason string) {
	if st == nil {
		return
	}
	_, _ = st.MarkAgentExecutionTerminationRequested(execID, reason, executionTimestampNow())
}

func markTerminationVerified(st *store.Store, execID int64) {
	if st == nil {
		return
	}
	_, _ = st.MarkAgentExecutionTreeVerifiedAbsent(execID, executionTimestampNow())
}

func markTerminationFailed(st *store.Store, execID int64, errText string) *store.AgentExecution {
	if st == nil {
		return nil
	}
	exec, err := st.MarkAgentExecutionTerminationFailed(execID, errText)
	if err != nil {
		return nil
	}
	return exec
}

type CancellationResult struct {
	RunID                   int64
	RunStatus               string
	ExecutionID             int64
	ExecutionStatus         string
	CancellationRequestedAt string
	TerminalReason          string
	LifecycleState          string
	Initiated               bool
	Terminal                bool
}

func CancelExecution(ctx context.Context, st *store.Store, hub *events.Hub, log *slog.Logger, runID int64, controller pipeline.ProcessController) (CancellationResult, error) {
	_ = ctx
	if controller == nil {
		controller = pipeline.DefaultProcessController()
	}
	run, err := st.GetRun(runID)
	if err != nil {
		return CancellationResult{}, err
	}
	exec, err := st.GetActiveAgentExecutionByRun(runID)
	if err != nil {
		return CancellationResult{}, err
	}
	if exec == nil {
		latest, _ := st.GetLatestAgentExecutionByRun(runID)
		if latest != nil && terminalStatus(latest.Status) {
			return cancellationResultFrom(run.Status, latest, false), nil
		}
		return CancellationResult{}, fmt.Errorf("no active executor attempt for run %d", runID)
	}

	now := executionTimestampNow()
	updated, initiated, err := st.RequestAgentExecutionCancellation(exec.ID, now)
	if err != nil {
		return CancellationResult{}, err
	}
	if updated == nil {
		return CancellationResult{}, fmt.Errorf("execution %d not found", exec.ID)
	}

	token := ""
	if updated.OwnershipToken.Valid {
		token = updated.OwnershipToken.String
	}
	if h, ok := globalRuntimeRegistry.get(updated.ID, token); ok {
		if h.controller != nil {
			controller = h.controller
		}
		h.cancel()
		if h.launchDone != nil {
			select {
			case <-h.launchDone:
			case <-time.After(5 * time.Second):
				createEvent(st, runID, "warn", "Executor cancellation is waiting for launch ownership to settle")
			}
		}
	} else {
		identity, identityErr := processIdentityFromExecution(updated)
		if identityErr == nil {
			markTerminationRequested(st, updated.ID, TerminalReasonCanceled)
			owned, openErr := controller.OpenOwned(identity)
			if openErr != nil {
				err := openErr
				createEvent(st, runID, "warn", "Executor cancellation ownership reopen warning: "+err.Error())
				if failed := markTerminationFailed(st, updated.ID, "executor cancellation ownership reopen failed: "+err.Error()); failed != nil {
					updated = failed
				}
			} else {
				defer func() {
					if releaseErr := owned.Release(); releaseErr != nil {
						createEvent(st, runID, "warn", "Executor cancellation ownership release warning: "+releaseErr.Error())
					}
				}()
				result, err := owned.Terminate(2 * time.Second)
				if err != nil && !errors.Is(err, pipeline.ErrProcessNotRunning) {
					createEvent(st, runID, "warn", "Executor cancellation termination warning: "+err.Error())
					if failed := markTerminationFailed(st, updated.ID, "executor cancellation termination failed: "+err.Error()); failed != nil {
						updated = failed
					}
				} else if !result.VerifiedAbsent {
					createEvent(st, runID, "warn", "Executor cancellation termination warning: process absence was not verified")
					if failed := markTerminationFailed(st, updated.ID, "executor cancellation termination failed: process absence was not verified"); failed != nil {
						updated = failed
					}
				} else {
					markTerminationVerified(st, updated.ID)
				}
			}
		} else if updated.Status == ExecutionStatusCancelRequested && updated.ProcessIdentity.Valid {
			createEvent(st, runID, "warn", "Executor cancellation could not verify process identity: "+identityErr.Error())
			if failed := markTerminationFailed(st, updated.ID, "executor cancellation identity unverifiable: "+identityErr.Error()); failed != nil {
				updated = failed
			}
		} else if updated.Status == ExecutionStatusCancelRequested {
			createEvent(st, runID, "warn", "Executor cancellation cannot prove the process never started because no process identity is registered")
			if failed := markTerminationFailed(st, updated.ID, "executor cancellation unresolved: process identity is missing and start was not prevented"); failed != nil {
				updated = failed
			}
		}
	}

	latest, _ := st.GetAgentExecution(updated.ID)
	if latest != nil && latest.Status == ExecutionStatusCancelRequested && latest.LaunchState == "start_prevented" {
		finished := executionTimestampNow()
		latest, _, _ = terminalizeExecution(st, hub, log, runID, latest.ID, terminalExecutionInput{
			Status:                  ExecutionStatusCanceled,
			Reason:                  TerminalReasonCanceled,
			FinishedAt:              finished,
			CancellationCompletedAt: finished,
			EventLevel:              "warn",
			EventMessage:            "Executor canceled before process start",
			StepEventStatus:         "canceled",
			RunStatus:               StatusExecutorBlocked,
			RunEventStatus:          "blocked",
		})
	}
	if latest == nil {
		latest = updated
	}
	if refreshedRun, err := st.GetRun(runID); err == nil && refreshedRun != nil {
		run = refreshedRun
	}
	return cancellationResultFrom(run.Status, latest, initiated), nil
}

func cancellationResultFrom(runStatus string, exec *store.AgentExecution, initiated bool) CancellationResult {
	res := CancellationResult{
		RunID:           exec.RunID,
		RunStatus:       runStatus,
		ExecutionID:     exec.ID,
		ExecutionStatus: exec.Status,
		Initiated:       initiated,
		Terminal:        terminalStatus(exec.Status),
	}
	if exec.CancellationRequestedAt.Valid {
		res.CancellationRequestedAt = exec.CancellationRequestedAt.String
	}
	if exec.TerminalReason.Valid {
		res.TerminalReason = exec.TerminalReason.String
	}
	return res
}

func processIdentityFromExecution(exec *store.AgentExecution) (pipeline.ProcessIdentity, error) {
	if exec == nil || !exec.ProcessIdentity.Valid {
		return pipeline.ProcessIdentity{}, pipeline.ErrProcessUnverifiable
	}
	return pipeline.DecodeProcessIdentity(exec.ProcessIdentity.String)
}
