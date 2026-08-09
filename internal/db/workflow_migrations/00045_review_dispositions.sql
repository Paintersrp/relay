-- +goose Up
-- A review records only its bounded disposition over its already immutable
-- candidate or brief basis. Historical completion-only reviews conservatively
-- require a fresh review before they can authorize approval.
ALTER TABLE planning_candidate_reviews
    ADD COLUMN disposition TEXT NOT NULL DEFAULT 'needs_revision'
    CHECK (disposition IN ('ready_for_approval', 'needs_revision'));

ALTER TABLE ticket_design_brief_reviews
    ADD COLUMN disposition TEXT NOT NULL DEFAULT 'needs_revision'
    CHECK (disposition IN ('ready_for_approval', 'needs_revision'));

-- +goose Down
ALTER TABLE ticket_design_brief_reviews DROP COLUMN disposition;
ALTER TABLE planning_candidate_reviews DROP COLUMN disposition;
