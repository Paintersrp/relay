-- name: CreateRefactorDiscoveryTask :one
INSERT INTO refactor_discovery_tasks (
  task_id,
  project_row_id,
  project_id,
  title,
  prompt,
  target_scope_json,
  priority,
  status,
  tags_json,
  created_from,
  metadata_json,
  closed_reason,
  completed_at,
  closed_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRefactorDiscoveryTaskByTaskID :one
SELECT * FROM refactor_discovery_tasks
WHERE project_row_id = ? AND task_id = ?;

-- name: GetRefactorDiscoveryTaskByRowID :one
SELECT * FROM refactor_discovery_tasks
WHERE project_row_id = ? AND id = ?;

-- name: ListRefactorDiscoveryTasksByProject :many
SELECT * FROM refactor_discovery_tasks
WHERE project_row_id = ?
ORDER BY updated_at DESC, id DESC
LIMIT ?;

-- name: ListRefactorDiscoveryTasksByProjectAndStatus :many
SELECT * FROM refactor_discovery_tasks
WHERE project_row_id = ? AND status = ?
ORDER BY updated_at DESC, id DESC
LIMIT ?;

-- name: UpdateRefactorDiscoveryTask :one
UPDATE refactor_discovery_tasks
SET
  title = ?,
  prompt = ?,
  target_scope_json = ?,
  priority = ?,
  tags_json = ?,
  metadata_json = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND task_id = ?
RETURNING *;

-- name: UpdateRefactorDiscoveryTaskStatus :one
UPDATE refactor_discovery_tasks
SET
  status = ?,
  closed_reason = ?,
  completed_at = ?,
  closed_at = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND task_id = ?
RETURNING *;

-- name: CreateRefactorCandidate :one
INSERT INTO refactor_candidates (
  candidate_id,
  project_row_id,
  project_id,
  title,
  problem_summary,
  current_behavior,
  desired_behavior,
  rationale,
  proposed_pass_name,
  proposed_pass_goal,
  proposed_pass_scope_json,
  proposed_non_goals_json,
  target_files_json,
  validation_commands_json,
  audit_focus_json,
  constraints_json,
  risk_level,
  status,
  dependency_notes,
  metadata_json
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRefactorCandidateByCandidateID :one
SELECT * FROM refactor_candidates
WHERE project_row_id = ? AND candidate_id = ?;

-- name: GetRefactorCandidateByRowID :one
SELECT * FROM refactor_candidates
WHERE project_row_id = ? AND id = ?;

-- name: ListRefactorCandidatesByProject :many
SELECT * FROM refactor_candidates
WHERE project_row_id = ?
ORDER BY updated_at DESC, id DESC
LIMIT ?;

-- name: ListRefactorCandidatesByProjectAndStatus :many
SELECT * FROM refactor_candidates
WHERE project_row_id = ? AND status = ?
ORDER BY updated_at DESC, id DESC
LIMIT ?;

-- name: SearchRefactorCandidatesByProject :many
SELECT * FROM refactor_candidates
WHERE project_row_id = sqlc.arg(project_row_id)
  AND (
    title LIKE sqlc.arg(query)
    OR problem_summary LIKE sqlc.arg(query)
    OR desired_behavior LIKE sqlc.arg(query)
    OR proposed_pass_goal LIKE sqlc.arg(query)
  )
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg(limit);

-- name: UpdateRefactorCandidate :one
UPDATE refactor_candidates
SET
  title = ?,
  problem_summary = ?,
  current_behavior = ?,
  desired_behavior = ?,
  rationale = ?,
  proposed_pass_name = ?,
  proposed_pass_goal = ?,
  proposed_pass_scope_json = ?,
  proposed_non_goals_json = ?,
  target_files_json = ?,
  validation_commands_json = ?,
  audit_focus_json = ?,
  constraints_json = ?,
  dependency_notes = ?,
  metadata_json = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND candidate_id = ?
RETURNING *;

-- name: UpdateRefactorCandidateStatusMetadata :one
UPDATE refactor_candidates
SET
  status = ?,
  defer_reason = ?,
  deferred_until = ?,
  rejected_reason = ?,
  superseded_by_candidate_id = ?,
  superseded_reason = ?,
  scheduled_at = ?,
  completed_at = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND candidate_id = ?
RETURNING *;

-- name: CreateRefactorCandidateDiscoveryLink :one
INSERT INTO refactor_candidate_discovery_links (
  link_id,
  project_row_id,
  project_id,
  candidate_row_id,
  discovery_task_row_id,
  note
)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRefactorCandidateDiscoveryLinks :many
SELECT * FROM refactor_candidate_discovery_links
WHERE project_row_id = ? AND candidate_row_id = ?
ORDER BY created_at DESC, id DESC;

-- name: ListRefactorDiscoveryTaskCandidateLinks :many
SELECT * FROM refactor_candidate_discovery_links
WHERE project_row_id = ? AND discovery_task_row_id = ?
ORDER BY created_at DESC, id DESC;

-- name: CreateRefactorCandidateDependency :one
INSERT INTO refactor_candidate_dependencies (
  dependency_id,
  project_row_id,
  project_id,
  candidate_row_id,
  depends_on_candidate_row_id,
  dependency_type,
  note
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRefactorCandidateDependencies :many
SELECT * FROM refactor_candidate_dependencies
WHERE project_row_id = ? AND candidate_row_id = ?
ORDER BY created_at DESC, id DESC;

-- name: DeleteRefactorCandidateDependencies :exec
DELETE FROM refactor_candidate_dependencies
WHERE project_row_id = ? AND candidate_row_id = ?;

-- name: CreateRefactorCandidateScheduleRef :one
INSERT INTO refactor_candidate_schedule_refs (
  schedule_ref_id,
  project_row_id,
  project_id,
  candidate_row_id,
  schedule_kind,
  status,
  plan_row_id,
  plan_pass_row_id,
  run_row_id,
  plan_id,
  pass_id,
  run_id,
  note
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRefactorCandidateScheduleRefs :many
SELECT * FROM refactor_candidate_schedule_refs
WHERE project_row_id = ? AND candidate_row_id = ?
ORDER BY created_at DESC, id DESC;

-- name: GetActiveRefactorCandidateScheduleRef :one
SELECT * FROM refactor_candidate_schedule_refs
WHERE project_row_id = ? AND candidate_row_id = ? AND status = 'scheduled'
ORDER BY updated_at DESC, id DESC
LIMIT 1;

-- name: UpdateRefactorCandidateScheduleRefStatus :one
UPDATE refactor_candidate_schedule_refs
SET
  status = ?,
  note = ?,
  updated_at = datetime('now')
WHERE project_row_id = ? AND schedule_ref_id = ?
RETURNING *;

-- name: CreateRefactorCandidateStatusEvent :one
INSERT INTO refactor_candidate_status_events (
  event_id,
  project_row_id,
  project_id,
  candidate_row_id,
  event_type,
  from_status,
  to_status,
  reason,
  detail_json
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRefactorCandidateStatusEvents :many
SELECT * FROM refactor_candidate_status_events
WHERE project_row_id = ? AND candidate_row_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?;
