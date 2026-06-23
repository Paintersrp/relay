-- name: CreateLocalAudit :one
INSERT INTO local_audits (
  audit_id,
  project_row_id,
  project_id,
  mode,
  title,
  status,
  plan_id,
  pass_id,
  source_snapshot_id,
  context_packet_id,
  manifest_path,
  packet_path,
  input_summary_path,
  blockers_json,
  warnings_json,
  completed_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLocalAuditByAuditID :one
SELECT * FROM local_audits WHERE audit_id = ?;

-- name: ListLocalAuditsByProject :many
SELECT * FROM local_audits
WHERE project_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;

-- name: ListLocalAuditsByProjectAndMode :many
SELECT * FROM local_audits
WHERE project_id = ? AND mode = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;
