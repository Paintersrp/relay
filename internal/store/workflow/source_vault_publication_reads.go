package workflowstore

import "context"

func (s *Store) GetSourceVaultByRowID(ctx context.Context, rowID int64) (SourceVault, error) {
	return scanSourceVault(s.db.QueryRowContext(ctx, `
SELECT `+sourceVaultColumns+`
FROM source_vaults
WHERE id = ?`, rowID))
}

func (s *Store) GetSourceVaultClosureByRowID(ctx context.Context, rowID int64) (SourceVaultClosure, error) {
	return getSourceVaultClosureByRowID(ctx, s.db, rowID)
}

func (s *Store) GetSourceVaultRetentionByRowID(ctx context.Context, rowID int64) (SourceVaultRetention, error) {
	return scanSourceVaultRetention(s.db.QueryRowContext(ctx, `
SELECT `+sourceVaultRetentionColumns+`
FROM source_vault_retentions
WHERE id = ?`, rowID))
}
