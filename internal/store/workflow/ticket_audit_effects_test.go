package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	relaydb "relay/internal/db"
	workflowgenerated "relay/internal/store/workflowgenerated"

	"github.com/pressly/goose/v3"
)

func TestTicketAuditSatisfactionIsExactImmutableAndTransactional(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	record := seedTicketAuditEffect(t, ctx, store, "accepted")

	errInjected := errors.New("injected accepted audit effect rollback")
	err := store.WithTx(ctx, func(tx *Tx) error {
		if _, err := workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionSatisfaction(ctx, workflowgenerated.CreateDeliveryTicketRevisionSatisfactionParams{
			DeliveryTicketRevisionRowID:      record.inputs.revision.ID,
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
		}); err != nil {
			return err
		}
		return errInjected
	})
	if !errors.Is(err, errInjected) {
		t.Fatalf("accepted audit effect transaction error = %v, want injected rollback", err)
	}
	assertWorkflowCount(t, store.DB(), "delivery_ticket_revision_satisfactions", 0)

	var satisfaction workflowgenerated.DeliveryTicketRevisionSatisfaction
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		satisfaction, err = workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionSatisfaction(ctx, workflowgenerated.CreateDeliveryTicketRevisionSatisfactionParams{
			DeliveryTicketRevisionRowID:      record.inputs.revision.ID,
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if satisfaction.DeliveryTicketRevisionRowID != record.inputs.revision.ID || satisfaction.AuditTicketRevisionDecisionRowID != record.revisionDecision.ID {
		t.Fatalf("stored exact ticket satisfaction = %#v", satisfaction)
	}
	if _, err := store.DB().Exec(`UPDATE delivery_ticket_revision_satisfactions SET delivery_ticket_revision_row_id = ? WHERE id = ?`, record.inputs.revision.ID, satisfaction.ID); err == nil {
		t.Fatal("ticket satisfaction was mutable")
	}
	if _, err := store.DB().Exec(`UPDATE audit_decisions SET rationale = 'rewritten' WHERE id = ?`, record.decision.ID); err == nil {
		t.Fatal("audit decision was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM audit_packet_ticket_obligations WHERE id = ?`, record.obligation.ID); err == nil {
		t.Fatal("audit packet ticket obligation was deletable")
	}
}

func TestTicketAuditAcceptanceCannotSatisfyAReplacedRevision(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	record := seedTicketAuditEffect(t, ctx, store, "accepted")
	createReplacementRevision(t, ctx, store, record.inputs)

	err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionSatisfaction(ctx, workflowgenerated.CreateDeliveryTicketRevisionSatisfactionParams{
			DeliveryTicketRevisionRowID:      record.inputs.revision.ID,
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
		})
		return err
	})
	if err == nil {
		t.Fatal("accepted audit decision satisfied a replaced ticket revision")
	}
	assertWorkflowCount(t, store.DB(), "delivery_ticket_revision_satisfactions", 0)
}

func TestTicketAuditRemediationSeedIsUniqueClassifiedAndReopenable(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	record := seedTicketAuditEffect(t, ctx, store, "needs_revision")

	var remediation workflowgenerated.AuditRemediationSeed
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		queries := workflowgenerated.New(tx.tx)
		remediation, err = queries.CreateAuditRemediationSeed(ctx, workflowgenerated.CreateAuditRemediationSeedParams{
			RemediationSeedID:                "remediation-package-1",
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
			AuditPacketRowID:                 record.packet.ID,
			ExecutionPackageRowID:            record.executionPackage.ID,
			AuditedCommit:                    record.packet.AuditedCommit,
			DecisionRationale:                record.decision.Rationale,
		})
		if err != nil {
			return err
		}
		finding, err := queries.CreateAuditRemediationSeedFinding(ctx, workflowgenerated.CreateAuditRemediationSeedFindingParams{
			RemediationSeedRowID:   remediation.ID,
			Sequence:               1,
			UpstreamClassification: "execution_spec",
			Summary:                "The execution specification omitted required persistence coverage.",
			Evidence:               "The packet validation evidence does not exercise the required audit binding.",
			RequiredRemediation:    "Create a current remediation ticket with complete persistence proof.",
		})
		if err != nil {
			return err
		}
		if finding.UpstreamClassification != "execution_spec" {
			return errors.New("remediation finding lost its upstream classification")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateAuditRemediationSeed(ctx, workflowgenerated.CreateAuditRemediationSeedParams{
			RemediationSeedID:                "remediation-package-duplicate",
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
			AuditPacketRowID:                 record.packet.ID,
			ExecutionPackageRowID:            record.executionPackage.ID,
			AuditedCommit:                    record.packet.AuditedCommit,
			DecisionRationale:                record.decision.Rationale,
		})
		return err
	}); err == nil {
		t.Fatal("multiple remediation seeds were accepted for one ticket revision decision")
	}
	if _, err := store.DB().Exec(`UPDATE audit_remediation_seed_findings SET summary = 'rewritten' WHERE remediation_seed_row_id = ?`, remediation.ID); err == nil {
		t.Fatal("remediation seed finding was mutable")
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionSatisfaction(ctx, workflowgenerated.CreateDeliveryTicketRevisionSatisfactionParams{
			DeliveryTicketRevisionRowID:      record.inputs.revision.ID,
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
		})
		return err
	}); err == nil {
		t.Fatal("needs-revision decision satisfied a ticket")
	}

	replacement := createReplacementRevision(t, ctx, store, record.inputs)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateAuditRemediationSeedReopening(ctx, workflowgenerated.CreateAuditRemediationSeedReopeningParams{
			RemediationSeedRowID:   remediation.ID,
			ReopeningRevisionRowID: replacement.ID,
			ReopeningKind:          "replacement_ticket_revision",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureWorkspaceCompletionIsExplicitAndReopensOnCurrentReplacement(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	record := seedTicketAuditEffect(t, ctx, store, "accepted")

	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateDeliveryTicketRevisionSatisfaction(ctx, workflowgenerated.CreateDeliveryTicketRevisionSatisfactionParams{
			DeliveryTicketRevisionRowID:      record.inputs.revision.ID,
			AuditTicketRevisionDecisionRowID: record.revisionDecision.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var completion workflowgenerated.FeatureWorkspaceCompletionDecision
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		completion, err = workflowgenerated.New(tx.tx).CreateFeatureWorkspaceCompletionDecision(ctx, workflowgenerated.CreateFeatureWorkspaceCompletionDecisionParams{
			CompletionDecisionID:   "completion-package-1",
			WorkspaceRowID:         record.inputs.workspace.ID,
			AuthorityRevisionRowID: record.inputs.authority.ID,
			SourceClosureRowID:     record.inputs.closure.ID,
			Decision:               "completed",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if completion.Decision != "completed" {
		t.Fatalf("feature completion decision = %#v", completion)
	}
	if _, err := store.DB().Exec(`UPDATE feature_workspace_completion_decisions SET decision = 'completed' WHERE id = ?`, completion.ID); err == nil {
		t.Fatal("feature completion decision was mutable")
	}

	replacement := createReplacementRevision(t, ctx, store, record.inputs)
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateFeatureWorkspaceCompletionReopening(ctx, workflowgenerated.CreateFeatureWorkspaceCompletionReopeningParams{
			CompletionDecisionRowID:         completion.ID,
			ReopeningKind:                   "ticket_revision",
			ReopeningTicketRevisionRowID:    sql.NullInt64{Int64: replacement.ID, Valid: true},
			ReopeningAuthorityRevisionRowID: sql.NullInt64{},
			ReopeningRemediationSeedRowID:   sql.NullInt64{},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := workflowgenerated.New(store.DB()).GetCurrentFeatureWorkspaceCompletionDecision(ctx, record.inputs.workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("current completion after reopening error = %v, want sql.ErrNoRows", err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := workflowgenerated.New(tx.tx).CreateFeatureWorkspaceCompletionDecision(ctx, workflowgenerated.CreateFeatureWorkspaceCompletionDecisionParams{
			CompletionDecisionID:   "completion-package-2",
			WorkspaceRowID:         record.inputs.workspace.ID,
			AuthorityRevisionRowID: record.inputs.authority.ID,
			SourceClosureRowID:     record.inputs.closure.ID,
			Decision:               "completed",
		})
		return err
	}); err == nil {
		t.Fatal("feature completion ignored the unfinished reopening revision")
	}
}

func TestTicketAuditEffectsMigrationPreservesExistingAuditHistory(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "workflow.sqlite")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(relaydb.WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, "workflow_migrations", 14); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	packetHash := strings.Repeat("b", 64)
	if _, err := database.Exec(`INSERT INTO repository_targets (repo_target, local_path) VALUES ('relay', '/repo')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO runs (run_id, feature_slug, repo_target, status, branch, base_commit, canonical_sha256)
VALUES ('run-audit-history', 'audit-history', 'relay', 'created', 'main', ?, ?)`, commit, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	for _, transition := range [][2]string{{"created", "setup_ready"}, {"setup_ready", "executing"}, {"executing", "validating"}, {"validating", "audit_ready"}} {
		if _, err := database.Exec(`UPDATE runs SET status = ? WHERE run_id = ? AND status = ?`, transition[1], "run-audit-history", transition[0]); err != nil {
			t.Fatal(err)
		}
	}
	var runRowID int64
	if err := database.QueryRow(`SELECT id FROM runs WHERE run_id = 'run-audit-history'`).Scan(&runRowID); err != nil {
		t.Fatal(err)
	}
	var artifactRowID int64
	if err := database.QueryRow(`
INSERT INTO artifacts (artifact_id, owner_type, run_row_id, kind, relative_path, media_type, sha256, size_bytes)
VALUES ('artifact-audit-history', 'run', ?, 'audit_packet', 'runs/run-audit-history/audit-packet.json', 'application/json', ?, 1)
RETURNING id`, runRowID, packetHash).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO audit_packets (
    audit_packet_id, run_row_id, implementation_actor_kind, artifact_row_id,
    base_commit, audited_commit, packet_sha256, status
)
VALUES ('packet-audit-history', ?, 'applier', ?, ?, ?, ?, 'current')`, runRowID, artifactRowID, commit, commit, packetHash); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO audit_decisions (
    audit_decision_id, run_row_id, audit_packet_artifact_row_id,
    audited_commit, packet_sha256, decision, rationale
)
VALUES ('audit-history', ?, ?, ?, ?, 'accepted', 'Historical audit decision remains evidence.')`, runRowID, artifactRowID, commit, packetHash); err != nil {
		t.Fatal(err)
	}

	if err := relaydb.AutoMigrateWorkflow(database); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_decisions WHERE audit_decision_id = 'audit-history'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("historical audit decision count after upgrade = %d, want 1", count)
	}
	if _, err := database.Exec(`UPDATE audit_decisions SET rationale = 'rewritten' WHERE audit_decision_id = 'audit-history'`); err == nil {
		t.Fatal("upgraded historical audit decision was mutable")
	}
	if err := goose.DownTo(database, "workflow_migrations", 14); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit_decisions WHERE audit_decision_id = 'audit-history'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("historical audit decision count after rollback = %d, want 1", count)
	}
}

type ticketAuditEffectRecord struct {
	inputs           executionPackageSeed
	executionPackage workflowgenerated.ExecutionPackage
	packet           AuditPacket
	decision         AuditDecision
	obligation       workflowgenerated.AuditPacketTicketObligation
	revisionDecision workflowgenerated.AuditTicketRevisionDecision
}

func seedTicketAuditEffect(t *testing.T, ctx context.Context, store *Store, decision string) ticketAuditEffectRecord {
	t.Helper()
	inputs := seedExecutionPackageInputs(t, ctx, store)
	record := ticketAuditEffectRecord{inputs: inputs}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		queries := workflowgenerated.New(tx.tx)
		var err error
		record.executionPackage, err = createExecutionPackage(ctx, queries, inputs)
		if err != nil {
			return err
		}
		member, err := queries.CreateExecutionPackageMember(ctx, workflowgenerated.CreateExecutionPackageMemberParams{
			PackageRowID:         record.executionPackage.ID,
			SelectionMemberRowID: inputs.selectionMember.ID,
			Sequence:             1,
			RevisionRowID:        inputs.revision.ID,
			MemberSha256:         executionPackageHash('4'),
		})
		if err != nil {
			return err
		}
		if _, err := queries.CreateExecutionPackageApprovalBinding(ctx, workflowgenerated.CreateExecutionPackageApprovalBindingParams{
			PackageRowID:           record.executionPackage.ID,
			PackageMemberRowID:     member.ID,
			ApprovalRowID:          inputs.approval.ID,
			AuthorityRevisionRowID: inputs.authority.ID,
			SourceClosureRowID:     inputs.closure.ID,
			ApprovalBasisSha256:    executionPackageHash('5'),
		}); err != nil {
			return err
		}
		if _, err := queries.ConsumeDeliveryTicketSelection(ctx, inputs.selection.SelectionID); err != nil {
			return err
		}
		run, err := tx.CreateRun(ctx, CreateRunParams{
			RunID:           "run-ticket-audit",
			FeatureSlug:     "package-test",
			RepoTarget:      "relay",
			Status:          "created",
			Branch:          "main",
			BaseCommit:      inputs.closure.CommitOID,
			CanonicalSHA256: executionPackageHash('6'),
		})
		if err != nil {
			return err
		}
		if _, err := tx.LinkRunToExecutionPackage(ctx, run.RunID, record.executionPackage.ID); err != nil {
			return err
		}
		for _, transition := range [][2]string{{"created", "setup_ready"}, {"setup_ready", "executing"}, {"executing", "validating"}, {"validating", "audit_ready"}} {
			if _, err := tx.TransitionRun(ctx, run.RunID, transition[0], transition[1]); err != nil {
				return err
			}
		}
		artifact, err := tx.CreateArtifact(ctx, CreateArtifactParams{
			ArtifactID:   "artifact-ticket-audit",
			OwnerType:    "run",
			RunRowID:     sql.NullInt64{Int64: run.ID, Valid: true},
			Kind:         "audit_packet",
			RelativePath: "runs/run-ticket-audit/audit-packet.json",
			MediaType:    "application/json",
			SHA256:       executionPackageHash('7'),
			SizeBytes:    1,
		})
		if err != nil {
			return err
		}
		record.packet, err = tx.CreateAuditPacket(ctx, CreateAuditPacketParams{
			AuditPacketID:           "packet-ticket-audit",
			RunRowID:                run.ID,
			ImplementationActorKind: "applier",
			ArtifactRowID:           artifact.ID,
			BaseCommit:              inputs.closure.CommitOID,
			AuditedCommit:           strings.Repeat("d", 40),
			PacketSHA256:            executionPackageHash('7'),
		})
		if err != nil {
			return err
		}
		record.obligation, err = queries.CreateAuditPacketTicketObligation(ctx, workflowgenerated.CreateAuditPacketTicketObligationParams{
			AuditPacketRowID:            record.packet.ID,
			ExecutionPackageRowID:       record.executionPackage.ID,
			ExecutionPackageMemberRowID: member.ID,
			DeliveryTicketRowID:         inputs.ticket.ID,
			DeliveryTicketRevisionRowID: inputs.revision.ID,
			AuthorityRevisionRowID:      inputs.authority.ID,
			SourceClosureRowID:          inputs.closure.ID,
		})
		if err != nil {
			return err
		}
		record.decision, err = tx.CreateAuditDecision(ctx, CreateAuditDecisionParams{
			AuditDecisionID:          "audit-ticket-audit",
			RunRowID:                 run.ID,
			AuditPacketArtifactRowID: artifact.ID,
			AuditedCommit:            record.packet.AuditedCommit,
			PacketSHA256:             record.packet.PacketSHA256,
			Decision:                 decision,
			Rationale:                "Recorded against the exact package ticket obligation.",
		})
		if err != nil {
			return err
		}
		record.revisionDecision, err = queries.CreateAuditTicketRevisionDecision(ctx, workflowgenerated.CreateAuditTicketRevisionDecisionParams{
			AuditDecisionRowID:               record.decision.ID,
			AuditPacketTicketObligationRowID: record.obligation.ID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return record
}

func createReplacementRevision(t *testing.T, ctx context.Context, store *Store, inputs executionPackageSeed) DeliveryTicketRevision {
	t.Helper()
	var replacement DeliveryTicketRevision
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		replacement, err = tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID:     inputs.ticket.ID,
			RevisionNumber:          inputs.revision.RevisionNumber + 1,
			ReplacesRevisionRowID:   sql.NullInt64{Int64: inputs.revision.ID, Valid: true},
			RepoTarget:              "relay",
			Branch:                  "main",
			BaseCommit:              inputs.closure.CommitOID,
			SourceClosureRowID:      inputs.closure.ID,
			SourcePath:              "tickets/p5-t1.r2.delivery-ticket.json",
			Goal:                    "Replace the audited ticket with a current remediation definition.",
			Context:                 "The earlier revision remains immutable historical evidence.",
			TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		_, err = tx.SetDeliveryTicketCurrentRevision(ctx, inputs.ticket.TicketID, replacement.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return replacement
}
