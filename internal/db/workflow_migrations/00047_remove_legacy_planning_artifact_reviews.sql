-- +goose Up
-- Releases before 00046 created durable review results. Reviews are now
-- transient, so remove their rows and every supporting schema object after the
-- Brief table rebuild has completed.
DROP TRIGGER IF EXISTS ticket_design_brief_review_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_review_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_review_binding_guard;
DROP TRIGGER IF EXISTS planning_candidate_review_delete_guard;
DROP TRIGGER IF EXISTS planning_candidate_review_update_immutable;
DROP TRIGGER IF EXISTS planning_candidate_review_binding_guard;

DROP INDEX IF EXISTS idx_ticket_design_brief_reviews_brief;
DROP INDEX IF EXISTS idx_planning_candidate_reviews_candidate;

DROP TABLE IF EXISTS ticket_design_brief_reviews;
DROP TABLE IF EXISTS planning_candidate_reviews;

-- +goose Down
SELECT 1;
