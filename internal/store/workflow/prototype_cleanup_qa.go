package workflowstore

import (
	"context"
	"database/sql"
	"fmt"
)

type PrototypeCleanupReconciliation struct {
	ID, RunRowID, ExpectedRunVersion                                                      int64
	ReconciliationID, MutationIdentity, TriggerKind, Status, Detail, CreatedAt, UpdatedAt string
	ClosedAt                                                                              sql.NullString
}

type PrototypeQAPacketMember struct {
	ID, ReconciliationRowID, RunRowID, Sequence   int64
	PacketMemberID, MemberKind, SHA256, CreatedAt string
	ArtifactRowID                                 sql.NullInt64
}

type PrototypeQAAdmission struct {
	ID, ReconciliationRowID, RunRowID, PacketMemberRowID    int64
	AdmissionID, AdmissionKind, Decision, Reason, CreatedAt string
}

func scanPrototypeCleanupReconciliation(r rowScanner) (v PrototypeCleanupReconciliation, err error) {
	err = r.Scan(&v.ID, &v.ReconciliationID, &v.RunRowID, &v.ExpectedRunVersion, &v.MutationIdentity, &v.TriggerKind, &v.Status, &v.Detail, &v.CreatedAt, &v.UpdatedAt, &v.ClosedAt)
	return
}
func scanPrototypeQAPacketMember(r rowScanner) (v PrototypeQAPacketMember, err error) {
	err = r.Scan(&v.ID, &v.PacketMemberID, &v.ReconciliationRowID, &v.RunRowID, &v.Sequence, &v.MemberKind, &v.ArtifactRowID, &v.SHA256, &v.CreatedAt)
	return
}
func scanPrototypeQAAdmission(r rowScanner) (v PrototypeQAAdmission, err error) {
	err = r.Scan(&v.ID, &v.AdmissionID, &v.ReconciliationRowID, &v.RunRowID, &v.PacketMemberRowID, &v.AdmissionKind, &v.Decision, &v.Reason, &v.CreatedAt)
	return
}

const prototypeCleanupReconciliationColumns = "id,reconciliation_id,run_row_id,expected_run_version,mutation_identity,trigger_kind,status,detail,created_at,updated_at,closed_at"
const prototypeQAPacketMemberColumns = "id,packet_member_id,reconciliation_row_id,run_row_id,sequence,member_kind,artifact_row_id,sha256,created_at"
const prototypeQAAdmissionColumns = "id,admission_id,reconciliation_row_id,run_row_id,packet_member_row_id,admission_kind,decision,reason,created_at"

func (tx *Tx) CreatePrototypeCleanupReconciliation(ctx context.Context, v PrototypeCleanupReconciliation) (PrototypeCleanupReconciliation, error) {
	if v.ReconciliationID == "" {
		v.ReconciliationID = NewPrototypeReconciliationID()
	}
	return scanPrototypeCleanupReconciliation(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_cleanup_reconciliations(reconciliation_id,run_row_id,expected_run_version,mutation_identity,trigger_kind,detail) VALUES(?,?,?,?,?,?) RETURNING `+prototypeCleanupReconciliationColumns, v.ReconciliationID, v.RunRowID, v.ExpectedRunVersion, v.MutationIdentity, v.TriggerKind, v.Detail))
}
func (tx *Tx) GetPrototypeCleanupReconciliation(ctx context.Context, id string) (PrototypeCleanupReconciliation, error) {
	return scanPrototypeCleanupReconciliation(tx.tx.QueryRowContext(ctx, `SELECT `+prototypeCleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations WHERE reconciliation_id=?`, id))
}
func (s *Store) GetPrototypeCleanupReconciliation(ctx context.Context, id string) (PrototypeCleanupReconciliation, error) {
	return scanPrototypeCleanupReconciliation(s.db.QueryRowContext(ctx, `SELECT `+prototypeCleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations WHERE reconciliation_id=?`, id))
}
func (s *Store) ListPrototypeCleanupCandidates(ctx context.Context) ([]PrototypeCleanupReconciliation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prototypeCleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations WHERE status IN ('pending','in_progress','failed') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeCleanupReconciliation
	for rows.Next() {
		v, e := scanPrototypeCleanupReconciliation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (tx *Tx) ListPrototypeCleanupCandidates(ctx context.Context) ([]PrototypeCleanupReconciliation, error) {
	rows, err := tx.tx.QueryContext(ctx, `SELECT `+prototypeCleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations WHERE status IN ('pending','in_progress','failed') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeCleanupReconciliation
	for rows.Next() {
		v, e := scanPrototypeCleanupReconciliation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (tx *Tx) ClosePrototypeRun(ctx context.Context, runID string, expectedVersion int64, identity string) (PrototypeRun, error) {
	run, err := getPrototypeRun(ctx, tx.tx, runID)
	if err != nil {
		return run, err
	}
	if run.Version != expectedVersion || run.LifecycleState == "closed" {
		return run, sql.ErrNoRows
	}
	if run.LifecycleState != "succeeded" && run.LifecycleState != "failed" && run.LifecycleState != "cancelled" && run.LifecycleState != "timed_out" && run.LifecycleState != "cleanup_required" {
		return run, fmt.Errorf("prototype run is not closeable")
	}
	from := run.LifecycleState
	run, err = scanPrototypeRun(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='closed',version=version+1,cleanup_status='complete',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND version=? RETURNING `+prototypeRunColumns, run.ID, expectedVersion))
	if err != nil {
		return run, err
	}
	_, err = tx.tx.ExecContext(ctx, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, identity, from, "closed", run.Version)
	return run, err
}
func (tx *Tx) ClosePrototypeCleanupReconciliation(ctx context.Context, id string, detail string) (PrototypeCleanupReconciliation, error) {
	return scanPrototypeCleanupReconciliation(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_prototype_cleanup_reconciliations SET status='closed',detail=?,closed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE reconciliation_id=? AND status<>'closed' RETURNING `+prototypeCleanupReconciliationColumns, detail, id))
}

func (tx *Tx) CreatePrototypeQAPacketMember(ctx context.Context, v PrototypeQAPacketMember) (PrototypeQAPacketMember, error) {
	if v.PacketMemberID == "" {
		v.PacketMemberID = NewPrototypeQAPacketMemberID()
	}
	return scanPrototypeQAPacketMember(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_qa_packet_members(packet_member_id,reconciliation_row_id,run_row_id,sequence,member_kind,artifact_row_id,sha256) VALUES(?,?,?,?,?,?,?) RETURNING `+prototypeQAPacketMemberColumns, v.PacketMemberID, v.ReconciliationRowID, v.RunRowID, v.Sequence, v.MemberKind, v.ArtifactRowID, v.SHA256))
}
func (tx *Tx) CreatePrototypeQAAdmission(ctx context.Context, v PrototypeQAAdmission) (PrototypeQAAdmission, error) {
	if v.AdmissionID == "" {
		v.AdmissionID = NewPrototypeQAAdmissionID()
	}
	return scanPrototypeQAAdmission(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_qa_admissions(admission_id,reconciliation_row_id,run_row_id,packet_member_row_id,admission_kind,decision,reason) VALUES(?,?,?,?,?,?,?) RETURNING `+prototypeQAAdmissionColumns, v.AdmissionID, v.ReconciliationRowID, v.RunRowID, nullableInt64(v.PacketMemberRowID), v.AdmissionKind, v.Decision, v.Reason))
}
func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (s *Store) ListPrototypeQAPacketMembers(ctx context.Context, reconciliationID string) ([]PrototypeQAPacketMember, error) {
	return listQAPacketMembers(ctx, s.db, reconciliationID)
}
func (s *Store) ListPrototypeQAAdmissions(ctx context.Context, reconciliationID string) ([]PrototypeQAAdmission, error) {
	return listQAAdmissions(ctx, s.db, reconciliationID)
}
func (tx *Tx) ListPrototypeQAPacketMembers(ctx context.Context, reconciliationID string) ([]PrototypeQAPacketMember, error) {
	return listQAPacketMembers(ctx, tx.tx, reconciliationID)
}
func (tx *Tx) ListPrototypeQAAdmissions(ctx context.Context, reconciliationID string) ([]PrototypeQAAdmission, error) {
	return listQAAdmissions(ctx, tx.tx, reconciliationID)
}
func listQAPacketMembers(ctx context.Context, q rowsQueryer, id string) ([]PrototypeQAPacketMember, error) {
	rows, e := q.QueryContext(ctx, `SELECT `+prototypeQAPacketMemberColumns+` FROM feature_workspace_prototype_qa_packet_members m JOIN feature_workspace_prototype_cleanup_reconciliations c ON c.id=m.reconciliation_row_id WHERE c.reconciliation_id=? ORDER BY m.sequence`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []PrototypeQAPacketMember
	for rows.Next() {
		v, e := scanPrototypeQAPacketMember(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func listQAAdmissions(ctx context.Context, q rowsQueryer, id string) ([]PrototypeQAAdmission, error) {
	rows, e := q.QueryContext(ctx, `SELECT `+prototypeQAAdmissionColumns+` FROM feature_workspace_prototype_qa_admissions a JOIN feature_workspace_prototype_cleanup_reconciliations c ON c.id=a.reconciliation_row_id WHERE c.reconciliation_id=? ORDER BY a.id`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []PrototypeQAAdmission
	for rows.Next() {
		v, e := scanPrototypeQAAdmission(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListPrototypeCleanupObligations(ctx context.Context, runID string) ([]PrototypeCleanupObligation, error) {
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
func (tx *Tx) ListPrototypeCleanupObligations(ctx context.Context, runRowID int64) ([]PrototypeCleanupObligation, error) {
	rows, err := tx.tx.QueryContext(ctx, `SELECT id,run_row_id,cleanup_obligation_id,obligation_kind,status,detail,created_at,updated_at FROM feature_workspace_prototype_cleanup_obligations WHERE run_row_id=? ORDER BY id`, runRowID)
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

func (s *Store) ClosePrototypeRun(ctx context.Context, runID string, expectedVersion int64, identity string) (PrototypeRun, error) {
	var out PrototypeRun
	err := s.WithTx(ctx, func(tx *Tx) error {
		var err error
		out, err = tx.ClosePrototypeRun(ctx, runID, expectedVersion, identity)
		return err
	})
	return out, err
}
func (s *Store) ListPrototypeCleanupReconciliations(ctx context.Context, runID string) ([]PrototypeCleanupReconciliation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prototypeCleanupReconciliationColumns+` FROM feature_workspace_prototype_cleanup_reconciliations c JOIN feature_workspace_prototype_runs r ON r.id=c.run_row_id WHERE r.prototype_run_id=? ORDER BY c.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeCleanupReconciliation
	for rows.Next() {
		v, e := scanPrototypeCleanupReconciliation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListPrototypeQAPacketMembersByRun(ctx context.Context, runID string) ([]PrototypeQAPacketMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prototypeQAPacketMemberColumns+` FROM feature_workspace_prototype_qa_packet_members m JOIN feature_workspace_prototype_runs r ON r.id=m.run_row_id WHERE r.prototype_run_id=? ORDER BY m.sequence`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeQAPacketMember
	for rows.Next() {
		v, e := scanPrototypeQAPacketMember(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListPrototypeQAAdmissionsByRun(ctx context.Context, runID string) ([]PrototypeQAAdmission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prototypeQAAdmissionColumns+` FROM feature_workspace_prototype_qa_admissions a JOIN feature_workspace_prototype_runs r ON r.id=a.run_row_id WHERE r.prototype_run_id=? ORDER BY a.id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrototypeQAAdmission
	for rows.Next() {
		v, e := scanPrototypeQAAdmission(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetPrototypeQAPacketMember(ctx context.Context, id string) (PrototypeQAPacketMember, error) {
	return scanPrototypeQAPacketMember(s.db.QueryRowContext(ctx, `SELECT `+prototypeQAPacketMemberColumns+` FROM feature_workspace_prototype_qa_packet_members WHERE packet_member_id=?`, id))
}
func (s *Store) GetPrototypeQAAdmission(ctx context.Context, id string) (PrototypeQAAdmission, error) {
	return scanPrototypeQAAdmission(s.db.QueryRowContext(ctx, `SELECT `+prototypeQAAdmissionColumns+` FROM feature_workspace_prototype_qa_admissions WHERE admission_id=?`, id))
}
