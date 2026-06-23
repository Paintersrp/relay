-- name: CreateContextPacket :one
INSERT INTO context_packets (
  context_packet_id,
  project_row_id,
  project_id,
  plan_id,
  pass_id,
  task_slug,
  source_snapshot_row_id,
  source_snapshot_id,
  status,
  packet_json_path,
  packet_markdown_path,
  coverage_report_path,
  source_count,
  covered_seed_count,
  blocked_seed_count,
  missing_seed_count,
  truncated,
  blockers_json,
  summary_json,
  completed_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetContextPacketByID :one
SELECT * FROM context_packets WHERE context_packet_id = ?;

-- name: ListContextPacketsByProject :many
SELECT * FROM context_packets WHERE project_id = ? ORDER BY created_at DESC, id DESC;

-- name: CreateContextPacketSource :one
INSERT INTO context_packet_sources (
  context_packet_row_id,
  source_id,
  source_type,
  project_id,
  repo_id,
  source_snapshot_id,
  path,
  line_start,
  line_end,
  content_hash,
  snippet_hash,
  redaction_status,
  truncated,
  generated_at,
  reason
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListContextPacketSources :many
SELECT * FROM context_packet_sources WHERE context_packet_row_id = ? ORDER BY id ASC;

-- name: GetLatestContextPacketForPass :one
SELECT * FROM context_packets
WHERE project_id = ? AND plan_id = ? AND pass_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 1;
