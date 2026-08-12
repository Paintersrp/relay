package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/guidedapp"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidAuthorityRequest       = errors.New("invalid feature authority request")
	ErrWorkspaceNotFound             = errors.New("feature workspace not found")
	ErrVersionConflict               = errors.New("feature workspace version conflict")
	ErrFeatureCompletionConfirmation = errors.New("feature completion confirmation is required")
	ErrFeatureCompletionNotReady     = errors.New("feature workspace completion gates are not satisfied")
	ErrFeatureCompletionRecorded     = errors.New("feature workspace completion is already current")
	ErrApprovalNotFound              = errors.New("governing artifact approval not found")
	ErrApprovalMismatch              = errors.New("governing artifact approval does not match the layer artifact")
	ErrApprovalInvalidated           = errors.New("governing artifact approval has been invalidated")
	ErrInvalidApprovalInput          = errors.New("invalid approval request")
)

type IDGenerator interface {
	AuthorityRevisionID() string
	CompletionDecisionID() string
	GoverningArtifactApprovalID() string
}
type defaultIDGenerator struct{}

func (defaultIDGenerator) AuthorityRevisionID() string {
	return workflowstore.NewFeatureWorkspaceAuthorityRevisionID()
}
func (defaultIDGenerator) CompletionDecisionID() string {
	return workflowstore.NewFeatureWorkspaceCompletionDecisionID()
}
func (defaultIDGenerator) GoverningArtifactApprovalID() string {
	return workflowstore.NewGoverningArtifactApprovalID()
}

type Service struct {
	store             *workflowstore.Store
	ids               IDGenerator
	prototypeExecutor prototypeexecution.Executor
	prototypeCleaner  prototypeexecution.Cleaner
	guidedPackages    guidedapp.PackageOwner
	guidedAudit       guidedapp.AuditOwner
	guidedPrograms    guidedapp.ProgramOwner
	guidedTickets     GuidedTicketOwner

	// reviewContinuations holds the process-local, non-durable planning review
	// continuation per workspace. It is created only after a ready review reads
	// the exact candidate bytes, is consumed exactly once by the workspace-only
	// approval transition, and is cleared by a needs-revision review. It is not
	// a token, receipt, or durable lifecycle record and is never exposed to a
	// client; every consumption revalidates the exact candidate server-side.
	reviewContinuations map[string]*planningReviewContinuation
	reviewMu            sync.Mutex
}

func (s *Service) SetPrototypeExecutor(v prototypeexecution.Executor) error {
	if v == nil {
		return fmt.Errorf("prototype executor is required")
	}
	s.prototypeExecutor = v
	return nil
}
func (s *Service) SetPrototypeExecutorForTest(v prototypeexecution.Executor) { s.prototypeExecutor = v }

func (s *Service) SetPrototypeCleaner(v prototypeexecution.Cleaner) error {
	if v == nil {
		return fmt.Errorf("prototype cleaner is required")
	}
	s.prototypeCleaner = v
	return nil
}

// SetGuidedPackageOwner binds the packages owner used by the guided delivery
// projection and approve action. The owner resolves all package identities
// server-side; the guided boundary never accepts them from a client.
func (s *Service) SetGuidedPackageOwner(v guidedapp.PackageOwner) error {
	if v == nil {
		return fmt.Errorf("guided package owner is required")
	}
	s.guidedPackages = v
	return nil
}
func (s *Service) SetGuidedPackageOwnerForTest(v guidedapp.PackageOwner) { s.guidedPackages = v }

// SetGuidedAuditOwner binds the audits owner used by the guided delivery
// projection and audit/remediation handoffs. The owner resolves all audit and
// remediation identities server-side; the guided boundary never reconstructs
// them from rows.
func (s *Service) SetGuidedAuditOwner(v guidedapp.AuditOwner) error {
	if v == nil {
		return fmt.Errorf("guided audit owner is required")
	}
	s.guidedAudit = v
	return nil
}
func (s *Service) SetGuidedAuditOwnerForTest(v guidedapp.AuditOwner) { s.guidedAudit = v }

func (s *Service) SetGuidedProgramOwner(v guidedapp.ProgramOwner) error {
	if v == nil {
		return fmt.Errorf("guided program owner is required")
	}
	s.guidedPrograms = v
	return nil
}
func (s *Service) SetGuidedProgramOwnerForTest(v guidedapp.ProgramOwner) { s.guidedPrograms = v }

// SetGuidedTicketOwner binds the ticket owner used by the guided delivery
// projection, selection, and production dispatch. The owner must be the exact
// ticket Service instance the server constructs so guided reads observe the
// same process-local planning review continuation that the external auditor
// completion records; the Feature owner never constructs a second ticket
// Service for guided work.
func (s *Service) SetGuidedTicketOwner(v GuidedTicketOwner) error {
	if v == nil {
		return fmt.Errorf("guided ticket owner is required")
	}
	s.guidedTickets = v
	return nil
}
func (s *Service) SetGuidedTicketOwnerForTest(v GuidedTicketOwner) { s.guidedTickets = v }

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{})
}
func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator) (*Service, error) {
	if store == nil || ids == nil {
		return nil, ErrInvalidAuthorityRequest
	}
	return &Service{store: store, ids: ids, reviewContinuations: map[string]*planningReviewContinuation{}}, nil
}

type AuthorityLayerInput struct {
	Kind                   string
	ArtifactRowID          sql.NullInt64
	RetainedArtifact       sql.NullInt64
	CandidateArtifactRowID sql.NullInt64
	ArtifactSHA256         string
	SourceClosureID        sql.NullInt64
	ApprovalRowID          sql.NullInt64
}

type PublishAuthorityInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	SourceClosureID sql.NullInt64
	Layers          []AuthorityLayerInput
}

type AuthorityRevisionDetail struct {
	Revision   workflowstore.FeatureWorkspaceAuthorityRevision
	Layers     []workflowstore.FeatureWorkspaceAuthorityLayer
	Historical bool
}

// PublishAuthority creates immutable replacement history. A workspace may
// deliberately have no authority revision; publication itself requires the
// selected governing layers to be exact, distinct artifacts with valid
// governing-artifact approval references.
func (s *Service) PublishAuthority(ctx context.Context, input PublishAuthorityInput) (AuthorityRevisionDetail, workflowstore.FeatureWorkspace, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || len(input.Layers) == 0 || len(input.Layers) > 3 {
		return AuthorityRevisionDetail{}, workflowstore.FeatureWorkspace{}, ErrInvalidAuthorityRequest
	}
	seen := map[string]bool{}
	for _, layer := range input.Layers {
		if !oneOf(layer.Kind, "requirements", "design", "transition_plan") || seen[layer.Kind] || layer.ArtifactRowID.Valid == layer.RetainedArtifact.Valid || !validSHA256(layer.ArtifactSHA256) || !layer.ApprovalRowID.Valid {
			return AuthorityRevisionDetail{}, workflowstore.FeatureWorkspace{}, ErrInvalidAuthorityRequest
		}
		seen[layer.Kind] = true
	}
	var detail AuthorityRevisionDetail
	var updated workflowstore.FeatureWorkspace
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		prior, err := tx.ListFeatureWorkspaceAuthorityRevisions(ctx, workspace.ID)
		if err != nil {
			return err
		}
		detail.Revision, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: s.ids.AuthorityRevisionID(), WorkspaceRowID: workspace.ID, RevisionNumber: int64(len(prior) + 1), SourceClosureRowID: input.SourceClosureID})
		if err != nil {
			return err
		}
		detail.Layers = make([]workflowstore.FeatureWorkspaceAuthorityLayer, 0, len(input.Layers))
		for sequence, layer := range input.Layers {
			approval, err := tx.GetValidGoverningArtifactApproval(ctx, workflowstore.GetValidGoverningArtifactApprovalParams{
				WorkspaceRowID:        workspace.ID,
				Family:                layer.Kind,
				ArtifactSha256:        layer.ArtifactSHA256,
				ArtifactRowID:         layer.ArtifactRowID,
				RetainedArtifactRowID: layer.RetainedArtifact,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return ErrApprovalNotFound
			}
			if err != nil {
				return err
			}
			if approval.ID != layer.ApprovalRowID.Int64 {
				return ErrApprovalMismatch
			}
			created, err := tx.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{
				AuthorityRevisionRowID: detail.Revision.ID, LayerKind: storageLayerKind(layer.Kind),
				Sequence: int64(sequence + 1), ArtifactRowID: layer.ArtifactRowID,
				RetainedArtifactRowID: layer.RetainedArtifact, CandidateArtifactRowID: layer.CandidateArtifactRowID, ArtifactSha256: layer.ArtifactSHA256,
				SourceClosureRowID: layer.SourceClosureID,
				ApprovalRowID:      sql.NullInt64{Int64: approval.ID, Valid: true},
			})
			if err != nil {
				return err
			}
			detail.Layers = append(detail.Layers, applicationLayerKind(created))
		}
		updated, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, detail.Revision.ID, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return err
		}
		return reopenCurrentFeatureCompletionForAuthority(ctx, tx, updated, detail.Revision)
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrVersionConflict
	}
	return detail, updated, err
}

// RecordAuthorityApproval persists an immutable governing-artifact approval
// bound to its exact workspace, artifact source, family, and SHA-256. The
// approval is a separate operation from authority publication.
func (s *Service) RecordAuthorityApproval(ctx context.Context, input RecordAuthorityApprovalInput) (RecordAuthorityApprovalResult, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" || input.Family == "" || !validSHA256(input.ArtifactSHA256) || input.ArtifactRowID.Valid == input.RetainedArtifact.Valid {
		return RecordAuthorityApprovalResult{}, ErrInvalidApprovalInput
	}
	if !oneOf(input.Family, "requirements", "design", "transition_plan") {
		return RecordAuthorityApprovalResult{}, ErrInvalidApprovalInput
	}
	evidence := strings.TrimSpace(input.OperatorConfirmationEvidence)
	if evidence == "" || len(evidence) > 4096 {
		return RecordAuthorityApprovalResult{}, ErrInvalidApprovalInput
	}
	var result RecordAuthorityApprovalResult
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		approval, err := tx.CreateGoverningArtifactApproval(ctx, workflowstore.CreateGoverningArtifactApprovalParams{
			ApprovalID:                   s.ids.GoverningArtifactApprovalID(),
			WorkspaceRowID:               workspace.ID,
			ArtifactRowID:                input.ArtifactRowID,
			RetainedArtifactRowID:        input.RetainedArtifact,
			Family:                       input.Family,
			ArtifactSha256:               input.ArtifactSHA256,
			OperatorConfirmationEvidence: evidence,
		})
		if err != nil {
			return err
		}
		result = RecordAuthorityApprovalResult{Approval: approval, Workspace: workspace}
		return nil
	})
	return result, err
}

func (s *Service) ReadAuthority(ctx context.Context, workspaceID string) ([]AuthorityRevisionDetail, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidAuthorityRequest
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	if err != nil {
		return nil, err
	}
	revisions, err := s.store.ListFeatureWorkspaceAuthorityRevisions(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	result := make([]AuthorityRevisionDetail, len(revisions))
	for index, revision := range revisions {
		result[index].Revision = revision
		result[index].Historical = !workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentAuthorityRevisionRowID.Int64 != revision.ID
		result[index].Layers, err = s.store.ListFeatureWorkspaceAuthorityLayers(ctx, revision.ID)
		if err != nil {
			return nil, err
		}
		for layerIndex := range result[index].Layers {
			result[index].Layers[layerIndex] = applicationLayerKind(result[index].Layers[layerIndex])
		}
	}
	return result, nil
}

func oneOf(value string, accepted ...string) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}

// P3-T1's durable schema predates the route vocabulary and encodes this one
// layer as "plan". Keep that compatibility detail below the application
// boundary; callers only observe the governing transition_plan authority.
func storageLayerKind(kind string) string {
	if kind == "transition_plan" {
		return "plan"
	}
	if kind == "shared_design" {
		return "design"
	}
	return kind
}

func applicationLayerKind(layer workflowstore.FeatureWorkspaceAuthorityLayer) workflowstore.FeatureWorkspaceAuthorityLayer {
	if layer.LayerKind == "plan" {
		layer.LayerKind = "transition_plan"
	}
	if layer.LayerKind == "design" {
		layer.LayerKind = "shared_design"
	}
	return layer
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

type CompletionInput struct {
	WorkspaceID       string
	ExpectedVersion   int64
	OperatorConfirmed bool
}

type CompletionGate struct {
	Name  string
	Ready bool
}

type CompletionStatus struct {
	Workspace       workflowstore.FeatureWorkspace
	Gates           []CompletionGate
	CurrentDecision *workflowstore.FeatureWorkspaceCompletionDecision
}

type CompletionResult struct {
	Decision  workflowstore.FeatureWorkspaceCompletionDecision
	Workspace workflowstore.FeatureWorkspace
}

// EvaluateCompletion exposes the current gate matrix without creating a
// completion record. Completion itself remains an explicit confirmed action.
func (s *Service) EvaluateCompletion(ctx context.Context, workspaceID string) (CompletionStatus, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return CompletionStatus{}, ErrInvalidAuthorityRequest
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return CompletionStatus{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return CompletionStatus{}, err
	}
	gates, err := s.featureCompletionGates(ctx, s.store, workspace)
	if err != nil {
		return CompletionStatus{}, err
	}
	status := CompletionStatus{Workspace: workspace, Gates: gates}
	if decision, err := s.store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); err == nil {
		status.CurrentDecision = &decision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CompletionStatus{}, err
	}
	return status, nil
}

func (s *Service) Complete(ctx context.Context, input CompletionInput) (CompletionResult, error) {
	return s.recordCompletionDecision(ctx, input, "completed")
}

// Abandon records an immutable abandoned closing decision on the same current
// basis and packet evidence as completion. Abandonment eligibility is exactly
// the completion gate matrix: every authoritative validation, exact discovery
// packet check, and current authority basis check runs before any mutation, and
// the same typed confirmation, version conflict, recorded, and not-ready errors
// are returned. An abandoned decision is not a completion: it is reopened by
// the same source-backed reopen path and never becomes current automatically.
func (s *Service) Abandon(ctx context.Context, input CompletionInput) (CompletionResult, error) {
	return s.recordCompletionDecision(ctx, input, "abandoned")
}

func (s *Service) recordCompletionDecision(ctx context.Context, input CompletionInput, decision string) (CompletionResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.WorkspaceID == "" || input.ExpectedVersion < 1 {
		return CompletionResult{}, ErrInvalidAuthorityRequest
	}
	if !input.OperatorConfirmed {
		return CompletionResult{}, ErrFeatureCompletionConfirmation
	}
	result := CompletionResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		if _, err := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); err == nil {
			return ErrFeatureCompletionRecorded
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		gates, err := s.featureCompletionGates(ctx, tx, workspace)
		if err != nil {
			return err
		}
		if !completionGatesReady(gates) {
			return ErrFeatureCompletionNotReady
		}
		packetAssociation := sql.NullInt64{}
		authorityAssociation := sql.NullInt64{}
		sourceClosureAssociation := sql.NullInt64{}
		if _, err := tx.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); err == nil {
			if !workspace.CurrentDiscoveryClosurePacketRowID.Valid {
				return ErrFeatureCompletionNotReady
			}
			packet, err := tx.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
			if err != nil || packet.WorkspaceRowID != workspace.ID || !workspace.CurrentDiscoveryRevisionRowID.Valid || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 {
				return ErrFeatureCompletionNotReady
			}
			artifact, err := tx.GetDiscoveryArtifactByRowID(ctx, packet.ManifestArtifactRowID)
			if err != nil {
				return ErrFeatureCompletionNotReady
			}
			if _, _, err = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: packet.ManifestSha256, SizeBytes: packet.ManifestSizeBytes, MediaType: packet.ManifestMediaType}, 16<<20); err != nil {
				return ErrFeatureCompletionNotReady
			}
			_, manifest, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType}, 16<<20)
			if err != nil || artifact.WorkspaceRowID != workspace.ID || artifact.SHA256 != packet.ManifestSha256 || artifact.SizeBytes != packet.ManifestSizeBytes || artifact.MediaType != packet.ManifestMediaType {
				return ErrFeatureCompletionNotReady
			}
			members, err := tx.ListDiscoveryClosurePacketMembers(ctx, packet.ID)
			if err != nil || len(members) == 0 {
				return ErrFeatureCompletionNotReady
			}
			for _, member := range members {
				memberArtifact, memberErr := tx.GetDiscoveryArtifactByRowID(ctx, member.ArtifactRowID)
				if memberErr != nil || memberArtifact.WorkspaceRowID != workspace.ID || memberArtifact.SHA256 != member.Sha256 || memberArtifact.SizeBytes != member.SizeBytes || memberArtifact.MediaType != member.MediaType {
					return ErrFeatureCompletionNotReady
				}
				if _, _, memberErr = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: memberArtifact.RelativePath, SHA256: member.Sha256, SizeBytes: member.SizeBytes, MediaType: member.MediaType}, 16<<20); memberErr != nil {
					return ErrFeatureCompletionNotReady
				}
			}
			if err = validateDiscoveryManifest(manifest, packet, members); err != nil {
				return ErrFeatureCompletionNotReady
			}
			assessment, err := s.assessDiscovery(ctx, tx, workspace)
			if err != nil || assessment.State != DiscoveryStateClosed || len(assessment.Blockers) > 0 || len(assessment.PendingIntegrations) > 0 || len(assessment.ActiveOperations) > 0 || len(assessment.RouteMaterialOpen) > 0 || len(assessment.RequiredEvidence) > 0 {
				return ErrFeatureCompletionNotReady
			}
			// A workspace with no planning authority may only close on the
			// no-delivery route: the exact current closed packet must itself
			// declare no_delivery_work. Any other packet requires the explicit
			// authority basis below.
			if !workspace.CurrentAuthorityRevisionRowID.Valid && packet.Destination != string(DiscoveryDestinationNoDeliveryWork) {
				return ErrFeatureCompletionNotReady
			}
			packetAssociation = sql.NullInt64{Int64: packet.ID, Valid: true}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if workspace.CurrentAuthorityRevisionRowID.Valid {
			authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
			if err != nil || !authority.SourceClosureRowID.Valid {
				return ErrFeatureCompletionNotReady
			}
			authorityAssociation = sql.NullInt64{Int64: authority.ID, Valid: true}
			sourceClosureAssociation = authority.SourceClosureRowID
		} else if !packetAssociation.Valid {
			// Without an authority revision and without the current closed
			// discovery packet basis there is no closing basis to record.
			return ErrFeatureCompletionNotReady
		}
		decision, err := tx.CreateFeatureWorkspaceCompletionDecision(ctx, workflowstore.CreateFeatureWorkspaceCompletionDecisionParams{
			CompletionDecisionID:        s.ids.CompletionDecisionID(),
			WorkspaceRowID:              workspace.ID,
			AuthorityRevisionRowID:      authorityAssociation,
			SourceClosureRowID:          sourceClosureAssociation,
			DiscoveryClosurePacketRowID: packetAssociation,
			Decision:                    decision,
		})
		if err != nil {
			return err
		}
		updated, err := tx.BumpFeatureWorkspaceVersion(ctx, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return ErrVersionConflict
		}
		result = CompletionResult{Decision: decision, Workspace: updated}
		return nil
	})
	return result, err
}

type featureCompletionReader interface {
	GetDiscoveryLifecycleAdoption(context.Context, int64) (workflowstore.DiscoveryLifecycleAdoption, error)
	GetDiscoveryClosurePacketByRowID(context.Context, int64) (workflowstore.DiscoveryClosurePacket, error)
	GetCurrentIntegratedDiscoveryRevision(context.Context, string) (workflowstore.IntegratedDiscoveryRevision, error)
	GetDiscoveryArtifactByRowID(context.Context, int64) (workflowstore.DiscoveryArtifact, error)
	ListDiscoveryWorkItemMetadata(context.Context, int64) ([]workflowstore.DiscoveryWorkItemMetadata, error)
	ListDiscoveryIntegrationConsequences(context.Context, int64) ([]workflowstore.DiscoveryIntegrationConsequence, error)
	ListFeatureWorkspaceDiscoveryTickets(context.Context, int64) ([]workflowstore.FeatureWorkspaceDiscoveryTicket, error)
	ListFeatureWorkspaceTicketResolutions(context.Context, int64) ([]workflowstore.FeatureWorkspaceTicketResolution, error)
	ListFeatureWorkspaceTicketDependencies(context.Context, int64) ([]workflowstore.FeatureWorkspaceTicketDependency, error)
	ListFeatureWorkspaceRouteStates(context.Context, int64) ([]workflowstore.FeatureWorkspaceRouteState, error)
	GetFeatureWorkspaceAuthorityRevisionByRowID(context.Context, int64) (workflowstore.FeatureWorkspaceAuthorityRevision, error)
	GetSourceVaultClosureByRowID(context.Context, int64) (workflowstore.SourceVaultClosure, error)
	ListFeatureWorkspaceAuthorityLayers(context.Context, int64) ([]workflowstore.FeatureWorkspaceAuthorityLayer, error)
	ListDeliveryTicketsByWorkspace(context.Context, int64) ([]workflowstore.DeliveryTicket, error)
	GetDeliveryTicketRevisionByRowID(context.Context, int64) (workflowstore.DeliveryTicketRevision, error)
	GetDeliveryTicketRevisionSatisfaction(context.Context, int64) (workflowstore.DeliveryTicketRevisionSatisfaction, error)
	ListDeliveryTicketSelectionsByWorkspace(context.Context, int64) ([]workflowstore.DeliveryTicketSelection, error)
	ListExecutionPackagesByWorkspace(context.Context, int64) ([]workflowstore.ExecutionPackage, error)
	ListAuditRemediationSeedsByWorkspace(context.Context, int64) ([]workflowstore.AuditRemediationSeed, error)
	GetAuditRemediationSeedReopening(context.Context, int64) (workflowstore.AuditRemediationSeedReopening, error)
}

func (s *Service) featureCompletionGates(ctx context.Context, reader featureCompletionReader, workspace workflowstore.FeatureWorkspace) ([]CompletionGate, error) {
	closureReady := true
	noDeliveryPacket := false
	if _, err := reader.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); err == nil {
		closureReady = workspace.CurrentDiscoveryRevisionRowID.Valid && workspace.CurrentDiscoveryClosurePacketRowID.Valid
		if closureReady {
			packet, packetErr := reader.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
			closureReady = packetErr == nil && packet.WorkspaceRowID == workspace.ID && packet.ClosingRevisionRowID == workspace.CurrentDiscoveryRevisionRowID.Int64
			noDeliveryPacket = closureReady && packet.Destination == string(DiscoveryDestinationNoDeliveryWork)
		}
		// The projection must use the same current closed discovery facts as the
		// mutation owner. Otherwise a stale blocker or unresolved route material
		// could advertise a closing action that Complete/Abandon must reject.
		assessment, assessmentErr := s.assessDiscovery(ctx, reader, workspace)
		if assessmentErr != nil {
			// Assessment integrity failures are a closed-basis failure, not an
			// alternate successful projection. The mutating owner likewise
			// reports not-ready for an unverifiable discovery basis.
			closureReady = false
		} else {
			closureReady = closureReady && assessment.State == DiscoveryStateClosed &&
				len(assessment.Blockers) == 0 && len(assessment.PendingIntegrations) == 0 &&
				len(assessment.ActiveOperations) == 0 && len(assessment.RouteMaterialOpen) == 0 &&
				len(assessment.RequiredEvidence) == 0
		}
		noDeliveryPacket = noDeliveryPacket && closureReady
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	authorityReady := workspace.CurrentAuthorityRevisionRowID.Valid
	var authority workflowstore.FeatureWorkspaceAuthorityRevision
	if authorityReady {
		value, err := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if errors.Is(err, sql.ErrNoRows) {
			authorityReady = false
		} else if err != nil {
			return nil, err
		} else {
			authority = value
			if !authority.SourceClosureRowID.Valid {
				authorityReady = false
			} else {
				closure, err := reader.GetSourceVaultClosureByRowID(ctx, authority.SourceClosureRowID.Int64)
				if err != nil {
					return nil, err
				}
				authorityReady = authority.WorkspaceRowID == workspace.ID && closure.State == workflowstore.SourceVaultClosureStateReady
			}
		}
	}

	tickets, err := reader.ListDeliveryTicketsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	ticketsReady, auditReady, transitionsReady := true, true, true
	requiresTransition := false
	for _, ticket := range tickets {
		if !ticket.CurrentRevisionRowID.Valid {
			continue
		}
		revision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
		if err != nil {
			return nil, err
		}
		if revision.CancellationReason.Valid {
			continue
		}
		if _, err := reader.GetDeliveryTicketRevisionSatisfaction(ctx, revision.ID); errors.Is(err, sql.ErrNoRows) {
			ticketsReady, auditReady = false, false
		} else if err != nil {
			return nil, err
		}
		if revision.TransitionApplicability == "required" {
			requiresTransition = true
		}
	}
	if requiresTransition && authorityReady {
		layers, err := reader.ListFeatureWorkspaceAuthorityLayers(ctx, authority.ID)
		if err != nil {
			return nil, err
		}
		transitionsReady = false
		for _, layer := range layers {
			if layer.LayerKind == "plan" || layer.LayerKind == "transition_plan" {
				transitionsReady = true
				break
			}
		}
	}
	if requiresTransition && !authorityReady {
		transitionsReady = false
	}

	selections, err := reader.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	integrationReady := true
	for _, selection := range selections {
		if selection.State == "active" {
			integrationReady = false
			break
		}
	}

	remediationReady := true
	seeds, err := reader.ListAuditRemediationSeedsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, seed := range seeds {
		reopening, err := reader.GetAuditRemediationSeedReopening(ctx, seed.ID)
		if errors.Is(err, sql.ErrNoRows) {
			remediationReady = false
			break
		}
		if err != nil || reopening.ReopeningRevisionRowID < 1 {
			if err != nil {
				return nil, err
			}
			remediationReady = false
			break
		}
		remediationRevision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, reopening.ReopeningRevisionRowID)
		if err != nil {
			return nil, err
		}
		if remediationRevision.CancellationReason.Valid {
			remediationReady = false
			break
		}
	}
	packageReady := true
	packages, err := reader.ListExecutionPackagesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, packageRow := range packages {
		if !workspace.CurrentAuthorityRevisionRowID.Valid || packageRow.AuthorityRevisionRowID != workspace.CurrentAuthorityRevisionRowID.Int64 || packageRow.SourceClosureRowID < 1 {
			packageReady = false
			break
		}
	}
	if !workspace.CurrentAuthorityRevisionRowID.Valid && !authorityReady && noDeliveryPacket && len(tickets) == 0 && len(packages) == 0 && len(seeds) == 0 && integrationReady {
		// No-delivery route: the exact current closed no-delivery discovery
		// packet is the complete closing basis, so the planning authority gate
		// does not apply. The waiver requires the workspace to carry no current
		// authority revision pointer at all and no delivery work of any kind; a
		// present-but-invalid authority projection stays blocked, and
		// delivery-bearing routes keep requiring their explicit authority and
		// evidence.
		authorityReady = true
	}
	currentnessReady := closureReady && authorityReady && packageReady
	return []CompletionGate{
		{Name: "closure", Ready: closureReady},
		{Name: "authority", Ready: authorityReady},
		{Name: "currentness", Ready: currentnessReady},
		{Name: "tickets", Ready: ticketsReady},
		{Name: "integration", Ready: integrationReady},
		{Name: "transitions", Ready: transitionsReady},
		{Name: "package", Ready: packageReady},
		{Name: "remediation", Ready: remediationReady},
		{Name: "audit", Ready: auditReady},
	}, nil
}

func completionGatesReady(gates []CompletionGate) bool {
	for _, gate := range gates {
		if !gate.Ready {
			return false
		}
	}
	return true
}

func reopenCurrentFeatureCompletionForAuthority(
	ctx context.Context,
	tx *workflowstore.Tx,
	workspace workflowstore.FeatureWorkspace,
	authority workflowstore.FeatureWorkspaceAuthorityRevision,
) error {
	completion, err := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.CreateFeatureWorkspaceCompletionReopening(ctx, workflowstore.CreateFeatureWorkspaceCompletionReopeningParams{
		CompletionDecisionRowID:         completion.ID,
		ReopeningKind:                   "authority_revision",
		ReopeningAuthorityRevisionRowID: sql.NullInt64{Int64: authority.ID, Valid: true},
	})
	return err
}
