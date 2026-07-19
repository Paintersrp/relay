-- name: CreateRepositoryTarget :one
INSERT INTO repository_targets (repo_target, local_path)
VALUES (?, ?)
RETURNING *;

-- name: GetRepositoryTarget :one
SELECT *
FROM repository_targets
WHERE repo_target = ? COLLATE NOCASE;

-- name: ListRepositoryTargets :many
SELECT *
FROM repository_targets
ORDER BY repo_target COLLATE NOCASE;

-- name: CreatePlan :one
INSERT INTO plans (
    project_row_id,
    plan_id,
    feature_slug,
    status,
    canonical_sha256
)
VALUES (?, ?, ?, 'active', ?)
RETURNING *;

-- name: GetPlanByPlanID :one
SELECT *
FROM plans
WHERE plan_id = ?;

-- name: CreatePlanRepositoryTarget :one
INSERT INTO plan_repository_targets (
    plan_row_id,
    sequence,
    repo_target,
    branch,
    planning_base_commit
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListPlanRepositoryTargets :many
SELECT *
FROM plan_repository_targets
WHERE plan_row_id = ?
ORDER BY sequence;

-- name: CreatePlanPass :one
INSERT INTO plan_passes (
    pass_id,
    plan_row_id,
    pass_number,
    name,
    repo_target,
    status
)
VALUES (?, ?, ?, ?, ?, 'planned')
RETURNING *;

-- name: GetPlanPassByRowID :one
SELECT *
FROM plan_passes
WHERE id = ?;

-- name: GetPlanPassByPassID :one
SELECT *
FROM plan_passes
WHERE pass_id = ?;

-- name: GetPlanPassByPlanAndNumber :one
SELECT *
FROM plan_passes
WHERE plan_row_id = ? AND pass_number = ?;

-- name: ListPlanPasses :many
SELECT *
FROM plan_passes
WHERE plan_row_id = ?
ORDER BY pass_number;

-- name: CreatePlanPassDependency :exec
INSERT INTO plan_pass_dependencies (pass_row_id, depends_on_pass_row_id)
VALUES (?, ?);

-- name: ListPlanPassDependencies :many
SELECT *
FROM plan_pass_dependencies
WHERE pass_row_id = ?
ORDER BY depends_on_pass_row_id;

-- name: CountIncompletePlanPasses :one
SELECT COUNT(*)
FROM plan_passes
WHERE plan_row_id = ? AND status <> 'completed';

-- name: TransitionPlanPassStatus :one
UPDATE plan_passes
SET
    status = ?,
    started_at = CASE
        WHEN ? = 'in_progress' THEN COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE started_at
    END,
    completed_at = CASE
        WHEN ? = 'completed' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE pass_id = ? AND status = ?
RETURNING *;

-- name: CompletePlan :one
UPDATE plans
SET
    status = 'completed',
    completed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ? AND status = 'active'
RETURNING *;

-- name: CreateRun :one
INSERT INTO runs (
    run_id,
    feature_slug,
    repo_target,
    plan_row_id,
    plan_pass_row_id,
    remediates_run_row_id,
    status,
    branch,
    base_commit,
    canonical_sha256
)
VALUES (?, ?, ?, ?, ?, ?, 'created', ?, ?, ?)
RETURNING *;

-- name: GetRunByRowID :one
SELECT *
FROM runs
WHERE id = ?;

-- name: GetRunByRunID :one
SELECT *
FROM runs
WHERE run_id = ?;

-- name: ListRunsByPlanPass :many
SELECT *
FROM runs
WHERE plan_pass_row_id = ?
ORDER BY created_at, id;

-- name: TransitionRunStatus :one
UPDATE runs
SET
    status = ?,
    completed_at = CASE
        WHEN ? = 'completed' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE run_id = ? AND status = ?
RETURNING *;

-- name: NextExecutionAttemptNumber :one
SELECT COALESCE(MAX(attempt_number), 0) + 1
FROM execution_attempts
WHERE run_row_id = ?;

-- name: CreateExecutionAttempt :one
INSERT INTO execution_attempts (
    attempt_id,
    run_row_id,
    attempt_number,
    adapter,
    model,
    status
)
VALUES (?, ?, ?, ?, ?, 'pending')
RETURNING *;

-- name: GetExecutionAttemptByAttemptID :one
SELECT *
FROM execution_attempts
WHERE attempt_id = ?;

-- name: GetLatestExecutionAttemptByRun :one
SELECT *
FROM execution_attempts
WHERE run_row_id = ?
ORDER BY attempt_number DESC
LIMIT 1;

-- name: TransitionExecutionAttemptStatus :one
UPDATE execution_attempts
SET
    status = sqlc.arg(status),
    result_json = ?,
    started_at = CASE
        WHEN sqlc.arg(status) IN ('running', 'cancelled') THEN COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE started_at
    END,
    finished_at = CASE
        WHEN sqlc.arg(status) IN ('succeeded', 'failed', 'cancelled', 'timed_out') THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
        ELSE NULL
    END,
    cancellation_requested_at = CASE
        WHEN sqlc.arg(status) = 'cancelled' THEN COALESCE(cancellation_requested_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
        ELSE cancellation_requested_at
    END
WHERE attempt_id = ? AND status = ?
RETURNING *;

-- name: CreateArtifact :one
INSERT INTO artifacts (
    artifact_id,
    owner_type,
    plan_row_id,
    run_row_id,
    execution_attempt_row_id,
    kind,
    relative_path,
    media_type,
    sha256,
    size_bytes
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetArtifactByArtifactID :one
SELECT *
FROM artifacts
WHERE artifact_id = ?;

-- name: ListArtifactsByPlan :many
SELECT *
FROM artifacts
WHERE plan_row_id = ?
ORDER BY created_at, id;

-- name: ListArtifactsByRun :many
SELECT *
FROM artifacts
WHERE run_row_id = ?
ORDER BY created_at, id;

-- name: ListArtifactsByExecutionAttempt :many
SELECT *
FROM artifacts
WHERE execution_attempt_row_id = ?
ORDER BY created_at, id;

-- name: CreateAuditDecision :one
INSERT INTO audit_decisions (
    audit_decision_id,
    run_row_id,
    audit_packet_artifact_row_id,
    audited_commit,
    packet_sha256,
    decision,
    rationale
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAuditDecisionByDecisionID :one
SELECT *
FROM audit_decisions
WHERE audit_decision_id = ?;

-- name: CreateFeatureWorkspace :one
INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetFeatureWorkspaceByWorkspaceID :one
SELECT *
FROM feature_workspaces
WHERE workspace_id = ?;

-- name: ListFeatureWorkspacesByProject :many
SELECT *
FROM feature_workspaces
WHERE project_row_id = ?
ORDER BY feature_slug, id;

-- name: CreateFeatureWorkspaceAdmittedInput :one
INSERT INTO feature_workspace_admitted_inputs (
    admitted_input_id, workspace_row_id, sequence, input_name, input_role,
    source_kind, artifact_row_id, retained_artifact_row_id, source_closure_row_id,
    artifact_sha256, source_reference
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListFeatureWorkspaceAdmittedInputs :many
SELECT *
FROM feature_workspace_admitted_inputs
WHERE workspace_row_id = ?
ORDER BY sequence, id;

-- name: CreateFeatureWorkspaceDestination :one
INSERT INTO feature_workspace_destinations (
    destination_id, workspace_row_id, sequence, destination_kind, destination_key,
    repo_target, source_closure_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListFeatureWorkspaceDestinations :many
SELECT *
FROM feature_workspace_destinations
WHERE workspace_row_id = ?
ORDER BY sequence, id;

-- name: CreateFeatureWorkspaceDiscoveryTicket :one
INSERT INTO feature_workspace_discovery_tickets (
    discovery_ticket_id, workspace_row_id, ticket_key, subject
)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetFeatureWorkspaceDiscoveryTicketByID :one
SELECT *
FROM feature_workspace_discovery_tickets
WHERE discovery_ticket_id = ?;

-- name: ListFeatureWorkspaceDiscoveryTickets :many
SELECT *
FROM feature_workspace_discovery_tickets
WHERE workspace_row_id = ?
ORDER BY id;

-- name: TransitionFeatureWorkspaceDiscoveryTicket :one
UPDATE feature_workspace_discovery_tickets
SET state = ?, version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE discovery_ticket_id = ? AND state = ? AND version = ?
RETURNING *;

-- name: CreateFeatureWorkspaceTicketDependency :exec
INSERT INTO feature_workspace_ticket_dependencies (
    ticket_row_id, depends_on_ticket_row_id, dependency_kind
)
VALUES (?, ?, ?);

-- name: ListFeatureWorkspaceTicketDependencies :many
SELECT *
FROM feature_workspace_ticket_dependencies
WHERE ticket_row_id = ?
ORDER BY depends_on_ticket_row_id;

-- name: CreateFeatureWorkspaceTicketResolution :one
INSERT INTO feature_workspace_ticket_resolutions (
    resolution_id, ticket_row_id, sequence, resolution_kind, artifact_row_id,
    retained_artifact_row_id, artifact_sha256, source_closure_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListFeatureWorkspaceTicketResolutions :many
SELECT *
FROM feature_workspace_ticket_resolutions
WHERE ticket_row_id = ?
ORDER BY sequence, id;

-- name: CreateFeatureWorkspaceRouteState :one
INSERT INTO feature_workspace_route_states (
    route_state_id, workspace_row_id, sequence, workspace_version, state, ticket_row_id
)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListFeatureWorkspaceRouteStates :many
SELECT *
FROM feature_workspace_route_states
WHERE workspace_row_id = ?
ORDER BY sequence, id;

-- name: AdvanceFeatureWorkspaceRouteState :one
UPDATE feature_workspaces
SET current_route_state_row_id = ?, state = ?, version = version + 1,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE workspace_id = ? AND version = ?
RETURNING *;

-- name: CreateFeatureWorkspaceInvestigation :one
INSERT INTO feature_workspace_investigations (
    investigation_id, workspace_row_id, ticket_row_id, sequence, investigation_kind,
    artifact_row_id, retained_artifact_row_id, artifact_sha256, source_closure_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetFeatureWorkspaceInvestigationByID :one
SELECT *
FROM feature_workspace_investigations
WHERE investigation_id = ?;

-- name: ListFeatureWorkspaceInvestigations :many
SELECT *
FROM feature_workspace_investigations
WHERE workspace_row_id = ?
ORDER BY sequence, id;

-- name: CreateFeatureWorkspaceAuthorityRevision :one
INSERT INTO feature_workspace_authority_revisions (
    authority_revision_id, workspace_row_id, revision_number, source_closure_row_id
)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListFeatureWorkspaceAuthorityRevisions :many
SELECT *
FROM feature_workspace_authority_revisions
WHERE workspace_row_id = ?
ORDER BY revision_number, id;

-- name: CreateFeatureWorkspaceAuthorityLayer :one
INSERT INTO feature_workspace_authority_layers (
    authority_revision_row_id, layer_kind, sequence, artifact_row_id,
    retained_artifact_row_id, artifact_sha256, source_closure_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListFeatureWorkspaceAuthorityLayers :many
SELECT *
FROM feature_workspace_authority_layers
WHERE authority_revision_row_id = ?
ORDER BY sequence, id;

-- name: SetFeatureWorkspaceAuthorityRevision :one
UPDATE feature_workspaces
SET current_authority_revision_row_id = ?, version = version + 1,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE workspace_id = ? AND version = ?
RETURNING *;

-- name: CreateDeliveryTicket :one
INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetDeliveryTicketByTicketID :one
SELECT *
FROM delivery_tickets
WHERE ticket_id = ?;

-- name: ListDeliveryTicketsByWorkspace :many
SELECT *
FROM delivery_tickets
WHERE workspace_row_id = ?
ORDER BY external_priority DESC, ticket_id, id;

-- name: UpdateDeliveryTicketExternalPriority :one
UPDATE delivery_tickets
SET external_priority = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE ticket_id = ?
RETURNING *;

-- name: CreateDeliveryTicketRevision :one
INSERT INTO delivery_ticket_revisions (
    delivery_ticket_row_id, revision_number, replaces_revision_row_id,
    cancellation_reason, repo_target, branch, base_commit, source_closure_row_id,
    source_path, goal, context, transition_applicability
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDeliveryTicketRevisionByRowID :one
SELECT *
FROM delivery_ticket_revisions
WHERE id = ?;

-- name: ListDeliveryTicketRevisions :many
SELECT *
FROM delivery_ticket_revisions
WHERE delivery_ticket_row_id = ?
ORDER BY revision_number, id;

-- name: SetDeliveryTicketCurrentRevision :one
UPDATE delivery_tickets
SET current_revision_row_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE ticket_id = ?
RETURNING *;

-- name: CreateDeliveryTicketRevisionMember :one
INSERT INTO delivery_ticket_revision_members (
    revision_row_id, sequence, member_kind, member_path, member_text
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDeliveryTicketRevisionMembers :many
SELECT *
FROM delivery_ticket_revision_members
WHERE revision_row_id = ?
ORDER BY sequence, id;

-- name: CreateDeliveryTicketRevisionDependency :one
INSERT INTO delivery_ticket_revision_dependencies (
    revision_row_id, sequence, depends_on_revision_row_id, outcome
)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListDeliveryTicketRevisionDependencies :many
SELECT *
FROM delivery_ticket_revision_dependencies
WHERE revision_row_id = ?
ORDER BY sequence, id;

-- name: CreateDeliveryTicketRevisionApproval :one
INSERT INTO delivery_ticket_revision_approvals (
    approval_id, revision_row_id, approval_kind, approval_state, rationale,
    source_closure_row_id, authority_revision_row_id
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDeliveryTicketRevisionApprovals :many
SELECT *
FROM delivery_ticket_revision_approvals
WHERE revision_row_id = ?
ORDER BY id;

-- name: CreateDeliveryTicketSelection :one
INSERT INTO delivery_ticket_selections (
    selection_id, workspace_row_id, state, rationale, source_closure_row_id
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDeliveryTicketSelectionBySelectionID :one
SELECT *
FROM delivery_ticket_selections
WHERE selection_id = ?;

-- name: ListDeliveryTicketSelectionsByWorkspace :many
SELECT *
FROM delivery_ticket_selections
WHERE workspace_row_id = ?
ORDER BY created_at, id;

-- name: TransitionDeliveryTicketSelection :one
UPDATE delivery_ticket_selections
SET state = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE selection_id = ? AND state = 'active'
RETURNING *;

-- name: CreateDeliveryTicketSelectionMember :one
INSERT INTO delivery_ticket_selection_members (
    selection_row_id, sequence, revision_row_id, approval_row_id
)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListDeliveryTicketSelectionMembers :many
SELECT *
FROM delivery_ticket_selection_members
WHERE selection_row_id = ?
ORDER BY sequence, id;

-- name: CreateExecutionPackage :one
INSERT INTO execution_packages (
    package_id,
    selection_row_id,
    workspace_row_id,
    repo_target,
    branch,
    base_commit,
    source_closure_row_id,
    authority_revision_row_id,
    package_sha256,
    authority_sha256,
    source_sha256,
    design_brief_sha256,
    execution_spec_sha256
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetExecutionPackageByPackageID :one
SELECT *
FROM execution_packages
WHERE package_id = ?;

-- name: GetExecutionPackageBySelectionRowID :one
SELECT *
FROM execution_packages
WHERE selection_row_id = ?;

-- name: ListExecutionPackagesByWorkspace :many
SELECT *
FROM execution_packages
WHERE workspace_row_id = ?
ORDER BY created_at, id;

-- name: CreateExecutionPackageMember :one
INSERT INTO execution_package_members (
    package_row_id,
    selection_member_row_id,
    sequence,
    revision_row_id,
    member_sha256
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListExecutionPackageMembers :many
SELECT *
FROM execution_package_members
WHERE package_row_id = ?
ORDER BY sequence, id;

-- name: CreateExecutionPackageApprovalBinding :one
INSERT INTO execution_package_approval_bindings (
    package_row_id,
    package_member_row_id,
    approval_row_id,
    authority_revision_row_id,
    source_closure_row_id,
    approval_basis_sha256
)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListExecutionPackageApprovalBindings :many
SELECT *
FROM execution_package_approval_bindings
WHERE package_row_id = ?
ORDER BY package_member_row_id, id;

-- name: ConsumeDeliveryTicketSelection :one
UPDATE delivery_ticket_selections
SET state = 'consumed', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE selection_id = ? AND state = 'active'
RETURNING *;

-- name: LinkRunToExecutionPackage :one
UPDATE runs
SET execution_package_row_id = ?
WHERE run_id = ? AND execution_package_row_id IS NULL
RETURNING *;

-- name: CreateRepositoryBranchMutationLease :one
INSERT INTO repository_branch_mutation_leases (
    lease_id,
    repo_target,
    branch,
    owner_kind,
    owner_identity,
    state,
    uncertainty_state,
    uncertainty_reason,
    reconciliation_state,
    reconciliation_note,
    reconciliation_started_at,
    reconciled_at
)
VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRepositoryBranchMutationLeaseByLeaseID :one
SELECT *
FROM repository_branch_mutation_leases
WHERE lease_id = ?;

-- name: GetActiveRepositoryBranchMutationLease :one
SELECT *
FROM repository_branch_mutation_leases
WHERE repo_target = ? COLLATE NOCASE
  AND branch = ?
  AND state = 'active';

-- name: ListRepositoryBranchMutationLeases :many
SELECT *
FROM repository_branch_mutation_leases
WHERE repo_target = ? COLLATE NOCASE
  AND branch = ?
ORDER BY acquired_at, id;

-- name: UpdateRepositoryBranchMutationLeaseFacts :one
UPDATE repository_branch_mutation_leases
SET
    uncertainty_state = ?,
    uncertainty_reason = ?,
    reconciliation_state = ?,
    reconciliation_note = ?,
    reconciliation_started_at = ?,
    reconciled_at = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE lease_id = ? AND state = ?
RETURNING *;

-- name: ReleaseRepositoryBranchMutationLease :one
UPDATE repository_branch_mutation_leases
SET
    state = 'released',
    released_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE lease_id = ? AND state = 'active'
RETURNING *;
