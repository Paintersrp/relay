package tickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidSelection             = errors.New("invalid delivery ticket selection")
	ErrSelectionWorkspaceNotFound   = errors.New("delivery ticket selection workspace not found")
	ErrSelectionConflict            = errors.New("delivery ticket selection conflict")
	ErrSelectionMemberStale         = errors.New("delivery ticket selection member is stale")
	ErrSelectionMemberNotReady      = errors.New("delivery ticket selection member is not ready")
	ErrSelectionSourceStale         = errors.New("delivery ticket selection source is stale")
	ErrSelectionAuthorityStale      = errors.New("delivery ticket selection authority is stale")
	ErrSelectionDependenciesInvalid = errors.New("delivery ticket selection dependencies are not satisfied")
	ErrIncompatibleSelection        = errors.New("delivery ticket selection members are incompatible")
)

type SelectInput struct {
	WorkspaceID   string
	TicketID      string
	RevisionRowID int64
	Rationale     string
}

type SelectedTicket struct {
	TicketID       string
	RevisionRowID  int64
	RevisionNumber int64
	ApprovalRowID  int64
}

type SelectionResult struct {
	Selection      workflowstore.DeliveryTicketSelection
	SelectedTicket SelectedTicket
}

type selectionCandidate struct {
	ticket   workflowstore.DeliveryTicket
	revision workflowstore.DeliveryTicketRevision
	approval workflowstore.DeliveryTicketRevisionApproval
}

func (s *Service) Select(ctx context.Context, input SelectInput) (SelectionResult, error) {
	if err := validateSelectInput(input); err != nil {
		return SelectionResult{}, err
	}

	result := SelectionResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrSelectionWorkspaceNotFound, input.WorkspaceID)
		}
		if err != nil {
			return err
		}
		existing, err := tx.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
		if err != nil {
			return err
		}
		for _, selection := range existing {
			if selection.State == "active" {
				return ErrSelectionConflict
			}
		}

		ticket, err := tx.GetDeliveryTicketByTicketID(ctx, input.TicketID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: ticket %s was not found", ErrSelectionMemberStale, input.TicketID)
		}
		if err != nil {
			return err
		}
		if ticket.WorkspaceRowID != workspace.ID {
			return fmt.Errorf("%w: ticket %s belongs to another workspace", ErrSelectionMemberStale, input.TicketID)
		}
		revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, input.RevisionRowID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: revision %d was not found", ErrSelectionMemberStale, input.RevisionRowID)
		}
		if err != nil {
			return err
		}
		if revision.DeliveryTicketRowID != ticket.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
			return fmt.Errorf("%w: ticket %s no longer has revision %d as current", ErrSelectionMemberStale, input.TicketID, input.RevisionRowID)
		}
		detail, err := ticketDetailForRevision(ctx, tx, ticket, revision)
		if err != nil {
			return err
		}
		readiness, err := deriveTicketReadiness(ctx, tx, detail)
		if err != nil {
			return err
		}
		if readiness.Selected {
			return ErrSelectionConflict
		}
		if !readiness.Ready {
			return selectionReadinessError(ticket.TicketID, readiness)
		}
		approval, ok := currentDeliveryApproval(workspace, revision, detail.Approvals)
		if !ok {
			return fmt.Errorf("%w: ticket %s approval changed", ErrSelectionAuthorityStale, ticket.TicketID)
		}

		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID:        workflowstore.NewDeliveryTicketSelectionID(),
			WorkspaceRowID:     workspace.ID,
			State:              "active",
			Rationale:          input.Rationale,
			SourceClosureRowID: sql.NullInt64{Int64: revision.SourceClosureRowID, Valid: true},
		})
		if err != nil {
			return selectionConflictError(err)
		}

		if _, err := tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID,
			Sequence:       1,
			RevisionRowID:  revision.ID,
			ApprovalRowID:  approval.ID,
		}); err != nil {
			return err
		}
		result = SelectionResult{
			Selection: selection,
			SelectedTicket: SelectedTicket{
				TicketID:       ticket.TicketID,
				RevisionRowID:  revision.ID,
				RevisionNumber: revision.RevisionNumber,
				ApprovalRowID:  approval.ID,
			},
		}
		return nil
	})
	if err != nil {
		return SelectionResult{}, selectionConflictError(err)
	}
	return result, nil
}

func validateSelectInput(input SelectInput) error {
	if !nonBlank(input.WorkspaceID) || !nonBlank(input.TicketID) || input.RevisionRowID < 1 || !nonBlank(input.Rationale) {
		return ErrInvalidSelection
	}
	return nil
}

func currentDeliveryApproval(
	workspace workflowstore.FeatureWorkspace,
	revision workflowstore.DeliveryTicketRevision,
	approvals []workflowstore.DeliveryTicketRevisionApproval,
) (workflowstore.DeliveryTicketRevisionApproval, bool) {
	if !workspace.CurrentAuthorityRevisionRowID.Valid {
		return workflowstore.DeliveryTicketRevisionApproval{}, false
	}
	for _, approval := range approvals {
		if approval.ApprovalKind == "delivery" && approval.ApprovalState == "approved" &&
			approval.SourceClosureRowID == revision.SourceClosureRowID &&
			approval.AuthorityRevisionRowID.Valid && approval.AuthorityRevisionRowID.Int64 == workspace.CurrentAuthorityRevisionRowID.Int64 {
			return approval, true
		}
	}
	return workflowstore.DeliveryTicketRevisionApproval{}, false
}

func selectionReadinessError(ticketID string, readiness Readiness) error {
	switch {
	case readinessHasReason(readiness, "source_not_current"):
		return fmt.Errorf("%w: ticket %s", ErrSelectionSourceStale, ticketID)
	case readinessHasReason(readiness, "authority_missing"), readinessHasReason(readiness, "authority_stale"), readinessHasReason(readiness, "approval_missing_or_stale"):
		return fmt.Errorf("%w: ticket %s", ErrSelectionAuthorityStale, ticketID)
	case readinessHasReason(readiness, "dependency_outcome_not_satisfied"), readinessHasReason(readiness, "dependency_revision_stale"), readinessHasReason(readiness, "dependency_outcome_incomplete"):
		return fmt.Errorf("%w: ticket %s", ErrSelectionDependenciesInvalid, ticketID)
	default:
		return fmt.Errorf("%w: ticket %s (%s)", ErrSelectionMemberNotReady, ticketID, strings.Join(readiness.Reasons, ","))
	}
}

func readinessHasReason(readiness Readiness, want string) bool {
	for _, reason := range readiness.Reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func selectionConflictError(err error) error {
	if err == nil || errors.Is(err, ErrSelectionConflict) {
		return err
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "idx_delivery_ticket_selections_one_active_workspace") ||
		strings.Contains(message, "delivery_ticket_selections.workspace_row_id") {
		return fmt.Errorf("%w: %v", ErrSelectionConflict, err)
	}
	return err
}
