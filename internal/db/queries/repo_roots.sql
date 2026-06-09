-- name: CreateRepoRoot :one
INSERT INTO repo_roots (path, enabled)
VALUES (?, 1)
ON CONFLICT(path) DO UPDATE SET enabled = 1, updated_at = datetime('now')
RETURNING *;

-- name: ListRepoRoots :many
SELECT * FROM repo_roots ORDER BY path ASC;

-- name: ListEnabledRepoRoots :many
SELECT * FROM repo_roots WHERE enabled = 1 ORDER BY path ASC;

-- name: GetRepoRoot :one
SELECT * FROM repo_roots WHERE id = ?;

-- name: SetRepoRootEnabled :one
UPDATE repo_roots SET enabled = ?, updated_at = datetime('now') WHERE id = ? RETURNING *;

-- name: DeleteRepoRoot :exec
DELETE FROM repo_roots WHERE id = ?;

-- name: TouchRepoRootScanned :one
UPDATE repo_roots SET last_scanned_at = datetime('now'), updated_at = datetime('now') WHERE id = ? RETURNING *;
