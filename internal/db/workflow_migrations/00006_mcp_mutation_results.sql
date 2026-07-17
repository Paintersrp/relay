-- +goose Up
CREATE TABLE mcp_mutation_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    surface_contract_id TEXT NOT NULL CHECK (
        surface_contract_id IN (
            'planner-authoring.v1',
            'planner-plan.v1',
            'planner-execution.v1',
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
                'planner-authoring.v1',
                'planner-plan.v1',
                'planner-execution.v1',
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

-- +goose StatementBegin
CREATE TRIGGER mcp_mutation_result_immutable_update BEFORE UPDATE ON mcp_mutation_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'MCP mutation results are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER mcp_mutation_result_delete_guard BEFORE DELETE ON mcp_mutation_results FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'MCP mutation results are retained authority'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS mcp_mutation_result_delete_guard;
DROP TRIGGER IF EXISTS mcp_mutation_result_immutable_update;
DROP TABLE IF EXISTS mcp_mutation_results;

