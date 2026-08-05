-- +goose Up
DROP INDEX IF EXISTS idx_prototype_transitions_run;
ALTER TABLE feature_workspace_prototype_lifecycle_transitions RENAME TO feature_workspace_prototype_lifecycle_transitions_part1;
CREATE TABLE feature_workspace_prototype_lifecycle_transitions (id INTEGER PRIMARY KEY AUTOINCREMENT, run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT, transition_identity TEXT NOT NULL UNIQUE, from_state TEXT NOT NULL, to_state TEXT NOT NULL, run_version INTEGER NOT NULL CHECK (run_version >= 1), approval_row_id INTEGER REFERENCES feature_workspace_prototype_approvals(id) ON DELETE RESTRICT, created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), UNIQUE(run_row_id,run_version), CHECK ((from_state='proposed' AND to_state='approved' AND approval_row_id IS NOT NULL) OR (from_state='approved' AND to_state IN ('preparing','failed') AND approval_row_id IS NULL) OR (from_state='preparing' AND to_state IN ('running','launch_uncertain','failed') AND approval_row_id IS NULL) OR (from_state='launch_uncertain' AND to_state IN ('running','succeeded','failed','cancelled','timed_out','cleanup_required') AND approval_row_id IS NULL) OR (from_state='running' AND to_state IN ('succeeded','failed','cancelled','timed_out','cleanup_required') AND approval_row_id IS NULL)));
INSERT INTO feature_workspace_prototype_lifecycle_transitions SELECT * FROM feature_workspace_prototype_lifecycle_transitions_part1;
DROP TABLE feature_workspace_prototype_lifecycle_transitions_part1;
CREATE INDEX IF NOT EXISTS idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id,id);
ALTER TABLE feature_workspace_prototype_cleanup_obligations RENAME TO feature_workspace_prototype_cleanup_obligations_part1;
CREATE TABLE feature_workspace_prototype_cleanup_obligations (id INTEGER PRIMARY KEY AUTOINCREMENT, cleanup_obligation_id TEXT NOT NULL UNIQUE, run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT, obligation_kind TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','complete','failed')), detail TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), UNIQUE(run_row_id,obligation_kind), CHECK(cleanup_obligation_id GLOB 'prototype-cleanup-*' AND trim(cleanup_obligation_id)=cleanup_obligation_id), CHECK(trim(obligation_kind)<>''));
INSERT INTO feature_workspace_prototype_cleanup_obligations(id,cleanup_obligation_id,run_row_id,obligation_kind,status,detail,created_at,updated_at) SELECT id,cleanup_obligation_id,run_row_id,obligation_kind,status,detail,created_at,created_at FROM feature_workspace_prototype_cleanup_obligations_part1;
DROP TABLE feature_workspace_prototype_cleanup_obligations_part1;
CREATE TABLE feature_workspace_prototype_runtimes (id INTEGER PRIMARY KEY AUTOINCREMENT,runtime_id TEXT NOT NULL UNIQUE,run_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,authorized_commit TEXT NOT NULL,authorized_tree TEXT NOT NULL,runtime_root_path TEXT NOT NULL UNIQUE,worktree_path TEXT NOT NULL UNIQUE,ephemeral_target_key TEXT NOT NULL UNIQUE,lease_token TEXT NOT NULL UNIQUE,background_context_id TEXT NOT NULL UNIQUE,invocation_relative_path TEXT NOT NULL DEFAULT '.relay/prototype/invocation.json',result_relative_path TEXT NOT NULL DEFAULT '.relay/prototype/result.json',export_relative_path TEXT NOT NULL DEFAULT '.relay/prototype/export',preparation_phase TEXT NOT NULL DEFAULT 'reserved' CHECK(preparation_phase IN ('reserved','worktree_ready','preflight_ready','failed')),launch_phase TEXT NOT NULL DEFAULT 'not_claimed' CHECK(launch_phase IN ('not_claimed','claimed','identity_persisted','ownership_unresolved','settled')),process_identity TEXT,process_started_at TEXT,deadline_at TEXT NOT NULL,cancel_identity TEXT UNIQUE,cancel_requested_at TEXT,timeout_identity TEXT UNIQUE,timeout_claimed_at TEXT,preparation_error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')));
CREATE TABLE feature_workspace_prototype_targets (id INTEGER PRIMARY KEY AUTOINCREMENT,target_id TEXT NOT NULL UNIQUE,run_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,runtime_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runtimes(id) ON DELETE RESTRICT,target_key TEXT NOT NULL UNIQUE,worktree_path TEXT NOT NULL UNIQUE,authorized_commit TEXT NOT NULL,authorized_tree TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','ready','release_pending','released','failed')),created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),released_at TEXT);
CREATE TABLE feature_workspace_prototype_leases (id INTEGER PRIMARY KEY AUTOINCREMENT,lease_token TEXT NOT NULL UNIQUE,run_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,runtime_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runtimes(id) ON DELETE RESTRICT,ephemeral_target_key TEXT NOT NULL UNIQUE,owner_instance_id TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'held' CHECK(status IN ('held','release_pending','released','failed')),acquired_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),released_at TEXT,created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')));
CREATE TABLE feature_workspace_prototype_evidence_import_batches (id INTEGER PRIMARY KEY AUTOINCREMENT,evidence_batch_id TEXT NOT NULL UNIQUE,run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,runtime_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runtimes(id) ON DELETE RESTRICT,batch_identity TEXT NOT NULL UNIQUE,settlement_cause TEXT NOT NULL,observation_identity TEXT NOT NULL,process_outcome TEXT NOT NULL,envelope_status TEXT NOT NULL,completeness TEXT NOT NULL,artifact_count INTEGER NOT NULL CHECK(artifact_count>=0),total_size_bytes INTEGER NOT NULL CHECK(total_size_bytes>=0),created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),UNIQUE(run_row_id,settlement_cause,observation_identity));
CREATE UNIQUE INDEX idx_prototype_evidence_one_complete_batch ON feature_workspace_prototype_evidence_import_batches(run_row_id) WHERE completeness='complete';
CREATE TABLE feature_workspace_prototype_results (id INTEGER PRIMARY KEY AUTOINCREMENT,result_id TEXT NOT NULL UNIQUE,run_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,runtime_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_runtimes(id) ON DELETE RESTRICT,evidence_batch_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_prototype_evidence_import_batches(id) ON DELETE RESTRICT,artifact_row_id INTEGER REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,validation_status TEXT NOT NULL,process_exit_code INTEGER,process_outcome TEXT NOT NULL,envelope_sha256 TEXT,validation_error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')));
CREATE TABLE feature_workspace_prototype_evidence_members (id INTEGER PRIMARY KEY AUTOINCREMENT,evidence_member_id TEXT NOT NULL UNIQUE,run_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_runs(id) ON DELETE RESTRICT,evidence_batch_row_id INTEGER NOT NULL REFERENCES feature_workspace_prototype_evidence_import_batches(id) ON DELETE RESTRICT,sequence INTEGER NOT NULL,semantic_role TEXT NOT NULL,relative_path TEXT NOT NULL,artifact_row_id INTEGER NOT NULL UNIQUE REFERENCES feature_workspace_discovery_artifacts(id) ON DELETE RESTRICT,sha256 TEXT NOT NULL,size_bytes INTEGER NOT NULL,media_type TEXT NOT NULL,completeness TEXT NOT NULL,created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),UNIQUE(evidence_batch_row_id,sequence),UNIQUE(evidence_batch_row_id,semantic_role),UNIQUE(evidence_batch_row_id,relative_path));
CREATE INDEX idx_prototype_runtime_launch_phase ON feature_workspace_prototype_runtimes(launch_phase,id);
CREATE INDEX idx_prototype_target_status ON feature_workspace_prototype_targets(status,id);
CREATE INDEX idx_prototype_lease_status ON feature_workspace_prototype_leases(status,id);
CREATE INDEX idx_prototype_batch_run_completeness ON feature_workspace_prototype_evidence_import_batches(run_row_id,completeness,id);
CREATE INDEX idx_prototype_result_validation ON feature_workspace_prototype_results(validation_status,id);
CREATE INDEX idx_prototype_evidence_batch_sequence ON feature_workspace_prototype_evidence_members(evidence_batch_row_id,sequence);
-- +goose StatementBegin
CREATE TRIGGER prototype_runtime_authorization_guard BEFORE INSERT ON feature_workspace_prototype_runtimes FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runs r JOIN feature_workspace_prototype_authorizations a ON a.id=r.authorization_row_id WHERE r.id=NEW.run_row_id AND a.source_commit=NEW.authorized_commit AND a.source_tree=NEW.authorized_tree) BEGIN SELECT RAISE(ABORT,'prototype runtime authorization mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_target_binding_guard BEFORE INSERT ON feature_workspace_prototype_targets FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runtimes r WHERE r.id=NEW.runtime_row_id AND r.run_row_id=NEW.run_row_id AND r.ephemeral_target_key=NEW.target_key AND r.worktree_path=NEW.worktree_path AND r.authorized_commit=NEW.authorized_commit AND r.authorized_tree=NEW.authorized_tree) BEGIN SELECT RAISE(ABORT,'prototype target binding mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_lease_binding_guard BEFORE INSERT ON feature_workspace_prototype_leases FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runtimes r JOIN feature_workspace_prototype_targets t ON t.runtime_row_id=r.id WHERE r.id=NEW.runtime_row_id AND r.run_row_id=NEW.run_row_id AND r.lease_token=NEW.lease_token AND r.ephemeral_target_key=NEW.ephemeral_target_key AND t.run_row_id=NEW.run_row_id AND t.runtime_row_id=NEW.runtime_row_id AND t.target_key=NEW.ephemeral_target_key) BEGIN SELECT RAISE(ABORT,'prototype lease binding mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_batch_binding_guard BEFORE INSERT ON feature_workspace_prototype_evidence_import_batches FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runtimes WHERE id=NEW.runtime_row_id AND run_row_id=NEW.run_row_id) BEGIN SELECT RAISE(ABORT,'prototype batch binding mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_result_binding_guard BEFORE INSERT ON feature_workspace_prototype_results FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_runtimes r JOIN feature_workspace_prototype_evidence_import_batches b ON b.runtime_row_id=r.id WHERE r.id=NEW.runtime_row_id AND r.run_row_id=NEW.run_row_id AND b.id=NEW.evidence_batch_row_id AND b.run_row_id=NEW.run_row_id AND b.completeness='complete' AND b.envelope_status=NEW.validation_status) BEGIN SELECT RAISE(ABORT,'prototype result binding mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_evidence_binding_guard BEFORE INSERT ON feature_workspace_prototype_evidence_members FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM feature_workspace_prototype_evidence_import_batches WHERE id=NEW.evidence_batch_row_id AND run_row_id=NEW.run_row_id) BEGIN SELECT RAISE(ABORT,'prototype evidence binding mismatch'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_runtime_identity_immutable BEFORE UPDATE ON feature_workspace_prototype_runtimes FOR EACH ROW WHEN OLD.runtime_id<>NEW.runtime_id OR OLD.run_row_id<>NEW.run_row_id OR OLD.authorized_commit<>NEW.authorized_commit OR OLD.authorized_tree<>NEW.authorized_tree OR OLD.runtime_root_path<>NEW.runtime_root_path OR OLD.worktree_path<>NEW.worktree_path OR OLD.ephemeral_target_key<>NEW.ephemeral_target_key OR OLD.lease_token<>NEW.lease_token OR OLD.background_context_id<>NEW.background_context_id OR OLD.invocation_relative_path<>NEW.invocation_relative_path OR OLD.result_relative_path<>NEW.result_relative_path OR OLD.export_relative_path<>NEW.export_relative_path BEGIN SELECT RAISE(ABORT,'prototype runtime identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_batch_immutable BEFORE UPDATE ON feature_workspace_prototype_evidence_import_batches BEGIN SELECT RAISE(ABORT,'prototype evidence batches are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_result_immutable BEFORE UPDATE ON feature_workspace_prototype_results BEGIN SELECT RAISE(ABORT,'prototype results are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_evidence_immutable BEFORE UPDATE ON feature_workspace_prototype_evidence_members BEGIN SELECT RAISE(ABORT,'prototype evidence members are immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_cleanup_identity_immutable BEFORE UPDATE ON feature_workspace_prototype_cleanup_obligations FOR EACH ROW WHEN OLD.cleanup_obligation_id<>NEW.cleanup_obligation_id OR OLD.run_row_id<>NEW.run_row_id OR OLD.obligation_kind<>NEW.obligation_kind BEGIN SELECT RAISE(ABORT,'prototype cleanup identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_runtime_shape_guard BEFORE INSERT ON feature_workspace_prototype_runtimes FOR EACH ROW WHEN NEW.runtime_id NOT GLOB 'prototype-runtime-*' OR trim(NEW.runtime_id)<>NEW.runtime_id OR trim(NEW.authorized_commit)='' OR trim(NEW.authorized_tree)='' OR trim(NEW.runtime_root_path)='' OR trim(NEW.worktree_path)='' OR trim(NEW.ephemeral_target_key)='' OR trim(NEW.lease_token)='' OR trim(NEW.background_context_id)='' OR NEW.invocation_relative_path<>'.relay/prototype/invocation.json' OR NEW.result_relative_path<>'.relay/prototype/result.json' OR NEW.export_relative_path<>'.relay/prototype/export' BEGIN SELECT RAISE(ABORT,'prototype runtime shape invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_target_shape_guard BEFORE INSERT ON feature_workspace_prototype_targets FOR EACH ROW WHEN NEW.target_id NOT GLOB 'prototype-target-*' OR trim(NEW.target_id)<>NEW.target_id OR NEW.target_key NOT GLOB 'prototype:*' OR trim(NEW.target_key)<>NEW.target_key OR trim(NEW.worktree_path)='' OR trim(NEW.authorized_commit)='' OR trim(NEW.authorized_tree)='' BEGIN SELECT RAISE(ABORT,'prototype target shape invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_lease_shape_guard BEFORE INSERT ON feature_workspace_prototype_leases FOR EACH ROW WHEN NEW.lease_token NOT GLOB 'prototype-lease-*' OR trim(NEW.lease_token)<>NEW.lease_token OR trim(NEW.ephemeral_target_key)='' OR trim(NEW.owner_instance_id)='' BEGIN SELECT RAISE(ABORT,'prototype lease shape invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_batch_shape_guard BEFORE INSERT ON feature_workspace_prototype_evidence_import_batches FOR EACH ROW WHEN NEW.evidence_batch_id NOT GLOB 'prototype-evidence-batch-*' OR trim(NEW.evidence_batch_id)<>NEW.evidence_batch_id OR trim(NEW.batch_identity)='' OR trim(NEW.observation_identity)='' OR NEW.settlement_cause NOT IN ('runner_success','runner_failure','reconcile_exit','cancel','timeout','host_failure','launch_uncertain') OR NEW.process_outcome NOT IN ('succeeded','failed','cancelled','timed_out','host_failed','unknown') OR NEW.envelope_status NOT IN ('valid','invalid','missing') OR NEW.completeness NOT IN ('complete','partial') OR NEW.artifact_count<0 OR NEW.total_size_bytes<0 BEGIN SELECT RAISE(ABORT,'prototype evidence batch shape invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_result_shape_guard BEFORE INSERT ON feature_workspace_prototype_results FOR EACH ROW WHEN NEW.result_id NOT GLOB 'prototype-result-*' OR trim(NEW.result_id)<>NEW.result_id OR NEW.validation_status NOT IN ('valid','invalid','missing') OR NEW.process_outcome NOT IN ('succeeded','failed','cancelled','timed_out','host_failed','unknown') OR (NEW.validation_status='valid' AND (NEW.artifact_row_id IS NULL OR length(NEW.envelope_sha256)<>64 OR NEW.envelope_sha256 GLOB '*[^0-9a-f]*')) BEGIN SELECT RAISE(ABORT,'prototype result shape invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_evidence_shape_guard BEFORE INSERT ON feature_workspace_prototype_evidence_members FOR EACH ROW WHEN NEW.evidence_member_id NOT GLOB 'prototype-evidence-*' OR trim(NEW.evidence_member_id)<>NEW.evidence_member_id OR NEW.sequence<1 OR trim(NEW.semantic_role)='' OR trim(NEW.media_type)='' OR length(NEW.sha256)<>64 OR NEW.sha256 GLOB '*[^0-9a-f]*' OR NEW.size_bytes<0 OR NEW.completeness NOT IN ('complete','partial') OR trim(NEW.relative_path)<>NEW.relative_path OR NEW.relative_path NOT GLOB '.relay/prototype/export/*' OR NEW.relative_path LIKE '/%' OR NEW.relative_path LIKE '%/../%' OR NEW.relative_path LIKE '../%' OR NEW.relative_path LIKE '%/..' BEGIN SELECT RAISE(ABORT,'prototype evidence member shape invalid'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER prototype_runtime_state_guard BEFORE UPDATE ON feature_workspace_prototype_runtimes FOR EACH ROW WHEN ((NEW.launch_phase IN ('not_claimed','claimed') AND NEW.process_identity IS NOT NULL) OR (NEW.launch_phase IN ('identity_persisted','settled') AND NEW.process_identity IS NULL) OR ((NEW.cancel_identity IS NULL) <> (NEW.cancel_requested_at IS NULL)) OR ((NEW.timeout_identity IS NULL) <> (NEW.timeout_claimed_at IS NULL))) BEGIN SELECT RAISE(ABORT,'prototype runtime state invalid'); END;
-- +goose StatementEnd
-- +goose Down
DROP TABLE IF EXISTS feature_workspace_prototype_evidence_members;
DROP TABLE IF EXISTS feature_workspace_prototype_results;
DROP TABLE IF EXISTS feature_workspace_prototype_evidence_import_batches;
DROP TABLE IF EXISTS feature_workspace_prototype_leases;
DROP TABLE IF EXISTS feature_workspace_prototype_targets;
DROP TABLE IF EXISTS feature_workspace_prototype_runtimes;
ALTER TABLE feature_workspace_prototype_cleanup_obligations RENAME TO feature_workspace_prototype_cleanup_obligations_part2;
ALTER TABLE feature_workspace_prototype_lifecycle_transitions RENAME TO feature_workspace_prototype_lifecycle_transitions_part2;
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
CREATE INDEX IF NOT EXISTS idx_prototype_transitions_run ON feature_workspace_prototype_lifecycle_transitions(run_row_id, id);
INSERT INTO feature_workspace_prototype_cleanup_obligations(id, cleanup_obligation_id, run_row_id, obligation_kind, status, detail, created_at)
SELECT id, cleanup_obligation_id, run_row_id, obligation_kind, status, detail, created_at
FROM feature_workspace_prototype_cleanup_obligations_part2;
INSERT INTO feature_workspace_prototype_lifecycle_transitions(id, run_row_id, transition_identity, from_state, to_state, run_version, approval_row_id, created_at)
SELECT id, run_row_id, transition_identity, from_state, to_state, run_version, approval_row_id, created_at
FROM feature_workspace_prototype_lifecycle_transitions_part2
WHERE from_state = 'proposed' AND to_state = 'approved';
DROP TABLE feature_workspace_prototype_cleanup_obligations_part2;
DROP TABLE feature_workspace_prototype_lifecycle_transitions_part2;