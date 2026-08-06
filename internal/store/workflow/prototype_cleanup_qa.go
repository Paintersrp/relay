package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type PrototypeCleanupReconciliation struct {
	ID, RunRowID, ExpectedRunVersion                                  int64
	ReconciliationID, MutationIdentity, TriggerKind, ObservedRunState string
	ProcessOwnershipStatus, EvidenceSettlementStatus, WorktreeStatus  string
	EphemeralTargetStatus, PrototypeLeaseStatus, ResultingRunState    string
	Diagnostic, CreatedAt                                             string
}

type PrototypeQAPacket struct {
	ID, WorkspaceRowID, RunRowID, ExpectedRunVersion, MemberCount, TotalBytes int64
	QAPacketID, MutationIdentity, Status, CreatedAt                           string
	AdmittedAt                                                                sql.NullString
}

type PrototypeQAPacketMember struct {
	ID, QAPacketRowID, Sequence, ArtifactRowID, SizeBytes      int64
	QAPacketMemberID, MemberKind, SHA256, MediaType, CreatedAt string
}

type PrototypeQAEvidence struct {
	ID, QAPacketRowID, Sequence, ArtifactRowID, SizeBytes    int64
	QAEvidenceID, SemanticRole, SHA256, MediaType, CreatedAt string
}

type PrototypeQAAdmission struct {
	ID, QAPacketRowID, AdmittedMemberCount, AdmittedTotalBytes               int64
	QAAdmissionID, MutationIdentity, OperatorConfirmationEvidence, CreatedAt string
}

const cleanupReconciliationColumns = `id,reconciliation_id,run_row_id,mutation_identity,trigger_kind,expected_run_version,observed_run_state,process_ownership_status,evidence_settlement_status,worktree_status,ephemeral_target_status,prototype_lease_status,resulting_run_state,diagnostic,created_at`
const qaPacketColumns = `id,qa_packet_id,workspace_row_id,run_row_id,mutation_identity,expected_run_version,status,member_count,total_bytes,created_at,admitted_at`
const qaPacketMemberColumns = `id,qa_packet_member_id,qa_packet_row_id,sequence,member_kind,artifact_row_id,sha256,media_type,size_bytes,created_at`
const qaEvidenceColumns = `id,qa_evidence_id,qa_packet_row_id,sequence,semantic_role,artifact_row_id,sha256,media_type,size_bytes,created_at`
const qaAdmissionColumns = `id,qa_admission_id,qa_packet_row_id,mutation_identity,operator_confirmation_evidence,admitted_member_count,admitted_total_bytes,created_at`

func scanPrototypeCleanupReconciliation(r rowScanner) (v PrototypeCleanupReconciliation, err error) {
	err = r.Scan(&v.ID, &v.ReconciliationID, &v.RunRowID, &v.MutationIdentity, &v.TriggerKind, &v.ExpectedRunVersion, &v.ObservedRunState, &v.ProcessOwnershipStatus, &v.EvidenceSettlementStatus, &v.WorktreeStatus, &v.EphemeralTargetStatus, &v.PrototypeLeaseStatus, &v.ResultingRunState, &v.Diagnostic, &v.CreatedAt)
	return
}
func scanPrototypeQAPacket(r rowScanner) (v PrototypeQAPacket, err error) {
	err = r.Scan(&v.ID, &v.QAPacketID, &v.WorkspaceRowID, &v.RunRowID, &v.MutationIdentity, &v.ExpectedRunVersion, &v.Status, &v.MemberCount, &v.TotalBytes, &v.CreatedAt, &v.AdmittedAt)
	return
}
func scanPrototypeQAPacketMember(r rowScanner) (v PrototypeQAPacketMember, err error) {
	err = r.Scan(&v.ID, &v.QAPacketMemberID, &v.QAPacketRowID, &v.Sequence, &v.MemberKind, &v.ArtifactRowID, &v.SHA256, &v.MediaType, &v.SizeBytes, &v.CreatedAt)
	return
}
func scanPrototypeQAEvidence(r rowScanner) (v PrototypeQAEvidence, err error) {
	err = r.Scan(&v.ID, &v.QAEvidenceID, &v.QAPacketRowID, &v.Sequence, &v.SemanticRole, &v.ArtifactRowID, &v.SHA256, &v.MediaType, &v.SizeBytes, &v.CreatedAt)
	return
}
func scanPrototypeQAAdmission(r rowScanner) (v PrototypeQAAdmission, err error) {
	err = r.Scan(&v.ID, &v.QAAdmissionID, &v.QAPacketRowID, &v.MutationIdentity, &v.OperatorConfirmationEvidence, &v.AdmittedMemberCount, &v.AdmittedTotalBytes, &v.CreatedAt)
	return
}

func (tx *Tx) CreatePrototypeCleanupReconciliation(ctx context.Context, v PrototypeCleanupReconciliation) (PrototypeCleanupReconciliation, error) {
	if v.ReconciliationID == "" {
		v.ReconciliationID = NewPrototypeReconciliationID()
	}
	return scanPrototypeCleanupReconciliation(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_cleanup_reconciliations (reconciliation_id,run_row_id,mutation_identity,trigger_kind,expected_run_version,observed_run_state,process_ownership_status,evidence_settlement_status,worktree_status,ephemeral_target_status,prototype_lease_status,resulting_run_state,diagnostic) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING `+cleanupReconciliationColumns, v.ReconciliationID, v.RunRowID, v.MutationIdentity, v.TriggerKind, v.ExpectedRunVersion, v.ObservedRunState, v.ProcessOwnershipStatus, v.EvidenceSettlementStatus, v.WorktreeStatus, v.EphemeralTargetStatus, v.PrototypeLeaseStatus, v.ResultingRunState, v.Diagnostic))
}
func (s *Store) GetPrototypeCleanupReconciliation(ctx context.Context, id string) (PrototypeCleanupReconciliation, error) {
	return scanPrototypeCleanupReconciliation(s.db.QueryRowContext(ctx, `SELECT `+cleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations WHERE reconciliation_id=?`, id))
}
func (s *Store) GetPrototypeCleanupReconciliationByMutationIdentity(ctx context.Context, identity string) (PrototypeCleanupReconciliation, error) {
	return scanPrototypeCleanupReconciliation(s.db.QueryRowContext(ctx, `SELECT `+cleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations WHERE mutation_identity=?`, identity))
}
func (s *Store) ListPrototypeCleanupReconciliations(ctx context.Context, runID string) ([]PrototypeCleanupReconciliation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.reconciliation_id,c.run_row_id,c.mutation_identity,c.trigger_kind,c.expected_run_version,c.observed_run_state,c.process_ownership_status,c.evidence_settlement_status,c.worktree_status,c.ephemeral_target_status,c.prototype_lease_status,c.resulting_run_state,c.diagnostic,c.created_at FROM feature_workspace_prototype_cleanup_reconciliations c JOIN feature_workspace_prototype_runs r ON r.id=c.run_row_id WHERE r.prototype_run_id=? ORDER BY c.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeCleanupReconciliation
	for rows.Next() {
		v, err := scanPrototypeCleanupReconciliation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListPrototypeCleanupCandidates(ctx context.Context, limit int) ([]PrototypeRun, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("cleanup candidate limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+prototypeRunColumns+` FROM feature_workspace_prototype_runs r WHERE r.lifecycle_state IN ('succeeded','failed','cancelled','timed_out','launch_uncertain','cleanup_required') AND r.lifecycle_state<>'closed' ORDER BY r.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeRun
	for rows.Next() {
		v, err := scanPrototypeRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListPrototypeCleanupObligationsByRunID(ctx context.Context, runID string) ([]PrototypeCleanupObligation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.run_row_id,c.cleanup_obligation_id,c.obligation_kind,c.status,c.detail,c.created_at,c.updated_at FROM feature_workspace_prototype_cleanup_obligations c JOIN feature_workspace_prototype_runs r ON r.id=c.run_row_id WHERE r.prototype_run_id=? ORDER BY c.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeCleanupObligation
	for rows.Next() {
		var v PrototypeCleanupObligation
		if err := rows.Scan(&v.ID, &v.RunRowID, &v.CleanupObligationID, &v.ObligationKind, &v.Status, &v.Detail, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListPrototypeCleanupObligations is retained as a small compatibility alias.
func (s *Store) ListPrototypeCleanupObligations(ctx context.Context, runID string) ([]PrototypeCleanupObligation, error) {
	return s.ListPrototypeCleanupObligationsByRunID(ctx, runID)
}

func (tx *Tx) MarkPrototypeRuntimeSettled(ctx context.Context, runID string) (PrototypeRuntime, error) {
	_, err := tx.tx.ExecContext(ctx, `UPDATE feature_workspace_prototype_runtimes SET launch_phase='settled',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND process_identity IS NOT NULL`, runID)
	if err != nil {
		return PrototypeRuntime{}, err
	}
	return scanPrototypeRuntime(tx.tx.QueryRowContext(ctx, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?)`, runID))
}

func (tx *Tx) MarkPrototypeCleanupRequired(ctx context.Context, runID string, expectedVersion int64, identity, outcome string) (PrototypeRun, error) {
	run, err := getPrototypeRun(ctx, tx.tx, runID)
	if err != nil {
		return run, err
	}
	if run.LifecycleState == "cleanup_required" {
		return run, nil
	}
	if run.LifecycleState == "closed" || run.Version != expectedVersion || !oneOfPrototypeTerminalState(run.LifecycleState) {
		return run, sql.ErrNoRows
	}
	from := run.LifecycleState
	run, err = scanPrototypeRun(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='cleanup_required',process_outcome=COALESCE(NULLIF(?,''),process_outcome),cleanup_status='failed',version=version+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND version=? RETURNING `+prototypeRunColumns, outcome, run.ID, expectedVersion))
	if err != nil {
		return run, err
	}
	_, err = tx.tx.ExecContext(ctx, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, identity, from, "cleanup_required", run.Version)
	return run, err
}
func oneOfPrototypeTerminalState(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled" || state == "timed_out"
}

func (tx *Tx) ClosePrototypeRun(ctx context.Context, runID string, expectedVersion int64, transitionIdentity string) (PrototypeRun, error) {
	run, err := getPrototypeRun(ctx, tx.tx, runID)
	if err != nil {
		return run, err
	}
	if run.LifecycleState == "closed" {
		var count int
		err = tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_prototype_lifecycle_transitions WHERE run_row_id=? AND transition_identity=? AND to_state='closed'`, run.ID, transitionIdentity).Scan(&count)
		if err == nil && count == 1 {
			return run, nil
		}
		return run, prototypeExecutionStoreConflict("closed prototype run has a different transition identity")
	}
	if run.Version != expectedVersion || (!oneOfPrototypeTerminalState(run.LifecycleState) && run.LifecycleState != "cleanup_required") {
		return run, prototypeExecutionStoreConflict("prototype run is not closeable")
	}
	var incomplete int
	if err = tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_prototype_cleanup_obligations WHERE run_row_id=? AND obligation_kind IN ('process_ownership','evidence_settlement','worktree','ephemeral_target','prototype_lease') AND status<>'complete'`, run.ID).Scan(&incomplete); err != nil {
		return run, err
	}
	if incomplete != 0 {
		return run, prototypeExecutionStoreConflict("prototype cleanup obligations are incomplete")
	}
	var targetStatus, leaseStatus, launchPhase string
	if err = tx.tx.QueryRowContext(ctx, `SELECT status FROM feature_workspace_prototype_targets WHERE run_row_id=?`, run.ID).Scan(&targetStatus); err != nil {
		return run, err
	}
	if err = tx.tx.QueryRowContext(ctx, `SELECT status FROM feature_workspace_prototype_leases WHERE run_row_id=?`, run.ID).Scan(&leaseStatus); err != nil {
		return run, err
	}
	if err = tx.tx.QueryRowContext(ctx, `SELECT launch_phase FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID).Scan(&launchPhase); err != nil {
		return run, err
	}
	if targetStatus != "released" || leaseStatus != "released" || launchPhase != "settled" {
		return run, prototypeExecutionStoreConflict("prototype resources are not settled")
	}
	from := run.LifecycleState
	run, err = scanPrototypeRun(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='closed',version=version+1,cleanup_status='complete',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND version=? RETURNING `+prototypeRunColumns, run.ID, expectedVersion))
	if err != nil {
		return run, err
	}
	_, err = tx.tx.ExecContext(ctx, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, transitionIdentity, from, "closed", run.Version)
	return run, err
}
func prototypeExecutionStoreConflict(detail string) error {
	return fmt.Errorf("%w: %s", ErrPrototypeCleanupConflict, detail)
}

func (tx *Tx) CreatePrototypeQAPacket(ctx context.Context, v PrototypeQAPacket) (PrototypeQAPacket, error) {
	if v.QAPacketID == "" {
		v.QAPacketID = NewPrototypeQAPacketID()
	}
	return scanPrototypeQAPacket(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_qa_packets (qa_packet_id,workspace_row_id,run_row_id,mutation_identity,expected_run_version,status,member_count,total_bytes) VALUES (?,?,?,?,?,'prepared',?,?) RETURNING `+qaPacketColumns, v.QAPacketID, v.WorkspaceRowID, v.RunRowID, v.MutationIdentity, v.ExpectedRunVersion, v.MemberCount, v.TotalBytes))
}
func (tx *Tx) CreatePrototypeQAPacketMember(ctx context.Context, v PrototypeQAPacketMember) (PrototypeQAPacketMember, error) {
	if v.QAPacketMemberID == "" {
		v.QAPacketMemberID = NewPrototypeQAPacketMemberID()
	}
	return scanPrototypeQAPacketMember(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_qa_packet_members (qa_packet_member_id,qa_packet_row_id,sequence,member_kind,artifact_row_id,sha256,media_type,size_bytes) VALUES (?,?,?,?,?,?,?,?) RETURNING `+qaPacketMemberColumns, v.QAPacketMemberID, v.QAPacketRowID, v.Sequence, v.MemberKind, v.ArtifactRowID, v.SHA256, v.MediaType, v.SizeBytes))
}
func (tx *Tx) CreatePrototypeQAEvidence(ctx context.Context, v PrototypeQAEvidence) (PrototypeQAEvidence, error) {
	if v.QAEvidenceID == "" {
		v.QAEvidenceID = NewPrototypeQAEvidenceID()
	}
	return scanPrototypeQAEvidence(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_qa_evidence (qa_evidence_id,qa_packet_row_id,sequence,semantic_role,artifact_row_id,sha256,media_type,size_bytes) VALUES (?,?,?,?,?,?,?,?) RETURNING `+qaEvidenceColumns, v.QAEvidenceID, v.QAPacketRowID, v.Sequence, v.SemanticRole, v.ArtifactRowID, v.SHA256, v.MediaType, v.SizeBytes))
}
func (tx *Tx) CreatePrototypeQAAdmission(ctx context.Context, v PrototypeQAAdmission) (PrototypeQAAdmission, error) {
	if v.QAAdmissionID == "" {
		v.QAAdmissionID = NewPrototypeQAAdmissionID()
	}
	return scanPrototypeQAAdmission(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_qa_admissions (qa_admission_id,qa_packet_row_id,mutation_identity,operator_confirmation_evidence,admitted_member_count,admitted_total_bytes) VALUES (?,?,?,?,?,?) RETURNING `+qaAdmissionColumns, v.QAAdmissionID, v.QAPacketRowID, v.MutationIdentity, v.OperatorConfirmationEvidence, v.AdmittedMemberCount, v.AdmittedTotalBytes))
}
func (tx *Tx) MarkPrototypeQAPacketAdmitted(ctx context.Context, packetID, _ string, admittedAt string) (PrototypeQAPacket, error) {
	return scanPrototypeQAPacket(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_prototype_qa_packets SET status='admitted',admitted_at=? WHERE qa_packet_id=? AND status='prepared' RETURNING `+qaPacketColumns, admittedAt, packetID))
}

func (s *Store) GetPrototypeQAPacketByPacketID(ctx context.Context, id string) (PrototypeQAPacket, error) {
	return scanPrototypeQAPacket(s.db.QueryRowContext(ctx, `SELECT `+qaPacketColumns+` FROM feature_workspace_prototype_qa_packets WHERE qa_packet_id=?`, id))
}
func (s *Store) GetPrototypeQAPacketByMutationIdentity(ctx context.Context, identity string) (PrototypeQAPacket, error) {
	return scanPrototypeQAPacket(s.db.QueryRowContext(ctx, `SELECT `+qaPacketColumns+` FROM feature_workspace_prototype_qa_packets WHERE mutation_identity=?`, identity))
}
func (s *Store) ListPrototypeQAPacketsByRunID(ctx context.Context, runID string) ([]PrototypeQAPacket, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.qa_packet_id,p.workspace_row_id,p.run_row_id,p.mutation_identity,p.expected_run_version,p.status,p.member_count,p.total_bytes,p.created_at,p.admitted_at FROM feature_workspace_prototype_qa_packets p JOIN feature_workspace_prototype_runs r ON r.id=p.run_row_id WHERE r.prototype_run_id=? ORDER BY p.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeQAPacket
	for rows.Next() {
		v, err := scanPrototypeQAPacket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListPrototypeQAPacketMembers(ctx context.Context, packetID string) ([]PrototypeQAPacketMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.qa_packet_member_id,m.qa_packet_row_id,m.sequence,m.member_kind,m.artifact_row_id,m.sha256,m.media_type,m.size_bytes,m.created_at FROM feature_workspace_prototype_qa_packet_members m JOIN feature_workspace_prototype_qa_packets p ON p.id=m.qa_packet_row_id WHERE p.qa_packet_id=? ORDER BY m.sequence`, packetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeQAPacketMember
	for rows.Next() {
		v, err := scanPrototypeQAPacketMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListPrototypeQAEvidenceByPacketID(ctx context.Context, packetID string) ([]PrototypeQAEvidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.qa_evidence_id,e.qa_packet_row_id,e.sequence,e.semantic_role,e.artifact_row_id,e.sha256,e.media_type,e.size_bytes,e.created_at FROM feature_workspace_prototype_qa_evidence e JOIN feature_workspace_prototype_qa_packets p ON p.id=e.qa_packet_row_id WHERE p.qa_packet_id=? ORDER BY e.sequence`, packetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeQAEvidence
	for rows.Next() {
		v, err := scanPrototypeQAEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetPrototypeQAAdmissionByPacketID(ctx context.Context, packetID string) (PrototypeQAAdmission, error) {
	return scanPrototypeQAAdmission(s.db.QueryRowContext(ctx, `SELECT a.id,a.qa_admission_id,a.qa_packet_row_id,a.mutation_identity,a.operator_confirmation_evidence,a.admitted_member_count,a.admitted_total_bytes,a.created_at FROM feature_workspace_prototype_qa_admissions a JOIN feature_workspace_prototype_qa_packets p ON p.id=a.qa_packet_row_id WHERE p.qa_packet_id=?`, packetID))
}
func (s *Store) GetPrototypeQAAdmissionByMutationIdentity(ctx context.Context, identity string) (PrototypeQAAdmission, error) {
	return scanPrototypeQAAdmission(s.db.QueryRowContext(ctx, `SELECT `+qaAdmissionColumns+` FROM feature_workspace_prototype_qa_admissions WHERE mutation_identity=?`, identity))
}

func (s *Store) GetPrototypeRunByApprovalMutationIdentity(ctx context.Context, identity string) (PrototypeRun, error) {
	return scanPrototypeRun(s.db.QueryRowContext(ctx, `SELECT r.id,r.prototype_run_id,r.authorization_row_id,r.workspace_row_id,r.work_item_row_id,r.lifecycle_state,r.version,r.process_outcome,r.cleanup_status,r.launch_uncertainty_reason,r.external_process_identity,r.created_at,r.updated_at FROM feature_workspace_prototype_runs r JOIN feature_workspace_prototype_approvals a ON a.run_row_id=r.id WHERE a.mutation_identity=?`, identity))
}

// Keep errors.Is behavior stable for callers that previously received a
// database no-rows value from the close path.
var _ = errors.Is

func (s *Store) ClosePrototypeRun(ctx context.Context, runID string, expectedVersion int64, transitionIdentity string) (PrototypeRun, error) {
	var run PrototypeRun
	err := s.WithTx(ctx, func(tx *Tx) error {
		var err error
		run, err = tx.ClosePrototypeRun(ctx, runID, expectedVersion, transitionIdentity)
		return err
	})
	return run, err
}
