-- +goose Up
-- Cutover is explicit activation history. A prepared record is inert until the
-- exact Transition Plan evidence is complete, then it can become the one
-- current active cutover. Crossing the first ticket-oriented execution
-- boundary is one-way.
CREATE TABLE cutover_activations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cutover_activation_id TEXT NOT NULL UNIQUE,
    workspace_row_id INTEGER NOT NULL REFERENCES feature_workspaces(id) ON DELETE RESTRICT,
    transition_plan_ticket_revision_row_id INTEGER NOT NULL REFERENCES delivery_ticket_revisions(id) ON DELETE RESTRICT,
    transition_plan_ticket_id TEXT NOT NULL,
    transition_plan_ticket_revision INTEGER NOT NULL CHECK (transition_plan_ticket_revision >= 1),
    transition_plan_authority_layer_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_layers(id) ON DELETE RESTRICT,
    transition_plan_sha256 TEXT NOT NULL CHECK (length(transition_plan_sha256) = 64 AND transition_plan_sha256 NOT GLOB '*[^0-9a-f]*'),
    authority_revision_row_id INTEGER NOT NULL REFERENCES feature_workspace_authority_revisions(id) ON DELETE RESTRICT,
    authority_revision_id TEXT NOT NULL,
    authority_revision_number INTEGER NOT NULL CHECK (authority_revision_number >= 1),
    authority_sha256 TEXT NOT NULL CHECK (length(authority_sha256) = 64 AND authority_sha256 NOT GLOB '*[^0-9a-f]*'),
    rollback_eligibility TEXT NOT NULL CHECK (rollback_eligibility IN ('eligible', 'not_eligible')),
    activation_status TEXT NOT NULL DEFAULT 'prepared' CHECK (activation_status IN ('prepared', 'active', 'rolled_back')),
    activated_at TEXT,
    execution_boundary_status TEXT NOT NULL DEFAULT 'open' CHECK (execution_boundary_status IN ('open', 'crossed')),
    first_new_execution_run_row_id INTEGER UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    first_new_execution_at TEXT,
    rollback_status TEXT NOT NULL DEFAULT 'pending' CHECK (rollback_status IN ('pending', 'available', 'not_eligible', 'forbidden', 'rolled_back')),
    roll_forward_status TEXT NOT NULL DEFAULT 'pending' CHECK (roll_forward_status IN ('pending', 'required', 'completed', 'not_required')),
    rolled_back_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (cutover_activation_id GLOB 'cutover-*' AND trim(cutover_activation_id) = cutover_activation_id),
    CHECK (transition_plan_ticket_id <> '' AND trim(transition_plan_ticket_id) = transition_plan_ticket_id),
    CHECK (authority_revision_id GLOB 'authority-*' AND trim(authority_revision_id) = authority_revision_id),
    CHECK (activated_at IS NULL OR trim(activated_at) <> ''),
    CHECK (first_new_execution_at IS NULL OR trim(first_new_execution_at) <> ''),
    CHECK (rolled_back_at IS NULL OR trim(rolled_back_at) <> ''),
    CHECK (
        (activation_status = 'prepared'
            AND activated_at IS NULL
            AND execution_boundary_status = 'open'
            AND first_new_execution_run_row_id IS NULL
            AND first_new_execution_at IS NULL
            AND rollback_status = 'pending'
            AND roll_forward_status = 'pending'
            AND rolled_back_at IS NULL) OR
        (activation_status = 'active'
            AND activated_at IS NOT NULL
            AND execution_boundary_status = 'open'
            AND first_new_execution_run_row_id IS NULL
            AND first_new_execution_at IS NULL
            AND ((rollback_eligibility = 'eligible' AND rollback_status = 'available')
                OR (rollback_eligibility = 'not_eligible' AND rollback_status = 'not_eligible'))
            AND roll_forward_status = 'pending'
            AND rolled_back_at IS NULL) OR
        (activation_status = 'active'
            AND activated_at IS NOT NULL
            AND execution_boundary_status = 'crossed'
            AND first_new_execution_run_row_id IS NOT NULL
            AND first_new_execution_at IS NOT NULL
            AND rollback_status = 'forbidden'
            AND roll_forward_status IN ('required', 'completed')
            AND rolled_back_at IS NULL) OR
        (activation_status = 'rolled_back'
            AND activated_at IS NOT NULL
            AND execution_boundary_status = 'open'
            AND first_new_execution_run_row_id IS NULL
            AND first_new_execution_at IS NULL
            AND rollback_status = 'rolled_back'
            AND roll_forward_status = 'not_required'
            AND rolled_back_at IS NOT NULL)
    )
);

CREATE TABLE cutover_activation_prerequisite_evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activation_row_id INTEGER NOT NULL REFERENCES cutover_activations(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    prerequisite TEXT NOT NULL CHECK (prerequisite <> '' AND trim(prerequisite) <> ''),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (activation_row_id, sequence)
);

CREATE TABLE cutover_activation_obligation_evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activation_row_id INTEGER NOT NULL REFERENCES cutover_activations(id) ON DELETE RESTRICT,
    obligation_kind TEXT NOT NULL CHECK (obligation_kind IN ('activation', 'rollback')),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    obligation TEXT NOT NULL CHECK (obligation <> '' AND trim(obligation) <> ''),
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (activation_row_id, obligation_kind, sequence)
);

CREATE TABLE cutover_roll_forward_criteria (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activation_row_id INTEGER NOT NULL REFERENCES cutover_activations(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    completion_criterion TEXT NOT NULL CHECK (completion_criterion <> '' AND trim(completion_criterion) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (activation_row_id, sequence)
);

CREATE TABLE cutover_roll_forward_evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activation_row_id INTEGER NOT NULL REFERENCES cutover_activations(id) ON DELETE RESTRICT,
    criterion_row_id INTEGER NOT NULL UNIQUE REFERENCES cutover_roll_forward_criteria(id) ON DELETE RESTRICT,
    evidence TEXT NOT NULL CHECK (evidence <> '' AND trim(evidence) <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (activation_row_id, criterion_row_id)
);

CREATE TABLE cutover_current_states (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    activation_row_id INTEGER UNIQUE REFERENCES cutover_activations(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX idx_cutover_activations_one_active
ON cutover_activations(activation_status)
WHERE activation_status = 'active';
CREATE INDEX idx_cutover_activations_workspace
ON cutover_activations(workspace_row_id, created_at, id);
CREATE INDEX idx_cutover_activation_prerequisites_activation
ON cutover_activation_prerequisite_evidence(activation_row_id, sequence, id);
CREATE INDEX idx_cutover_activation_obligations_activation
ON cutover_activation_obligation_evidence(activation_row_id, obligation_kind, sequence, id);
CREATE INDEX idx_cutover_roll_forward_criteria_activation
ON cutover_roll_forward_criteria(activation_row_id, sequence, id);

-- +goose StatementBegin
CREATE TRIGGER cutover_activation_insert_guard
BEFORE INSERT ON cutover_activations
FOR EACH ROW WHEN NOT (
    NEW.activation_status = 'prepared'
    AND NEW.activated_at IS NULL
    AND NEW.execution_boundary_status = 'open'
    AND NEW.first_new_execution_run_row_id IS NULL
    AND NEW.first_new_execution_at IS NULL
    AND NEW.rollback_status = 'pending'
    AND NEW.roll_forward_status = 'pending'
    AND NEW.rolled_back_at IS NULL
    AND EXISTS (
        SELECT 1
        FROM feature_workspaces AS workspace
        JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.transition_plan_ticket_revision_row_id
        JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
        JOIN feature_workspace_authority_revisions AS authority ON authority.id = NEW.authority_revision_row_id
        JOIN feature_workspace_authority_layers AS layer ON layer.id = NEW.transition_plan_authority_layer_row_id
        WHERE workspace.id = NEW.workspace_row_id
          AND workspace.current_authority_revision_row_id = authority.id
          AND ticket.workspace_row_id = workspace.id
          AND ticket.ticket_id = NEW.transition_plan_ticket_id
          AND ticket.current_revision_row_id = revision.id
          AND revision.revision_number = NEW.transition_plan_ticket_revision
          AND revision.transition_applicability = 'required'
          AND revision.cancellation_reason IS NULL
          AND authority.workspace_row_id = workspace.id
          AND authority.authority_revision_id = NEW.authority_revision_id
          AND authority.revision_number = NEW.authority_revision_number
          AND layer.authority_revision_row_id = authority.id
          AND layer.layer_kind = 'plan'
          AND layer.artifact_sha256 = NEW.transition_plan_sha256
    )
)
BEGIN SELECT RAISE(ABORT, 'cutover activation must bind the current exact transition plan ticket and authority'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_current_binding_guard
BEFORE UPDATE OF activation_status ON cutover_activations
FOR EACH ROW WHEN NEW.activation_status = 'active' AND NOT EXISTS (
    SELECT 1
    FROM feature_workspaces AS workspace
    JOIN delivery_ticket_revisions AS revision ON revision.id = NEW.transition_plan_ticket_revision_row_id
    JOIN delivery_tickets AS ticket ON ticket.id = revision.delivery_ticket_row_id
    JOIN feature_workspace_authority_revisions AS authority ON authority.id = NEW.authority_revision_row_id
    JOIN feature_workspace_authority_layers AS layer ON layer.id = NEW.transition_plan_authority_layer_row_id
    WHERE workspace.id = NEW.workspace_row_id
      AND workspace.current_authority_revision_row_id = authority.id
      AND ticket.workspace_row_id = workspace.id
      AND ticket.ticket_id = NEW.transition_plan_ticket_id
      AND ticket.current_revision_row_id = revision.id
      AND revision.revision_number = NEW.transition_plan_ticket_revision
      AND revision.transition_applicability = 'required'
      AND revision.cancellation_reason IS NULL
      AND authority.workspace_row_id = workspace.id
      AND authority.authority_revision_id = NEW.authority_revision_id
      AND authority.revision_number = NEW.authority_revision_number
      AND layer.authority_revision_row_id = authority.id
      AND layer.layer_kind = 'plan'
      AND layer.artifact_sha256 = NEW.transition_plan_sha256
)
BEGIN SELECT RAISE(ABORT, 'cutover activation requires the current exact transition plan ticket and authority'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_state_guard
BEFORE UPDATE OF activation_status, activated_at, execution_boundary_status,
    first_new_execution_run_row_id, first_new_execution_at, rollback_status,
    roll_forward_status, rolled_back_at ON cutover_activations
FOR EACH ROW WHEN NOT (
    (
        OLD.activation_status = 'prepared'
        AND NEW.activation_status = 'active'
        AND NEW.activated_at IS NOT NULL
        AND NEW.execution_boundary_status IS OLD.execution_boundary_status
        AND NEW.first_new_execution_run_row_id IS OLD.first_new_execution_run_row_id
        AND NEW.first_new_execution_at IS OLD.first_new_execution_at
        AND NEW.rollback_status = CASE OLD.rollback_eligibility WHEN 'eligible' THEN 'available' ELSE 'not_eligible' END
        AND NEW.roll_forward_status IS OLD.roll_forward_status
        AND NEW.rolled_back_at IS OLD.rolled_back_at
        AND EXISTS (
            SELECT 1 FROM cutover_current_states
            WHERE singleton_id = 1 AND activation_row_id = OLD.id
        )
        AND EXISTS (
            SELECT 1 FROM cutover_activation_prerequisite_evidence
            WHERE activation_row_id = OLD.id
        )
        AND EXISTS (
            SELECT 1 FROM cutover_activation_obligation_evidence
            WHERE activation_row_id = OLD.id AND obligation_kind = 'activation'
        )
        AND (
            (OLD.rollback_eligibility = 'eligible' AND EXISTS (
                SELECT 1 FROM cutover_activation_obligation_evidence
                WHERE activation_row_id = OLD.id AND obligation_kind = 'rollback'
            )) OR
            (OLD.rollback_eligibility = 'not_eligible' AND NOT EXISTS (
                SELECT 1 FROM cutover_activation_obligation_evidence
                WHERE activation_row_id = OLD.id AND obligation_kind = 'rollback'
            ))
        )
        AND EXISTS (
            SELECT 1 FROM cutover_roll_forward_criteria
            WHERE activation_row_id = OLD.id
        )
    ) OR
    (
        OLD.activation_status = 'active'
        AND OLD.execution_boundary_status = 'open'
        AND OLD.rollback_status IN ('available', 'not_eligible')
        AND OLD.roll_forward_status = 'pending'
        AND OLD.rolled_back_at IS NULL
        AND NEW.activation_status IS OLD.activation_status
        AND NEW.activated_at IS OLD.activated_at
        AND NEW.execution_boundary_status = 'crossed'
        AND NEW.first_new_execution_run_row_id IS NOT NULL
        AND NEW.first_new_execution_at IS NOT NULL
        AND NEW.rollback_status = 'forbidden'
        AND NEW.roll_forward_status = 'required'
        AND NEW.rolled_back_at IS OLD.rolled_back_at
        AND EXISTS (
            SELECT 1 FROM cutover_current_states
            WHERE singleton_id = 1 AND activation_row_id = OLD.id
        )
        AND EXISTS (
            SELECT 1
            FROM runs AS run
            JOIN execution_packages AS package ON package.id = run.execution_package_row_id
            WHERE run.id = NEW.first_new_execution_run_row_id
              AND run.created_at >= NEW.activated_at
              AND package.authority_revision_row_id = OLD.authority_revision_row_id
              AND EXISTS (
                  SELECT 1 FROM execution_package_members AS member
                  WHERE member.package_row_id = package.id
                    AND member.revision_row_id = OLD.transition_plan_ticket_revision_row_id
              )
        )
    ) OR
    (
        OLD.activation_status = 'active'
        AND OLD.execution_boundary_status = 'crossed'
        AND OLD.rollback_status = 'forbidden'
        AND OLD.roll_forward_status = 'required'
        AND NEW.activation_status IS OLD.activation_status
        AND NEW.activated_at IS OLD.activated_at
        AND NEW.execution_boundary_status IS OLD.execution_boundary_status
        AND NEW.first_new_execution_run_row_id IS OLD.first_new_execution_run_row_id
        AND NEW.first_new_execution_at IS OLD.first_new_execution_at
        AND NEW.rollback_status IS OLD.rollback_status
        AND NEW.roll_forward_status = 'completed'
        AND NEW.rolled_back_at IS OLD.rolled_back_at
        AND EXISTS (
            SELECT 1 FROM cutover_current_states
            WHERE singleton_id = 1 AND activation_row_id = OLD.id
        )
        AND NOT EXISTS (
            SELECT 1
            FROM cutover_roll_forward_criteria AS criterion
            WHERE criterion.activation_row_id = OLD.id
              AND NOT EXISTS (
                  SELECT 1 FROM cutover_roll_forward_evidence AS evidence
                  WHERE evidence.activation_row_id = OLD.id
                    AND evidence.criterion_row_id = criterion.id
              )
        )
    ) OR
    (
        OLD.activation_status = 'active'
        AND OLD.execution_boundary_status = 'open'
        AND OLD.rollback_eligibility = 'eligible'
        AND OLD.rollback_status = 'available'
        AND OLD.roll_forward_status = 'pending'
        AND OLD.rolled_back_at IS NULL
        AND NEW.activation_status = 'rolled_back'
        AND NEW.activated_at IS OLD.activated_at
        AND NEW.execution_boundary_status IS OLD.execution_boundary_status
        AND NEW.first_new_execution_run_row_id IS OLD.first_new_execution_run_row_id
        AND NEW.first_new_execution_at IS OLD.first_new_execution_at
        AND NEW.rollback_status = 'rolled_back'
        AND NEW.roll_forward_status = 'not_required'
        AND NEW.rolled_back_at IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM cutover_current_states
            WHERE singleton_id = 1 AND activation_row_id = OLD.id
        )
    )
)
BEGIN SELECT RAISE(ABORT, 'cutover activation lifecycle is one-way'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_identity_immutable
BEFORE UPDATE OF id, cutover_activation_id, workspace_row_id,
    transition_plan_ticket_revision_row_id, transition_plan_ticket_id,
    transition_plan_ticket_revision, transition_plan_authority_layer_row_id,
    transition_plan_sha256, authority_revision_row_id, authority_revision_id,
    authority_revision_number, authority_sha256, rollback_eligibility, created_at
ON cutover_activations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover activation identity is immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_delete_guard
BEFORE DELETE ON cutover_activations
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover activations are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER cutover_activation_prerequisite_insert_guard
BEFORE INSERT ON cutover_activation_prerequisite_evidence
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM cutover_activations
    WHERE id = NEW.activation_row_id AND activation_status = 'prepared'
)
BEGIN SELECT RAISE(ABORT, 'cutover prerequisite evidence may only be recorded while activation is prepared'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_prerequisite_update_immutable
BEFORE UPDATE ON cutover_activation_prerequisite_evidence
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover prerequisite evidence is immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_prerequisite_delete_guard
BEFORE DELETE ON cutover_activation_prerequisite_evidence
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover prerequisite evidence is retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER cutover_activation_obligation_insert_guard
BEFORE INSERT ON cutover_activation_obligation_evidence
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM cutover_activations
    WHERE id = NEW.activation_row_id AND activation_status = 'prepared'
)
BEGIN SELECT RAISE(ABORT, 'cutover obligation evidence may only be recorded while activation is prepared'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_obligation_update_immutable
BEFORE UPDATE ON cutover_activation_obligation_evidence
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover obligation evidence is immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_activation_obligation_delete_guard
BEFORE DELETE ON cutover_activation_obligation_evidence
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover obligation evidence is retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER cutover_roll_forward_criteria_insert_guard
BEFORE INSERT ON cutover_roll_forward_criteria
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1 FROM cutover_activations
    WHERE id = NEW.activation_row_id AND activation_status = 'prepared'
)
BEGIN SELECT RAISE(ABORT, 'cutover roll-forward criteria may only be recorded while activation is prepared'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_roll_forward_criteria_update_immutable
BEFORE UPDATE ON cutover_roll_forward_criteria
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover roll-forward criteria are immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_roll_forward_criteria_delete_guard
BEFORE DELETE ON cutover_roll_forward_criteria
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover roll-forward criteria are retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER cutover_roll_forward_evidence_insert_guard
BEFORE INSERT ON cutover_roll_forward_evidence
FOR EACH ROW WHEN NOT EXISTS (
    SELECT 1
    FROM cutover_activations AS activation
    JOIN cutover_roll_forward_criteria AS criterion ON criterion.id = NEW.criterion_row_id
    JOIN cutover_current_states AS current_state ON current_state.activation_row_id = activation.id
    WHERE activation.id = NEW.activation_row_id
      AND criterion.activation_row_id = activation.id
      AND current_state.singleton_id = 1
      AND activation.activation_status = 'active'
      AND activation.execution_boundary_status = 'crossed'
      AND activation.roll_forward_status = 'required'
)
BEGIN SELECT RAISE(ABORT, 'cutover roll-forward evidence must bind a current crossed activation criterion'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_roll_forward_evidence_update_immutable
BEFORE UPDATE ON cutover_roll_forward_evidence
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover roll-forward evidence is immutable history'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_roll_forward_evidence_delete_guard
BEFORE DELETE ON cutover_roll_forward_evidence
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover roll-forward evidence is retained history'); END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER cutover_current_state_insert_guard
BEFORE INSERT ON cutover_current_states
FOR EACH ROW WHEN NOT (
    NEW.singleton_id = 1
    AND NEW.activation_row_id IS NOT NULL
    AND EXISTS (
        SELECT 1 FROM cutover_activations
        WHERE id = NEW.activation_row_id AND activation_status IN ('prepared', 'active')
    )
)
BEGIN SELECT RAISE(ABORT, 'cutover current state must point to one prepared or active activation'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_current_state_update_guard
BEFORE UPDATE ON cutover_current_states
FOR EACH ROW WHEN NOT (
    NEW.singleton_id IS OLD.singleton_id
    AND NEW.created_at IS OLD.created_at
    AND (
        (OLD.activation_row_id IS NULL AND NEW.activation_row_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM cutover_activations
            WHERE id = NEW.activation_row_id AND activation_status IN ('prepared', 'active')
        )) OR
        (OLD.activation_row_id IS NOT NULL AND NEW.activation_row_id IS NULL AND EXISTS (
            SELECT 1 FROM cutover_activations
            WHERE id = OLD.activation_row_id AND activation_status = 'rolled_back'
        ))
    )
)
BEGIN SELECT RAISE(ABORT, 'cutover current state may only advance to a prepared activation or clear a rolled-back activation'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER cutover_current_state_delete_guard
BEFORE DELETE ON cutover_current_states
FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'cutover current state is retained'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS cutover_current_state_delete_guard;
DROP TRIGGER IF EXISTS cutover_current_state_update_guard;
DROP TRIGGER IF EXISTS cutover_current_state_insert_guard;
DROP TRIGGER IF EXISTS cutover_roll_forward_evidence_delete_guard;
DROP TRIGGER IF EXISTS cutover_roll_forward_evidence_update_immutable;
DROP TRIGGER IF EXISTS cutover_roll_forward_evidence_insert_guard;
DROP TRIGGER IF EXISTS cutover_roll_forward_criteria_delete_guard;
DROP TRIGGER IF EXISTS cutover_roll_forward_criteria_update_immutable;
DROP TRIGGER IF EXISTS cutover_roll_forward_criteria_insert_guard;
DROP TRIGGER IF EXISTS cutover_activation_obligation_delete_guard;
DROP TRIGGER IF EXISTS cutover_activation_obligation_update_immutable;
DROP TRIGGER IF EXISTS cutover_activation_obligation_insert_guard;
DROP TRIGGER IF EXISTS cutover_activation_prerequisite_delete_guard;
DROP TRIGGER IF EXISTS cutover_activation_prerequisite_update_immutable;
DROP TRIGGER IF EXISTS cutover_activation_prerequisite_insert_guard;
DROP TRIGGER IF EXISTS cutover_activation_delete_guard;
DROP TRIGGER IF EXISTS cutover_activation_identity_immutable;
DROP TRIGGER IF EXISTS cutover_activation_state_guard;
DROP TRIGGER IF EXISTS cutover_activation_current_binding_guard;
DROP TRIGGER IF EXISTS cutover_activation_insert_guard;
DROP INDEX IF EXISTS idx_cutover_roll_forward_criteria_activation;
DROP INDEX IF EXISTS idx_cutover_activation_obligations_activation;
DROP INDEX IF EXISTS idx_cutover_activation_prerequisites_activation;
DROP INDEX IF EXISTS idx_cutover_activations_workspace;
DROP INDEX IF EXISTS idx_cutover_activations_one_active;
DROP TABLE IF EXISTS cutover_current_states;
DROP TABLE IF EXISTS cutover_roll_forward_evidence;
DROP TABLE IF EXISTS cutover_roll_forward_criteria;
DROP TABLE IF EXISTS cutover_activation_obligation_evidence;
DROP TABLE IF EXISTS cutover_activation_prerequisite_evidence;
DROP TABLE IF EXISTS cutover_activations;
