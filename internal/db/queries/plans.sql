-- name: CreatePlan :one
INSERT INTO plans (
  plan_id,
  schema_version,
  title,
  goal,
  repo_target,
  branch_context,
  status,
  source_intent_summary,
  source_artifact_path
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlan :one
SELECT * FROM plans WHERE id = ?;

-- name: GetPlanByPlanID :one
SELECT * FROM plans WHERE plan_id = ?;

-- name: ListPlans :many
SELECT * FROM plans ORDER BY updated_at DESC, id DESC LIMIT ?;

-- name: ListPlansByStatus :many
SELECT * FROM plans WHERE status = ? ORDER BY updated_at DESC, id DESC LIMIT ?;

-- name: UpdatePlanStatus :one
UPDATE plans
SET status = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: CreatePlanPass :one
INSERT INTO plan_passes (
  plan_row_id,
  pass_id,
  sequence,
  name,
  goal,
  intended_execution_scope_json,
  non_goals_json,
  dependencies_json,
  status
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlanPass :one
SELECT * FROM plan_passes WHERE id = ?;

-- name: GetPlanPassByPassID :one
SELECT * FROM plan_passes WHERE plan_row_id = ? AND pass_id = ?;

-- name: ListPlanPassesByPlan :many
SELECT * FROM plan_passes WHERE plan_row_id = ? ORDER BY sequence ASC;

-- name: ListPlanPassesByStatus :many
SELECT * FROM plan_passes WHERE plan_row_id = ? AND status = ? ORDER BY sequence ASC;

-- name: UpdatePlanPassStatus :one
UPDATE plan_passes
SET status = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;
