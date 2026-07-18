-- +goose Up
-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_dependency_insert_guard
BEFORE INSERT ON operation_packet_retention_dependencies
FOR EACH ROW WHEN EXISTS (
    SELECT 1
    FROM operation_packets AS packet
    JOIN operation_packet_publications AS publication
      ON publication.publication_id = packet.coordinated_publication_id
     AND publication.packet_row_id = packet.id
    WHERE packet.id = NEW.packet_row_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication dependencies are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_dependency_update_guard
BEFORE UPDATE ON operation_packet_retention_dependencies
FOR EACH ROW WHEN EXISTS (
    SELECT 1
    FROM operation_packets AS packet
    JOIN operation_packet_publications AS publication
      ON publication.publication_id = packet.coordinated_publication_id
     AND publication.packet_row_id = packet.id
    WHERE packet.id = OLD.packet_row_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication dependencies are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_dependency_delete_guard
BEFORE DELETE ON operation_packet_retention_dependencies
FOR EACH ROW WHEN EXISTS (
    SELECT 1
    FROM operation_packets AS packet
    JOIN operation_packet_publications AS publication
      ON publication.publication_id = packet.coordinated_publication_id
     AND publication.packet_row_id = packet.id
    WHERE packet.id = OLD.packet_row_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication dependencies are retained authority');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_mutation_result_authority_guard
BEFORE INSERT ON operation_packet_publications
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM operation_packets AS packet
    JOIN mcp_mutation_results AS mutation_result
      ON mutation_result.id = NEW.mutation_result_row_id
    WHERE packet.id = NEW.packet_row_id
      AND packet.surface_contract_id = mutation_result.surface_contract_id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication mutation result authority does not match');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_mutation_result_update_guard
BEFORE UPDATE ON mcp_mutation_results
FOR EACH ROW WHEN EXISTS (
    SELECT 1
    FROM operation_packet_publications
    WHERE mutation_result_row_id = OLD.id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication mutation result is immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_mutation_result_delete_guard
BEFORE DELETE ON mcp_mutation_results
FOR EACH ROW WHEN EXISTS (
    SELECT 1
    FROM operation_packet_publications
    WHERE mutation_result_row_id = OLD.id
) BEGIN
    SELECT RAISE(ABORT, 'operation packet publication mutation result is retained authority');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS operation_packet_publication_mutation_result_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_mutation_result_update_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_mutation_result_authority_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_dependency_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_dependency_update_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_dependency_insert_guard;
