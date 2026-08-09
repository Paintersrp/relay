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

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

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
// auditor review of the current brief. ReviewedBytes identifies the exact
// bytes the auditor reviewed; the owner recalculates their SHA-256 and
// requires them to match the verified current admissible brief bytes and
// digest before either disposition is accepted. No findings or prose are
// accepted or persisted.
type CompleteBriefReviewInput struct {
	WorkspaceID      string
	ReviewerIdentity string
	Disposition      TicketDesignBriefReviewDisposition
	ReviewedBytes    []byte
}

type TicketDesignBriefReviewResult struct {
	Brief       workflowstore.TicketDesignBrief
	Disposition TicketDesignBriefReviewDisposition
	Review      TicketDesignBriefReviewCompletion
	Refresh     *TicketDesignBriefReviewRefresh
}

type TicketDesignBriefReviewCompletion struct{ ReviewerIdentity, Disposition string }

// TicketDesignBriefReviewRefresh carries only the immediate exact inputs for
// the ordinary planner.ticket_design_brief refresh after needs_revision.
type TicketDesignBriefReviewRefresh struct {
	OperationID, AuditorReviewResult string
	ReviewedBrief                    []byte
}

type briefReviewContinuation struct {
	workspaceID, briefID, sha256  string
	sizeBytes                     int64
	selectionRowID, revisionRowID int64
	bytes                         []byte
}

// setReviewContinuation records the private exact ready-review continuation
// for the workspace. It lives only in process memory and is never durable.
func (s *Service) setReviewContinuation(workspaceID string, continuation *briefReviewContinuation) {
	s.reviewMutex.Lock()
	defer s.reviewMutex.Unlock()
	s.reviewContinuations[workspaceID] = continuation
}

// takeReviewContinuation consumes the workspace's process-local continuation
// so the explicit approval mutation is single-use per workspace.
func (s *Service) takeReviewContinuation(workspaceID string) *briefReviewContinuation {
	s.reviewMutex.Lock()
	defer s.reviewMutex.Unlock()
	continuation := s.reviewContinuations[workspaceID]
	delete(s.reviewContinuations, workspaceID)
	return continuation
}

// restoreReviewContinuation returns a continuation after a failed approval
// unless a newer review has replaced it in the meantime.
func (s *Service) restoreReviewContinuation(workspaceID string, continuation *briefReviewContinuation) {
	if continuation == nil {
		return
	}
	s.reviewMutex.Lock()
	defer s.reviewMutex.Unlock()
	if _, ok := s.reviewContinuations[workspaceID]; !ok {
		s.reviewContinuations[workspaceID] = continuation
	}
}

// clearReviewContinuation invalidates any pending ready-review continuation
// for the workspace. needs_revision reviews and new brief admissions both
// clear it.
func (s *Service) clearReviewContinuation(workspaceID string) {
	s.reviewMutex.Lock()
	defer s.reviewMutex.Unlock()
	delete(s.reviewContinuations, workspaceID)
}

// ApproveCurrentBriefInput carries only workspace-level guided inputs for the
// explicit approval mutation; the current brief identity, exact bytes, and
// basis are resolved server-side by the delivery owner.
type ApproveCurrentBriefInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	Evidence        string
}

// WorkspaceBriefState reports only durable Brief and approval state. Review is
// transient and intentionally never appears in this projection.
type WorkspaceBriefState struct {
	State          string
	TicketID       string
	RevisionNumber int64
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
	BriefID, SelectionID, SelectionState, TicketID string
	AttemptNumber, RevisionNumber                  int64
	Filename, SHA256                               string
	SizeBytes                                      int64
	Status, ApprovalID                             string
	Historical                                     bool
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
		attemptNumber, err := s.nextTicketDesignBriefAttempt(ctx, tx, selection, revision)
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
			AttemptNumber: attemptNumber, RevisionRowID: revision.ID, Filename: filename, ArtifactRowID: artifact.ID,
			ArtifactSha256: file.SHA256, ArtifactSizeBytes: file.SizeBytes,
			CreatedIdentity: strings.TrimSpace(input.CreatedIdentity),
		})
		if err != nil {
			return briefConflictError(err)
		}
		if _, err := tx.SetCurrentTicketDesignBrief(ctx, workflowstore.SetCurrentTicketDesignBriefParams{
			CurrentTicketDesignBriefRowID: sql.NullInt64{Int64: brief.ID, Valid: true}, ID: selection.ID,
		}); err != nil {
			return briefConflictError(err)
		}
		result = TicketDesignBriefAdmissionResult{Brief: brief, Workspace: workspace, Filename: filename}
		return nil
	})
	if err != nil {
		return result, err
	}
	// A new brief attempt invalidates any pending ready-review continuation:
	// review can never cross a replacement on the same active selection.
	s.clearReviewContinuation(workspace.WorkspaceID)
	return result, nil
}

// nextTicketDesignBriefAttempt preserves an immutable rejected Brief and its
// review on the active selection, then returns the next attempt number for a
// replacement bound to that same source-backed selection.
func (s *Service) nextTicketDesignBriefAttempt(
	ctx context.Context,
	tx *workflowstore.Tx,
	selection workflowstore.DeliveryTicketSelection,
	revision workflowstore.DeliveryTicketRevision,
) (int64, error) {
	brief, err := tx.GetCurrentTicketDesignBriefBySelectionRowID(ctx, selection.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	if brief.RevisionRowID != revision.ID {
		return 0, ErrTicketDesignBriefBytesMismatch
	}
	if _, err := tx.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); err == nil {
		return 0, fmt.Errorf("%w: the current brief is already approved", ErrTicketDesignBriefConflict)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	// A replacement is a new immutable attempt. Review never persists a gate
	// that could be reused by this attempt.
	return brief.AttemptNumber + 1, nil
}

// ApproveTicketDesignBrief is the explicit confirmed owner mutation that
// approves the current admissible brief. The brief identity, exact bytes, and
// source-backed basis are resolved server-side; only the operator confirmation
// evidence and identity are accepted from the caller. It consumes the
// process-local continuation recorded by the preceding ready review and never
// accepts a brief ID or digest.
func (s *Service) ApproveTicketDesignBrief(ctx context.Context, input TicketDesignBriefApprovalInput) (TicketDesignBriefApprovalResult, error) {
	evidence := strings.TrimSpace(input.OperatorConfirmationEvidence)
	if !nonBlank(input.WorkspaceID) || input.ExpectedVersion < 1 || evidence == "" || len(evidence) > 4096 || !nonBlank(input.CreatedIdentity) {
		return TicketDesignBriefApprovalResult{}, ErrTicketDesignBriefApproval
	}
	continuation := s.takeReviewContinuation(strings.TrimSpace(input.WorkspaceID))
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
		brief, err := tx.GetCurrentTicketDesignBriefBySelectionRowID(ctx, selection.ID)
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
		if continuation == nil || continuation.workspaceID != workspace.WorkspaceID || continuation.briefID != brief.BriefID || continuation.selectionRowID != selection.ID || continuation.revisionRowID != revision.ID || continuation.sha256 != brief.ArtifactSha256 || continuation.sizeBytes != brief.ArtifactSizeBytes || !equalBytes(continuation.bytes, stored) {
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
	if err != nil {
		if !errors.Is(err, ErrBriefReviewIncomplete) {
			s.restoreReviewContinuation(strings.TrimSpace(input.WorkspaceID), continuation)
		}
		return result, err
	}
	return result, nil
}

// CompleteTicketDesignBriefReview validates that the reviewed bytes identify
// the exact current Brief and returns a transient result. The server recalculates
// the SHA-256 of the supplied reviewed bytes and requires byte-for-byte and
// digest equality with the verified current admissible artifact before accepting
// either disposition, so a review composed against a replaced or stale brief is
// rejected and no result ever attaches to a replacement. It never writes review
// material or lifecycle state.
func (s *Service) CompleteTicketDesignBriefReview(ctx context.Context, input CompleteBriefReviewInput) (TicketDesignBriefReviewResult, error) {
	if !nonBlank(input.WorkspaceID) || !nonBlank(input.ReviewerIdentity) || len(input.ReviewedBytes) == 0 ||
		(input.Disposition != TicketDesignBriefReviewReadyForApproval && input.Disposition != TicketDesignBriefReviewNeedsRevision) {
		return TicketDesignBriefReviewResult{}, ErrTicketDesignBriefReview
	}
	result := TicketDesignBriefReviewResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if err != nil {
			return err
		}
		selection, revision, _, err := s.currentActiveSelectionBasis(ctx, tx, workspace.ID)
		if err != nil {
			return err
		}
		brief, err := tx.GetCurrentTicketDesignBriefBySelectionRowID(ctx, selection.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: the active selection has no authored brief", ErrTicketDesignBriefNotFound)
		}
		if err != nil {
			return err
		}
		if brief.RevisionRowID != revision.ID {
			return ErrTicketDesignBriefBytesMismatch
		}
		if _, err := tx.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); err == nil {
			return fmt.Errorf("%w: the current brief is already approved", ErrTicketDesignBriefConflict)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		bytes, err := tx.ReadTicketDesignBriefBytes(ctx, brief.BriefID, int(brief.ArtifactSizeBytes))
		if err != nil || digestCandidate(bytes) != brief.ArtifactSha256 || int64(len(bytes)) != brief.ArtifactSizeBytes {
			return ErrTicketDesignBriefBytesMismatch
		}
		if digestCandidate(input.ReviewedBytes) != brief.ArtifactSha256 || !equalBytes(input.ReviewedBytes, bytes) {
			return ErrTicketDesignBriefBytesMismatch
		}
		result = TicketDesignBriefReviewResult{Brief: brief, Disposition: input.Disposition, Review: TicketDesignBriefReviewCompletion{ReviewerIdentity: strings.TrimSpace(input.ReviewerIdentity), Disposition: string(input.Disposition)}}
		if input.Disposition == TicketDesignBriefReviewReadyForApproval {
			// The private exact continuation is retained only in process
			// memory for the distinct explicit approval mutation.
			s.setReviewContinuation(workspace.WorkspaceID, &briefReviewContinuation{workspaceID: workspace.WorkspaceID, briefID: brief.BriefID, sha256: brief.ArtifactSha256, sizeBytes: brief.ArtifactSizeBytes, selectionRowID: selection.ID, revisionRowID: revision.ID, bytes: append([]byte(nil), input.ReviewedBytes...)})
		} else {
			// A needs_revision review clears any pending ready continuation and
			// returns only the ordinary planner.ticket_design_brief refresh.
			s.clearReviewContinuation(workspace.WorkspaceID)
			result.Refresh = &TicketDesignBriefReviewRefresh{OperationID: "planner.ticket_design_brief", AuditorReviewResult: string(input.Disposition), ReviewedBrief: append([]byte(nil), input.ReviewedBytes...)}
		}
		return nil
	})
	return result, err
}

// ApproveReviewedTicketDesignBrief approves the current brief using the
// process-local continuation recorded by the preceding ready review. The
// review argument is retained for owner fixtures that model the two distinct
// transitions as one convenience call; the external API always uses the
// separate approval mutation.
func (s *Service) ApproveReviewedTicketDesignBrief(ctx context.Context, review TicketDesignBriefReviewResult, input TicketDesignBriefApprovalInput) (TicketDesignBriefApprovalResult, error) {
	if review.Disposition != TicketDesignBriefReviewReadyForApproval {
		return TicketDesignBriefApprovalResult{}, ErrBriefReviewIncomplete
	}
	return s.ApproveTicketDesignBrief(ctx, input)
}

func (s *Service) CompleteAndApproveTicketDesignBrief(ctx context.Context, review CompleteBriefReviewInput, approval TicketDesignBriefApprovalInput) (TicketDesignBriefApprovalResult, error) {
	completed, err := s.CompleteTicketDesignBriefReview(ctx, review)
	if err != nil {
		return TicketDesignBriefApprovalResult{}, err
	}
	return s.ApproveReviewedTicketDesignBrief(ctx, completed, approval)
}

// ApproveCurrentTicketDesignBrief cannot manufacture a review continuation.
// It is the guided convenience that resolves the current brief server-side;
// ready approval remains available only after a ready review records its
// process-local continuation.
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

// HasPendingCurrentBriefApproval is the transient owner read consumed by the
// guided decision. It reports whether the current exact brief on the
// workspace's active selection carries a pending process-local ready-review
// continuation and reads only that workspace's own entry, so a ready review in
// another workspace can never displace it. It first validates the current
// source-backed selection basis and brief binding through the same read path
// the approval mutation uses, so the transient answer can never be observed
// across a replacement brief, a stale selection, or a different workspace. It
// is not a durable gate and is never exposed as a lifecycle state: the final
// approval mutation revalidates the exact brief, basis, and bytes before any
// approval is written.
func (s *Service) HasPendingCurrentBriefApproval(ctx context.Context, workspaceID string) (bool, error) {
	if !nonBlank(workspaceID) {
		return false, ErrInvalidTicket
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return false, err
	}
	selection, revision, _, err := s.currentActiveSelectionBasis(ctx, s.store, workspace.ID)
	if err != nil {
		// A missing or stale basis means no current exact brief can carry a
		// pending approval; the final approval mutation revalidates the basis
		// before writing anything, so a false transient read can never
		// authorize progression.
		return false, nil
	}
	brief, err := s.store.GetCurrentTicketDesignBriefBySelectionRowID(ctx, selection.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if brief.RevisionRowID != revision.ID {
		return false, nil
	}
	s.reviewMutex.Lock()
	defer s.reviewMutex.Unlock()
	continuation := s.reviewContinuations[workspace.WorkspaceID]
	if continuation == nil {
		return false, nil
	}
	return continuation.workspaceID == workspace.WorkspaceID &&
		continuation.briefID == brief.BriefID &&
		continuation.selectionRowID == selection.ID &&
		continuation.revisionRowID == revision.ID &&
		continuation.sha256 == brief.ArtifactSha256 &&
		continuation.sizeBytes == brief.ArtifactSizeBytes, nil
}

// ReadWorkspaceBriefState is the delivery-owner semantic read consumed by the
// guided journey projection. It resolves the workspace's active selection and
// reports none | authored | approved for the durable brief bound to it.
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
	var current workflowstore.DeliveryTicketSelection
	found := false
	for _, selection := range selections {
		if selection.State != "active" {
			continue
		}
		if found {
			return WorkspaceBriefState{}, ErrSelectionConflict
		}
		current, found = selection, true
	}
	if !found {
		return result, nil
	}
	members, err := s.store.ListDeliveryTicketSelectionMembers(ctx, current.ID)
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
	brief, err := s.store.GetCurrentTicketDesignBriefBySelectionRowID(ctx, current.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return WorkspaceBriefState{}, err
	}
	result.State = "authored"
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
		entry := TicketDesignBriefIntegrity{BriefID: brief.BriefID, AttemptNumber: brief.AttemptNumber, Status: "authored"}
		selection, ok := selectionByRowID[brief.SelectionRowID]
		if !ok {
			result.Diagnostics = append(result.Diagnostics, TicketDesignBriefIntegrityDiagnostic{BriefID: brief.BriefID, Condition: "inconsistent"})
			result.Briefs = append(result.Briefs, entry)
			continue
		}
		entry.SelectionID, entry.SelectionState = selection.SelectionID, selection.State
		entry.Historical = selection.State != "active" || !selection.CurrentTicketDesignBriefRowID.Valid || selection.CurrentTicketDesignBriefRowID.Int64 != brief.ID
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
		if approval, approvalErr := s.store.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); approvalErr == nil {
			entry.Status, entry.ApprovalID = "approved", approval.ApprovalID
			if approval.BriefArtifactRowID != brief.ArtifactRowID || approval.BriefSha256 != brief.ArtifactSha256 || approval.BriefSizeBytes != brief.ArtifactSizeBytes {
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
