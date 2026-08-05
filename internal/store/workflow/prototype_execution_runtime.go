package workflowstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

var (
	ErrPrototypePreparationClaimed   = errors.New("prototype preparation already claimed")
	ErrPrototypeLaunchAlreadyClaimed = errors.New("prototype launch already claimed")
	ErrPrototypeEvidenceUnsafe       = errors.New("prototype evidence is unsafe")
	ErrPrototypeCleanupConflict      = errors.New("prototype cleanup obligation transition conflicts")
	ErrPrototypeMutationConflict     = errors.New("prototype mutation identity conflicts")
)

type PrototypeRuntime struct {
	ID, RunRowID                                                  int64
	RuntimeID, AuthorizedCommit, AuthorizedTree                   string
	RuntimeRootPath, WorktreePath, EphemeralTargetKey, LeaseToken string
	BackgroundContextID, InvocationRelativePath                   string
	ResultRelativePath, ExportRelativePath                        string
	PreparationPhase, LaunchPhase                                 string
	ProcessIdentity, ProcessStartedAt                             sql.NullString
	DeadlineAt                                                    string
	CancelIdentity, CancelRequestedAt                             sql.NullString
	TimeoutIdentity, TimeoutClaimedAt                             sql.NullString
	PreparationError, CreatedAt, UpdatedAt                        string
}
type PrototypeTarget struct {
	ID, RunRowID, RuntimeRowID               int64
	TargetID, TargetKey, WorktreePath        string
	AuthorizedCommit, AuthorizedTree, Status string
	CreatedAt, UpdatedAt                     string
	ReleasedAt                               sql.NullString
}
type PrototypeLease struct {
	ID, RunRowID, RuntimeRowID                              int64
	LeaseToken, EphemeralTargetKey, OwnerInstanceID, Status string
	AcquiredAt                                              string
	ReleasedAt                                              sql.NullString
	CreatedAt, UpdatedAt                                    string
}
type PrototypeEvidenceImportBatch struct {
	ID, RunRowID, RuntimeRowID, ArtifactCount, TotalSizeBytes            int64
	EvidenceBatchID, BatchIdentity, SettlementCause, ObservationIdentity string
	ProcessOutcome, EnvelopeStatus, Completeness, CreatedAt              string
}
type PrototypeResult struct {
	ID, RunRowID, RuntimeRowID, EvidenceBatchRowID int64
	ArtifactRowID                                  sql.NullInt64
	ResultID, ValidationStatus, ProcessOutcome     string
	ProcessExitCode                                sql.NullInt64
	EnvelopeSHA256                                 sql.NullString
	ValidationError, CreatedAt                     string
}
type PrototypeEvidenceMember struct {
	ID, RunRowID, EvidenceBatchRowID, Sequence, ArtifactRowID, SizeBytes                     int64
	EvidenceMemberID, SemanticRole, RelativePath, SHA256, MediaType, Completeness, CreatedAt string
}
type PrototypeResultMember struct {
	ID, RunRowID, Sequence, ArtifactRowID         int64
	ResultMemberID, MemberKind, SHA256, CreatedAt string
}
type PrototypeCleanupObligation struct {
	ID, RunRowID                                                              int64
	CleanupObligationID, ObligationKind, Status, Detail, CreatedAt, UpdatedAt string
}

const runtimeCols = `id,run_row_id,runtime_id,authorized_commit,authorized_tree,runtime_root_path,worktree_path,ephemeral_target_key,lease_token,background_context_id,invocation_relative_path,result_relative_path,export_relative_path,preparation_phase,launch_phase,process_identity,process_started_at,deadline_at,cancel_identity,cancel_requested_at,timeout_identity,timeout_claimed_at,preparation_error,created_at,updated_at`
const targetCols = `id,run_row_id,runtime_row_id,target_id,target_key,worktree_path,authorized_commit,authorized_tree,status,created_at,updated_at,released_at`
const leaseCols = `id,run_row_id,runtime_row_id,lease_token,ephemeral_target_key,owner_instance_id,status,acquired_at,released_at,created_at,updated_at`

func scanPrototypeRuntime(r rowScanner) (v PrototypeRuntime, e error) {
	e = r.Scan(&v.ID, &v.RunRowID, &v.RuntimeID, &v.AuthorizedCommit, &v.AuthorizedTree, &v.RuntimeRootPath, &v.WorktreePath, &v.EphemeralTargetKey, &v.LeaseToken, &v.BackgroundContextID, &v.InvocationRelativePath, &v.ResultRelativePath, &v.ExportRelativePath, &v.PreparationPhase, &v.LaunchPhase, &v.ProcessIdentity, &v.ProcessStartedAt, &v.DeadlineAt, &v.CancelIdentity, &v.CancelRequestedAt, &v.TimeoutIdentity, &v.TimeoutClaimedAt, &v.PreparationError, &v.CreatedAt, &v.UpdatedAt)
	return
}
func scanPrototypeTarget(r rowScanner) (v PrototypeTarget, e error) {
	e = r.Scan(&v.ID, &v.RunRowID, &v.RuntimeRowID, &v.TargetID, &v.TargetKey, &v.WorktreePath, &v.AuthorizedCommit, &v.AuthorizedTree, &v.Status, &v.CreatedAt, &v.UpdatedAt, &v.ReleasedAt)
	return
}
func scanPrototypeLease(r rowScanner) (v PrototypeLease, e error) {
	e = r.Scan(&v.ID, &v.RunRowID, &v.RuntimeRowID, &v.LeaseToken, &v.EphemeralTargetKey, &v.OwnerInstanceID, &v.Status, &v.AcquiredAt, &v.ReleasedAt, &v.CreatedAt, &v.UpdatedAt)
	return
}
func scanPrototypeBatch(r rowScanner) (v PrototypeEvidenceImportBatch, e error) {
	e = r.Scan(&v.ID, &v.RunRowID, &v.RuntimeRowID, &v.EvidenceBatchID, &v.BatchIdentity, &v.SettlementCause, &v.ObservationIdentity, &v.ProcessOutcome, &v.EnvelopeStatus, &v.Completeness, &v.ArtifactCount, &v.TotalSizeBytes, &v.CreatedAt)
	return
}
func scanPrototypeResult(r rowScanner) (v PrototypeResult, e error) {
	e = r.Scan(&v.ID, &v.ResultID, &v.RunRowID, &v.RuntimeRowID, &v.EvidenceBatchRowID, &v.ArtifactRowID, &v.ValidationStatus, &v.ProcessExitCode, &v.ProcessOutcome, &v.EnvelopeSHA256, &v.ValidationError, &v.CreatedAt)
	return
}
func scanPrototypeMember(r rowScanner) (v PrototypeEvidenceMember, e error) {
	e = r.Scan(&v.ID, &v.EvidenceMemberID, &v.RunRowID, &v.EvidenceBatchRowID, &v.Sequence, &v.SemanticRole, &v.RelativePath, &v.ArtifactRowID, &v.SHA256, &v.SizeBytes, &v.MediaType, &v.Completeness, &v.CreatedAt)
	return
}

func (s *Store) GetPrototypeRuntimeByRunID(c context.Context, id string) (PrototypeRuntime, error) {
	return scanPrototypeRuntime(s.db.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes r JOIN feature_workspace_prototype_runs p ON p.id=r.run_row_id WHERE p.prototype_run_id=?`, id))
}
func (s *Store) GetPrototypeTargetByRunID(c context.Context, id string) (PrototypeTarget, error) {
	return scanPrototypeTarget(s.db.QueryRowContext(c, `SELECT `+targetCols+` FROM feature_workspace_prototype_targets t JOIN feature_workspace_prototype_runs p ON p.id=t.run_row_id WHERE p.prototype_run_id=?`, id))
}
func (s *Store) GetPrototypeLeaseByRunID(c context.Context, id string) (PrototypeLease, error) {
	return scanPrototypeLease(s.db.QueryRowContext(c, `SELECT `+leaseCols+` FROM feature_workspace_prototype_leases l JOIN feature_workspace_prototype_runs p ON p.id=l.run_row_id WHERE p.prototype_run_id=?`, id))
}
func (s *Store) ListPrototypeEvidenceBatches(c context.Context, id string) (out []PrototypeEvidenceImportBatch, e error) {
	rows, e := s.db.QueryContext(c, `SELECT b.id,b.run_row_id,b.runtime_row_id,b.evidence_batch_id,b.batch_identity,b.settlement_cause,b.observation_identity,b.process_outcome,b.envelope_status,b.completeness,b.artifact_count,b.total_size_bytes,b.created_at FROM feature_workspace_prototype_evidence_import_batches b JOIN feature_workspace_prototype_runs r ON r.id=b.run_row_id WHERE r.prototype_run_id=? ORDER BY b.id`, id)
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		v, x := scanPrototypeBatch(rows)
		if x != nil {
			return nil, x
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GetPrototypeResultByRunID(c context.Context, id string) (PrototypeResult, error) {
	return scanPrototypeResult(s.db.QueryRowContext(c, `SELECT x.id,x.result_id,x.run_row_id,x.runtime_row_id,x.evidence_batch_row_id,x.artifact_row_id,x.validation_status,x.process_exit_code,x.process_outcome,x.envelope_sha256,x.validation_error,x.created_at FROM feature_workspace_prototype_results x JOIN feature_workspace_prototype_runs r ON r.id=x.run_row_id WHERE r.prototype_run_id=?`, id))
}
func (s *Store) ListPrototypeEvidenceMembers(c context.Context, id string) (out []PrototypeEvidenceMember, e error) {
	rows, e := s.db.QueryContext(c, `SELECT m.id,m.evidence_member_id,m.run_row_id,m.evidence_batch_row_id,m.sequence,m.semantic_role,m.relative_path,m.artifact_row_id,m.sha256,m.size_bytes,m.media_type,m.completeness,m.created_at FROM feature_workspace_prototype_evidence_members m JOIN feature_workspace_prototype_runs r ON r.id=m.run_row_id WHERE r.prototype_run_id=? ORDER BY m.sequence`, id)
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		v, x := scanPrototypeMember(rows)
		if x != nil {
			return nil, x
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListPrototypeResultMembers(c context.Context, id string) (out []PrototypeResultMember, e error) {
	rows, e := s.db.QueryContext(c, `SELECT m.id,m.run_row_id,m.sequence,m.artifact_row_id,m.member_kind,COALESCE(m.sha256,''),m.created_at FROM feature_workspace_prototype_result_members m JOIN feature_workspace_prototype_runs r ON r.id=m.run_row_id WHERE r.prototype_run_id=? ORDER BY m.sequence`, id)
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v PrototypeResultMember
		if x := rows.Scan(&v.ID, &v.RunRowID, &v.Sequence, &v.ArtifactRowID, &v.MemberKind, &v.SHA256, &v.CreatedAt); x != nil {
			return nil, x
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (tx *Tx) ReservePrototypeRuntime(c context.Context, runID string, expected int64, r PrototypeRuntime, t PrototypeTarget, l PrototypeLease) (PrototypeRun, PrototypeRuntime, PrototypeTarget, PrototypeLease, error) {
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, r, t, l, e
	}
	if run.LifecycleState != "approved" || run.Version != expected {
		return run, r, t, l, ErrPrototypePreparationClaimed
	}
	if existing, existingErr := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID)); existingErr == nil {
		if existing.RuntimeID != r.RuntimeID || existing.AuthorizedCommit != r.AuthorizedCommit || existing.AuthorizedTree != r.AuthorizedTree || existing.RuntimeRootPath != r.RuntimeRootPath || existing.WorktreePath != r.WorktreePath || existing.EphemeralTargetKey != r.EphemeralTargetKey || existing.LeaseToken != r.LeaseToken || existing.DeadlineAt != r.DeadlineAt {
			return run, existing, t, l, ErrPrototypePreparationClaimed
		}
		r = existing
		var targetErr error
		t, targetErr = scanPrototypeTarget(tx.tx.QueryRowContext(c, `SELECT `+targetCols+` FROM feature_workspace_prototype_targets WHERE run_row_id=?`, run.ID))
		if targetErr != nil {
			return run, r, t, l, targetErr
		}
		var leaseErr error
		l, leaseErr = scanPrototypeLease(tx.tx.QueryRowContext(c, `SELECT `+leaseCols+` FROM feature_workspace_prototype_leases WHERE run_row_id=?`, run.ID))
		return run, r, t, l, leaseErr
	} else if !errors.Is(existingErr, sql.ErrNoRows) {
		return run, r, t, l, existingErr
	}
	runtimeRow := `INSERT INTO feature_workspace_prototype_runtimes(runtime_id,run_row_id,authorized_commit,authorized_tree,runtime_root_path,worktree_path,ephemeral_target_key,lease_token,background_context_id,invocation_relative_path,result_relative_path,export_relative_path,deadline_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING ` + runtimeCols
	r, e = scanPrototypeRuntime(tx.tx.QueryRowContext(c, runtimeRow, r.RuntimeID, run.ID, r.AuthorizedCommit, r.AuthorizedTree, r.RuntimeRootPath, r.WorktreePath, r.EphemeralTargetKey, r.LeaseToken, r.BackgroundContextID, r.InvocationRelativePath, r.ResultRelativePath, r.ExportRelativePath, r.DeadlineAt))
	if e != nil {
		return run, r, t, l, e
	}
	_, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_targets(target_id,run_row_id,runtime_row_id,target_key,worktree_path,authorized_commit,authorized_tree) VALUES(?,?,?,?,?,?,?)`, t.TargetID, run.ID, r.ID, t.TargetKey, t.WorktreePath, t.AuthorizedCommit, t.AuthorizedTree)
	if e != nil {
		return run, r, t, l, e
	}
	t, e = scanPrototypeTarget(tx.tx.QueryRowContext(c, `SELECT `+targetCols+` FROM feature_workspace_prototype_targets WHERE run_row_id=?`, run.ID))
	if e != nil {
		return run, r, t, l, e
	}
	_, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_leases(lease_token,run_row_id,runtime_row_id,ephemeral_target_key,owner_instance_id) VALUES(?,?,?,?,?)`, l.LeaseToken, run.ID, r.ID, l.EphemeralTargetKey, l.OwnerInstanceID)
	if e != nil {
		return run, r, t, l, e
	}
	l, e = scanPrototypeLease(tx.tx.QueryRowContext(c, `SELECT `+leaseCols+` FROM feature_workspace_prototype_leases WHERE run_row_id=?`, run.ID))
	if e != nil {
		return run, r, t, l, e
	}
	for _, kind := range []string{"process_ownership", "evidence_settlement", "prototype_lease", "ephemeral_target", "worktree"} {
		if _, e = tx.GetOrCreatePrototypeCleanupObligation(c, run.ID, kind, ""); e != nil {
			return run, r, t, l, e
		}
	}
	return run, r, t, l, nil
}
func (tx *Tx) MarkPrototypeWorktreeReady(c context.Context, runID string) (PrototypeRuntime, error) {
	_, e := tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET preparation_phase='worktree_ready',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND preparation_phase='reserved'`, runID)
	if e != nil {
		return PrototypeRuntime{}, e
	}
	return scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?)`, runID))
}
func (tx *Tx) MarkPrototypePreparationReady(c context.Context, runID string, expected int64) (PrototypeRun, PrototypeRuntime, error) {
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	if run.LifecycleState != "approved" || run.Version != expected {
		return run, PrototypeRuntime{}, sql.ErrNoRows
	}
	if _, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET preparation_phase='preflight_ready',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=? AND preparation_phase IN ('reserved','worktree_ready')`, run.ID); e != nil {
		return run, PrototypeRuntime{}, e
	}
	rt, e := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	return run, rt, e
}
func (tx *Tx) MarkPrototypePreparationFailed(c context.Context, runID string, expected int64, detail string) (PrototypeRun, PrototypeRuntime, error) {
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	if run.Version != expected || (run.LifecycleState != "approved" && run.LifecycleState != "preparing") {
		return run, PrototypeRuntime{}, sql.ErrNoRows
	}
	from := run.LifecycleState
	if _, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET preparation_phase='failed',preparation_error=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=? AND preparation_phase<>'failed'`, detail, run.ID); e != nil {
		return run, PrototypeRuntime{}, e
	}
	run, e = scanPrototypeRun(tx.tx.QueryRowContext(c, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='failed',version=version+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND version=? RETURNING `+prototypeRunColumns, run.ID, expected))
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	identity := "preparation-failed:" + runID + ":" + strconv.FormatInt(run.Version, 10)
	if _, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, identity, from, "failed", run.Version); e != nil {
		return run, PrototypeRuntime{}, e
	}
	rt, e := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	return run, rt, e
}
func (tx *Tx) MarkPrototypeTargetReady(c context.Context, runID, key string) (PrototypeTarget, error) {
	_, e := tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_targets SET status='ready',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND target_key=? AND status='reserved'`, runID, key)
	if e != nil {
		return PrototypeTarget{}, e
	}
	return scanPrototypeTarget(tx.tx.QueryRowContext(c, `SELECT `+targetCols+` FROM feature_workspace_prototype_targets WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND target_key=?`, runID, key))
}
func (tx *Tx) CreatePrototypeEvidenceImportBatch(c context.Context, v PrototypeEvidenceImportBatch) (PrototypeEvidenceImportBatch, error) {
	return scanPrototypeBatch(tx.tx.QueryRowContext(c, `INSERT INTO feature_workspace_prototype_evidence_import_batches(evidence_batch_id,run_row_id,runtime_row_id,batch_identity,settlement_cause,observation_identity,process_outcome,envelope_status,completeness,artifact_count,total_size_bytes) VALUES(?,?,?,?,?,?,?,?,?,?,?) RETURNING id,run_row_id,runtime_row_id,evidence_batch_id,batch_identity,settlement_cause,observation_identity,process_outcome,envelope_status,completeness,artifact_count,total_size_bytes,created_at`, v.EvidenceBatchID, v.RunRowID, v.RuntimeRowID, v.BatchIdentity, v.SettlementCause, v.ObservationIdentity, v.ProcessOutcome, v.EnvelopeStatus, v.Completeness, v.ArtifactCount, v.TotalSizeBytes))
}
func (tx *Tx) CreatePrototypeResult(c context.Context, v PrototypeResult) (PrototypeResult, error) {
	return scanPrototypeResult(tx.tx.QueryRowContext(c, `INSERT INTO feature_workspace_prototype_results(result_id,run_row_id,runtime_row_id,evidence_batch_row_id,artifact_row_id,validation_status,process_exit_code,process_outcome,envelope_sha256,validation_error) VALUES(?,?,?,?,?,?,?,?,?,?) RETURNING id,result_id,run_row_id,runtime_row_id,evidence_batch_row_id,artifact_row_id,validation_status,process_exit_code,process_outcome,envelope_sha256,validation_error,created_at`, v.ResultID, v.RunRowID, v.RuntimeRowID, v.EvidenceBatchRowID, v.ArtifactRowID, v.ValidationStatus, v.ProcessExitCode, v.ProcessOutcome, v.EnvelopeSHA256, v.ValidationError))
}
func (tx *Tx) CreatePrototypeEvidenceMember(c context.Context, v PrototypeEvidenceMember) (PrototypeEvidenceMember, error) {
	return scanPrototypeMember(tx.tx.QueryRowContext(c, `INSERT INTO feature_workspace_prototype_evidence_members(evidence_member_id,run_row_id,evidence_batch_row_id,sequence,semantic_role,relative_path,artifact_row_id,sha256,size_bytes,media_type,completeness) VALUES(?,?,?,?,?,?,?,?,?,?,?) RETURNING id,evidence_member_id,run_row_id,evidence_batch_row_id,sequence,semantic_role,relative_path,artifact_row_id,sha256,size_bytes,media_type,completeness,created_at`, v.EvidenceMemberID, v.RunRowID, v.EvidenceBatchRowID, v.Sequence, v.SemanticRole, v.RelativePath, v.ArtifactRowID, v.SHA256, v.SizeBytes, v.MediaType, v.Completeness))
}
func (tx *Tx) CreatePrototypeResultMember(c context.Context, v PrototypeResultMember) (PrototypeResultMember, error) {
	var out PrototypeResultMember
	err := tx.tx.QueryRowContext(c, `INSERT INTO feature_workspace_prototype_result_members(run_row_id,sequence,member_kind,artifact_row_id,sha256) VALUES(?,?,?,?,?) RETURNING id,run_row_id,sequence,artifact_row_id,member_kind,COALESCE(sha256,''),created_at`, v.RunRowID, v.Sequence, v.MemberKind, v.ArtifactRowID, v.SHA256).Scan(&out.ID, &out.RunRowID, &out.Sequence, &out.ArtifactRowID, &out.MemberKind, &out.SHA256, &out.CreatedAt)
	if err == nil {
		out.ResultMemberID = v.ResultMemberID
		return out, nil
	}
	var existing PrototypeResultMember
	lookupErr := tx.tx.QueryRowContext(c, `SELECT id,run_row_id,sequence,artifact_row_id,member_kind,COALESCE(sha256,''),created_at FROM feature_workspace_prototype_result_members WHERE run_row_id=? AND sequence=?`, v.RunRowID, v.Sequence).Scan(&existing.ID, &existing.RunRowID, &existing.Sequence, &existing.ArtifactRowID, &existing.MemberKind, &existing.SHA256, &existing.CreatedAt)
	if lookupErr == nil {
		if existing.ArtifactRowID == v.ArtifactRowID && existing.MemberKind == v.MemberKind && existing.SHA256 == v.SHA256 {
			existing.ResultMemberID = v.ResultMemberID
			return existing, nil
		}
		return PrototypeResultMember{}, ErrPrototypeEvidenceUnsafe
	}
	return PrototypeResultMember{}, err
}

func (tx *Tx) ClaimPrototypeLaunch(c context.Context, runID string, expected int64, claimID, protocol string) (PrototypeRun, PrototypeLaunchClaim, error) {
	var claim PrototypeLaunchClaim
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, claim, e
	}
	runtime, runtimeErr := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	if runtimeErr != nil {
		return run, claim, runtimeErr
	}
	if run.Version != expected {
		return run, claim, ErrPrototypeLaunchAlreadyClaimed
	}
	existing, existingErr := scanPrototypeLaunchClaim(tx.tx.QueryRowContext(c, `SELECT id,launch_claim_id,run_row_id,launch_protocol,claimed_at FROM feature_workspace_prototype_launch_claims WHERE run_row_id=?`, run.ID))
	if existingErr == nil {
		if existing.LaunchClaimID == claimID && existing.LaunchProtocol == protocol && run.LifecycleState == "preparing" && runtime.LaunchPhase == "claimed" {
			return run, existing, nil
		}
		return run, claim, ErrPrototypeLaunchAlreadyClaimed
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return run, claim, existingErr
	}
	if run.LifecycleState != "approved" || runtime.PreparationPhase != "preflight_ready" || runtime.LaunchPhase != "not_claimed" {
		return run, claim, ErrPrototypePreparationClaimed
	}
	_, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_launch_claims(launch_claim_id,run_row_id,launch_protocol) VALUES(?,?,?)`, claimID, run.ID, protocol)
	if e != nil {
		return run, claim, e
	}
	run, e = scanPrototypeRun(tx.tx.QueryRowContext(c, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='preparing',cleanup_status='pending',version=version+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND lifecycle_state='approved' AND version=? RETURNING `+prototypeRunColumns, run.ID, expected))
	if e != nil {
		return run, claim, e
	}
	_, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET launch_phase='claimed',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=?`, run.ID)
	if e != nil {
		return run, claim, e
	}
	if _, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, claimID, "approved", "preparing", run.Version); e != nil {
		return run, claim, e
	}
	claim, e = scanPrototypeLaunchClaim(tx.tx.QueryRowContext(c, `SELECT id,launch_claim_id,run_row_id,launch_protocol,claimed_at FROM feature_workspace_prototype_launch_claims WHERE run_row_id=?`, run.ID))
	return run, claim, e
}
func (tx *Tx) PersistPrototypeProcessIdentity(c context.Context, runID string, expected int64, identity, started string) (PrototypeRun, PrototypeRuntime, error) {
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	if run.Version != expected || run.LifecycleState != "preparing" {
		return run, PrototypeRuntime{}, sql.ErrNoRows
	}
	rt, e := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	if e != nil {
		return run, rt, e
	}
	if rt.LaunchPhase != "claimed" || strings.TrimSpace(identity) == "" {
		return run, rt, sql.ErrNoRows
	}
	run, e = scanPrototypeRun(tx.tx.QueryRowContext(c, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='running',version=version+1,external_process_identity=? WHERE id=? AND lifecycle_state='preparing' AND version=? RETURNING `+prototypeRunColumns, identity, run.ID, expected))
	if e != nil {
		return run, rt, e
	}
	_, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET launch_phase='identity_persisted',process_identity=?,process_started_at=? WHERE run_row_id=?`, identity, started, run.ID)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	sum := sha256.Sum256([]byte(identity))
	_, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, "process-start:"+hex.EncodeToString(sum[:]), "preparing", "running", run.Version)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	rt, e = scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	return run, rt, e
}
func (tx *Tx) MarkPrototypeLaunchUncertain(c context.Context, runID string, expected int64, reason string) (PrototypeRun, error) {
	run, e := scanPrototypeRun(tx.tx.QueryRowContext(c, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='launch_uncertain',version=version+1,launch_uncertainty_reason=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE prototype_run_id=? AND lifecycle_state='preparing' AND version=? RETURNING `+prototypeRunColumns, reason, runID, expected))
	if e != nil {
		return run, e
	}
	var claimID string
	if e = tx.tx.QueryRowContext(c, `SELECT launch_claim_id FROM feature_workspace_prototype_launch_claims WHERE run_row_id=?`, run.ID).Scan(&claimID); e != nil {
		return run, e
	}
	_, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, "launch-uncertain:"+claimID, "preparing", "launch_uncertain", run.Version)
	return run, e
}
func (tx *Tx) RequestPrototypeCancellation(c context.Context, runID string, expected int64, id string) (PrototypeRun, PrototypeRuntime, error) {
	return tx.setMutation(c, runID, expected, "cancel_identity", "cancel_requested_at", id)
}
func (tx *Tx) ClaimPrototypeTimeout(c context.Context, runID string, expected int64, id string) (PrototypeRun, PrototypeRuntime, error) {
	return tx.setMutation(c, runID, expected, "timeout_identity", "timeout_claimed_at", id)
}
func (tx *Tx) setMutation(c context.Context, runID string, expected int64, col, at, id string) (PrototypeRun, PrototypeRuntime, error) {
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	rt, e := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	if e != nil {
		return run, rt, e
	}
	var existing sql.NullString
	if col == "cancel_identity" {
		existing = rt.CancelIdentity
	} else {
		existing = rt.TimeoutIdentity
	}
	if existing.Valid {
		if existing.String == id {
			return run, rt, nil
		}
		return run, rt, ErrPrototypeMutationConflict
	}
	if run.Version != expected {
		return run, rt, sql.ErrNoRows
	}
	_, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET `+col+`=?,`+at+`=strftime('%Y-%m-%dT%H:%M:%fZ') WHERE run_row_id=? AND `+col+` IS NULL`, id, run.ID)
	if e != nil {
		return run, rt, e
	}
	rt, e = scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	return run, rt, e
}
func (tx *Tx) SettlePrototypeProcess(c context.Context, runID string, expected int64, state, outcome, identity string) (PrototypeRun, PrototypeRuntime, error) {
	if state != "succeeded" && state != "failed" && state != "cancelled" && state != "timed_out" && state != "cleanup_required" {
		return PrototypeRun{}, PrototypeRuntime{}, sql.ErrNoRows
	}
	run, e := getPrototypeRun(c, tx.tx, runID)
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	if run.Version != expected || (run.LifecycleState != "running" && run.LifecycleState != "launch_uncertain") {
		return run, PrototypeRuntime{}, sql.ErrNoRows
	}
	from := run.LifecycleState
	run, e = scanPrototypeRun(tx.tx.QueryRowContext(c, `UPDATE feature_workspace_prototype_runs SET lifecycle_state=?,process_outcome=?,version=version+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=? AND version=? RETURNING `+prototypeRunColumns, state, outcome, run.ID, expected))
	if e != nil {
		return run, PrototypeRuntime{}, e
	}
	if _, e = tx.tx.ExecContext(c, `INSERT INTO feature_workspace_prototype_lifecycle_transitions(run_row_id,transition_identity,from_state,to_state,run_version) VALUES(?,?,?,?,?)`, run.ID, identity, from, state, run.Version); e != nil {
		return run, PrototypeRuntime{}, e
	}
	phase := "settled"
	if state == "cleanup_required" {
		phase = "ownership_unresolved"
	}
	if _, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_runtimes SET launch_phase=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=?`, phase, run.ID); e != nil {
		return run, PrototypeRuntime{}, e
	}
	rt, e := scanPrototypeRuntime(tx.tx.QueryRowContext(c, `SELECT `+runtimeCols+` FROM feature_workspace_prototype_runtimes WHERE run_row_id=?`, run.ID))
	return run, rt, e
}
func (tx *Tx) GetOrCreatePrototypeCleanupObligation(c context.Context, runID int64, kind, detail string) (PrototypeCleanupObligation, error) {
	var v PrototypeCleanupObligation
	e := tx.tx.QueryRowContext(c, `INSERT INTO feature_workspace_prototype_cleanup_obligations(cleanup_obligation_id,run_row_id,obligation_kind,detail) VALUES(?,?,?,?) ON CONFLICT(run_row_id,obligation_kind) DO UPDATE SET updated_at=updated_at RETURNING id,run_row_id,cleanup_obligation_id,obligation_kind,status,detail,created_at,updated_at`, NewPrototypeCleanupObligationID(), runID, kind, detail).Scan(&v.ID, &v.RunRowID, &v.CleanupObligationID, &v.ObligationKind, &v.Status, &v.Detail, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func (tx *Tx) CompletePrototypeCleanupObligation(c context.Context, runID int64, kind string) (PrototypeCleanupObligation, error) {
	v, e := tx.cleanup(c, runID, kind, nil)
	if e != nil {
		return v, e
	}
	if v.Status == "complete" {
		return v, nil
	}
	if v.Status == "failed" {
		_, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_cleanup_obligations SET status='complete',detail='verified absent after compensation',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, v.ID)
	} else {
		_, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_cleanup_obligations SET status='complete',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, v.ID)
	}
	return tx.cleanup(c, runID, kind, e)
}
func (tx *Tx) FailPrototypeCleanupObligation(c context.Context, runID int64, kind, detail string) (PrototypeCleanupObligation, error) {
	v, e := tx.cleanup(c, runID, kind, nil)
	if e != nil {
		return v, e
	}
	if v.Status == "complete" {
		return v, ErrPrototypeCleanupConflict
	}
	if v.Status == "failed" && v.Detail == detail {
		return v, nil
	}
	_, e = tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_cleanup_obligations SET status='failed',detail=?,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, detail, v.ID)
	return tx.cleanup(c, runID, kind, e)
}
func (tx *Tx) cleanup(c context.Context, runID int64, kind string, e error) (PrototypeCleanupObligation, error) {
	var v PrototypeCleanupObligation
	if e == nil {
		e = tx.tx.QueryRowContext(c, `SELECT id,run_row_id,cleanup_obligation_id,obligation_kind,status,detail,created_at,updated_at FROM feature_workspace_prototype_cleanup_obligations WHERE run_row_id=? AND obligation_kind=?`, runID, kind).Scan(&v.ID, &v.RunRowID, &v.CleanupObligationID, &v.ObligationKind, &v.Status, &v.Detail, &v.CreatedAt, &v.UpdatedAt)
	}
	return v, e
}
func (tx *Tx) ReleasePrototypeTarget(c context.Context, runID, key, when string) (PrototypeTarget, error) {
	_, e := tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_targets SET status='released',released_at=?,updated_at=? WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND target_key=?`, when, when, runID, key)
	if e != nil {
		return PrototypeTarget{}, e
	}
	return scanPrototypeTarget(tx.tx.QueryRowContext(c, `SELECT `+targetCols+` FROM feature_workspace_prototype_targets WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND target_key=?`, runID, key))
}
func (tx *Tx) FailPrototypeTarget(c context.Context, runID, key string) (PrototypeTarget, error) {
	_, e := tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_targets SET status='failed' WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND target_key=?`, runID, key)
	if e != nil {
		return PrototypeTarget{}, e
	}
	return scanPrototypeTarget(tx.tx.QueryRowContext(c, `SELECT `+targetCols+` FROM feature_workspace_prototype_targets WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND target_key=?`, runID, key))
}
func (tx *Tx) ReleasePrototypeLease(c context.Context, runID, token, when string) (PrototypeLease, error) {
	_, e := tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_leases SET status='released',released_at=?,updated_at=? WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND lease_token=?`, when, when, runID, token)
	if e != nil {
		return PrototypeLease{}, e
	}
	return scanPrototypeLease(tx.tx.QueryRowContext(c, `SELECT `+leaseCols+` FROM feature_workspace_prototype_leases WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND lease_token=?`, runID, token))
}
func (tx *Tx) FailPrototypeLease(c context.Context, runID, token, detail string) (PrototypeLease, error) {
	_, e := tx.tx.ExecContext(c, `UPDATE feature_workspace_prototype_leases SET status='failed',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND lease_token=?`, runID, token)
	if e != nil {
		return PrototypeLease{}, e
	}
	return scanPrototypeLease(tx.tx.QueryRowContext(c, `SELECT `+leaseCols+` FROM feature_workspace_prototype_leases WHERE run_row_id=(SELECT id FROM feature_workspace_prototype_runs WHERE prototype_run_id=?) AND lease_token=?`, runID, token))
}
