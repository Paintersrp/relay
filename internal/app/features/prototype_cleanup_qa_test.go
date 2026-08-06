package features

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

func TestPrototypeQAPersistenceBoundsAndRedaction(t *testing.T) {
	ctx, store, service, workspace, _, proposal, authorization, run := preparedPrototype(t)
	approval, approved, err := service.ApprovePrototypeExecution(ctx, approvalInput(workspace, proposal, authorization, run))
	if err != nil {
		t.Fatal(err)
	}
	if approval.ID == 0 || approved.Version != run.Version+1 {
		t.Fatalf("approval=%#v approved=%#v", approval, approved)
	}
	var runtime workflowstore.PrototypeRuntime
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		runtime = workflowstore.PrototypeRuntime{
			RuntimeID: "prototype-runtime-qa", AuthorizedCommit: authorization.SourceCommit, AuthorizedTree: authorization.SourceTree,
			RuntimeRootPath: t.TempDir(), WorktreePath: t.TempDir(), EphemeralTargetKey: "prototype:qa", LeaseToken: "prototype-lease-qa",
			BackgroundContextID: "prototype-context-qa", InvocationRelativePath: ".relay/prototype/invocation.json", ResultRelativePath: ".relay/prototype/result.json", ExportRelativePath: ".relay/prototype/export", DeadlineAt: "2026-08-05T00:00:00Z",
		}
		target := workflowstore.PrototypeTarget{TargetID: "prototype-target-qa", TargetKey: runtime.EphemeralTargetKey, WorktreePath: runtime.WorktreePath, AuthorizedCommit: runtime.AuthorizedCommit, AuthorizedTree: runtime.AuthorizedTree}
		lease := workflowstore.PrototypeLease{LeaseToken: runtime.LeaseToken, EphemeralTargetKey: runtime.EphemeralTargetKey, OwnerInstanceID: "qa-owner"}
		_, runtime, target, lease, err = tx.ReservePrototypeRuntime(ctx, run.PrototypeRunID, approved.Version, runtime, target, lease)
		if err != nil {
			return err
		}
		artifact, err := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{
			DiscoveryArtifactID: "discovery-artifact-qa-result-feature", WorkspaceRowID: workspace.ID,
			RelativePath: "feature-discovery/workspace-prototype/qa/result.json", SHA256: strings.Repeat("a", 64), MediaType: "application/json", SizeBytes: 12,
		})
		if err != nil {
			return err
		}
		batch, err := tx.CreatePrototypeEvidenceImportBatch(ctx, workflowstore.PrototypeEvidenceImportBatch{
			EvidenceBatchID: "prototype-evidence-batch-qa", RunRowID: approved.ID, RuntimeRowID: runtime.ID,
			BatchIdentity: "qa-batch", SettlementCause: "runner_success", ObservationIdentity: "qa-observation",
			ProcessOutcome: "succeeded", EnvelopeStatus: "valid", Completeness: "complete", ArtifactCount: 1, TotalSizeBytes: artifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		if _, err = tx.CreatePrototypeResult(ctx, workflowstore.PrototypeResult{
			ResultID: "prototype-result-qa", RunRowID: approved.ID, RuntimeRowID: runtime.ID, EvidenceBatchRowID: batch.ID,
			ArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true}, ValidationStatus: "valid", ProcessExitCode: sql.NullInt64{Int64: 0, Valid: true},
			ProcessOutcome: "succeeded", EnvelopeSHA256: sql.NullString{String: strings.Repeat("a", 64), Valid: true},
		}); err != nil {
			return err
		}
		for _, kind := range []string{"process_ownership", "evidence_settlement", "worktree", "ephemeral_target", "prototype_lease"} {
			if _, err = tx.CompletePrototypeCleanupObligation(ctx, approved.ID, kind); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='closed' WHERE id=?`, approved.ID); err != nil {
		t.Fatal(err)
	}
	closed, err := store.GetPrototypeRun(ctx, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := service.PrepareQADiscoveryPacket(ctx, PrepareQADiscoveryPacketInput{
		WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ExpectedRunVersion: closed.Version,
		MutationIdentity: "qa-packet-feature", OperatorPrompt: "Review the prototype result.", ValidationInstructions: []string{"Confirm the result envelope."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if packet.Packet.Status != "prepared" || packet.Packet.MemberCount != int64(len(packet.Members)) || len(packet.Members) != 3 {
		t.Fatalf("packet=%#v members=%#v", packet.Packet, packet.Members)
	}
	for _, member := range packet.Members {
		if member.MemberKind == "prototype_result" && member.SHA256 != strings.Repeat("a", 64) {
			t.Fatalf("result member digest=%q", member.SHA256)
		}
	}
	secret := []byte("Authorization: Bearer redacted-test-secret")
	secretSum := sha256.Sum256(secret)
	_, err = service.AdmitOperatorQAEvidence(ctx, AdmitOperatorQAEvidenceInput{
		WorkspaceID: workspace.WorkspaceID, QAPacketID: packet.Packet.QAPacketID, MutationIdentity: "qa-evidence-secret",
		OperatorConfirmationEvidence: "confirmed", Evidence: []OperatorQAEvidenceInput{{SemanticRole: "operator-note", MediaType: "text/plain", Content: secret, SHA256: hex.EncodeToString(secretSum[:])}},
	})
	if !errors.Is(err, prototypeexecution.ErrQAEvidenceInvalid) {
		t.Fatalf("secret evidence error=%v", err)
	}
	content := []byte("Operator confirmed the bounded review.")
	digest := sha256.Sum256(content)
	admitted, err := service.AdmitOperatorQAEvidence(ctx, AdmitOperatorQAEvidenceInput{
		WorkspaceID: workspace.WorkspaceID, QAPacketID: packet.Packet.QAPacketID, MutationIdentity: "qa-evidence-feature",
		OperatorConfirmationEvidence: "confirmed", Evidence: []OperatorQAEvidenceInput{{SemanticRole: "operator-note", MediaType: "text/plain", Content: content, SHA256: hex.EncodeToString(digest[:])}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if admitted.Packet.Status != "admitted" || admitted.Admission == nil || len(admitted.Evidence) != 1 || admitted.Evidence[0].SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("admitted packet=%#v", admitted)
	}
	view, err := service.ReadPrototypeEvidenceForWayfinder(ctx, workspace.WorkspaceID, run.PrototypeRunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.QAPackets) != 1 || view.QAPackets[0].Admission == nil || view.QAPackets[0].Packet.QAPacketID != packet.Packet.QAPacketID {
		t.Fatalf("Wayfinder QA projection=%#v", view.QAPackets)
	}
}

func TestPrototypeAnotherExecutionUsesFreshDurableIdentities(t *testing.T) {
	ctx, store, service, workspace, _, proposal, authorization, prior := preparedPrototype(t)
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspace_prototype_runs SET lifecycle_state='closed' WHERE id=?`, prior.ID); err != nil {
		t.Fatal(err)
	}
	created, err := service.PrepareAnotherPrototypeExecution(ctx, PrepareAnotherPrototypeExecutionInput{
		WorkspaceID: workspace.WorkspaceID, PriorRunID: prior.PrototypeRunID, ExpectedPriorRunVersion: prior.Version,
		MutationIdentity: "another-execution-one", OperatorConfirmationEvidence: "operator confirmed retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Run.PrototypeRunID == prior.PrototypeRunID || created.Run.AuthorizationRowID == prior.AuthorizationRowID || created.Run.LifecycleState != "approved" {
		t.Fatalf("new execution reused prior identity: prior=%#v new=%#v", prior, created.Run)
	}
	var proposalCount, authorizationCount, runCount, approvalCount int
	for table := range map[string]struct{}{"feature_workspace_prototype_proposals": {}, "feature_workspace_prototype_authorizations": {}, "feature_workspace_prototype_runs": {}, "feature_workspace_prototype_approvals": {}} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		switch table {
		case "feature_workspace_prototype_proposals":
			proposalCount = count
		case "feature_workspace_prototype_authorizations":
			authorizationCount = count
		case "feature_workspace_prototype_runs":
			runCount = count
		case "feature_workspace_prototype_approvals":
			approvalCount = count
		}
	}
	if proposalCount != 2 || authorizationCount != 2 || runCount != 2 || approvalCount != 1 {
		t.Fatalf("identity counts proposals=%d authorizations=%d runs=%d approvals=%d", proposalCount, authorizationCount, runCount, approvalCount)
	}
	replayed, err := service.PrepareAnotherPrototypeExecution(ctx, PrepareAnotherPrototypeExecutionInput{
		WorkspaceID: workspace.WorkspaceID, PriorRunID: prior.PrototypeRunID, ExpectedPriorRunVersion: prior.Version,
		MutationIdentity: "another-execution-one", OperatorConfirmationEvidence: "operator confirmed retry",
	})
	if err != nil || replayed.Run.PrototypeRunID != created.Run.PrototypeRunID {
		t.Fatalf("another execution replay=%#v err=%v", replayed, err)
	}
	_ = proposal
	_ = authorization
}

func TestPrototypeAnotherExecutionEligibilityIsBounded(t *testing.T) {
	ctx, store, service, workspace, _, _, _, prior := preparedPrototype(t)
	_, err := service.PrepareAnotherPrototypeExecution(ctx, PrepareAnotherPrototypeExecutionInput{
		WorkspaceID: workspace.WorkspaceID, PriorRunID: prior.PrototypeRunID, ExpectedPriorRunVersion: prior.Version,
		MutationIdentity: "another-ineligible", OperatorConfirmationEvidence: "confirmed",
	})
	if !errors.Is(err, prototypeexecution.ErrAnotherExecutionIneligible) {
		t.Fatalf("proposed prior run error=%v", err)
	}
	var runs int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_prototype_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("ineligible request created %d runs", runs)
	}
}
