package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	featureapp "relay/internal/app/features"
	workflowstore "relay/internal/store/workflow"
)

const (
	maxWorkflowAuditMaterialFindings = 32
	maxWorkflowAuditObservations     = 32
)

type workflowAuditCurrentnessReader interface {
	featureapp.CurrentnessReader
	GetFeatureWorkspaceByRowID(context.Context, int64) (workflowstore.FeatureWorkspace, error)
	GetExecutionPackageByRowID(context.Context, int64) (workflowstore.ExecutionPackage, error)
	GetRunExecutionPackageApproval(context.Context, int64) (workflowstore.ExecutionPackageApproval, error)
	GetDeliveryTicketSelectionByRowID(context.Context, int64) (workflowstore.DeliveryTicketSelection, error)
	ListDeliveryTicketSelectionMembers(context.Context, int64) ([]workflowstore.DeliveryTicketSelectionMember, error)
	ListExecutionPackageMembers(context.Context, int64) ([]workflowstore.ExecutionPackageMember, error)
	GetDeliveryTicketRevisionByRowID(context.Context, int64) (workflowstore.DeliveryTicketRevision, error)
	GetDeliveryTicketByRowID(context.Context, int64) (workflowstore.DeliveryTicket, error)
	GetDeliveryTicketRevisionApprovalByRowID(context.Context, int64) (workflowstore.DeliveryTicketRevisionApproval, error)
}

func verifyWorkflowPackageCurrentness(ctx context.Context, reader workflowAuditCurrentnessReader, run workflowstore.Run) error {
	if !run.ExecutionPackageRowID.Valid || !run.PackageApprovalRowID.Valid {
		return ErrWorkflowAuditPacketStale
	}
	pkg, err := reader.GetExecutionPackageByRowID(ctx, run.ExecutionPackageRowID.Int64)
	if err != nil || pkg.ID != run.ExecutionPackageRowID.Int64 || pkg.RepoTarget != run.RepoTarget || pkg.Branch != run.Branch || pkg.BaseCommit != run.BaseCommit {
		return ErrWorkflowAuditPacketStale
	}
	approval, err := reader.GetRunExecutionPackageApproval(ctx, run.ID)
	if err != nil || approval.ID != run.PackageApprovalRowID.Int64 || approval.PackageRowID != pkg.ID || approval.PackageSha256 != pkg.PackageSha256 {
		return ErrWorkflowAuditPacketStale
	}
	workspace, err := reader.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil {
		return ErrWorkflowAuditPacketStale
	}
	currentness, err := featureapp.EvaluateCurrentness(ctx, reader, workspace.WorkspaceID)
	if err != nil || currentness.Readiness != featureapp.FeatureCurrent || currentness.WorkspaceVersion != workspace.Version || !currentness.AuthorityRevisionRowID.Valid || currentness.AuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
		return ErrWorkflowAuditPacketStale
	}
	authority, err := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, pkg.AuthorityRevisionRowID)
	if err != nil || authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return ErrWorkflowAuditPacketStale
	}
	closure, err := reader.GetSourceVaultClosureByRowID(ctx, pkg.SourceClosureRowID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != pkg.BaseCommit {
		return ErrWorkflowAuditPacketStale
	}
	selection, err := reader.GetDeliveryTicketSelectionByRowID(ctx, pkg.SelectionRowID)
	if err != nil || selection.State != "consumed" || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != closure.ID {
		return ErrWorkflowAuditPacketStale
	}
	members, err := reader.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || len(members) == 0 {
		return ErrWorkflowAuditPacketStale
	}
	selectionMembers, err := reader.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil || len(selectionMembers) != len(members) {
		return ErrWorkflowAuditPacketStale
	}
	for _, member := range members {
		var selected workflowstore.DeliveryTicketSelectionMember
		found := false
		for _, candidate := range selectionMembers {
			if candidate.ID == member.SelectionMemberRowID && candidate.RevisionRowID == member.RevisionRowID {
				selected, found = candidate, true
				break
			}
		}
		if !found || selected.ApprovalRowID < 1 {
			return ErrWorkflowAuditPacketStale
		}
		revision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if err != nil || revision.CancellationReason.Valid || revision.SourceClosureRowID != closure.ID || revision.RepoTarget != pkg.RepoTarget || revision.Branch != pkg.Branch || revision.BaseCommit != pkg.BaseCommit {
			return ErrWorkflowAuditPacketStale
		}
		ticket, err := reader.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
			return ErrWorkflowAuditPacketStale
		}
		ticketApproval, err := reader.GetDeliveryTicketRevisionApprovalByRowID(ctx, selected.ApprovalRowID)
		if err != nil || ticketApproval.RevisionRowID != revision.ID || ticketApproval.ApprovalKind != "delivery" || ticketApproval.ApprovalState != "approved" || ticketApproval.SourceClosureRowID != closure.ID || !ticketApproval.AuthorityRevisionRowID.Valid || ticketApproval.AuthorityRevisionRowID.Int64 != authority.ID {
			return ErrWorkflowAuditPacketStale
		}
	}
	return nil
}

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
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil {
		return ErrWorkflowAuditPacketStale
	}
	currentness, err := featureapp.EvaluateCurrentness(ctx, tx, workspace.WorkspaceID)
	if err != nil || currentness.Readiness != featureapp.FeatureCurrent || currentness.WorkspaceVersion != workspace.Version ||
		!currentness.AuthorityRevisionRowID.Valid || currentness.AuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
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
	selection, err := tx.GetDeliveryTicketSelectionByRowID(ctx, pkg.SelectionRowID)
	if err != nil || selection.State != "consumed" || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != closure.ID {
		return ErrWorkflowAuditPacketStale
	}
	members, err := tx.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || len(members) == 0 {
		return ErrWorkflowAuditPacketStale
	}
	selectionMembers, err := tx.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil || len(selectionMembers) != len(members) {
		return ErrWorkflowAuditPacketStale
	}
	for _, member := range members {
		var selectedMember workflowstore.DeliveryTicketSelectionMember
		found := false
		for _, candidate := range selectionMembers {
			if candidate.ID == member.SelectionMemberRowID && candidate.RevisionRowID == member.RevisionRowID {
				selectedMember, found = candidate, true
				break
			}
		}
		if !found || selectedMember.ApprovalRowID < 1 {
			return ErrWorkflowAuditPacketStale
		}
		revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if err != nil || revision.SourceClosureRowID != closure.ID || revision.RepoTarget != pkg.RepoTarget || revision.Branch != pkg.Branch || revision.BaseCommit != pkg.BaseCommit || revision.CancellationReason.Valid {
			return ErrWorkflowAuditPacketStale
		}
		ticket, err := tx.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
			return ErrWorkflowAuditPacketStale
		}
		ticketApproval, err := tx.GetDeliveryTicketRevisionApprovalByRowID(ctx, selectedMember.ApprovalRowID)
		if err != nil || ticketApproval.RevisionRowID != revision.ID || ticketApproval.ApprovalKind != "delivery" || ticketApproval.ApprovalState != "approved" || ticketApproval.SourceClosureRowID != closure.ID || !ticketApproval.AuthorityRevisionRowID.Valid || ticketApproval.AuthorityRevisionRowID.Int64 != authority.ID {
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

func validateWorkflowPackageAuditDecisionInput(input RecordWorkflowAuditDecisionInput, packet json.RawMessage) error {
	if strings.TrimSpace(input.Rationale) == "" || len(input.MaterialFindings) > maxWorkflowAuditMaterialFindings || len(input.Observations) > maxWorkflowAuditObservations {
		return ErrWorkflowAuditDecisionInput
	}
	if input.Decision == workflowstore.AuditDecisionAccepted && len(input.MaterialFindings) != 0 {
		return ErrWorkflowAuditDecisionInput
	}
	if input.Decision == workflowstore.AuditDecisionNeedsRevision && len(input.MaterialFindings) == 0 {
		return ErrWorkflowAuditDecisionInput
	}
	var document WorkflowPackageAuditPacket
	if validateWorkflowPackageAuditPacketBytes(packet) != nil || json.Unmarshal(packet, &document) != nil {
		return ErrWorkflowAuditPacketStale
	}
	for _, finding := range input.MaterialFindings {
		if (finding.Source != "implementation" && finding.Source != "governing_package" && finding.Source != "both") || strings.TrimSpace(finding.Summary) == "" || strings.TrimSpace(finding.Evidence) == "" || strings.TrimSpace(finding.RequiredRemediation) == "" {
			return ErrWorkflowAuditDecisionInput
		}
		if (finding.Source == "implementation" || finding.Source == "both") && document.Execution.Status == "" {
			return ErrWorkflowAuditDecisionInput
		}
		if (finding.Source == "governing_package" || finding.Source == "both") && len(document.Authority.DeliveryTicket.Content) == 0 {
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

func applyWorkflowPackageAuditTicketDecisionEffects(ctx context.Context, tx *workflowstore.Tx, run workflowstore.Run, packet workflowstore.AuditPacket, decision workflowstore.AuditDecision, document WorkflowPackageAuditPacket, input RecordWorkflowAuditDecisionInput) ([]workflowstore.AuditTicketRevisionDecision, []workflowstore.DeliveryTicketRevisionSatisfaction, []workflowstore.AuditRemediationSeed, error) {
	obligations, err := tx.ListAuditPacketTicketObligations(ctx, packet.ID)
	if err != nil || len(obligations) == 0 {
		return nil, nil, nil, ErrWorkflowAuditPacketStale
	}
	decisions := make([]workflowstore.AuditTicketRevisionDecision, 0, len(obligations))
	for _, obligation := range obligations {
		if err := verifyWorkflowPackageAuditTicketDecisionEligibility(ctx, tx, run, packet, obligation); err != nil {
			return nil, nil, nil, err
		}
		approval, err := tx.GetRunExecutionPackageApproval(ctx, run.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		if !obligation.PackageApprovalRowID.Valid || !obligation.ApprovedPackageSha256.Valid || obligation.PackageApprovalRowID.Int64 != approval.ID || obligation.ApprovedPackageSha256.String != approval.PackageSha256 {
			return nil, nil, nil, ErrWorkflowAuditTicketIneligible
		}
		d, err := tx.CreateAuditTicketRevisionDecision(ctx, workflowstore.CreateAuditTicketRevisionDecisionParams{AuditDecisionRowID: decision.ID, AuditPacketTicketObligationRowID: obligation.ID, PackageApprovalRowID: sql.NullInt64{Int64: approval.ID, Valid: true}, ApprovedPackageSha256: sql.NullString{String: approval.PackageSha256, Valid: true}})
		if err != nil {
			return nil, nil, nil, err
		}
		decisions = append(decisions, d)
	}
	if input.Decision == workflowstore.AuditDecisionNeedsRevision {
		seeds := make([]workflowstore.AuditRemediationSeed, 0, len(decisions))
		for _, revisionDecision := range decisions {
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
					RemediationSeedRowID:   seed.ID,
					Sequence:               int64(sequence + 1),
					UpstreamClassification: finding.Source,
					Summary:                finding.Summary,
					Evidence:               finding.Evidence,
					RequiredRemediation:    finding.RequiredRemediation,
				}); err != nil {
					return nil, nil, nil, err
				}
			}
			seeds = append(seeds, seed)
		}
		return decisions, []workflowstore.DeliveryTicketRevisionSatisfaction{}, seeds, nil
	}
	satisfactions := make([]workflowstore.DeliveryTicketRevisionSatisfaction, 0, len(obligations))
	for i, obligation := range obligations {
		revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, obligation.DeliveryTicketRevisionRowID)
		if err != nil {
			return nil, nil, nil, ErrWorkflowAuditTicketIneligible
		}
		if revision.TransitionApplicability == "required" && !workflowPackageAuditTransitionProof(document, tx, ctx, obligation.AuthorityRevisionRowID) {
			return nil, nil, nil, ErrWorkflowAuditTicketIneligible
		}
		s, err := tx.CreateDeliveryTicketRevisionSatisfaction(ctx, workflowstore.CreateDeliveryTicketRevisionSatisfactionParams{DeliveryTicketRevisionRowID: obligation.DeliveryTicketRevisionRowID, AuditTicketRevisionDecisionRowID: decisions[i].ID})
		if err != nil {
			return nil, nil, nil, err
		}
		satisfactions = append(satisfactions, s)
	}
	return decisions, satisfactions, nil, nil
}

// verifyWorkflowPackageDecisionAuthority reloads all decision authority using
// the caller's transaction. The pre-transaction evidence is the immutable
// comparison basis; it must never be loaded again while persistence is open.
func verifyWorkflowPackageDecisionAuthority(ctx context.Context, tx *workflowstore.Tx, run workflowstore.Run, packet workflowstore.AuditPacket, evidence WorkflowPackageExecutionEvidence) error {
	if err := verifyWorkflowPackageCurrentness(ctx, tx, run); err != nil {
		return err
	}
	if !sameWorkflowPackageRunIdentity(evidence.Run, run) {
		return ErrWorkflowAuditPacketStale
	}
	pkg, err := tx.GetExecutionPackageByRowID(ctx, evidence.Authority.Package.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(pkg, evidence.Authority.Package) || !run.ExecutionPackageRowID.Valid || run.ExecutionPackageRowID.Int64 != pkg.ID {
		return ErrWorkflowAuditPacketStale
	}
	approval, err := tx.GetRunExecutionPackageApproval(ctx, run.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkflowAuditPacketStale
		}
		return err
	}
	if !reflect.DeepEqual(approval, evidence.Authority.PackageApproval) || !run.PackageApprovalRowID.Valid || run.PackageApprovalRowID.Int64 != approval.ID || approval.PackageRowID != pkg.ID || approval.PackageSha256 != pkg.PackageSha256 {
		return ErrWorkflowAuditPacketStale
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil {
		return err
	}
	currentness, err := featureapp.EvaluateCurrentness(ctx, tx, workspace.WorkspaceID)
	if err != nil || currentness.Readiness != featureapp.FeatureCurrent || currentness.WorkspaceVersion != workspace.Version || !currentness.AuthorityRevisionRowID.Valid || currentness.AuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
		return ErrWorkflowAuditPacketStale
	}
	authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, pkg.AuthorityRevisionRowID)
	if err != nil {
		return err
	}
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, pkg.SourceClosureRowID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(workspace, evidence.Authority.Workspace) || !reflect.DeepEqual(authority, evidence.Authority.Authority) || !reflect.DeepEqual(closure, evidence.Authority.Source) || !workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentAuthorityRevisionRowID.Int64 != authority.ID || authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != closure.ID || closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != pkg.BaseCommit {
		return ErrWorkflowAuditPacketStale
	}
	ticket, err := tx.GetDeliveryTicketByRowID(ctx, evidence.Authority.Ticket.ID)
	if err != nil {
		return err
	}
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, evidence.Authority.TicketRevision.ID)
	if err != nil {
		return err
	}
	approvals, err := tx.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return err
	}
	approved := false
	for _, candidate := range approvals {
		if reflect.DeepEqual(candidate, evidence.Authority.TicketApproval) {
			approved = true
			break
		}
	}
	if !reflect.DeepEqual(ticket, evidence.Authority.Ticket) || !reflect.DeepEqual(revision, evidence.Authority.TicketRevision) || !approved || ticket.TicketID != evidence.Authority.Ticket.TicketID || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID || revision.DeliveryTicketRowID != ticket.ID || revision.RepoTarget != pkg.RepoTarget || revision.Branch != pkg.Branch || revision.BaseCommit != pkg.BaseCommit || revision.CancellationReason.Valid || revision.SourceClosureRowID != closure.ID || evidence.Authority.TicketApproval.RevisionRowID != revision.ID || evidence.Authority.TicketApproval.ApprovalKind != "delivery" || evidence.Authority.TicketApproval.ApprovalState != "approved" || evidence.Authority.TicketApproval.SourceClosureRowID != closure.ID || !evidence.Authority.TicketApproval.AuthorityRevisionRowID.Valid || evidence.Authority.TicketApproval.AuthorityRevisionRowID.Int64 != authority.ID {
		return ErrWorkflowAuditPacketStale
	}
	members, err := tx.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil {
		return err
	}
	obligations, err := tx.ListAuditPacketTicketObligations(ctx, packet.ID)
	if err != nil {
		return err
	}
	if len(obligations) == 0 {
		return ErrWorkflowAuditPacketStale
	}
	for _, obligation := range obligations {
		if obligation.AuditPacketRowID != packet.ID || obligation.ExecutionPackageRowID != pkg.ID || obligation.DeliveryTicketRowID != ticket.ID || obligation.DeliveryTicketRevisionRowID != revision.ID || obligation.AuthorityRevisionRowID != authority.ID || obligation.SourceClosureRowID != closure.ID || !obligation.PackageApprovalRowID.Valid || obligation.PackageApprovalRowID.Int64 != approval.ID || !obligation.ApprovedPackageSha256.Valid || obligation.ApprovedPackageSha256.String != approval.PackageSha256 {
			return ErrWorkflowAuditTicketIneligible
		}
		memberFound := false
		for _, member := range members {
			if member.ID == obligation.ExecutionPackageMemberRowID && member.PackageRowID == pkg.ID && member.RevisionRowID == revision.ID {
				memberFound = true
				break
			}
		}
		if !memberFound {
			return ErrWorkflowAuditTicketIneligible
		}
	}
	return nil
}

func verifyWorkflowPackageAuditTicketDecisionEligibility(ctx context.Context, tx *workflowstore.Tx, run workflowstore.Run, packet workflowstore.AuditPacket, obligation workflowstore.AuditPacketTicketObligation) error {
	if !run.ExecutionPackageRowID.Valid || obligation.AuditPacketRowID != packet.ID || packet.Status != workflowstore.AuditPacketStatusCurrent || obligation.ExecutionPackageRowID != run.ExecutionPackageRowID.Int64 {
		return ErrWorkflowAuditTicketIneligible
	}
	pkg, err := tx.GetExecutionPackageByRowID(ctx, obligation.ExecutionPackageRowID)
	if err != nil {
		return err
	}
	if pkg.AuthorityRevisionRowID != obligation.AuthorityRevisionRowID || pkg.SourceClosureRowID != obligation.SourceClosureRowID {
		return ErrWorkflowAuditTicketIneligible
	}
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, obligation.DeliveryTicketRevisionRowID)
	if err != nil {
		return err
	}
	if revision.DeliveryTicketRowID != obligation.DeliveryTicketRowID || revision.CancellationReason.Valid {
		return ErrWorkflowAuditTicketIneligible
	}
	ticket, err := tx.GetDeliveryTicketByRowID(ctx, obligation.DeliveryTicketRowID)
	if err != nil {
		return err
	}
	if !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
		return ErrWorkflowAuditTicketIneligible
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil {
		return err
	}
	if !workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentAuthorityRevisionRowID.Int64 != obligation.AuthorityRevisionRowID {
		return ErrWorkflowAuditTicketIneligible
	}
	members, err := tx.ListExecutionPackageMembers(ctx, obligation.ExecutionPackageRowID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.ID == obligation.ExecutionPackageMemberRowID && member.PackageRowID == obligation.ExecutionPackageRowID && member.RevisionRowID == obligation.DeliveryTicketRevisionRowID {
			return nil
		}
	}
	return ErrWorkflowAuditTicketIneligible
}

func workflowPackageAuditTransitionProof(document WorkflowPackageAuditPacket, tx *workflowstore.Tx, ctx context.Context, authorityRowID int64) bool {
	layers, err := tx.ListFeatureWorkspaceAuthorityLayers(ctx, authorityRowID)
	if err != nil || len(document.Validation) == 0 {
		return false
	}
	hasTransition := false
	for _, layer := range layers {
		if layer.LayerKind == "plan" || layer.LayerKind == "transition_plan" {
			hasTransition = true
			break
		}
	}
	if !hasTransition {
		return false
	}
	for _, validation := range document.Validation {
		if validation.Status != "passed" {
			return false
		}
	}
	return true
}
