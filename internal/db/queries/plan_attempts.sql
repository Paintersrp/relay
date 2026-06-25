-- Intent packet queries

-- name: CreateIntentPacket :one
INSERT INTO intent_packets (
    intent_packet_id,
    project_row_id,
    project_id,
    intent_thread_id,
    root_intent_packet_id,
    parent_intent_packet_id,
    revision_of_plan_attempt_id,
    kind,
    captured_from,
    captured_by,
    source_artifact_path,
    summary,
    literal_user_request,
    constraints_json,
    redaction_status,
    content_hash
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetIntentPacketByID :one
SELECT * FROM intent_packets WHERE intent_packet_id = ?;

-- name: GetIntentPacketByIDAndProject :one
SELECT * FROM intent_packets WHERE intent_packet_id = ? AND project_row_id = ?;

-- name: ListIntentPacketsByThread :many
SELECT * FROM intent_packets
WHERE project_row_id = ? AND intent_thread_id = ?
ORDER BY created_at ASC, id ASC;

-- name: GetRootIntentPacket :one
SELECT * FROM intent_packets WHERE intent_packet_id = ? AND kind = 'original';

-- Plan attempt queries

-- name: CreatePlanAttempt :one
INSERT INTO plan_attempts (
    plan_attempt_id,
    project_row_id,
    project_id,
    intent_thread_id,
    root_intent_packet_id,
    current_intent_packet_id,
    supersedes_plan_attempt_id,
    replacement_plan_attempt_id,
    status,
    review_state,
    drift_review_mode,
    model_tier,
    plan_json_artifact_path,
    plan_json_artifact_sha256,
    raw_plan_json,
    raw_plan_json_hash,
    plan_markdown_artifact_path,
    plan_markdown_artifact_sha256
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlanAttemptByID :one
SELECT * FROM plan_attempts WHERE plan_attempt_id = ?;

-- name: GetPlanAttemptForProject :one
SELECT * FROM plan_attempts WHERE project_row_id = ? AND plan_attempt_id = ?;

-- name: ListPlanAttemptsByThread :many
SELECT * FROM plan_attempts
WHERE project_row_id = ? AND intent_thread_id = ?
ORDER BY created_at ASC, id ASC;

-- name: ListPlanAttemptsByProject :many
SELECT * FROM plan_attempts
WHERE project_row_id = ? AND status != 'superseded'
ORDER BY created_at DESC, id DESC;

-- name: UpdatePlanAttemptReviewState :one
UPDATE plan_attempts
SET review_state = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: UpdatePlanAttemptStatus :one
UPDATE plan_attempts
SET status = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: MarkPlanAttemptSuperseded :one
UPDATE plan_attempts
SET status = 'superseded',
    review_state = 'revision_requested',
    replacement_plan_attempt_id = ?,
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: VoidPlanAttempt :one
UPDATE plan_attempts
SET status = 'voided', updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: ApprovePlanAttempt :one
UPDATE plan_attempts
SET status = 'approved',
    review_state = 'approval_ready',
    accepted_drift_review_id = ?,
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: MarkPlanAttemptSubmitted :one
UPDATE plan_attempts
SET status = 'submitted',
    submitted_plan_row_id = ?,
    submitted_plan_id = ?,
    submitted_at = datetime('now'),
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: UpdatePlanAttemptAcceptedDriftReview :one
UPDATE plan_attempts
SET accepted_drift_review_id = ?, updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- Intent drift review queries

-- name: CreateIntentDriftReview :one
INSERT INTO intent_drift_reviews (
    intent_drift_review_id,
    project_row_id,
    project_id,
    plan_attempt_row_id,
    plan_attempt_id,
    intent_thread_id,
    root_intent_packet_id,
    reviewed_intent_packet_id,
    review_packet_hash,
    review_source,
    submitted_by,
    source_artifact_path,
    overall_alignment,
    confidence,
    findings_json,
    recommended_action,
    approval_gate_status,
    model_metadata_json,
    input_hash,
    output_hash
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetIntentDriftReviewByID :one
SELECT * FROM intent_drift_reviews WHERE intent_drift_review_id = ?;

-- name: GetIntentDriftReviewByIDAndProject :one
SELECT * FROM intent_drift_reviews WHERE intent_drift_review_id = ? AND project_row_id = ?;

-- name: ListIntentDriftReviewsByAttempt :many
SELECT * FROM intent_drift_reviews
WHERE plan_attempt_row_id = ?
ORDER BY created_at DESC, id DESC;

-- name: ListIntentDriftReviewsByThread :many
SELECT * FROM intent_drift_reviews
WHERE project_row_id = ? AND intent_thread_id = ?
ORDER BY created_at DESC, id DESC;