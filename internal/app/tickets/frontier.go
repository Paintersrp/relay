package tickets

import (
	"context"
	"sort"

	workflowstore "relay/internal/store/workflow"
)

// Frontier contains the currently selectable tickets for one Feature Workspace.
// It intentionally contains only derived readiness; no readiness state is stored.
type Frontier struct {
	WorkspaceID string
	Entries     []FrontierEntry
}

// FrontierEntry carries the exact current revision selected by the frontier.
// TieWithPrevious is present only when this entry and the immediately preceding
// entry have the same external priority.
type FrontierEntry struct {
	TicketID           string
	RevisionRowID      int64
	RevisionNumber     int64
	ExternalPriority   int64
	CreatedAt          string
	RepoTarget         string
	Branch             string
	SourceClosureRowID int64
	TieWithPrevious    *AdjacentTieReason
}

// AdjacentTieReason makes the deterministic tie-break visible to transport and
// UI owners without letting either layer recalculate frontier ordering.
type AdjacentTieReason struct {
	PreviousTicketID string
	Rule             FrontierTieRule
}

type FrontierTieRule string

const (
	FrontierTieRuleEarlierCreation FrontierTieRule = "earlier_creation_time"
	FrontierTieRuleStableTicketID  FrontierTieRule = "stable_ticket_id"
)

type ticketReadStore interface {
	GetSourceVaultClosureByRowID(context.Context, int64) (workflowstore.SourceVaultClosure, error)
	GetFeatureWorkspaceByRowID(context.Context, int64) (workflowstore.FeatureWorkspace, error)
	GetFeatureWorkspaceAuthorityRevisionByRowID(context.Context, int64) (workflowstore.FeatureWorkspaceAuthorityRevision, error)
	GetDeliveryTicketRevisionByRowID(context.Context, int64) (workflowstore.DeliveryTicketRevision, error)
	GetDeliveryTicketByRowID(context.Context, int64) (workflowstore.DeliveryTicket, error)
	ListDeliveryTicketRevisionDependencies(context.Context, int64) ([]workflowstore.DeliveryTicketRevisionDependency, error)
	ListDeliveryTicketRevisionApprovals(context.Context, int64) ([]workflowstore.DeliveryTicketRevisionApproval, error)
	GetDeliveryTicketRevisionSatisfaction(context.Context, int64) (workflowstore.DeliveryTicketRevisionSatisfaction, error)
	ListDeliveryTicketSelectionsByWorkspace(context.Context, int64) ([]workflowstore.DeliveryTicketSelection, error)
	ListDeliveryTicketSelectionMembers(context.Context, int64) ([]workflowstore.DeliveryTicketSelectionMember, error)
}

// ListFrontier derives and orders the ready, unselected current ticket revisions
// for one workspace. Priority is descending; ties use ticket creation time and
// then the stable ticket ID.
func (s *Service) ListFrontier(ctx context.Context, workspaceID string) (Frontier, error) {
	if !nonBlank(workspaceID) {
		return Frontier{}, ErrInvalidTicket
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return Frontier{}, err
	}
	tickets, err := s.store.ListDeliveryTicketsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return Frontier{}, err
	}

	entries := make([]FrontierEntry, 0, len(tickets))
	for _, ticket := range tickets {
		if !ticket.CurrentRevisionRowID.Valid {
			continue
		}
		revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
		if err != nil {
			return Frontier{}, err
		}
		detail, err := ticketDetailForRevision(ctx, s.store, ticket, revision)
		if err != nil {
			return Frontier{}, err
		}
		readiness, err := deriveTicketReadiness(ctx, s.store, detail)
		if err != nil {
			return Frontier{}, err
		}
		if !readiness.Ready || readiness.Selected || readiness.Completed {
			continue
		}
		entries = append(entries, FrontierEntry{
			TicketID:           ticket.TicketID,
			RevisionRowID:      revision.ID,
			RevisionNumber:     revision.RevisionNumber,
			ExternalPriority:   ticket.ExternalPriority,
			CreatedAt:          ticket.CreatedAt,
			RepoTarget:         revision.RepoTarget,
			Branch:             revision.Branch,
			SourceClosureRowID: revision.SourceClosureRowID,
		})
	}

	sort.Slice(entries, func(left, right int) bool {
		return frontierEntryBefore(entries[left], entries[right])
	})
	annotateFrontierTies(entries)
	return Frontier{WorkspaceID: workspace.WorkspaceID, Entries: entries}, nil
}

func frontierEntryBefore(left, right FrontierEntry) bool {
	if left.ExternalPriority != right.ExternalPriority {
		return left.ExternalPriority > right.ExternalPriority
	}
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt < right.CreatedAt
	}
	return left.TicketID < right.TicketID
}

func annotateFrontierTies(entries []FrontierEntry) {
	for index := 1; index < len(entries); index++ {
		previous := entries[index-1]
		current := &entries[index]
		if previous.ExternalPriority != current.ExternalPriority {
			continue
		}
		rule := FrontierTieRuleEarlierCreation
		if previous.CreatedAt == current.CreatedAt {
			rule = FrontierTieRuleStableTicketID
		}
		current.TieWithPrevious = &AdjacentTieReason{PreviousTicketID: previous.TicketID, Rule: rule}
	}
}

func ticketDetailForRevision(
	ctx context.Context,
	reader ticketReadStore,
	ticket workflowstore.DeliveryTicket,
	revision workflowstore.DeliveryTicketRevision,
) (TicketDetail, error) {
	detail := TicketDetail{Ticket: ticket, Revision: revision}
	var err error
	if detail.Dependencies, err = reader.ListDeliveryTicketRevisionDependencies(ctx, revision.ID); err != nil {
		return TicketDetail{}, err
	}
	if detail.Approvals, err = reader.ListDeliveryTicketRevisionApprovals(ctx, revision.ID); err != nil {
		return TicketDetail{}, err
	}
	return detail, nil
}
