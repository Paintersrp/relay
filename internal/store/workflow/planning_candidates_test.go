package workflowstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

func TestPlanningCandidatePersistenceExactReadAndProductionLink(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	workspaceID, candidateFile, closureID := seedPlanningCandidateBasis(t, ctx, store)
	batch, err := store.ArtifactStore().Begin("feature-discovery/workspace-candidate/candidate-1")
	if err != nil {
		t.Fatal(err)
	}
	candidateBytes := []byte("# Candidate exact bytes\n\x00")
	candidateArtifact, err := batch.Stage("planning_candidate_requirements", "requirements.md", "text/markdown", candidateBytes)
	if err != nil {
		t.Fatal(err)
	}
	deliveryCandidateBytes := []byte("{\"ticket\":true}\n")
	deliveryCandidateArtifact, err := batch.Stage("planning_candidate_delivery_ticket", "ticket.json", "application/json", deliveryCandidateBytes)
	if err != nil {
		t.Fatal(err)
	}
	manifestArtifact, err := batch.Stage("discovery_closure_manifest", "closure.json", "application/vnd.relay.feature-discovery-closure+json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalArtifact, err := batch.Stage("produced_canonical_json", "canonical.json", "application/json", []byte("{\"ticket\":true}\n"))
	if err != nil {
		t.Fatal(err)
	}
	markdownArtifact, err := batch.Stage("produced_rendered_markdown", "rendered.md", "text/markdown", []byte("# Produced\n"))
	if err != nil {
		t.Fatal(err)
	}

	var candidate PlanningCandidate
	var deliveryCandidate PlanningCandidate
	var link DeliveryTicketProductionLink
	if err := store.CommitArtifactBatch(ctx, batch, func(tx *Tx) error {
		candidateArtifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: candidateArtifact.RelativePath, Sha256: candidateArtifact.SHA256, MediaType: candidateArtifact.MediaType, SizeBytes: candidateArtifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		deliveryCandidateArtifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: deliveryCandidateArtifact.RelativePath, Sha256: deliveryCandidateArtifact.SHA256, MediaType: deliveryCandidateArtifact.MediaType, SizeBytes: deliveryCandidateArtifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		manifestArtifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: manifestArtifact.RelativePath, Sha256: manifestArtifact.SHA256, MediaType: manifestArtifact.MediaType, SizeBytes: manifestArtifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		canonicalArtifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: canonicalArtifact.RelativePath, Sha256: canonicalArtifact.SHA256, MediaType: canonicalArtifact.MediaType, SizeBytes: canonicalArtifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		markdownArtifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: markdownArtifact.RelativePath, Sha256: markdownArtifact.SHA256, MediaType: markdownArtifact.MediaType, SizeBytes: markdownArtifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		var revisionID int64
		if err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity) VALUES (?, ?, 1, ?, ?) RETURNING id`, NewFeatureWorkspaceDiscoveryRevisionID(), workspaceID, manifestArtifactRow.ID, "candidate-test").Scan(&revisionID); err != nil {
			return err
		}
		var packetID int64
		if err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES (?, ?, ?, 'requirements', ?, ?, ?, ?) RETURNING id`, NewFeatureWorkspaceDiscoveryClosurePacketID(), workspaceID, revisionID, manifestArtifactRow.ID, manifestArtifact.SHA256, manifestArtifact.SizeBytes, manifestArtifact.MediaType).Scan(&packetID); err != nil {
			return err
		}
		if _, err := tx.tx.ExecContext(ctx, `UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = version + 1 WHERE id = ?`, revisionID, packetID, workspaceID); err != nil {
			return err
		}
		candidate, err = tx.CreatePlanningCandidate(ctx, CreatePlanningCandidateParams{
			CandidateID: NewPlanningCandidateID(), WorkspaceRowID: workspaceID, Family: "requirements", Filename: "requirements.md",
			ArtifactRowID: candidateArtifactRow.ID, ArtifactSha256: candidateArtifact.SHA256, ArtifactSizeBytes: candidateArtifact.SizeBytes,
			DiscoveryClosurePacketRowID: packetID, RepoTarget: "candidate-repo", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: "requirements", CreatedIdentity: "candidate-test",
		})
		if err != nil {
			return err
		}
		deliveryCandidate, err = tx.CreatePlanningCandidate(ctx, CreatePlanningCandidateParams{
			CandidateID: NewPlanningCandidateID(), WorkspaceRowID: workspaceID, Family: "delivery_ticket", Filename: "ticket.json",
			ArtifactRowID: deliveryCandidateArtifactRow.ID, ArtifactSha256: deliveryCandidateArtifact.SHA256, ArtifactSizeBytes: deliveryCandidateArtifact.SizeBytes,
			DiscoveryClosurePacketRowID: packetID, RepoTarget: "candidate-repo", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: "direct_delivery_ticket", CreatedIdentity: "candidate-test",
		})
		if err != nil {
			return err
		}
		ticket, err := tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{TicketID: "P1-T1", WorkspaceRowID: workspaceID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revisionValue, err := tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "candidate-repo", Branch: "main", BaseCommit: strings.Repeat("a", 40),
			SourceClosureRowID: closureID, SourcePath: "tickets/P1-T1.json", Goal: "Produce ticket", Context: "Candidate output", TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		revisionID = revisionValue.ID
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revisionValue.ID); err != nil {
			return err
		}
		link, err = tx.CreateDeliveryTicketProductionLink(ctx, CreateDeliveryTicketProductionLinkParams{
			ProductionLinkID: NewDeliveryTicketProductionLinkID(), DeliveryTicketRowID: ticket.ID, CandidateRowID: candidate.ID,
			CandidateArtifactRowID: candidateArtifactRow.ID, CandidateSha256: candidate.ArtifactSha256, CandidateSizeBytes: candidate.ArtifactSizeBytes,
			CanonicalJsonArtifactRowID: canonicalArtifactRow.ID, CanonicalJsonSha256: canonicalArtifact.SHA256, CanonicalJsonSizeBytes: canonicalArtifact.SizeBytes,
			RenderedMarkdownArtifactRowID: markdownArtifactRow.ID, RenderedMarkdownSha256: markdownArtifact.SHA256, RenderedMarkdownSizeBytes: markdownArtifact.SizeBytes,
			ProducedRevisionRowID: revisionID, ProducedRevisionIdentity: "P1-T1:r1", CreatedIdentity: "compiler-test",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if candidate.ID == 0 || deliveryCandidate.ID == 0 || link.ID == 0 || candidate.WorkspaceRowID != workspaceID || deliveryCandidate.WorkspaceRowID != workspaceID {
		t.Fatalf("created candidate/link = %#v %#v %#v", candidate, deliveryCandidate, link)
	}
	read, err := store.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, 1024)
	if err != nil || !bytes.Equal(read, candidateBytes) {
		t.Fatalf("exact candidate read = %q, %v", read, err)
	}
	readDelivery, err := store.ReadPlanningCandidateBytes(ctx, deliveryCandidate.CandidateID, 1024)
	if err != nil || !bytes.Equal(readDelivery, deliveryCandidateBytes) {
		t.Fatalf("delivery ticket candidate exact read = %q, %v", readDelivery, err)
	}
	readDeliveryCandidate, err := store.GetPlanningCandidateByCandidateID(ctx, deliveryCandidate.CandidateID)
	if err != nil || readDeliveryCandidate.Family != "delivery_ticket" || readDeliveryCandidate.Destination != "direct_delivery_ticket" {
		t.Fatalf("delivery ticket candidate read = %#v, %v", readDeliveryCandidate, err)
	}
	listed, err := store.ListPlanningCandidatesByWorkspace(ctx, workspaceID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("candidate history = %#v, %v", listed, err)
	}
	if got, err := store.GetDeliveryTicketProductionLinkByLinkID(ctx, link.ProductionLinkID); err != nil || got.ProducedRevisionIdentity != "P1-T1:r1" {
		t.Fatalf("production link read = %#v, %v", got, err)
	}

	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreatePlanningCandidateApproval(ctx, CreatePlanningCandidateApprovalParams{
			ApprovalID: NewPlanningCandidateApprovalID(), CandidateRowID: candidate.ID, CandidateArtifactRowID: candidate.ArtifactRowID,
			CandidateSha256: candidate.ArtifactSha256, CandidateSizeBytes: candidate.ArtifactSizeBytes, OperatorConfirmationEvidence: "exact candidate confirmed", CreatedIdentity: "operator-test",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	approvals, err := store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
	if err != nil || len(approvals) != 1 || approvals[0].CandidateSha256 != candidate.ArtifactSha256 {
		t.Fatalf("candidate approvals = %#v, %v", approvals, err)
	}
	if _, err := store.DB().Exec(`UPDATE planning_candidates SET filename='changed' WHERE id=?`, candidate.ID); err == nil {
		t.Fatal("candidate identity was mutable")
	}
	if _, err := store.DB().Exec(`UPDATE delivery_ticket_production_links SET produced_revision_identity='changed' WHERE id=?`, link.ID); err == nil {
		t.Fatal("production link was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM delivery_ticket_production_links WHERE id=?`, link.ID); err == nil {
		t.Fatal("production link was deletable")
	}
	if _, err := store.DB().Exec(`DELETE FROM planning_candidate_approvals WHERE id=?`, approvals[0].ID); err == nil {
		t.Fatal("candidate approval was deletable")
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreatePlanningCandidateApproval(ctx, CreatePlanningCandidateApprovalParams{
			ApprovalID: NewPlanningCandidateApprovalID(), CandidateRowID: candidate.ID, CandidateArtifactRowID: candidate.ArtifactRowID,
			CandidateSha256: strings.Repeat("b", 64), CandidateSizeBytes: candidate.ArtifactSizeBytes, OperatorConfirmationEvidence: "wrong digest", CreatedIdentity: "operator-test",
		})
		return err
	}); err == nil {
		t.Fatal("mismatched candidate approval was accepted")
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreatePlanningCandidate(ctx, CreatePlanningCandidateParams{
			CandidateID: "candidate-rolled-back", WorkspaceRowID: workspaceID, Family: "requirements", Filename: "requirements.md",
			ArtifactRowID: candidate.ArtifactRowID, ArtifactSha256: candidate.ArtifactSha256, ArtifactSizeBytes: candidate.ArtifactSizeBytes,
			DiscoveryClosurePacketRowID: candidate.DiscoveryClosurePacketRowID, RepoTarget: candidate.RepoTarget, Branch: candidate.Branch, BaseCommit: candidate.BaseCommit, Destination: candidate.Destination, CreatedIdentity: "rollback-test",
		})
		if err != nil {
			return err
		}
		return errors.New("intentional rollback")
	}); err == nil {
		t.Fatal("expected candidate transaction rollback")
	}
	if _, err := store.GetPlanningCandidateByCandidateID(ctx, "candidate-rolled-back"); err == nil {
		t.Fatal("rolled-back candidate persisted")
	}

	artifactPath := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(candidateFile))
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadPlanningCandidateBytes(ctx, candidate.CandidateID, 1024); err == nil {
		t.Fatal("tampered candidate bytes were returned")
	}
}

func TestPlanningCandidateReviewPersistenceIsNarrowImmutableFact(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	workspaceID, _, _ := seedPlanningCandidateBasis(t, ctx, store)
	batch, err := store.ArtifactStore().Begin("feature-discovery/workspace-candidate/candidate-review")
	if err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# Reviewable candidate\n")
	artifact, err := batch.Stage("planning_candidate_requirements", "requirements.md", "text/markdown", bytes)
	if err != nil {
		t.Fatal(err)
	}
	manifestArtifact, err := batch.Stage("discovery_closure_manifest", "closure.json", "application/vnd.relay.feature-discovery-closure+json", []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var candidate PlanningCandidate
	if err := store.CommitArtifactBatch(ctx, batch, func(tx *Tx) error {
		artifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: artifact.RelativePath, Sha256: artifact.SHA256, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		manifestArtifactRow, err := tx.CreateFeatureWorkspaceDiscoveryArtifact(ctx, CreateFeatureWorkspaceDiscoveryArtifactParams{
			DiscoveryArtifactID: NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspaceID,
			RelativePath: manifestArtifact.RelativePath, Sha256: manifestArtifact.SHA256, MediaType: manifestArtifact.MediaType, SizeBytes: manifestArtifact.SizeBytes,
		})
		if err != nil {
			return err
		}
		var revisionID int64
		if err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity) VALUES (?, ?, 1, ?, 'candidate-test') RETURNING id`, NewFeatureWorkspaceDiscoveryRevisionID(), workspaceID, manifestArtifactRow.ID).Scan(&revisionID); err != nil {
			return err
		}
		var packetID int64
		if err := tx.tx.QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES (?, ?, ?, 'requirements', ?, ?, ?, ?) RETURNING id`, NewFeatureWorkspaceDiscoveryClosurePacketID(), workspaceID, revisionID, manifestArtifactRow.ID, manifestArtifact.SHA256, manifestArtifact.SizeBytes, manifestArtifact.MediaType).Scan(&packetID); err != nil {
			return err
		}
		if _, err := tx.tx.ExecContext(ctx, `UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = version + 1 WHERE id = ?`, revisionID, packetID, workspaceID); err != nil {
			return err
		}
		candidate, err = tx.CreatePlanningCandidate(ctx, CreatePlanningCandidateParams{
			CandidateID: NewPlanningCandidateID(), WorkspaceRowID: workspaceID, Family: "requirements", Filename: "requirements.md",
			ArtifactRowID: artifactRow.ID, ArtifactSha256: artifact.SHA256, ArtifactSizeBytes: artifact.SizeBytes,
			DiscoveryClosurePacketRowID: packetID, RepoTarget: "candidate-repo", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: "requirements", CreatedIdentity: "candidate-test",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var review PlanningCandidateReview
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		review, err = tx.CreatePlanningCandidateReview(ctx, CreatePlanningCandidateReviewParams{
			ReviewID: NewPlanningCandidateReviewID(), CandidateRowID: candidate.ID, ReviewerIdentity: "auditor", Disposition: "ready_for_approval",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if review.CandidateRowID != candidate.ID || review.ReviewerIdentity != "auditor" || review.Disposition != "ready_for_approval" || review.CompletedAt == "" {
		t.Fatalf("created review = %#v", review)
	}
	if read, err := store.GetPlanningCandidateReviewByReviewID(ctx, review.ReviewID); err != nil || read.ID != review.ID {
		t.Fatalf("review readback = %#v, %v", read, err)
	}
	if read, err := store.GetPlanningCandidateReviewByCandidateRowID(ctx, candidate.ID); err != nil || read.ReviewID != review.ReviewID {
		t.Fatalf("candidate review readback = %#v, %v", read, err)
	}
	// A review binds at most one candidate, mirrors the exact candidate, and is
	// retained immutable history with no outcome or verdict columns.
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreatePlanningCandidateReview(ctx, CreatePlanningCandidateReviewParams{
			ReviewID: NewPlanningCandidateReviewID(), CandidateRowID: candidate.ID, ReviewerIdentity: "second", Disposition: "needs_revision",
		})
		return err
	}); err == nil {
		t.Fatal("second review for the same candidate was accepted")
	}
	if _, err := store.DB().Exec(`UPDATE planning_candidate_reviews SET reviewer_identity='changed' WHERE id=?`, review.ID); err == nil {
		t.Fatal("candidate review was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM planning_candidate_reviews WHERE id=?`, review.ID); err == nil {
		t.Fatal("candidate review was deletable")
	}
}

func seedPlanningCandidateBasis(t *testing.T, ctx context.Context, store *Store) (int64, string, int64) {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('candidate-repo', 'C:/candidate-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var projectID, workspaceID, closureID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-candidate', 'Candidate') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	workspace, err := workflowgenerated.New(store.DB()).CreateFeatureWorkspace(ctx, workflowgenerated.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-candidate", ProjectRowID: projectID, FeatureSlug: "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID = workspace.ID
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-candidate', 'candidate-repo', 'source-vaults/candidate')`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-candidate', (SELECT id FROM source_vaults WHERE vault_id='vault-candidate'), ?, ?, 1, 'refs/relay/closures/candidate', 'ready', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z') RETURNING id`, strings.Repeat("a", 40), strings.Repeat("b", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	return workspaceID, "feature-discovery/workspace-candidate/candidate-1/requirements.md", closureID
}
