package tickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
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

// SelectionMemberInput identifies the exact revision an operator observed in
// the frontier. The service rejects it rather than silently selecting a later
// replacement revision.
type SelectionMemberInput struct {
	TicketID      string
	RevisionRowID int64
}

type SelectInput struct {
	WorkspaceID string
	Members     []SelectionMemberInput
	Rationale   string
}

type SelectedTicket struct {
	TicketID       string
	RevisionRowID  int64
	RevisionNumber int64
	ApprovalRowID  int64
}

// SelectionResult retains the exact selection record and the exact approved
// revisions that were atomically reserved by it.
type SelectionResult struct {
	Selection workflowstore.DeliveryTicketSelection
	Members   []SelectedTicket
}

type selectionCandidate struct {
	ticket   workflowstore.DeliveryTicket
	revision workflowstore.DeliveryTicketRevision
	approval workflowstore.DeliveryTicketRevisionApproval
}

// Select creates one active selection only after it has revalidated every
// requested ticket revision in the same database transaction. It does not
// create packages, Runs, or mutate any ticket or priority fields.
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

		candidates := make([]selectionCandidate, 0, len(input.Members))
		for _, requested := range input.Members {
			ticket, err := tx.GetDeliveryTicketByTicketID(ctx, requested.TicketID)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: ticket %s was not found", ErrSelectionMemberStale, requested.TicketID)
			}
			if err != nil {
				return err
			}
			if ticket.WorkspaceRowID != workspace.ID {
				return fmt.Errorf("%w: ticket %s belongs to another workspace", ErrSelectionMemberStale, requested.TicketID)
			}
			revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, requested.RevisionRowID)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: revision %d was not found", ErrSelectionMemberStale, requested.RevisionRowID)
			}
			if err != nil {
				return err
			}
			if revision.DeliveryTicketRowID != ticket.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
				return fmt.Errorf("%w: ticket %s no longer has revision %d as current", ErrSelectionMemberStale, requested.TicketID, requested.RevisionRowID)
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
			candidates = append(candidates, selectionCandidate{ticket: ticket, revision: revision, approval: approval})
		}

		if err := validateSelectionBundle(candidates); err != nil {
			return err
		}
		sort.Slice(candidates, func(left, right int) bool {
			return frontierEntryBefore(frontierEntryForCandidate(candidates[left]), frontierEntryForCandidate(candidates[right]))
		})

		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
			SelectionID:        workflowstore.NewDeliveryTicketSelectionID(),
			WorkspaceRowID:     workspace.ID,
			State:              "active",
			Rationale:          input.Rationale,
			SourceClosureRowID: sql.NullInt64{Int64: candidates[0].revision.SourceClosureRowID, Valid: true},
		})
		if err != nil {
			return selectionConflictError(err)
		}

		members := make([]SelectedTicket, 0, len(candidates))
		for index, candidate := range candidates {
			if _, err := tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{
				SelectionRowID: selection.ID,
				Sequence:       int64(index + 1),
				RevisionRowID:  candidate.revision.ID,
				ApprovalRowID:  candidate.approval.ID,
			}); err != nil {
				return err
			}
			members = append(members, SelectedTicket{
				TicketID:       candidate.ticket.TicketID,
				RevisionRowID:  candidate.revision.ID,
				RevisionNumber: candidate.revision.RevisionNumber,
				ApprovalRowID:  candidate.approval.ID,
			})
		}
		result = SelectionResult{Selection: selection, Members: members}
		return nil
	})
	if err != nil {
		return SelectionResult{}, selectionConflictError(err)
	}
	return result, nil
}

func validateSelectInput(input SelectInput) error {
	if !nonBlank(input.WorkspaceID) || !nonBlank(input.Rationale) || len(input.Members) == 0 {
		return ErrInvalidSelection
	}
	seenTicketIDs := make(map[string]struct{}, len(input.Members))
	seenRevisionIDs := make(map[int64]struct{}, len(input.Members))
	for _, member := range input.Members {
		if !nonBlank(member.TicketID) || member.RevisionRowID < 1 {
			return ErrInvalidSelection
		}
		if _, exists := seenTicketIDs[member.TicketID]; exists {
			return ErrInvalidSelection
		}
		if _, exists := seenRevisionIDs[member.RevisionRowID]; exists {
			return ErrInvalidSelection
		}
		seenTicketIDs[member.TicketID] = struct{}{}
		seenRevisionIDs[member.RevisionRowID] = struct{}{}
	}
	return nil
}

func frontierEntryForCandidate(candidate selectionCandidate) FrontierEntry {
	return FrontierEntry{
		TicketID:         candidate.ticket.TicketID,
		ExternalPriority: candidate.ticket.ExternalPriority,
		CreatedAt:        candidate.ticket.CreatedAt,
	}
}

func validateSelectionBundle(candidates []selectionCandidate) error {
	if len(candidates) == 0 {
		return ErrInvalidSelection
	}
	first := candidates[0].revision
	for _, candidate := range candidates[1:] {
		revision := candidate.revision
		if revision.RepoTarget != first.RepoTarget || revision.Branch != first.Branch ||
			revision.SourceClosureRowID != first.SourceClosureRowID || revision.BaseCommit != first.BaseCommit {
			return fmt.Errorf("%w: tickets must share one repository, branch, and retained source basis", ErrIncompatibleSelection)
		}
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
	case readinessHasReason(readiness, "dependency_outcome_not_satisfied"), readinessHasReason(readiness, "dependency_revision_stale"):
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
