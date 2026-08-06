package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrPrototypeCapabilityDisabled   = errors.New("prototype execution capability is disabled")
	ErrPrototypeWorkspaceStale       = errors.New("prototype workspace is stale")
	ErrPrototypeWorkItemStale        = errors.New("prototype work item is stale")
	ErrPrototypeProposalMissing      = errors.New("prototype proposal is missing or drifted")
	ErrPrototypeAuthorizationMissing = errors.New("prototype authorization is missing or drifted")
	ErrPrototypeApprovalMissing      = errors.New("prototype approval is missing")
	ErrPrototypeApprovalStale        = errors.New("prototype approval is stale")
	ErrPrototypeApprovalConflicting  = errors.New("prototype approval conflicts")
	ErrPrototypeApprovalConsumed     = errors.New("prototype approval already consumed")
	ErrPrototypeRunStale             = errors.New("prototype run is stale")
	ErrPrototypeInvalidTransition    = errors.New("invalid prototype lifecycle transition")
	ErrPrototypeSourceDivergence     = errors.New("prototype source diverged")
	ErrPrototypeArtifactPersistence  = errors.New("prototype artifact persistence failure")
	ErrPrototypeOwnership            = errors.New("prototype workspace or work item ownership violation")
)

type PreparePrototypeProposalInput struct {
	WorkspaceID, WorkItemID                           string
	ExpectedWorkspaceVersion, ExpectedWorkItemVersion int64
	Proposal                                          []byte
	MediaType                                         string
}
type PreparePrototypeExecutionInput struct {
	WorkspaceID, WorkItemID, ProposalID, SourceClosureID, RepoTarget, BaseCommit, Adapter, Model string
	ExpectedWorkspaceVersion, ExpectedWorkItemVersion                                            int64
	Variants, EvidenceObligations                                                                []string
	Limits                                                                                       map[string]any
}
type ApprovePrototypeExecutionInput struct {
	WorkspaceID, RunID, ProposalID, AuthorizationID, InvocationSHA256, MutationIdentity, OperatorConfirmationEvidence string
	ExpectedRunVersion                                                                                                int64
}
type PrototypeExecutionDetail struct {
	workflowstore.PrototypeExecutionAggregate
	Runtime         *workflowstore.PrototypeRuntime
	Target          *workflowstore.PrototypeTarget
	Lease           *workflowstore.PrototypeLease
	EvidenceBatches []workflowstore.PrototypeEvidenceImportBatch
	FinalResult     *workflowstore.PrototypeResult
	Evidence        []workflowstore.PrototypeEvidenceMember
}

var (
	ErrPrototypePreparationClaimed         = prototypeexecution.ErrPreparationClaimed
	ErrPrototypeLaunchAlreadyClaimed       = prototypeexecution.ErrLaunchAlreadyClaimed
	ErrPrototypeLaunchUncertain            = prototypeexecution.ErrLaunchUncertain
	ErrPrototypeProcessOwnership           = prototypeexecution.ErrProcessOwnership
	ErrPrototypeWorktreePreparation        = prototypeexecution.ErrWorktreePreparation
	ErrPrototypeEphemeralTarget            = prototypeexecution.ErrEphemeralTarget
	ErrPrototypeLease                      = prototypeexecution.ErrLease
	ErrPrototypeWorkingDirectory           = prototypeexecution.ErrWorkingDirectory
	ErrPrototypeInvocation                 = prototypeexecution.ErrInvocation
	ErrPrototypeCancellation               = prototypeexecution.ErrCancellation
	ErrPrototypeTimeout                    = prototypeexecution.ErrTimeout
	ErrPrototypeResultInvalid              = prototypeexecution.ErrResultInvalid
	ErrPrototypeEvidenceUnsafe             = prototypeexecution.ErrEvidenceUnsafe
	ErrPrototypeEvidenceMissing            = prototypeexecution.ErrEvidenceMissing
	ErrPrototypeCleanupRequired            = prototypeexecution.ErrCleanupRequired
	ErrPrototypeLimitsInvalid              = prototypeexecution.ErrLimitsInvalid
	ErrPrototypeReconciliationIncomplete   = prototypeexecution.ErrReconciliationIncomplete
	ErrPrototypeCleanupOwnershipMismatch   = prototypeexecution.ErrCleanupOwnershipMismatch
	ErrPrototypeQAPacketInvalid            = prototypeexecution.ErrQAPacketInvalid
	ErrPrototypeQAEvidenceInvalid          = prototypeexecution.ErrQAEvidenceInvalid
	ErrPrototypeAnotherExecutionIneligible = prototypeexecution.ErrAnotherExecutionIneligible
)

type prototypeInvocationEnvelope struct {
	ProposedRunID, ProposalID, SourceClosureID, RepoTarget, BaseCommit, Adapter, Model string
	Variants, Evidence                                                                 []string
	Limits                                                                             map[string]any
}
type prototypeApprovalRequest struct {
	WorkspaceID, RunID, ProposalID, AuthorizationID, InvocationSHA256, MutationIdentity, OperatorConfirmationEvidence string
	ExpectedRunVersion                                                                                                int64
}

func prototypeDigest(v any) string {
	b, _ := json.Marshal(v)
	return prototypeSHA256(b)
}
func prototypeSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func prototypeInvocationEnvelopeBytes(v prototypeInvocationEnvelope) ([]byte, error) {
	return json.Marshal(v)
}
func prototypeInvocationEnvelopeFromAuthorization(authorization workflowstore.PrototypeAuthorization, proposalID, sourceClosureID string) (prototypeInvocationEnvelope, error) {
	var variants, evidence []string
	var limits map[string]any
	if err := json.Unmarshal([]byte(authorization.VariantsJSON), &variants); err != nil {
		return prototypeInvocationEnvelope{}, err
	}
	if err := json.Unmarshal([]byte(authorization.EvidenceObligationsJSON), &evidence); err != nil {
		return prototypeInvocationEnvelope{}, err
	}
	if err := json.Unmarshal([]byte(authorization.LimitsJSON), &limits); err != nil {
		return prototypeInvocationEnvelope{}, err
	}
	return prototypeInvocationEnvelope{ProposedRunID: authorization.ProposedRunID, ProposalID: proposalID, SourceClosureID: sourceClosureID, RepoTarget: authorization.RepoTarget, BaseCommit: authorization.BaseCommit, Adapter: authorization.Adapter, Model: authorization.Model, Variants: variants, Evidence: evidence, Limits: limits}, nil
}
func prototypeArtifactError(err error) error {
	if err == nil {
		return nil
	}
	for _, typed := range []error{ErrPrototypeCapabilityDisabled, ErrPrototypeWorkspaceStale, ErrPrototypeWorkItemStale, ErrPrototypeProposalMissing, ErrPrototypeAuthorizationMissing, ErrPrototypeSourceDivergence, ErrPrototypeOwnership} {
		if errors.Is(err, typed) {
			return err
		}
	}
	return fmt.Errorf("%w: %v", ErrPrototypeArtifactPersistence, err)
}

func (s *Service) PreparePrototypeProposal(ctx context.Context, in PreparePrototypeProposalInput) (workflowstore.PrototypeProposal, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.WorkItemID) == "" || in.ExpectedWorkspaceVersion < 1 || in.ExpectedWorkItemVersion < 1 || len(in.Proposal) == 0 || strings.TrimSpace(in.MediaType) == "" {
		return workflowstore.PrototypeProposal{}, ErrPrototypeProposalMissing
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.PrototypeProposal{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return workflowstore.PrototypeProposal{}, err
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return workflowstore.PrototypeProposal{}, ErrPrototypeCapabilityDisabled
	}
	artifactID := workflowstore.NewFeatureWorkspaceDiscoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return workflowstore.PrototypeProposal{}, fmt.Errorf("%w: %v", ErrPrototypeArtifactPersistence, err)
	}
	file, err := batch.Stage("prototype_proposal", "proposal.bin", strings.TrimSpace(in.MediaType), in.Proposal)
	if err != nil {
		_ = batch.Rollback()
		return workflowstore.PrototypeProposal{}, fmt.Errorf("%w: %v", ErrPrototypeArtifactPersistence, err)
	}
	var result workflowstore.PrototypeProposal
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, e := tx.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
		if e != nil {
			return e
		}
		if current.DiscoveryCapabilityEnabled != 1 {
			return ErrPrototypeCapabilityDisabled
		}
		if current.Version != in.ExpectedWorkspaceVersion {
			return ErrPrototypeWorkspaceStale
		}
		ticket, e := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, in.WorkItemID)
		if e != nil {
			return ErrPrototypeProposalMissing
		}
		if ticket.WorkspaceRowID != current.ID {
			return ErrPrototypeOwnership
		}
		if ticket.Version != in.ExpectedWorkItemVersion {
			return ErrPrototypeWorkItemStale
		}
		rev, e := tx.GetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID)
		if e != nil {
			return ErrPrototypeSourceDivergence
		}
		artifact, e := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if e != nil {
			return e
		}
		result, e = tx.CreatePrototypeProposal(ctx, workflowstore.PrototypeProposal{ProposalID: workflowstore.NewPrototypeProposalID(), WorkspaceRowID: current.ID, WorkItemRowID: ticket.ID, DiscoveryRevisionRowID: rev.ID, ArtifactRowID: artifact.ID, ProposalSHA256: file.SHA256, ProposalSizeBytes: file.SizeBytes, ProposalMediaType: file.MediaType})
		return e
	})
	if err != nil {
		return workflowstore.PrototypeProposal{}, prototypeArtifactError(err)
	}
	return result, nil
}

func (s *Service) PreparePrototypeExecution(ctx context.Context, in PreparePrototypeExecutionInput) (workflowstore.PrototypeAuthorization, workflowstore.PrototypeRun, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.WorkItemID) == "" || strings.TrimSpace(in.ProposalID) == "" || strings.TrimSpace(in.SourceClosureID) == "" || strings.TrimSpace(in.RepoTarget) == "" || strings.TrimSpace(in.BaseCommit) == "" || strings.TrimSpace(in.Adapter) == "" || strings.TrimSpace(in.Model) == "" || in.ExpectedWorkspaceVersion < 1 || in.ExpectedWorkItemVersion < 1 {
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, ErrPrototypeAuthorizationMissing
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
	if err != nil {
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, err
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, ErrPrototypeCapabilityDisabled
	}
	proposedRunID := workflowstore.NewPrototypeRunID()
	envelope, err := prototypeInvocationEnvelopeBytes(prototypeInvocationEnvelope{ProposedRunID: proposedRunID, ProposalID: in.ProposalID, SourceClosureID: in.SourceClosureID, RepoTarget: in.RepoTarget, BaseCommit: in.BaseCommit, Adapter: in.Adapter, Model: in.Model, Variants: in.Variants, Evidence: in.EvidenceObligations, Limits: in.Limits})
	if err != nil {
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, ErrPrototypeAuthorizationMissing
	}
	artifactID := workflowstore.NewFeatureWorkspaceDiscoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, fmt.Errorf("%w: %v", ErrPrototypeArtifactPersistence, err)
	}
	file, err := batch.Stage("prototype_invocation", "invocation.json", "application/vnd.relay.prototype-invocation+json", envelope)
	if err != nil {
		_ = batch.Rollback()
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, fmt.Errorf("%w: %v", ErrPrototypeArtifactPersistence, err)
	}
	var authorization workflowstore.PrototypeAuthorization
	var run workflowstore.PrototypeRun
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, e := tx.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
		if e != nil {
			return e
		}
		if current.DiscoveryCapabilityEnabled != 1 {
			return ErrPrototypeCapabilityDisabled
		}
		if current.Version != in.ExpectedWorkspaceVersion {
			return ErrPrototypeWorkspaceStale
		}
		ticket, e := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, in.WorkItemID)
		if e != nil {
			return ErrPrototypeProposalMissing
		}
		if ticket.WorkspaceRowID != current.ID {
			return ErrPrototypeOwnership
		}
		if ticket.Version != in.ExpectedWorkItemVersion {
			return ErrPrototypeWorkItemStale
		}
		proposal, e := tx.GetPrototypeProposal(ctx, in.ProposalID)
		if e != nil {
			return ErrPrototypeProposalMissing
		}
		if proposal.WorkspaceRowID != current.ID || proposal.WorkItemRowID != ticket.ID {
			return ErrPrototypeOwnership
		}
		rev, e := tx.GetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID)
		if e != nil || proposal.DiscoveryRevisionRowID != rev.ID {
			return ErrPrototypeProposalMissing
		}
		closure, e := tx.GetSourceVaultClosureByClosureID(ctx, in.SourceClosureID)
		if e != nil || closure.State != "ready" {
			return ErrPrototypeSourceDivergence
		}
		vault, e := tx.GetSourceVaultByRepositoryTarget(ctx, in.RepoTarget)
		if e != nil || vault.ID != closure.VaultRowID || in.BaseCommit != closure.CommitOID {
			return ErrPrototypeSourceDivergence
		}
		artifact, e := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if e != nil {
			return e
		}
		variants, _ := json.Marshal(in.Variants)
		evidence, _ := json.Marshal(in.EvidenceObligations)
		limits, _ := json.Marshal(in.Limits)
		authorization, e = tx.CreatePrototypeAuthorization(ctx, workflowstore.PrototypeAuthorization{AuthorizationID: workflowstore.NewPrototypeAuthorizationID(), ProposalRowID: proposal.ID, ProposedRunID: proposedRunID, WorkspaceRowID: current.ID, WorkspaceVersion: current.Version, WorkItemRowID: ticket.ID, WorkItemVersion: ticket.Version, DiscoveryRevisionRowID: rev.ID, ProposalSHA256: proposal.ProposalSHA256, SourceClosureRowID: closure.ID, SourceCommit: closure.CommitOID, SourceTree: closure.TreeOID, RepoTarget: in.RepoTarget, BaseCommit: in.BaseCommit, Adapter: in.Adapter, Model: in.Model, VariantsJSON: string(variants), EvidenceObligationsJSON: string(evidence), LimitsJSON: string(limits), InvocationArtifactRowID: artifact.ID, InvocationSHA256: file.SHA256, InvocationSizeBytes: file.SizeBytes, InvocationMediaType: file.MediaType})
		if e != nil {
			return e
		}
		run, e = tx.CreatePrototypeRun(ctx, workflowstore.PrototypeRun{PrototypeRunID: proposedRunID, AuthorizationRowID: authorization.ID, WorkspaceRowID: current.ID, WorkItemRowID: ticket.ID})
		return e
	})
	if err != nil {
		return workflowstore.PrototypeAuthorization{}, workflowstore.PrototypeRun{}, prototypeArtifactError(err)
	}
	return authorization, run, nil
}

func (s *Service) ApprovePrototypeExecution(ctx context.Context, in ApprovePrototypeExecutionInput) (workflowstore.PrototypeApproval, workflowstore.PrototypeRun, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.RunID) == "" || strings.TrimSpace(in.ProposalID) == "" || strings.TrimSpace(in.AuthorizationID) == "" || !validSHA256(in.InvocationSHA256) || strings.TrimSpace(in.MutationIdentity) == "" || strings.TrimSpace(in.OperatorConfirmationEvidence) == "" || in.ExpectedRunVersion < 1 {
		return workflowstore.PrototypeApproval{}, workflowstore.PrototypeRun{}, ErrPrototypeApprovalMissing
	}
	requestDigest := prototypeDigest(prototypeApprovalRequest{in.WorkspaceID, in.RunID, in.ProposalID, in.AuthorizationID, in.InvocationSHA256, in.MutationIdentity, in.OperatorConfirmationEvidence, in.ExpectedRunVersion})
	var approval workflowstore.PrototypeApproval
	var run workflowstore.PrototypeRun
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var e error
		run, e = tx.GetPrototypeRun(ctx, in.RunID)
		if e != nil {
			return ErrPrototypeApprovalMissing
		}
		existing, e := tx.GetPrototypeApprovalByMutationIdentity(ctx, in.MutationIdentity)
		if e == nil {
			if existing.RunRowID == run.ID && existing.ApprovalRequestSHA256 == requestDigest {
				approval = existing
				return nil
			}
			return ErrPrototypeApprovalConflicting
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if existing, e = tx.GetPrototypeApprovalByRun(ctx, run.ID); e == nil {
			return ErrPrototypeApprovalConflicting
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		workspace, e := tx.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
		if e != nil {
			return ErrPrototypeApprovalMissing
		}
		if workspace.DiscoveryCapabilityEnabled != 1 {
			return ErrPrototypeCapabilityDisabled
		}
		if run.WorkspaceRowID != workspace.ID {
			return ErrPrototypeOwnership
		}
		if run.LifecycleState != "proposed" {
			return ErrPrototypeInvalidTransition
		}
		if run.Version != in.ExpectedRunVersion {
			return ErrPrototypeRunStale
		}
		authorization, e := tx.GetPrototypeAuthorization(ctx, in.AuthorizationID)
		if e != nil || authorization.ID != run.AuthorizationRowID || authorization.ProposedRunID != run.PrototypeRunID {
			return ErrPrototypeAuthorizationMissing
		}
		if workspace.Version != authorization.WorkspaceVersion {
			return ErrPrototypeWorkspaceStale
		}
		ticket, e := tx.GetFeatureWorkspaceDiscoveryTicketByRowID(ctx, authorization.WorkItemRowID)
		if e != nil || ticket.WorkspaceRowID != workspace.ID {
			return ErrPrototypeOwnership
		}
		if ticket.Version != authorization.WorkItemVersion {
			return ErrPrototypeWorkItemStale
		}
		proposal, e := tx.GetPrototypeProposal(ctx, in.ProposalID)
		if e != nil || proposal.ID != authorization.ProposalRowID || proposal.WorkspaceRowID != workspace.ID || proposal.WorkItemRowID != ticket.ID || proposal.DiscoveryRevisionRowID != authorization.DiscoveryRevisionRowID || proposal.ProposalSHA256 != authorization.ProposalSHA256 {
			return ErrPrototypeProposalMissing
		}
		rev, e := tx.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID)
		if e != nil || rev.ID != authorization.DiscoveryRevisionRowID {
			return ErrPrototypeProposalMissing
		}
		artifact, e := tx.GetDiscoveryArtifactByRowID(ctx, authorization.InvocationArtifactRowID)
		if e != nil || artifact.WorkspaceRowID != workspace.ID || artifact.SHA256 != authorization.InvocationSHA256 || artifact.SizeBytes != authorization.InvocationSizeBytes || artifact.MediaType != authorization.InvocationMediaType {
			return ErrPrototypeAuthorizationMissing
		}
		closure, e := tx.GetSourceVaultClosureByRowID(ctx, authorization.SourceClosureRowID)
		if e != nil {
			return ErrPrototypeSourceDivergence
		}
		envelope, e := prototypeInvocationEnvelopeFromAuthorization(authorization, proposal.ProposalID, closure.ClosureID)
		if e != nil {
			return ErrPrototypeAuthorizationMissing
		}
		canonicalEnvelope, e := prototypeInvocationEnvelopeBytes(envelope)
		if e != nil {
			return ErrPrototypeAuthorizationMissing
		}
		canonicalInvocationSHA256 := prototypeSHA256(canonicalEnvelope)
		if canonicalInvocationSHA256 != authorization.InvocationSHA256 || canonicalInvocationSHA256 != in.InvocationSHA256 || canonicalInvocationSHA256 != artifact.SHA256 {
			return ErrPrototypeAuthorizationMissing
		}
		if closure.State != "ready" || closure.CommitOID != authorization.SourceCommit || closure.TreeOID != authorization.SourceTree {
			return ErrPrototypeSourceDivergence
		}
		vault, e := tx.GetSourceVaultByRepositoryTarget(ctx, authorization.RepoTarget)
		if e != nil || vault.ID != closure.VaultRowID || authorization.BaseCommit != closure.CommitOID {
			return ErrPrototypeSourceDivergence
		}
		approval, e = tx.CreatePrototypeApproval(ctx, workflowstore.PrototypeApproval{ApprovalID: workflowstore.NewPrototypeApprovalID(), RunRowID: run.ID, AuthorizationRowID: authorization.ID, MutationIdentity: in.MutationIdentity, ApprovalRequestSHA256: requestDigest, OperatorConfirmationEvidence: strings.TrimSpace(in.OperatorConfirmationEvidence), ConsumedIdentity: "consumed:" + in.MutationIdentity})
		if e != nil {
			return ErrPrototypeApprovalConsumed
		}
		run, e = tx.ApprovePrototypeRun(ctx, in.RunID, in.ExpectedRunVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrPrototypeRunStale
		}
		if e != nil {
			return e
		}
		return tx.CreatePrototypeLifecycleTransition(ctx, run.ID, "transition:"+in.MutationIdentity, approval.ID, run.Version)
	})
	return approval, run, err
}
func (s *Service) ReadPrototypeExecution(ctx context.Context, workspaceID, runID string) (PrototypeExecutionDetail, error) {
	d, e := s.store.ReadPrototypeExecution(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(runID))
	if e != nil {
		return PrototypeExecutionDetail{}, e
	}
	result := PrototypeExecutionDetail{PrototypeExecutionAggregate: d}
	if v, err := s.store.GetPrototypeRuntimeByRunID(ctx, runID); err == nil {
		result.Runtime = &v
	}
	if v, err := s.store.GetPrototypeTargetByRunID(ctx, runID); err == nil {
		result.Target = &v
	}
	if v, err := s.store.GetPrototypeLeaseByRunID(ctx, runID); err == nil {
		result.Lease = &v
	}
	result.EvidenceBatches, _ = s.store.ListPrototypeEvidenceBatches(ctx, runID)
	if v, err := s.store.GetPrototypeResultByRunID(ctx, runID); err == nil {
		result.FinalResult = &v
	}
	result.Evidence, _ = s.store.ListPrototypeEvidenceMembers(ctx, runID)
	return result, nil
}
