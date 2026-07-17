package workflowstore

import (
	"context"
	"database/sql"
	"errors"
)

const mcpMutationResultColumns = `id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256, committed_at`

type mcpMutationResultScanner interface {
	Scan(...any) error
}

func (s *Store) GetMCPMutationResult(ctx context.Context, key MCPMutationKey) (MCPMutationResult, error) {
	return getMCPMutationResult(ctx, s.db, key)
}

func (s *Store) GetMCPMutationResultOptional(ctx context.Context, key MCPMutationKey) (MCPMutationResult, bool, error) {
	value, err := getMCPMutationResult(ctx, s.db, key)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPMutationResult{}, false, nil
	}
	return value, err == nil, err
}

func (tx *Tx) GetMCPMutationResult(ctx context.Context, key MCPMutationKey) (MCPMutationResult, error) {
	return getMCPMutationResult(ctx, tx.tx, key)
}

func (tx *Tx) GetMCPMutationResultOptional(ctx context.Context, key MCPMutationKey) (MCPMutationResult, bool, error) {
	value, err := getMCPMutationResult(ctx, tx.tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPMutationResult{}, false, nil
	}
	return value, err == nil, err
}

func (tx *Tx) CreateMCPMutationResult(ctx context.Context, params CreateMCPMutationResultParams) (MCPMutationResult, error) {
	return scanMCPMutationResult(tx.tx.QueryRowContext(ctx, `
INSERT INTO mcp_mutation_results (
    surface_contract_id,
    tool_name,
    mutation_id,
    surface_manifest_sha256,
    semantic_identity_version,
    semantic_request_sha256,
    result_kind,
    result_identity_json,
    result_sha256
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING `+mcpMutationResultColumns,
		params.SurfaceContractID,
		params.ToolName,
		params.MutationID,
		params.SurfaceManifestSHA256,
		params.SemanticIdentityVersion,
		params.SemanticRequestSHA256,
		params.ResultKind,
		params.ResultIdentityJSON,
		params.ResultSHA256,
	))
}

func getMCPMutationResult(ctx context.Context, queryer rowQueryer, key MCPMutationKey) (MCPMutationResult, error) {
	return scanMCPMutationResult(queryer.QueryRowContext(ctx, `
SELECT `+mcpMutationResultColumns+`
FROM mcp_mutation_results
WHERE surface_contract_id = ? AND tool_name = ? AND mutation_id = ?`,
		key.SurfaceContractID,
		key.ToolName,
		key.MutationID,
	))
}

func scanMCPMutationResult(scanner mcpMutationResultScanner) (MCPMutationResult, error) {
	var value MCPMutationResult
	err := scanner.Scan(
		&value.ID,
		&value.SurfaceContractID,
		&value.ToolName,
		&value.MutationID,
		&value.SurfaceManifestSHA256,
		&value.SemanticIdentityVersion,
		&value.SemanticRequestSHA256,
		&value.ResultKind,
		&value.ResultIdentityJSON,
		&value.ResultSHA256,
		&value.CommittedAt,
	)
	return value, err
}
