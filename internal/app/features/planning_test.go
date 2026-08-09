package features

import (
	"context"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestPlanningCandidateAdmissionContinuesToReview(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	if _, closed, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	} else {
		workspace = closed
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-review-repo', 'C:/planning-review-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# Requirements\n")
	result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-review-repo", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil || result.AuthorizedNextAction != "review_candidate" {
		t.Fatalf("admission=%+v err=%v", result, err)
	}
}

func TestPlanningCandidateAdmissionRejectsInvalidInputs(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-invalid', 'C:/planning-invalid', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# requirements\n")
	base := CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-invalid", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"}
	invalid := map[string]CandidateAdmissionInput{
		"digest":   func() CandidateAdmissionInput { input := base; input.SHA256 = strings.Repeat("0", 64); return input }(),
		"filename": func() CandidateAdmissionInput { input := base; input.Filename = "wrong.md"; return input }(),
		"destination": func() CandidateAdmissionInput {
			input := base
			input.Destination = DiscoveryDestinationSharedDesign
			return input
		}(),
		"workspace": func() CandidateAdmissionInput { input := base; input.WorkspaceID = "missing-workspace"; return input }(),
	}
	unsupported := base
	unsupported.Family = "unsupported"
	if _, err := service.AdmitPlanningCandidate(ctx, unsupported); !errors.Is(err, ErrInvalidCandidateFamily) {
		t.Fatalf("unsupported family error = %v", err)
	}
	for name, input := range invalid {
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

func TestPlanningCandidateReviewRefreshPreservesPlannerOperations(t *testing.T) {
	cases := []struct {
		family, filename, operation string
	}{
		{CandidateFamilyRequirements, "", "planner.requirements"},
		{CandidateFamilySharedDesign, "", "planner.shared_design"},
		{CandidateFamilyDeliveryTicket, "", "planner.delivery_ticket"},
	}
	for _, tc := range cases {
		t.Run(tc.family, func(t *testing.T) {
			destination := DiscoveryDestinationRequirements
			if tc.family == CandidateFamilySharedDesign {
				destination = DiscoveryDestinationSharedDesign
			}
			if tc.family == CandidateFamilyDeliveryTicket {
				destination = DiscoveryDestinationDirectDeliveryTicket
			}
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, destination)
			if _, closed, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator"}); err != nil {
				t.Fatal(err)
			} else {
				workspace = closed
			}
			filename := workspace.FeatureSlug + ".requirements.md"
			if tc.family == CandidateFamilySharedDesign {
				filename = workspace.FeatureSlug + ".design.md"
			}
			if tc.family == CandidateFamilyDeliveryTicket {
				filename = "discovery-proof.ticket-P3-REFRESH.r1.delivery-ticket.json"
			}
			repoTarget := "planning-refresh-" + strings.ReplaceAll(tc.family, "_", "-")
			if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES (?, ?, 'refs/heads/main', 1)`, repoTarget, "C:/"+repoTarget); err != nil {
				t.Fatal(err)
			}
			insertReadyPlanningSourceClosure(t, ctx, store, workspace, repoTarget, strings.Repeat("a", 40))
			bytes := []byte("# refresh " + tc.family + "\n")
			admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: tc.family, Filename: filename, Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: repoTarget, Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: destination, CreatedIdentity: "planner"})
			if err != nil {
				t.Fatal(err)
			}
			rejected, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: admitted.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewNeedsRevision, ReviewedBytes: bytes})
			if err != nil || rejected.Refresh == nil || rejected.Refresh.OperationID != tc.operation || rejected.Refresh.AuditorReviewResult != string(PlanningCandidateReviewNeedsRevision) || string(rejected.Refresh.ReviewedCandidate) != string(bytes) {
				t.Fatalf("refresh=%+v err=%v", rejected, err)
			}
		})
	}
}

func TestPlanningCandidateApprovalRejectsReplacedCandidateAndNeedsFreshReview(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	if _, closed, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	} else {
		workspace = closed
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-replaced', 'C:/planning-replaced', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-replaced", strings.Repeat("a", 40))
	admit := func(text string) CandidateAdmissionResult {
		bytes := []byte(text)
		result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-replaced", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := admit("# first\n")
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: []byte("# first\n")}); err != nil {
		t.Fatal(err)
	}
	// A newer immutable candidate replaces the reviewed one on the same basis.
	// The stored continuation no longer matches the current exact candidate, so
	// approval is rejected and the journey demands a fresh review.
	second := admit("# replacement\n")
	if _, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "replaced candidate", CreatedIdentity: "operator"}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("replaced candidate approval error=%v", err)
	}
	planning, err := service.guidedPlanning(ctx, workspace, FeatureCurrentnessDecision{Readiness: FeatureCurrent}, nil)
	if err != nil || planning.AwaitingApproval != 0 || planning.AwaitingReview != 1 || planning.Requirements.State != "admitted" {
		t.Fatalf("replaced planning=%+v err=%v", planning, err)
	}
	// A fresh ready review of the current replacement candidate arms its own
	// continuation and approval succeeds on the current exact candidate.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: second.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: []byte("# replacement\n")}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "fresh review approval", CreatedIdentity: "operator"})
	if err != nil || approved.Candidate.CandidateID == first.Candidate.CandidateID {
		t.Fatalf("fresh review approval=%+v err=%v", approved, err)
	}
}

func TestPlanningCandidateReviewUsesImmediateContinuationOrRefresh(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	if _, closed, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	} else {
		workspace = closed
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-review-continuation', 'C:/planning-review-continuation', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-review-continuation", strings.Repeat("a", 40))
	admit := func(text string) CandidateAdmissionResult {
		bytes := []byte(text)
		result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-review-continuation", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := admit("# first\n")
	rejected, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewNeedsRevision, ReviewedBytes: []byte("# first\n")})
	if err != nil || rejected.Refresh == nil || rejected.Refresh.OperationID != "planner.requirements" || string(rejected.Refresh.ReviewedCandidate) != "# first\n" {
		t.Fatalf("rejected=%+v err=%v", rejected, err)
	}
	// A needs-revision review clears any continuation: the workspace-only
	// approval cannot consume a fresh continuation and is rejected.
	if _, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "no fresh continuation", CreatedIdentity: "operator"}); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("approval after needs-revision error=%v", err)
	}
	second := admit("# replacement\n")
	ready, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: second.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: []byte("# replacement\n")})
	if err != nil || ready.Disposition != PlanningCandidateReviewReadyForApproval {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	approved, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "confirmed exact replacement", CreatedIdentity: "operator"})
	if err != nil || approved.Candidate.CandidateID != second.Candidate.CandidateID || approved.Approval.ApprovalID == "" || first.Candidate.CandidateID == second.Candidate.CandidateID {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	// The continuation is consumed by the single approval: a second approval
	// requires a fresh ready review.
	if _, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "consumed continuation", CreatedIdentity: "operator"}); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("second approval error=%v", err)
	}
}

func TestPlanningCandidateReviewBindingRejectsStaleCompositionAfterReplacement(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	if _, closed, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	} else {
		workspace = closed
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-binding', 'C:/planning-binding', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-binding", strings.Repeat("a", 40))
	admit := func(text string) CandidateAdmissionResult {
		bytes := []byte(text)
		result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-binding", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	// The auditor composes the read-only review envelope over candidate A's
	// exact bytes.
	first := admit("# candidate A\n")
	composed, err := service.ComposeAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID})
	if err != nil || string(composed.CandidateBytes) != "# candidate A\n" {
		t.Fatalf("composed review = %#v, %v", composed, err)
	}
	// Candidate B replaces A on the same basis before the completion arrives.
	second := admit("# candidate B\n")
	if first.Candidate.CandidateID == second.Candidate.CandidateID {
		t.Fatal("replacement did not create a distinct candidate")
	}
	// A stale ready completion naming A's exact identity is rejected because B
	// is now the newest admissible candidate on the same basis; no result,
	// continuation, or refresh attaches to B.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: composed.CandidateBytes}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("stale ready review error = %v, want ErrStaleCandidateBasis", err)
	}
	// A stale needs-revision completion naming A is also rejected, so the stale
	// result cannot drive a planner refresh replacement.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewNeedsRevision, ReviewedBytes: composed.CandidateBytes}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("stale needs-revision review error = %v, want ErrStaleCandidateBasis", err)
	}
	// Neither stale review armed a continuation, so approval of B still
	// requires a fresh ready review of B's exact bytes.
	if _, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "stale result must not attach", CreatedIdentity: "operator"}); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("approval after stale reviews error = %v, want ErrCandidateReviewIncomplete", err)
	}
	// A fresh ready review of B's exact bytes arms B's continuation and the
	// distinct explicit approval attaches to B alone.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: second.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: []byte("# candidate B\n")}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "fresh B review approval", CreatedIdentity: "operator"})
	if err != nil || approved.Candidate.CandidateID != second.Candidate.CandidateID {
		t.Fatalf("fresh B approval=%+v err=%v", approved, err)
	}
}

func TestPlanningCandidateReviewRejectsSameByteNewerImmutableCandidate(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	if _, closed, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	} else {
		workspace = closed
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-same-bytes', 'C:/planning-same-bytes', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-same-bytes", strings.Repeat("a", 40))
	bytes := []byte("# identical requirements bytes\n")
	admit := func() CandidateAdmissionResult {
		result, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-same-bytes", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := admit()
	second := admit()
	// A same-byte replacement is still a distinct immutable candidate: the
	// identity, not the bytes, is the exact review binding.
	if first.Candidate.CandidateID == second.Candidate.CandidateID {
		t.Fatal("same-byte replacement did not create a distinct immutable candidate")
	}
	if first.Candidate.ArtifactSha256 != second.Candidate.ArtifactSha256 || first.Candidate.ArtifactSizeBytes != second.Candidate.ArtifactSizeBytes {
		t.Fatalf("same-byte candidates diverged: first=%#v second=%#v", first.Candidate, second.Candidate)
	}
	// A review naming the replaced candidate A is rejected for both
	// dispositions even though B's bytes are identical: no result,
	// continuation, or refresh can attach to B.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("same-byte stale ready review error = %v, want ErrStaleCandidateBasis", err)
	}
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewNeedsRevision, ReviewedBytes: bytes}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("same-byte stale needs-revision review error = %v, want ErrStaleCandidateBasis", err)
	}
	// Neither rejected review armed a continuation, so approval of B still
	// requires a fresh ready review of B.
	if _, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "no continuation from rejected reviews", CreatedIdentity: "operator"}); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("approval after same-byte stale reviews error = %v, want ErrCandidateReviewIncomplete", err)
	}
	// A fresh ready review naming B arms B's continuation and the distinct
	// explicit approval attaches to B alone.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: second.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "fresh B review approval", CreatedIdentity: "operator"})
	if err != nil || approved.Candidate.CandidateID != second.Candidate.CandidateID {
		t.Fatalf("fresh B approval=%+v err=%v", approved, err)
	}
}

func TestPlanningCandidateReviewRejectsCrossWorkspaceAndUnknownIdentity(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-cross', 'C:/planning-cross', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# cross workspace candidate\n")
	first, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-cross", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	// A second independent workspace with its own closed discovery.
	other, err := createFeatureWorkspace(ctx, store, "workspace-review-cross", "review-cross")
	if err != nil {
		t.Fatal(err)
	}
	other, err = service.SetIntegratedDiscoveryCapability(ctx, other.WorkspaceID, other.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, other, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: other.WorkspaceID, ExpectedVersion: other.Version, OperatorIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	otherContent := []byte("# other workspace discovery\n")
	started, other, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: other.WorkspaceID, ExpectedVersion: other.Version, Markdown: otherContent, SHA256: discoveryTestDigest(otherContent), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	_, other, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: other.WorkspaceID, ExpectedVersion: other.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	// Reviewing workspace A's candidate through workspace B's endpoint is
	// rejected: the reviewed identity must belong to the workspace.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: other.WorkspaceID, CandidateID: first.Candidate.CandidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes}); !errors.Is(err, ErrCandidateReview) {
		t.Fatalf("cross-workspace review error = %v, want ErrCandidateReview", err)
	}
	// An unknown candidate identity is rejected as an invalid review.
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: "missing-candidate", ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: bytes}); !errors.Is(err, ErrCandidateReview) {
		t.Fatalf("unknown candidate review error = %v, want ErrCandidateReview", err)
	}
	// No continuation was armed by any rejected review.
	if _, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: "no continuation from rejected reviews", CreatedIdentity: "operator"}); !errors.Is(err, ErrCandidateReviewIncomplete) {
		t.Fatalf("approval after rejected reviews error = %v, want ErrCandidateReviewIncomplete", err)
	}
}

func TestPlanningCandidateAuthoringAndReviewCompositionIsExactReadOnly(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-composition-read', 'C:/planning-composition-read', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("# exact review candidate\n\x00bytes\n")
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-composition-read", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := workspace.Version
	planner, err := service.ComposePlannerAuthoring(ctx, PlannerAuthoringInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidate.Candidate.CandidateID})
	if err != nil || planner.Historical || string(planner.CandidateBytes) != string(bytes) || len(planner.Manifest) == 0 {
		t.Fatalf("planner envelope = %#v, %v", planner, err)
	}
	auditor, err := service.ComposeAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidate.Candidate.CandidateID})
	if err != nil || string(auditor.CandidateBytes) != string(bytes) || string(auditor.Manifest) != string(planner.Manifest) {
		t.Fatalf("auditor envelope = %#v, %v", auditor, err)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.Version != beforeVersion {
		t.Fatalf("read-only composition changed workspace = %#v, %v", current, err)
	}
}

func TestPlanningCandidateRequirementsAndSharedDesignPromotionSequences(t *testing.T) {
	cases := []struct {
		name        string
		destination DiscoveryDestination
		families    []string
		wantLayers  []string
	}{
		{"requirements", DiscoveryDestinationRequirements, []string{CandidateFamilyRequirements}, []string{CandidateFamilyRequirements}},
		{"shared design", DiscoveryDestinationSharedDesign, []string{CandidateFamilySharedDesign}, []string{CandidateFamilySharedDesign}},
		{"requirements then shared design", DiscoveryDestinationRequirementsThenSharedDesign, []string{CandidateFamilyRequirements, CandidateFamilySharedDesign}, []string{CandidateFamilyRequirements, CandidateFamilySharedDesign}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, tc.destination)
			_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: tc.destination, CreatedIdentity: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-sequence', 'C:/planning-sequence', 'refs/heads/main', 1)`); err != nil {
				t.Fatal(err)
			}
			closure := insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-sequence", strings.Repeat("a", 40))
			for _, family := range tc.families {
				bytes := []byte("# " + family + "\n")
				filename := workspace.FeatureSlug + ".requirements.md"
				if family == CandidateFamilySharedDesign {
					filename = workspace.FeatureSlug + ".design.md"
				}
				candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: family, Filename: filename, Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-sequence", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: tc.destination, CreatedIdentity: "planner"})
				if err != nil {
					t.Fatal(err)
				}
				approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, candidate.Candidate.CandidateID, "approved exact candidate", bytes)
				if approval.Candidate.CandidateID != candidate.Candidate.CandidateID {
					t.Fatalf("approved candidate = %q, want %q", approval.Candidate.CandidateID, candidate.Candidate.CandidateID)
				}
				promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
				if err != nil {
					t.Fatal(err)
				}
				workspace = promoted.Workspace
				if !promoted.Detail.Revision.SourceClosureRowID.Valid || promoted.Detail.Revision.SourceClosureRowID.Int64 != closure.ID {
					t.Fatalf("authority source closure = %#v", promoted.Detail.Revision)
				}
			}
			history, err := service.ReadAuthority(ctx, workspace.WorkspaceID)
			if err != nil || len(history) != len(tc.families) {
				t.Fatalf("authority history = %#v, %v", history, err)
			}
			layers := history[len(history)-1].Layers
			if len(layers) != len(tc.wantLayers) {
				t.Fatalf("authority layers = %#v", layers)
			}
			for i, want := range tc.wantLayers {
				if layers[i].Sequence != int64(i+1) || layers[i].LayerKind != want || !layers[i].CandidateArtifactRowID.Valid {
					t.Fatalf("layer %d = %#v", i, layers[i])
				}
			}
			currentness, err := service.EvaluateCurrentness(ctx, workspace.WorkspaceID)
			if err != nil || currentness.Readiness != FeatureCurrent {
				t.Fatalf("promoted currentness = %#v, %v", currentness, err)
			}
		})
	}
}

func TestFeatureCurrentnessMarksLegacyAndReopenedWorkspacesStale(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := createFeatureWorkspace(ctx, store, "workspace-currentness-legacy", "currentness-legacy")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := service.EvaluateCurrentness(ctx, legacy.WorkspaceID)
	if err != nil || decision.Readiness != FeatureLegacy || decision.BlockedOperation != "progression" {
		t.Fatalf("legacy currentness = %#v, %v", decision, err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-currentness-adopted", "currentness-adopted")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("# currentness\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := store.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# reopened currentness\n")
	_, workspace, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: packet.ClosurePacketID, OperatorConfirmed: true, Cause: "fresh evidence", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	decision, err = service.EvaluateCurrentness(ctx, workspace.WorkspaceID)
	if err != nil || decision.Readiness != FeatureStale || decision.StaleOwner != "discovery_closure" {
		t.Fatalf("reopened currentness = %#v, %v", decision, err)
	}
}

func TestPlanningCandidatePromotionConflictAndRollbackAreAtomic(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationSharedDesign)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-atomic', 'C:/planning-atomic', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-atomic", strings.Repeat("a", 40))
	admitAndApprove := func(text string) CandidateApprovalResult {
		bytes := []byte(text)
		admitted, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilySharedDesign, Filename: workspace.FeatureSlug + ".design.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-atomic", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner"})
		if err != nil {
			t.Fatal(err)
		}
		return approveCurrentPlanningCandidate(t, ctx, service, workspace, admitted.Candidate.CandidateID, "approved", bytes)
	}
	first := admitAndApprove("# first\n")
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: first.Candidate.CandidateID, ApprovalID: first.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	workspace = promoted.Workspace
	duplicate := admitAndApprove("# duplicate\n")
	beforeVersion := workspace.Version
	var beforeCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_authority_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: duplicate.Candidate.CandidateID, ApprovalID: duplicate.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); !errors.Is(err, ErrAuthorityDuplicate) {
		t.Fatalf("duplicate promotion error = %v", err)
	}
	rollback := admitAndApprove("# rollback\n")
	if _, err := store.DB().ExecContext(ctx, `CREATE TRIGGER fail_candidate_authority_layer BEFORE INSERT ON feature_workspace_authority_layers BEGIN SELECT RAISE(ABORT, 'candidate authority rollback'); END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.DB().ExecContext(ctx, `DROP TRIGGER fail_candidate_authority_layer`) })
	if _, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: rollback.Candidate.CandidateID, ApprovalID: rollback.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); err == nil {
		t.Fatal("rollback promotion succeeded")
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.Version != beforeVersion {
		t.Fatalf("workspace after failed promotions = %#v, %v", current, err)
	}
	var afterCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_authority_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&afterCount); err != nil || afterCount != beforeCount {
		t.Fatalf("authority revisions after failed promotions = %d, %v", afterCount, err)
	}
}

func TestPlanningCandidateSharedDesignPromotionComposesCurrentRequirements(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-composition', 'C:/planning-composition', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-composition", strings.Repeat("a", 40))
	requirementsBytes := []byte("# requirements candidate\n")
	requirements, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: requirementsBytes, SHA256: discoveryTestDigest(requirementsBytes), RepoTarget: "planning-composition", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	requirementsApproval := approveCurrentPlanningCandidate(t, ctx, service, workspace, requirements.Candidate.CandidateID, "requirements exact approval", requirementsBytes)
	promotedRequirements, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: requirements.Candidate.CandidateID, ApprovalID: requirementsApproval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	workspace = promotedRequirements.Workspace
	replacement := []byte("# shared design discovery\n")
	opened, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "prepare shared design", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationSharedDesign})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: opened.DiscoveryRevisionID, Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	designBytes := []byte("# shared design candidate\n")
	design, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilySharedDesign, Filename: workspace.FeatureSlug + ".design.md", Bytes: designBytes, SHA256: discoveryTestDigest(designBytes), RepoTarget: "planning-composition", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	designApproval := approveCurrentPlanningCandidate(t, ctx, service, workspace, design.Candidate.CandidateID, "shared design exact approval", designBytes)
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: design.Candidate.CandidateID, ApprovalID: designApproval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted.Detail.Layers) != 2 || promoted.Detail.Layers[0].LayerKind != CandidateFamilyRequirements || promoted.Detail.Layers[0].CandidateArtifactRowID.Int64 != requirements.Candidate.ArtifactRowID || promoted.Detail.Layers[1].LayerKind != CandidateFamilySharedDesign || promoted.Detail.Layers[1].CandidateArtifactRowID.Int64 != design.Candidate.ArtifactRowID {
		t.Fatalf("composed authority layers = %#v", promoted.Detail.Layers)
	}
}

func TestPlanningCandidateAdmissionRequiresCanonicalGoverningFilenames(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirementsThenSharedDesign)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirementsThenSharedDesign, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-filenames', 'C:/planning-filenames', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	admit := func(family, filename string) error {
		bytes := []byte("# canonical\n")
		_, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: family, Filename: filename, Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-filenames", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirementsThenSharedDesign, CreatedIdentity: "planner"})
		return err
	}
	for _, tc := range []struct{ family, filename string }{{CandidateFamilyRequirements, workspace.FeatureSlug + ".requirements.md"}, {CandidateFamilySharedDesign, workspace.FeatureSlug + ".design.md"}} {
		if err := admit(tc.family, tc.filename); err != nil {
			t.Fatalf("canonical %s filename rejected: %v", tc.family, err)
		}
	}
	for _, tc := range []struct{ family, filename string }{{CandidateFamilyRequirements, "requirements.md"}, {CandidateFamilyRequirements, "wrong.requirements.md"}, {CandidateFamilySharedDesign, "shared-design.md"}, {CandidateFamilySharedDesign, "wrong.design.md"}} {
		if err := admit(tc.family, tc.filename); !errors.Is(err, ErrInvalidCandidateInput) {
			t.Fatalf("%s filename %q error = %v", tc.family, tc.filename, err)
		}
	}
}

func TestPlanningCandidatePromotionRejectsUnavailableSourceClosure(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	_, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('planning-unavailable', 'C:/planning-unavailable', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	closure := insertReadyPlanningSourceClosure(t, ctx, store, workspace, "planning-unavailable", strings.Repeat("a", 40))
	bytes := []byte("# unavailable source\n")
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyRequirements, Filename: workspace.FeatureSlug + ".requirements.md", Bytes: bytes, SHA256: discoveryTestDigest(bytes), RepoTarget: "planning-unavailable", Branch: "main", BaseCommit: strings.Repeat("a", 40), Destination: DiscoveryDestinationRequirements, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, candidate.Candidate.CandidateID, "source closure must remain ready", bytes)
	if _, err := store.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'source_commit_missing', verified_at = NULL WHERE id = ?`, closure.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "planner"}); !errors.Is(err, ErrStaleCandidateBasis) {
		t.Fatalf("unavailable source promotion error = %v", err)
	}
}

func approveCurrentPlanningCandidate(t *testing.T, ctx context.Context, service *Service, workspace workflowstore.FeatureWorkspace, candidateID, evidence string, reviewedBytes []byte) CandidateApprovalResult {
	t.Helper()
	if _, err := service.CompletePlanningCandidateReview(ctx, CompleteCandidateReviewInput{WorkspaceID: workspace.WorkspaceID, CandidateID: candidateID, ReviewerIdentity: "auditor", Disposition: PlanningCandidateReviewReadyForApproval, ReviewedBytes: reviewedBytes}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorConfirmationEvidence: evidence, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	return approved
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
