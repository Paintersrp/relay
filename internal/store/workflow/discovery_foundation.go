package workflowstore

import (
	"context"
	"database/sql"
)

type DiscoveryArtifact struct {
	ID, WorkspaceRowID, SizeBytes                                   int64
	DiscoveryArtifactID, RelativePath, SHA256, MediaType, CreatedAt string
}

type IntegratedDiscoveryRevision struct {
	ID, WorkspaceRowID, RevisionNumber, ArtifactRowID int64
	DiscoveryRevisionID                               string
	PredecessorRevisionRowID                          sql.NullInt64
	CreatedIdentity, CreatedAt                        string
}

type DiscoveryWorkItemMetadata struct {
	TicketRowID                  int64
	WorkItemKind                 string
	RouteMaterial, LegacyAdopted bool
	UpdatedAt                    string
}

type DiscoveryIntegrationConsequence struct {
	ID, WorkspaceRowID, TicketRowID, ResolutionRowID                    int64
	IntegrationConsequenceID, ConsequenceKind, EvidenceBasis, CreatedAt string
	ProducedRevisionRowID, ReplacementTicketRowID                       sql.NullInt64
}

func (s *Store) GetDiscoveryArtifactByRowID(ctx context.Context, id int64) (DiscoveryArtifact, error) {
	return getDiscoveryArtifactByRowID(ctx, s.db, id)
}
func (s *Store) GetCurrentIntegratedDiscoveryRevision(ctx context.Context, workspaceID string) (IntegratedDiscoveryRevision, error) {
	return getCurrentIntegratedDiscoveryRevision(ctx, s.db, workspaceID)
}
func (s *Store) ListDiscoveryWorkItemMetadata(ctx context.Context, workspaceRowID int64) ([]DiscoveryWorkItemMetadata, error) {
	return listDiscoveryWorkItemMetadata(ctx, s.db, workspaceRowID)
}
func (s *Store) ListDiscoveryIntegrationConsequences(ctx context.Context, workspaceRowID int64) ([]DiscoveryIntegrationConsequence, error) {
	return listDiscoveryIntegrationConsequences(ctx, s.db, workspaceRowID)
}
func (s *Store) WithDiscoveryCapability(ctx context.Context, workspaceID string, expectedVersion int64, enabled bool) (FeatureWorkspace, error) {
	var result FeatureWorkspace
	err := s.WithTx(ctx, func(tx *Tx) error {
		var err error
		result, err = tx.SetDiscoveryCapability(ctx, workspaceID, enabled, expectedVersion)
		return err
	})
	return result, err
}

func (tx *Tx) GetDiscoveryArtifactByRowID(ctx context.Context, id int64) (DiscoveryArtifact, error) {
	return getDiscoveryArtifactByRowID(ctx, tx.tx, id)
}
func (tx *Tx) GetCurrentIntegratedDiscoveryRevision(ctx context.Context, workspaceID string) (IntegratedDiscoveryRevision, error) {
	return getCurrentIntegratedDiscoveryRevision(ctx, tx.tx, workspaceID)
}
func (tx *Tx) ListDiscoveryWorkItemMetadata(ctx context.Context, workspaceRowID int64) ([]DiscoveryWorkItemMetadata, error) {
	return listDiscoveryWorkItemMetadata(ctx, tx.tx, workspaceRowID)
}
func (tx *Tx) ListDiscoveryIntegrationConsequences(ctx context.Context, workspaceRowID int64) ([]DiscoveryIntegrationConsequence, error) {
	return listDiscoveryIntegrationConsequences(ctx, tx.tx, workspaceRowID)
}

func (tx *Tx) CreateDiscoveryArtifact(ctx context.Context, value DiscoveryArtifact) (DiscoveryArtifact, error) {
	return scanDiscoveryArtifact(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES (?, ?, ?, ?, ?, ?) RETURNING id, discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes, created_at`, value.DiscoveryArtifactID, value.WorkspaceRowID, value.RelativePath, value.SHA256, value.MediaType, value.SizeBytes))
}
func (tx *Tx) CreateIntegratedDiscoveryRevision(ctx context.Context, value IntegratedDiscoveryRevision) (IntegratedDiscoveryRevision, error) {
	return scanIntegratedDiscoveryRevision(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, predecessor_revision_row_id, created_identity) VALUES (?, ?, ?, ?, ?, ?) RETURNING id, discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, predecessor_revision_row_id, created_identity, created_at`, value.DiscoveryRevisionID, value.WorkspaceRowID, value.RevisionNumber, value.ArtifactRowID, value.PredecessorRevisionRowID, value.CreatedIdentity))
}
func (tx *Tx) UpsertDiscoveryWorkItemMetadata(ctx context.Context, ticketRowID int64, kind string, routeMaterial bool) (DiscoveryWorkItemMetadata, error) {
	return scanDiscoveryWorkItemMetadata(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_work_item_metadata (ticket_row_id, work_item_kind, route_material) VALUES (?, ?, ?) ON CONFLICT(ticket_row_id) DO UPDATE SET work_item_kind = excluded.work_item_kind, route_material = excluded.route_material, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') RETURNING ticket_row_id, work_item_kind, route_material, legacy_adopted, updated_at`, ticketRowID, kind, routeMaterial))
}
func (tx *Tx) CreateDiscoveryIntegrationConsequence(ctx context.Context, value DiscoveryIntegrationConsequence) (DiscoveryIntegrationConsequence, error) {
	return scanDiscoveryIntegrationConsequence(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_integration_consequences (integration_consequence_id, workspace_row_id, ticket_row_id, resolution_row_id, consequence_kind, produced_revision_row_id, replacement_ticket_row_id, evidence_basis) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, integration_consequence_id, workspace_row_id, ticket_row_id, resolution_row_id, consequence_kind, produced_revision_row_id, replacement_ticket_row_id, evidence_basis, created_at`, value.IntegrationConsequenceID, value.WorkspaceRowID, value.TicketRowID, value.ResolutionRowID, value.ConsequenceKind, value.ProducedRevisionRowID, value.ReplacementTicketRowID, value.EvidenceBasis))
}

func (tx *Tx) SetDiscoveryCapability(ctx context.Context, workspaceID string, enabled bool, expectedVersion int64) (FeatureWorkspace, error) {
	return updateFeatureWorkspace(ctx, tx.tx, `discovery_capability_enabled = ?`, enabled, workspaceID, expectedVersion)
}
func (tx *Tx) SetCurrentIntegratedDiscoveryRevision(ctx context.Context, workspaceID string, revisionRowID, expectedVersion int64) (FeatureWorkspace, error) {
	return updateFeatureWorkspace(ctx, tx.tx, `current_discovery_revision_row_id = ?`, revisionRowID, workspaceID, expectedVersion)
}
func (tx *Tx) BumpDiscoveryWorkItemVersion(ctx context.Context, ticketID string, expectedVersion int64) (FeatureWorkspaceDiscoveryTicket, error) {
	return workflowgeneratedTicket(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_discovery_tickets SET version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE discovery_ticket_id = ? AND version = ? RETURNING id, discovery_ticket_id, workspace_row_id, ticket_key, subject, state, version, created_at, updated_at`, ticketID, expectedVersion))
}

func updateFeatureWorkspace(ctx context.Context, q rowQueryer, set string, value any, workspaceID string, expectedVersion int64) (FeatureWorkspace, error) {
	var workspace FeatureWorkspace
	err := q.QueryRowContext(ctx, `UPDATE feature_workspaces SET `+set+`, version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE workspace_id = ? AND version = ? RETURNING id, workspace_id, project_row_id, feature_slug, state, version, current_route_state_row_id, current_authority_revision_row_id, discovery_capability_enabled, current_discovery_revision_row_id, created_at, updated_at`, value, workspaceID, expectedVersion).Scan(&workspace.ID, &workspace.WorkspaceID, &workspace.ProjectRowID, &workspace.FeatureSlug, &workspace.State, &workspace.Version, &workspace.CurrentRouteStateRowID, &workspace.CurrentAuthorityRevisionRowID, &workspace.DiscoveryCapabilityEnabled, &workspace.CurrentDiscoveryRevisionRowID, &workspace.CreatedAt, &workspace.UpdatedAt)
	return workspace, err
}
func getDiscoveryArtifactByRowID(ctx context.Context, q rowQueryer, id int64) (DiscoveryArtifact, error) {
	return scanDiscoveryArtifact(q.QueryRowContext(ctx, `SELECT id, discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes, created_at FROM feature_workspace_discovery_artifacts WHERE id = ?`, id))
}
func getCurrentIntegratedDiscoveryRevision(ctx context.Context, q rowQueryer, workspaceID string) (IntegratedDiscoveryRevision, error) {
	return scanIntegratedDiscoveryRevision(q.QueryRowContext(ctx, `SELECT r.id, r.discovery_revision_id, r.workspace_row_id, r.revision_number, r.artifact_row_id, r.predecessor_revision_row_id, r.created_identity, r.created_at FROM feature_workspaces AS w JOIN feature_workspace_integrated_discovery_revisions AS r ON r.id = w.current_discovery_revision_row_id WHERE w.workspace_id = ?`, workspaceID))
}
func listDiscoveryWorkItemMetadata(ctx context.Context, q rowsQueryer, workspaceRowID int64) ([]DiscoveryWorkItemMetadata, error) {
	rows, err := q.QueryContext(ctx, `SELECT m.ticket_row_id, m.work_item_kind, m.route_material, m.legacy_adopted, m.updated_at FROM feature_workspace_discovery_work_item_metadata AS m JOIN feature_workspace_discovery_tickets AS t ON t.id = m.ticket_row_id WHERE t.workspace_row_id = ? ORDER BY t.id`, workspaceRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DiscoveryWorkItemMetadata
	for rows.Next() {
		value, err := scanDiscoveryWorkItemMetadata(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func listDiscoveryIntegrationConsequences(ctx context.Context, q rowsQueryer, workspaceRowID int64) ([]DiscoveryIntegrationConsequence, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, integration_consequence_id, workspace_row_id, ticket_row_id, resolution_row_id, consequence_kind, produced_revision_row_id, replacement_ticket_row_id, evidence_basis, created_at FROM feature_workspace_discovery_integration_consequences WHERE workspace_row_id = ? ORDER BY id`, workspaceRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DiscoveryIntegrationConsequence
	for rows.Next() {
		value, err := scanDiscoveryIntegrationConsequence(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
func scanDiscoveryArtifact(row rowScanner) (DiscoveryArtifact, error) {
	var v DiscoveryArtifact
	err := row.Scan(&v.ID, &v.DiscoveryArtifactID, &v.WorkspaceRowID, &v.RelativePath, &v.SHA256, &v.MediaType, &v.SizeBytes, &v.CreatedAt)
	return v, err
}
func scanIntegratedDiscoveryRevision(row rowScanner) (IntegratedDiscoveryRevision, error) {
	var v IntegratedDiscoveryRevision
	err := row.Scan(&v.ID, &v.DiscoveryRevisionID, &v.WorkspaceRowID, &v.RevisionNumber, &v.ArtifactRowID, &v.PredecessorRevisionRowID, &v.CreatedIdentity, &v.CreatedAt)
	return v, err
}
func scanDiscoveryWorkItemMetadata(row rowScanner) (DiscoveryWorkItemMetadata, error) {
	var v DiscoveryWorkItemMetadata
	err := row.Scan(&v.TicketRowID, &v.WorkItemKind, &v.RouteMaterial, &v.LegacyAdopted, &v.UpdatedAt)
	return v, err
}
func scanDiscoveryIntegrationConsequence(row rowScanner) (DiscoveryIntegrationConsequence, error) {
	var v DiscoveryIntegrationConsequence
	err := row.Scan(&v.ID, &v.IntegrationConsequenceID, &v.WorkspaceRowID, &v.TicketRowID, &v.ResolutionRowID, &v.ConsequenceKind, &v.ProducedRevisionRowID, &v.ReplacementTicketRowID, &v.EvidenceBasis, &v.CreatedAt)
	return v, err
}
func workflowgeneratedTicket(row rowScanner) (FeatureWorkspaceDiscoveryTicket, error) {
	var v FeatureWorkspaceDiscoveryTicket
	err := row.Scan(&v.ID, &v.DiscoveryTicketID, &v.WorkspaceRowID, &v.TicketKey, &v.Subject, &v.State, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
