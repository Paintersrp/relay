package features

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestPlanningCandidateRequirementsAndSharedDesignPromotionSequences(t *testing.T) {
	cases := []struct {
		name        string
		destination DiscoveryDestination
		families    []string
		wantLayers  []string
	}{
		{name: "requirements-only", destination: DiscoveryDestinationRequirements, families: []string{CandidateFamilyRequirements}, wantLayers: []string{CandidateFamilyRequirements}},
		{name: "shared-design-only", destination: DiscoveryDestinationSharedDesign, families: []string{CandidateFamilySharedDesign}, wantLayers: []string{CandidateFamilySharedDesign}},
		{name: "requirements-then-shared-design", destination: DiscoveryDestinationRequirementsThenSharedDesign, families: []string{CandidateFamilyRequirements, CandidateFamilySharedDesign}, wantLayers: []string{CandidateFamilyRequirements, CandidateFamilySharedDesign}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, test.destination)
			var err error
			if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: test.destination, CreatedIdentity: "operator"}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo', 'C:/planning-repo', 'refs/heads/main', 1)`); err != nil {
				t.Fatal(err)
			}
			closure := insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-repo", strings.Repeat("a", 40))
			for index, family := range test.families {
				bytes := []byte("# " + family + " candidate\nexact\x00bytes\n")
				filename := workspace.FeatureSlug + ".requirements.md"
				if family == CandidateFamilySharedDesign {
					filename = workspace.FeatureSlug + ".design.md"
				}
				candidateResult, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
					WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: family,
					Filename: filename, Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-repo",
					Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: test.destination, CreatedIdentity: "planner",
				})
				if err != nil {
					t.Fatal(err)
				}
				if candidateResult.Candidate.ArtifactSha256 != discoveryTestDigest(bytes) || candidateResult.Candidate.ArtifactSizeBytes != int64(len(bytes)) || candidateResult.AuthorizedNextAction != "approve_candidate" {
					t.Fatalf("candidate admission = %#v", candidateResult)
				}
				completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
				approvalResult, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
					CandidateID: candidateResult.Candidate.CandidateID, ExpectedSHA256: candidateResult.Candidate.ArtifactSha256,
					ExpectedSizeBytes: candidateResult.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version,
					ExpectedClosurePacketRowID:     sql.NullInt64{Int64: workspace.CurrentDiscoveryClosurePacketRowID.Int64, Valid: true},
					ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "exact candidate confirmed", CreatedIdentity: "auditor",
				})
				if err != nil {
					t.Fatal(err)
				}
				beforeAuthority, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
				if err != nil {
					t.Fatal(err)
				}
				if index == 0 && len(beforeAuthority) != 0 && len(test.families) == 1 {
					t.Fatalf("candidate approval promoted authority: %#v", beforeAuthority)
				}
				promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidateResult.Candidate.CandidateID, ApprovalID: approvalResult.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
				if err != nil {
					t.Fatal(err)
				}
				workspace = promoted.Workspace
				if !promoted.Detail.Revision.SourceClosureRowID.Valid || promoted.Detail.Revision.SourceClosureRowID.Int64 != closure.ID {
					t.Fatalf("authority source closure = %#v, want %d", promoted.Detail.Revision, closure.ID)
				}
				currentness, err := service.EvaluateCurrentness(ctx, workspace.WorkspaceID)
				if err != nil || currentness.Readiness != FeatureCurrent {
					t.Fatalf("promoted authority currentness = %#v, %v", currentness, err)
				}
				if err := requireCurrentnessForProgression(ctx, store, workspace); err != nil {
					t.Fatalf("promoted authority progression gate = %v", err)
				}
			}
			history, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
			if err != nil || len(history) != len(test.families) {
				t.Fatalf("authority history = %#v, %v", history, err)
			}
			layers := history[len(history)-1].Layers
			if len(layers) != len(test.wantLayers) {
				t.Fatalf("authority layers = %#v", layers)
			}
			for index, want := range test.wantLayers {
				if layers[index].Sequence != int64(index+1) || layers[index].LayerKind != want || !layers[index].CandidateArtifactRowID.Valid {
					t.Fatalf("layer %d = %#v", index, layers[index])
				}
				if layers[index].SourceClosureRowID.Valid {
					t.Fatalf("layer %d unexpectedly supplied source closure mask = %#v", index, layers[index])
				}
			}
			if len(history) > 1 && history[0].Layers[0].LayerKind != CandidateFamilyRequirements {
				t.Fatalf("historical authority order = %#v", history)
			}
		})
	}
}

func TestPlanningCandidateAdmissionRejectsDeliveryAndInvalidInputs(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-invalid', 'C:/planning-repo-invalid', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# Requirements\n")
	base := CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-repo-invalid", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"}
	if _, err := service.AdmitPlanningCandidate(ctx, func() CandidateAdmissionInput { value := base; value.Family = "unsupported_family"; return value }()); !errors.Is(err, ErrInvalidCandidateFamily) {
		t.Fatalf("unsupported candidate error = %v", err)
	}
	for name, input := range map[string]CandidateAdmissionInput{
		"digest":   func() CandidateAdmissionInput { value := base; value.SHA256 = strings.Repeat("0", 64); return value }(),
		"filename": func() CandidateAdmissionInput { value := base; value.Filename = "wrong.md"; return value }(),
		"destination": func() CandidateAdmissionInput {
			value := base
			value.Destination = DiscoveryDestinationSharedDesign
			return value
		}(),
		"workspace": func() CandidateAdmissionInput { value := base; value.WorkspaceID = "missing-workspace"; return value }(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.AdmitPlanningCandidate(ctx, input); err == nil {
				t.Fatal("invalid candidate admission succeeded")
			}
		})
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM planning_candidates`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid admission persisted %d candidates, err=%v", count, err)
	}
}

func TestPlanningCandidateApprovalReviewIsExactReadOnlyAndRejectsStaleOrAlteredBytes(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-review', 'C:/planning-repo-review', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# Review candidate\n\x00exact\n")
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-repo-review", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := workspace.Version
	envelope, err := service.ComposePlannerAuthoring(ctx, PlannerAuthoringInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidate.Candidate.CandidateID})
	if err != nil || string(envelope.CandidateBytes) != string(bytes) || len(envelope.Manifest) == 0 || envelope.Historical {
		t.Fatalf("planner envelope = %#v, %v", envelope, err)
	}
	auditor, err := service.ComposeAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidate.Candidate.CandidateID})
	if err != nil || string(auditor.CandidateBytes) != string(bytes) || string(auditor.Manifest) != string(envelope.Manifest) {
		t.Fatalf("auditor envelope = %#v, %v", auditor, err)
	}
	if current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID); err != nil || current.Version != beforeVersion {
		t.Fatalf("read-only composition changed workspace = %#v, %v", current, err)
	}
	completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
	bad := append([]byte(nil), bytes...)
	bad[0] = 'X'
	if _, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: bad, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "wrong bytes", CreatedIdentity: "auditor"}); !errors.Is(err, ErrCandidateBytesMismatch) {
		t.Fatalf("altered candidate bytes error = %v", err)
	}
	if _, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "missing closure basis", CreatedIdentity: "auditor"}); !errors.Is(err, ErrCandidateApprovalInvalid) {
		t.Fatalf("missing basis error = %v", err)
	}
	oldPacket, err := store.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Historical basis\n")
	_, workspace, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: oldPacket.ClosurePacketID, OperatorConfirmed: true, Cause: "historical basis test", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: sql.NullInt64{Int64: oldPacket.ID, Valid: true}, OperatorConfirmationEvidence: "historical basis", CreatedIdentity: "auditor"}); !errors.Is(err, ErrHistoricalBasis) {
		t.Fatalf("historical basis error = %v", err)
	}
}

func TestFeatureCurrentnessMarksLegacyAndReopenedWorkspacesStale(t *testing.T) {
	ctx, store, service, legacy, _ := openFeatureServiceForCurrentnessTest(t)
	legacyDecision, err := service.EvaluateCurrentness(ctx, legacy.WorkspaceID)
	if err != nil || legacyDecision.Readiness != FeatureLegacy || legacyDecision.BlockedOperation != "progression" {
		t.Fatalf("legacy currentness = %#v, %v", legacyDecision, err)
	}
	legacyBytes := []byte("# legacy candidate\n")
	if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: legacy.WorkspaceID, ExpectedVersion: legacy.Version, Family: CandidateFamilyRequirements,
		Filename: legacy.FeatureSlug + ".requirements.md", Bytes: legacyBytes, SHA256: discoveryTestDigest(legacyBytes), RepoTarget: "legacy-repo",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	}); !errors.Is(err, ErrLegacyCurrentness) {
		t.Fatalf("legacy candidate admission error = %v, want ErrLegacyCurrentness", err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-currentness-adopted", "currentness-adopted")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Currentness\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.EvaluateCurrentness(ctx, workspace.WorkspaceID)
	if err != nil || current.Readiness != FeatureCurrent {
		t.Fatalf("current closure currentness = %#v, %v", current, err)
	}
	packet, err := store.GetDiscoveryClosurePacketByRowID(ctx, current.ClosurePacketRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Reopened currentness\n")
	_, workspace, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: packet.ClosurePacketID, OperatorConfirmed: true, Cause: "fresh evidence", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := service.EvaluateCurrentness(ctx, workspace.WorkspaceID)
	if err != nil || stale.Readiness != FeatureStale || stale.StaleOwner != "discovery_closure" {
		t.Fatalf("reopened currentness = %#v, %v", stale, err)
	}
}

func openFeatureServiceForCurrentnessTest(t *testing.T) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, error) {
	t.Helper()
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		return ctx, store, nil, workflowstore.FeatureWorkspace{}, err
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-currentness-legacy", "currentness-legacy")
	return ctx, store, service, workspace, err
}

func TestPlanningCandidatePromotionConflictAndRollbackAreAtomic(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationSharedDesign)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-conflict', 'C:/planning-repo-conflict', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-repo-conflict", strings.Repeat("a", 40))
	admit := func(value []byte, expectedVersion int64) (CandidateApprovalResult, error) {
		candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: expectedVersion, Family: CandidateFamilySharedDesign, Filename: workspace.FeatureSlug + ".design.md", Bytes: value, SHA256: discoveryTestDigest(value), RepoTarget: "planning-repo-conflict", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner"})
		if err != nil {
			return CandidateApprovalResult{}, err
		}
		completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
		return service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: value, ExpectedVersion: expectedVersion, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "approved", CreatedIdentity: "auditor"})
	}
	firstBytes := []byte("# First shared design\n")
	first, err := admit(firstBytes, workspace.Version)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: first.Candidate.CandidateID, ApprovalID: first.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	workspace = promoted.Workspace
	secondBytes := []byte("# Duplicate shared design\n")
	second, err := admit(secondBytes, workspace.Version)
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := workspace.Version
	beforeAuthorityCount := 0
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_authority_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&beforeAuthorityCount); err != nil {
		t.Fatal(err)
	}
	if _, err = service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: second.Candidate.CandidateID, ApprovalID: second.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); !errors.Is(err, ErrAuthorityDuplicate) {
		t.Fatalf("duplicate promotion error = %v", err)
	}
	var afterAuthorityCount int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_authority_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&afterAuthorityCount); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.Version != beforeVersion || afterAuthorityCount != beforeAuthorityCount {
		t.Fatalf("duplicate promotion mutated state = %#v, revisions=%d, err=%v", current, afterAuthorityCount, err)
	}

	rollbackBytes := []byte("# Rollback shared design\n")
	rollback, err := admit(rollbackBytes, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `CREATE TRIGGER fail_candidate_authority_layer BEFORE INSERT ON feature_workspace_authority_layers BEGIN SELECT RAISE(ABORT, 'candidate authority rollback'); END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.DB().ExecContext(ctx, `DROP TRIGGER fail_candidate_authority_layer`) })
	if _, err = service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: rollback.Candidate.CandidateID, ApprovalID: rollback.Approval.ApprovalID, ExpectedVersion: current.Version, CreatedIdentity: "planner"}); err == nil {
		t.Fatal("rollback promotion succeeded")
	}
	var rollbackAuthorityCount int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_authority_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&rollbackAuthorityCount); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.Version != beforeVersion || rollbackAuthorityCount != beforeAuthorityCount {
		t.Fatalf("rollback promotion mutated state = %#v, revisions=%d, err=%v", current, rollbackAuthorityCount, err)
	}
}

func TestPlanningCandidateSharedDesignPromotionComposesCurrentRequirements(t *testing.T) {
	t.Run("current requirements are retained exactly once", func(t *testing.T) {
		ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
		var err error
		closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-shared-design', 'C:/planning-repo-shared-design', 'refs/heads/main', 1)`); err != nil {
			t.Fatal(err)
		}
		insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-repo-shared-design", strings.Repeat("a", 40))

		requirementsBytes := []byte("# requirements candidate\n")
		requirementsCandidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
			WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
			Filename: workspace.FeatureSlug + ".requirements.md", Bytes: requirementsBytes, SHA256: discoveryTestDigest(requirementsBytes), RepoTarget: "planning-repo-shared-design",
			Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
		})
		if err != nil {
			t.Fatal(err)
		}
		completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
		requirementsApproval, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
			CandidateID: requirementsCandidate.Candidate.CandidateID, ExpectedSHA256: requirementsCandidate.Candidate.ArtifactSha256,
			ExpectedSizeBytes: requirementsCandidate.Candidate.ArtifactSizeBytes, Bytes: requirementsBytes, ExpectedVersion: workspace.Version,
			ExpectedClosurePacketRowID: sql.NullInt64{Int64: closed.Packet.ID, Valid: true}, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
			OperatorConfirmationEvidence: "exact requirements candidate confirmed", CreatedIdentity: "auditor",
		})
		if err != nil {
			t.Fatal(err)
		}
		promotedRequirements, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: requirementsCandidate.Candidate.CandidateID, ApprovalID: requirementsApproval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		workspace = promotedRequirements.Workspace

		sharedDesignBytes := []byte("# shared design discovery\n")
		reopened, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{
			WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID,
			OperatorConfirmed: true, Cause: "prepare shared design candidate", CreatedIdentity: "operator", Markdown: sharedDesignBytes,
			SHA256: discoveryTestDigest(sharedDesignBytes), Destination: DiscoveryDestinationSharedDesign,
		})
		if err != nil {
			t.Fatal(err)
		}
		sharedClosed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: reopened.DiscoveryRevisionID, Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "operator"})
		if err != nil {
			t.Fatal(err)
		}

		sharedDesignCandidateBytes := []byte("# shared design candidate\n")
		sharedDesignCandidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
			WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilySharedDesign,
			Filename: workspace.FeatureSlug + ".design.md", Bytes: sharedDesignCandidateBytes, SHA256: discoveryTestDigest(sharedDesignCandidateBytes), RepoTarget: "planning-repo-shared-design",
			Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
		})
		if err != nil {
			t.Fatal(err)
		}
		completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
		sharedDesignApproval, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
			CandidateID: sharedDesignCandidate.Candidate.CandidateID, ExpectedSHA256: sharedDesignCandidate.Candidate.ArtifactSha256,
			ExpectedSizeBytes: sharedDesignCandidate.Candidate.ArtifactSizeBytes, Bytes: sharedDesignCandidateBytes, ExpectedVersion: workspace.Version,
			ExpectedClosurePacketRowID: sql.NullInt64{Int64: sharedClosed.Packet.ID, Valid: true}, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
			OperatorConfirmationEvidence: "exact shared design candidate confirmed", CreatedIdentity: "auditor",
		})
		if err != nil {
			t.Fatal(err)
		}
		promotedSharedDesign, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: sharedDesignCandidate.Candidate.CandidateID, ApprovalID: sharedDesignApproval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}

		history, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 2 || len(promotedSharedDesign.Detail.Layers) != 2 {
			t.Fatalf("authority history = %#v, promoted detail = %#v", history, promotedSharedDesign.Detail)
		}
		layers := promotedSharedDesign.Detail.Layers
		wantKinds := []string{CandidateFamilyRequirements, CandidateFamilySharedDesign}
		for index, wantKind := range wantKinds {
			if layers[index].Sequence != int64(index+1) || layers[index].LayerKind != wantKind || !layers[index].CandidateArtifactRowID.Valid {
				t.Fatalf("authority layer %d = %#v", index, layers[index])
			}
		}
		if layers[0].CandidateArtifactRowID.Int64 != requirementsCandidate.Candidate.ArtifactRowID || layers[1].CandidateArtifactRowID.Int64 != sharedDesignCandidate.Candidate.ArtifactRowID {
			t.Fatalf("authority candidate artifacts = %#v", layers)
		}
	})
}

func TestPlanningCandidateAdmissionRequiresCanonicalGoverningFilenames(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirementsThenSharedDesign)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirementsThenSharedDesign, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-filenames', 'C:/planning-repo-filenames', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# canonical candidate\n")
	admit := func(family, filename string) error {
		_, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
			WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: family,
			Filename: filename, Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-repo-filenames",
			Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirementsThenSharedDesign, CreatedIdentity: "planner",
		})
		return err
	}
	if err := admit(CandidateFamilyRequirements, workspace.FeatureSlug+".requirements.md"); err != nil {
		t.Fatalf("canonical requirements filename rejected: %v", err)
	}
	if err := admit(CandidateFamilySharedDesign, workspace.FeatureSlug+".design.md"); err != nil {
		t.Fatalf("canonical shared design filename rejected: %v", err)
	}
	for _, test := range []struct {
		family, filename string
	}{
		{CandidateFamilyRequirements, "requirements.md"},
		{CandidateFamilyRequirements, "wrong-slug.requirements.md"},
		{CandidateFamilySharedDesign, "shared-design.md"},
		{CandidateFamilySharedDesign, "shared_design.md"},
		{CandidateFamilySharedDesign, "wrong-slug.design.md"},
	} {
		if err := admit(test.family, test.filename); !errors.Is(err, ErrInvalidCandidateInput) {
			t.Fatalf("%s filename %q error = %v, want ErrInvalidCandidateInput", test.family, test.filename, err)
		}
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM planning_candidates`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("candidate count after filename validation = %d, err=%v", count, err)
	}
}

func insertReadyPlanningSourceClosure(t *testing.T, ctx context.Context, store *workflowstore.Store, workspace workflowstore.FeatureWorkspace, repoTarget, commit string) workflowstore.SourceVaultClosure {
	t.Helper()
	vaultIDValue := "vault-planning-" + workspace.WorkspaceID + "-" + strings.ReplaceAll(repoTarget, "_", "-")
	closureIDValue := "closure-planning-" + workspace.WorkspaceID + "-" + strings.ReplaceAll(repoTarget, "_", "-")
	var vaultID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES (?, ?, ?) RETURNING id`, vaultIDValue, repoTarget, "vaults/"+repoTarget).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES (?, ?, ?, ?, 1, ?, 'ready', '2026-08-01T00:00:00.000000000Z', '2026-08-01T00:00:01.000000000Z')`, closureIDValue, vaultID, commit, strings.Repeat("b", 40), "refs/relay/closures/"+workspace.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	closure, err := store.GetSourceVaultClosureByClosureID(ctx, closureIDValue)
	if err != nil {
		t.Fatal(err)
	}
	return closure
}

func TestPlanningCandidatePromotionRejectsUnavailableSourceClosure(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-unavailable', 'C:/planning-repo-unavailable', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	closure := insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-repo-unavailable", strings.Repeat("a", 40))
	bytes := []byte("# unavailable source candidate\n")
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
		Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-repo-unavailable",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
	approval, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "source closure must remain ready", CreatedIdentity: "auditor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'source_commit_missing', verified_at = NULL WHERE id = ?`, closure.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("unavailable source promotion error = %v, want ErrStaleCandidateBasis", err)
	}
	var authorities int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_authority_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&authorities); err != nil || authorities != 0 {
		t.Fatalf("authority revisions after unavailable source = %d, err=%v", authorities, err)
	}
}

func completeReadyPlanningReview(t *testing.T, ctx context.Context, service *Service, workspaceID string) {
	t.Helper()
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); err != nil {
		t.Fatal(err)
	}
}

func TestCompletePlanningCandidateReviewRecordsNarrowFactWithoutOutcome(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-repo-review', 'C:/planning-repo-review', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-repo-review", strings.Repeat("a", 40))
	bytes := []byte("# reviewable requirements\n")
	admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
		Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-repo-review",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: ""}); !errors.Is(err, ErrCandidateReview) {
		t.Fatalf("review completion without identity error = %v, want ErrCandidateReview", err)
	}
	completed, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Review.CandidateRowID != admitted.Candidate.ID || completed.Review.ReviewerIdentity != "auditor" || completed.Review.Disposition != string(PlanningCandidateReviewReadyForApproval) {
		t.Fatalf("completed review = %#v", completed.Review)
	}
	read, err := service.store.GetPlanningCandidateReviewByCandidateRowID(ctx, admitted.Candidate.ID)
	if err != nil || read.ReviewID != completed.Review.ReviewID || read.CompletedAt == "" {
		t.Fatalf("review readback = %#v, %v", read, err)
	}
	// The completion fact is recorded exactly once and cannot overwrite the
	// review history.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); !errors.Is(err, ErrCandidateReview) {
		t.Fatalf("duplicate review completion error = %v, want ErrCandidateReview", err)
	}
	// An already-approved candidate cannot record a later review completion.
	workspace, err = service.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: admitted.Candidate.CandidateID, ExpectedSHA256: admitted.Candidate.ArtifactSha256,
		ExpectedSizeBytes: admitted.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "reviewed then approved", CreatedIdentity: "auditor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval}); !errors.Is(err, ErrCandidateReview) {
		t.Fatalf("review completion after approval error = %v, want ErrCandidateReview", err)
	}
	// No findings, report, or prose is persisted: the table carries only the
	// identity, bounded disposition, and completion time.
	var reviewColumns []string
	rows, err := store.DB().QueryContext(ctx, `PRAGMA table_info(planning_candidate_reviews)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		reviewColumns = append(reviewColumns, name)
	}
	for _, forbidden := range []string{"outcome", "verdict", "content", "finding", "report"} {
		for _, column := range reviewColumns {
			if strings.Contains(column, forbidden) {
				t.Fatalf("review schema persists forbidden outcome column %q", column)
			}
		}
	}
}

func TestPlanningCandidateNeedsRevisionAndReplacementCannotAuthorizeApproval(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-review-disposition', 'C:/planning-review-disposition', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-review-disposition", strings.Repeat("a", 40))
	admit := func(bytes []byte) CandidateAdmissionResult {
		result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-review-disposition", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	approve := func(candidate CandidateAdmissionResult, bytes []byte) error {
		_, err := service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256, ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version, ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID, OperatorConfirmationEvidence: "review disposition", CreatedIdentity: "operator"})
		return err
	}
	firstBytes := []byte("# first review basis\n")
	first := admit(firstBytes)
	needsRevision, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewNeedsRevision})
	if err != nil || needsRevision.Review.CandidateRowID != first.Candidate.ID || needsRevision.Review.Disposition != string(PlanningCandidateReviewNeedsRevision) {
		t.Fatalf("needs-revision review = %#v, %v", needsRevision, err)
	}
	if err := approve(first, firstBytes); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("needs-revision approval error = %v, want ErrCandidateReviewIncomplete", err)
	}
	secondBytes := []byte("# replacement review basis\n")
	second := admit(secondBytes)
	if err := approve(first, firstBytes); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("replaced candidate approval error = %v, want ErrStaleCandidateBasis", err)
	}
	if err := approve(second, secondBytes); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("replacement inherited review error = %v, want ErrCandidateReviewIncomplete", err)
	}
	completeReadyPlanningReview(t, ctx, service, workspace.WorkspaceID)
	if err := approve(second, secondBytes); err != nil {
		t.Fatalf("ready replacement approval error = %v", err)
	}
}

func TestPlanningCandidateApprovalPropagatesUnreadableReview(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var err error
	if _, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-review-read-error', 'C:/planning-review-read-error', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-review-read-error", strings.Repeat("a", 40))
	bytes := []byte("# unreadable review\n")
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements,
		Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-review-read-error",
		Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `ALTER TABLE planning_candidate_reviews RENAME COLUMN disposition TO unreadable_disposition`); err != nil {
		t.Fatal(err)
	}
	_, readErr := store.GetPlanningCandidateReviewByCandidateRowID(ctx, candidate.Candidate.ID)
	if readErr == nil || errors.Is(readErr, sql.ErrNoRows) {
		t.Fatalf("review read error = %v, want non-no-row error", readErr)
	}
	_, err = service.ApprovePlanningCandidate(ctx, CandidateApprovalInput{
		CandidateID: candidate.Candidate.CandidateID, ExpectedSHA256: candidate.Candidate.ArtifactSha256,
		ExpectedSizeBytes: candidate.Candidate.ArtifactSizeBytes, Bytes: bytes, ExpectedVersion: workspace.Version,
		ExpectedClosurePacketRowID: workspace.CurrentDiscoveryClosurePacketRowID, ExpectedAuthorityRevisionRowID: workspace.CurrentAuthorityRevisionRowID,
		OperatorConfirmationEvidence: "review read must propagate", CreatedIdentity: "auditor",
	})
	if err == nil || errors.Is(err, ErrCandidateReviewIncomplete) || err.Error() != readErr.Error() {
		t.Fatalf("approval error = %v, want propagated review read error %v", err, readErr)
	}
}
