-- +goose Up
-- Enforce cardinality-one for ordinary delivery ticket selections. New inserts
-- into delivery_ticket_selection_members must fail when the target selection
-- already has one member. Historical multi-member selections are preserved.
-- +goose StatementBegin
CREATE TRIGGER trg_single_selection_member
BEFORE INSERT ON delivery_ticket_selection_members
FOR EACH ROW WHEN EXISTS (
    SELECT 1 FROM delivery_ticket_selection_members
    WHERE selection_row_id = NEW.selection_row_id
)
BEGIN
    SELECT RAISE(ABORT, 'ordinary selection may only have one member');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_single_selection_member;
