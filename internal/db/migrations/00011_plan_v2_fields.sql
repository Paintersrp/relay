-- +goose Up
ALTER TABLE plans ADD COLUMN plan_meta_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plans ADD COLUMN project_context_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plans ADD COLUMN mcp_capability_profile_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plans ADD COLUMN global_context_rules_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plans ADD COLUMN submission_note TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN raw_plan_json TEXT NOT NULL DEFAULT '';

ALTER TABLE plan_passes ADD COLUMN pass_type TEXT NOT NULL DEFAULT '';
ALTER TABLE plan_passes ADD COLUMN context_plan_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plan_passes ADD COLUMN source_snapshot_requirements_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plan_passes ADD COLUMN handoff_readiness_criteria_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE plan_passes ADD COLUMN risk_level TEXT NOT NULL DEFAULT '';
ALTER TABLE plan_passes ADD COLUMN context_budget_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plan_passes ADD COLUMN raw_pass_json TEXT NOT NULL DEFAULT '{}';

-- +goose Down
SELECT 1;
