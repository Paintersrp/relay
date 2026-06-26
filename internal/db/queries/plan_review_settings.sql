-- name: GetPlanReviewSettingsByProject :one
SELECT * FROM plan_review_settings WHERE project_row_id = ?;

-- name: CreatePlanReviewSettings :one
INSERT INTO plan_review_settings (
  project_row_id,
  project_id,
  drift_review_mode,
  model_tier
)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: UpsertPlanReviewSettings :one
INSERT INTO plan_review_settings (
  project_row_id,
  project_id,
  drift_review_mode,
  model_tier
)
VALUES (?, ?, ?, ?)
ON CONFLICT(project_row_id) DO UPDATE SET
  project_id = excluded.project_id,
  drift_review_mode = excluded.drift_review_mode,
  model_tier = excluded.model_tier,
  updated_at = datetime('now')
RETURNING *;
