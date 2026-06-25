package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"relay/internal/store"
	"relay/internal/store/generated"
)

const testSHA256 = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCreatePlanAttemptWithIntentCreatesDraftWithoutManagedPlan(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	result := createTestPlanAttempt(t, svc, "manual", "")

	if !result.OK {
		t.Fatalf("expected ok=true, got %#v", result)
	}
	if result.IntentPacket.Kind != IntentKindOriginal {
		t.Fatalf("expected original intent packet, got %q", result.IntentPacket.Kind)
	}
	if result.PlanAttempt.Status != PlanAttemptStatusDraft {
		t.Fatalf("expected draft attempt, got %q", result.PlanAttempt.Status)
	}
	if result.PlanAttempt.ReviewState != PlanAttemptReviewPacketReady {
		t.Fatalf("expected review_packet_ready, got %q", result.PlanAttempt.ReviewState)
	}
	if got := countRows(t, st.DB(), "intent_packets"); got != 1 {
		t.Fatalf("expected 1 intent packet, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_attempts"); got != 1 {
		t.Fatalf("expected 1 plan attempt, got %d", got)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 managed plans, got %d", got)
	}
}

func TestGetPlanIntentReviewPacketIsReadOnlyAndComplete(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	created := createTestPlanAttempt(t, svc, "manual", "")
	before := loadAttempt(t, svc.store, created.PlanAttempt.PlanAttemptID)

	result, err := svc.GetPlanIntentReviewPacket(context.Background(), GetPlanIntentReviewPacketRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
	})
	if err != nil {
		t.Fatalf("GetPlanIntentReviewPacket error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok=true, got %#v", result)
	}
	packet := result.ReviewPacket
	if packet.RootIntentPacket.IntentPacketID == "" || packet.ReviewedIntentPacket.IntentPacketID == "" {
		t.Fatalf("expected intent evidence in packet: %#v", packet)
	}
	if packet.PlanAttempt.PlanAttemptID != created.PlanAttempt.PlanAttemptID {
		t.Fatalf("expected attempt evidence for %q, got %q", created.PlanAttempt.PlanAttemptID, packet.PlanAttempt.PlanAttemptID)
	}
	if packet.PlanArtifacts.JSONArtifactSHA256 != created.PlanAttempt.PlanJsonArtifactSha256 {
		t.Fatalf("expected artifact evidence hash %q, got %q", created.PlanAttempt.PlanJsonArtifactSha256, packet.PlanArtifacts.JSONArtifactSHA256)
	}
	if packet.RedactionStatus == "" || packet.PacketHash == "" {
		t.Fatalf("expected redaction status and packet hash, got %#v", packet)
	}
	if !packet.RetrievalSemantics.RetrievalOnly || packet.RetrievalSemantics.ModelCallPerformed || packet.RetrievalSemantics.StateMutated {
		t.Fatalf("unexpected retrieval semantics: %#v", packet.RetrievalSemantics)
	}
	after := loadAttempt(t, svc.store, created.PlanAttempt.PlanAttemptID)
	assertAttemptRowsEqual(t, before, after)
}

func TestReviewPacketHashStableAcrossRetrievals(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	created := createTestPlanAttempt(t, svc, "manual", "")

	first, err := svc.GetPlanIntentReviewPacket(context.Background(), GetPlanIntentReviewPacketRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
	})
	if err != nil {
		t.Fatalf("GetPlanIntentReviewPacket first error: %v", err)
	}
	second, err := svc.GetPlanIntentReviewPacket(context.Background(), GetPlanIntentReviewPacketRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
	})
	if err != nil {
		t.Fatalf("GetPlanIntentReviewPacket second error: %v", err)
	}
	if !first.OK || first.ReviewPacket == nil {
		t.Fatalf("expected first ok review packet, got %#v", first)
	}
	if !second.OK || second.ReviewPacket == nil {
		t.Fatalf("expected second ok review packet, got %#v", second)
	}
	if first.ReviewPacket.GeneratedAt == "" || second.ReviewPacket.GeneratedAt == "" {
		t.Fatalf("expected generated_at values, got first=%#v second=%#v", first.ReviewPacket, second.ReviewPacket)
	}
	if first.ReviewPacket.PacketHash == "" || second.ReviewPacket.PacketHash == "" {
		t.Fatalf("expected packet hashes, got first=%#v second=%#v", first.ReviewPacket, second.ReviewPacket)
	}
	if first.ReviewPacket.PacketHash != second.ReviewPacket.PacketHash {
		t.Fatalf("expected stable packet hash, got first=%q second=%q", first.ReviewPacket.PacketHash, second.ReviewPacket.PacketHash)
	}
}

func TestSubmitIntentDriftReviewPersistsEvidenceOnly(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, "external", "")

	result := submitTestReview(t, svc, *created.PlanAttempt, ReviewSourceExternal, ApprovalGateStatusReady)
	if !result.OK {
		t.Fatalf("expected ok=true, got %#v", result)
	}
	if result.DriftReview.ReviewSource != ReviewSourceExternal {
		t.Fatalf("expected external review, got %q", result.DriftReview.ReviewSource)
	}
	if result.PlanAttempt.Status != PlanAttemptStatusDraft {
		t.Fatalf("expected draft attempt, got %q", result.PlanAttempt.Status)
	}
	if result.PlanAttempt.ReviewState != PlanAttemptReviewExternalSubmitted {
		t.Fatalf("expected external review state, got %q", result.PlanAttempt.ReviewState)
	}
	if got := countRows(t, st.DB(), "intent_drift_reviews"); got != 1 {
		t.Fatalf("expected 1 drift review, got %d", got)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 managed plans, got %d", got)
	}
}

func TestSubmitIntentDriftReviewBlocksMismatchedReviewPacketHashWithoutMutation(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, "external", "")
	before := loadAttempt(t, svc.store, created.PlanAttempt.PlanAttemptID)

	result, err := svc.SubmitIntentDriftReview(context.Background(), SubmitIntentDriftReviewRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
		DriftReview: DriftReviewInput{
			IntentDriftReviewID:    "review-mismatch-" + generateSlug(),
			PlanAttemptID:          created.PlanAttempt.PlanAttemptID,
			IntentThreadID:         created.PlanAttempt.IntentThreadID,
			RootIntentPacketID:     created.PlanAttempt.RootIntentPacketID,
			ReviewedIntentPacketID: created.PlanAttempt.CurrentIntentPacketID,
			ReviewPacketHash:       testSHA256,
			ReviewSource:           ReviewSourceExternal,
			SubmittedBy:            "tester",
			SourceArtifactPath:     "reviews/review.json",
			OverallAlignment:       OverallAlignmentAligned,
			Confidence:             0.95,
			FindingsJSON:           json.RawMessage(`[]`),
			RecommendedAction:      RecommendedActionApprove,
			ApprovalGateStatus:     ApprovalGateStatusReady,
			ModelMetadataJSON:      json.RawMessage(`{"model":"test"}`),
			InputHash:              testSHA256,
			OutputHash:             testSHA256,
		},
	})
	if err != nil {
		t.Fatalf("SubmitIntentDriftReview error: %v", err)
	}
	if result.OK || result.BlockerCode != BlockerStaleAttempt {
		t.Fatalf("expected stale attempt blocker, got %#v", result)
	}
	if got := countRows(t, st.DB(), "intent_drift_reviews"); got != 0 {
		t.Fatalf("expected 0 drift reviews, got %d", got)
	}
	after := loadAttempt(t, svc.store, created.PlanAttempt.PlanAttemptID)
	if after.Status != before.Status {
		t.Fatalf("expected status unchanged %q, got %q", before.Status, after.Status)
	}
	if after.ReviewState != before.ReviewState {
		t.Fatalf("expected review_state unchanged %q, got %q", before.ReviewState, after.ReviewState)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 managed plans, got %d", got)
	}
}

func TestRevisePlanAttemptSupersedesAndCreatesRevisionLineage(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, "manual", "")
	replacementRaw := planRawWithID(t, "plan-revision")
	_, replacementHash, err := canonicalRawPlanJSON(replacementRaw)
	if err != nil {
		t.Fatalf("canonicalRawPlanJSON: %v", err)
	}

	result, err := svc.RevisePlanAttempt(context.Background(), RevisePlanAttemptRequest{
		ProjectID:         "relay",
		PlanAttemptID:     created.PlanAttempt.PlanAttemptID,
		NewPlanAttemptID:  "attempt-revision",
		NewIntentPacketID: "intent-revision",
		PlanArtifactRef: PlanArtifactRef{
			Path:         "handoffs/planner/revision.planner-pass-plan.json",
			SHA256:       replacementHash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON: replacementRaw,
		NewIntentPacket: IntentPacketInput{
			Summary:            "Revision request",
			LiteralUserRequest: "Revise the draft plan.",
			Constraints:        []string{"Keep backend scope."},
			Source:             IntentSource{CapturedFrom: CapturedFromRevisionNotes, CapturedBy: "tester"},
			RedactionStatus:    RedactionStatusVerifiedNoSecrets,
		},
	})
	if err != nil {
		t.Fatalf("RevisePlanAttempt error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok=true, got %#v", result)
	}
	oldAttempt := loadAttempt(t, svc.store, created.PlanAttempt.PlanAttemptID)
	if oldAttempt.Status != PlanAttemptStatusSuperseded || oldAttempt.ReplacementPlanAttemptID.String != result.PlanAttempt.PlanAttemptID {
		t.Fatalf("old attempt not superseded correctly: %#v", oldAttempt)
	}
	if result.PlanAttempt.Status != PlanAttemptStatusDraft || result.PlanAttempt.SupersedesPlanAttemptID.String != oldAttempt.PlanAttemptID {
		t.Fatalf("new attempt lineage incorrect: %#v", result.PlanAttempt)
	}
	intent := loadIntentPacket(t, st, result.IntentPacket.IntentPacketID)
	if intent.Kind != IntentKindRevision || intent.ParentIntentPacketID.String != created.PlanAttempt.CurrentIntentPacketID || intent.RevisionOfPlanAttemptID.String != created.PlanAttempt.PlanAttemptID {
		t.Fatalf("revision intent lineage incorrect: %#v", intent)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 managed plans, got %d", got)
	}
}

func TestVoidPlanAttemptDoesNotCreateReplacementOrPlan(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, "manual", "")

	result, err := svc.VoidPlanAttempt(context.Background(), VoidPlanAttemptRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
	})
	if err != nil {
		t.Fatalf("VoidPlanAttempt error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok=true, got %#v", result)
	}
	if result.PlanAttempt.Status != PlanAttemptStatusVoided {
		t.Fatalf("expected voided, got %q", result.PlanAttempt.Status)
	}
	if result.PlanAttempt.ReplacementPlanAttemptID.Valid {
		t.Fatalf("expected no replacement, got %#v", result.PlanAttempt.ReplacementPlanAttemptID)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 managed plans, got %d", got)
	}
}

func TestApprovePlanAttemptGateMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mode         string
		reviewSource string
		gate         string
		driftAck     bool
		noReviewAck  bool
		wantOK       bool
		wantBlocker  PlanAttemptBlockerCode
	}{
		{name: "disabled no review", mode: DriftReviewModeDisabled, wantOK: true},
		{name: "manual no ack", mode: DriftReviewModeManual, wantBlocker: BlockerDriftAcknowledgementReq},
		{name: "manual no review ack", mode: DriftReviewModeManual, noReviewAck: true, wantOK: true},
		{name: "automatic missing review", mode: DriftReviewModeAutomatic, wantBlocker: BlockerDriftReviewRequired},
		{name: "external missing review", mode: DriftReviewModeExternal, wantBlocker: BlockerDriftReviewRequired},
		{name: "ready", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gate: ApprovalGateStatusReady, wantOK: true},
		{name: "ack required without ack", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gate: ApprovalGateStatusAckRequired, wantBlocker: BlockerDriftAcknowledgementReq},
		{name: "ack required with ack", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gate: ApprovalGateStatusAckRequired, driftAck: true, wantOK: true},
		{name: "revision required", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gate: ApprovalGateStatusRevisionRequired, wantBlocker: BlockerDriftRevisionRequired},
		{name: "blocked", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gate: ApprovalGateStatusBlocked, wantBlocker: BlockerDriftReviewBlocked},
		{name: "source mismatch", mode: DriftReviewModeAutomatic, reviewSource: ReviewSourceExternal, gate: ApprovalGateStatusReady, wantBlocker: BlockerDriftReviewRequired},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, _ := newTestService(t)
			created := createTestPlanAttempt(t, svc, tc.mode, "")
			reviewID := ""
			if tc.reviewSource != "" {
				reviewResult := submitTestReview(t, svc, *created.PlanAttempt, tc.reviewSource, tc.gate)
				reviewID = reviewResult.DriftReview.IntentDriftReviewID
			}
			result, err := svc.ApprovePlanAttempt(context.Background(), ApprovePlanAttemptRequest{
				ProjectID:                 "relay",
				PlanAttemptID:             created.PlanAttempt.PlanAttemptID,
				Approved:                  true,
				AcceptedDriftReviewID:     reviewID,
				DriftAcknowledged:         tc.driftAck,
				NoDriftReviewAcknowledged: tc.noReviewAck,
			})
			if err != nil {
				t.Fatalf("ApprovePlanAttempt error: %v", err)
			}
			if tc.wantOK {
				if !result.OK {
					t.Fatalf("expected ok=true, got %#v", result)
				}
				if result.PlanAttempt.Status != PlanAttemptStatusApproved {
					t.Fatalf("expected approved, got %q", result.PlanAttempt.Status)
				}
				return
			}
			if result.OK || result.BlockerCode != tc.wantBlocker {
				t.Fatalf("expected blocker %q, got %#v", tc.wantBlocker, result)
			}
		})
	}
}

func TestSubmitPlanAttemptCreatesManagedPlanWithLineage(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, DriftReviewModeExternal, "")
	review := submitTestReview(t, svc, *created.PlanAttempt, ReviewSourceExternal, ApprovalGateStatusReady)
	approved, err := svc.ApprovePlanAttempt(context.Background(), ApprovePlanAttemptRequest{
		ProjectID:             "relay",
		PlanAttemptID:         created.PlanAttempt.PlanAttemptID,
		Approved:              true,
		AcceptedDriftReviewID: review.DriftReview.IntentDriftReviewID,
	})
	if err != nil {
		t.Fatalf("ApprovePlanAttempt error: %v", err)
	}
	if !approved.OK {
		t.Fatalf("expected approval ok=true, got %#v", approved)
	}

	artifactHash := created.PlanAttempt.PlanJsonArtifactSha256
	submitted, err := svc.SubmitPlanAttempt(context.Background(), SubmitPlanAttemptRequest{
		ProjectID:                      "relay",
		PlanAttemptID:                  created.PlanAttempt.PlanAttemptID,
		SubmissionConfirmed:            true,
		ReviewedPlanJSONArtifactSHA256: artifactHash,
		AcceptedDriftReviewID:          review.DriftReview.IntentDriftReviewID,
	})
	if err != nil {
		t.Fatalf("SubmitPlanAttempt error: %v", err)
	}
	if !submitted.OK {
		t.Fatalf("expected submit ok=true, got %#v", submitted)
	}
	if submitted.PlanAttempt.Status != PlanAttemptStatusSubmitted || !submitted.PlanAttempt.SubmittedPlanID.Valid {
		t.Fatalf("expected submitted attempt with submitted plan id, got %#v", submitted.PlanAttempt)
	}
	plan := loadPlanByPlanID(t, st, submitted.Plan.PlanID)
	if plan.SubmittedPlanAttemptID.String != created.PlanAttempt.PlanAttemptID ||
		plan.IntentThreadID.String != created.PlanAttempt.IntentThreadID ||
		plan.RootIntentPacketID.String != created.PlanAttempt.RootIntentPacketID ||
		plan.SubmittedIntentPacketID.String != created.PlanAttempt.CurrentIntentPacketID ||
		plan.AcceptedDriftReviewID.String != review.DriftReview.IntentDriftReviewID {
		t.Fatalf("managed plan lineage incorrect: %#v", plan)
	}
	if plan.SourceArtifactPath != created.PlanAttempt.PlanJsonArtifactPath {
		t.Fatalf("expected source artifact path %q, got %q", created.PlanAttempt.PlanJsonArtifactPath, plan.SourceArtifactPath)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 2 {
		t.Fatalf("expected 2 plan passes, got %d", got)
	}
}

// T1: approved attempt + SubmissionConfirmed=false must block and leave plans/plan_passes unchanged.
func TestSubmitPlanAttemptBlocksMissingConfirmation(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, DriftReviewModeDisabled, "")
	approved, err := svc.ApprovePlanAttempt(context.Background(), ApprovePlanAttemptRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
		Approved:      true,
	})
	if err != nil {
		t.Fatalf("ApprovePlanAttempt error: %v", err)
	}
	if !approved.OK {
		t.Fatalf("expected approval ok=true, got %#v", approved)
	}

	// SubmissionConfirmed omitted (false)
	result, err := svc.SubmitPlanAttempt(context.Background(), SubmitPlanAttemptRequest{
		ProjectID:                      "relay",
		PlanAttemptID:                  created.PlanAttempt.PlanAttemptID,
		SubmissionConfirmed:            false,
		ReviewedPlanJSONArtifactSHA256: created.PlanAttempt.PlanJsonArtifactSha256,
	})
	if err != nil {
		t.Fatalf("SubmitPlanAttempt error: %v", err)
	}
	if result.OK || result.BlockerCode != BlockerApprovalRequired {
		t.Fatalf("expected approval_required blocker, got %#v", result)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plans after blocked submit, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes after blocked submit, got %d", got)
	}
}

// T2: approved attempt + wrong ReviewedPlanJSONArtifactSHA256 must block and leave plans/plan_passes unchanged.
func TestSubmitPlanAttemptBlocksWrongArtifactHash(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	created := createTestPlanAttempt(t, svc, DriftReviewModeDisabled, "")
	approved, err := svc.ApprovePlanAttempt(context.Background(), ApprovePlanAttemptRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
		Approved:      true,
	})
	if err != nil {
		t.Fatalf("ApprovePlanAttempt error: %v", err)
	}
	if !approved.OK {
		t.Fatalf("expected approval ok=true, got %#v", approved)
	}

	result, err := svc.SubmitPlanAttempt(context.Background(), SubmitPlanAttemptRequest{
		ProjectID:                      "relay",
		PlanAttemptID:                  created.PlanAttempt.PlanAttemptID,
		SubmissionConfirmed:            true,
		ReviewedPlanJSONArtifactSHA256: testSHA256, // wrong hash
	})
	if err != nil {
		t.Fatalf("SubmitPlanAttempt error: %v", err)
	}
	if result.OK || result.BlockerCode != BlockerArtifactHashMismatch {
		t.Fatalf("expected artifact_hash_mismatch blocker, got %#v", result)
	}
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plans after blocked submit, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes after blocked submit, got %d", got)
	}
}

func TestDirectSubmitPlanStillWorksWithoutAttemptLineage(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{
		RawJSON:            mustMarshalPlan(t, validPlannerPassPlan()),
		SourceArtifactPath: "handoffs/planner/direct.planner-pass-plan.json",
	})
	if err != nil {
		t.Fatalf("SubmitPlan error: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid report, got %#v", result.Report.Issues)
	}
	plan := loadPlanByPlanID(t, st, result.Plan.PlanID)
	if plan.SubmittedPlanAttemptID.Valid ||
		plan.IntentThreadID.Valid ||
		plan.RootIntentPacketID.Valid ||
		plan.SubmittedIntentPacketID.Valid ||
		plan.AcceptedDriftReviewID.Valid {
		t.Fatalf("expected null attempt lineage for direct submit, got %#v", plan)
	}
}

func createTestPlanAttempt(t *testing.T, svc *Service, mode string, planID string) *PlanAttemptResult {
	t.Helper()

	raw := planRawWithID(t, planID)
	_, hash, err := canonicalRawPlanJSON(raw)
	if err != nil {
		t.Fatalf("canonicalRawPlanJSON: %v", err)
	}
	result, err := svc.CreatePlanAttemptWithIntent(context.Background(), CreatePlanAttemptWithIntentRequest{
		ProjectID:      "relay",
		PlanAttemptID:  "attempt-" + sanitizeSlug(mode) + "-" + generateSlug(),
		IntentPacketID: "intent-" + sanitizeSlug(mode) + "-" + generateSlug(),
		IntentThreadID: "thread-" + sanitizeSlug(mode) + "-" + generateSlug(),
		PlanArtifactRef: PlanArtifactRef{
			Path:         "handoffs/planner/" + sanitizeSlug(mode) + ".planner-pass-plan.json",
			SHA256:       hash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON:     raw,
		DriftReviewMode: mode,
		ModelTier:       ModelTierStandard,
		IntentPacket: IntentPacketInput{
			Summary:            "Original request",
			LiteralUserRequest: "Create a reviewed draft plan attempt.",
			Constraints:        []string{"No route changes."},
			Source:             IntentSource{CapturedFrom: CapturedFromPlannerChat, CapturedBy: "tester", SourceArtifactPath: "handoffs/source.md"},
			RedactionStatus:    RedactionStatusVerifiedNoSecrets,
		},
	})
	if err != nil {
		t.Fatalf("CreatePlanAttemptWithIntent error: %v", err)
	}
	if !result.OK {
		t.Fatalf("CreatePlanAttemptWithIntent blocked: %#v", result)
	}
	return result
}

func submitTestReview(t *testing.T, svc *Service, attempt store.PlanAttempt, source string, gate string) *PlanAttemptResult {
	t.Helper()

	if gate == "" {
		gate = ApprovalGateStatusReady
	}
	packetResult, err := svc.GetPlanIntentReviewPacket(context.Background(), GetPlanIntentReviewPacketRequest{
		ProjectID:     "relay",
		PlanAttemptID: attempt.PlanAttemptID,
	})
	if err != nil {
		t.Fatalf("GetPlanIntentReviewPacket error: %v", err)
	}
	if !packetResult.OK || packetResult.ReviewPacket == nil || packetResult.ReviewPacket.PacketHash == "" {
		t.Fatalf("expected review packet hash, got %#v", packetResult)
	}
	result, err := svc.SubmitIntentDriftReview(context.Background(), SubmitIntentDriftReviewRequest{
		ProjectID:     "relay",
		PlanAttemptID: attempt.PlanAttemptID,
		DriftReview: DriftReviewInput{
			IntentDriftReviewID:    "review-" + sanitizeSlug(source) + "-" + generateSlug(),
			PlanAttemptID:          attempt.PlanAttemptID,
			IntentThreadID:         attempt.IntentThreadID,
			RootIntentPacketID:     attempt.RootIntentPacketID,
			ReviewedIntentPacketID: attempt.CurrentIntentPacketID,
			ReviewPacketHash:       packetResult.ReviewPacket.PacketHash,
			ReviewSource:           source,
			SubmittedBy:            "tester",
			SourceArtifactPath:     "reviews/review.json",
			OverallAlignment:       OverallAlignmentAligned,
			Confidence:             0.95,
			FindingsJSON:           json.RawMessage(`[]`),
			RecommendedAction:      RecommendedActionApprove,
			ApprovalGateStatus:     gate,
			ModelMetadataJSON:      json.RawMessage(`{"model":"test"}`),
			InputHash:              testSHA256,
			OutputHash:             testSHA256,
		},
	})
	if err != nil {
		t.Fatalf("SubmitIntentDriftReview error: %v", err)
	}
	if !result.OK {
		t.Fatalf("SubmitIntentDriftReview blocked: %#v", result)
	}
	return result
}

func planRawWithID(t *testing.T, planID string) json.RawMessage {
	t.Helper()

	plan := validPlannerPassPlan()
	if planID != "" {
		plan.PlanMeta.PlanID = planID
	}
	return mustMarshalPlan(t, plan)
}

func loadAttempt(t *testing.T, st *store.Store, attemptID string) store.PlanAttempt {
	t.Helper()

	attempt, err := generated.New(st.DB()).GetPlanAttemptByID(context.Background(), attemptID)
	if err != nil {
		t.Fatalf("GetPlanAttemptByID: %v", err)
	}
	return attempt
}

func loadIntentPacket(t *testing.T, st *store.Store, intentPacketID string) store.IntentPacket {
	t.Helper()

	packet, err := generated.New(st.DB()).GetIntentPacketByID(context.Background(), intentPacketID)
	if err != nil {
		t.Fatalf("GetIntentPacketByID: %v", err)
	}
	return packet
}

func loadPlanByPlanID(t *testing.T, st *store.Store, planID string) store.Plan {
	t.Helper()

	plan, err := generated.New(st.DB()).GetPlanByPlanID(context.Background(), planID)
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	return plan
}

func assertAttemptRowsEqual(t *testing.T, before store.PlanAttempt, after store.PlanAttempt) {
	t.Helper()

	if before != after {
		t.Fatalf("expected attempt unchanged\nbefore: %#v\nafter: %#v", before, after)
	}
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
