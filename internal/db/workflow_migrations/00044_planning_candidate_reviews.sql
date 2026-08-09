-- +goose Up
-- Planning candidate reviews are the narrowest durable indication that the
-- read-only auditor review handoff completed for one planning candidate. They
-- record only the completion fact (reviewer identity, completion time, exact
-- candidate basis) and deliberately persist no review outcome, verdict, or
-- content; the review itself remains read-only. Approval remains a separate
-- explicit confirmed owner mutation and is never treated as review completion.
CREATE TABLE planning_candidate_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    review_id TEXT NOT NULL UNIQUE,
    candidate_row_id INTEGER NOT NULL UNIQUE REFERENCES planning_candidates(id) ON DELETE RESTRICT,
    reviewer_identity TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (review_id GLOB 'candidate-review-*' AND trim(review_id) = review_id),
    CHECK (reviewer_identity <> '' AND trim(reviewer_identity) = reviewer_identity)
);

CREATE INDEX idx_planning_candidate_reviews_candidate ON planning_candidate_reviews(candidate_row_id, id);

-- A review record must bind an existing candidate.
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_review_binding_guard
BEFORE INSERT ON planning_candidate_reviews
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM planning_candidates WHERE id = NEW.candidate_row_id
)
BEGIN SELECT RAISE(ABORT, 'candidate review must bind an existing candidate'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_review_update_immutable
BEFORE UPDATE ON planning_candidate_reviews
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'candidate reviews are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER planning_candidate_review_delete_guard
BEFORE DELETE ON planning_candidate_reviews
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'candidate reviews are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS planning_candidate_review_delete_guard;
DROP TRIGGER IF EXISTS planning_candidate_review_update_immutable;
DROP TRIGGER IF EXISTS planning_candidate_review_binding_guard;
DROP INDEX IF EXISTS idx_planning_candidate_reviews_candidate;
DROP TABLE IF EXISTS planning_candidate_reviews;
