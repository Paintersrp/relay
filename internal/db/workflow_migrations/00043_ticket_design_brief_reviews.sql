-- +goose Up
-- Ticket Design Brief reviews are the narrowest durable indication that the
-- read-only auditor review handoff completed for one brief. They record only
-- the completion fact (reviewer identity, completion time, exact brief basis)
-- and deliberately persist no review outcome, verdict, or content; the review
-- itself remains read-only. Approval remains a separate explicit confirmed
-- owner mutation and is never treated as review completion.
CREATE TABLE ticket_design_brief_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    review_id TEXT NOT NULL UNIQUE,
    brief_row_id INTEGER NOT NULL UNIQUE REFERENCES ticket_design_briefs(id) ON DELETE RESTRICT,
    reviewer_identity TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (review_id GLOB 'brief-review-*' AND trim(review_id) = review_id),
    CHECK (reviewer_identity <> '' AND trim(reviewer_identity) = reviewer_identity)
);

CREATE INDEX idx_ticket_design_brief_reviews_brief ON ticket_design_brief_reviews(brief_row_id, id);

-- A review record must bind an existing brief row.
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_review_binding_guard
BEFORE INSERT ON ticket_design_brief_reviews
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM ticket_design_briefs WHERE id = NEW.brief_row_id
)
BEGIN SELECT RAISE(ABORT, 'review must bind an existing brief'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_review_update_immutable
BEFORE UPDATE ON ticket_design_brief_reviews
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'brief reviews are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER ticket_design_brief_review_delete_guard
BEFORE DELETE ON ticket_design_brief_reviews
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'brief reviews are retained history'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS ticket_design_brief_review_delete_guard;
DROP TRIGGER IF EXISTS ticket_design_brief_review_update_immutable;
DROP TRIGGER IF EXISTS ticket_design_brief_review_binding_guard;
DROP INDEX IF EXISTS idx_ticket_design_brief_reviews_brief;
DROP TABLE IF EXISTS ticket_design_brief_reviews;
