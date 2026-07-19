package tickets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/app/approvals"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidTicket        = errors.New("invalid delivery ticket")
	ErrTicketNotFound       = errors.New("delivery ticket not found")
	ErrRevisionConflict     = errors.New("delivery ticket revision conflict")
	ErrDependencyNotCurrent = errors.New("delivery ticket dependency is not current")
	ErrRemediationSeed      = errors.New("delivery ticket remediation seed is invalid")
)

type Service struct {
	store     *workflowstore.Store
	approvals *approvals.Service
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, ErrInvalidTicket
	}
	approvalService, err := approvals.NewService(store)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, approvals: approvalService}, nil
}

type RevisionMemberInput struct {
	Kind string
	Path string
	Text string
}

type DependencyInput struct {
	RevisionRowID int64
	Outcome       string
}

type RevisionInput struct {
	RepoTarget              string
	Branch                  string
	BaseCommit              string
	SourceClosureRowID      int64
	SourcePath              string
	Goal                    string
	Context                 string
	TransitionApplicability string
	CancellationReason      string
	CanonicalJSON           []byte
	RenderedMarkdown        []byte
	Members                 []RevisionMemberInput
	Dependencies            []DependencyInput
}

type PublishInput struct {
	WorkspaceID            string
	TicketID               string
	ExternalPriority       int64
	ExpectedRevisionNumber int64
	RemediationSeedID      string
	Revision               RevisionInput
}

type StoredArtifact struct {
	RelativePath string
	SHA256       string
	SizeBytes    int64
}

type PublishedRevision struct {
	Ticket               workflowstore.DeliveryTicket
	Revision             workflowstore.DeliveryTicketRevision
	Canonical            StoredArtifact
	Rendered             StoredArtifact
	RemediationReopening *workflowstore.AuditRemediationSeedReopening
}

func (s *Service) Publish(ctx context.Context, input PublishInput) (PublishedRevision, error) {
	if !validPublish(input) {
		return PublishedRevision{}, ErrInvalidTicket
	}
	batch, err := s.store.ArtifactStore().Begin(ticketRevisionNamespace(input.TicketID, input.ExpectedRevisionNumber+1))
	if err != nil {
		return PublishedRevision{}, err
	}
	canonical, err := batch.Stage("delivery_ticket_canonical", "delivery-ticket.json", "application/json", input.Revision.CanonicalJSON)
	if err != nil {
		_ = batch.Rollback()
		return PublishedRevision{}, err
	}
	rendered, err := batch.Stage("delivery_ticket_rendered", "delivery-ticket.md", "text/markdown", input.Revision.RenderedMarkdown)
	if err != nil {
		_ = batch.Rollback()
		return PublishedRevision{}, err
	}

	result := PublishedRevision{Canonical: storedArtifact(canonical.RelativePath, canonical.SHA256, canonical.SizeBytes), Rendered: storedArtifact(rendered.RelativePath, rendered.SHA256, rendered.SizeBytes)}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		ticket, err := tx.GetDeliveryTicketByTicketID(ctx, input.TicketID)
		if errors.Is(err, sql.ErrNoRows) {
			if input.ExpectedRevisionNumber != 0 {
				return ErrRevisionConflict
			}
			ticket, err = tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: input.TicketID, WorkspaceRowID: workspace.ID, ExternalPriority: input.ExternalPriority})
		}
		if err != nil {
			return err
		}
		if ticket.WorkspaceRowID != workspace.ID {
			return ErrInvalidTicket
		}
		priorRevisionID := sql.NullInt64{}
		nextRevision := int64(1)
		if ticket.CurrentRevisionRowID.Valid {
			prior, err := tx.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
			if err != nil {
				return err
			}
			if input.ExpectedRevisionNumber != prior.RevisionNumber {
				return ErrRevisionConflict
			}
			nextRevision = prior.RevisionNumber + 1
			priorRevisionID = sql.NullInt64{Int64: prior.ID, Valid: true}
		}
		if input.ExpectedRevisionNumber != nextRevision-1 {
			return ErrRevisionConflict
		}
		closure, err := tx.GetSourceVaultClosureByRowID(ctx, input.Revision.SourceClosureRowID)
		if err != nil {
			return err
		}
		if closure.State != workflowstore.SourceVaultClosureStateReady {
			return ErrInvalidTicket
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: nextRevision, ReplacesRevisionRowID: priorRevisionID,
			CancellationReason: nullableString(input.Revision.CancellationReason), RepoTarget: input.Revision.RepoTarget,
			Branch: input.Revision.Branch, BaseCommit: input.Revision.BaseCommit, SourceClosureRowID: input.Revision.SourceClosureRowID,
			SourcePath: input.Revision.SourcePath, Goal: input.Revision.Goal, Context: input.Revision.Context,
			TransitionApplicability: input.Revision.TransitionApplicability,
		})
		if err != nil {
			return err
		}
		for index, member := range input.Revision.Members {
			if _, err := tx.CreateDeliveryTicketRevisionMember(ctx, workflowstore.CreateDeliveryTicketRevisionMemberParams{
				RevisionRowID: revision.ID, Sequence: int64(index + 1), MemberKind: member.Kind,
				MemberPath: nullableString(member.Path), MemberText: member.Text,
			}); err != nil {
				return err
			}
		}
		for index, dependency := range input.Revision.Dependencies {
			dependencyRevision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, dependency.RevisionRowID)
			if err != nil {
				return err
			}
			dependencyTicket, err := tx.GetDeliveryTicketByRowID(ctx, dependencyRevision.DeliveryTicketRowID)
			if err != nil {
				return err
			}
			if dependencyTicket.WorkspaceRowID != workspace.ID || !dependencyTicket.CurrentRevisionRowID.Valid || dependencyTicket.CurrentRevisionRowID.Int64 != dependencyRevision.ID {
				return ErrDependencyNotCurrent
			}
			if _, err := tx.CreateDeliveryTicketRevisionDependency(ctx, workflowstore.CreateDeliveryTicketRevisionDependencyParams{
				RevisionRowID: revision.ID, Sequence: int64(index + 1), DependsOnRevisionRowID: dependencyRevision.ID, Outcome: dependency.Outcome,
			}); err != nil {
				return err
			}
		}
		ticket, err = tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID)
		if err != nil {
			return err
		}
		if err := reopenCurrentFeatureCompletionForTicket(ctx, tx, ticket, revision); err != nil {
			return err
		}
		if input.RemediationSeedID != "" {
			reopening, err := linkRemediationSeed(ctx, tx, input.RemediationSeedID, ticket, revision)
			if err != nil {
				return err
			}
			result.RemediationReopening = &reopening
		}
		result.Ticket, result.Revision = ticket, revision
		return nil
	})
	return result, err
}

func (s *Service) UpdateExternalPriority(ctx context.Context, ticketID string, externalPriority int64) (workflowstore.DeliveryTicket, error) {
	if strings.TrimSpace(ticketID) != ticketID || ticketID == "" || externalPriority < 0 {
		return workflowstore.DeliveryTicket{}, ErrInvalidTicket
	}
	var ticket workflowstore.DeliveryTicket
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.UpdateDeliveryTicketExternalPriority(ctx, ticketID, externalPriority)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.DeliveryTicket{}, ErrTicketNotFound
	}
	return ticket, err
}

type ApproveInput struct {
	TicketID            string
	RevisionRowID       int64
	AuthorityRevisionID string
	Rationale           string
}

func (s *Service) Approve(ctx context.Context, input ApproveInput) (workflowstore.DeliveryTicketRevisionApproval, error) {
	return s.approvals.ApproveDeliveryTicket(ctx, approvals.ApproveDeliveryTicketInput{
		TicketID: input.TicketID, RevisionRowID: input.RevisionRowID, AuthorityRevisionID: input.AuthorityRevisionID, Rationale: input.Rationale,
	})
}

type Readiness struct {
	Ready     bool
	Selected  bool
	Completed bool
	Reasons   []string
}

type TicketDetail struct {
	Ticket       workflowstore.DeliveryTicket
	Revision     workflowstore.DeliveryTicketRevision
	Members      []workflowstore.DeliveryTicketRevisionMember
	Dependencies []workflowstore.DeliveryTicketRevisionDependency
	Approvals    []workflowstore.DeliveryTicketRevisionApproval
	Canonical    StoredArtifact
	Rendered     StoredArtifact
	Readiness    Readiness
}

func (s *Service) Read(ctx context.Context, ticketID string) (TicketDetail, error) {
	ticket, err := s.store.GetDeliveryTicketByTicketID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return TicketDetail{}, ErrTicketNotFound
	}
	if err != nil {
		return TicketDetail{}, err
	}
	if !ticket.CurrentRevisionRowID.Valid {
		return TicketDetail{Ticket: ticket, Readiness: Readiness{Reasons: []string{"no_current_revision"}}}, nil
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
	if err != nil {
		return TicketDetail{}, err
	}
	detail := TicketDetail{Ticket: ticket, Revision: revision}
	if detail.Members, err = s.store.ListDeliveryTicketRevisionMembers(ctx, revision.ID); err != nil {
		return TicketDetail{}, err
	}
	if detail.Dependencies, err = s.store.ListDeliveryTicketRevisionDependencies(ctx, revision.ID); err != nil {
		return TicketDetail{}, err
	}
	if detail.Approvals, err = s.store.ListDeliveryTicketRevisionApprovals(ctx, revision.ID); err != nil {
		return TicketDetail{}, err
	}
	if detail.Canonical, err = s.readArtifact(ticket.TicketID, revision.RevisionNumber, "delivery-ticket.json"); err != nil {
		return TicketDetail{}, err
	}
	if detail.Rendered, err = s.readArtifact(ticket.TicketID, revision.RevisionNumber, "delivery-ticket.md"); err != nil {
		return TicketDetail{}, err
	}
	detail.Readiness, err = s.deriveReadiness(ctx, detail)
	return detail, err
}

func (s *Service) deriveReadiness(ctx context.Context, detail TicketDetail) (Readiness, error) {
	return deriveTicketReadiness(ctx, s.store, detail)
}

func deriveTicketReadiness(ctx context.Context, reader ticketReadStore, detail TicketDetail) (Readiness, error) {
	reasons := make([]string, 0)
	if detail.Revision.CancellationReason.Valid {
		reasons = append(reasons, "cancelled")
	}
	closure, err := reader.GetSourceVaultClosureByRowID(ctx, detail.Revision.SourceClosureRowID)
	if err != nil {
		return Readiness{}, err
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady {
		reasons = append(reasons, "source_not_current")
	}
	workspace, err := reader.GetFeatureWorkspaceByRowID(ctx, detail.Ticket.WorkspaceRowID)
	if err != nil {
		return Readiness{}, err
	}
	if !workspace.CurrentAuthorityRevisionRowID.Valid {
		reasons = append(reasons, "authority_missing")
	} else {
		authority, err := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil {
			return Readiness{}, err
		}
		if authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != detail.Revision.SourceClosureRowID {
			reasons = append(reasons, "authority_stale")
		}
	}

	approved := map[int64]struct{}{}
	for _, approval := range detail.Approvals {
		if approval.ApprovalKind == "delivery" && approval.ApprovalState == "approved" &&
			approval.SourceClosureRowID == detail.Revision.SourceClosureRowID &&
			workspace.CurrentAuthorityRevisionRowID.Valid && approval.AuthorityRevisionRowID.Valid &&
			approval.AuthorityRevisionRowID.Int64 == workspace.CurrentAuthorityRevisionRowID.Int64 {
			approved[approval.ID] = struct{}{}
		}
	}
	if len(approved) == 0 {
		reasons = append(reasons, "approval_missing_or_stale")
	}
	for _, dependency := range detail.Dependencies {
		if dependency.Outcome != "satisfied" {
			reasons = append(reasons, "dependency_outcome_not_satisfied")
			continue
		}
		dependencyRevision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, dependency.DependsOnRevisionRowID)
		if err != nil {
			return Readiness{}, err
		}
		dependencyTicket, err := reader.GetDeliveryTicketByRowID(ctx, dependencyRevision.DeliveryTicketRowID)
		if err != nil {
			return Readiness{}, err
		}
		if !dependencyTicket.CurrentRevisionRowID.Valid || dependencyTicket.CurrentRevisionRowID.Int64 != dependencyRevision.ID {
			reasons = append(reasons, "dependency_revision_stale")
			continue
		}
		if _, err := reader.GetDeliveryTicketRevisionSatisfaction(ctx, dependencyRevision.ID); errors.Is(err, sql.ErrNoRows) {
			reasons = append(reasons, "dependency_outcome_incomplete")
		} else if err != nil {
			return Readiness{}, err
		}
	}
	completed := false
	if _, err := reader.GetDeliveryTicketRevisionSatisfaction(ctx, detail.Revision.ID); err == nil {
		completed = true
		reasons = append(reasons, "completed")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Readiness{}, err
	}

	selected := false
	selections, err := reader.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return Readiness{}, err
	}
	for _, selection := range selections {
		if selection.State != "active" {
			continue
		}
		members, err := reader.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
		if err != nil {
			return Readiness{}, err
		}
		for _, member := range members {
			if member.RevisionRowID == detail.Revision.ID {
				_, selected = approved[member.ApprovalRowID]
				if selected {
					break
				}
			}
		}
		if selected {
			break
		}
	}
	return Readiness{Ready: len(reasons) == 0, Selected: selected, Completed: completed, Reasons: reasons}, nil
}

func (s *Service) readArtifact(ticketID string, revisionNumber int64, filename string) (StoredArtifact, error) {
	relativePath := filepath.ToSlash(filepath.Join(ticketRevisionNamespace(ticketID, revisionNumber), filename))
	data, err := os.ReadFile(filepath.Join(s.store.ArtifactStore().Root(), filepath.FromSlash(relativePath)))
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("read delivery ticket artifact: %w", err)
	}
	digest := sha256.Sum256(data)
	return storedArtifact(relativePath, hex.EncodeToString(digest[:]), int64(len(data))), nil
}

func storedArtifact(relativePath, sha256 string, sizeBytes int64) StoredArtifact {
	return StoredArtifact{RelativePath: relativePath, SHA256: sha256, SizeBytes: sizeBytes}
}

func ticketRevisionNamespace(ticketID string, revisionNumber int64) string {
	return fmt.Sprintf("delivery-tickets/%s/revisions/%d", ticketID, revisionNumber)
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func validPublish(input PublishInput) bool {
	if strings.TrimSpace(input.WorkspaceID) != input.WorkspaceID || input.WorkspaceID == "" ||
		strings.TrimSpace(input.TicketID) != input.TicketID || input.TicketID == "" || input.ExternalPriority < 0 || input.ExpectedRevisionNumber < 0 ||
		len(input.Revision.CanonicalJSON) == 0 || len(input.Revision.RenderedMarkdown) == 0 ||
		!nonBlank(input.Revision.RepoTarget) || !nonBlank(input.Revision.Branch) || !nonBlank(input.Revision.BaseCommit) ||
		input.Revision.SourceClosureRowID < 1 || !nonBlank(input.Revision.SourcePath) || !nonBlank(input.Revision.Goal) || !nonBlank(input.Revision.Context) ||
		(input.Revision.TransitionApplicability != "required" && input.Revision.TransitionApplicability != "not_required") ||
		(strings.TrimSpace(input.RemediationSeedID) != input.RemediationSeedID) ||
		(strings.TrimSpace(input.Revision.CancellationReason) != input.Revision.CancellationReason) {
		return false
	}
	for _, member := range input.Revision.Members {
		if !nonBlank(member.Kind) || strings.TrimSpace(member.Path) != member.Path || !nonBlank(member.Text) {
			return false
		}
	}
	for _, dependency := range input.Revision.Dependencies {
		if dependency.RevisionRowID < 1 || (dependency.Outcome != "satisfied" && dependency.Outcome != "blocked" && dependency.Outcome != "not_applicable") {
			return false
		}
	}
	return true
}

func nonBlank(value string) bool { return strings.TrimSpace(value) == value && value != "" }

func reopenCurrentFeatureCompletionForTicket(
	ctx context.Context,
	tx *workflowstore.Tx,
	ticket workflowstore.DeliveryTicket,
	revision workflowstore.DeliveryTicketRevision,
) error {
	completion, err := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, ticket.WorkspaceRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.CreateFeatureWorkspaceCompletionReopening(ctx, workflowstore.CreateFeatureWorkspaceCompletionReopeningParams{
		CompletionDecisionRowID:      completion.ID,
		ReopeningKind:                "ticket_revision",
		ReopeningTicketRevisionRowID: sql.NullInt64{Int64: revision.ID, Valid: true},
	})
	return err
}

func linkRemediationSeed(
	ctx context.Context,
	tx *workflowstore.Tx,
	seedID string,
	ticket workflowstore.DeliveryTicket,
	revision workflowstore.DeliveryTicketRevision,
) (workflowstore.AuditRemediationSeedReopening, error) {
	seed, err := tx.GetAuditRemediationSeedBySeedID(ctx, seedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workflowstore.AuditRemediationSeedReopening{}, ErrRemediationSeed
		}
		return workflowstore.AuditRemediationSeedReopening{}, err
	}
	if _, err := tx.GetAuditRemediationSeedReopening(ctx, seed.ID); err == nil {
		return workflowstore.AuditRemediationSeedReopening{}, ErrRemediationSeed
	} else if !errors.Is(err, sql.ErrNoRows) {
		return workflowstore.AuditRemediationSeedReopening{}, err
	}
	revisionDecision, err := tx.GetAuditTicketRevisionDecisionByRowID(ctx, seed.AuditTicketRevisionDecisionRowID)
	if err != nil {
		return workflowstore.AuditRemediationSeedReopening{}, err
	}
	obligation, err := tx.GetAuditPacketTicketObligationByRowID(ctx, revisionDecision.AuditPacketTicketObligationRowID)
	if err != nil || ticket.WorkspaceRowID == 0 {
		return workflowstore.AuditRemediationSeedReopening{}, ErrRemediationSeed
	}
	auditedTicket, err := tx.GetDeliveryTicketByRowID(ctx, obligation.DeliveryTicketRowID)
	if err != nil || auditedTicket.WorkspaceRowID != ticket.WorkspaceRowID {
		return workflowstore.AuditRemediationSeedReopening{}, ErrRemediationSeed
	}
	kind := "remediation_ticket"
	if auditedTicket.ID == ticket.ID {
		kind = "replacement_ticket_revision"
		if !revision.ReplacesRevisionRowID.Valid || revision.ReplacesRevisionRowID.Int64 != obligation.DeliveryTicketRevisionRowID {
			return workflowstore.AuditRemediationSeedReopening{}, ErrRemediationSeed
		}
	}
	return tx.CreateAuditRemediationSeedReopening(ctx, workflowstore.CreateAuditRemediationSeedReopeningParams{
		RemediationSeedRowID: seed.ID, ReopeningRevisionRowID: revision.ID, ReopeningKind: kind,
	})
}
