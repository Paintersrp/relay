package audits

import (
	"context"
	"strings"
	"testing"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

// guidedReadFixture builds a workspace with one package Run and optionally the
// audit packet, decision, and remediation seed chain, mirroring the guided
// journey's audit owner reads. The run walks the real transition chain so
// inserts observe the audit constraints: the decision row is inserted while
// the Run is audit_ready, then the Run transitions to completed or
// needs_revision. withReopening adds a current remediation ticket revision
// reopening the seed.
func guidedReadFixture(t *testing.T, runStatus string, withPacket, withDecision, withRemediation, withReopening bool) (*workflowstore.Store, *WorkflowAuditService, string, string) {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	ctx := context.Background()
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-guided-read', 'Guided Read')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-guided-read', (SELECT id FROM projects WHERE project_id = 'project-guided-read'), 'guided')`); err != nil {
		t.Fatal(err)
	}
	var runRowID int64
	if err := store.DB().QueryRowContext(ctx, `
INSERT INTO runs (run_id, feature_slug, repo_target, status, branch, base_commit)
VALUES ('run-guided-read', 'guided', 'relay', 'created', 'main', ?)
RETURNING id`, strings.Repeat("a", 40)).Scan(&runRowID); err != nil {
		t.Fatal(err)
	}
	// Decisions may only be inserted while the Run is audit_ready; completed
	// and needs_revision Runs transition from audit_ready afterwards.
	insertStatus := runStatus
	if insertStatus == "completed" || insertStatus == "needs_revision" {
		insertStatus = "audit_ready"
	}
	if insertStatus != "created" {
		walkGuidedRunStatus(t, store, ctx, "created", insertStatus)
	}

	var packetArtifactID int64
	if withRemediation || withPacket {
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, run_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-guided-read', 'run', ?, 'audit_packet', 'audit-packets/packet-guided-read/audit-packet.json', 'application/json', ?, 1) RETURNING id`, runRowID, strings.Repeat("c", 64)).Scan(&packetArtifactID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().ExecContext(ctx, `
INSERT INTO audit_packets (audit_packet_id, run_row_id, implementation_actor_kind, artifact_row_id, base_commit, audited_commit, packet_sha256, status)
VALUES ('packet-guided-read', ?, 'applier', ?, ?, ?, ?, 'current')`, runRowID, packetArtifactID, strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if withRemediation || withDecision {
		if _, err := store.DB().ExecContext(ctx, `
INSERT INTO audit_decisions (audit_decision_id, run_row_id, audit_packet_artifact_row_id, audited_commit, packet_sha256, decision, rationale)
VALUES ('audit-decision-guided-read', ?, ?, ?, ?, 'needs_revision', 'guided read needs revision')`, runRowID, packetArtifactID, strings.Repeat("b", 40), strings.Repeat("c", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if withRemediation {
		var vaultID, closureID, authorityID, ticketID, revisionID, approvalID, selectionID, selectionMemberID, packageID, packageMemberID int64
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-guided-read', 'relay', 'vaults/guided-read') RETURNING id`).Scan(&vaultID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-guided-read', ?, ?, ?, 1, 'refs/relay/closures/closure-guided-read', 'ready', '2026-08-01T00:00:00.000000000Z', '2026-08-01T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("a", 40), strings.Repeat("b", 40)).Scan(&closureID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_authority_revisions (authority_revision_id, workspace_row_id, revision_number, source_closure_row_id) VALUES ('authority-guided-read', (SELECT id FROM feature_workspaces WHERE workspace_id='workspace-guided-read'), 1, ?) RETURNING id`, closureID).Scan(&authorityID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P6-T1', (SELECT id FROM feature_workspaces WHERE workspace_id='workspace-guided-read'), 10) RETURNING id`).Scan(&ticketID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, 'tickets/P6-T1.json', 'guided read', 'guided read context', 'not_required') RETURNING id`, ticketID, strings.Repeat("a", 40), closureID).Scan(&revisionID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().ExecContext(ctx, `UPDATE delivery_tickets SET current_revision_row_id=? WHERE id=?`, revisionID, ticketID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_ticket_revision_approvals (approval_id, revision_row_id, approval_kind, approval_state, rationale, source_closure_row_id, authority_revision_row_id) VALUES ('approval-guided-read', ?, 'delivery', 'approved', 'guided read approval', ?, ?) RETURNING id`, revisionID, closureID, authorityID).Scan(&approvalID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_ticket_selections (selection_id, workspace_row_id, state, rationale, source_closure_row_id) VALUES ('selection-guided-read', (SELECT id FROM feature_workspaces WHERE workspace_id='workspace-guided-read'), 'active', 'guided read selection', ?) RETURNING id`, closureID).Scan(&selectionID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_ticket_selection_members (selection_row_id, sequence, revision_row_id, approval_row_id) VALUES (?, 1, ?, ?) RETURNING id`, selectionID, revisionID, approvalID).Scan(&selectionMemberID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO execution_packages (package_id, selection_row_id, workspace_row_id, repo_target, branch, base_commit, source_closure_row_id, authority_revision_row_id, package_sha256, authority_sha256, source_sha256) VALUES ('package-guided-read', ?, (SELECT id FROM feature_workspaces WHERE workspace_id='workspace-guided-read'), 'relay', 'main', ?, ?, ?, ?, ?, ?) RETURNING id`, selectionID, strings.Repeat("a", 40), closureID, authorityID, strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)).Scan(&packageID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO execution_package_members (package_row_id, selection_member_row_id, sequence, revision_row_id, member_sha256) VALUES (?, ?, 1, ?, ?) RETURNING id`, packageID, selectionMemberID, revisionID, strings.Repeat("5", 64)).Scan(&packageMemberID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().ExecContext(ctx, `UPDATE runs SET execution_package_row_id=? WHERE run_id='run-guided-read'`, packageID); err != nil {
			t.Fatal(err)
		}
		var obligationID int64
		if err := store.DB().QueryRowContext(ctx, `
INSERT INTO audit_packet_ticket_obligations (audit_packet_row_id, execution_package_row_id, execution_package_member_row_id, delivery_ticket_row_id, delivery_ticket_revision_row_id, authority_revision_row_id, source_closure_row_id)
VALUES ((SELECT id FROM audit_packets WHERE audit_packet_id='packet-guided-read'), ?, ?, ?, ?, ?, ?)
RETURNING id`, packageID, packageMemberID, ticketID, revisionID, authorityID, closureID).Scan(&obligationID); err != nil {
			t.Fatal(err)
		}
		var revisionDecisionID int64
		if err := store.DB().QueryRowContext(ctx, `
INSERT INTO audit_ticket_revision_decisions (audit_decision_row_id, audit_packet_ticket_obligation_row_id)
VALUES ((SELECT id FROM audit_decisions WHERE audit_decision_id='audit-decision-guided-read'), ?) RETURNING id`, obligationID).Scan(&revisionDecisionID); err != nil {
			t.Fatal(err)
		}
		var seedRowID int64
		if err := store.DB().QueryRowContext(ctx, `
INSERT INTO audit_remediation_seeds (remediation_seed_id, audit_ticket_revision_decision_row_id, audit_packet_row_id, execution_package_row_id, audited_commit, decision_rationale)
VALUES ('remediation-guided-read', ?, (SELECT id FROM audit_packets WHERE audit_packet_id='packet-guided-read'), ?, ?, 'guided read needs revision')
RETURNING id`, revisionDecisionID, packageID, strings.Repeat("b", 40)).Scan(&seedRowID); err != nil {
			t.Fatal(err)
		}
		if withReopening {
			var reopeningTicketID, reopeningRevisionID int64
			if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P6-T2', (SELECT id FROM feature_workspaces WHERE workspace_id='workspace-guided-read'), 10) RETURNING id`).Scan(&reopeningTicketID); err != nil {
				t.Fatal(err)
			}
			if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, 'tickets/P6-T2.json', 'guided read remediation', 'guided read remediation context', 'not_required') RETURNING id`, reopeningTicketID, strings.Repeat("a", 40), closureID).Scan(&reopeningRevisionID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().ExecContext(ctx, `UPDATE delivery_tickets SET current_revision_row_id=? WHERE id=?`, reopeningRevisionID, reopeningTicketID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().ExecContext(ctx, `
INSERT INTO audit_remediation_seed_reopenings (remediation_seed_row_id, reopening_revision_row_id, reopening_kind)
VALUES (?, ?, 'remediation_ticket')`, seedRowID, reopeningRevisionID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if runStatus == "completed" || runStatus == "needs_revision" {
		walkGuidedRunStatus(t, store, ctx, "audit_ready", runStatus)
	}
	service, err := newWorkflowAuditService(store, func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error) {
		return workflowrepos.AuditCommitEvidence{}, nil
	}, func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
		return WorkflowPackageExecutionEvidence{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, service, "run-guided-read", "workspace-guided-read"
}

// walkGuidedRunStatus advances the Run through the allowed transition chain to
// the requested status. completed and needs_revision are single terminal steps
// from audit_ready.
func walkGuidedRunStatus(t *testing.T, store *workflowstore.Store, ctx context.Context, from, to string) {
	t.Helper()
	chain := []string{"created", "setup_ready", "executing", "validating", "audit_ready"}
	var path []string
	fromIndex := -1
	for index, status := range chain {
		if status == from {
			fromIndex = index
		}
	}
	if fromIndex < 0 {
		t.Fatalf("unsupported run status walk from %q", from)
	}
	switch to {
	case "setup_ready", "executing", "validating", "audit_ready":
		toIndex := -1
		for index, status := range chain {
			if status == to {
				toIndex = index
			}
		}
		if toIndex < fromIndex {
			t.Fatalf("unsupported run status walk %q -> %q", from, to)
		}
		path = chain[fromIndex+1 : toIndex+1]
	case "completed", "needs_revision":
		path = append(chain[fromIndex+1:], to)
	default:
		t.Fatalf("unsupported run status walk to %q", to)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current := from
		for _, next := range path {
			if _, err := tx.TransitionRun(ctx, "run-guided-read", current, next); err != nil {
				return err
			}
			current = next
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReadRunAuditStateTracksPacketAndDecision(t *testing.T) {
	t.Run("setup ready has no audit state", func(t *testing.T) {
		_, service, runID, _ := guidedReadFixture(t, "setup_ready", false, false, false, false)
		state, err := service.ReadRunAuditState(context.Background(), runID)
		if err != nil || state.State != "none" || state.RunStatus != "setup_ready" {
			t.Fatalf("setup-ready audit state = %+v, %v", state, err)
		}
	})
	t.Run("audit ready awaits audit", func(t *testing.T) {
		_, service, runID, _ := guidedReadFixture(t, "audit_ready", false, false, false, false)
		state, err := service.ReadRunAuditState(context.Background(), runID)
		if err != nil || state.State != "awaiting_audit" {
			t.Fatalf("audit-ready audit state = %+v, %v", state, err)
		}
	})
	t.Run("packet recorded", func(t *testing.T) {
		_, service, runID, _ := guidedReadFixture(t, "audit_ready", true, false, false, false)
		state, err := service.ReadRunAuditState(context.Background(), runID)
		if err != nil || state.State != "packet_recorded" || state.AuditPacketID != "packet-guided-read" || state.AuditedCommit != strings.Repeat("b", 40) {
			t.Fatalf("packet audit state = %+v, %v", state, err)
		}
	})
	t.Run("decision recorded", func(t *testing.T) {
		_, service, runID, _ := guidedReadFixture(t, "completed", true, true, false, false)
		state, err := service.ReadRunAuditState(context.Background(), runID)
		if err != nil || state.State != "decision_recorded" || state.AuditDecisionID != "audit-decision-guided-read" || state.AuditPacketID != "packet-guided-read" {
			t.Fatalf("decision audit state = %+v, %v", state, err)
		}
	})
	t.Run("needs revision carries diagnostic", func(t *testing.T) {
		_, service, runID, _ := guidedReadFixture(t, "needs_revision", true, true, false, false)
		state, err := service.ReadRunAuditState(context.Background(), runID)
		if err != nil || len(state.Diagnostics) != 1 || state.Diagnostics[0] != "run_needs_revision" {
			t.Fatalf("needs-revision diagnostics = %+v, %v", state, err)
		}
	})
}

func TestReadWorkspaceRemediationStateTracksOpenAndReopened(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		_, service, _, workspaceID := guidedReadFixture(t, "needs_revision", true, true, true, false)
		state, err := service.ReadWorkspaceRemediationState(context.Background(), workspaceID)
		if err != nil || state.State != "open" || len(state.SeedIDs) != 1 || state.SeedIDs[0] != "remediation-guided-read" {
			t.Fatalf("open remediation state = %+v, %v", state, err)
		}
	})
	t.Run("reopened", func(t *testing.T) {
		_, service, _, workspaceID := guidedReadFixture(t, "needs_revision", true, true, true, true)
		state, err := service.ReadWorkspaceRemediationState(context.Background(), workspaceID)
		if err != nil || state.State != "reopened" {
			t.Fatalf("reopened remediation state = %+v, %v", state, err)
		}
	})
	t.Run("none without seeds", func(t *testing.T) {
		_, service, _, workspaceID := guidedReadFixture(t, "created", false, false, false, false)
		state, err := service.ReadWorkspaceRemediationState(context.Background(), workspaceID)
		if err != nil || state.State != "none" {
			t.Fatalf("no-seed remediation state = %+v, %v", state, err)
		}
	})
}
