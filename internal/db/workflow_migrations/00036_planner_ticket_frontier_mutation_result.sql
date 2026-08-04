-- +goose Up
-- This is a forward-only allowlist expansion. SQLite requires rebuilding the
-- table to retain its existing constraints while admitting the published
-- Planner frontier surface.
PRAGMA foreign_keys=off;

DROP TRIGGER IF EXISTS mcp_mutation_result_delete_guard;
DROP TRIGGER IF EXISTS mcp_mutation_result_immutable_update;
DROP TRIGGER IF EXISTS operation_packet_publication_mutation_result_delete_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_mutation_result_update_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_closure_guard;
DROP TRIGGER IF EXISTS operation_packet_publication_mutation_result_authority_guard;

CREATE TABLE mcp_mutation_results_frontier_next (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    surface_contract_id TEXT NOT NULL CHECK (
        surface_contract_id IN (
            'wayfinder-workspace.v1',
            'wayfinder-discovery.v1',
            'wayfinder-investigation.v1',
            'planner-authoring.v1',
            'planner-plan.v1',
            'planner-execution.v1',
            'planner-ticket-frontier.v1',
            'auditor-review.v1',
            'auditor-audit.v1',
            'auditor-remediation.v1'
        )
    ),
    tool_name TEXT NOT NULL CHECK (
        tool_name IN (
            'create_operation_packet',
            'refresh_operation_packet',
            'close_operation_packet',
            'submit_plan',
            'create_run',
            'record_audit_decision'
        )
    ),
    mutation_id TEXT NOT NULL CHECK (mutation_id GLOB '[A-Za-z0-9]*' AND mutation_id NOT GLOB '*[^A-Za-z0-9._:-]*' AND length(mutation_id) BETWEEN 1 AND 128),
    surface_manifest_sha256 TEXT NOT NULL CHECK (length(surface_manifest_sha256) = 64 AND surface_manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
    semantic_identity_version TEXT NOT NULL CHECK (semantic_identity_version <> '' AND trim(semantic_identity_version) = semantic_identity_version AND length(semantic_identity_version) <= 255),
    semantic_request_sha256 TEXT NOT NULL CHECK (length(semantic_request_sha256) = 64 AND semantic_request_sha256 NOT GLOB '*[^0-9a-f]*'),
    result_kind TEXT NOT NULL CHECK (
        result_kind IN (
            'create_operation_packet_result',
            'refresh_operation_packet_result',
            'close_operation_packet_result',
            'submit_plan_result',
            'create_run_result',
            'record_audit_decision_result'
        )
    ),
    result_identity_json TEXT NOT NULL CHECK (
        length(CAST(result_identity_json AS BLOB)) BETWEEN 2 AND 65536
        AND json_valid(result_identity_json)
        AND json_type(result_identity_json) = 'object'
        AND json(result_identity_json) = result_identity_json
    ),
    result_sha256 TEXT NOT NULL CHECK (length(result_sha256) = 64 AND result_sha256 NOT GLOB '*[^0-9a-f]*'),
    committed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (surface_contract_id, tool_name, mutation_id),
    CHECK (
        (tool_name IN ('create_operation_packet', 'refresh_operation_packet', 'close_operation_packet')
            AND surface_contract_id IN (
                'wayfinder-workspace.v1',
                'wayfinder-discovery.v1',
                'wayfinder-investigation.v1',
                'planner-authoring.v1',
                'planner-plan.v1',
                'planner-execution.v1',
                'planner-ticket-frontier.v1',
                'auditor-review.v1',
                'auditor-audit.v1',
                'auditor-remediation.v1'
            ))
        OR (tool_name = 'submit_plan' AND surface_contract_id = 'planner-plan.v1')
        OR (tool_name = 'create_run' AND surface_contract_id IN ('planner-execution.v1', 'auditor-remediation.v1'))
        OR (tool_name = 'record_audit_decision' AND surface_contract_id = 'auditor-audit.v1')
    ),
    CHECK (
        (tool_name = 'create_operation_packet' AND result_kind = 'create_operation_packet_result')
        OR (tool_name = 'refresh_operation_packet' AND result_kind = 'refresh_operation_packet_result')
        OR (tool_name = 'close_operation_packet' AND result_kind = 'close_operation_packet_result')
        OR (tool_name = 'submit_plan' AND result_kind = 'submit_plan_result')
        OR (tool_name = 'create_run' AND result_kind = 'create_run_result')
        OR (tool_name = 'record_audit_decision' AND result_kind = 'record_audit_decision_result')
    )
);

INSERT INTO mcp_mutation_results_frontier_next (
    id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256,
    semantic_identity_version, semantic_request_sha256, result_kind,
    result_identity_json, result_sha256, committed_at
)
SELECT id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256,
       semantic_identity_version, semantic_request_sha256, result_kind,
       result_identity_json, result_sha256, committed_at
FROM mcp_mutation_results;

DROP TABLE mcp_mutation_results;
ALTER TABLE mcp_mutation_results_frontier_next RENAME TO mcp_mutation_results;

-- +goose StatementBegin
CREATE TRIGGER mcp_mutation_result_immutable_update BEFORE UPDATE ON mcp_mutation_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'MCP mutation results are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER mcp_mutation_result_delete_guard BEFORE DELETE ON mcp_mutation_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'MCP mutation results are retained authority'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_mutation_result_update_guard BEFORE UPDATE ON mcp_mutation_results FOR EACH ROW WHEN EXISTS (SELECT 1 FROM operation_packet_publications WHERE mutation_result_row_id = OLD.id) BEGIN SELECT RAISE(ABORT, 'operation packet publication mutation result is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_mutation_result_delete_guard BEFORE DELETE ON mcp_mutation_results FOR EACH ROW WHEN EXISTS (SELECT 1 FROM operation_packet_publications WHERE mutation_result_row_id = OLD.id) BEGIN SELECT RAISE(ABORT, 'operation packet publication mutation result is retained authority'); END;
-- +goose StatementEnd
-- The publication guards reference the rebuilt table and must be recreated.
-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_closure_guard
BEFORE INSERT ON operation_packet_publications
FOR EACH ROW WHEN
    NOT EXISTS (
        SELECT 1
        FROM operation_packets AS packet
        JOIN operation_packet_artifacts AS artifact ON artifact.id = packet.packet_artifact_row_id
        WHERE packet.id = NEW.packet_row_id
          AND packet.coordinated_publication_id = NEW.publication_id
          AND packet.packet_artifact_row_id = NEW.packet_artifact_row_id
          AND packet.packet_sha256 = artifact.sha256
    )
    OR NOT EXISTS (SELECT 1 FROM mcp_mutation_results WHERE id = NEW.mutation_result_row_id)
    OR NEW.expected_retained_artifact_count <> (SELECT COUNT(*) FROM operation_packet_retained_artifacts WHERE publication_id = NEW.publication_id)
    OR NEW.expected_binding_count <> (SELECT COUNT(*) FROM operation_packet_artifact_bindings WHERE publication_id = NEW.publication_id AND packet_row_id = NEW.packet_row_id)
    OR NEW.expected_dependency_count <> (SELECT COUNT(*) FROM operation_packet_retention_dependencies WHERE packet_row_id = NEW.packet_row_id)
    OR NEW.expected_vault_relationship_count <> (SELECT COUNT(*) FROM operation_packet_vault_relationships WHERE publication_id = NEW.publication_id AND packet_row_id = NEW.packet_row_id)
    OR 1 <> (SELECT COUNT(*) FROM operation_packet_artifact_bindings WHERE publication_id = NEW.publication_id AND packet_row_id = NEW.packet_row_id AND dependency_class = 'packet_document' AND packet_artifact_row_id = NEW.packet_artifact_row_id AND retained_artifact_row_id IS NULL)
    OR EXISTS (SELECT 1 FROM operation_packet_retained_artifacts AS retained WHERE retained.publication_id = NEW.publication_id AND NOT EXISTS (SELECT 1 FROM operation_packet_artifact_bindings AS binding WHERE binding.publication_id = NEW.publication_id AND binding.retained_artifact_row_id = retained.id))
    OR EXISTS (SELECT 1 FROM operation_packet_artifact_bindings AS binding LEFT JOIN operation_packet_retained_artifacts AS retained ON retained.id = binding.retained_artifact_row_id LEFT JOIN operation_packet_retention_dependencies AS dependency ON dependency.packet_row_id = NEW.packet_row_id AND dependency.dependency_class = binding.dependency_class AND dependency.dependency_key = binding.dependency_key LEFT JOIN operation_packet_artifacts AS packet_artifact ON packet_artifact.id = binding.packet_artifact_row_id WHERE binding.publication_id = NEW.publication_id AND (binding.packet_row_id <> NEW.packet_row_id OR dependency.id IS NULL OR dependency.required <> 1 OR dependency.attached <> 1 OR dependency.retained <> 1 OR dependency.owner_identity IS NULL OR dependency.owner_identity <> COALESCE(packet_artifact.artifact_id, retained.artifact_id) OR (binding.retained_artifact_row_id IS NOT NULL AND retained.publication_id <> NEW.publication_id)))
    OR EXISTS (SELECT 1 FROM operation_packet_vault_relationships AS relationship LEFT JOIN operation_packet_retention_dependencies AS dependency ON dependency.packet_row_id = NEW.packet_row_id AND dependency.dependency_class = relationship.dependency_class AND dependency.dependency_key = relationship.dependency_key LEFT JOIN source_vault_retentions AS retention ON retention.id = relationship.retention_row_id LEFT JOIN source_vault_closures AS closure ON closure.id = relationship.closure_row_id LEFT JOIN source_vaults AS vault ON vault.id = relationship.vault_row_id WHERE relationship.publication_id = NEW.publication_id AND (relationship.packet_row_id <> NEW.packet_row_id OR dependency.id IS NULL OR dependency.required <> 1 OR dependency.attached <> 1 OR dependency.retained <> 1 OR dependency.owner_identity <> relationship.owner_identity OR retention.id IS NULL OR retention.owner_class <> 'operation_packet' OR retention.owner_identity <> relationship.owner_identity OR retention.state <> 'active' OR retention.closure_row_id <> relationship.closure_row_id OR closure.id IS NULL OR closure.state <> 'ready' OR closure.vault_row_id <> relationship.vault_row_id OR closure.commit_oid <> relationship.commit_oid OR closure.tree_oid <> relationship.tree_oid OR vault.id IS NULL))
    OR EXISTS (SELECT 1 FROM operation_packet_retention_dependencies AS dependency WHERE dependency.packet_row_id = NEW.packet_row_id AND NOT EXISTS (SELECT 1 FROM operation_packet_artifact_bindings AS binding WHERE binding.publication_id = NEW.publication_id AND binding.dependency_class = dependency.dependency_class AND binding.dependency_key = dependency.dependency_key) AND NOT EXISTS (SELECT 1 FROM operation_packet_vault_relationships AS relationship WHERE relationship.publication_id = NEW.publication_id AND relationship.dependency_class = dependency.dependency_class AND relationship.dependency_key = dependency.dependency_key))
    OR EXISTS (SELECT 1 FROM operation_packet_artifact_bindings AS binding JOIN operation_packet_vault_relationships AS relationship ON relationship.publication_id = binding.publication_id AND relationship.dependency_class = binding.dependency_class AND relationship.dependency_key = binding.dependency_key WHERE binding.publication_id = NEW.publication_id)
BEGIN SELECT RAISE(ABORT, 'operation packet publication closure is incomplete'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER operation_packet_publication_mutation_result_authority_guard BEFORE INSERT ON operation_packet_publications FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM operation_packets AS packet JOIN mcp_mutation_results AS mutation_result ON mutation_result.id = NEW.mutation_result_row_id WHERE packet.id = NEW.packet_row_id AND packet.surface_contract_id = mutation_result.surface_contract_id) BEGIN SELECT RAISE(ABORT, 'operation packet publication mutation result authority does not match'); END;
-- +goose StatementEnd

PRAGMA foreign_keys=on;

-- +goose Down
-- Historical migrations are immutable; this forward repair intentionally has
-- no destructive down path for installed Relay databases.
