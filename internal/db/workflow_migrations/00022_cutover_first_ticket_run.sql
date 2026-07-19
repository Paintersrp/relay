-- +goose Up
-- Replace the open->crossed guard requiring the Transition Plan Ticket member.
-- Require ticket-oriented route, post-activation Run, matching package and
-- immutable package approval, permitted authority/source basis, cutover scope,
-- unset first Run, and rollback eligibility. Make crossing monotonic and unique.

DROP TRIGGER IF EXISTS cutover_activation_state_guard;

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
                  SELECT 1 FROM execution_package_approvals
                  WHERE execution_package_approvals.package_row_id = package.id
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

-- +goose Down
DROP TRIGGER IF EXISTS cutover_activation_state_guard;

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
