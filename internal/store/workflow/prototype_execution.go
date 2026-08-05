package workflowstore

import (
	"context"
	"database/sql"
)

type PrototypeProposal struct {
	ID, WorkspaceRowID, WorkItemRowID, DiscoveryRevisionRowID, ArtifactRowID, ProposalSizeBytes int64
	ProposalID, ProposalSHA256, ProposalMediaType, CreatedAt                                    string
}
type PrototypeAuthorization struct {
	ID, ProposalRowID, WorkspaceRowID, WorkItemRowID, DiscoveryRevisionRowID, SourceClosureRowID, InvocationArtifactRowID, InvocationSizeBytes                                             int64
	AuthorizationID, SourceCommit, SourceTree, RepoTarget, BaseCommit, Adapter, Model, VariantsJSON, EvidenceObligationsJSON, LimitsJSON, InvocationSHA256, InvocationMediaType, CreatedAt string
}
type PrototypeRun struct {
	ID, AuthorizationRowID, WorkspaceRowID, WorkItemRowID, Version      int64
	PrototypeRunID, LifecycleState, CleanupStatus, CreatedAt, UpdatedAt string
	ProcessOutcome, LaunchUncertaintyReason, ExternalProcessIdentity    sql.NullString
}
type PrototypeApproval struct {
	ID, RunRowID, AuthorizationRowID                                                                    int64
	ApprovalID, MutationIdentity, OperatorConfirmationEvidence, ConsumedIdentity, ConsumedAt, CreatedAt string
}
type PrototypeLaunchClaim struct {
	ID, RunRowID                             int64
	LaunchClaimID, LaunchProtocol, ClaimedAt string
}
type PrototypeExecutionAggregate struct {
	Proposal      PrototypeProposal
	Authorization PrototypeAuthorization
	Run           PrototypeRun
	Approval      *PrototypeApproval
	LaunchClaim   *PrototypeLaunchClaim
}

func (s *Store) GetPrototypeProposal(ctx context.Context, id string) (PrototypeProposal, error) {
	return getPrototypeProposal(ctx, s.db, id)
}
func (s *Store) GetPrototypeAuthorization(ctx context.Context, id string) (PrototypeAuthorization, error) {
	return getPrototypeAuthorization(ctx, s.db, id)
}
func (s *Store) GetPrototypeRun(ctx context.Context, id string) (PrototypeRun, error) {
	return getPrototypeRun(ctx, s.db, id)
}
func (s *Store) ReadPrototypeExecution(ctx context.Context, workspaceID, runID string) (PrototypeExecutionAggregate, error) {
	return readPrototypeExecution(ctx, s.db, workspaceID, runID)
}
func (tx *Tx) GetPrototypeProposal(ctx context.Context, id string) (PrototypeProposal, error) {
	return getPrototypeProposal(ctx, tx.tx, id)
}
func (tx *Tx) GetPrototypeAuthorization(ctx context.Context, id string) (PrototypeAuthorization, error) {
	return getPrototypeAuthorization(ctx, tx.tx, id)
}
func (tx *Tx) GetPrototypeRun(ctx context.Context, id string) (PrototypeRun, error) {
	return getPrototypeRun(ctx, tx.tx, id)
}
func (tx *Tx) GetPrototypeApprovalByRun(ctx context.Context, runID int64) (PrototypeApproval, error) {
	return scanPrototypeApproval(tx.tx.QueryRowContext(ctx, `SELECT id, approval_id, run_row_id, authorization_row_id, mutation_identity, operator_confirmation_evidence, consumed_identity, consumed_at, created_at FROM feature_workspace_prototype_approvals WHERE run_row_id = ?`, runID))
}
func (tx *Tx) CreatePrototypeProposal(ctx context.Context, v PrototypeProposal) (PrototypeProposal, error) {
	return scanPrototypeProposal(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_proposals (proposal_id, workspace_row_id, work_item_row_id, discovery_revision_row_id, artifact_row_id, proposal_sha256, proposal_size_bytes, proposal_media_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, proposal_id, workspace_row_id, work_item_row_id, discovery_revision_row_id, artifact_row_id, proposal_sha256, proposal_size_bytes, proposal_media_type, created_at`, v.ProposalID, v.WorkspaceRowID, v.WorkItemRowID, v.DiscoveryRevisionRowID, v.ArtifactRowID, v.ProposalSHA256, v.ProposalSizeBytes, v.ProposalMediaType))
}
func (tx *Tx) CreatePrototypeAuthorization(ctx context.Context, v PrototypeAuthorization) (PrototypeAuthorization, error) {
	return scanPrototypeAuthorization(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_authorizations (authorization_id, proposal_row_id, workspace_row_id, work_item_row_id, discovery_revision_row_id, source_closure_row_id, source_commit, source_tree, repo_target, base_commit, adapter, model, variants_json, evidence_obligations_json, limits_json, invocation_artifact_row_id, invocation_sha256, invocation_size_bytes, invocation_media_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, authorization_id, proposal_row_id, workspace_row_id, work_item_row_id, discovery_revision_row_id, source_closure_row_id, source_commit, source_tree, repo_target, base_commit, adapter, model, variants_json, evidence_obligations_json, limits_json, invocation_artifact_row_id, invocation_sha256, invocation_size_bytes, invocation_media_type, created_at`, v.AuthorizationID, v.ProposalRowID, v.WorkspaceRowID, v.WorkItemRowID, v.DiscoveryRevisionRowID, v.SourceClosureRowID, v.SourceCommit, v.SourceTree, v.RepoTarget, v.BaseCommit, v.Adapter, v.Model, v.VariantsJSON, v.EvidenceObligationsJSON, v.LimitsJSON, v.InvocationArtifactRowID, v.InvocationSHA256, v.InvocationSizeBytes, v.InvocationMediaType))
}
func (tx *Tx) CreatePrototypeRun(ctx context.Context, v PrototypeRun) (PrototypeRun, error) {
	return scanPrototypeRun(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_runs (prototype_run_id, authorization_row_id, workspace_row_id, work_item_row_id) VALUES (?, ?, ?, ?) RETURNING id, prototype_run_id, authorization_row_id, workspace_row_id, work_item_row_id, lifecycle_state, version, process_outcome, cleanup_status, launch_uncertainty_reason, external_process_identity, created_at, updated_at`, v.PrototypeRunID, v.AuthorizationRowID, v.WorkspaceRowID, v.WorkItemRowID))
}
func (tx *Tx) CreatePrototypeApproval(ctx context.Context, v PrototypeApproval) (PrototypeApproval, error) {
	return scanPrototypeApproval(tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_prototype_approvals (approval_id, run_row_id, authorization_row_id, mutation_identity, operator_confirmation_evidence, consumed_identity) VALUES (?, ?, ?, ?, ?, ?) RETURNING id, approval_id, run_row_id, authorization_row_id, mutation_identity, operator_confirmation_evidence, consumed_identity, consumed_at, created_at`, v.ApprovalID, v.RunRowID, v.AuthorizationRowID, v.MutationIdentity, v.OperatorConfirmationEvidence, v.ConsumedIdentity))
}
func (tx *Tx) ApprovePrototypeRun(ctx context.Context, runID string, expectedVersion int64) (PrototypeRun, error) {
	return scanPrototypeRun(tx.tx.QueryRowContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state = 'approved', version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE prototype_run_id = ? AND lifecycle_state = 'proposed' AND version = ? RETURNING id, prototype_run_id, authorization_row_id, workspace_row_id, work_item_row_id, lifecycle_state, version, process_outcome, cleanup_status, launch_uncertainty_reason, external_process_identity, created_at, updated_at`, runID, expectedVersion))
}
func (tx *Tx) CreatePrototypeLifecycleTransition(ctx context.Context, runRowID int64, identity string, approvalRowID int64, version int64) error {
	_, err := tx.tx.ExecContext(ctx, `INSERT INTO feature_workspace_prototype_lifecycle_transitions (run_row_id, transition_identity, from_state, to_state, run_version, approval_row_id) VALUES (?, ?, 'proposed', 'approved', ?, ?)`, runRowID, identity, version, approvalRowID)
	return err
}
func getPrototypeProposal(ctx context.Context, q rowQueryer, id string) (PrototypeProposal, error) {
	return scanPrototypeProposal(q.QueryRowContext(ctx, `SELECT id, proposal_id, workspace_row_id, work_item_row_id, discovery_revision_row_id, artifact_row_id, proposal_sha256, proposal_size_bytes, proposal_media_type, created_at FROM feature_workspace_prototype_proposals WHERE proposal_id = ?`, id))
}
func getPrototypeAuthorization(ctx context.Context, q rowQueryer, id string) (PrototypeAuthorization, error) {
	return scanPrototypeAuthorization(q.QueryRowContext(ctx, `SELECT id, authorization_id, proposal_row_id, workspace_row_id, work_item_row_id, discovery_revision_row_id, source_closure_row_id, source_commit, source_tree, repo_target, base_commit, adapter, model, variants_json, evidence_obligations_json, limits_json, invocation_artifact_row_id, invocation_sha256, invocation_size_bytes, invocation_media_type, created_at FROM feature_workspace_prototype_authorizations WHERE authorization_id = ?`, id))
}
func getPrototypeRun(ctx context.Context, q rowQueryer, id string) (PrototypeRun, error) {
	return scanPrototypeRun(q.QueryRowContext(ctx, `SELECT id, prototype_run_id, authorization_row_id, workspace_row_id, work_item_row_id, lifecycle_state, version, process_outcome, cleanup_status, launch_uncertainty_reason, external_process_identity, created_at, updated_at FROM feature_workspace_prototype_runs WHERE prototype_run_id = ?`, id))
}
func readPrototypeExecution(ctx context.Context, q rowQueryer, workspaceID, runID string) (PrototypeExecutionAggregate, error) {
	var a PrototypeExecutionAggregate
	var err error
	a.Run, err = scanPrototypeRun(q.QueryRowContext(ctx, `SELECT r.id,r.prototype_run_id,r.authorization_row_id,r.workspace_row_id,r.work_item_row_id,r.lifecycle_state,r.version,r.process_outcome,r.cleanup_status,r.launch_uncertainty_reason,r.external_process_identity,r.created_at,r.updated_at FROM feature_workspace_prototype_runs r JOIN feature_workspaces w ON w.id=r.workspace_row_id WHERE w.workspace_id=? AND r.prototype_run_id=?`, workspaceID, runID))
	if err != nil {
		return a, err
	}
	a.Authorization, err = getPrototypeAuthorization(ctx, q, prototypeAuthorizationIDByRow(ctx, q, a.Run.AuthorizationRowID))
	if err != nil {
		return a, err
	}
	a.Proposal, err = getPrototypeProposal(ctx, q, prototypeProposalIDByRow(ctx, q, a.Authorization.ProposalRowID))
	if err != nil {
		return a, err
	}
	approval, err := scanPrototypeApproval(q.QueryRowContext(ctx, `SELECT id,approval_id,run_row_id,authorization_row_id,mutation_identity,operator_confirmation_evidence,consumed_identity,consumed_at,created_at FROM feature_workspace_prototype_approvals WHERE run_row_id=?`, a.Run.ID))
	if err == nil {
		a.Approval = &approval
	} else if err != sql.ErrNoRows {
		return a, err
	}
	claim, err := scanPrototypeLaunchClaim(q.QueryRowContext(ctx, `SELECT id,launch_claim_id,run_row_id,launch_protocol,claimed_at FROM feature_workspace_prototype_launch_claims WHERE run_row_id=?`, a.Run.ID))
	if err == nil {
		a.LaunchClaim = &claim
	} else if err != sql.ErrNoRows {
		return a, err
	}
	return a, nil
}
func prototypeAuthorizationIDByRow(ctx context.Context, q rowQueryer, id int64) string {
	var v string
	_ = q.QueryRowContext(ctx, `SELECT authorization_id FROM feature_workspace_prototype_authorizations WHERE id=?`, id).Scan(&v)
	return v
}
func prototypeProposalIDByRow(ctx context.Context, q rowQueryer, id int64) string {
	var v string
	_ = q.QueryRowContext(ctx, `SELECT proposal_id FROM feature_workspace_prototype_proposals WHERE id=?`, id).Scan(&v)
	return v
}
func scanPrototypeProposal(r rowScanner) (v PrototypeProposal, e error) {
	e = r.Scan(&v.ID, &v.ProposalID, &v.WorkspaceRowID, &v.WorkItemRowID, &v.DiscoveryRevisionRowID, &v.ArtifactRowID, &v.ProposalSHA256, &v.ProposalSizeBytes, &v.ProposalMediaType, &v.CreatedAt)
	return
}
func scanPrototypeAuthorization(r rowScanner) (v PrototypeAuthorization, e error) {
	e = r.Scan(&v.ID, &v.AuthorizationID, &v.ProposalRowID, &v.WorkspaceRowID, &v.WorkItemRowID, &v.DiscoveryRevisionRowID, &v.SourceClosureRowID, &v.SourceCommit, &v.SourceTree, &v.RepoTarget, &v.BaseCommit, &v.Adapter, &v.Model, &v.VariantsJSON, &v.EvidenceObligationsJSON, &v.LimitsJSON, &v.InvocationArtifactRowID, &v.InvocationSHA256, &v.InvocationSizeBytes, &v.InvocationMediaType, &v.CreatedAt)
	return
}
func scanPrototypeRun(r rowScanner) (v PrototypeRun, e error) {
	e = r.Scan(&v.ID, &v.PrototypeRunID, &v.AuthorizationRowID, &v.WorkspaceRowID, &v.WorkItemRowID, &v.LifecycleState, &v.Version, &v.ProcessOutcome, &v.CleanupStatus, &v.LaunchUncertaintyReason, &v.ExternalProcessIdentity, &v.CreatedAt, &v.UpdatedAt)
	return
}
func scanPrototypeApproval(r rowScanner) (v PrototypeApproval, e error) {
	e = r.Scan(&v.ID, &v.ApprovalID, &v.RunRowID, &v.AuthorizationRowID, &v.MutationIdentity, &v.OperatorConfirmationEvidence, &v.ConsumedIdentity, &v.ConsumedAt, &v.CreatedAt)
	return
}
func scanPrototypeLaunchClaim(r rowScanner) (v PrototypeLaunchClaim, e error) {
	e = r.Scan(&v.ID, &v.LaunchClaimID, &v.RunRowID, &v.LaunchProtocol, &v.ClaimedAt)
	return
}
