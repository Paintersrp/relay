-- name: CreateRunSubmissionProvenance :one
INSERT INTO run_submission_provenance (
  run_id,
  planner_handoff_sha256,
  planner_handoff_bytes,
  source,
  client_trace_id,
  source_artifact_path,
  repo_target,
  branch_context,
  plan_id,
  pass_id,
  plan_row_id,
  plan_pass_row_id,
  managed_plan_pass,
  managed_plan_pass_name,
  context_packet_id,
  source_snapshot_id,
  handoff_metadata_json,
  submission_args_json
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRunSubmissionProvenanceByRun :one
SELECT * FROM run_submission_provenance WHERE run_id = ?;

-- name: ListRunSubmissionProvenanceByPlanPass :many
SELECT * FROM run_submission_provenance
WHERE plan_id = ? AND pass_id = ?
ORDER BY created_at DESC, id DESC;

-- name: ListRunSubmissionProvenanceByPlan :many
SELECT * FROM run_submission_provenance
WHERE plan_id = ?
ORDER BY created_at DESC, id DESC;
