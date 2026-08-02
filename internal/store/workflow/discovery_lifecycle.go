package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

type (
	DiscoveryLifecycleAdoption     = workflowgenerated.FeatureWorkspaceDiscoveryAdoption
	DiscoveryDestinationAssessment = workflowgenerated.FeatureWorkspaceDiscoveryDestinationAssessment
	DiscoveryClosurePacket         = workflowgenerated.FeatureWorkspaceDiscoveryClosurePacket
	DiscoveryClosurePacketMember   = workflowgenerated.FeatureWorkspaceDiscoveryClosurePacketMember
	DiscoveryReopenEvent           = workflowgenerated.FeatureWorkspaceDiscoveryReopenEvent
)

func (s *Store) GetDiscoveryLifecycleAdoption(ctx context.Context, workspaceRowID int64) (DiscoveryLifecycleAdoption, error) {
	return workflowgenerated.New(s.db).GetFeatureWorkspaceDiscoveryAdoption(ctx, workspaceRowID)
}
func (s *Store) GetDiscoveryClosurePacket(ctx context.Context, packetID string) (DiscoveryClosurePacket, error) {
	return workflowgenerated.New(s.db).GetFeatureWorkspaceDiscoveryClosurePacketByID(ctx, packetID)
}
func (s *Store) ListDiscoveryClosurePacketMembers(ctx context.Context, packetRowID int64) ([]DiscoveryClosurePacketMember, error) {
	return workflowgenerated.New(s.db).ListFeatureWorkspaceDiscoveryClosurePacketMembers(ctx, packetRowID)
}
func (tx *Tx) GetDiscoveryLifecycleAdoption(ctx context.Context, workspaceRowID int64) (DiscoveryLifecycleAdoption, error) {
	return workflowgenerated.New(tx.tx).GetFeatureWorkspaceDiscoveryAdoption(ctx, workspaceRowID)
}
func (tx *Tx) GetDiscoveryClosurePacket(ctx context.Context, packetID string) (DiscoveryClosurePacket, error) {
	return workflowgenerated.New(tx.tx).GetFeatureWorkspaceDiscoveryClosurePacketByID(ctx, packetID)
}
func (tx *Tx) GetDiscoveryClosurePacketByRowID(ctx context.Context, rowID int64) (DiscoveryClosurePacket, error) {
	var v DiscoveryClosurePacket
	err := tx.tx.QueryRowContext(ctx, `SELECT id, closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type, created_at FROM feature_workspace_discovery_closure_packets WHERE id = ?`, rowID).Scan(&v.ID, &v.ClosurePacketID, &v.WorkspaceRowID, &v.ClosingRevisionRowID, &v.Destination, &v.ManifestArtifactRowID, &v.ManifestSha256, &v.ManifestSizeBytes, &v.ManifestMediaType, &v.CreatedAt)
	return v, err
}
func (tx *Tx) CreateDiscoveryLifecycleAdoption(ctx context.Context, workspaceRowID int64, adoptionID, operator string, version int64) (DiscoveryLifecycleAdoption, error) {
	var v DiscoveryLifecycleAdoption
	err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_adoptions (workspace_row_id, adoption_id, operator_identity, adopted_workspace_version) VALUES (?, ?, ?, ?) RETURNING id, workspace_row_id, adoption_id, operator_identity, adopted_workspace_version, created_at`, workspaceRowID, adoptionID, operator, version).Scan(&v.ID, &v.WorkspaceRowID, &v.AdoptionID, &v.OperatorIdentity, &v.AdoptedWorkspaceVersion, &v.CreatedAt)
	return v, err
}
func (tx *Tx) CreateDiscoveryDestinationAssessment(ctx context.Context, v DiscoveryDestinationAssessment) (DiscoveryDestinationAssessment, error) {
	err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_destination_assessments (assessment_id, workspace_row_id, discovery_revision_row_id, workspace_version, discovery_state, destination, rationale, blockers_json, restoration_actions_json, continuation_json, created_identity) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, assessment_id, workspace_row_id, discovery_revision_row_id, workspace_version, discovery_state, destination, rationale, blockers_json, restoration_actions_json, continuation_json, created_identity, created_at`, v.AssessmentID, v.WorkspaceRowID, v.DiscoveryRevisionRowID, v.WorkspaceVersion, v.DiscoveryState, v.Destination, v.Rationale, v.BlockersJson, v.RestorationActionsJson, v.ContinuationJson, v.CreatedIdentity).Scan(&v.ID, &v.AssessmentID, &v.WorkspaceRowID, &v.DiscoveryRevisionRowID, &v.WorkspaceVersion, &v.DiscoveryState, &v.Destination, &v.Rationale, &v.BlockersJson, &v.RestorationActionsJson, &v.ContinuationJson, &v.CreatedIdentity, &v.CreatedAt)
	return v, err
}
func (tx *Tx) CreateDiscoveryClosurePacket(ctx context.Context, v DiscoveryClosurePacket) (DiscoveryClosurePacket, error) {
	err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type, created_at`, v.ClosurePacketID, v.WorkspaceRowID, v.ClosingRevisionRowID, v.Destination, v.ManifestArtifactRowID, v.ManifestSha256, v.ManifestSizeBytes, v.ManifestMediaType).Scan(&v.ID, &v.ClosurePacketID, &v.WorkspaceRowID, &v.ClosingRevisionRowID, &v.Destination, &v.ManifestArtifactRowID, &v.ManifestSha256, &v.ManifestSizeBytes, &v.ManifestMediaType, &v.CreatedAt)
	return v, err
}
func (tx *Tx) CreateDiscoveryClosurePacketMember(ctx context.Context, v DiscoveryClosurePacketMember) (DiscoveryClosurePacketMember, error) {
	err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packet_members (closure_packet_row_id, sequence, owner_family, artifact_row_id, source_identity, sha256, size_bytes, media_type, semantic_role) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, closure_packet_row_id, sequence, owner_family, artifact_row_id, source_identity, sha256, size_bytes, media_type, semantic_role, created_at`, v.ClosurePacketRowID, v.Sequence, v.OwnerFamily, v.ArtifactRowID, v.SourceIdentity, v.Sha256, v.SizeBytes, v.MediaType, v.SemanticRole).Scan(&v.ID, &v.ClosurePacketRowID, &v.Sequence, &v.OwnerFamily, &v.ArtifactRowID, &v.SourceIdentity, &v.Sha256, &v.SizeBytes, &v.MediaType, &v.SemanticRole, &v.CreatedAt)
	return v, err
}
func (tx *Tx) SetCurrentDiscoveryClosurePacket(ctx context.Context, workspaceID string, packetRowID sql.NullInt64, expectedVersion int64) (FeatureWorkspace, error) {
	return updateFeatureWorkspace(ctx, tx.tx, "current_discovery_closure_packet_row_id = ?", packetRowID, workspaceID, expectedVersion)
}
func (tx *Tx) CreateDiscoveryReopenEvent(ctx context.Context, v DiscoveryReopenEvent) (DiscoveryReopenEvent, error) {
	err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_reopen_events (reopen_event_id, workspace_row_id, closure_packet_row_id, replacement_revision_row_id, cause_text, confirmed_operator_identity) VALUES (?, ?, ?, ?, ?, ?) RETURNING id, reopen_event_id, workspace_row_id, closure_packet_row_id, replacement_revision_row_id, cause_text, confirmed_operator_identity, created_at`, v.ReopenEventID, v.WorkspaceRowID, v.ClosurePacketRowID, v.ReplacementRevisionRowID, v.CauseText, v.ConfirmedOperatorIdentity).Scan(&v.ID, &v.ReopenEventID, &v.WorkspaceRowID, &v.ClosurePacketRowID, &v.ReplacementRevisionRowID, &v.CauseText, &v.ConfirmedOperatorIdentity, &v.CreatedAt)
	return v, err
}

// SetDiscoveryReopenPointers moves the two currentness pointers in one
// optimistic workspace update; callers must have already created the revision.
func (tx *Tx) SetDiscoveryReopenPointers(ctx context.Context, workspaceID string, revisionRowID, expectedVersion int64) (FeatureWorkspace, error) {
	var v FeatureWorkspace
	err := tx.tx.QueryRowContext(ctx, `UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = NULL, version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE workspace_id = ? AND version = ? RETURNING id, workspace_id, project_row_id, feature_slug, state, version, current_route_state_row_id, current_authority_revision_row_id, discovery_capability_enabled, current_discovery_revision_row_id, current_discovery_closure_packet_row_id, created_at, updated_at`, revisionRowID, workspaceID, expectedVersion).Scan(&v.ID, &v.WorkspaceID, &v.ProjectRowID, &v.FeatureSlug, &v.State, &v.Version, &v.CurrentRouteStateRowID, &v.CurrentAuthorityRevisionRowID, &v.DiscoveryCapabilityEnabled, &v.CurrentDiscoveryRevisionRowID, &v.CurrentDiscoveryClosurePacketRowID, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
