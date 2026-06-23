-- name: CreateProjectContextRecord :one
INSERT INTO project_context_records (
  context_record_id,
  project_row_id,
  project_id,
  kind,
  title,
  body,
  body_hash,
  status,
  importance,
  tags_json,
  source,
  created_by,
  dedupe_reason,
  redaction_status,
  supersedes_record_id,
  superseded_by_record_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetProjectContextRecordByRecordID :one
SELECT * FROM project_context_records
WHERE project_row_id = ? AND context_record_id = ?;

-- name: ListProjectContextRecords :many
SELECT * FROM project_context_records
WHERE project_row_id = ?
ORDER BY
  CASE importance
    WHEN 'critical' THEN 4
    WHEN 'high' THEN 3
    WHEN 'normal' THEN 2
    ELSE 1
  END DESC,
  updated_at DESC,
  id DESC
LIMIT ?;

-- name: SearchProjectContextRecords :many
SELECT * FROM project_context_records
WHERE project_row_id = ?
ORDER BY
  CASE importance
    WHEN 'critical' THEN 4
    WHEN 'high' THEN 3
    WHEN 'normal' THEN 2
    ELSE 1
  END DESC,
  updated_at DESC,
  id DESC
LIMIT ?;

-- name: MarkProjectContextRecordSuperseded :one
UPDATE project_context_records
SET
  status = 'superseded',
  superseded_by_record_id = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND context_record_id = ?
RETURNING *;

-- name: GetActiveProjectContextRecordByBodyHash :one
SELECT * FROM project_context_records
WHERE project_row_id = ? AND body_hash = ? AND status = 'active';
