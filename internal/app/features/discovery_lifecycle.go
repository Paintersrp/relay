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
}
type DiscoveryPacketContent struct {
	Packet      workflowstore.DiscoveryClosurePacket
	Manifest    []byte
	Members     []workflowstore.DiscoveryClosurePacketMember
	Currentness DiscoveryCurrentness
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
		manifest := canonicalDiscoveryManifest(lifecycleID(s, "packet"), current, *assessment.Revision, input.Destination, revisionArtifact)
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
		packet, err := tx.CreateDiscoveryClosurePacket(ctx, workflowstore.DiscoveryClosurePacket{ClosurePacketID: manifestPacketID(manifest), WorkspaceRowID: current.ID, ClosingRevisionRowID: assessment.Revision.ID, Destination: string(input.Destination), ManifestArtifactRowID: artifact.ID, ManifestSha256: file.SHA256, ManifestSizeBytes: file.SizeBytes, ManifestMediaType: file.MediaType})
		if err != nil {
			return err
		}
		member, err := tx.CreateDiscoveryClosurePacketMember(ctx, workflowstore.DiscoveryClosurePacketMember{ClosurePacketRowID: packet.ID, Sequence: 1, OwnerFamily: "integrated_discovery", ArtifactRowID: revisionArtifact.ID, SourceIdentity: assessment.Revision.DiscoveryRevisionID, Sha256: revisionArtifact.SHA256, SizeBytes: revisionArtifact.SizeBytes, MediaType: revisionArtifact.MediaType, SemanticRole: "closing_revision"})
		if err != nil {
			return err
		}
		updated, err = tx.SetCurrentDiscoveryClosurePacket(ctx, current.WorkspaceID, sql.NullInt64{Int64: packet.ID, Valid: true}, current.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		if err != nil {
			return err
		}
		result = DiscoveryPacketContent{Packet: packet, Manifest: manifest, Members: []workflowstore.DiscoveryClosurePacketMember{member}, Currentness: DiscoveryCurrent}
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
	_, manifest, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: packet.ManifestSha256, SizeBytes: packet.ManifestSizeBytes, MediaType: packet.ManifestMediaType}, 16<<20)
	if err != nil {
		return DiscoveryPacketContent{}, fmt.Errorf("%w: %v", ErrDiscoveryIntegrity, err)
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
		if _, _, err = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: a.RelativePath, SHA256: member.Sha256, SizeBytes: member.SizeBytes, MediaType: member.MediaType}, 16<<20); err != nil {
			return DiscoveryPacketContent{}, fmt.Errorf("%w: %v", ErrDiscoveryIntegrity, err)
		}
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
	if strings.TrimSpace(input.Cause) == "" || strings.TrimSpace(input.CreatedIdentity) == "" || len(input.Markdown) == 0 || !validSHA256(input.SHA256) || digest(input.Markdown) != input.SHA256 {
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
		if _, _, err = s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: manifest.RelativePath, SHA256: packet.ManifestSha256, SizeBytes: packet.ManifestSizeBytes, MediaType: packet.ManifestMediaType}, 16<<20); err != nil {
			return fmt.Errorf("%w: %v", ErrDiscoveryIntegrity, err)
		}
		prior, err := tx.GetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID)
		if err != nil {
			return err
		}
		artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		revision, err = tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: s.discoveryRevisionID(), WorkspaceRowID: current.ID, RevisionNumber: prior.RevisionNumber + 1, ArtifactRowID: artifact.ID, PredecessorRevisionRowID: sql.NullInt64{Int64: prior.ID, Valid: true}, CreatedIdentity: strings.TrimSpace(input.CreatedIdentity)})
		if err != nil {
			return err
		}
		if _, err = tx.CreateDiscoveryReopenEvent(ctx, workflowstore.DiscoveryReopenEvent{ReopenEventID: lifecycleID(s, "reopen"), WorkspaceRowID: current.ID, ClosurePacketRowID: packet.ID, ReplacementRevisionRowID: revision.ID, CauseText: strings.TrimSpace(input.Cause), ConfirmedOperatorIdentity: strings.TrimSpace(input.CreatedIdentity)}); err != nil {
			return err
		}
		updated, err = tx.SetDiscoveryReopenPointers(ctx, current.WorkspaceID, revision.ID, current.Version)
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
	ListFeatureWorkspaceDiscoveryTickets(context.Context, int64) ([]workflowstore.FeatureWorkspaceDiscoveryTicket, error)
}, workspace workflowstore.FeatureWorkspace) (DiscoveryAssessment, error) {
	result := DiscoveryAssessment{Workspace: workspace, Currentness: DiscoveryNotClosed}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return result, nil
	}
	if _, err := reader.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); errors.Is(err, sql.ErrNoRows) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	if workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		result.State = DiscoveryStateClosed
		result.Currentness = DiscoveryCurrent
		return result, nil
	}
	revision, err := reader.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		result.State = DiscoveryStateNotStarted
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Revision = &revision
	artifact, err := reader.GetDiscoveryArtifactByRowID(ctx, revision.ArtifactRowID)
	if err != nil {
		return result, err
	}
	_, bytes, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType}, 16<<20)
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
	route := map[int64]bool{}
	for _, m := range metadata {
		route[m.TicketRowID] = m.RouteMaterial
	}
	blocked := false
	active := false
	hasRoute := false
	for _, ticket := range tickets {
		if route[ticket.ID] {
			hasRoute = true
		}
		if route[ticket.ID] && (ticket.State == "blocked") {
			blocked = true
		}
		if route[ticket.ID] && ticket.State == "open" {
			active = true
		}
	}
	if blocked && !active {
		result.State = DiscoveryStateBlocked
		result.Blockers = []string{"route_material_work_blocked"}
		result.RestorationActions = []string{"resolve_blocking_discovery_work"}
		result.Rationale = "route-material discovery work is blocked"
		return result, nil
	}
	result.State = DiscoveryStateActive
	result.Rationale = "current integrated discovery revision is open"
	if !hasRoute {
		result.Destination = DiscoveryDestinationNoDeliveryWork
		result.Continuation = string(result.Destination)
	}
	if !active {
		for _, line := range strings.Split(string(bytes), "\n") {
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "destination:"))
			if strings.HasPrefix(strings.TrimSpace(line), "destination:") && validDiscoveryDestination(DiscoveryDestination(value)) {
				result.Destination = DiscoveryDestination(value)
				result.Continuation = value
				break
			}
		}
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
	return workflowstore.DiscoveryDestinationAssessment{AssessmentID: id, WorkspaceRowID: a.Workspace.ID, DiscoveryRevisionRowID: a.Revision.ID, WorkspaceVersion: a.Workspace.Version, DiscoveryState: string(a.State), Destination: destination, Rationale: a.Rationale, BlockersJson: string(blockers), RestorationActionsJson: string(restoration), ContinuationJson: string(continuation), CreatedIdentity: strings.TrimSpace(identity)}
}
func canonicalDiscoveryManifest(packetID string, workspace workflowstore.FeatureWorkspace, revision workflowstore.IntegratedDiscoveryRevision, destination DiscoveryDestination, artifact workflowstore.DiscoveryArtifact) []byte {
	v := struct {
		SchemaVersion   int                  `json:"schema_version"`
		PacketID        string               `json:"packet_id"`
		WorkspaceID     string               `json:"workspace_id"`
		FeatureSlug     string               `json:"feature_slug"`
		ClosingRevision string               `json:"closing_revision"`
		Destination     DiscoveryDestination `json:"destination"`
		SourceBasis     string               `json:"source_basis"`
		Members         []struct {
			ArtifactID, SHA256, MediaType, Role string
			Size                                int64
		} `json:"members"`
		IntegratedSummary string `json:"integrated_summary"`
		CreatedAt         string `json:"created_at"`
	}{SchemaVersion: 1, PacketID: packetID, WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, ClosingRevision: revision.DiscoveryRevisionID, Destination: destination, SourceBasis: revision.DiscoveryRevisionID, IntegratedSummary: "integrated discovery closure", CreatedAt: revision.CreatedAt}
	v.Members = append(v.Members, struct {
		ArtifactID, SHA256, MediaType, Role string
		Size                                int64
	}{artifact.DiscoveryArtifactID, artifact.SHA256, artifact.MediaType, "closing_revision", artifact.SizeBytes})
	data, _ := json.Marshal(v)
	return append(data, '\n')
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
