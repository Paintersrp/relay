package workflowstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type SourcePathSelector struct {
	ID                     int64
	SelectorID             string
	PacketRowID            int64
	PacketID               string
	SurfaceContractID      string
	OperationID            string
	ProjectID              string
	RepositoryKey          string
	PublicationID          string
	VaultRelationshipRowID int64
	CommitOID              string
	TreeOID                string
	PathID                 string
	PathByteLength         int64
	PathBytes              []byte
	CreatedAt              string
}

type CreateOrGetSourcePathSelectorParams struct {
	SelectorID             string
	PacketRowID            int64
	PacketID               string
	SurfaceContractID      string
	OperationID            string
	ProjectID              string
	RepositoryKey          string
	PublicationID          string
	VaultRelationshipRowID int64
	CommitOID              string
	TreeOID                string
	PathID                 string
	PathBytes              []byte
}

const sourcePathSelectorColumns = `
    id, selector_id, packet_row_id, packet_id, surface_contract_id, operation_id,
    project_id, repository_key, publication_id, vault_relationship_row_id,
    commit_oid, tree_oid, path_id, path_byte_length, path_bytes, created_at`

func (s *Store) CreateOrGetSourcePathSelector(ctx context.Context, params CreateOrGetSourcePathSelectorParams) (SourcePathSelector, error) {
	if s == nil || !validSourcePathSelectorParams(params) {
		return SourcePathSelector{}, fmt.Errorf("source path selector parameters are invalid")
	}
	value, err := scanSourcePathSelector(s.db.QueryRowContext(ctx, `
INSERT OR IGNORE INTO source_path_selectors (
    selector_id, packet_row_id, packet_id, surface_contract_id, operation_id,
    project_id, repository_key, publication_id, vault_relationship_row_id,
    commit_oid, tree_oid, path_id, path_byte_length, path_bytes
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+sourcePathSelectorColumns,
		params.SelectorID, params.PacketRowID, params.PacketID, params.SurfaceContractID,
		params.OperationID, params.ProjectID, params.RepositoryKey, params.PublicationID,
		params.VaultRelationshipRowID, params.CommitOID, params.TreeOID, params.PathID,
		len(params.PathBytes), append([]byte(nil), params.PathBytes...),
	))
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourcePathSelector{}, err
	}
	existing, err := s.GetSourcePathSelector(ctx, params.SelectorID)
	if err != nil {
		return SourcePathSelector{}, err
	}
	if !sourcePathSelectorMatches(existing, params) {
		return SourcePathSelector{}, fmt.Errorf("source path selector identity conflicts with existing authority")
	}
	return existing, nil
}

func (s *Store) GetSourcePathSelector(ctx context.Context, selectorID string) (SourcePathSelector, error) {
	if s == nil || !validSelectorID(selectorID) {
		return SourcePathSelector{}, sql.ErrNoRows
	}
	return scanSourcePathSelector(s.db.QueryRowContext(ctx, `SELECT `+sourcePathSelectorColumns+` FROM source_path_selectors WHERE selector_id = ?`, selectorID))
}

func scanSourcePathSelector(row rowScanner) (SourcePathSelector, error) {
	var value SourcePathSelector
	err := row.Scan(&value.ID, &value.SelectorID, &value.PacketRowID, &value.PacketID, &value.SurfaceContractID, &value.OperationID, &value.ProjectID, &value.RepositoryKey, &value.PublicationID, &value.VaultRelationshipRowID, &value.CommitOID, &value.TreeOID, &value.PathID, &value.PathByteLength, &value.PathBytes, &value.CreatedAt)
	if err != nil {
		return SourcePathSelector{}, err
	}
	value.PathBytes = append([]byte(nil), value.PathBytes...)
	if value.PathByteLength != int64(len(value.PathBytes)) {
		return SourcePathSelector{}, fmt.Errorf("source path selector byte length is invalid")
	}
	return value, nil
}

func validSourcePathSelectorParams(value CreateOrGetSourcePathSelectorParams) bool {
	return validSelectorID(value.SelectorID) && value.PacketRowID > 0 && validBoundedString(value.PacketID) && validBoundedString(value.SurfaceContractID) && validBoundedString(value.OperationID) && validBoundedString(value.ProjectID) && validBoundedString(value.RepositoryKey) && validBoundedString(value.PublicationID) && value.VaultRelationshipRowID > 0 && validLowerHex(value.CommitOID, 40) && validLowerHex(value.TreeOID, 40) && validLowerHex(value.PathID, 64) && len(value.PathBytes) > 0
}

func sourcePathSelectorMatches(value SourcePathSelector, params CreateOrGetSourcePathSelectorParams) bool {
	return value.SelectorID == params.SelectorID && value.PacketRowID == params.PacketRowID && value.PacketID == params.PacketID && value.SurfaceContractID == params.SurfaceContractID && value.OperationID == params.OperationID && value.ProjectID == params.ProjectID && value.RepositoryKey == params.RepositoryKey && value.PublicationID == params.PublicationID && value.VaultRelationshipRowID == params.VaultRelationshipRowID && value.CommitOID == params.CommitOID && value.TreeOID == params.TreeOID && value.PathID == params.PathID && value.PathByteLength == int64(len(params.PathBytes)) && string(value.PathBytes) == string(params.PathBytes)
}

func validSelectorID(value string) bool {
	return strings.HasPrefix(value, "spath-") && len(value) == 70 && validLowerHex(value[6:], 64)
}

func validBoundedString(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= 512
}

func validLowerHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
