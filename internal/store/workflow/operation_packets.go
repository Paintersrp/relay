package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
)

const operationPacketColumns = `
    id, packet_id, packet_sha256, schema_version, role, operation_id,
    surface_contract_id, project_id, readiness_state, lifecycle_state,
    prior_packet_row_id, replacement_packet_row_id, created_at, superseded_at,
    closed_at, packet_artifact_row_id`

const operationPacketArtifactColumns = `
    id, artifact_id, kind, relative_path, media_type, sha256, size_bytes, created_at`

const operationPacketDependencyColumns = `
    id, packet_row_id, dependency_class, dependency_key, required, attached,
    retained, owner_identity, created_at, updated_at`

func (s *Store) GetOperationPacketByPacketID(ctx context.Context, packetID string) (OperationPacket, error) {
	return getOperationPacketByPacketID(ctx, s.db, packetID)
}
func (s *Store) GetOperationPacketByRowID(ctx context.Context, rowID int64) (OperationPacket, error) {
	return getOperationPacketByRowID(ctx, s.db, rowID)
}
func (s *Store) GetOperationPacketArtifact(ctx context.Context, packetRowID int64) (OperationPacketArtifact, error) {
	return getOperationPacketArtifact(ctx, s.db, packetRowID)
}
func (s *Store) GetOperationPacketReplacement(ctx context.Context, packetRowID int64) (OperationPacketReplacement, error) {
	return getOperationPacketReplacement(ctx, s.db, packetRowID)
}
func (s *Store) ListOperationPacketRetentionDependencies(ctx context.Context, packetRowID int64) ([]OperationPacketRetentionDependency, error) {
	return listOperationPacketRetentionDependencies(ctx, s.db, packetRowID)
}
func (s *Store) GetOperationPacketRetentionDependency(ctx context.Context, packetRowID int64, dependencyClass, dependencyKey string) (OperationPacketRetentionDependency, error) {
	return getOperationPacketRetentionDependency(ctx, s.db, packetRowID, dependencyClass, dependencyKey)
}

func getOperationPacketByPacketID(ctx context.Context, query rowQueryer, packetID string) (OperationPacket, error) {
	return scanOperationPacket(query.QueryRowContext(ctx, `SELECT `+operationPacketColumns+` FROM operation_packets WHERE packet_id = ?`, packetID))
}
func getOperationPacketByRowID(ctx context.Context, query rowQueryer, rowID int64) (OperationPacket, error) {
	return scanOperationPacket(query.QueryRowContext(ctx, `SELECT `+operationPacketColumns+` FROM operation_packets WHERE id = ?`, rowID))
}
func getOperationPacketArtifact(ctx context.Context, query rowQueryer, packetRowID int64) (OperationPacketArtifact, error) {
	return scanOperationPacketArtifact(query.QueryRowContext(ctx, `SELECT `+operationPacketArtifactColumns+` FROM operation_packet_artifacts WHERE id = (SELECT packet_artifact_row_id FROM operation_packets WHERE id = ?)`, packetRowID))
}
func getOperationPacketReplacement(ctx context.Context, query rowQueryer, packetRowID int64) (OperationPacketReplacement, error) {
	var value OperationPacketReplacement
	err := query.QueryRowContext(ctx, `SELECT replacement.packet_id, replacement.packet_sha256, replacement.role, replacement.operation_id, replacement.surface_contract_id FROM operation_packets AS prior JOIN operation_packets AS replacement ON replacement.id = prior.replacement_packet_row_id WHERE prior.id = ?`, packetRowID).Scan(&value.PacketID, &value.PacketSHA256, &value.Role, &value.OperationID, &value.SurfaceContractID)
	return value, err
}
func listOperationPacketRetentionDependencies(ctx context.Context, query rowsQueryer, packetRowID int64) ([]OperationPacketRetentionDependency, error) {
	rows, err := query.QueryContext(ctx, `SELECT `+operationPacketDependencyColumns+` FROM operation_packet_retention_dependencies WHERE packet_row_id = ? ORDER BY dependency_class, dependency_key`, packetRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []OperationPacketRetentionDependency
	for rows.Next() {
		value, err := scanOperationPacketDependency(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}
func getOperationPacketRetentionDependency(ctx context.Context, query rowQueryer, packetRowID int64, dependencyClass, dependencyKey string) (OperationPacketRetentionDependency, error) {
	return scanOperationPacketDependency(query.QueryRowContext(ctx, `SELECT `+operationPacketDependencyColumns+` FROM operation_packet_retention_dependencies WHERE packet_row_id = ? AND dependency_class = ? AND dependency_key = ?`, packetRowID, dependencyClass, dependencyKey))
}
func scanOperationPacket(row rowScanner) (OperationPacket, error) {
	var value OperationPacket
	err := row.Scan(&value.ID, &value.PacketID, &value.PacketSHA256, &value.SchemaVersion, &value.Role, &value.OperationID, &value.SurfaceContractID, &value.ProjectID, &value.ReadinessState, &value.LifecycleState, &value.PriorPacketRowID, &value.ReplacementPacketRowID, &value.CreatedAt, &value.SupersededAt, &value.ClosedAt, &value.PacketArtifactRowID)
	return value, err
}
func scanOperationPacketArtifact(row rowScanner) (OperationPacketArtifact, error) {
	var value OperationPacketArtifact
	err := row.Scan(&value.ID, &value.ArtifactID, &value.Kind, &value.RelativePath, &value.MediaType, &value.SHA256, &value.SizeBytes, &value.CreatedAt)
	return value, err
}
func scanOperationPacketDependency(row rowScanner) (OperationPacketRetentionDependency, error) {
	var value OperationPacketRetentionDependency
	var required, attached, retained int64
	err := row.Scan(&value.ID, &value.PacketRowID, &value.DependencyClass, &value.DependencyKey, &required, &attached, &retained, &value.OwnerIdentity, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return OperationPacketRetentionDependency{}, err
	}
	if (required != 0 && required != 1) || (attached != 0 && attached != 1) || (retained != 0 && retained != 1) {
		return OperationPacketRetentionDependency{}, fmt.Errorf("invalid operation packet dependency boolean")
	}
	value.Required, value.Attached, value.Retained = required == 1, attached == 1, retained == 1
	return value, nil
}
func operationPacketDependencyBool(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
func operationPacketOptionalReplacement(ctx context.Context, query rowQueryer, packet OperationPacket) (OperationPacketReplacement, bool, error) {
	if !packet.ReplacementPacketRowID.Valid {
		return OperationPacketReplacement{}, false, nil
	}
	value, err := getOperationPacketReplacement(ctx, query, packet.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return OperationPacketReplacement{}, false, fmt.Errorf("operation packet replacement relationship is incomplete")
		}
		return OperationPacketReplacement{}, false, err
	}
	return value, true, nil
}
