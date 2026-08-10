package packages

import (
	"context"
	"fmt"
	"path/filepath"

	"relay/internal/sourcevault"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

// selectedTicketBasis is the server-resolved selected approved Delivery Ticket
// basis. It carries the exact source-vault bytes and the deterministic
// projection so every consumer (prepare, approve revalidation, approved
// authority load) binds the identical exact bytes and projection. No Brief
// identity, bytes, digest, or projection exists on this surface.
type selectedTicketBasis struct {
	selectionMember workflowstore.DeliveryTicketSelectionMember
	revision        workflowstore.DeliveryTicketRevision
	ticket          workflowstore.DeliveryTicket
	approval        workflowstore.DeliveryTicketRevisionApproval
	closure         workflowstore.SourceVaultClosure
	sourceSHA256    string
	sourceBytes     []byte
	objectOID       string
	document        *speccompiler.DeliveryTicketDocument
	projection      speccompiler.DeliveryTicketProjection
}

// resolveSelectedTicketBasis resolves the selected approved Delivery Ticket
// revision from the active selection and verifies its exact source-vault bytes
// and deterministic projection against the stored revision identity, members,
// and dependencies. The source-vault reader is required: the server resolves
// the selected Ticket's exact bytes; no caller-supplied Brief or digest can
// substitute.
func (s *Service) resolveSelectedTicketBasis(ctx context.Context, tx *workflowstore.Tx, selection workflowstore.DeliveryTicketSelection, workspace workflowstore.FeatureWorkspace, selectionMember workflowstore.DeliveryTicketSelectionMember) (selectedTicketBasis, error) {
	if s.sourceVaults == nil {
		return selectedTicketBasis{}, fmt.Errorf("%w: source-vault reader is not configured", ErrPackageBasisChanged)
	}
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, selectionMember.RevisionRowID)
	if err != nil {
		return selectedTicketBasis{}, err
	}
	ticket, err := tx.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return selectedTicketBasis{}, err
	}
	if !selection.SourceClosureRowID.Valid {
		return selectedTicketBasis{}, fmt.Errorf("%w: selection has no source closure", ErrPackageBasisChanged)
	}
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, selection.SourceClosureRowID.Int64)
	if err != nil {
		return selectedTicketBasis{}, err
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != revision.BaseCommit || revision.SourceClosureRowID != closure.ID {
		return selectedTicketBasis{}, fmt.Errorf("%w: source closure is not the exact ready Ticket base", ErrPackageBasisChanged)
	}
	if !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID {
		return selectedTicketBasis{}, fmt.Errorf("%w: selected Ticket is not current on the exact package source", ErrPackageBasisChanged)
	}
	if ticket.WorkspaceRowID != workspace.ID {
		return selectedTicketBasis{}, fmt.Errorf("%w: selected Ticket does not belong to the package workspace", ErrPackageBasisChanged)
	}
	if err := validateDeliveryTicketSourcePath(revision.SourcePath); err != nil {
		return selectedTicketBasis{}, fmt.Errorf("%w: %v", ErrPackageBasisChanged, err)
	}
	result, err := s.sourceVaults.ReadPath(ctx, sourcevault.ReadPathRequest{
		ClosureID: closure.ClosureID,
		Path:      revision.SourcePath,
		MaxBytes:  approvedAuthorityReadLimit,
	})
	if err != nil {
		return selectedTicketBasis{}, fmt.Errorf("%w: selected Delivery Ticket source document is unavailable: %w", ErrPackageBasisChanged, err)
	}
	if !validOID(result.ObjectOID) {
		return selectedTicketBasis{}, fmt.Errorf("%w: selected Delivery Ticket source object OID is invalid", ErrPackageBasisChanged)
	}
	compileResult, document := speccompiler.CompileDeliveryTicket(filepath.Base(revision.SourcePath), result.Bytes)
	if len(compileResult.Errors) != 0 || document == nil {
		return selectedTicketBasis{}, fmt.Errorf("%w: Delivery Ticket compiler rejected exact bytes: %v", ErrPackageBasisChanged, compileResult.Errors)
	}
	projection, projectionDiagnostics := speccompiler.ProjectDeliveryTicket(document)
	if len(projectionDiagnostics) != 0 {
		return selectedTicketBasis{}, fmt.Errorf("%w: Delivery Ticket projection is inconsistent: %v", ErrPackageBasisChanged, projectionDiagnostics)
	}
	if err := verifyTicketDocumentIdentity(ctx, tx, document, workspace, ticket, revision); err != nil {
		return selectedTicketBasis{}, err
	}
	approvals, err := tx.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return selectedTicketBasis{}, err
	}
	var approval workflowstore.DeliveryTicketRevisionApproval
	foundApproval := false
	for _, candidate := range approvals {
		if candidate.ID == selectionMember.ApprovalRowID {
			approval, foundApproval = candidate, true
			break
		}
	}
	if !foundApproval {
		return selectedTicketBasis{}, fmt.Errorf("%w: selected Ticket approval is missing", ErrPackageBasisChanged)
	}
	return selectedTicketBasis{
		selectionMember: selectionMember,
		revision:        revision,
		ticket:          ticket,
		approval:        approval,
		closure:         closure,
		sourceSHA256:    sha256Hex(result.Bytes),
		sourceBytes:     append([]byte(nil), result.Bytes...),
		objectOID:       result.ObjectOID,
		document:        document,
		projection:      projection,
	}, nil
}

// verifyTicketDocumentIdentity binds the compiled Delivery Ticket document to
// the stored selected revision, its ticket, its members, and its dependencies.
// The exact bytes are the authority: any identity drift rejects the basis.
func verifyTicketDocumentIdentity(
	ctx context.Context,
	tx *workflowstore.Tx,
	document *speccompiler.DeliveryTicketDocument,
	workspace workflowstore.FeatureWorkspace,
	ticket workflowstore.DeliveryTicket,
	revision workflowstore.DeliveryTicketRevision,
) error {
	if document.FeatureSlug != workspace.FeatureSlug ||
		document.TicketID != ticket.TicketID ||
		document.Revision != revision.RevisionNumber ||
		document.RepoTarget != revision.RepoTarget ||
		document.Branch != revision.Branch ||
		document.BaseCommit != revision.BaseCommit ||
		document.Goal != revision.Goal ||
		document.Context != revision.Context ||
		document.TransitionApplicability != revision.TransitionApplicability {
		return fmt.Errorf("%w: Delivery Ticket source document identity does not match selected revision", ErrPackageBasisChanged)
	}
	if revision.ReplacesRevisionRowID.Valid {
		replacesRevision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, revision.ReplacesRevisionRowID.Int64)
		if err != nil {
			return err
		}
		if document.ReplacesRevision == nil || *document.ReplacesRevision != replacesRevision.RevisionNumber {
			return fmt.Errorf("%w: Delivery Ticket source document replaces_revision does not match selected revision", ErrPackageBasisChanged)
		}
	} else if document.ReplacesRevision != nil {
		return fmt.Errorf("%w: Delivery Ticket source document replaces_revision does not match selected revision", ErrPackageBasisChanged)
	}
	if revision.CancellationReason.Valid {
		if document.Cancellation == nil || document.Cancellation.Reason != revision.CancellationReason.String {
			return fmt.Errorf("%w: Delivery Ticket source document cancellation does not match selected revision", ErrPackageBasisChanged)
		}
	} else if document.Cancellation != nil {
		return fmt.Errorf("%w: Delivery Ticket source document cancellation does not match selected revision", ErrPackageBasisChanged)
	}
	dependencies, err := tx.ListDeliveryTicketRevisionDependencies(ctx, revision.ID)
	if err != nil {
		return err
	}
	if len(document.DependsOn) != len(dependencies) {
		return fmt.Errorf("%w: Delivery Ticket source document dependencies count does not match selected revision", ErrPackageBasisChanged)
	}
	for index, dep := range dependencies {
		depRevision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, dep.DependsOnRevisionRowID)
		if err != nil {
			return err
		}
		depTicket, err := tx.GetDeliveryTicketByRowID(ctx, depRevision.DeliveryTicketRowID)
		if err != nil {
			return err
		}
		if document.DependsOn[index].TicketID != depTicket.TicketID || document.DependsOn[index].Revision != depRevision.RevisionNumber {
			return fmt.Errorf("%w: Delivery Ticket source document dependency %d does not match stored revision", ErrPackageBasisChanged, index)
		}
	}
	members, err := tx.ListDeliveryTicketRevisionMembers(ctx, revision.ID)
	if err != nil {
		return err
	}
	var obligations []workflowstore.DeliveryTicketRevisionMember
	for _, member := range members {
		if member.MemberKind == "implementation_obligation" {
			obligations = append(obligations, member)
		}
	}
	if len(document.ImplementationObligations) != len(obligations) {
		return fmt.Errorf("%w: Delivery Ticket source document implementation obligations count does not match selected revision", ErrPackageBasisChanged)
	}
	for index, obligation := range obligations {
		docObligation := document.ImplementationObligations[index]
		sourceAreaMatches := obligation.MemberPath.Valid == (docObligation.SourceArea != nil) && (!obligation.MemberPath.Valid || *docObligation.SourceArea == obligation.MemberPath.String)
		if !sourceAreaMatches || docObligation.Obligation != obligation.MemberText {
			return fmt.Errorf("%w: Delivery Ticket source document implementation obligation %d does not match stored revision", ErrPackageBasisChanged, index)
		}
	}
	return nil
}

// dependenciesBasisSHA256 binds the ordered completed dependencies of the
// selected Ticket revision: each dependency's identity (depends-on Ticket and
// revision number), its sequence, and its satisfied outcome. A dependency that
// is not completed is rejected because the package basis requires completed
// dependencies only.
func dependenciesBasisSHA256(ctx context.Context, tx *workflowstore.Tx, revisionID int64, dependencies []workflowstore.DeliveryTicketRevisionDependency) (string, error) {
	parts := []string{"ticket-dependencies-v1", fmt.Sprint(revisionID)}
	for _, dep := range dependencies {
		if dep.Outcome != "satisfied" {
			return "", fmt.Errorf("%w: dependency %d is not completed", ErrPackageBasisChanged, dep.Sequence)
		}
		depRevision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, dep.DependsOnRevisionRowID)
		if err != nil {
			return "", err
		}
		depTicket, err := tx.GetDeliveryTicketByRowID(ctx, depRevision.DeliveryTicketRowID)
		if err != nil {
			return "", err
		}
		if !depTicket.CurrentRevisionRowID.Valid || depTicket.CurrentRevisionRowID.Int64 != depRevision.ID {
			return "", fmt.Errorf("%w: dependency %d revision is not current", ErrPackageBasisChanged, dep.Sequence)
		}
		parts = append(parts, fmt.Sprint(dep.ID), fmt.Sprint(dep.Sequence), depTicket.TicketID, fmt.Sprint(depRevision.RevisionNumber), dep.Outcome)
	}
	return compoundSHA256(parts...), nil
}

// resolveApprovedCompletedDependencies resolves each dependency of the
// selected Ticket revision to its exact depends-on Ticket identity and revision
// number and verifies the completed outcome, mirroring the package basis
// binding (dependenciesBasisSHA256). Only satisfied, current dependencies are
// canonical completed outcome records; the approved package basis requires
// exactly those. The raw TicketDependencies rows remain exposed unchanged.
func resolveApprovedCompletedDependencies(ctx context.Context, tx *workflowstore.Tx, revisionID int64, dependencies []workflowstore.DeliveryTicketRevisionDependency) ([]ApprovedCompletedDependency, error) {
	result := make([]ApprovedCompletedDependency, 0, len(dependencies))
	for _, dep := range dependencies {
		if dep.RevisionRowID != revisionID || dep.Sequence < 1 || dep.DependsOnRevisionRowID < 1 || dep.Outcome != "satisfied" {
			return nil, fmt.Errorf("%w: dependency %d is not a completed outcome of the selected revision", ErrApprovedAuthorityInvalid, dep.Sequence)
		}
		depRevision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, dep.DependsOnRevisionRowID)
		if err != nil {
			return nil, fmt.Errorf("%w: load dependency %d revision: %v", ErrApprovedAuthorityInvalid, dep.Sequence, err)
		}
		depTicket, err := tx.GetDeliveryTicketByRowID(ctx, depRevision.DeliveryTicketRowID)
		if err != nil {
			return nil, fmt.Errorf("%w: load dependency %d Ticket: %v", ErrApprovedAuthorityInvalid, dep.Sequence, err)
		}
		if !depTicket.CurrentRevisionRowID.Valid || depTicket.CurrentRevisionRowID.Int64 != depRevision.ID {
			return nil, fmt.Errorf("%w: dependency %d revision is not current", ErrApprovedAuthorityInvalid, dep.Sequence)
		}
		result = append(result, ApprovedCompletedDependency{Sequence: dep.Sequence, TicketID: depTicket.TicketID, Revision: depRevision.RevisionNumber, Outcome: dep.Outcome})
	}
	return result, nil
}

// readSelectedTicketDocument re-resolves the exact selected Ticket source
// bytes from the source vault for revalidation. It is the no-Brief successor
// of the package-artifact brief read: the source vault is the sole exact-byte
// authority for the selected Ticket.
func (s *Service) readSelectedTicketDocument(ctx context.Context, closure workflowstore.SourceVaultClosure, sourcePath string) (selectedTicketSource, error) {
	if s.sourceVaults == nil {
		return selectedTicketSource{}, fmt.Errorf("%w: source-vault reader is not configured", ErrPackageBasisChanged)
	}
	if err := validateDeliveryTicketSourcePath(sourcePath); err != nil {
		return selectedTicketSource{}, fmt.Errorf("%w: %v", ErrPackageBasisChanged, err)
	}
	result, err := s.sourceVaults.ReadPath(ctx, sourcevault.ReadPathRequest{
		ClosureID: closure.ClosureID,
		Path:      sourcePath,
		MaxBytes:  approvedAuthorityReadLimit,
	})
	if err != nil {
		return selectedTicketSource{}, fmt.Errorf("%w: selected Delivery Ticket source document is unavailable: %w", ErrPackageBasisChanged, err)
	}
	if !validOID(result.ObjectOID) {
		return selectedTicketSource{}, fmt.Errorf("%w: selected Delivery Ticket source object OID is invalid", ErrPackageBasisChanged)
	}
	return selectedTicketSource{sha256: sha256Hex(result.Bytes), bytes: append([]byte(nil), result.Bytes...), objectOID: result.ObjectOID}, nil
}

type selectedTicketSource struct {
	sha256    string
	bytes     []byte
	objectOID string
}
