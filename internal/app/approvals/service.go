package approvals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidApproval = errors.New("invalid delivery ticket approval")
	ErrStaleAuthority  = errors.New("delivery ticket approval authority is stale")
)

type IDGenerator interface {
	ApprovalID() string
}

type defaultIDGenerator struct{}

func (defaultIDGenerator) ApprovalID() string { return workflowstore.NewDeliveryTicketApprovalID() }

type Service struct {
	store *workflowstore.Store
	ids   IDGenerator
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{})
}

func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator) (*Service, error) {
	if store == nil || ids == nil {
		return nil, ErrInvalidApproval
	}
	return &Service{store: store, ids: ids}, nil
}

type ApproveDeliveryTicketInput struct {
	TicketID            string
	RevisionRowID       int64
	AuthorityRevisionID string
	Rationale           string
}

func (s *Service) ApproveDeliveryTicket(ctx context.Context, input ApproveDeliveryTicketInput) (workflowstore.DeliveryTicketRevisionApproval, error) {
	if strings.TrimSpace(input.TicketID) != input.TicketID || input.TicketID == "" || input.RevisionRowID < 1 ||
		strings.TrimSpace(input.AuthorityRevisionID) != input.AuthorityRevisionID || input.AuthorityRevisionID == "" ||
		strings.TrimSpace(input.Rationale) != input.Rationale || input.Rationale == "" {
		return workflowstore.DeliveryTicketRevisionApproval{}, ErrInvalidApproval
	}

	var approval workflowstore.DeliveryTicketRevisionApproval
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		ticket, err := tx.GetDeliveryTicketByTicketID(ctx, input.TicketID)
		if err != nil {
			return err
		}
		revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, input.RevisionRowID)
		if err != nil {
			return err
		}
		if revision.DeliveryTicketRowID != ticket.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
			return ErrStaleAuthority
		}
		workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, ticket.WorkspaceRowID)
		if err != nil {
			return err
		}
		if !workspace.CurrentAuthorityRevisionRowID.Valid {
			return ErrStaleAuthority
		}
		authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil {
			return err
		}
		if authority.WorkspaceRowID != workspace.ID || authority.AuthorityRevisionID != input.AuthorityRevisionID ||
			!authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != revision.SourceClosureRowID {
			return ErrStaleAuthority
		}
		closure, err := tx.GetSourceVaultClosureByRowID(ctx, revision.SourceClosureRowID)
		if err != nil {
			return err
		}
		if closure.State != workflowstore.SourceVaultClosureStateReady {
			return ErrStaleAuthority
		}
		existing, err := tx.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
		if err != nil {
			return err
		}
		for _, value := range existing {
			if value.ApprovalKind != "delivery" {
				continue
			}
			if !value.AuthorityRevisionRowID.Valid || value.AuthorityRevisionRowID.Int64 != authority.ID || value.SourceClosureRowID != revision.SourceClosureRowID {
				return ErrStaleAuthority
			}
			return fmt.Errorf("delivery ticket revision is already approved")
		}
		approval, err = tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID: s.ids.ApprovalID(), RevisionRowID: revision.ID, ApprovalKind: "delivery",
			ApprovalState: "approved", Rationale: input.Rationale, SourceClosureRowID: revision.SourceClosureRowID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: authority.ID, Valid: true},
		})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.DeliveryTicketRevisionApproval{}, fmt.Errorf("%w: ticket revision or authority was not found", ErrStaleAuthority)
	}
	return approval, err
}
