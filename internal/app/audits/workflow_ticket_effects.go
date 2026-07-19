package audits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

const (
	maxWorkflowAuditMaterialFindings = 32
	maxWorkflowAuditObservations     = 32
)

func bindWorkflowAuditPacketTicketObligations(
	ctx context.Context,
	tx *workflowstore.Tx,
	run workflowstore.Run,
	packet workflowstore.AuditPacket,
) error {
	if !run.ExecutionPackageRowID.Valid {
		return nil
	}
	if !run.PackageApprovalRowID.Valid {
		return ErrWorkflowAuditPacketStale
	}
	approval, err := tx.GetRunExecutionPackageApproval(ctx, run.ID)
	if err != nil {
		return ErrWorkflowAuditPacketStale
	}
	pkg, err := tx.GetExecutionPackageByRowID(ctx, run.ExecutionPackageRowID.Int64)
	if err != nil || pkg.ID != run.ExecutionPackageRowID.Int64 || pkg.RepoTarget != run.RepoTarget ||
		pkg.Branch != run.Branch || pkg.BaseCommit != run.BaseCommit || packet.BaseCommit != pkg.BaseCommit {
		return ErrWorkflowAuditPacketStale
	}
	if approval.PackageRowID != pkg.ID || approval.PackageSha256 != pkg.PackageSha256 {
		return ErrWorkflowAuditPacketStale
	}
	selection, err := tx.GetDeliveryTicketSelectionByRowID(ctx, pkg.SelectionRowID)
	if err != nil || selection.State != "consumed" || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return ErrWorkflowAuditPacketStale
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil || !workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentAuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
		return ErrWorkflowAuditPacketStale
	}
	authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, pkg.AuthorityRevisionRowID)
	if err != nil || authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return ErrWorkflowAuditPacketStale
	}
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, pkg.SourceClosureRowID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != pkg.BaseCommit {
		return ErrWorkflowAuditPacketStale
	}
	members, err := tx.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || len(members) == 0 {
		return ErrWorkflowAuditPacketStale
	}
	for _, member := range members {
		revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if err != nil || revision.CancellationReason.Valid || revision.SourceClosureRowID != pkg.SourceClosureRowID ||
			revision.RepoTarget != pkg.RepoTarget || revision.Branch != pkg.Branch || revision.BaseCommit != pkg.BaseCommit {
			return ErrWorkflowAuditPacketStale
		}
		ticket, err := tx.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
			return ErrWorkflowAuditPacketStale
		}
		if _, err := tx.CreateAuditPacketTicketObligation(ctx, workflowstore.CreateAuditPacketTicketObligationParams{
			AuditPacketRowID:            packet.ID,
			ExecutionPackageRowID:       pkg.ID,
			ExecutionPackageMemberRowID: member.ID,
			DeliveryTicketRowID:         ticket.ID,
			DeliveryTicketRevisionRowID: revision.ID,
			AuthorityRevisionRowID:      authority.ID,
			SourceClosureRowID:          closure.ID,
			PackageApprovalRowID:        sql.NullInt64{Int64: approval.ID, Valid: true},
			ApprovedPackageSha256:       sql.NullString{String: approval.PackageSha256, Valid: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowAuditDecisionInput(input RecordWorkflowAuditDecisionInput, ticketRoute bool) error {
	if strings.TrimSpace(input.Rationale) == "" || len(input.MaterialFindings) > maxWorkflowAuditMaterialFindings || len(input.Observations) > maxWorkflowAuditObservations {
		return ErrWorkflowAuditDecisionInput
	}
	if input.Decision == workflowstore.AuditDecisionAccepted && len(input.MaterialFindings) != 0 {
		return ErrWorkflowAuditDecisionInput
	}
	if input.Decision == workflowstore.AuditDecisionNeedsRevision && ticketRoute && len(input.MaterialFindings) == 0 {
		return ErrWorkflowAuditDecisionInput
	}
	for _, finding := range input.MaterialFindings {
		if !oneOfWorkflowAuditFindingSource(finding.Source) || strings.TrimSpace(finding.Summary) == "" ||
			strings.TrimSpace(finding.Evidence) == "" || strings.TrimSpace(finding.RequiredRemediation) == "" {
			return ErrWorkflowAuditDecisionInput
		}
	}
	for _, observation := range input.Observations {
		if strings.TrimSpace(observation) == "" {
			return ErrWorkflowAuditDecisionInput
		}
	}
	return nil
}

func oneOfWorkflowAuditFindingSource(source string) bool {
	return source == "executor_implementation" || source == "execution_spec" || source == "both"
}

func applyWorkflowAuditTicketDecisionEffects(
	ctx context.Context,
	tx *workflowstore.Tx,
	run workflowstore.Run,
	packet workflowstore.AuditPacket,
	decision workflowstore.AuditDecision,
	document WorkflowAuditPacket,
	input RecordWorkflowAuditDecisionInput,
) ([]workflowstore.AuditTicketRevisionDecision, []workflowstore.DeliveryTicketRevisionSatisfaction, []workflowstore.AuditRemediationSeed, error) {
	if !run.ExecutionPackageRowID.Valid {
		return nil, nil, nil, nil
	}
	obligations, err := tx.ListAuditPacketTicketObligations(ctx, packet.ID)
	if err != nil || len(obligations) == 0 {
		return nil, nil, nil, ErrWorkflowAuditPacketStale
	}
	for _, obligation := range obligations {
		if err := verifyWorkflowAuditTicketDecisionEligibility(ctx, tx, run, packet, obligation); err != nil {
			return nil, nil, nil, err
		}
	}
	decisions := make([]workflowstore.AuditTicketRevisionDecision, 0, len(obligations))
	for _, obligation := range obligations {
		revisionDecision, err := tx.CreateAuditTicketRevisionDecision(ctx, workflowstore.CreateAuditTicketRevisionDecisionParams{
			AuditDecisionRowID: decision.ID, AuditPacketTicketObligationRowID: obligation.ID,
			PackageApprovalRowID:  obligation.PackageApprovalRowID,
			ApprovedPackageSha256: obligation.ApprovedPackageSha256,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		decisions = append(decisions, revisionDecision)
	}
	if input.Decision == workflowstore.AuditDecisionAccepted {
		satisfactions := make([]workflowstore.DeliveryTicketRevisionSatisfaction, 0, len(obligations))
		for index, obligation := range obligations {
			if err := verifyWorkflowAuditTicketAcceptance(ctx, tx, run, packet, obligation, document); err != nil {
				return nil, nil, nil, err
			}
			satisfaction, err := tx.CreateDeliveryTicketRevisionSatisfaction(ctx, workflowstore.CreateDeliveryTicketRevisionSatisfactionParams{
				DeliveryTicketRevisionRowID:      obligation.DeliveryTicketRevisionRowID,
				AuditTicketRevisionDecisionRowID: decisions[index].ID,
			})
			if err != nil {
				return nil, nil, nil, err
			}
			satisfactions = append(satisfactions, satisfaction)
		}
		return decisions, satisfactions, nil, nil
	}

	seeds := make([]workflowstore.AuditRemediationSeed, 0, len(decisions))
	for index, revisionDecision := range decisions {
		seed, err := tx.CreateAuditRemediationSeed(ctx, workflowstore.CreateAuditRemediationSeedParams{
			RemediationSeedID:                workflowstore.NewAuditRemediationSeedID(),
			AuditTicketRevisionDecisionRowID: revisionDecision.ID,
			AuditPacketRowID:                 packet.ID,
			ExecutionPackageRowID:            run.ExecutionPackageRowID.Int64,
			AuditedCommit:                    decision.AuditedCommit,
			DecisionRationale:                decision.Rationale,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		for sequence, finding := range input.MaterialFindings {
			if _, err := tx.CreateAuditRemediationSeedFinding(ctx, workflowstore.CreateAuditRemediationSeedFindingParams{
				RemediationSeedRowID: seed.ID, Sequence: int64(sequence + 1),
				UpstreamClassification: finding.Source, Summary: finding.Summary,
				Evidence: finding.Evidence, RequiredRemediation: finding.RequiredRemediation,
			}); err != nil {
				return nil, nil, nil, err
			}
		}
		if err := reopenCurrentFeatureCompletionForSeed(ctx, tx, obligations[index].DeliveryTicketRowID, seed); err != nil {
			return nil, nil, nil, err
		}
		seeds = append(seeds, seed)
	}
	return decisions, nil, seeds, nil
}

func verifyWorkflowAuditTicketAcceptance(
	ctx context.Context,
	tx *workflowstore.Tx,
	run workflowstore.Run,
	packet workflowstore.AuditPacket,
	obligation workflowstore.AuditPacketTicketObligation,
	document WorkflowAuditPacket,
) error {
	if err := verifyWorkflowAuditTicketDecisionEligibility(ctx, tx, run, packet, obligation); err != nil {
		return err
	}
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, obligation.DeliveryTicketRevisionRowID)
	if err != nil {
		return ErrWorkflowAuditTicketIneligible
	}
	if revision.TransitionApplicability == "required" && !workflowAuditTransitionProof(document, tx, ctx, obligation.AuthorityRevisionRowID) {
		return ErrWorkflowAuditTicketIneligible
	}
	return nil
}

func verifyWorkflowAuditTicketDecisionEligibility(
	ctx context.Context,
	tx *workflowstore.Tx,
	run workflowstore.Run,
	packet workflowstore.AuditPacket,
	obligation workflowstore.AuditPacketTicketObligation,
) error {
	pkg, err := tx.GetExecutionPackageByRowID(ctx, obligation.ExecutionPackageRowID)
	if err != nil || !run.ExecutionPackageRowID.Valid || pkg.ID != run.ExecutionPackageRowID.Int64 ||
		packet.ID != obligation.AuditPacketRowID || packet.Status != workflowstore.AuditPacketStatusCurrent ||
		pkg.AuthorityRevisionRowID != obligation.AuthorityRevisionRowID || pkg.SourceClosureRowID != obligation.SourceClosureRowID {
		return ErrWorkflowAuditTicketIneligible
	}
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, obligation.DeliveryTicketRevisionRowID)
	if err != nil || revision.DeliveryTicketRowID != obligation.DeliveryTicketRowID || revision.CancellationReason.Valid {
		return ErrWorkflowAuditTicketIneligible
	}
	ticket, err := tx.GetDeliveryTicketByRowID(ctx, obligation.DeliveryTicketRowID)
	if err != nil || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
		return ErrWorkflowAuditTicketIneligible
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil || !workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentAuthorityRevisionRowID.Int64 != obligation.AuthorityRevisionRowID {
		return ErrWorkflowAuditTicketIneligible
	}
	return nil
}

func workflowAuditTransitionProof(document WorkflowAuditPacket, tx *workflowstore.Tx, ctx context.Context, authorityRowID int64) bool {
	layers, err := tx.ListFeatureWorkspaceAuthorityLayers(ctx, authorityRowID)
	if err != nil || len(document.Validation) == 0 {
		return false
	}
	hasTransitionAuthority := false
	for _, layer := range layers {
		if layer.LayerKind == "plan" || layer.LayerKind == "transition_plan" {
			hasTransitionAuthority = true
			break
		}
	}
	if !hasTransitionAuthority {
		return false
	}
	for _, validation := range document.Validation {
		if validation.Status != "passed" {
			return false
		}
	}
	return true
}

func reopenCurrentFeatureCompletionForSeed(ctx context.Context, tx *workflowstore.Tx, ticketRowID int64, seed workflowstore.AuditRemediationSeed) error {
	if ticketRowID < 1 {
		return fmt.Errorf("%w: remediation seed has no ticket", ErrWorkflowAuditTicketIneligible)
	}
	ticket, err := tx.GetDeliveryTicketByRowID(ctx, ticketRowID)
	if err != nil {
		return err
	}
	completion, err := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, ticket.WorkspaceRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.CreateFeatureWorkspaceCompletionReopening(ctx, workflowstore.CreateFeatureWorkspaceCompletionReopeningParams{
		CompletionDecisionRowID: completion.ID, ReopeningKind: "remediation_seed",
		ReopeningRemediationSeedRowID: sql.NullInt64{Int64: seed.ID, Valid: true},
	})
	return err
}
