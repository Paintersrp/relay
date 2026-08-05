-- +goose Up
-- Prototype discovery execution is independently owned and intentionally has
-- no foreign keys to production runs, attempts, packages, or leases.
CREATE TABLE feature_workspace_prototype_proposals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    work_item_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    discovery_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    proposal_sha256 TEXT NOT NULL CHECK (length(proposal_sha256) = 64 AND proposal_sha256 NOT GLOB '*[^0-9a-f]*'),
    proposal_size_bytes INTEGER NOT NULL CHECK (proposal_size_bytes >= 0),
    proposal_media_type TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (proposal_id GLOB 'prototype-proposal-*' AND trim(proposal_id) = proposal_id)
);
CREATE TABLE feature_workspace_prototype_authorizations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    authorization_id TEXT NOT NULL UNIQUE,
    proposal_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_proposals(id) ON DELETE RESTRICT,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    work_item_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    discovery_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_integrated_discovery_revisions(id) ON DELETE RESTRICT,
    source_closure_row_id INTEGER NOT NULL REFERENCES source_vault_closures(id) ON DELETE RESTRICT,
    source_commit TEXT NOT NULL,
    source_tree TEXT NOT NULL,
    repo_target TEXT NOT NULL COLLATE NOCASE REFERENCES repository_targets(repo_target) ON DELETE RESTRICT,
    base_commit TEXT NOT NULL,
    adapter TEXT NOT NULL,
    model TEXT NOT NULL,
    variants_json TEXT NOT NULL CHECK (json_valid(variants_json) AND json_type(variants_json) = 'array'),
    evidence_obligations_json TEXT NOT NULL CHECK (json_valid(evidence_obligations_json) AND json_type(evidence_obligations_json) = 'array'),
    limits_json TEXT NOT NULL CHECK (json_valid(limits_json) AND json_type(limits_json) = 'object'),
    invocation_artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    invocation_sha256 TEXT NOT NULL CHECK (length(invocation_sha256) = 64 AND invocation_sha256 NOT GLOB '*[^0-9a-f]*'),
    invocation_size_bytes INTEGER NOT NULL CHECK (invocation_size_bytes >= 0),
    invocation_media_type TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (authorization_id GLOB 'prototype-authorization-*' AND trim(authorization_id) = authorization_id),
    CHECK (trim(adapter) <> '' AND trim(model) <> '' AND trim(repo_target) <> '' AND trim(base_commit) <> '')
);
CREATE TABLE feature_workspace_prototype_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prototype_run_id TEXT NOT NULL UNIQUE,
    authorization_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_authorizations(id) ON DELETE RESTRICT,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    work_item_row_id INTEGER NOT NULL REFERENCES feature_workspace_discovery_tickets(id) ON DELETE RESTRICT,
    lifecycle_state TEXT NOT NULL DEFAULT 'proposed' CHECK (lifecycle_state IN ('proposed', 'approved', 'preparing', 'terminal')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    process_outcome TEXT,
    cleanup_status TEXT NOT NULL DEFAULT 'not_required' CHECK (cleanup_status IN ('not_required', 'pending', 'complete', 'failed')),
    launch_uncertainty_reason TEXT,
    external_process_identity TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (prototype_run_id GLOB 'prototype-run-*' AND trim(prototype_run_id) = prototype_run_id),
    CHECK (external_process_identity IS NULL OR trim(external_process_identity) <> '')
);
CREATE TABLE feature_workspace_prototype_approvals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    authorization_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_authorizations(id) ON DELETE RESTRICT,
    mutation_identity TEXT NOT NULL UNIQUE,
    operator_confirmation_evidence TEXT NOT NULL,
    consumed_identity TEXT NOT NULL UNIQUE,
    consumed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (approval_id GLOB 'prototype-approval-*' AND trim(approval_id) = approval_id),
    CHECK (trim(mutation_identity) <> '' AND trim(consumed_identity) <> '' AND length(operator_confirmation_evidence) BETWEEN 1 AND 4096 AND operator_confirmation_evidence = trim(operator_confirmation_evidence))
);
CREATE TABLE feature_workspace_prototype_lifecycle_transitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    transition_identity TEXT NOT NULL UNIQUE,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    run_version INTEGER NOT NULL CHECK (run_version >= 1),
    approval_row_id INTEGER REFERENCES feature_workspace_prototype_approvals(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(run_row_id, run_version),
    CHECK (from_state = 'proposed' AND to_state = 'approved')
);
CREATE TABLE feature_workspace_prototype_launch_claims (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    launch_claim_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    launch_protocol TEXT NOT NULL,
    claimed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (launch_claim_id GLOB 'prototype-launch-claim-*' AND trim(launch_claim_id) = launch_claim_id),
    CHECK (trim(launch_protocol) <> '')
);
CREATE TABLE feature_workspace_prototype_result_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    member_kind TEXT NOT NULL,
    artifact_row_id INTEGER REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    sha256 TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(run_row_id, sequence)
);
CREATE TABLE feature_workspace_prototype_cleanup_obligations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cleanup_obligation_id TEXT NOT NULL UNIQUE,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    obligation_kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'complete', 'failed')),
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (cleanup_obligation_id GLOB 'prototype-cleanup-*' AND trim(cleanup_obligation_id) = cleanup_obligation_id)
);
CREATE TABLE feature_workspace_prototype_qa_associations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,
    association_kind TEXT NOT NULL,
    artifact_row_id INTEGER REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(run_row_id, association_kind, artifact_row_id)
);
CREATE INDEX idx_prototype_proposals_workspace ON feature_workspace_prototype_proposals(workspace_row_id, id);
CREATE INDEX idx_prototype_runs_workspace ON feature_workspace_prototype_runs(workspace_row_id, id);
CREATE INDEX idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id, id);
-- +goose StatementBegin
CREATE TRIGGER prototype_proposal_workspace_guard BEFORE INSERT ON feature_workspace_prototype_proposals FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_tickets WHERE id = NEW.work_item_row_id AND workspace_row_id = NEW.workspace_row_id) OR NOT EXISTS (SELECT 1 FROM feature_workspace_integrated_discovery_revisions WHERE id = NEW.discovery_revision_row_id AND workspace_row_id = NEW.workspace_row_id) OR NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts WHERE id = NEW.artifact_row_id AND workspace_row_id = NEW.workspace_row_id AND sha256 = NEW.proposal_sha256 AND size_bytes = NEW.proposal_size_bytes AND media_type = NEW.proposal_media_type) BEGIN SELECT RAISE(ABORT, 'prototype proposal ownership or artifact mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_authorization_workspace_guard BEFORE INSERT ON feature_workspace_prototype_authorizations FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_proposals WHERE id = NEW.proposal_row_id AND workspace_row_id = NEW.workspace_row_id AND work_item_row_id = NEW.work_item_row_id AND discovery_revision_row_id = NEW.discovery_revision_row_id) OR NOT EXISTS (SELECT 1 FROM feature_workspace_discovery_artifacts WHERE id = NEW.invocation_artifact_row_id AND workspace_row_id = NEW.workspace_row_id AND sha256 = NEW.invocation_sha256 AND size_bytes = NEW.invocation_size_bytes AND media_type = NEW.invocation_media_type) OR NOT EXISTS (SELECT 1 FROM source_vault_closures WHERE id = NEW.source_closure_row_id AND commit_oid = NEW.source_commit AND tree_oid = NEW.source_tree AND state = 'ready') BEGIN SELECT RAISE(ABORT, 'prototype authorization ownership or source mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_run_workspace_guard BEFORE INSERT ON feature_workspace_prototype_runs FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_authorizations WHERE id = NEW.authorization_row_id AND workspace_row_id = NEW.workspace_row_id AND work_item_row_id = NEW.work_item_row_id) BEGIN SELECT RAISE(ABORT, 'prototype run ownership mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_approval_binding_guard BEFORE INSERT ON feature_workspace_prototype_approvals FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runs WHERE id = NEW.run_row_id AND authorization_row_id = NEW.authorization_row_id AND lifecycle_state = 'proposed') BEGIN SELECT RAISE(ABORT, 'prototype approval must bind a proposed exact run authorization'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_immutable_history BEFORE UPDATE ON feature_workspace_prototype_proposals FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'prototype proposals are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_authorization_immutable_history BEFORE UPDATE ON feature_workspace_prototype_authorizations FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'prototype authorizations are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_approval_immutable_history BEFORE UPDATE ON feature_workspace_prototype_approvals FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'prototype approvals are immutable'); END;
-- +goose StatementEnd
-- +goose Down
DROP TABLE IF EXISTS feature_workspace_prototype_qa_associations;
DROP TABLE IF EXISTS feature_workspace_prototype_cleanup_obligations;
DROP TABLE IF EXISTS feature_workspace_prototype_result_members;
DROP TABLE IF EXISTS feature_workspace_prototype_launch_claims;
DROP TABLE IF EXISTS feature_workspace_prototype_lifecycle_transitions;
DROP TABLE IF EXISTS feature_workspace_prototype_approvals;
DROP TABLE IF EXISTS feature_workspace_prototype_runs;
DROP TABLE IF EXISTS feature_workspace_prototype_authorizations;
DROP TABLE IF EXISTS feature_workspace_prototype_proposals;