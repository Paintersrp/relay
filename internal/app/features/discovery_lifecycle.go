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

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

const discoveryClosureMediaType = "application/vnd.relay.feature-discovery-closure+json"

var (
	ErrDiscoveryUnadopted          = errors.New("discovery lifecycle is not adopted")
	ErrDiscoveryAlreadyAdopted     = errors.New("discovery lifecycle is already adopted")
	ErrDiscoveryNotStarted         = errors.New("discovery has not started")
	ErrDiscoveryAlreadyClosed      = errors.New("discovery is already closed")
	ErrDiscoveryNotClosed          = errors.New("discovery is not closed")
	ErrDiscoveryBlocked            = errors.New("discovery is blocked")
	ErrDiscoveryInvalidDestination = errors.New("invalid discovery destination")
	ErrDiscoveryStalePacket        = errors.New("discovery closure packet is stale")
	ErrDiscoveryReopenConfirmation = errors.New("discovery reopen confirmation is required")
	ErrDiscoveryClosureIneligible  = errors.New("discovery closure is ineligible")
	ErrDiscoveryPendingIntegration = errors.New("discovery integration is pending")
	ErrDiscoveryActiveOperation    = errors.New("discovery operation remains active")
	ErrDiscoveryMemberUnavailable  = errors.New("discovery packet member is unavailable")
	ErrDiscoveryMemberIntegrity    = errors.New("discovery packet member integrity failure")
	ErrDiscoveryManifestIntegrity  = errors.New("discovery packet manifest integrity failure")
	ErrDiscoveryLegacyUnbound      = errors.New("discovery lifecycle is legacy unbound")
	ErrDiscoveryAdoptionProduction = errors.New("discovery adoption is incompatible with active production work")
)

type DiscoveryState string

const (
	DiscoveryStateNotStarted DiscoveryState = "not_started"
	DiscoveryStateActive     DiscoveryState = "active"
	DiscoveryStateBlocked    DiscoveryState = "blocked"
	DiscoveryStateClosed     DiscoveryState = "closed"
)

type DiscoveryDestination string

const (
	DiscoveryDestinationNoDeliveryWork               DiscoveryDestination = "no_delivery_work"
	DiscoveryDestinationDirectDeliveryTicket         DiscoveryDestination = "direct_delivery_ticket"
	DiscoveryDestinationRequirements                 DiscoveryDestination = "requirements"
	DiscoveryDestinationSharedDesign                 DiscoveryDestination = "shared_design"
	DiscoveryDestinationRequirementsThenSharedDesign DiscoveryDestination = "requirements_then_shared_design"
	DiscoveryDestinationExistingRouteContinuation    DiscoveryDestination = "existing_route_continuation"
)

type DiscoveryCurrentness string

const (
	DiscoveryCurrent         DiscoveryCurrentness = "current"
	DiscoveryHistorical      DiscoveryCurrentness = "historical"
	DiscoveryNotClosed       DiscoveryCurrentness = "not_closed"
	DiscoveryLegacyUnbound   DiscoveryCurrentness = "legacy_unbound"
	DiscoveryIntegrityFailed DiscoveryCurrentness = "integrity_failed"
)

type DiscoveryAssessment struct {
	Workspace                    workflowstore.FeatureWorkspace
	Revision                     *workflowstore.IntegratedDiscoveryRevision
	State                        DiscoveryState
	Destination                  DiscoveryDestination
	Rationale                    string
	Blockers, RestorationActions []string
	PendingIntegrations          []string
	ActiveOperations             []string
	RouteMaterialOpen            []string
	RequiredEvidence             []string
	Continuation                 string
	Currentness                  DiscoveryCurrentness
}
type AdoptFeatureDiscoveryLifecycleInput struct {
	WorkspaceID      string
	ExpectedVersion  int64
	OperatorIdentity string
}
type RecordDiscoveryDestinationAssessmentInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	CreatedIdentity string
}
type CloseFeatureDiscoveryInput struct {
	WorkspaceID        string
	ExpectedVersion    int64
	ExpectedRevisionID string
	Destination        DiscoveryDestination
	CreatedIdentity    string
}
type ReopenFeatureDiscoveryInput struct {
	WorkspaceID                    string
	ExpectedVersion                int64
	ExpectedPacketID               string
	OperatorConfirmed              bool
	Cause, CreatedIdentity, SHA256 string
	Markdown                       []byte
	Destination                    DiscoveryDestination
	Continuation                   string
}
type DiscoveryPacketContent struct {
	Packet      workflowstore.DiscoveryClosurePacket
	Manifest    []byte
	Members     []workflowstore.DiscoveryClosurePacketMember
	Currentness DiscoveryCurrentness
}

type discoveryPacketMemberBasis struct {
	Artifact       workflowstore.DiscoveryArtifact
	OwnerFamily    string
	SourceIdentity string
	SemanticRole   string
}

func validDiscoveryDestination(value DiscoveryDestination) bool {
	return value == DiscoveryDestinationNoDeliveryWork || value == DiscoveryDestinationDirectDeliveryTicket || value == DiscoveryDestinationRequirements || value == DiscoveryDestinationSharedDesign || value == DiscoveryDestinationRequirementsThenSharedDesign || value == DiscoveryDestinationExistingRouteContinuation
}
func (s *Service) AdoptFeatureDiscoveryLifecycle(ctx context.Context, input AdoptFeatureDiscoveryLifecycleInput) (workflowstore.DiscoveryLifecycleAdoption, workflowstore.FeatureWorkspace, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || strings.TrimSpace(input.OperatorIdentity) == "" {
		return workflowstore.DiscoveryLifecycleAdoption{}, workflowstore.FeatureWorkspace{}, ErrInvalidDiscoveryConsequence
	}
	var adoption workflowstore.DiscoveryLifecycleAdoption
	var updated workflowstore.FeatureWorkspace
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.DiscoveryCapabilityEnabled != 1 {
			return ErrDiscoveryCapabilityDisabled
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrDiscoveryStaleState
		}
		if _, err = tx.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); err == nil {
			return ErrDiscoveryAlreadyAdopted
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		incompatible, err := tx.HasIncompatibleActiveProductionMutation(ctx, workspace.ID)
		if err != nil {
			return err
		}
		if incompatible {
			return ErrDiscoveryAdoptionProduction
		}
		adoption, err = tx.CreateDiscoveryLifecycleAdoption(ctx, workspace.ID, lifecycleID(s, "adoption"), strings.TrimSpace(input.OperatorIdentity), workspace.Version)
		if err != nil {
			return err
		}
		updated, err = tx.BumpFeatureWorkspaceVersion(ctx, workspace.WorkspaceID, workspace.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		return err
	})
	return adoption, updated, err
}
func (s *Service) AssessDiscoveryDestination(ctx context.Context, workspaceID string) (DiscoveryAssessment, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return DiscoveryAssessment{}, err
	}
	return s.assessDiscovery(ctx, s.store, workspace)
}
func (s *Service) RecordDiscoveryDestinationAssessment(ctx context.Context, input RecordDiscoveryDestinationAssessmentInput) (workflowstore.DiscoveryDestinationAssessment, workflowstore.FeatureWorkspace, error) {
	var record workflowstore.DiscoveryDestinationAssessment
	var updated workflowstore.FeatureWorkspace
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrDiscoveryStaleState
		}
		assessment, err := s.assessDiscovery(ctx, tx, workspace)
		if err != nil {
			return err
		}
		if assessment.Revision == nil {
			return ErrDiscoveryNotStarted
		}
		record, err = tx.CreateDiscoveryDestinationAssessment(ctx, assessmentRecord(assessment, lifecycleID(s, "assessment"), input.CreatedIdentity))
		if err != nil {
			return err
		}
		updated, err = tx.BumpFeatureWorkspaceVersion(ctx, workspace.WorkspaceID, workspace.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		return err
	})
	return record, updated, err
}
func (s *Service) CloseFeatureDiscovery(ctx context.Context, input CloseFeatureDiscoveryInput) (DiscoveryPacketContent, workflowstore.FeatureWorkspace, error) {
	if !validDiscoveryDestination(input.Destination) || strings.TrimSpace(input.CreatedIdentity) == "" {
		return DiscoveryPacketContent{}, workflowstore.FeatureWorkspace{}, ErrDiscoveryInvalidDestination
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
	if err != nil {
		return DiscoveryPacketContent{}, workflowstore.FeatureWorkspace{}, err
	}
	artifactID := s.discoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return DiscoveryPacketContent{}, workflowstore.FeatureWorkspace{}, err
	}
	var result DiscoveryPacketContent
	var updated workflowstore.FeatureWorkspace
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if current.Version != input.ExpectedVersion {
			return ErrDiscoveryStaleState
		}
		assessment, err := s.assessDiscovery(ctx, tx, current)
		if err != nil {
			return err
		}
		if assessment.Revision == nil {
			return ErrDiscoveryNotStarted
		}
		if current.CurrentDiscoveryClosurePacketRowID.Valid {
			return ErrDiscoveryAlreadyClosed
		}
		if input.ExpectedRevisionID != assessment.Revision.DiscoveryRevisionID {
			return ErrDiscoveryStaleState
		}
		if assessment.State == DiscoveryStateBlocked {
			return ErrDiscoveryBlocked
		}
		if assessment.Currentness == DiscoveryLegacyUnbound {
			return ErrDiscoveryLegacyUnbound
		}
		if len(assessment.PendingIntegrations) > 0 {
			return fmt.Errorf("%w: %s", ErrDiscoveryPendingIntegration, strings.Join(assessment.PendingIntegrations, ","))
		}
		if len(assessment.RouteMaterialOpen) > 0 || len(assessment.RequiredEvidence) > 0 {
			return fmt.Errorf("%w: %s", ErrDiscoveryClosureIneligible, strings.Join(append(append([]string{}, assessment.RouteMaterialOpen...), assessment.RequiredEvidence...), ","))
		}
		if len(assessment.ActiveOperations) > 0 {
			return fmt.Errorf("%w: %s", ErrDiscoveryActiveOperation, strings.Join(assessment.ActiveOperations, ","))
		}
		if assessment.State != DiscoveryStateActive {
			return ErrDiscoveryStaleState
		}
		if assessment.Destination == "" {
			return ErrDiscoveryStaleState
		}
		if assessment.Destination != input.Destination {
			return ErrDiscoveryInvalidDestination
		}
		revisionArtifact, err := tx.GetDiscoveryArtifactByRowID(ctx, assessment.Revision.ArtifactRowID)
		if err != nil {
			return err
		}
		if _, _, err = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: revisionArtifact.RelativePath, SHA256: revisionArtifact.SHA256, SizeBytes: revisionArtifact.SizeBytes, MediaType: revisionArtifact.MediaType}, 16<<20); err != nil {
			return fmt.Errorf("%w: %v", ErrDiscoveryIntegrity, err)
		}
		members, err := s.buildDiscoveryPacketMembers(ctx, tx, batch, current, *assessment.Revision, revisionArtifact)
		if err != nil {
			return err
		}
		packetID := lifecycleID(s, "packet")
		manifest := canonicalDiscoveryManifest(packetID, current, *assessment.Revision, input.Destination, members)
		file, err := batch.Stage("closure_packet", "closure.json", discoveryClosureMediaType, manifest)
		if err != nil {
			return err
		}
		assessment.Destination = input.Destination
		if _, err = tx.CreateDiscoveryDestinationAssessment(ctx, assessmentRecord(assessment, lifecycleID(s, "assessment"), input.CreatedIdentity)); err != nil {
			return err
		}
		artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		packet, err := tx.CreateDiscoveryClosurePacket(ctx, workflowstore.DiscoveryClosurePacket{ClosurePacketID: packetID, WorkspaceRowID: current.ID, ClosingRevisionRowID: assessment.Revision.ID, Destination: string(input.Destination), ManifestArtifactRowID: artifact.ID, ManifestSha256: file.SHA256, ManifestSizeBytes: file.SizeBytes, ManifestMediaType: file.MediaType})
		if err != nil {
			return err
		}
		memberRows := make([]workflowstore.DiscoveryClosurePacketMember, 0, len(members))
		for index, basis := range members {
			member, memberErr := tx.CreateDiscoveryClosurePacketMember(ctx, workflowstore.DiscoveryClosurePacketMember{ClosurePacketRowID: packet.ID, Sequence: int64(index + 1), OwnerFamily: basis.OwnerFamily, ArtifactRowID: basis.Artifact.ID, SourceIdentity: basis.SourceIdentity, Sha256: basis.Artifact.SHA256, SizeBytes: basis.Artifact.SizeBytes, MediaType: basis.Artifact.MediaType, SemanticRole: basis.SemanticRole})
			if memberErr != nil {
				return memberErr
			}
			memberRows = append(memberRows, member)
		}
		routes, err := tx.ListFeatureWorkspaceRouteStates(ctx, current.ID)
		if err != nil {
			return err
		}
		route, err := tx.CreateFeatureWorkspaceRouteState(ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: workflowstore.NewFeatureWorkspaceRouteStateID(), WorkspaceRowID: current.ID, Sequence: int64(len(routes) + 1), WorkspaceVersion: current.Version + 1, State: "ready"})
		if err != nil {
			return err
		}
		updated, err = tx.ApplyDiscoveryCurrentness(ctx, current.WorkspaceID, assessment.Revision.ID, sql.NullInt64{Int64: packet.ID, Valid: true}, route.ID, current.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		if err != nil {
			return err
		}
		result = DiscoveryPacketContent{Packet: packet, Manifest: manifest, Members: memberRows, Currentness: DiscoveryCurrent}
		return nil
	})
	return result, updated, err
}
func (s *Service) ReadDiscoveryClosurePacket(ctx context.Context, workspaceID, packetID string) (DiscoveryPacketContent, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	packet, err := s.store.GetDiscoveryClosurePacket(ctx, packetID)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	if packet.WorkspaceRowID != workspace.ID {
		return DiscoveryPacketContent{}, ErrDiscoveryCrossWorkspace
	}
	artifact, err := s.store.GetDiscoveryArtifactByRowID(ctx, packet.ManifestArtifactRowID)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	if artifact.WorkspaceRowID != workspace.ID || artifact.SHA256 != packet.ManifestSha256 || artifact.SizeBytes != packet.ManifestSizeBytes || artifact.MediaType != packet.ManifestMediaType {
		return DiscoveryPacketContent{}, ErrDiscoveryManifestIntegrity
	}
	_, manifest, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: packet.ManifestSha256, SizeBytes: packet.ManifestSizeBytes, MediaType: packet.ManifestMediaType}, 16<<20)
	if err != nil {
		return DiscoveryPacketContent{}, fmt.Errorf("%w: %v", ErrDiscoveryManifestIntegrity, err)
	}
	members, err := s.store.ListDiscoveryClosurePacketMembers(ctx, packet.ID)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	for _, member := range members {
		a, err := s.store.GetDiscoveryArtifactByRowID(ctx, member.ArtifactRowID)
		if err != nil {
			return DiscoveryPacketContent{}, err
		}
		if a.WorkspaceRowID != workspace.ID || a.SHA256 != member.Sha256 || a.SizeBytes != member.SizeBytes || a.MediaType != member.MediaType {
			return DiscoveryPacketContent{}, fmt.Errorf("%w: %s", ErrDiscoveryMemberIntegrity, member.SourceIdentity)
		}
		if _, _, err = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: a.RelativePath, SHA256: member.Sha256, SizeBytes: member.SizeBytes, MediaType: member.MediaType}, 16<<20); err != nil {
			return DiscoveryPacketContent{}, fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, member.SourceIdentity)
		}
	}
	if err := validateDiscoveryManifest(manifest, packet, members); err != nil {
		return DiscoveryPacketContent{}, err
	}
	state := DiscoveryHistorical
	if workspace.CurrentDiscoveryClosurePacketRowID.Valid && workspace.CurrentDiscoveryClosurePacketRowID.Int64 == packet.ID {
		state = DiscoveryCurrent
	}
	return DiscoveryPacketContent{Packet: packet, Manifest: manifest, Members: members, Currentness: state}, nil
}

// PrepareDiscoveryReopen verifies the exact historical packet before an
// operator submits the replacement integrated revision.
func (s *Service) PrepareDiscoveryReopen(ctx context.Context, workspaceID, packetID string) (DiscoveryPacketContent, error) {
	return s.ReadDiscoveryClosurePacket(ctx, workspaceID, packetID)
}
func (s *Service) ReopenFeatureDiscovery(ctx context.Context, input ReopenFeatureDiscoveryInput) (workflowstore.IntegratedDiscoveryRevision, workflowstore.FeatureWorkspace, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || strings.TrimSpace(input.ExpectedPacketID) == "" || !input.OperatorConfirmed {
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, ErrDiscoveryReopenConfirmation
	}
	if strings.TrimSpace(input.Cause) == "" || strings.TrimSpace(input.CreatedIdentity) == "" || len(input.Markdown) == 0 || !validSHA256(input.SHA256) || digest(input.Markdown) != input.SHA256 || (input.Destination != "" && !validDiscoveryDestination(input.Destination)) {
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, ErrInvalidDiscoveryConsequence
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
	if err != nil {
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, err
	}
	artifactID := s.discoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, err
	}
	file, err := batch.Stage("integrated_discovery", "reopened-discovery.md", "text/markdown", input.Markdown)
	if err != nil {
		_ = batch.Rollback()
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, err
	}
	causeFile, err := batch.Stage("reopen_cause", "reopen-cause.txt", "text/plain", []byte(strings.TrimSpace(input.Cause)+"\n"))
	if err != nil {
		_ = batch.Rollback()
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, err
	}
	causeArtifactID := s.discoveryArtifactID()
	var revision workflowstore.IntegratedDiscoveryRevision
	var updated workflowstore.FeatureWorkspace
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if current.Version != input.ExpectedVersion {
			return ErrDiscoveryStaleState
		}
		if !current.CurrentDiscoveryClosurePacketRowID.Valid {
			return ErrDiscoveryNotClosed
		}
		packet, err := tx.GetDiscoveryClosurePacket(ctx, input.ExpectedPacketID)
		if err != nil {
			return err
		}
		if packet.ID != current.CurrentDiscoveryClosurePacketRowID.Int64 || packet.WorkspaceRowID != current.ID {
			return ErrDiscoveryStalePacket
		}
		manifest, err := tx.GetDiscoveryArtifactByRowID(ctx, packet.ManifestArtifactRowID)
		if err != nil {
			return err
		}
		if manifest.WorkspaceRowID != current.ID || manifest.SHA256 != packet.ManifestSha256 || manifest.SizeBytes != packet.ManifestSizeBytes || manifest.MediaType != packet.ManifestMediaType {
			return ErrDiscoveryManifestIntegrity
		}
		_, manifestBytes, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: manifest.RelativePath, SHA256: packet.ManifestSha256, SizeBytes: packet.ManifestSizeBytes, MediaType: packet.ManifestMediaType}, 16<<20)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrDiscoveryManifestIntegrity, err)
		}
		members, err := tx.ListDiscoveryClosurePacketMembers(ctx, packet.ID)
		if err != nil {
			return err
		}
		for _, member := range members {
			memberArtifact, memberErr := tx.GetDiscoveryArtifactByRowID(ctx, member.ArtifactRowID)
			if memberErr != nil {
				return fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, member.SourceIdentity)
			}
			if memberArtifact.WorkspaceRowID != current.ID || memberArtifact.SHA256 != member.Sha256 || memberArtifact.SizeBytes != member.SizeBytes || memberArtifact.MediaType != member.MediaType {
				return fmt.Errorf("%w: %s", ErrDiscoveryMemberIntegrity, member.SourceIdentity)
			}
			if _, _, memberErr = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: memberArtifact.RelativePath, SHA256: member.Sha256, SizeBytes: member.SizeBytes, MediaType: member.MediaType}, 16<<20); memberErr != nil {
				return fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, member.SourceIdentity)
			}
		}
		if err = validateDiscoveryManifest(manifestBytes, packet, members); err != nil {
			return err
		}
		prior, err := tx.GetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID)
		if err != nil {
			return err
		}
		if packet.ClosingRevisionRowID != prior.ID {
			return ErrDiscoveryStalePacket
		}
		destination := input.Destination
		if destination == "" {
			destination = DiscoveryDestination(packet.Destination)
		}
		artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		revision, err = tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: s.discoveryRevisionID(), WorkspaceRowID: current.ID, RevisionNumber: prior.RevisionNumber + 1, ArtifactRowID: artifact.ID, PredecessorRevisionRowID: sql.NullInt64{Int64: prior.ID, Valid: true}, CreatedIdentity: strings.TrimSpace(input.CreatedIdentity), SettledDestination: nullableDestination(destination), ContinuationJSON: continuationJSON(input.Continuation)})
		if err != nil {
			return err
		}
		causeArtifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: causeArtifactID, WorkspaceRowID: current.ID, RelativePath: causeFile.RelativePath, SHA256: causeFile.SHA256, MediaType: causeFile.MediaType, SizeBytes: causeFile.SizeBytes})
		if err != nil {
			return err
		}
		if _, err = tx.CreateDiscoveryReopenEvent(ctx, workflowstore.DiscoveryReopenEvent{ReopenEventID: lifecycleID(s, "reopen"), WorkspaceRowID: current.ID, ClosurePacketRowID: packet.ID, ReplacementRevisionRowID: revision.ID, CauseText: strings.TrimSpace(input.Cause), ConfirmedOperatorIdentity: strings.TrimSpace(input.CreatedIdentity), CauseArtifactRowID: sql.NullInt64{Int64: causeArtifact.ID, Valid: true}}); err != nil {
			return err
		}
		if completion, completionErr := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, current.ID); completionErr == nil {
			if !current.CurrentAuthorityRevisionRowID.Valid {
				return ErrFeatureCompletionNotReady
			}
			if _, completionErr = tx.CreateFeatureWorkspaceCompletionReopening(ctx, workflowstore.CreateFeatureWorkspaceCompletionReopeningParams{CompletionDecisionRowID: completion.ID, ReopeningKind: "authority_revision", ReopeningAuthorityRevisionRowID: current.CurrentAuthorityRevisionRowID}); completionErr != nil {
				return completionErr
			}
		} else if !errors.Is(completionErr, sql.ErrNoRows) {
			return completionErr
		}
		routes, err := tx.ListFeatureWorkspaceRouteStates(ctx, current.ID)
		if err != nil {
			return err
		}
		route, err := tx.CreateFeatureWorkspaceRouteState(ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: workflowstore.NewFeatureWorkspaceRouteStateID(), WorkspaceRowID: current.ID, Sequence: int64(len(routes) + 1), WorkspaceVersion: current.Version + 1, State: "discovery"})
		if err != nil {
			return err
		}
		updated, err = tx.ApplyDiscoveryCurrentness(ctx, current.WorkspaceID, revision.ID, sql.NullInt64{}, route.ID, current.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		return err
	})
	return revision, updated, err
}
func (s *Service) assessDiscovery(ctx context.Context, reader interface {
	GetDiscoveryLifecycleAdoption(context.Context, int64) (workflowstore.DiscoveryLifecycleAdoption, error)
	GetCurrentIntegratedDiscoveryRevision(context.Context, string) (workflowstore.IntegratedDiscoveryRevision, error)
	GetDiscoveryArtifactByRowID(context.Context, int64) (workflowstore.DiscoveryArtifact, error)
	ListDiscoveryWorkItemMetadata(context.Context, int64) ([]workflowstore.DiscoveryWorkItemMetadata, error)
	ListDiscoveryIntegrationConsequences(context.Context, int64) ([]workflowstore.DiscoveryIntegrationConsequence, error)
	ListFeatureWorkspaceDiscoveryTickets(context.Context, int64) ([]workflowstore.FeatureWorkspaceDiscoveryTicket, error)
	ListFeatureWorkspaceTicketResolutions(context.Context, int64) ([]workflowstore.FeatureWorkspaceTicketResolution, error)
	ListFeatureWorkspaceTicketDependencies(context.Context, int64) ([]workflowstore.FeatureWorkspaceTicketDependency, error)
	ListFeatureWorkspaceRouteStates(context.Context, int64) ([]workflowstore.FeatureWorkspaceRouteState, error)
}, workspace workflowstore.FeatureWorkspace) (DiscoveryAssessment, error) {
	result := DiscoveryAssessment{Workspace: workspace, Currentness: DiscoveryNotClosed}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return result, nil
	}
	adoption, err := reader.GetDiscoveryLifecycleAdoption(ctx, workspace.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	revision, err := reader.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		result.State = DiscoveryStateNotStarted
		if workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentRouteStateRowID.Valid {
			result.Currentness = DiscoveryLegacyUnbound
		}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Revision = &revision
	closed := workspace.CurrentDiscoveryClosurePacketRowID.Valid
	if closed {
		result.Currentness = DiscoveryCurrent
		result.Destination = DiscoveryDestination(revision.SettledDestination.String)
		result.Rationale = "current packet closes the current integrated discovery revision"
	}
	artifact, err := reader.GetDiscoveryArtifactByRowID(ctx, revision.ArtifactRowID)
	if err != nil {
		return result, err
	}
	_, _, err = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType}, 16<<20)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrDiscoveryIntegrity, err)
	}
	tickets, err := reader.ListFeatureWorkspaceDiscoveryTickets(ctx, workspace.ID)
	if err != nil {
		return result, err
	}
	metadata, err := reader.ListDiscoveryWorkItemMetadata(ctx, workspace.ID)
	if err != nil {
		return result, err
	}
	consequences, err := reader.ListDiscoveryIntegrationConsequences(ctx, workspace.ID)
	if err != nil {
		return result, err
	}
	integrated := map[int64]bool{}
	for _, consequence := range consequences {
		integrated[consequence.ResolutionRowID] = true
	}
	route := map[int64]bool{}
	for _, m := range metadata {
		route[m.TicketRowID] = m.RouteMaterial
	}
	blocked := false
	eligibleRoute := false
	hasRoute := false
	for _, ticket := range tickets {
		if route[ticket.ID] {
			hasRoute = true
		}
		if ticket.State == "blocked" {
			blocked = true
			result.Blockers = append(result.Blockers, ticket.DiscoveryTicketID)
			result.RestorationActions = append(result.RestorationActions, "restore_work_item:"+ticket.DiscoveryTicketID)
		}
		if ticket.State == "open" {
			result.ActiveOperations = append(result.ActiveOperations, ticket.DiscoveryTicketID)
			if route[ticket.ID] {
				result.RouteMaterialOpen = append(result.RouteMaterialOpen, ticket.DiscoveryTicketID)
				eligible := true
				dependencies, dependencyErr := reader.ListFeatureWorkspaceTicketDependencies(ctx, ticket.ID)
				if dependencyErr != nil {
					return result, dependencyErr
				}
				for _, dependency := range dependencies {
					if dependency.DependencyKind == "blocks" {
						eligible = false
					}
				}
				eligibleRoute = eligibleRoute || eligible
			}
		}
		if ticket.State == "resolved" || ticket.State == "cancelled" {
			resolutions, resolutionErr := reader.ListFeatureWorkspaceTicketResolutions(ctx, ticket.ID)
			if resolutionErr != nil {
				return result, resolutionErr
			}
			if len(resolutions) == 0 {
				result.RequiredEvidence = append(result.RequiredEvidence, ticket.DiscoveryTicketID)
				result.RestorationActions = append(result.RestorationActions, "record_resolution:"+ticket.DiscoveryTicketID)
			} else if !integrated[resolutions[len(resolutions)-1].ID] {
				result.PendingIntegrations = append(result.PendingIntegrations, resolutions[len(resolutions)-1].ResolutionID)
				result.RestorationActions = append(result.RestorationActions, "integrate_resolution:"+resolutions[len(resolutions)-1].ResolutionID)
			}
		}
	}
	if blocked && !eligibleRoute && len(result.RouteMaterialOpen) == 0 && !closed {
		result.State = DiscoveryStateBlocked
		result.Rationale = "route-material discovery work is blocked"
		return result, nil
	}
	result.State = DiscoveryStateActive
	result.Rationale = "current integrated discovery revision is open"
	if !hasRoute {
		result.Destination = DiscoveryDestinationNoDeliveryWork
		result.Continuation = string(result.Destination)
	}
	if revision.SettledDestination.Valid {
		result.Destination = DiscoveryDestination(revision.SettledDestination.String)
		result.Continuation = revision.ContinuationJSON
	}
	routes, err := reader.ListFeatureWorkspaceRouteStates(ctx, workspace.ID)
	if err != nil {
		return result, err
	}
	if !revision.SettledDestination.Valid && (workspace.CurrentAuthorityRevisionRowID.Valid || (len(routes) > 0 && routes[0].WorkspaceVersion <= adoption.AdoptedWorkspaceVersion)) {
		result.Currentness = DiscoveryLegacyUnbound
		result.Blockers = append(result.Blockers, "legacy_governing_basis")
		result.RestorationActions = append(result.RestorationActions, "replace_integrated_revision_with_settled_destination")
	}
	if closed {
		result.State = DiscoveryStateClosed
		result.Currentness = DiscoveryCurrent
		result.Rationale = "current packet closes the current integrated discovery revision"
	}
	return result, nil
}
func assessmentRecord(a DiscoveryAssessment, id, identity string) workflowstore.DiscoveryDestinationAssessment {
	blockers, _ := json.Marshal(a.Blockers)
	restoration, _ := json.Marshal(a.RestorationActions)
	continuation, _ := json.Marshal(struct {
		Next string `json:"next"`
	}{a.Continuation})
	destination := sql.NullString{}
	if a.Destination != "" {
		destination = sql.NullString{String: string(a.Destination), Valid: true}
	}
	return workflowstore.DiscoveryDestinationAssessment{AssessmentID: id, WorkspaceRowID: a.Workspace.ID, DiscoveryRevisionRowID: a.Revision.ID, WorkspaceVersion: a.Workspace.Version, DiscoveryState: string(a.State), Destination: destination, Rationale: a.Rationale, BlockersJson: string(blockers), RestorationActionsJson: string(restoration), ContinuationJson: string(continuation), CreatedIdentity: strings.TrimSpace(identity), Currentness: string(a.Currentness)}
}

func nullableDestination(value DiscoveryDestination) sql.NullString {
	return sql.NullString{String: string(value), Valid: value != ""}
}

func continuationJSON(value string) string {
	data, _ := json.Marshal(struct {
		Next string `json:"next"`
	}{Next: strings.TrimSpace(value)})
	return string(data)
}
func (s *Service) buildDiscoveryPacketMembers(ctx context.Context, tx *workflowstore.Tx, batch *workflowartifacts.Batch, workspace workflowstore.FeatureWorkspace, revision workflowstore.IntegratedDiscoveryRevision, revisionArtifact workflowstore.DiscoveryArtifact) ([]discoveryPacketMemberBasis, error) {
	result := make([]discoveryPacketMemberBasis, 0)
	appendRetained := func(owner, source, role, path, expectedSHA, mediaType string, size int64) error {
		if strings.TrimSpace(source) == "" || strings.TrimSpace(role) == "" {
			return ErrDiscoveryClosureIneligible
		}
		_, bytes, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: path, SHA256: expectedSHA, SizeBytes: size, MediaType: mediaType}, 16<<20)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, source)
		}
		file, err := batch.Stage("closure_member", fmt.Sprintf("member-%03d.bin", len(result)+1), mediaType, bytes)
		if err != nil {
			return err
		}
		artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: s.discoveryArtifactID(), WorkspaceRowID: workspace.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		result = append(result, discoveryPacketMemberBasis{Artifact: artifact, OwnerFamily: owner, SourceIdentity: source, SemanticRole: role})
		return nil
	}
	appendExternal := func(owner, source, role string, artifactRowID, retainedRowID sql.NullInt64, expectedSHA string) error {
		if artifactRowID.Valid == retainedRowID.Valid {
			return fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, source)
		}
		if artifactRowID.Valid {
			artifact, err := tx.GetArtifactByRowID(ctx, artifactRowID.Int64)
			if err != nil || artifact.SHA256 != expectedSHA {
				return fmt.Errorf("%w: %s", ErrDiscoveryMemberIntegrity, source)
			}
			return appendRetained(owner, source, role, artifact.RelativePath, artifact.SHA256, artifact.MediaType, artifact.SizeBytes)
		}
		artifact, err := tx.GetOperationPacketRetainedArtifactByRowID(ctx, retainedRowID.Int64)
		if err != nil || artifact.SHA256 != expectedSHA {
			return fmt.Errorf("%w: %s", ErrDiscoveryMemberIntegrity, source)
		}
		return appendRetained(owner, source, role, artifact.RelativePath, artifact.SHA256, artifact.MediaType, artifact.SizeBytes)
	}

	inputs, err := tx.ListFeatureWorkspaceAdmittedInputs(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, input := range inputs {
		if !input.ArtifactSha256.Valid {
			return nil, fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, input.AdmittedInputID)
		}
		if err := appendExternal("admitted_input", input.AdmittedInputID, fmt.Sprintf("admitted_input:%03d", input.Sequence), input.ArtifactRowID, input.RetainedArtifactRowID, input.ArtifactSha256.String); err != nil {
			return nil, err
		}
	}
	if revisionArtifact.WorkspaceRowID != workspace.ID {
		return nil, ErrDiscoveryCrossWorkspace
	}
	result = append(result, discoveryPacketMemberBasis{Artifact: revisionArtifact, OwnerFamily: "integrated_discovery", SourceIdentity: revision.DiscoveryRevisionID, SemanticRole: "closing_revision"})

	metadata, err := tx.ListDiscoveryWorkItemMetadata(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	route := map[int64]bool{}
	for _, value := range metadata {
		route[value.TicketRowID] = value.RouteMaterial
	}
	consequences, err := tx.ListDiscoveryIntegrationConsequences(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	integrated := map[int64]bool{}
	for _, value := range consequences {
		integrated[value.ResolutionRowID] = true
	}
	tickets, err := tx.ListFeatureWorkspaceDiscoveryTickets(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, ticket := range tickets {
		if !route[ticket.ID] {
			continue
		}
		resolutions, resolutionErr := tx.ListFeatureWorkspaceTicketResolutions(ctx, ticket.ID)
		if resolutionErr != nil {
			return nil, resolutionErr
		}
		for _, resolution := range resolutions {
			if !integrated[resolution.ID] {
				continue
			}
			if err := appendExternal("discovery_resolution", resolution.ResolutionID, fmt.Sprintf("route_result:%s:%03d", ticket.DiscoveryTicketID, resolution.Sequence), resolution.ArtifactRowID, resolution.RetainedArtifactRowID, resolution.ArtifactSha256); err != nil {
				return nil, err
			}
		}
	}
	investigations, err := tx.ListFeatureWorkspaceInvestigations(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, investigation := range investigations {
		if err := appendExternal("discovery_evidence", investigation.InvestigationID, fmt.Sprintf("represented_evidence:%03d", investigation.Sequence), investigation.ArtifactRowID, investigation.RetainedArtifactRowID, investigation.ArtifactSHA256); err != nil {
			return nil, err
		}
	}
	reopens, err := tx.ListDiscoveryReopenEvents(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, reopen := range reopens {
		if !reopen.CauseArtifactRowID.Valid {
			continue
		}
		artifact, artifactErr := tx.GetDiscoveryArtifactByRowID(ctx, reopen.CauseArtifactRowID.Int64)
		if artifactErr != nil || artifact.WorkspaceRowID != workspace.ID {
			return nil, fmt.Errorf("%w: %s", ErrDiscoveryMemberUnavailable, reopen.ReopenEventID)
		}
		result = append(result, discoveryPacketMemberBasis{Artifact: artifact, OwnerFamily: "discovery_reopen", SourceIdentity: reopen.ReopenEventID, SemanticRole: fmt.Sprintf("reopen_history:%03d", len(result)+1)})
	}
	return result, nil
}

func canonicalDiscoveryManifest(packetID string, workspace workflowstore.FeatureWorkspace, revision workflowstore.IntegratedDiscoveryRevision, destination DiscoveryDestination, basis []discoveryPacketMemberBasis) []byte {
	type member struct {
		Sequence       int64  `json:"sequence"`
		OwnerFamily    string `json:"owner_family"`
		ArtifactID     string `json:"artifact_id"`
		SourceIdentity string `json:"source_identity"`
		SHA256         string `json:"sha256"`
		SizeBytes      int64  `json:"size_bytes"`
		MediaType      string `json:"media_type"`
		SemanticRole   string `json:"semantic_role"`
	}
	v := struct {
		SchemaVersion     int                  `json:"schema_version"`
		PacketID          string               `json:"packet_id"`
		WorkspaceID       string               `json:"workspace_id"`
		FeatureSlug       string               `json:"feature_slug"`
		ClosingRevision   string               `json:"closing_revision"`
		Destination       DiscoveryDestination `json:"destination"`
		SourceBasis       string               `json:"source_basis"`
		Members           []member             `json:"members"`
		IntegratedSummary string               `json:"integrated_summary"`
		CreatedAt         string               `json:"created_at"`
	}{SchemaVersion: 1, PacketID: packetID, WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, ClosingRevision: revision.DiscoveryRevisionID, Destination: destination, SourceBasis: revision.DiscoveryRevisionID, IntegratedSummary: "integrated discovery closure", CreatedAt: revision.CreatedAt}
	for index, value := range basis {
		v.Members = append(v.Members, member{Sequence: int64(index + 1), OwnerFamily: value.OwnerFamily, ArtifactID: value.Artifact.DiscoveryArtifactID, SourceIdentity: value.SourceIdentity, SHA256: value.Artifact.SHA256, SizeBytes: value.Artifact.SizeBytes, MediaType: value.Artifact.MediaType, SemanticRole: value.SemanticRole})
	}
	data, _ := json.Marshal(v)
	return append(data, '\n')
}
func validateDiscoveryManifest(data []byte, packet workflowstore.DiscoveryClosurePacket, members []workflowstore.DiscoveryClosurePacketMember) error {
	var manifest struct {
		PacketID        string `json:"packet_id"`
		WorkspaceID     string `json:"workspace_id"`
		ClosingRevision string `json:"closing_revision"`
		Destination     string `json:"destination"`
		Members         []struct {
			Sequence       int64  `json:"sequence"`
			OwnerFamily    string `json:"owner_family"`
			SourceIdentity string `json:"source_identity"`
			SHA256         string `json:"sha256"`
			SizeBytes      int64  `json:"size_bytes"`
			MediaType      string `json:"media_type"`
			SemanticRole   string `json:"semantic_role"`
		} `json:"members"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.PacketID != packet.ClosurePacketID || manifest.Destination != packet.Destination || len(manifest.Members) != len(members) {
		return ErrDiscoveryManifestIntegrity
	}
	for index, row := range members {
		member := manifest.Members[index]
		if member.Sequence != row.Sequence || member.OwnerFamily != row.OwnerFamily || member.SourceIdentity != row.SourceIdentity || member.SHA256 != row.Sha256 || member.SizeBytes != row.SizeBytes || member.MediaType != row.MediaType || member.SemanticRole != row.SemanticRole {
			return ErrDiscoveryManifestIntegrity
		}
	}
	return nil
}
func lifecycleID(s *Service, kind string) string {
	if ids, ok := s.ids.(interface{ DiscoveryLifecycleID(string) string }); ok {
		return ids.DiscoveryLifecycleID(kind)
	}
	switch kind {
	case "adoption":
		return workflowstore.NewFeatureWorkspaceDiscoveryAdoptionID()
	case "assessment":
		return workflowstore.NewFeatureWorkspaceDiscoveryAssessmentID()
	case "packet":
		return workflowstore.NewFeatureWorkspaceDiscoveryClosurePacketID()
	default:
		return workflowstore.NewFeatureWorkspaceDiscoveryReopenEventID()
	}
}
func manifestPacketID(manifest []byte) string {
	var v struct {
		PacketID string `json:"packet_id"`
	}
	_ = json.Unmarshal(manifest, &v)
	return v.PacketID
}
func manifestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
