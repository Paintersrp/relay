package tickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"relay/internal/planningartifacts"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidTicketDesignBrief       = errors.New("invalid ticket design brief")
	ErrTicketDesignBriefNotFound      = errors.New("ticket design brief not found")
	ErrTicketDesignBriefConflict      = errors.New("ticket design brief conflict")
	ErrTicketDesignBriefApproval      = errors.New("ticket design brief approval is invalid")
	ErrTicketDesignBriefBytesMismatch = errors.New("ticket design brief bytes or digest mismatch")
	ErrTicketDesignBriefReview        = errors.New("ticket design brief review is invalid")
	ErrBriefReviewIncomplete          = errors.New("ticket design brief review is not completed")
	ErrNoActiveSelection              = errors.New("no active delivery ticket selection")
)

// TicketDesignBriefAdmissionInput is the delivery-owner admission boundary for
// the durable Ticket Design Brief. Only the operator-authored Markdown and an
// identity are accepted; the active selection, canonical filename, and digest
// are resolved server-side.
type TicketDesignBriefAdmissionInput struct {
	WorkspaceID     string
	Bytes           []byte
	CreatedIdentity string
}

type TicketDesignBriefAdmissionResult struct {
	Brief     workflowstore.TicketDesignBrief
	Workspace workflowstore.FeatureWorkspace
	Filename  string
}

type TicketDesignBriefApprovalInput struct {
	WorkspaceID                  string
	ExpectedVersion              int64
	OperatorConfirmationEvidence string
	CreatedIdentity              string
}

type TicketDesignBriefApprovalResult struct {
	Brief    workflowstore.TicketDesignBrief
	Approval workflowstore.TicketDesignBriefApproval
}

type TicketDesignBriefReviewDisposition string

const (
	TicketDesignBriefReviewReadyForApproval TicketDesignBriefReviewDisposition = "ready_for_approval"
	TicketDesignBriefReviewNeedsRevision    TicketDesignBriefReviewDisposition = "needs_revision"
)

// CompleteBriefReviewInput records the bounded disposition of the read-only
// auditor review of the current brief. No findings or prose are accepted or
// persisted.
type CompleteBriefReviewInput struct {
	WorkspaceID      string
	ReviewerIdentity string
	Disposition      TicketDesignBriefReviewDisposition
}

type TicketDesignBriefReviewResult struct {
	Brief  workflowstore.TicketDesignBrief
	Review workflowstore.TicketDesignBriefReview
}

// ApproveCurrentBriefInput carries only workspace-level guided inputs for the
// explicit approval mutation; the current brief identity, exact bytes, and
// basis are resolved server-side by the delivery owner.
type ApproveCurrentBriefInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	Evidence        string
}

// WorkspaceBriefState is the delivery-owner semantic read of the durable
// Ticket Design Brief for the workspace's current selection:
// none | authored | reviewed | approved. Consumers must not reconstruct brief
// state from ticket_design_briefs rows. ReviewDisposition preserves the narrow
// authoritative disposition of the completed read-only review, so consumers
// can distinguish ready_for_approval from needs_revision without treating a
// completed review as approval.
type WorkspaceBriefState struct {
	State             string
	ReviewDisposition string
	TicketID          string
	RevisionNumber    int64
}

// WorkspaceBriefIntegrity is the durable, read-only lineage for Ticket Design
// Briefs in a workspace. It is separate from WorkspaceBriefState because it
// retains historical selection bindings for inspection rather than using them
// as lifecycle authority.
type WorkspaceBriefIntegrity struct {
	Briefs      []TicketDesignBriefIntegrity
	Diagnostics []TicketDesignBriefIntegrityDiagnostic
}

type TicketDesignBriefIntegrity struct {
	BriefID, SelectionID, SelectionState, TicketID               string
	RevisionNumber                                               int64
	Filename, SHA256                                             string
	SizeBytes                                                    int64
	Status, ReviewState, ReviewDisposition, ReviewID, ApprovalID string
	Historical                                                   bool
}

// TicketDesignBriefIntegrityDiagnostic describes a partial inspection failure
// without pretending that a missing or unverifiable value is an exact
// identity. Conditions are intentionally stable and contain no storage error
// text.
type TicketDesignBriefIntegrityDiagnostic struct {
	BriefID   string
	Condition string // unreadable | inconsistent | unverifiable
}

// AdmitTicketDesignBrief durably records the authored Ticket Design Brief for
// the current active selection. Admission validates the exact authored bytes
// against the planning-artifact contract and binds the brief to the selected
// Ticket revision; it never creates approval or package state.
func (s *Service) AdmitTicketDesignBrief(ctx context.Context, input TicketDesignBriefAdmissionInput) (TicketDesignBriefAdmissionResult, error) {
	if !nonBlank(input.WorkspaceID) || len(input.Bytes) == 0 || !nonBlank(input.CreatedIdentity) {
		return TicketDesignBriefAdmissionResult{}, ErrInvalidTicketDesignBrief
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
	if err != nil {
		return TicketDesignBriefAdmissionResult{}, err
	}
	_, revision, ticket, err := s.currentActiveSelectionBasis(ctx, s.store, workspace.ID)
	if err != nil {
		return TicketDesignBriefAdmissionResult{}, err
	}
	filename := ticketDesignBriefFilename(workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	if diagnostics := planningartifacts.Validate(speccompiler.ArtifactTicketDesignBrief, input.Bytes); len(diagnostics) != 0 {
		return TicketDesignBriefAdmissionResult{}, fmt.Errorf("%w: brief for %s is not admissible: %v", ErrInvalidTicketDesignBrief, ticket.TicketID, diagnostics)
	}
	artifactID := workflowstore.NewFeatureWorkspaceDiscoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return TicketDesignBriefAdmissionResult{}, err
	}
	file, err := batch.Stage("ticket_design_brief", filename, "text/markdown", input.Bytes)
	if err != nil {
		_ = batch.Rollback()
		return TicketDesignBriefAdmissionResult{}, err
	}
	result := TicketDesignBriefAdmissionResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if err != nil {
			return err
		}
		selection, revision, _, err := s.currentActiveSelectionBasis(ctx, tx, workspace.ID)
		if err != nil {
			return err
		}
		selection, err = s.replaceNeedsRevisionBriefSelection(ctx, tx, workspace, selection, revision)
		if err != nil {
			return err
		}
		artifact, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: artifactID, WorkspaceRowID: workspace.ID, RelativePath: file.RelativePath,
			Sha256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes,
		})
		if err != nil {
			return err
		}
		brief, err := tx.CreateTicketDesignBrief(ctx, workflowstore.CreateTicketDesignBriefParams{
			BriefID: workflowstore.NewTicketDesignBriefID(), WorkspaceRowID: workspace.ID, SelectionRowID: selection.ID,
			RevisionRowID: revision.ID, Filename: filename, ArtifactRowID: artifact.ID,
			ArtifactSha256: file.SHA256, ArtifactSizeBytes: file.SizeBytes,
			CreatedIdentity: strings.TrimSpace(input.CreatedIdentity),
		})
		if err != nil {
			return briefConflictError(err)
		}
		result = TicketDesignBriefAdmissionResult{Brief: brief, Workspace: workspace, Filename: filename}
		return nil
	})
	return result, err
}

// replaceNeedsRevisionBriefSelection preserves an immutable rejected brief and
// its review by superseding the active selection, then creates a new active
// selection for the exact still-current ticket revision. This is the only
// replacement path: authored, ready, and approved briefs remain bound to their
// original immutable selection.
func (s *Service) replaceNeedsRevisionBriefSelection(
	ctx context.Context,
	tx *workflowstore.Tx,
	workspace workflowstore.FeatureWorkspace,
	selection workflowstore.DeliveryTicketSelection,
	revision workflowstore.DeliveryTicketRevision,
) (workflowstore.DeliveryTicketSelection, error) {
	brief, err := tx.GetTicketDesignBriefBySelectionRowID(ctx, selection.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return selection, nil
	}
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, err
	}
	if brief.RevisionRowID != revision.ID {
		return workflowstore.DeliveryTicketSelection{}, ErrTicketDesignBriefBytesMismatch
	}
	if _, err := tx.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); err == nil {
		return workflowstore.DeliveryTicketSelection{}, fmt.Errorf("%w: the current brief is already approved", ErrTicketDesignBriefConflict)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return workflowstore.DeliveryTicketSelection{}, err
	}
	review, err := tx.GetTicketDesignBriefReviewByBriefRowID(ctx, brief.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workflowstore.DeliveryTicketSelection{}, fmt.Errorf("%w: an existing brief may only be replaced after needs_revision review", ErrTicketDesignBriefConflict)
		}
		return workflowstore.DeliveryTicketSelection{}, err
	}
	if review.Disposition != string(TicketDesignBriefReviewNeedsRevision) {
		return workflowstore.DeliveryTicketSelection{}, fmt.Errorf("%w: an existing brief may only be replaced after needs_revision review", ErrTicketDesignBriefConflict)
	}
	approvals, err := tx.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, err
	}
	approval, ok := currentDeliveryApproval(workspace, revision, approvals)
	if !ok {
		return workflowstore.DeliveryTicketSelection{}, ErrSelectionAuthorityStale
	}
	if _, err := tx.TransitionDeliveryTicketSelection(ctx, selection.SelectionID, "superseded"); err != nil {
		return workflowstore.DeliveryTicketSelection{}, selectionConflictError(err)
	}
	replacement, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{
		SelectionID: workflowstore.NewDeliveryTicketSelectionID(), WorkspaceRowID: workspace.ID,
		State: "active", Rationale: "replace Ticket Design Brief after needs_revision review",
		SourceClosureRowID: sql.NullInt64{Int64: revision.SourceClosureRowID, Valid: true},
	})
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, selectionConflictError(err)
	}
	if _, err := tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{
		SelectionRowID: replacement.ID, Sequence: 1, RevisionRowID: revision.ID, ApprovalRowID: approval.ID,
	}); err != nil {
		return workflowstore.DeliveryTicketSelection{}, err
	}
	return replacement, nil
}

// ApproveTicketDesignBrief is the explicit confirmed owner mutation that
// approves the current admissible brief. The brief identity, exact bytes, and
// source-backed basis are resolved server-side; only the operator confirmation
// evidence and identity are accepted from the caller.
func (s *Service) ApproveTicketDesignBrief(ctx context.Context, input TicketDesignBriefApprovalInput) (TicketDesignBriefApprovalResult, error) {
	evidence := strings.TrimSpace(input.OperatorConfirmationEvidence)
	if !nonBlank(input.WorkspaceID) || input.ExpectedVersion < 1 || evidence == "" || len(evidence) > 4096 || !nonBlank(input.CreatedIdentity) {
		return TicketDesignBriefApprovalResult{}, ErrTicketDesignBriefApproval
	}
	result := TicketDesignBriefApprovalResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrRevisionConflict
		}
		selection, revision, ticket, err := s.currentActiveSelectionBasis(ctx, tx, workspace.ID)
		if err != nil {
			return err
		}
		_ = ticket
		brief, err := tx.GetTicketDesignBriefBySelectionRowID(ctx, selection.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: the active selection has no authored brief", ErrTicketDesignBriefNotFound)
		}
		if err != nil {
			return err
		}
		if brief.RevisionRowID != revision.ID {
			return fmt.Errorf("%w: brief is not bound to the current selected revision", ErrTicketDesignBriefBytesMismatch)
		}
		stored, err := tx.ReadTicketDesignBriefBytes(ctx, brief.BriefID, 1<<20)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTicketDesignBriefBytesMismatch, err)
		}
		if digestCandidate(stored) != brief.ArtifactSha256 || int64(len(stored)) != brief.ArtifactSizeBytes {
			return ErrTicketDesignBriefBytesMismatch
		}
		if _, err := tx.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); err == nil {
			return fmt.Errorf("%w: the current brief is already approved", ErrTicketDesignBriefConflict)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Approval is an explicit confirmed owner mutation that follows a
		// ready read-only review; review remains separate from approval.
		review, err := tx.GetTicketDesignBriefReviewByBriefRowID(ctx, brief.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: the current brief has no completed review", ErrBriefReviewIncomplete)
		}
		if err != nil {
			return err
		}
		if review.Disposition != string(TicketDesignBriefReviewReadyForApproval) {
			return fmt.Errorf("%w: the current brief has no completed review", ErrBriefReviewIncomplete)
		}
		approval, err := tx.CreateTicketDesignBriefApproval(ctx, workflowstore.CreateTicketDesignBriefApprovalParams{
			ApprovalID: workflowstore.NewTicketDesignBriefApprovalID(), BriefRowID: brief.ID,
			BriefArtifactRowID: brief.ArtifactRowID, BriefSha256: brief.ArtifactSha256,
			BriefSizeBytes: brief.ArtifactSizeBytes, OperatorConfirmationEvidence: evidence,
			CreatedIdentity: strings.TrimSpace(input.CreatedIdentity),
		})
		if err != nil {
			return briefConflictError(err)
		}
		result = TicketDesignBriefApprovalResult{Brief: brief, Approval: approval}
		return nil
	})
	return result, err
}

// CompleteTicketDesignBriefReview records the narrow authoritative fact that
// the read-only auditor review handoff completed for the current brief. The
// brief identity is resolved server-side; only its bounded disposition is
// persisted, and this completion is a separate fact from approval.
func (s *Service) CompleteTicketDesignBriefReview(ctx context.Context, input CompleteBriefReviewInput) (TicketDesignBriefReviewResult, error) {
	if !nonBlank(input.WorkspaceID) || !nonBlank(input.ReviewerIdentity) ||
		(input.Disposition != TicketDesignBriefReviewReadyForApproval && input.Disposition != TicketDesignBriefReviewNeedsRevision) {
		return TicketDesignBriefReviewResult{}, ErrTicketDesignBriefReview
	}
	result := TicketDesignBriefReviewResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if err != nil {
			return err
		}
		selection, _, _, err := s.currentActiveSelectionBasis(ctx, tx, workspace.ID)
		if err != nil {
			return err
		}
		brief, err := tx.GetTicketDesignBriefBySelectionRowID(ctx, selection.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: the active selection has no authored brief", ErrTicketDesignBriefNotFound)
		}
		if err != nil {
			return err
		}
		if _, err := tx.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); err == nil {
			return fmt.Errorf("%w: the current brief is already approved", ErrTicketDesignBriefConflict)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.GetTicketDesignBriefReviewByBriefRowID(ctx, brief.ID); err == nil {
			return fmt.Errorf("%w: the current brief review is already completed", ErrTicketDesignBriefConflict)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		review, err := tx.CreateTicketDesignBriefReview(ctx, workflowstore.CreateTicketDesignBriefReviewParams{
			ReviewID: workflowstore.NewTicketDesignBriefReviewID(), BriefRowID: brief.ID,
			ReviewerIdentity: strings.TrimSpace(input.ReviewerIdentity), Disposition: string(input.Disposition),
		})
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTicketDesignBriefReview, err)
		}
		result = TicketDesignBriefReviewResult{Brief: brief, Review: review}
		return nil
	})
	return result, err
}

// ApproveCurrentTicketDesignBrief is the tickets-owner implementation of the
// guided approve action. It resolves the current brief for the active
// selection server-side, verifies the completed review, and records the
// explicit confirmed approval with the exact brief snapshot. No brief identity
// or digest is accepted from the guided boundary.
func (s *Service) ApproveCurrentTicketDesignBrief(ctx context.Context, input ApproveCurrentBriefInput) (TicketDesignBriefApprovalResult, error) {
	evidence := strings.TrimSpace(input.Evidence)
	if !nonBlank(input.WorkspaceID) || input.ExpectedVersion < 1 || evidence == "" || len(evidence) > 4096 {
		return TicketDesignBriefApprovalResult{}, ErrTicketDesignBriefApproval
	}
	return s.ApproveTicketDesignBrief(ctx, TicketDesignBriefApprovalInput{
		WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion,
		OperatorConfirmationEvidence: evidence, CreatedIdentity: "guided-operator",
	})
}

// ReadWorkspaceBriefState is the delivery-owner semantic read consumed by the
// guided journey projection. It resolves the workspace's latest selection and
// reports none | authored | reviewed | approved for the durable brief bound to
// it. A completed review also retains its bounded disposition so the guided
// journey can distinguish remediation from approval readiness.
func (s *Service) ReadWorkspaceBriefState(ctx context.Context, workspaceID string) (WorkspaceBriefState, error) {
	if !nonBlank(workspaceID) {
		return WorkspaceBriefState{}, ErrInvalidTicket
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return WorkspaceBriefState{}, err
	}
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return WorkspaceBriefState{}, err
	}
	result := WorkspaceBriefState{State: "none"}
	if len(selections) == 0 {
		return result, nil
	}
	latest := selections[0]
	for _, selection := range selections {
		if selection.ID > latest.ID {
			latest = selection
		}
	}
	if latest.State != "active" {
		return result, nil
	}
	members, err := s.store.ListDeliveryTicketSelectionMembers(ctx, latest.ID)
	if err != nil {
		return WorkspaceBriefState{}, err
	}
	if len(members) > 0 {
		revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, members[0].RevisionRowID)
		if err != nil {
			return WorkspaceBriefState{}, err
		}
		ticket, err := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil {
			return WorkspaceBriefState{}, err
		}
		result.TicketID = ticket.TicketID
		result.RevisionNumber = revision.RevisionNumber
	}
	brief, err := s.store.GetTicketDesignBriefBySelectionRowID(ctx, latest.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return WorkspaceBriefState{}, err
	}
	result.State = "authored"
	if review, err := s.store.GetTicketDesignBriefReviewByBriefRowID(ctx, brief.ID); err == nil {
		result.State = "reviewed"
		result.ReviewDisposition = review.Disposition
	} else if !errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBriefState{}, err
	}
	if _, err := s.store.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); err == nil {
		result.State = "approved"
	} else if !errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBriefState{}, err
	}
	return result, nil
}

// ReadWorkspaceBriefIntegrity returns every durable Ticket Design Brief and
// its ticket/selection lineage. It verifies each artifact before exposing its
// digest and size; absent review or approval records are normal lifecycle
// states, while broken bindings are reported as typed diagnostics.
func (s *Service) ReadWorkspaceBriefIntegrity(ctx context.Context, workspaceID string) (WorkspaceBriefIntegrity, error) {
	if !nonBlank(workspaceID) {
		return WorkspaceBriefIntegrity{}, ErrInvalidTicket
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return WorkspaceBriefIntegrity{}, err
	}
	briefs, err := s.store.ListTicketDesignBriefsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return WorkspaceBriefIntegrity{}, err
	}
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return WorkspaceBriefIntegrity{}, err
	}
	selectionByRowID := make(map[int64]workflowstore.DeliveryTicketSelection, len(selections))
	for _, selection := range selections {
		selectionByRowID[selection.ID] = selection
	}
	result := WorkspaceBriefIntegrity{Briefs: make([]TicketDesignBriefIntegrity, 0, len(briefs))}
	for _, brief := range briefs {
		entry := TicketDesignBriefIntegrity{BriefID: brief.BriefID, Status: "authored", ReviewState: "none"}
		selection, ok := selectionByRowID[brief.SelectionRowID]
		if !ok {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "inconsistent"})
			result.Briefs = append(result.Briefs, entry)
			continue
		}
		entry.SelectionID, entry.SelectionState = selection.SelectionID, selection.State
		entry.Historical = selection.State != "active"
		members, membersErr := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
		if membersErr != nil {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: briefIntegrityReadCondition(membersErr)})
		} else if len(members) != 1 || members[0].RevisionRowID != brief.RevisionRowID {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "inconsistent"})
		} else if revision, revisionErr := s.store.GetDeliveryTicketRevisionByRowID(ctx, brief.RevisionRowID); revisionErr != nil {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: briefIntegrityReadCondition(revisionErr)})
		} else if ticket, ticketErr := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID); ticketErr != nil {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: briefIntegrityReadCondition(ticketErr)})
		} else {
			entry.TicketID, entry.RevisionNumber = ticket.TicketID, revision.RevisionNumber
			if brief.Filename != ticketDesignBriefFilename(workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber) {
				result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "inconsistent"})
			} else {
				entry.Filename = brief.Filename
			}
		}
		if bytes, bytesErr := s.store.ReadTicketDesignBriefBytes(ctx, brief.BriefID, 1<<20); bytesErr != nil || int64(len(bytes)) != brief.ArtifactSizeBytes {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "unverifiable"})
		} else {
			entry.SHA256, entry.SizeBytes = brief.ArtifactSha256, brief.ArtifactSizeBytes
		}
		if review, reviewErr := s.store.GetTicketDesignBriefReviewByBriefRowID(ctx, brief.ID); reviewErr == nil {
			entry.ReviewState, entry.ReviewDisposition, entry.ReviewID = "completed", review.Disposition, review.ReviewID
			if review.Disposition == string(TicketDesignBriefReviewReadyForApproval) {
				entry.Status = "reviewed"
			}
		} else if !errors.Is(reviewErr, sql.ErrNoRows) {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "unreadable"})
		}
		if approval, approvalErr := s.store.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); approvalErr == nil {
			entry.Status, entry.ApprovalID = "approved", approval.ApprovalID
			if entry.ReviewState != "completed" || entry.ReviewDisposition != string(TicketDesignBriefReviewReadyForApproval) || approval.BriefArtifactRowID != brief.ArtifactRowID || approval.BriefSha256 != brief.ArtifactSha256 || approval.BriefSizeBytes != brief.ArtifactSizeBytes {
				entry.ApprovalID = ""
				entry.Status = "authored"
				result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "inconsistent"})
			}
		} else if !errors.Is(approvalErr, sql.ErrNoRows) {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "unreadable"})
		}
		result.Briefs = append(result.Briefs, entry)
	}
	return result, nil
}

func briefIntegrityReadCondition(err error) string {
	if errors.Is(err, sql.ErrNoRows) {
		return "inconsistent"
	}
	return "unreadable"
}

// currentActiveSelectionBasis resolves the workspace's current active
// selection and verifies its source-backed basis: the selected revision is the
// ticket's current revision bound to the workspace's current authority and a
// ready source closure. The reader surface accepts both Store and Tx.
func (s *Service) currentActiveSelectionBasis(ctx context.Context, reader briefBasisReader, workspaceRowID int64) (workflowstore.DeliveryTicketSelection, workflowstore.DeliveryTicketRevision, workflowstore.DeliveryTicket, error) {
	selections, err := reader.ListDeliveryTicketSelectionsByWorkspace(ctx, workspaceRowID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	var selection workflowstore.DeliveryTicketSelection
	found := false
	for _, candidate := range selections {
		if candidate.State != "active" {
			continue
		}
		if found {
			return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionConflict
		}
		selection, found = candidate, true
	}
	if !found {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrNoActiveSelection
	}
	workspace, err := reader.GetFeatureWorkspaceByRowID(ctx, selection.WorkspaceRowID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	if !selection.SourceClosureRowID.Valid {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionAuthorityStale
	}
	closure, err := reader.GetSourceVaultClosureByRowID(ctx, selection.SourceClosureRowID.Int64)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionSourceStale
	}
	members, err := reader.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	if len(members) != 1 {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrInvalidSelection
	}
	revision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, members[0].RevisionRowID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	ticket, err := reader.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	if !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionMemberStale
	}
	if revision.SourceClosureRowID != selection.SourceClosureRowID.Int64 {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionSourceStale
	}
	if workspace.CurrentAuthorityRevisionRowID.Valid {
		authority, authorityErr := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if authorityErr != nil {
			return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, authorityErr
		}
		if authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != selection.SourceClosureRowID.Int64 {
			return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionAuthorityStale
		}
	} else {
		currentClosure, currentErr := reader.GetReadySourceVaultClosureByRepositoryTargetAndCommit(ctx, revision.RepoTarget, revision.BaseCommit)
		if currentErr != nil || currentClosure.ID != selection.SourceClosureRowID.Int64 {
			return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionSourceStale
		}
	}
	approvals, err := reader.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, err
	}
	if _, ok := currentDeliveryApproval(workspace, revision, approvals); !ok {
		return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketRevision{}, workflowstore.DeliveryTicket{}, ErrSelectionAuthorityStale
	}
	return selection, revision, ticket, nil
}

func ticketDesignBriefFilename(featureSlug, ticketID string, revisionNumber int64) string {
	return fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", featureSlug, ticketID, revisionNumber)
}

func briefConflictError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "ticket_design_briefs.selection_row_id") || strings.Contains(message, "unique constraint failed") {
		return fmt.Errorf("%w: %v", ErrTicketDesignBriefConflict, err)
	}
	return err
}

// briefBasisReader is the narrow read surface the brief owner requires; Store
// and Tx both implement it.
type briefBasisReader interface {
	ListDeliveryTicketSelectionsByWorkspace(context.Context, int64) ([]workflowstore.DeliveryTicketSelection, error)
	ListDeliveryTicketSelectionMembers(context.Context, int64) ([]workflowstore.DeliveryTicketSelectionMember, error)
	GetFeatureWorkspaceByRowID(context.Context, int64) (workflowstore.FeatureWorkspace, error)
	GetFeatureWorkspaceAuthorityRevisionByRowID(context.Context, int64) (workflowstore.FeatureWorkspaceAuthorityRevision, error)
	GetSourceVaultClosureByRowID(context.Context, int64) (workflowstore.SourceVaultClosure, error)
	GetReadySourceVaultClosureByRepositoryTargetAndCommit(context.Context, string, string) (workflowstore.SourceVaultClosure, error)
	GetDeliveryTicketRevisionByRowID(context.Context, int64) (workflowstore.DeliveryTicketRevision, error)
	GetDeliveryTicketByRowID(context.Context, int64) (workflowstore.DeliveryTicket, error)
	ListDeliveryTicketRevisionApprovals(context.Context, int64) ([]workflowstore.DeliveryTicketRevisionApproval, error)
}
