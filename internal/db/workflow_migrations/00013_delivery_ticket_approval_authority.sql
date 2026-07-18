-- +goose Up
ALTER TABLE delivery_ticket_revision_approvals
ADD COLUMN authority_revision_row_id INTEGER REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT;

CREATE INDEX idx_delivery_ticket_approvals_authority
ON delivery_ticket_revision_approvals(authority_revision_row_id, id);

-- +goose Down
DROP INDEX IF EXISTS idx_delivery_ticket_approvals_authority;
ALTER TABLE delivery_ticket_revision_approvals DROP COLUMN authority_revision_row_id;
