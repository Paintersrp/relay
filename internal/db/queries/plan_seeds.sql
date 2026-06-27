-- name: CreatePlanSeed :one
INSERT INTO plan_seeds (
  seed_id,
  project_row_id,
  project_id,
  title,
  quick_context,
  constraints_json,
  non_goals_json,
  tags_json,
  priority,
  status,
  source_type,
  source_label,
  source_ref_id,
  plan_attempt_id,
  managed_plan_id,
  planned_at,
  defer_reason,
  reject_reason
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlanSeedBySeedID :one
SELECT * FROM plan_seeds
WHERE project_row_id = ? AND seed_id = ?;

-- name: ListPlanSeedsByProject :many
SELECT * FROM plan_seeds
WHERE project_row_id = ?
ORDER BY updated_at DESC, id DESC
LIMIT ?;

-- name: ListPlanSeedsByProjectAndStatus :many
SELECT * FROM plan_seeds
WHERE project_row_id = ? AND status = ?
ORDER BY updated_at DESC, id DESC
LIMIT ?;

-- name: UpdatePlanSeedCaptureFields :one
UPDATE plan_seeds
SET
  title = ?,
  quick_context = ?,
  constraints_json = ?,
  non_goals_json = ?,
  tags_json = ?,
  priority = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND seed_id = ?
RETURNING *;

-- name: UpdatePlanSeedStatusMetadata :one
UPDATE plan_seeds
SET
  status = ?,
  defer_reason = ?,
  reject_reason = ?,
  planned_at = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND seed_id = ?
RETURNING *;

-- name: MarkPlanSeedPlanned :one
UPDATE plan_seeds
SET
  status = 'planned',
  plan_attempt_id = ?,
  planned_at = datetime('now'),
  defer_reason = '',
  reject_reason = '',
  updated_at = datetime('now')
WHERE project_row_id = ? AND seed_id = ?
RETURNING *;

-- name: LinkPlanSeedManagedPlan :one
UPDATE plan_seeds
SET
  managed_plan_id = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND seed_id = ?
RETURNING *;
