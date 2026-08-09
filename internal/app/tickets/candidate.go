package tickets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"relay/internal/app/approvals"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const candidateFamilyDeliveryTicket = "delivery_ticket"

var (
	ErrInvalidCandidateProduction = errors.New("invalid delivery ticket candidate production request")
	ErrCandidateNotDeliveryTicket = errors.New("planning candidate is not a delivery ticket")
	ErrCandidateCompilation       = errors.New("delivery ticket candidate compilation failed")
	ErrCandidateApprovalInvalid   = errors.New("planning candidate approval is invalid")
	ErrCandidateBytesMismatch     = errors.New("planning candidate bytes or digest mismatch")
	ErrCandidateVersionConflict   = errors.New("planning candidate workspace version conflict")
	ErrHistoricalBasis            = errors.New("historical candidate basis cannot produce a delivery ticket")
	ErrStaleCandidateBasis        = errors.New("delivery ticket candidate basis is stale")
)

type CandidateProductionInput struct {
	CandidateID      string
	ApprovalID       string
	ExpectedVersion  int64
	ExternalPriority int64
	CreatedIdentity  string
}

type CandidateProductionResult struct {
	PublishedRevision
	Candidate         workflowstore.PlanningCandidate
	CandidateApproval workflowstore.PlanningCandidateApproval
	ProducedApproval  workflowstore.DeliveryTicketRevisionApproval
	ProductionLink    workflowstore.DeliveryTicketProductionLink
}

// PromoteApprovedDeliveryTicketCandidate deterministically produces a Ticket
// from the exact approved candidate bytes. Candidate approval, compiler output,
// production linkage, produced-revision approval, currentness, and completion
// reopening share one CommitArtifactBatch transaction.
func (s *Service) PromoteApprovedDeliveryTicketCandidate(ctx context.Context, input CandidateProductionInput) (CandidateProductionResult, error) {
	if strings.TrimSpace(input.CandidateID) != input.CandidateID || input.CandidateID == "" ||
		strings.TrimSpace(input.ApprovalID) != input.ApprovalID || input.ApprovalID == "" ||
		input.ExpectedVersion < 1 || input.ExternalPriority < 0 || strings.TrimSpace(input.CreatedIdentity) != input.CreatedIdentity || input.CreatedIdentity == "" {
		return CandidateProductionResult{}, ErrInvalidCandidateProduction
	}
	candidate, err := s.store.GetPlanningCandidateByCandidateID(ctx, input.CandidateID)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateProductionResult{}, ErrCandidateApprovalInvalid
	}
	if err != nil {
		return CandidateProductionResult{}, err
	}
	if candidate.Family != candidateFamilyDeliveryTicket {
		return CandidateProductionResult{}, ErrCandidateNotDeliveryTicket
	}
	candidateBytes, err := s.store.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, int(candidate.ArtifactSizeBytes))
	if err != nil {
		return CandidateProductionResult{}, ErrCandidateBytesMismatch
	}
	compiled, document := speccompiler.CompileDeliveryTicket(candidate.Filename, candidateBytes)
	if len(compiled.Errors) != 0 || document == nil || compiled.OutputFilename == nil || compiled.Markdown == nil {
		return CandidateProductionResult{}, fmt.Errorf("%w: %v", ErrCandidateCompilation, compiled.Errors)
	}
	if document.FeatureSlug == "" || document.TicketID == "" || document.Revision < 1 {
		return CandidateProductionResult{}, ErrCandidateCompilation
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, candidate.WorkspaceRowID)
	if err != nil {
		return CandidateProductionResult{}, err
	}
	stagingArtifactID := workflowstore.NewFeatureWorkspaceDiscoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + stagingArtifactID)
	if err != nil {
		return CandidateProductionResult{}, err
	}
	canonical, err := batch.Stage("delivery_ticket_canonical", candidate.Filename, "application/json", candidateBytes)
	if err != nil {
		_ = batch.Rollback()
		return CandidateProductionResult{}, err
	}
	rendered, err := batch.Stage("delivery_ticket_rendered", *compiled.OutputFilename, "text/markdown", []byte(*compiled.Markdown))
	if err != nil {
		_ = batch.Rollback()
		return CandidateProductionResult{}, err
	}

	result := CandidateProductionResult{
		PublishedRevision: PublishedRevision{
			Canonical: storedArtifact(canonical.RelativePath, canonical.SHA256, canonical.SizeBytes),
			Rendered:  storedArtifact(rendered.RelativePath, rendered.SHA256, rendered.SizeBytes),
		},
	}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		candidate, err := tx.GetPlanningCandidateByCandidateID(ctx, input.CandidateID)
		if err != nil {
			return ErrCandidateApprovalInvalid
		}
		if candidate.Family != candidateFamilyDeliveryTicket {
			return ErrCandidateNotDeliveryTicket
		}
		workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, candidate.WorkspaceRowID)
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrCandidateVersionConflict
		}
		if err := currentDeliveryCandidateBasis(ctx, tx, candidate, workspace); err != nil {
			return err
		}
		if err := currentExactDeliveryCandidate(ctx, tx, candidate, workspace); err != nil {
			return err
		}
		stored, err := tx.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, int(candidate.ArtifactSizeBytes))
		if err != nil || !equalCandidateBytes(stored, candidateBytes) {
			return ErrCandidateBytesMismatch
		}
		candidateApproval, err := tx.GetPlanningCandidateApprovalByApprovalID(ctx, input.ApprovalID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCandidateApprovalInvalid
		}
		if err != nil {
			return err
		}
		if candidateApproval.CandidateRowID != candidate.ID || candidateApproval.CandidateArtifactRowID != candidate.ArtifactRowID || candidateApproval.CandidateSha256 != candidate.ArtifactSha256 || candidateApproval.CandidateSizeBytes != candidate.ArtifactSizeBytes {
			return ErrCandidateApprovalInvalid
		}
		if candidate.ArtifactSha256 != digestCandidate(candidateBytes) || candidate.ArtifactSizeBytes != int64(len(candidateBytes)) {
			return ErrCandidateBytesMismatch
		}
		if document.FeatureSlug != workspace.FeatureSlug || candidate.RepoTarget != document.RepoTarget || candidate.Branch != document.Branch || candidate.BaseCommit != document.BaseCommit {
			return ErrCandidateCompilation
		}

		sourceClosure, err := candidateSourceClosure(ctx, tx, candidate, document.BaseCommit)
		if err != nil {
			return err
		}
		ticket, err := tx.GetDeliveryTicketByTicketID(ctx, document.TicketID)
		nextRevision := document.Revision
		var priorRevision sql.NullInt64
		if errors.Is(err, sql.ErrNoRows) {
			if document.Revision != 1 || document.ReplacesRevision != nil {
				return ErrRevisionConflict
			}
			ticket, err = tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: document.TicketID, WorkspaceRowID: workspace.ID, ExternalPriority: input.ExternalPriority})
		} else if err == nil {
			if ticket.WorkspaceRowID != workspace.ID {
				return ErrInvalidTicket
			}
			if !ticket.CurrentRevisionRowID.Valid {
				if document.Revision != 1 || document.ReplacesRevision != nil {
					return ErrRevisionConflict
				}
			} else {
				prior, priorErr := tx.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
				if priorErr != nil {
					return priorErr
				}
				if document.Revision != prior.RevisionNumber+1 || document.ReplacesRevision == nil || *document.ReplacesRevision != prior.RevisionNumber {
					return ErrRevisionConflict
				}
				nextRevision = prior.RevisionNumber + 1
				priorRevision = sql.NullInt64{Int64: prior.ID, Valid: true}
			}
		} else {
			return err
		}
		if nextRevision != document.Revision {
			return ErrRevisionConflict
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: document.Revision, ReplacesRevisionRowID: priorRevision,
			CancellationReason: candidateCancellation(document), RepoTarget: document.RepoTarget, Branch: document.Branch,
			BaseCommit: document.BaseCommit, SourceClosureRowID: sourceClosure.ID, SourcePath: candidate.Filename,
			Goal: document.Goal, Context: document.Context, TransitionApplicability: document.TransitionApplicability,
		})
		if err != nil {
			return err
		}
		for index, obligation := range document.ImplementationObligations {
			if _, err := tx.CreateDeliveryTicketRevisionMember(ctx, workflowstore.CreateDeliveryTicketRevisionMemberParams{RevisionRowID: revision.ID, Sequence: int64(index + 1), MemberKind: "implementation_obligation", MemberPath: nullableString(obligation.Path), MemberText: obligation.Obligation}); err != nil {
				return err
			}
		}
		for index, validation := range document.ValidationIntent {
			if _, err := tx.CreateDeliveryTicketRevisionMember(ctx, workflowstore.CreateDeliveryTicketRevisionMemberParams{RevisionRowID: revision.ID, Sequence: int64(len(document.ImplementationObligations) + index + 1), MemberKind: "validation_intent", MemberText: validation}); err != nil {
				return err
			}
		}
		for index, dependency := range document.DependsOn {
			dependencyRevision, dependencyErr := candidateDependencyRevision(ctx, tx, workspace.ID, dependency)
			if dependencyErr != nil {
				return dependencyErr
			}
			if _, err := tx.CreateDeliveryTicketRevisionDependency(ctx, workflowstore.CreateDeliveryTicketRevisionDependencyParams{RevisionRowID: revision.ID, Sequence: int64(index + 1), DependsOnRevisionRowID: dependencyRevision.ID, Outcome: "satisfied"}); err != nil {
				return err
			}
		}
		canonicalArtifact, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryArtifactParams{DiscoveryArtifactID: workflowstore.NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspace.ID, RelativePath: canonical.RelativePath, Sha256: canonical.SHA256, MediaType: canonical.MediaType, SizeBytes: canonical.SizeBytes})
		if err != nil {
			return err
		}
		markdownArtifact, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryArtifactParams{DiscoveryArtifactID: workflowstore.NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspace.ID, RelativePath: rendered.RelativePath, Sha256: rendered.SHA256, MediaType: rendered.MediaType, SizeBytes: rendered.SizeBytes})
		if err != nil {
			return err
		}
		producedApproval, err := s.approvals.ApproveDeliveryTicketRevisionInTx(ctx, tx, approvals.DeliveryTicketRevisionApprovalInput{
			Ticket: ticket, Revision: revision, AuthorityRevisionID: candidate.AuthorityRevisionRowID,
			Rationale: "produced from approved delivery ticket candidate", RequireCurrentRevision: false,
		})
		if err != nil {
			return err
		}
		link, err := tx.CreateDeliveryTicketProductionLink(ctx, workflowstore.CreateDeliveryTicketProductionLinkParams{
			ProductionLinkID: workflowstore.NewDeliveryTicketProductionLinkID(), DeliveryTicketRowID: ticket.ID, CandidateRowID: candidate.ID,
			CandidateArtifactRowID: candidate.ArtifactRowID, CandidateSha256: candidate.ArtifactSha256, CandidateSizeBytes: candidate.ArtifactSizeBytes,
			CanonicalJsonArtifactRowID: canonicalArtifact.ID, CanonicalJsonSha256: canonical.SHA256, CanonicalJsonSizeBytes: canonical.SizeBytes,
			RenderedMarkdownArtifactRowID: markdownArtifact.ID, RenderedMarkdownSha256: rendered.SHA256, RenderedMarkdownSizeBytes: rendered.SizeBytes,
			ProducedRevisionRowID: revision.ID, ProducedRevisionIdentity: fmt.Sprintf("%s:r%d", document.TicketID, document.Revision), CreatedIdentity: input.CreatedIdentity,
		})
		if err != nil {
			return err
		}
		ticket, err = tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID)
		if err != nil {
			return err
		}
		if err := reopenCurrentFeatureCompletionForTicket(ctx, tx, ticket, revision); err != nil {
			return err
		}
		result.PublishedRevision.Ticket, result.PublishedRevision.Revision = ticket, revision
		result.Candidate, result.CandidateApproval, result.ProducedApproval, result.ProductionLink = candidate, candidateApproval, producedApproval, link
		return nil
	})
	return result, err
}

func (s *Service) PublishApprovedDeliveryTicketCandidate(ctx context.Context, input CandidateProductionInput) (CandidateProductionResult, error) {
	return s.PromoteApprovedDeliveryTicketCandidate(ctx, input)
}

func currentDeliveryCandidateBasis(ctx context.Context, tx *workflowstore.Tx, candidate workflowstore.PlanningCandidate, workspace workflowstore.FeatureWorkspace) error {
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || !workspace.CurrentDiscoveryRevisionRowID.Valid || candidate.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 {
		return ErrHistoricalBasis
	}
	packet, err := tx.GetDiscoveryClosurePacketByRowID(ctx, candidate.DiscoveryClosurePacketRowID)
	if err != nil || packet.WorkspaceRowID != workspace.ID || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 || packet.Destination != candidate.Destination {
		return ErrHistoricalBasis
	}
	if candidate.AuthorityRevisionRowID.Valid != workspace.CurrentAuthorityRevisionRowID.Valid || (candidate.AuthorityRevisionRowID.Valid && candidate.AuthorityRevisionRowID.Int64 != workspace.CurrentAuthorityRevisionRowID.Int64) {
		return ErrStaleCandidateBasis
	}
	return nil
}

// currentExactDeliveryCandidate prevents an older immutable candidate on the
// same current basis from producing a Ticket after a replacement has been
// admitted. The delivery owner enforces this independently of the guided
// projection so direct owner calls cannot resurrect stale candidate authority.
func currentExactDeliveryCandidate(ctx context.Context, tx *workflowstore.Tx, candidate workflowstore.PlanningCandidate, workspace workflowstore.FeatureWorkspace) error {
	candidates, err := tx.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return err
	}
	for index := len(candidates) - 1; index >= 0; index-- {
		current := candidates[index]
		if current.Family != candidateFamilyDeliveryTicket || current.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 || current.AuthorityRevisionRowID.Valid != workspace.CurrentAuthorityRevisionRowID.Valid || (current.AuthorityRevisionRowID.Valid && current.AuthorityRevisionRowID.Int64 != workspace.CurrentAuthorityRevisionRowID.Int64) {
			continue
		}
		if current.ID == candidate.ID {
			return nil
		}
		return ErrStaleCandidateBasis
	}
	return ErrStaleCandidateBasis
}

func candidateSourceClosure(ctx context.Context, tx *workflowstore.Tx, candidate workflowstore.PlanningCandidate, baseCommit string) (workflowstore.SourceVaultClosure, error) {
	if candidate.AuthorityRevisionRowID.Valid {
		authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, candidate.AuthorityRevisionRowID.Int64)
		if err != nil || !authority.SourceClosureRowID.Valid {
			return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
		}
		closure, err := tx.GetSourceVaultClosureByRowID(ctx, authority.SourceClosureRowID.Int64)
		if err != nil {
			return workflowstore.SourceVaultClosure{}, err
		}
		vault, err := tx.GetSourceVaultByRepositoryTarget(ctx, candidate.RepoTarget)
		if err != nil || vault.ID != closure.VaultRowID {
			return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
		}
		if closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != baseCommit {
			return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
		}
		return closure, nil
	}
	closure, err := tx.GetReadySourceVaultClosureByRepositoryTargetAndCommit(ctx, candidate.RepoTarget, baseCommit)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
	}
	return closure, nil
}

func digestCandidate(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func candidateCancellation(document *speccompiler.DeliveryTicketDocument) sql.NullString {
	if document.Cancellation == nil {
		return sql.NullString{}
	}
	return nullableString(document.Cancellation.Reason)
}

func candidateDependencyRevision(ctx context.Context, tx *workflowstore.Tx, workspaceID int64, dependency speccompiler.DeliveryTicketDependency) (workflowstore.DeliveryTicketRevision, error) {
	ticket, err := tx.GetDeliveryTicketByTicketID(ctx, dependency.TicketID)
	if err != nil || ticket.WorkspaceRowID != workspaceID || !ticket.CurrentRevisionRowID.Valid {
		return workflowstore.DeliveryTicketRevision{}, ErrDependencyNotCurrent
	}
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
	if err != nil || revision.RevisionNumber != dependency.Revision {
		return workflowstore.DeliveryTicketRevision{}, ErrDependencyNotCurrent
	}
	return revision, nil
}

func equalCandidateBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
