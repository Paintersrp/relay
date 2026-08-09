package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const (
	CandidateFamilyRequirements   = "requirements"
	CandidateFamilySharedDesign   = "shared_design"
	CandidateFamilyDeliveryTicket = "delivery_ticket"
)

var (
	ErrInvalidCandidateFamily      = errors.New("invalid planning candidate family")
	ErrInvalidCandidateDestination = errors.New("invalid planning candidate destination")
	ErrMissingCurrentClosure       = errors.New("current discovery closure is required")
	ErrStaleCandidateBasis         = errors.New("planning candidate basis is stale")
	ErrCandidateBytesMismatch      = errors.New("planning candidate bytes or digest mismatch")
	ErrCandidateApprovalInvalid    = errors.New("planning candidate approval is invalid")
	ErrCandidateReview             = errors.New("planning candidate review is invalid")
	ErrCandidateReviewIncomplete   = errors.New("planning candidate review is not ready for approval")
	ErrAuthorityConflict           = errors.New("feature authority layer conflicts with candidate")
	ErrAuthorityDuplicate          = errors.New("feature authority layer is already present")
	ErrHistoricalBasis             = errors.New("historical basis cannot authorize progression")
	ErrLegacyCurrentness           = errors.New("legacy workspace has insufficient currentness")
	ErrInvalidCandidateInput       = errors.New("invalid planning candidate request")
)

type CandidateAdmissionInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	Family          string
	Filename        string
	Bytes           []byte
	SHA256          string
	MediaType       string
	RepoTarget      string
	Branch          string
	BaseCommit      string
	Destination     DiscoveryDestination
	CreatedIdentity string
}

type CandidateApprovalInput struct {
	WorkspaceID string
	// Deprecated caller-supplied basis fields are ignored. They remain only so
	// older in-process callers fail closed rather than gaining a forgeable path.
	CandidateID                    string
	ExpectedSHA256                 string
	ExpectedSizeBytes              int64
	Bytes                          []byte
	ExpectedVersion                int64
	ExpectedClosurePacketRowID     sql.NullInt64
	ExpectedAuthorityRevisionRowID sql.NullInt64
	OperatorConfirmationEvidence   string
	CreatedIdentity                string
	continuation                   *planningReviewContinuation
}

type CandidatePromotionInput struct {
	CandidateID     string
	ApprovalID      string
	ExpectedVersion int64
	CreatedIdentity string
}

type CandidateAdmissionResult struct {
	Candidate            workflowstore.PlanningCandidate
	Workspace            workflowstore.FeatureWorkspace
	AuthorizedNextAction string
}

type CandidateApprovalResult struct {
	Approval  workflowstore.PlanningCandidateApproval
	Candidate workflowstore.PlanningCandidate
	Workspace workflowstore.FeatureWorkspace
}

type CandidatePromotionResult struct {
	Detail    AuthorityRevisionDetail
	Workspace workflowstore.FeatureWorkspace
	Candidate workflowstore.PlanningCandidate
	Approval  workflowstore.PlanningCandidateApproval
}

type PlanningCandidateReviewDisposition string

const (
	PlanningCandidateReviewReadyForApproval PlanningCandidateReviewDisposition = "ready_for_approval"
	PlanningCandidateReviewNeedsRevision    PlanningCandidateReviewDisposition = "needs_revision"
)

// CompleteCandidateReviewInput records the bounded disposition of a read-only
// auditor review of an exact immutable planning candidate. CandidateID names
// the immutable candidate the auditor reviewed; the owner verifies the identity
// belongs to the workspace and is still the newest admissible candidate on the
// current closure and authority basis before accepting either disposition.
// ReviewedBytes identifies the exact bytes the auditor reviewed; the owner
// recalculates their SHA-256 and requires them to match the verified stored
// candidate bytes, digest, and size before either disposition is accepted. No
// findings or prose are accepted or persisted.
type CompleteCandidateReviewInput struct {
	WorkspaceID      string
	CandidateID      string
	ReviewerIdentity string
	Disposition      PlanningCandidateReviewDisposition
	ReviewedBytes    []byte
}

type CompleteCandidateReviewResult struct {
	Candidate   workflowstore.PlanningCandidate
	Disposition PlanningCandidateReviewDisposition
	Review      PlanningCandidateReviewCompletion
	Refresh     *PlanningReviewRefresh
}

type PlanningCandidateReviewCompletion struct{ ReviewerIdentity, Disposition string }

// PlanningReviewRefresh is an immediate exact-input handoff for a rejected
// candidate. It is response-only and deliberately has no durable identity.
type PlanningReviewRefresh struct {
	OperationID, AuditorReviewResult string
	ReviewedCandidate                []byte
}

// planningReviewContinuation is deliberately private and process-local. It is
// created only after the owner reads the exact candidate bytes during a ready
// review, is held only by the Service keyed by workspace, and is consumed
// exactly once by the distinct workspace-only approval transition. It is not a
// receipt, token, or durable lifecycle record; every consumption revalidates
// the exact candidate, basis, currentness, and version before approval.
type planningReviewContinuation struct {
	workspaceID, candidateID, sha256 string
	sizeBytes                        int64
	closure, authority               sql.NullInt64
	bytes                            []byte
}

// storePlanningReviewContinuation records the exact process-local continuation
// after a ready review, keyed by workspace. A nil continuation clears any prior
// one, which is what a needs-revision review does so it can never be approved.
func (s *Service) storePlanningReviewContinuation(workspaceID string, continuation *planningReviewContinuation) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if continuation == nil {
		delete(s.reviewContinuations, workspaceID)
		return
	}
	s.reviewContinuations[workspaceID] = continuation
}

// consumePlanningReviewContinuation removes and returns the process-local
// continuation for the workspace. Consumption is single-use: once an approval
// transition claims the continuation it is gone, so a later approval attempt
// requires a fresh ready review.
func (s *Service) consumePlanningReviewContinuation(workspaceID string) *planningReviewContinuation {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	continuation := s.reviewContinuations[workspaceID]
	delete(s.reviewContinuations, workspaceID)
	return continuation
}

// hasPlanningReviewContinuationFor reports whether a process-local ready review
// continuation exists for the workspace and still refers to the exact current
// candidate. A newer immutable candidate replaces the reviewed one, so the
// continuation no longer matches and a fresh review is required.
func (s *Service) hasPlanningReviewContinuationFor(workspaceID, candidateID string) bool {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	continuation := s.reviewContinuations[workspaceID]
	return continuation != nil && continuation.candidateID == candidateID
}

func validBaseCommit(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func candidateDestinationAllowed(family string, destination DiscoveryDestination) bool {
	switch family {
	case CandidateFamilyRequirements:
		return destination == DiscoveryDestinationRequirements || destination == DiscoveryDestinationRequirementsThenSharedDesign
	case CandidateFamilySharedDesign:
		return destination == DiscoveryDestinationSharedDesign || destination == DiscoveryDestinationRequirementsThenSharedDesign
	case CandidateFamilyDeliveryTicket:
		return destination == DiscoveryDestinationDirectDeliveryTicket || destination == DiscoveryDestinationRequirements || destination == DiscoveryDestinationSharedDesign || destination == DiscoveryDestinationRequirementsThenSharedDesign || destination == DiscoveryDestinationExistingRouteContinuation
	default:
		return false
	}
}
func candidateFilenameAllowed(family, filename, featureSlug string) bool {
	if filename == "" || strings.TrimSpace(filename) != filename || strings.ContainsAny(filename, `/\\`) {
		return false
	}
	switch family {
	case CandidateFamilyRequirements:
		return featureSlug != "" && filename == featureSlug+".requirements.md"
	case CandidateFamilySharedDesign:
		return featureSlug != "" && filename == featureSlug+".design.md"
	case CandidateFamilyDeliveryTicket:
		info, diagnostics := speccompiler.ParseFilename(filename)
		return len(diagnostics) == 0 && info.Kind == speccompiler.ArtifactDeliveryTicket && strings.HasSuffix(filename, ".delivery-ticket.json")
	default:
		return false
	}
}

// AdmitPlanningCandidate is the Feature-owned admission boundary. It binds
// exact bytes to one current closure and authority basis before durable insert.
func (s *Service) AdmitPlanningCandidate(ctx context.Context, input CandidateAdmissionInput) (CandidateAdmissionResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || len(input.Bytes) == 0 ||
		!oneOf(input.Family, CandidateFamilyRequirements, CandidateFamilySharedDesign, CandidateFamilyDeliveryTicket) ||
		!candidateDestinationAllowed(input.Family, input.Destination) ||
		strings.TrimSpace(input.RepoTarget) == "" || strings.TrimSpace(input.Branch) == "" || !validBaseCommit(input.BaseCommit) ||
		strings.TrimSpace(input.CreatedIdentity) == "" {
		if !oneOf(input.Family, CandidateFamilyRequirements, CandidateFamilySharedDesign, CandidateFamilyDeliveryTicket) {
			return CandidateAdmissionResult{}, ErrInvalidCandidateFamily
		}
		if !candidateDestinationAllowed(input.Family, input.Destination) {
			return CandidateAdmissionResult{}, ErrInvalidCandidateDestination
		}
		return CandidateAdmissionResult{}, ErrInvalidCandidateInput
	}
	calculated := digest(input.Bytes)
	if input.SHA256 != "" && input.SHA256 != calculated {
		return CandidateAdmissionResult{}, ErrCandidateBytesMismatch
	}
	mediaType := input.MediaType
	if mediaType == "" {
		mediaType = "application/json"
		if input.Family != CandidateFamilyDeliveryTicket {
			mediaType = "text/markdown"
		}
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateAdmissionResult{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return CandidateAdmissionResult{}, err
	}
	if !candidateFilenameAllowed(input.Family, input.Filename, workspace.FeatureSlug) {
		return CandidateAdmissionResult{}, ErrInvalidCandidateInput
	}
	artifactID := workflowstore.NewFeatureWorkspaceDiscoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return CandidateAdmissionResult{}, err
	}
	file, err := batch.Stage("planning_candidate_"+input.Family, input.Filename, mediaType, input.Bytes)
	if err != nil {
		_ = batch.Rollback()
		return CandidateAdmissionResult{}, err
	}
	result := CandidateAdmissionResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
		if err != nil {
			return err
		}
		if current.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		if err := requireCurrentnessForProgression(ctx, tx, current); err != nil {
			return err
		}
		if !current.CurrentDiscoveryClosurePacketRowID.Valid || !current.CurrentDiscoveryRevisionRowID.Valid {
			return ErrMissingCurrentClosure
		}
		packet, err := tx.GetDiscoveryClosurePacketByRowID(ctx, current.CurrentDiscoveryClosurePacketRowID.Int64)
		if err != nil || packet.WorkspaceRowID != current.ID || packet.ClosingRevisionRowID != current.CurrentDiscoveryRevisionRowID.Int64 {
			return ErrStaleCandidateBasis
		}
		target, err := tx.GetRepositoryTarget(ctx, input.RepoTarget)
		if err != nil {
			return fmt.Errorf("repository target: %w", err)
		}
		if target.RepoTarget == "" {
			return ErrInvalidCandidateInput
		}
		if packet.Destination != string(input.Destination) {
			return ErrInvalidCandidateDestination
		}
		artifact, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryArtifactParams{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, Sha256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		authority := sql.NullInt64{}
		if current.CurrentAuthorityRevisionRowID.Valid {
			authority = current.CurrentAuthorityRevisionRowID
		}
		candidate, err := tx.CreatePlanningCandidate(ctx, workflowstore.CreatePlanningCandidateParams{
			CandidateID: workflowstore.NewPlanningCandidateID(), WorkspaceRowID: current.ID, Family: input.Family, Filename: input.Filename,
			ArtifactRowID: artifact.ID, ArtifactSha256: file.SHA256, ArtifactSizeBytes: file.SizeBytes,
			DiscoveryClosurePacketRowID: packet.ID, AuthorityRevisionRowID: authority, RepoTarget: target.RepoTarget,
			Branch: input.Branch, BaseCommit: input.BaseCommit, Destination: string(input.Destination), CreatedIdentity: strings.TrimSpace(input.CreatedIdentity),
		})
		if err != nil {
			return ErrStaleCandidateBasis
		}
		result = CandidateAdmissionResult{Candidate: candidate, Workspace: current, AuthorizedNextAction: "review_candidate"}
		return nil
	})
	return result, err
}

func requireCurrentnessForProgression(ctx context.Context, reader CurrentnessReader, workspace workflowstore.FeatureWorkspace) error {
	decision, err := EvaluateCurrentness(ctx, reader, workspace.WorkspaceID)
	if err != nil {
		return err
	}
	switch decision.Readiness {
	case FeatureLegacy:
		return ErrLegacyCurrentness
	case FeatureStale:
		// Candidate planning may establish the first authority revision before
		// one exists, and that candidate-owned authority intentionally has no
		// source-closure pointer until a source-backed authority is published.
		// The more specific candidate-basis checks below retain their
		// historical-closure errors. Once an authority points at a source
		// closure, however, an unavailable source or invalid pointer must not
		// authorize a new candidate, approval, or promotion.
		if decision.StaleOwner == "discovery_closure" {
			return nil
		}
		if decision.StaleOwner == "authority" && workspace.CurrentAuthorityRevisionRowID.Valid {
			authority, authorityErr := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
			if authorityErr == nil && authority.WorkspaceRowID == workspace.ID && !authority.SourceClosureRowID.Valid {
				return nil
			}
		}
		return ErrStaleCandidateBasis
	default:
		return nil
	}
}

func currentPlanningCandidateBasis(ctx context.Context, tx *workflowstore.Tx, candidate workflowstore.PlanningCandidate, workspace workflowstore.FeatureWorkspace) error {
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || !workspace.CurrentDiscoveryRevisionRowID.Valid || candidate.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 || !sameNullableInt64(candidate.AuthorityRevisionRowID, workspace.CurrentAuthorityRevisionRowID) {
		return ErrStaleCandidateBasis
	}
	packet, err := tx.GetDiscoveryClosurePacketByRowID(ctx, candidate.DiscoveryClosurePacketRowID)
	if err != nil || packet.WorkspaceRowID != workspace.ID || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 || packet.Destination != candidate.Destination {
		return ErrStaleCandidateBasis
	}
	return nil
}

// currentExactPlanningCandidate prevents an older candidate on the same
// closure and authority basis from retaining review authority after a newer
// immutable replacement is admitted.
func currentExactPlanningCandidate(ctx context.Context, tx *workflowstore.Tx, candidate workflowstore.PlanningCandidate, workspace workflowstore.FeatureWorkspace) error {
	candidates, err := tx.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return err
	}
	for index := len(candidates) - 1; index >= 0; index-- {
		current := candidates[index]
		if current.Family != candidate.Family || current.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 || !sameNullableInt64(current.AuthorityRevisionRowID, workspace.CurrentAuthorityRevisionRowID) {
			continue
		}
		if current.ID != candidate.ID {
			return ErrStaleCandidateBasis
		}
		return nil
	}
	return ErrStaleCandidateBasis
}

// AdmitCandidate is a concise alias retained for application callers.
func (s *Service) AdmitCandidate(ctx context.Context, input CandidateAdmissionInput) (CandidateAdmissionResult, error) {
	return s.AdmitPlanningCandidate(ctx, input)
}

type PlanningCandidateRead struct {
	Candidate  workflowstore.PlanningCandidate
	Historical bool
}

func (s *Service) ReadPlanningCandidates(ctx context.Context, workspaceID string) ([]workflowstore.PlanningCandidate, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
}

func (s *Service) ReadPlanningCandidate(ctx context.Context, workspaceID, candidateID string) (PlanningCandidateRead, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return PlanningCandidateRead{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return PlanningCandidateRead{}, err
	}
	candidate, err := s.store.GetPlanningCandidateByCandidateID(ctx, strings.TrimSpace(candidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return PlanningCandidateRead{}, ErrCandidateApprovalInvalid
	}
	if err != nil {
		return PlanningCandidateRead{}, err
	}
	if candidate.WorkspaceRowID != workspace.ID {
		return PlanningCandidateRead{}, ErrCandidateApprovalInvalid
	}
	return PlanningCandidateRead{Candidate: candidate, Historical: s.planningCandidateHistorical(ctx, workspace, candidate)}, nil
}

func (s *Service) planningCandidateHistorical(ctx context.Context, workspace workflowstore.FeatureWorkspace, candidate workflowstore.PlanningCandidate) bool {
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || !workspace.CurrentDiscoveryRevisionRowID.Valid || candidate.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 || !sameNullableInt64(candidate.AuthorityRevisionRowID, workspace.CurrentAuthorityRevisionRowID) {
		return true
	}
	packet, err := s.store.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil || packet.WorkspaceRowID != workspace.ID || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 {
		return true
	}
	if workspace.CurrentAuthorityRevisionRowID.Valid {
		authority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil || authority.WorkspaceRowID != workspace.ID {
			return true
		}
	}
	return false
}

func sameNullableInt64(left, right sql.NullInt64) bool {
	return left.Valid == right.Valid && (!left.Valid || left.Int64 == right.Int64)
}

func (s *Service) ApprovePlanningCandidate(ctx context.Context, input CandidateApprovalInput) (CandidateApprovalResult, error) {
	evidence := strings.TrimSpace(input.OperatorConfirmationEvidence)
	continuation := input.continuation
	if continuation == nil || strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || strings.TrimSpace(input.CreatedIdentity) == "" || evidence == "" {
		return CandidateApprovalResult{}, ErrCandidateApprovalInvalid
	}
	var result CandidateApprovalResult
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if err != nil {
			return err
		}
		if workspace.WorkspaceID != continuation.workspaceID || workspace.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		candidate, err := tx.GetPlanningCandidateByCandidateID(ctx, continuation.candidateID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCandidateApprovalInvalid
		}
		if err != nil {
			return err
		}
		if !oneOf(candidate.Family, CandidateFamilyRequirements, CandidateFamilySharedDesign, CandidateFamilyDeliveryTicket) {
			return ErrInvalidCandidateFamily
		}
		if candidate.WorkspaceRowID != workspace.ID {
			return ErrCandidateApprovalInvalid
		}
		if err := requireCurrentnessForProgression(ctx, tx, workspace); err != nil {
			return err
		}
		if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || !workspace.CurrentDiscoveryRevisionRowID.Valid || !sameNullableInt64(workspace.CurrentDiscoveryClosurePacketRowID, continuation.closure) {
			return ErrHistoricalBasis
		}
		if !sameNullableInt64(workspace.CurrentAuthorityRevisionRowID, continuation.authority) {
			return ErrStaleCandidateBasis
		}
		if err := currentPlanningCandidateBasis(ctx, tx, candidate, workspace); err != nil {
			return ErrHistoricalBasis
		}
		if err := currentExactPlanningCandidate(ctx, tx, candidate, workspace); err != nil {
			return err
		}
		if candidate.ArtifactSha256 != continuation.sha256 || candidate.ArtifactSizeBytes != continuation.sizeBytes {
			return ErrCandidateBytesMismatch
		}
		stored, err := tx.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, len(continuation.bytes))
		if err != nil || !equalBytes(stored, continuation.bytes) || digest(stored) != candidate.ArtifactSha256 {
			return ErrCandidateBytesMismatch
		}
		approval, err := tx.CreatePlanningCandidateApproval(ctx, workflowstore.CreatePlanningCandidateApprovalParams{ApprovalID: workflowstore.NewPlanningCandidateApprovalID(), CandidateRowID: candidate.ID, CandidateArtifactRowID: candidate.ArtifactRowID, CandidateSha256: candidate.ArtifactSha256, CandidateSizeBytes: candidate.ArtifactSizeBytes, OperatorConfirmationEvidence: evidence, CreatedIdentity: strings.TrimSpace(input.CreatedIdentity)})
		if err != nil {
			return ErrCandidateApprovalInvalid
		}
		result = CandidateApprovalResult{Approval: approval, Candidate: candidate, Workspace: workspace}
		return nil
	})
	return result, err
}

// ApproveCurrentPlanningCandidate is the workspace-only explicit approval
// transition. It consumes the exact process-local continuation stored by a
// ready review and reuses the full exact/currentness/version/bytes validation
// before approval. The client supplies no candidate identity or digest; the
// continuation is held only by the Service and is revalidated on consume.
func (s *Service) ApproveCurrentPlanningCandidate(ctx context.Context, input CandidateApprovalInput) (CandidateApprovalResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" {
		return CandidateApprovalResult{}, ErrCandidateApprovalInvalid
	}
	continuation := s.consumePlanningReviewContinuation(strings.TrimSpace(input.WorkspaceID))
	if continuation == nil {
		return CandidateApprovalResult{}, ErrCandidateReviewIncomplete
	}
	input.continuation = continuation
	return s.ApprovePlanningCandidate(ctx, input)
}
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CompletePlanningCandidateReview validates that the reviewed bytes and the
// supplied candidate identity identify the exact immutable current candidate,
// then returns a transient review result. The server recalculates the SHA-256
// of the supplied reviewed bytes and requires byte-for-byte and digest
// equality with the verified stored artifact of the identified candidate, which
// must belong to the workspace, match the current closure and authority basis,
// and still be the newest admissible candidate. A replaced, stale-basis, or
// otherwise changed candidate is rejected and no result ever attaches to the
// replacement, even when the replacement bytes are identical. It writes no
// durable review state: a ready review stores only the exact process-local
// continuation on the Service for the distinct workspace-only approval
// transition, and a needs-revision review clears any continuation and returns
// the exact planner refresh handoff.
func (s *Service) CompletePlanningCandidateReview(ctx context.Context, input CompleteCandidateReviewInput) (CompleteCandidateReviewResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.CandidateID) == "" || strings.TrimSpace(input.ReviewerIdentity) == "" || len(input.ReviewedBytes) == 0 ||
		(input.Disposition != PlanningCandidateReviewReadyForApproval && input.Disposition != PlanningCandidateReviewNeedsRevision) {
		return CompleteCandidateReviewResult{}, ErrCandidateReview
	}
	result := CompleteCandidateReviewResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || !workspace.CurrentDiscoveryRevisionRowID.Valid {
			return ErrMissingCurrentClosure
		}
		packet, err := tx.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
		if err != nil || packet.WorkspaceRowID != workspace.ID || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 {
			return ErrStaleCandidateBasis
		}
		candidate, err := tx.GetPlanningCandidateByCandidateID(ctx, strings.TrimSpace(input.CandidateID))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no such planning candidate in this workspace", ErrCandidateReview)
		}
		if err != nil {
			return err
		}
		// The reviewed identity must belong to the workspace: a candidate from
		// another workspace can never receive a review result here.
		if candidate.WorkspaceRowID != workspace.ID {
			return fmt.Errorf("%w: planning candidate does not belong to this workspace", ErrCandidateReview)
		}
		if !oneOf(candidate.Family, CandidateFamilyRequirements, CandidateFamilySharedDesign, CandidateFamilyDeliveryTicket) {
			return ErrInvalidCandidateFamily
		}
		// The immutable candidate must carry the family and destination the
		// closed discovery authorizes and must be bound to the workspace's
		// current closure and authority basis.
		if candidate.Destination != packet.Destination || !oneOf(candidate.Family, guidedCandidateFamiliesForDestination(DiscoveryDestination(packet.Destination))...) {
			return fmt.Errorf("%w: planning candidate is not reviewable for this destination", ErrCandidateReview)
		}
		if candidate.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 || !sameNullableInt64(candidate.AuthorityRevisionRowID, workspace.CurrentAuthorityRevisionRowID) {
			return ErrStaleCandidateBasis
		}
		// A newer immutable candidate on the same basis replaces the reviewed
		// one: a review of the older artifact is rejected even when the bytes
		// are byte-for-byte identical, so no result, continuation, or refresh
		// can attach to the replacement.
		if err := currentExactPlanningCandidate(ctx, tx, candidate, workspace); err != nil {
			return err
		}
		approvals, err := tx.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
		if err != nil {
			return err
		}
		if len(approvals) > 0 {
			return fmt.Errorf("%w: the current candidate is already approved", ErrCandidateReview)
		}
		bytes, err := tx.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, int(candidate.ArtifactSizeBytes))
		if err != nil || digest(bytes) != candidate.ArtifactSha256 || int64(len(bytes)) != candidate.ArtifactSizeBytes {
			return ErrCandidateBytesMismatch
		}
		if digest(input.ReviewedBytes) != candidate.ArtifactSha256 || !equalBytes(input.ReviewedBytes, bytes) {
			return ErrCandidateBytesMismatch
		}
		result = CompleteCandidateReviewResult{Candidate: candidate, Disposition: input.Disposition, Review: PlanningCandidateReviewCompletion{ReviewerIdentity: strings.TrimSpace(input.ReviewerIdentity), Disposition: string(input.Disposition)}}
		if input.Disposition == PlanningCandidateReviewReadyForApproval {
			s.storePlanningReviewContinuation(workspace.WorkspaceID, &planningReviewContinuation{workspaceID: workspace.WorkspaceID, candidateID: candidate.CandidateID, sha256: candidate.ArtifactSha256, sizeBytes: candidate.ArtifactSizeBytes, closure: workspace.CurrentDiscoveryClosurePacketRowID, authority: workspace.CurrentAuthorityRevisionRowID, bytes: append([]byte(nil), input.ReviewedBytes...)})
		} else {
			// A needs-revision review clears any prior ready continuation
			// so it can never be approved, and preserves the exact
			// planner refresh handoff for a replacement candidate.
			s.storePlanningReviewContinuation(workspace.WorkspaceID, nil)
			result.Refresh = &PlanningReviewRefresh{OperationID: planningRefreshOperation(candidate.Family), AuditorReviewResult: string(input.Disposition), ReviewedCandidate: append([]byte(nil), input.ReviewedBytes...)}
		}
		return nil
	})
	return result, err
}

func planningRefreshOperation(family string) string {
	switch family {
	case CandidateFamilyRequirements:
		return "planner.requirements"
	case CandidateFamilySharedDesign:
		return "planner.shared_design"
	case CandidateFamilyDeliveryTicket:
		return "planner.delivery_ticket"
	default:
		return ""
	}
}

func (s *Service) PromoteApprovedPlanningCandidate(ctx context.Context, input CandidatePromotionInput) (CandidatePromotionResult, error) {
	if strings.TrimSpace(input.CandidateID) == "" || strings.TrimSpace(input.ApprovalID) == "" || input.ExpectedVersion < 1 {
		return CandidatePromotionResult{}, ErrCandidateApprovalInvalid
	}
	result := CandidatePromotionResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		candidate, err := tx.GetPlanningCandidateByCandidateID(ctx, input.CandidateID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCandidateApprovalInvalid
		}
		if err != nil {
			return err
		}
		if !oneOf(candidate.Family, CandidateFamilyRequirements, CandidateFamilySharedDesign, CandidateFamilyDeliveryTicket) {
			return ErrInvalidCandidateFamily
		}
		workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, candidate.WorkspaceRowID)
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		if err := requireCurrentnessForProgression(ctx, tx, workspace); err != nil {
			return err
		}
		if err := currentPlanningCandidateBasis(ctx, tx, candidate, workspace); err != nil {
			return ErrHistoricalBasis
		}
		if err := currentExactPlanningCandidate(ctx, tx, candidate, workspace); err != nil {
			return err
		}
		if _, err = tx.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, int(candidate.ArtifactSizeBytes)); err != nil {
			return ErrCandidateBytesMismatch
		}
		approval, err := tx.GetPlanningCandidateApprovalByApprovalID(ctx, input.ApprovalID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCandidateApprovalInvalid
		}
		if err != nil {
			return err
		}
		if approval.CandidateRowID != candidate.ID || approval.CandidateArtifactRowID != candidate.ArtifactRowID || approval.CandidateSha256 != candidate.ArtifactSha256 || approval.CandidateSizeBytes != candidate.ArtifactSizeBytes {
			return ErrCandidateApprovalInvalid
		}
		priorLayers := []workflowstore.FeatureWorkspaceAuthorityLayer{}
		if workspace.CurrentAuthorityRevisionRowID.Valid {
			priorLayers, err = tx.ListFeatureWorkspaceAuthorityLayers(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
			if err != nil {
				return err
			}
		}
		for i := range priorLayers {
			priorLayers[i] = applicationLayerKind(priorLayers[i])
		}
		layers := make([]AuthorityLayerInput, 0, 2)
		for _, layer := range priorLayers {
			if layer.LayerKind == candidate.Family || (candidate.Family == CandidateFamilySharedDesign && layer.LayerKind == "design") {
				return ErrAuthorityDuplicate
			}
			layers = append(layers, AuthorityLayerInput{Kind: layer.LayerKind, ArtifactRowID: layer.ArtifactRowID, RetainedArtifact: layer.RetainedArtifactRowID, CandidateArtifactRowID: layer.CandidateArtifactRowID, ArtifactSHA256: layer.ArtifactSha256, SourceClosureID: layer.SourceClosureRowID, ApprovalRowID: layer.ApprovalRowID})
		}
		switch candidate.Family {
		case CandidateFamilyDeliveryTicket:
			return ErrInvalidCandidateFamily
		case CandidateFamilyRequirements:
			if len(layers) != 0 {
				return ErrAuthorityConflict
			}
		case CandidateFamilySharedDesign:
			switch candidate.Destination {
			case string(DiscoveryDestinationSharedDesign):
				if len(layers) == 0 {
					break
				}
				if len(layers) != 1 || layers[0].Kind != CandidateFamilyRequirements {
					return ErrAuthorityConflict
				}
			case string(DiscoveryDestinationRequirementsThenSharedDesign):
				if len(layers) != 1 || layers[0].Kind != CandidateFamilyRequirements {
					return ErrAuthorityConflict
				}
			default:
				return ErrInvalidCandidateDestination
			}
		}
		layers = append(layers, AuthorityLayerInput{Kind: candidate.Family, CandidateArtifactRowID: sql.NullInt64{Int64: candidate.ArtifactRowID, Valid: true}, ArtifactSHA256: candidate.ArtifactSha256})
		candidateSourceClosure, err := planningCandidateSourceClosure(ctx, tx, candidate)
		if err != nil {
			return err
		}
		prior, err := tx.ListFeatureWorkspaceAuthorityRevisions(ctx, workspace.ID)
		if err != nil {
			return err
		}
		revision, err := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: workflowstore.NewFeatureWorkspaceAuthorityRevisionID(), WorkspaceRowID: workspace.ID, RevisionNumber: int64(len(prior) + 1), SourceClosureRowID: sql.NullInt64{Int64: candidateSourceClosure.ID, Valid: true}})
		if err != nil {
			return err
		}
		createdLayers := make([]workflowstore.FeatureWorkspaceAuthorityLayer, 0, len(layers))
		for sequence, layer := range layers {
			created, err := tx.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{AuthorityRevisionRowID: revision.ID, LayerKind: storageLayerKind(layer.Kind), Sequence: int64(sequence + 1), ArtifactRowID: layer.ArtifactRowID, RetainedArtifactRowID: layer.RetainedArtifact, CandidateArtifactRowID: layer.CandidateArtifactRowID, ArtifactSha256: layer.ArtifactSHA256, SourceClosureRowID: layer.SourceClosureID, ApprovalRowID: layer.ApprovalRowID})
			if err != nil {
				return err
			}
			createdLayers = append(createdLayers, applicationLayerKind(created))
		}
		updated, err := tx.SetFeatureWorkspaceAuthorityRevision(ctx, revision.ID, workspace.WorkspaceID, workspace.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if err = reopenCurrentFeatureCompletionForAuthority(ctx, tx, updated, revision); err != nil {
			return err
		}
		result = CandidatePromotionResult{Detail: AuthorityRevisionDetail{Revision: revision, Layers: createdLayers}, Workspace: updated, Candidate: candidate, Approval: approval}
		return nil
	})
	return result, err
}
func planningCandidateSourceClosure(ctx context.Context, tx *workflowstore.Tx, candidate workflowstore.PlanningCandidate) (workflowstore.SourceVaultClosure, error) {
	if candidate.AuthorityRevisionRowID.Valid {
		authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, candidate.AuthorityRevisionRowID.Int64)
		if err != nil || !authority.SourceClosureRowID.Valid {
			return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
		}
		closure, err := tx.GetSourceVaultClosureByRowID(ctx, authority.SourceClosureRowID.Int64)
		if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != candidate.BaseCommit {
			return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
		}
		vault, err := tx.GetSourceVaultByRepositoryTarget(ctx, candidate.RepoTarget)
		if err != nil || vault.ID != closure.VaultRowID {
			return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
		}
		return closure, nil
	}
	closure, err := tx.GetReadySourceVaultClosureByRepositoryTargetAndCommit(ctx, candidate.RepoTarget, candidate.BaseCommit)
	if err != nil {
		return workflowstore.SourceVaultClosure{}, ErrStaleCandidateBasis
	}
	return closure, nil
}

func (s *Service) PromoteCandidate(ctx context.Context, input CandidatePromotionInput) (CandidatePromotionResult, error) {
	return s.PromoteApprovedPlanningCandidate(ctx, input)
}

// PlannerAuthoringEnvelope and AuditorReviewEnvelope are immutable read-only
// compositions. Neither method writes review lifecycle state.
type PlannerAuthoringInput struct{ WorkspaceID, CandidateID string }
type PlannerAuthoringEnvelope struct {
	Workspace        workflowstore.FeatureWorkspace
	Closure          workflowstore.DiscoveryClosurePacket
	Manifest         []byte
	Members          []workflowstore.DiscoveryClosurePacketMember
	Authority        []workflowstore.FeatureWorkspaceAuthorityLayer
	Candidate        workflowstore.PlanningCandidate
	CandidateBytes   []byte
	RepoTarget       workflowstore.RepositoryTarget
	SourceIdentities []string
	Destination      string
	Historical       bool
}
type AuditorReviewInput = PlannerAuthoringInput
type AuditorReviewEnvelope = PlannerAuthoringEnvelope

func (s *Service) ComposePlannerAuthoring(ctx context.Context, input PlannerAuthoringInput) (PlannerAuthoringEnvelope, error) {
	return s.composePlanningEnvelope(ctx, input)
}
func (s *Service) ReadPlannerAuthoring(ctx context.Context, input PlannerAuthoringInput) (PlannerAuthoringEnvelope, error) {
	return s.composePlanningEnvelope(ctx, input)
}
func (s *Service) ComposeAuditorReview(ctx context.Context, input AuditorReviewInput) (AuditorReviewEnvelope, error) {
	return s.composePlanningEnvelope(ctx, input)
}
func (s *Service) ReadAuditorReview(ctx context.Context, input AuditorReviewInput) (AuditorReviewEnvelope, error) {
	return s.composePlanningEnvelope(ctx, input)
}
func (s *Service) composePlanningEnvelope(ctx context.Context, input PlannerAuthoringInput) (PlannerAuthoringEnvelope, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return PlannerAuthoringEnvelope{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return PlannerAuthoringEnvelope{}, err
	}
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || !workspace.CurrentDiscoveryRevisionRowID.Valid {
		return PlannerAuthoringEnvelope{}, ErrMissingCurrentClosure
	}
	packet, err := s.store.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil || packet.WorkspaceRowID != workspace.ID || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 {
		return PlannerAuthoringEnvelope{}, ErrStaleCandidateBasis
	}
	manifestArtifact, err := s.store.GetDiscoveryArtifactByRowID(ctx, packet.ManifestArtifactRowID)
	if err != nil || manifestArtifact.WorkspaceRowID != workspace.ID || manifestArtifact.SHA256 != packet.ManifestSha256 || manifestArtifact.SizeBytes != packet.ManifestSizeBytes || manifestArtifact.MediaType != packet.ManifestMediaType {
		return PlannerAuthoringEnvelope{}, ErrStaleCandidateBasis
	}
	_, manifest, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: manifestArtifact.RelativePath, SHA256: packet.ManifestSha256, SizeBytes: packet.ManifestSizeBytes, MediaType: packet.ManifestMediaType}, 16<<20)
	if err != nil {
		return PlannerAuthoringEnvelope{}, ErrStaleCandidateBasis
	}
	members, err := s.store.ListDiscoveryClosurePacketMembers(ctx, packet.ID)
	if err != nil {
		return PlannerAuthoringEnvelope{}, err
	}
	authority := []workflowstore.FeatureWorkspaceAuthorityLayer{}
	if workspace.CurrentAuthorityRevisionRowID.Valid {
		authority, err = s.store.ListFeatureWorkspaceAuthorityLayers(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil {
			return PlannerAuthoringEnvelope{}, err
		}
		for i := range authority {
			authority[i] = applicationLayerKind(authority[i])
		}
	}
	sourceIdentities := make([]string, 0, len(members))
	for _, member := range members {
		sourceIdentities = append(sourceIdentities, member.SourceIdentity)
	}
	result := PlannerAuthoringEnvelope{Workspace: workspace, Closure: packet, Manifest: manifest, Members: members, Authority: authority, Destination: packet.Destination, SourceIdentities: sourceIdentities}
	if strings.TrimSpace(input.CandidateID) != "" {
		candidate, err := s.store.GetPlanningCandidateByCandidateID(ctx, input.CandidateID)
		if err != nil || candidate.WorkspaceRowID != workspace.ID {
			return PlannerAuthoringEnvelope{}, ErrCandidateApprovalInvalid
		}
		result.Candidate = candidate
		result.Historical = candidate.DiscoveryClosurePacketRowID != workspace.CurrentDiscoveryClosurePacketRowID.Int64 || !sameNullableInt64(candidate.AuthorityRevisionRowID, workspace.CurrentAuthorityRevisionRowID)
		result.CandidateBytes, err = s.store.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, int(candidate.ArtifactSizeBytes))
		if err != nil {
			return PlannerAuthoringEnvelope{}, ErrCandidateBytesMismatch
		}
		result.RepoTarget, err = s.store.GetRepositoryTarget(ctx, candidate.RepoTarget)
		if err != nil {
			return PlannerAuthoringEnvelope{}, err
		}
	}
	return result, nil
}

// Ensure the exact artifact type remains referenced by this owner.
var _ = workflowartifacts.File{}

// Compatibility aliases keep the Feature owner discoverable to callers using
// the shorter candidate vocabulary without creating another application owner.
func (s *Service) ApproveCandidate(ctx context.Context, input CandidateApprovalInput) (CandidateApprovalResult, error) {
	return s.ApprovePlanningCandidate(ctx, input)
}
func (s *Service) PromoteApprovedCandidate(ctx context.Context, input CandidatePromotionInput) (CandidatePromotionResult, error) {
	return s.PromoteApprovedPlanningCandidate(ctx, input)
}
func (s *Service) PublishCandidateAuthority(ctx context.Context, input CandidatePromotionInput) (CandidatePromotionResult, error) {
	return s.PromoteApprovedPlanningCandidate(ctx, input)
}
