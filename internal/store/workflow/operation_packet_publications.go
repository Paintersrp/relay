package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	workflowartifacts "relay/internal/artifacts/workflow"
)

const operationPacketPublicationColumns = `
    id, publication_id, packet_row_id, packet_artifact_row_id, mutation_result_row_id,
    namespace, manifest_sha256, expected_retained_artifact_count, expected_binding_count,
    expected_dependency_count, expected_vault_relationship_count, state, created_at`

const operationPacketRetainedArtifactColumns = `
    id, publication_id, artifact_id, kind, relative_path, media_type, sha256, size_bytes, created_at`

const operationPacketArtifactBindingColumns = `
    id, publication_id, packet_row_id, sequence, dependency_class, dependency_key,
    packet_artifact_row_id, retained_artifact_row_id, created_at`

const operationPacketVaultRelationshipColumns = `
    id, publication_id, packet_row_id, dependency_class, dependency_key, owner_identity,
    retention_row_id, closure_row_id, vault_row_id, commit_oid, tree_oid, created_at`

func (s *Store) CommitOperationPacketPublication(ctx context.Context, batch *workflowartifacts.PublicationBatch, fn func(*Tx) error) (err error) {
	if batch == nil || !batch.IsSealed() {
		return fmt.Errorf("sealed publication batch is required")
	}
	if fn == nil {
		_ = batch.Rollback()
		return fmt.Errorf("publication transaction callback is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = batch.Rollback()
		return fmt.Errorf("begin publication transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback publication transaction: %w", rollbackErr))
		}
		if artifactErr := batch.Rollback(); artifactErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback publication artifacts: %w", artifactErr))
		}
	}()

	wrapped := &Tx{tx: tx}
	if err := fn(wrapped); err != nil {
		return err
	}
	publication, err := wrapped.GetOperationPacketPublication(ctx, batch.PublicationID())
	if err != nil {
		return fmt.Errorf("load publication commit marker: %w", err)
	}
	manifest := batch.Manifest()
	if publication.Namespace != batch.Namespace() || publication.ManifestSHA256 != batch.ManifestSHA256() ||
		publication.ExpectedRetainedArtifactCount != manifest.Expectations.RetainedArtifactCount ||
		publication.ExpectedBindingCount != manifest.Expectations.BindingCount ||
		publication.ExpectedDependencyCount != manifest.Expectations.DependencyCount ||
		publication.ExpectedVaultRelationshipCount != manifest.Expectations.VaultRelationshipCount {
		return fmt.Errorf("publication commit marker does not match sealed manifest")
	}
	if err := batch.Promote(); err != nil {
		return fmt.Errorf("promote publication artifacts: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit publication transaction: %w", err)
	}
	batch.Commit()
	committed = true
	return nil
}

func (tx *Tx) CreateOperationPacketRetainedArtifact(ctx context.Context, params CreateOperationPacketRetainedArtifactParams) (OperationPacketRetainedArtifact, error) {
	return scanOperationPacketRetainedArtifact(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packet_retained_artifacts (
    publication_id, artifact_id, kind, relative_path, media_type, sha256, size_bytes
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING `+operationPacketRetainedArtifactColumns,
		params.PublicationID,
		params.ArtifactID,
		params.Kind,
		params.RelativePath,
		params.MediaType,
		params.SHA256,
		params.SizeBytes,
	))
}

func (tx *Tx) CreateOperationPacketArtifactBinding(ctx context.Context, params CreateOperationPacketArtifactBindingParams) (OperationPacketArtifactBinding, error) {
	return scanOperationPacketArtifactBinding(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packet_artifact_bindings (
    publication_id, packet_row_id, sequence, dependency_class, dependency_key,
    packet_artifact_row_id, retained_artifact_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING `+operationPacketArtifactBindingColumns,
		params.PublicationID,
		params.PacketRowID,
		params.Sequence,
		params.DependencyClass,
		params.DependencyKey,
		params.PacketArtifactRowID,
		params.RetainedArtifactRowID,
	))
}

func (tx *Tx) CreateOperationPacketVaultRelationship(ctx context.Context, params CreateOperationPacketVaultRelationshipParams) (OperationPacketVaultRelationship, error) {
	return scanOperationPacketVaultRelationship(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packet_vault_relationships (
    publication_id, packet_row_id, dependency_class, dependency_key, owner_identity,
    retention_row_id, closure_row_id, vault_row_id, commit_oid, tree_oid
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+operationPacketVaultRelationshipColumns,
		params.PublicationID,
		params.PacketRowID,
		params.DependencyClass,
		params.DependencyKey,
		params.OwnerIdentity,
		params.RetentionRowID,
		params.ClosureRowID,
		params.VaultRowID,
		params.CommitOID,
		params.TreeOID,
	))
}

func (tx *Tx) CreateOperationPacketPublication(ctx context.Context, params CreateOperationPacketPublicationParams) (OperationPacketPublication, error) {
	return scanOperationPacketPublication(tx.tx.QueryRowContext(ctx, `
INSERT INTO operation_packet_publications (
    publication_id, packet_row_id, packet_artifact_row_id, mutation_result_row_id,
    namespace, manifest_sha256, expected_retained_artifact_count, expected_binding_count,
    expected_dependency_count, expected_vault_relationship_count, state
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'committed')
RETURNING `+operationPacketPublicationColumns,
		params.PublicationID,
		params.PacketRowID,
		params.PacketArtifactRowID,
		params.MutationResultRowID,
		params.Namespace,
		params.ManifestSHA256,
		params.ExpectedRetainedArtifactCount,
		params.ExpectedBindingCount,
		params.ExpectedDependencyCount,
		params.ExpectedVaultRelationshipCount,
	))
}

func (tx *Tx) GetOperationPacketPublication(ctx context.Context, publicationID string) (OperationPacketPublication, error) {
	return getOperationPacketPublication(ctx, tx.tx, publicationID)
}

func (s *Store) GetOperationPacketPublication(ctx context.Context, publicationID string) (OperationPacketPublication, error) {
	return getOperationPacketPublication(ctx, s.db, publicationID)
}

func (s *Store) GetOperationPacketPublicationByPacketID(ctx context.Context, packetID string) (OperationPacketPublication, error) {
	return scanOperationPacketPublication(s.db.QueryRowContext(ctx, `
SELECT `+operationPacketPublicationColumns+`
FROM operation_packet_publications
WHERE packet_row_id = (SELECT id FROM operation_packets WHERE packet_id = ?)`, packetID))
}

func (s *Store) GetOperationPacketPublicationIntegrityByMutationKey(ctx context.Context, key MCPMutationKey) (OperationPacketPublicationIntegrity, error) {
	publication, err := scanOperationPacketPublication(s.db.QueryRowContext(ctx, `
SELECT `+operationPacketPublicationColumns+`
FROM operation_packet_publications
WHERE mutation_result_row_id = (
    SELECT id
    FROM mcp_mutation_results
    WHERE surface_contract_id = ?
      AND tool_name = ?
      AND mutation_id = ?
)`,
		key.SurfaceContractID,
		key.ToolName,
		key.MutationID,
	))
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	return s.GetOperationPacketPublicationIntegrity(ctx, publication.PublicationID)
}

func (s *Store) ListOperationPacketPublications(ctx context.Context) ([]OperationPacketPublication, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+operationPacketPublicationColumns+`
FROM operation_packet_publications
ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]OperationPacketPublication, 0)
	for rows.Next() {
		value, err := scanOperationPacketPublication(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) GetOperationPacketPublicationIntegrity(ctx context.Context, publicationID string) (OperationPacketPublicationIntegrity, error) {
	publication, err := s.GetOperationPacketPublication(ctx, publicationID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	packet, err := s.GetOperationPacketByRowID(ctx, publication.PacketRowID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	packetArtifact, err := s.GetOperationPacketArtifact(ctx, packet.ID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	mutationResult, err := scanMCPMutationResult(s.db.QueryRowContext(ctx, `
SELECT `+mcpMutationResultColumns+` FROM mcp_mutation_results WHERE id = ?`, publication.MutationResultRowID))
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	retained, err := listOperationPacketRetainedArtifacts(ctx, s.db, publicationID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	bindings, err := listOperationPacketArtifactBindings(ctx, s.db, publicationID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	dependencies, err := s.ListOperationPacketRetentionDependencies(ctx, packet.ID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	vaults, err := listOperationPacketVaultRelationships(ctx, s.db, publicationID)
	if err != nil {
		return OperationPacketPublicationIntegrity{}, err
	}
	return OperationPacketPublicationIntegrity{
		Publication:        publication,
		Packet:             packet,
		PacketArtifact:     packetArtifact,
		MutationResult:     mutationResult,
		RetainedArtifacts:  retained,
		Bindings:           bindings,
		Dependencies:       dependencies,
		VaultRelationships: vaults,
	}, nil
}

func getOperationPacketPublication(ctx context.Context, query rowQueryer, publicationID string) (OperationPacketPublication, error) {
	return scanOperationPacketPublication(query.QueryRowContext(ctx, `
SELECT `+operationPacketPublicationColumns+`
FROM operation_packet_publications
WHERE publication_id = ?`, publicationID))
}

func listOperationPacketRetainedArtifacts(ctx context.Context, query rowsQueryer, publicationID string) ([]OperationPacketRetainedArtifact, error) {
	rows, err := query.QueryContext(ctx, `
SELECT `+operationPacketRetainedArtifactColumns+`
FROM operation_packet_retained_artifacts
WHERE publication_id = ?
ORDER BY id`, publicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]OperationPacketRetainedArtifact, 0)
	for rows.Next() {
		value, err := scanOperationPacketRetainedArtifact(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func listOperationPacketArtifactBindings(ctx context.Context, query rowsQueryer, publicationID string) ([]OperationPacketArtifactBinding, error) {
	rows, err := query.QueryContext(ctx, `
SELECT `+operationPacketArtifactBindingColumns+`
FROM operation_packet_artifact_bindings
WHERE publication_id = ?
ORDER BY sequence, id`, publicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]OperationPacketArtifactBinding, 0)
	for rows.Next() {
		value, err := scanOperationPacketArtifactBinding(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func listOperationPacketVaultRelationships(ctx context.Context, query rowsQueryer, publicationID string) ([]OperationPacketVaultRelationship, error) {
	rows, err := query.QueryContext(ctx, `
SELECT `+operationPacketVaultRelationshipColumns+`
FROM operation_packet_vault_relationships
WHERE publication_id = ?
ORDER BY dependency_class, dependency_key, id`, publicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]OperationPacketVaultRelationship, 0)
	for rows.Next() {
		value, err := scanOperationPacketVaultRelationship(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanOperationPacketPublication(row rowScanner) (OperationPacketPublication, error) {
	var value OperationPacketPublication
	err := row.Scan(
		&value.ID,
		&value.PublicationID,
		&value.PacketRowID,
		&value.PacketArtifactRowID,
		&value.MutationResultRowID,
		&value.Namespace,
		&value.ManifestSHA256,
		&value.ExpectedRetainedArtifactCount,
		&value.ExpectedBindingCount,
		&value.ExpectedDependencyCount,
		&value.ExpectedVaultRelationshipCount,
		&value.State,
		&value.CreatedAt,
	)
	return value, err
}

func scanOperationPacketRetainedArtifact(row rowScanner) (OperationPacketRetainedArtifact, error) {
	var value OperationPacketRetainedArtifact
	err := row.Scan(
		&value.ID,
		&value.PublicationID,
		&value.ArtifactID,
		&value.Kind,
		&value.RelativePath,
		&value.MediaType,
		&value.SHA256,
		&value.SizeBytes,
		&value.CreatedAt,
	)
	return value, err
}

func scanOperationPacketArtifactBinding(row rowScanner) (OperationPacketArtifactBinding, error) {
	var value OperationPacketArtifactBinding
	err := row.Scan(
		&value.ID,
		&value.PublicationID,
		&value.PacketRowID,
		&value.Sequence,
		&value.DependencyClass,
		&value.DependencyKey,
		&value.PacketArtifactRowID,
		&value.RetainedArtifactRowID,
		&value.CreatedAt,
	)
	return value, err
}

func scanOperationPacketVaultRelationship(row rowScanner) (OperationPacketVaultRelationship, error) {
	var value OperationPacketVaultRelationship
	err := row.Scan(
		&value.ID,
		&value.PublicationID,
		&value.PacketRowID,
		&value.DependencyClass,
		&value.DependencyKey,
		&value.OwnerIdentity,
		&value.RetentionRowID,
		&value.ClosureRowID,
		&value.VaultRowID,
		&value.CommitOID,
		&value.TreeOID,
		&value.CreatedAt,
	)
	return value, err
}
