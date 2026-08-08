package audits

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

// ReadRunAuditState is the audits-owner semantic read of one package Run's
// audit progression: none (pre-audit), awaiting_audit, packet_recorded, or
// decision_recorded. It composes the audit owner's own packet and decision
// rows; consumers must not reconstruct audit state from audit_packets or
// audit_decisions rows.
func (s *WorkflowAuditService) ReadRunAuditState(ctx context.Context, runID string) (guidedapp.RunAuditState, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return guidedapp.RunAuditState{}, ErrWorkflowAuditPacketNotFound
	}
	run, err := s.store.GetRunByRunID(ctx, runID)
	if err != nil {
		return guidedapp.RunAuditState{}, err
	}
	result := guidedapp.RunAuditState{RunID: run.RunID, RunStatus: run.Status, State: "none"}
	packet, packetErr := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
	if packetErr == nil {
		result.State = "packet_recorded"
		result.AuditPacketID = packet.AuditPacketID
		result.AuditedCommit = packet.AuditedCommit
	} else if !errors.Is(packetErr, sql.ErrNoRows) {
		return guidedapp.RunAuditState{}, packetErr
	}
	if decision, decisionErr := s.store.GetAuditDecisionByRun(ctx, run.ID); decisionErr == nil && decision.AuditDecisionID != "" {
		result.State = "decision_recorded"
	} else if !errors.Is(decisionErr, sql.ErrNoRows) {
		return guidedapp.RunAuditState{}, decisionErr
	}
	if result.State == "none" && run.Status != workflowstore.RunStatusCreated && run.Status != workflowstore.RunStatusSetupReady &&
		run.Status != workflowstore.RunStatusExecuting && run.Status != workflowstore.RunStatusExecutionFailed &&
		run.Status != workflowstore.RunStatusCancelled {
		result.State = "awaiting_audit"
	}
	if result.RunStatus == workflowstore.RunStatusNeedsRevision {
		result.Diagnostics = append(result.Diagnostics, "run_needs_revision")
	}
	return result, nil
}

// ReadWorkspaceRemediationState is the audits-owner semantic read of audit
// remediation seeds for one workspace: none | open | reopened. It composes the
// audit owner's own seed and reopening rows; consumers must not reconstruct
// remediation state from audit_remediation_seeds rows.
func (s *WorkflowAuditService) ReadWorkspaceRemediationState(ctx context.Context, workspaceID string) (guidedapp.RemediationState, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return guidedapp.RemediationState{}, err
	}
	seeds, err := s.store.ListAuditRemediationSeedsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return guidedapp.RemediationState{}, err
	}
	result := guidedapp.RemediationState{State: "none"}
	if len(seeds) == 0 {
		return result, nil
	}
	result.State = "reopened"
	for _, seed := range seeds {
		result.SeedIDs = append(result.SeedIDs, seed.RemediationSeedID)
		if _, reopenErr := s.store.GetAuditRemediationSeedReopening(ctx, seed.ID); errors.Is(reopenErr, sql.ErrNoRows) {
			result.State = "open"
		} else if reopenErr != nil {
			return guidedapp.RemediationState{}, reopenErr
		}
	}
	return result, nil
}
