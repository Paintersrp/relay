package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const sourceVaultColumns = `
    id, vault_id, repo_target, relative_path, created_at, updated_at`

var (
	ErrSourceVaultStateConflict     = errors.New("source vault state conflict")
	ErrSourceVaultRetentionConflict = errors.New("source vault retention conflict")
	ErrSourceVaultCleanupBlocked    = errors.New("source vault cleanup blocked")
)

const sourceVaultClosureColumns = `
    id, closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name,
    state, failure_reason, import_started_at, verified_at, released_at,
    created_at, updated_at`

const sourceVaultRetentionColumns = `
    id, retention_id, closure_row_id, owner_class, owner_identity, state,
    created_at, updated_at, released_at`

func (tx *Tx) GetSourceVaultByVaultID(ctx context.Context, vaultID string) (SourceVault, error) {
	return getSourceVaultByVaultID(ctx, tx.tx, vaultID)
}

func (tx *Tx) GetSourceVaultByRepositoryTarget(ctx context.Context, repoTarget string) (SourceVault, error) {
	return getSourceVaultByRepositoryTarget(ctx, tx.tx, repoTarget)
}

func (tx *Tx) GetSourceVaultClosureByClosureID(ctx context.Context, closureID string) (SourceVaultClosure, error) {
	return getSourceVaultClosureByClosureID(ctx, tx.tx, closureID)
}

func (tx *Tx) GetSourceVaultClosureByRowID(ctx context.Context, rowID int64) (SourceVaultClosure, error) {
	return getSourceVaultClosureByRowID(ctx, tx.tx, rowID)
}

func (tx *Tx) GetCurrentSourceVaultClosureByIdentity(
	ctx context.Context,
	vaultRowID int64,
	commitOID string,
	treeOID string,
) (SourceVaultClosure, error) {
	return getCurrentSourceVaultClosureByIdentity(ctx, tx.tx, vaultRowID, commitOID, treeOID)
}

func (tx *Tx) ListSourceVaultClosuresByIdentity(
	ctx context.Context,
	vaultRowID int64,
	commitOID string,
	treeOID string,
) ([]SourceVaultClosure, error) {
	return listSourceVaultClosuresByIdentity(ctx, tx.tx, vaultRowID, commitOID, treeOID)
}

func (tx *Tx) GetSourceVaultRetentionByRetentionID(ctx context.Context, retentionID string) (SourceVaultRetention, error) {
	return getSourceVaultRetentionByRetentionID(ctx, tx.tx, retentionID)
}

func (tx *Tx) GetSourceVaultRetentionByOwnerEdge(
	ctx context.Context,
	closureRowID int64,
	ownerClass string,
	ownerIdentity string,
) (SourceVaultRetention, error) {
	return getSourceVaultRetentionByOwnerEdge(ctx, tx.tx, closureRowID, ownerClass, ownerIdentity)
}

func (tx *Tx) GetActiveSourceVaultRetentionByOwner(
	ctx context.Context,
	ownerClass string,
	ownerIdentity string,
) (SourceVaultRetention, error) {
	return getActiveSourceVaultRetentionByOwner(ctx, tx.tx, ownerClass, ownerIdentity)
}

func (tx *Tx) ListSourceVaultRetentions(ctx context.Context, closureRowID int64) ([]SourceVaultRetention, error) {
	return listSourceVaultRetentions(ctx, tx.tx, closureRowID)
}

func (tx *Tx) CountActiveSourceVaultRetentions(ctx context.Context, closureRowID int64) (int64, error) {
	return countActiveSourceVaultRetentions(ctx, tx.tx, closureRowID)
}

func getSourceVaultByVaultID(ctx context.Context, query rowQueryer, vaultID string) (SourceVault, error) {
	return scanSourceVault(query.QueryRowContext(ctx, `
SELECT `+sourceVaultColumns+`
FROM source_vaults
WHERE vault_id = ?`, vaultID))
}

func getSourceVaultByRepositoryTarget(ctx context.Context, query rowQueryer, repoTarget string) (SourceVault, error) {
	return scanSourceVault(query.QueryRowContext(ctx, `
SELECT `+sourceVaultColumns+`
FROM source_vaults
WHERE repo_target = ? COLLATE NOCASE`, repoTarget))
}

func getSourceVaultClosureByClosureID(ctx context.Context, query rowQueryer, closureID string) (SourceVaultClosure, error) {
	return scanSourceVaultClosure(query.QueryRowContext(ctx, `
SELECT `+sourceVaultClosureColumns+`
FROM source_vault_closures
WHERE closure_id = ?`, closureID))
}

func getSourceVaultClosureByRowID(ctx context.Context, query rowQueryer, rowID int64) (SourceVaultClosure, error) {
	return scanSourceVaultClosure(query.QueryRowContext(ctx, `
SELECT `+sourceVaultClosureColumns+`
FROM source_vault_closures
WHERE id = ?`, rowID))
}

func getCurrentSourceVaultClosureByIdentity(
	ctx context.Context,
	query rowQueryer,
	vaultRowID int64,
	commitOID string,
	treeOID string,
) (SourceVaultClosure, error) {
	return scanSourceVaultClosure(query.QueryRowContext(ctx, `
SELECT `+sourceVaultClosureColumns+`
FROM source_vault_closures
WHERE vault_row_id = ? AND commit_oid = ? AND tree_oid = ? AND state <> 'released'
LIMIT 1`, vaultRowID, commitOID, treeOID))
}

func listSourceVaultClosuresByIdentity(
	ctx context.Context,
	query rowsQueryer,
	vaultRowID int64,
	commitOID string,
	treeOID string,
) ([]SourceVaultClosure, error) {
	rows, err := query.QueryContext(ctx, `
SELECT `+sourceVaultClosureColumns+`
FROM source_vault_closures
WHERE vault_row_id = ? AND commit_oid = ? AND tree_oid = ?
ORDER BY generation, id`, vaultRowID, commitOID, treeOID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSourceVaultClosures(rows)
}

func getSourceVaultRetentionByRetentionID(ctx context.Context, query rowQueryer, retentionID string) (SourceVaultRetention, error) {
	return scanSourceVaultRetention(query.QueryRowContext(ctx, `
SELECT `+sourceVaultRetentionColumns+`
FROM source_vault_retentions
WHERE retention_id = ?`, retentionID))
}

func getSourceVaultRetentionByOwnerEdge(
	ctx context.Context,
	query rowQueryer,
	closureRowID int64,
	ownerClass string,
	ownerIdentity string,
) (SourceVaultRetention, error) {
	return scanSourceVaultRetention(query.QueryRowContext(ctx, `
SELECT `+sourceVaultRetentionColumns+`
FROM source_vault_retentions
WHERE closure_row_id = ? AND owner_class = ? AND owner_identity = ?`,
		closureRowID,
		ownerClass,
		ownerIdentity,
	))
}

func getActiveSourceVaultRetentionByOwner(
	ctx context.Context,
	query rowQueryer,
	ownerClass string,
	ownerIdentity string,
) (SourceVaultRetention, error) {
	return scanSourceVaultRetention(query.QueryRowContext(ctx, `
SELECT `+sourceVaultRetentionColumns+`
FROM source_vault_retentions
WHERE owner_class = ? AND owner_identity = ? AND state = 'active'
LIMIT 1`,
		ownerClass,
		ownerIdentity,
	))
}

func listSourceVaultRetentions(ctx context.Context, query rowsQueryer, closureRowID int64) ([]SourceVaultRetention, error) {
	rows, err := query.QueryContext(ctx, `
SELECT `+sourceVaultRetentionColumns+`
FROM source_vault_retentions
WHERE closure_row_id = ?
ORDER BY owner_class, owner_identity, id`, closureRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]SourceVaultRetention, 0)
	for rows.Next() {
		value, err := scanSourceVaultRetention(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func countActiveSourceVaultRetentions(ctx context.Context, query rowQueryer, closureRowID int64) (int64, error) {
	var count int64
	err := query.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM source_vault_retentions
WHERE closure_row_id = ? AND state = 'active'`, closureRowID).Scan(&count)
	return count, err
}

func scanSourceVaultClosures(rows *sql.Rows) ([]SourceVaultClosure, error) {
	values := make([]SourceVaultClosure, 0)
	for rows.Next() {
		value, err := scanSourceVaultClosure(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func scanSourceVault(row rowScanner) (SourceVault, error) {
	var value SourceVault
	err := row.Scan(
		&value.ID,
		&value.VaultID,
		&value.RepoTarget,
		&value.RelativePath,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func scanSourceVaultClosure(row rowScanner) (SourceVaultClosure, error) {
	var value SourceVaultClosure
	err := row.Scan(
		&value.ID,
		&value.ClosureID,
		&value.VaultRowID,
		&value.CommitOID,
		&value.TreeOID,
		&value.Generation,
		&value.RefName,
		&value.State,
		&value.FailureReason,
		&value.ImportStartedAt,
		&value.VerifiedAt,
		&value.ReleasedAt,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	return value, err
}

func scanSourceVaultRetention(row rowScanner) (SourceVaultRetention, error) {
	var value SourceVaultRetention
	err := row.Scan(
		&value.ID,
		&value.RetentionID,
		&value.ClosureRowID,
		&value.OwnerClass,
		&value.OwnerIdentity,
		&value.State,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.ReleasedAt,
	)
	return value, err
}

func sourceVaultNoRows(err error, operation string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrSourceVaultStateConflict, operation)
	}
	return err
}
