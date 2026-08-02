package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrDiscoveryCapabilityDisabled          = errors.New("integrated discovery capability is disabled")
	ErrDiscoveryStaleState                  = errors.New("integrated discovery state is stale")
	ErrDiscoveryIntegrity                   = errors.New("integrated discovery artifact integrity failure")
	ErrDuplicateDiscoveryIntegration        = errors.New("discovery resolution is already integrated")
	ErrInvalidDiscoveryConsequence          = errors.New("invalid discovery integration consequence")
	ErrDiscoveryDependencyUnresolved        = errors.New("discovery blocking dependency is unresolved")
	ErrDiscoveryCrossWorkspace              = errors.New("discovery references must share a workspace")
	ErrInvalidDiscoverySupersessionTopology = errors.New("invalid discovery supersession topology")
)

type StartIntegratedDiscoveryInput struct {
	WorkspaceID             string
	ExpectedVersion         int64
	Markdown                []byte
	SHA256, CreatedIdentity string
}
type DiscoveryWorkItemInput struct {
	WorkspaceID, TicketID, Kind string
	RouteMaterial               bool
	ExpectedVersion             int64
	Dependencies                []DiscoveryDependencyInput
}
type DiscoveryDependencyInput struct{ TicketID, Kind string }
type IntegrateDiscoveryResultInput struct {
	WorkspaceID, TicketID, ResolutionID, Consequence, ExpectedSHA256, EvidenceBasis string
	ExpectedWorkspaceVersion, ExpectedWorkItemVersion                               int64
	ReplacementTicketID                                                             string
	Markdown                                                                        []byte
	CreatedIdentity                                                                 string
}
type DiscoveryRevisionContent struct {
	Revision workflowstore.IntegratedDiscoveryRevision
	Artifact workflowstore.DiscoveryArtifact
	Markdown []byte
}
type DiscoveryWorkItemSummary struct {
	Ticket             workflowstore.FeatureWorkspaceDiscoveryTicket
	Metadata           *workflowstore.DiscoveryWorkItemMetadata
	Eligible           bool
	BlockingTicketID   string
	PendingIntegration bool
	Historical         bool
}
type DiscoveryFrontier struct {
	Current   DiscoveryRevisionContent
	WorkItems []DiscoveryWorkItemSummary
}

// SetIntegratedDiscoveryCapability is the controlled per-workspace activation
// boundary. The default is disabled for both new and legacy workspaces.
func (s *Service) SetIntegratedDiscoveryCapability(ctx context.Context, workspaceID string, expectedVersion int64, enabled bool) (workflowstore.FeatureWorkspace, error) {
	return s.store.WithDiscoveryCapability(ctx, workspaceID, expectedVersion, enabled)
}

func (s *Service) StartIntegratedDiscovery(ctx context.Context, input StartIntegratedDiscoveryInput) (DiscoveryRevisionContent, workflowstore.FeatureWorkspace, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || len(input.Markdown) == 0 || !validSHA256(input.SHA256) || strings.TrimSpace(input.CreatedIdentity) == "" || digest(input.Markdown) != input.SHA256 {
		return DiscoveryRevisionContent{}, workflowstore.FeatureWorkspace{}, ErrInvalidDiscoveryConsequence
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return DiscoveryRevisionContent{}, workflowstore.FeatureWorkspace{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return DiscoveryRevisionContent{}, workflowstore.FeatureWorkspace{}, err
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return DiscoveryRevisionContent{}, workflowstore.FeatureWorkspace{}, ErrDiscoveryCapabilityDisabled
	}
	artifactID := s.discoveryArtifactID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
	if err != nil {
		return DiscoveryRevisionContent{}, workflowstore.FeatureWorkspace{}, err
	}
	file, err := batch.Stage("integrated_discovery", "discovery.md", "text/markdown", input.Markdown)
	if err != nil {
		_ = batch.Rollback()
		return DiscoveryRevisionContent{}, workflowstore.FeatureWorkspace{}, err
	}
	var result DiscoveryRevisionContent
	var updated workflowstore.FeatureWorkspace
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if current.DiscoveryCapabilityEnabled != 1 {
			return ErrDiscoveryCapabilityDisabled
		}
		if current.Version != input.ExpectedVersion {
			return ErrDiscoveryStaleState
		}
		if current.CurrentDiscoveryRevisionRowID.Valid {
			return ErrInvalidDiscoveryConsequence
		}
		artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		revision, err := tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: s.discoveryRevisionID(), WorkspaceRowID: current.ID, RevisionNumber: 1, ArtifactRowID: artifact.ID, CreatedIdentity: strings.TrimSpace(input.CreatedIdentity)})
		if err != nil {
			return err
		}
		updated, err = tx.SetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID, revision.ID, current.Version)
		if err != nil {
			return ErrDiscoveryStaleState
		}
		result = DiscoveryRevisionContent{Revision: revision, Artifact: artifact, Markdown: append([]byte(nil), input.Markdown...)}
		return nil
	})
	return result, updated, err
}

func (s *Service) UpdateDiscoveryWorkItem(ctx context.Context, input DiscoveryWorkItemInput) (workflowstore.FeatureWorkspaceDiscoveryTicket, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.TicketID) == "" || strings.TrimSpace(input.Kind) == "" || input.ExpectedVersion < 1 {
		return workflowstore.FeatureWorkspaceDiscoveryTicket{}, ErrInvalidDiscoveryConsequence
	}
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if workspace.DiscoveryCapabilityEnabled != 1 {
			return ErrDiscoveryCapabilityDisabled
		}
		ticket, err = tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, input.TicketID)
		if err != nil {
			return err
		}
		if ticket.WorkspaceRowID != workspace.ID {
			return ErrDiscoveryCrossWorkspace
		}
		if ticket.Version != input.ExpectedVersion {
			return ErrDiscoveryStaleState
		}
		if _, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, strings.TrimSpace(input.Kind), input.RouteMaterial); err != nil {
			return err
		}
		for _, dependency := range input.Dependencies {
			if !oneOf(dependency.Kind, "blocks", "informs") {
				return ErrInvalidDiscoveryConsequence
			}
			target, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, dependency.TicketID)
			if err != nil {
				return err
			}
			if target.WorkspaceRowID != workspace.ID {
				return ErrDiscoveryCrossWorkspace
			}
			if target.ID == ticket.ID {
				return ErrInvalidDiscoveryConsequence
			}
			if err = tx.CreateFeatureWorkspaceTicketDependency(ctx, workflowstore.CreateFeatureWorkspaceTicketDependencyParams{TicketRowID: ticket.ID, DependsOnTicketRowID: target.ID, DependencyKind: dependency.Kind}); err != nil {
				return err
			}
		}
		ticket, err = tx.BumpDiscoveryWorkItemVersion(ctx, ticket.DiscoveryTicketID, ticket.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		return err
	})
	return ticket, err
}

func (s *Service) ReadIntegratedDiscoveryFrontier(ctx context.Context, workspaceID string) (DiscoveryFrontier, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return DiscoveryFrontier{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return DiscoveryFrontier{}, err
	}
	revision, err := s.store.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID)
	if err != nil {
		return DiscoveryFrontier{}, err
	}
	artifact, err := s.store.GetDiscoveryArtifactByRowID(ctx, revision.ArtifactRowID)
	if err != nil {
		return DiscoveryFrontier{}, err
	}
	_, bytes, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{RelativePath: artifact.RelativePath, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes, MediaType: artifact.MediaType}, 16<<20)
	if err != nil {
		return DiscoveryFrontier{}, fmt.Errorf("%w: %v", ErrDiscoveryIntegrity, err)
	}
	tickets, err := s.store.ListFeatureWorkspaceDiscoveryTickets(ctx, workspace.ID)
	if err != nil {
		return DiscoveryFrontier{}, err
	}
	metadata, err := s.store.ListDiscoveryWorkItemMetadata(ctx, workspace.ID)
	if err != nil {
		return DiscoveryFrontier{}, err
	}
	consequences, err := s.store.ListDiscoveryIntegrationConsequences(ctx, workspace.ID)
	if err != nil {
		return DiscoveryFrontier{}, err
	}
	meta := map[int64]workflowstore.DiscoveryWorkItemMetadata{}
	for _, value := range metadata {
		meta[value.TicketRowID] = value
	}
	byID := map[int64]workflowstore.FeatureWorkspaceDiscoveryTicket{}
	for _, ticket := range tickets {
		byID[ticket.ID] = ticket
	}
	consequenceByResolution := map[int64]workflowstore.DiscoveryIntegrationConsequence{}
	for _, value := range consequences {
		consequenceByResolution[value.ResolutionRowID] = value
	}
	result := DiscoveryFrontier{Current: DiscoveryRevisionContent{Revision: revision, Artifact: artifact, Markdown: bytes}, WorkItems: make([]DiscoveryWorkItemSummary, 0, len(tickets))}
	for _, ticket := range tickets {
		summary := DiscoveryWorkItemSummary{Ticket: ticket}
		if value, ok := meta[ticket.ID]; ok {
			copy := value
			summary.Metadata = &copy
			summary.Historical = value.LegacyAdopted
		}
		if ticket.State == "resolved" || ticket.State == "cancelled" {
			resolutions, err := s.store.ListFeatureWorkspaceTicketResolutions(ctx, ticket.ID)
			if err != nil {
				return DiscoveryFrontier{}, err
			}
			if len(resolutions) == 0 || consequenceByResolution[resolutions[len(resolutions)-1].ID].ID == 0 {
				summary.PendingIntegration = true
			}
		}
		dependencies, err := s.store.ListFeatureWorkspaceTicketDependencies(ctx, ticket.ID)
		if err != nil {
			return DiscoveryFrontier{}, err
		}
		summary.Eligible = ticket.State == "open"
		for _, dependency := range dependencies {
			if dependency.DependencyKind != "blocks" {
				continue
			}
			satisfied, blocking := discoveryDependencySatisfied(ctx, s.store, byID, consequenceByResolution, dependency.DependsOnTicketRowID, map[int64]bool{})
			if !satisfied {
				summary.Eligible = false
				summary.BlockingTicketID = blocking
				break
			}
		}
		result.WorkItems = append(result.WorkItems, summary)
	}
	return result, nil
}

func (s *Service) IntegrateDiscoveryResult(ctx context.Context, input IntegrateDiscoveryResultInput) (workflowstore.DiscoveryIntegrationConsequence, workflowstore.FeatureWorkspace, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.TicketID) == "" || strings.TrimSpace(input.ResolutionID) == "" || input.ExpectedWorkspaceVersion < 1 || input.ExpectedWorkItemVersion < 1 || !oneOf(input.Consequence, "integrated", "no_material_change", "superseded") || strings.TrimSpace(input.EvidenceBasis) == "" {
		return workflowstore.DiscoveryIntegrationConsequence{}, workflowstore.FeatureWorkspace{}, ErrInvalidDiscoveryConsequence
	}
	material := input.Consequence == "integrated"
	if material != (len(input.Markdown) > 0) || (material && (!validSHA256(input.ExpectedSHA256) || digest(input.Markdown) != input.ExpectedSHA256 || strings.TrimSpace(input.CreatedIdentity) == "")) || (!material && (input.ExpectedSHA256 != "" || strings.TrimSpace(input.CreatedIdentity) != "")) || (input.Consequence == "no_material_change" && strings.TrimSpace(input.ReplacementTicketID) != "") {
		return workflowstore.DiscoveryIntegrationConsequence{}, workflowstore.FeatureWorkspace{}, ErrInvalidDiscoveryConsequence
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
	if err != nil {
		return workflowstore.DiscoveryIntegrationConsequence{}, workflowstore.FeatureWorkspace{}, err
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return workflowstore.DiscoveryIntegrationConsequence{}, workflowstore.FeatureWorkspace{}, ErrDiscoveryCapabilityDisabled
	}
	var batch *workflowartifacts.Batch
	var file workflowartifacts.File
	artifactID := ""
	if material {
		artifactID = s.discoveryArtifactID()
		batch, err = s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + artifactID)
		if err != nil {
			return workflowstore.DiscoveryIntegrationConsequence{}, workflowstore.FeatureWorkspace{}, err
		}
		file, err = batch.Stage("integrated_discovery", "discovery.md", "text/markdown", input.Markdown)
		if err != nil {
			_ = batch.Rollback()
			return workflowstore.DiscoveryIntegrationConsequence{}, workflowstore.FeatureWorkspace{}, err
		}
	}
	var consequence workflowstore.DiscoveryIntegrationConsequence
	var updated workflowstore.FeatureWorkspace
	operation := func(tx *workflowstore.Tx) error {
		current, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		if current.DiscoveryCapabilityEnabled != 1 {
			return ErrDiscoveryCapabilityDisabled
		}
		if current.Version != input.ExpectedWorkspaceVersion {
			return ErrDiscoveryStaleState
		}
		ticket, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, input.TicketID)
		if err != nil {
			return err
		}
		if ticket.WorkspaceRowID != current.ID {
			return ErrDiscoveryCrossWorkspace
		}
		if ticket.Version != input.ExpectedWorkItemVersion {
			return ErrDiscoveryStaleState
		}
		if ticket.State != "resolved" && ticket.State != "cancelled" {
			return ErrInvalidDiscoveryConsequence
		}
		resolutions, err := tx.ListFeatureWorkspaceTicketResolutions(ctx, ticket.ID)
		if err != nil {
			return err
		}
		var resolution workflowstore.FeatureWorkspaceTicketResolution
		for _, candidate := range resolutions {
			if candidate.ResolutionID == input.ResolutionID {
				resolution = candidate
				break
			}
		}
		if resolution.ID == 0 {
			return ErrInvalidDiscoveryConsequence
		}
		all, err := tx.ListDiscoveryIntegrationConsequences(ctx, current.ID)
		if err != nil {
			return err
		}
		for _, prior := range all {
			if prior.ResolutionRowID == resolution.ID {
				return fmt.Errorf("%w: %s", ErrDuplicateDiscoveryIntegration, prior.IntegrationConsequenceID)
			}
		}
		var produced sql.NullInt64
		var replacement sql.NullInt64
		if input.Consequence == "superseded" {
			target, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, input.ReplacementTicketID)
			if err != nil {
				return err
			}
			if target.WorkspaceRowID != current.ID {
				return ErrDiscoveryCrossWorkspace
			}
			if target.ID == ticket.ID {
				return ErrInvalidDiscoverySupersessionTopology
			}
			if err := validateDiscoverySupersessionTopology(ctx, tx, current.ID, ticket.ID, target.ID); err != nil {
				return err
			}
			replacement = sql.NullInt64{Int64: target.ID, Valid: true}
		}
		if material {
			prior, err := tx.GetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID)
			if err != nil {
				return err
			}
			artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: artifactID, WorkspaceRowID: current.ID, RelativePath: file.RelativePath, SHA256: file.SHA256, MediaType: file.MediaType, SizeBytes: file.SizeBytes})
			if err != nil {
				return err
			}
			revision, err := tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: s.discoveryRevisionID(), WorkspaceRowID: current.ID, RevisionNumber: prior.RevisionNumber + 1, ArtifactRowID: artifact.ID, PredecessorRevisionRowID: sql.NullInt64{Int64: prior.ID, Valid: true}, CreatedIdentity: strings.TrimSpace(input.CreatedIdentity)})
			if err != nil {
				return err
			}
			produced = sql.NullInt64{Int64: revision.ID, Valid: true}
		}
		consequence, err = tx.CreateDiscoveryIntegrationConsequence(ctx, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: workflowstore.NewFeatureWorkspaceIntegrationConsequenceID(), WorkspaceRowID: current.ID, TicketRowID: ticket.ID, ResolutionRowID: resolution.ID, ConsequenceKind: input.Consequence, ProducedRevisionRowID: produced, ReplacementTicketRowID: replacement, EvidenceBasis: strings.TrimSpace(input.EvidenceBasis)})
		if err != nil {
			return err
		}
		if material {
			updated, err = tx.SetCurrentIntegratedDiscoveryRevision(ctx, current.WorkspaceID, produced.Int64, current.Version)
		} else {
			updated, err = tx.BumpFeatureWorkspaceVersion(ctx, current.WorkspaceID, current.Version)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDiscoveryStaleState
		}
		if err != nil {
			return err
		}
		_, err = tx.BumpDiscoveryWorkItemVersion(ctx, ticket.DiscoveryTicketID, ticket.Version)
		return err
	}
	if material {
		err = s.store.CommitArtifactBatch(ctx, batch, operation)
	} else {
		err = s.store.WithTx(ctx, operation)
	}
	return consequence, updated, err
}

// validateDiscoverySupersessionTopology rejects a new edge that would make an
// existing replacement chain cyclic or depend on inconsistent history.
func validateDiscoverySupersessionTopology(ctx context.Context, tx *workflowstore.Tx, workspaceRowID, sourceTicketRowID, replacementTicketRowID int64) error {
	tickets, err := tx.ListFeatureWorkspaceDiscoveryTickets(ctx, workspaceRowID)
	if err != nil {
		return err
	}
	byID := make(map[int64]workflowstore.FeatureWorkspaceDiscoveryTicket, len(tickets))
	for _, ticket := range tickets {
		byID[ticket.ID] = ticket
	}
	consequences, err := tx.ListDiscoveryIntegrationConsequences(ctx, workspaceRowID)
	if err != nil {
		return err
	}
	byResolution := make(map[int64]workflowstore.DiscoveryIntegrationConsequence, len(consequences))
	for _, consequence := range consequences {
		if _, exists := byResolution[consequence.ResolutionRowID]; exists {
			return ErrInvalidDiscoverySupersessionTopology
		}
		byResolution[consequence.ResolutionRowID] = consequence
	}

	seen := map[int64]bool{}
	for currentTicketRowID := replacementTicketRowID; ; {
		if currentTicketRowID == sourceTicketRowID || seen[currentTicketRowID] {
			return ErrInvalidDiscoverySupersessionTopology
		}
		current, ok := byID[currentTicketRowID]
		if !ok || current.WorkspaceRowID != workspaceRowID {
			return ErrInvalidDiscoverySupersessionTopology
		}
		seen[currentTicketRowID] = true
		resolutions, err := tx.ListFeatureWorkspaceTicketResolutions(ctx, currentTicketRowID)
		if err != nil {
			return ErrInvalidDiscoverySupersessionTopology
		}
		if len(resolutions) == 0 {
			return nil
		}
		resolution := resolutions[len(resolutions)-1]
		consequence, ok := byResolution[resolution.ID]
		if !ok {
			return nil
		}
		if consequence.WorkspaceRowID != workspaceRowID || consequence.TicketRowID != currentTicketRowID {
			return ErrInvalidDiscoverySupersessionTopology
		}
		if consequence.ConsequenceKind != "superseded" {
			return nil
		}
		if !consequence.ReplacementTicketRowID.Valid {
			return ErrInvalidDiscoverySupersessionTopology
		}
		currentTicketRowID = consequence.ReplacementTicketRowID.Int64
	}
}

func discoveryDependencySatisfied(ctx context.Context, store *workflowstore.Store, tickets map[int64]workflowstore.FeatureWorkspaceDiscoveryTicket, consequences map[int64]workflowstore.DiscoveryIntegrationConsequence, ticketID int64, seen map[int64]bool) (bool, string) {
	if seen[ticketID] {
		return false, tickets[ticketID].DiscoveryTicketID
	}
	seen[ticketID] = true
	ticket, ok := tickets[ticketID]
	if !ok || (ticket.State != "resolved" && ticket.State != "cancelled") {
		if ok {
			return false, ticket.DiscoveryTicketID
		}
		return false, "unknown"
	}
	resolutions, err := store.ListFeatureWorkspaceTicketResolutions(ctx, ticketID)
	if err != nil || len(resolutions) == 0 {
		return false, ticket.DiscoveryTicketID
	}
	consequence, ok := consequences[resolutions[len(resolutions)-1].ID]
	if !ok {
		return false, ticket.DiscoveryTicketID
	}
	if consequence.ConsequenceKind != "superseded" {
		return true, ""
	}
	return discoveryDependencySatisfied(ctx, store, tickets, consequences, consequence.ReplacementTicketRowID.Int64, seen)
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (s *Service) discoveryArtifactID() string {
	if ids, ok := s.ids.(interface{ DiscoveryArtifactID() string }); ok {
		return ids.DiscoveryArtifactID()
	}
	return workflowstore.NewFeatureWorkspaceDiscoveryArtifactID()
}

func (s *Service) discoveryRevisionID() string {
	if ids, ok := s.ids.(interface{ DiscoveryRevisionID() string }); ok {
		return ids.DiscoveryRevisionID()
	}
	return workflowstore.NewFeatureWorkspaceDiscoveryRevisionID()
}
