-- +goose Up
-- Merge conflict occurrence is factual runtime evidence. It is deliberately
-- separate from authored Ticket authority and from Git commit topology.
ALTER TABLE program_integration_merge_results
ADD COLUMN conflict_resolution TEXT NOT NULL DEFAULT 'clean'
CHECK (conflict_resolution IN ('clean', 'mechanically_resolved', 'material_conflict'));

-- +goose Down
-- SQLite cannot drop a column without rebuilding this retained-history table.
