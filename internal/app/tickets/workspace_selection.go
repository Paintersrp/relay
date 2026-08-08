package tickets

import (
	"context"
	"strings"
)

// WorkspaceSelection is the delivery-owner semantic read of the current
// Delivery Ticket selection for one workspace: none | active | consumed |
// superseded | cancelled. It carries the selected ticket's public identity and
// revision number when a selection exists.
type WorkspaceSelection struct {
	State          string
	SelectionID    string
	TicketID       string
	RevisionNumber int64
}

// ReadWorkspaceSelection is the tickets-owner semantic read consumed by the
// guided journey projection. Consumers must not reconstruct selection state
// from delivery_ticket_selections rows.
func (s *Service) ReadWorkspaceSelection(ctx context.Context, workspaceID string) (WorkspaceSelection, error) {
	if !nonBlank(workspaceID) {
		return WorkspaceSelection{}, ErrInvalidTicket
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return WorkspaceSelection{}, err
	}
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return WorkspaceSelection{}, err
	}
	result := WorkspaceSelection{State: "none"}
	if len(selections) == 0 {
		return result, nil
	}
	latest := selections[0]
	for _, selection := range selections {
		if selection.ID > latest.ID {
			latest = selection
		}
	}
	result.State = latest.State
	result.SelectionID = latest.SelectionID
	members, err := s.store.ListDeliveryTicketSelectionMembers(ctx, latest.ID)
	if err != nil {
		return WorkspaceSelection{}, err
	}
	if len(members) == 0 {
		return result, nil
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, members[0].RevisionRowID)
	if err != nil {
		return WorkspaceSelection{}, err
	}
	ticket, err := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return WorkspaceSelection{}, err
	}
	result.TicketID = ticket.TicketID
	result.RevisionNumber = revision.RevisionNumber
	return result, nil
}
