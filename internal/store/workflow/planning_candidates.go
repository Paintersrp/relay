package workflowstore

import (
	"context"
	"fmt"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowgenerated "relay/internal/store/workflowgenerated"
)

// Generated persistence types remain behind the workflow store boundary.
type (
	FeatureWorkspaceDiscoveryArtifact = workflowgenerated.FeatureWorkspaceDiscoveryArtifact
	PlanningCandidate                 = workflowgenerated.PlanningCandidate
	PlanningCandidateApproval         = workflowgenerated.PlanningCandidateApproval
	DeliveryTicketProductionLink      = workflowgenerated.DeliveryTicketProductionLink

	CreateFeatureWorkspaceDiscoveryArtifactParams = workflowgenerated.CreateFeatureWorkspaceDiscoveryArtifactParams
	CreatePlanningCandidateParams                 = workflowgenerated.CreatePlanningCandidateParams
	CreatePlanningCandidateApprovalParams         = workflowgenerated.CreatePlanningCandidateApprovalParams
	CreateDeliveryTicketProductionLinkParams      = workflowgenerated.CreateDeliveryTicketProductionLinkParams
)

func (s *Store) GetFeatureWorkspaceDiscoveryArtifactByID(ctx context.Context, artifactID string) (FeatureWorkspaceDiscoveryArtifact, error) {
	return workflowgenerated.New(s.db).GetFeatureWorkspaceDiscoveryArtifactByID(ctx, artifactID)
}

func (tx *Tx) CreateFeatureWorkspaceDiscoveryArtifact(ctx context.Context, params CreateFeatureWorkspaceDiscoveryArtifactParams) (FeatureWorkspaceDiscoveryArtifact, error) {
	return workflowgenerated.New(tx.tx).CreateFeatureWorkspaceDiscoveryArtifact(ctx, params)
}

func (tx *Tx) GetFeatureWorkspaceDiscoveryArtifactByID(ctx context.Context, artifactID string) (FeatureWorkspaceDiscoveryArtifact, error) {
	return workflowgenerated.New(tx.tx).GetFeatureWorkspaceDiscoveryArtifactByID(ctx, artifactID)
}

func (s *Store) CreatePlanningCandidate(ctx context.Context, params CreatePlanningCandidateParams) (PlanningCandidate, error) {
	return workflowgenerated.New(s.db).CreatePlanningCandidate(ctx, params)
}

func (s *Store) GetPlanningCandidateByCandidateID(ctx context.Context, candidateID string) (PlanningCandidate, error) {
	return workflowgenerated.New(s.db).GetPlanningCandidateByCandidateID(ctx, candidateID)
}

func (s *Store) GetPlanningCandidateByRowID(ctx context.Context, rowID int64) (PlanningCandidate, error) {
	return workflowgenerated.New(s.db).GetPlanningCandidateByRowID(ctx, rowID)
}

func (s *Store) ListPlanningCandidatesByWorkspace(ctx context.Context, workspaceRowID int64) ([]PlanningCandidate, error) {
	return workflowgenerated.New(s.db).ListPlanningCandidatesByWorkspace(ctx, workspaceRowID)
}

func (tx *Tx) CreatePlanningCandidate(ctx context.Context, params CreatePlanningCandidateParams) (PlanningCandidate, error) {
	return workflowgenerated.New(tx.tx).CreatePlanningCandidate(ctx, params)
}

func (tx *Tx) GetPlanningCandidateByCandidateID(ctx context.Context, candidateID string) (PlanningCandidate, error) {
	return workflowgenerated.New(tx.tx).GetPlanningCandidateByCandidateID(ctx, candidateID)
}

func (tx *Tx) GetPlanningCandidateByRowID(ctx context.Context, rowID int64) (PlanningCandidate, error) {
	return workflowgenerated.New(tx.tx).GetPlanningCandidateByRowID(ctx, rowID)
}

func (tx *Tx) ListPlanningCandidatesByWorkspace(ctx context.Context, workspaceRowID int64) ([]PlanningCandidate, error) {
	return workflowgenerated.New(tx.tx).ListPlanningCandidatesByWorkspace(ctx, workspaceRowID)
}

func (s *Store) GetPlanningCandidateApprovalByApprovalID(ctx context.Context, approvalID string) (PlanningCandidateApproval, error) {
	return workflowgenerated.New(s.db).GetPlanningCandidateApprovalByApprovalID(ctx, approvalID)
}

func (s *Store) GetPlanningCandidateApprovalByRowID(ctx context.Context, rowID int64) (PlanningCandidateApproval, error) {
	return workflowgenerated.New(s.db).GetPlanningCandidateApprovalByRowID(ctx, rowID)
}

func (s *Store) ListPlanningCandidateApprovalsByCandidate(ctx context.Context, candidateRowID int64) ([]PlanningCandidateApproval, error) {
	return workflowgenerated.New(s.db).ListPlanningCandidateApprovalsByCandidate(ctx, candidateRowID)
}

func (tx *Tx) CreatePlanningCandidateApproval(ctx context.Context, params CreatePlanningCandidateApprovalParams) (PlanningCandidateApproval, error) {
	return workflowgenerated.New(tx.tx).CreatePlanningCandidateApproval(ctx, params)
}

func (tx *Tx) GetPlanningCandidateApprovalByApprovalID(ctx context.Context, approvalID string) (PlanningCandidateApproval, error) {
	return workflowgenerated.New(tx.tx).GetPlanningCandidateApprovalByApprovalID(ctx, approvalID)
}

func (tx *Tx) GetPlanningCandidateApprovalByRowID(ctx context.Context, rowID int64) (PlanningCandidateApproval, error) {
	return workflowgenerated.New(tx.tx).GetPlanningCandidateApprovalByRowID(ctx, rowID)
}

func (tx *Tx) ListPlanningCandidateApprovalsByCandidate(ctx context.Context, candidateRowID int64) ([]PlanningCandidateApproval, error) {
	return workflowgenerated.New(tx.tx).ListPlanningCandidateApprovalsByCandidate(ctx, candidateRowID)
}

func (s *Store) GetDeliveryTicketProductionLinkByLinkID(ctx context.Context, linkID string) (DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketProductionLinkByLinkID(ctx, linkID)
}

func (s *Store) GetDeliveryTicketProductionLinkByRowID(ctx context.Context, rowID int64) (DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(s.db).GetDeliveryTicketProductionLinkByRowID(ctx, rowID)
}

func (s *Store) ListDeliveryTicketProductionLinksByTicket(ctx context.Context, ticketRowID int64) ([]DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketProductionLinksByTicket(ctx, ticketRowID)
}

func (s *Store) ListDeliveryTicketProductionLinksByCandidate(ctx context.Context, candidateRowID int64) ([]DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(s.db).ListDeliveryTicketProductionLinksByCandidate(ctx, candidateRowID)
}

func (tx *Tx) CreateDeliveryTicketProductionLink(ctx context.Context, params CreateDeliveryTicketProductionLinkParams) (DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(tx.tx).CreateDeliveryTicketProductionLink(ctx, params)
}

func (tx *Tx) GetDeliveryTicketProductionLinkByLinkID(ctx context.Context, linkID string) (DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketProductionLinkByLinkID(ctx, linkID)
}

func (tx *Tx) GetDeliveryTicketProductionLinkByRowID(ctx context.Context, rowID int64) (DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(tx.tx).GetDeliveryTicketProductionLinkByRowID(ctx, rowID)
}

func (tx *Tx) ListDeliveryTicketProductionLinksByTicket(ctx context.Context, ticketRowID int64) ([]DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryTicketProductionLinksByTicket(ctx, ticketRowID)
}

func (tx *Tx) ListDeliveryTicketProductionLinksByCandidate(ctx context.Context, candidateRowID int64) ([]DeliveryTicketProductionLink, error) {
	return workflowgenerated.New(tx.tx).ListDeliveryTicketProductionLinksByCandidate(ctx, candidateRowID)
}

// ReadPlanningCandidateBytes verifies the durable artifact path, digest, and
// size before returning bytes. The caller supplies a bound maximum to avoid
// unbounded artifact reads; errors never include artifact contents.
func (s *Store) ReadPlanningCandidateBytes(ctx context.Context, candidateID string, maxBytes int) ([]byte, error) {
	candidate, err := s.GetPlanningCandidateByCandidateID(ctx, candidateID)
	if err != nil {
		return nil, err
	}
	artifact, err := getFeatureWorkspaceDiscoveryArtifactByRowID(ctx, s.db, candidate.ArtifactRowID)
	if err != nil {
		return nil, err
	}
	return readPlanningCandidateArtifact(ctx, candidate, artifact, s.artifacts, maxBytes)
}

func (tx *Tx) ReadPlanningCandidateBytes(ctx context.Context, candidateID string, maxBytes int) ([]byte, error) {
	candidate, err := tx.GetPlanningCandidateByCandidateID(ctx, candidateID)
	if err != nil {
		return nil, err
	}
	artifact, err := getFeatureWorkspaceDiscoveryArtifactByRowID(ctx, tx.tx, candidate.ArtifactRowID)
	if err != nil {
		return nil, err
	}
	return readPlanningCandidateArtifact(ctx, candidate, artifact, tx.artifacts, maxBytes)
}

func readPlanningCandidateArtifact(_ context.Context, candidate PlanningCandidate, artifact FeatureWorkspaceDiscoveryArtifact, artifactStore *workflowartifacts.Store, maxBytes int) ([]byte, error) {
	if artifactStore == nil {
		return nil, fmt.Errorf("workflow artifact store is required")
	}
	if artifact.ID != candidate.ArtifactRowID || artifact.WorkspaceRowID != candidate.WorkspaceRowID || artifact.Sha256 != candidate.ArtifactSha256 || artifact.SizeBytes != candidate.ArtifactSizeBytes {
		return nil, fmt.Errorf("planning candidate artifact metadata does not match")
	}
	_, data, err := artifactStore.ReadVerifiedFile(workflowartifacts.File{
		Kind:         artifactKind(candidate),
		RelativePath: artifact.RelativePath,
		SHA256:       candidate.ArtifactSha256,
		SizeBytes:    candidate.ArtifactSizeBytes,
	}, maxBytes)
	return data, err
}

// ReadCandidateBytes is a short application-neutral alias for callers that do
// not need the persistence table name in their dependency surface.
func (s *Store) ReadCandidateBytes(ctx context.Context, candidateID string, maxBytes int) ([]byte, error) {
	return s.ReadPlanningCandidateBytes(ctx, candidateID, maxBytes)
}

func getFeatureWorkspaceDiscoveryArtifactByRowID(ctx context.Context, queryer rowQueryer, rowID int64) (FeatureWorkspaceDiscoveryArtifact, error) {
	var value FeatureWorkspaceDiscoveryArtifact
	err := queryer.QueryRowContext(ctx, `
SELECT id, discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes, created_at
FROM feature_workspace_discovery_artifacts
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.DiscoveryArtifactID, &value.WorkspaceRowID, &value.RelativePath,
		&value.Sha256, &value.MediaType, &value.SizeBytes, &value.CreatedAt,
	)
	return value, err
}

func artifactKind(candidate PlanningCandidate) string {
	return "planning_candidate_" + candidate.Family
}

func (s *Store) GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx context.Context, rowID int64) (FeatureWorkspaceDiscoveryArtifact, error) {
	return getFeatureWorkspaceDiscoveryArtifactByRowID(ctx, s.db, rowID)
}

func (tx *Tx) GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx context.Context, rowID int64) (FeatureWorkspaceDiscoveryArtifact, error) {
	return getFeatureWorkspaceDiscoveryArtifactByRowID(ctx, tx.tx, rowID)
}

func (s *Store) GetDeliveryTicketProductionLinkByProducedRevision(ctx context.Context, revisionRowID int64) (DeliveryTicketProductionLink, error) {
	var linkRowID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM delivery_ticket_production_links WHERE produced_revision_row_id = ?`, revisionRowID).Scan(&linkRowID); err != nil {
		return DeliveryTicketProductionLink{}, err
	}
	return s.GetDeliveryTicketProductionLinkByRowID(ctx, linkRowID)
}

func (tx *Tx) GetDeliveryTicketProductionLinkByProducedRevision(ctx context.Context, revisionRowID int64) (DeliveryTicketProductionLink, error) {
	var linkRowID int64
	if err := tx.tx.QueryRowContext(ctx, `SELECT id FROM delivery_ticket_production_links WHERE produced_revision_row_id = ?`, revisionRowID).Scan(&linkRowID); err != nil {
		return DeliveryTicketProductionLink{}, err
	}
	return tx.GetDeliveryTicketProductionLinkByRowID(ctx, linkRowID)
}
