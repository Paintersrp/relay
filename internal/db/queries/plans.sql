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
  source_artifact_path,
  plan_meta_json,
  project_context_json,
  mcp_capability_profile_json,
  global_context_rules_json,
  submission_note,
  raw_plan_json,
  project_row_id,
  project_id,
  submitted_plan_attempt_id,
  intent_thread_id,
  root_intent_packet_id,
  submitted_intent_packet_id,
  accepted_drift_review_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
  status,
  pass_type,
  context_plan_json,
  source_snapshot_requirements_json,
  handoff_readiness_criteria_json,
  risk_level,
  context_budget_json,
  raw_pass_json
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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

-- name: GetPlanByProjectAndPlanID :one
SELECT * FROM plans WHERE project_row_id = ? AND plan_id = ?;

-- name: ListPlansByProject :many
SELECT * FROM plans WHERE project_row_id = ? ORDER BY updated_at DESC, id DESC LIMIT ?;

-- name: ListPlansByProjectAndStatus :many
SELECT * FROM plans WHERE project_row_id = ? AND status = ? ORDER BY updated_at DESC, id DESC LIMIT ?;
