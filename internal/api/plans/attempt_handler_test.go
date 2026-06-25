package plans

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appplans "relay/internal/app/plans"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func TestPlanAttemptHTTPCreateReviewApproveSubmitFlow(t *testing.T) {
	_, st, router := newAttemptAPITestServer(t)

	rawPlan, planHash := attemptTestRawPlan(t, "plan-attempt-http")
	createBody := mustJSON(t, CreatePlanAttemptWithIntentAPIRequest{
		PlanAttemptID: "attempt-http-1",
		PlanArtifactRef: PlanArtifactRefAPI{
			Path:         "handoffs/plans/attempt-http.json",
			SHA256:       planHash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON:     RawPlanJSONAPI{Content: rawPlan, ContentHash: planHash},
		DriftReviewMode: appplans.DriftReviewModeExternal,
		IntentPacket: IntentPacketInputAPI{
			Summary:            "Expose plan attempt transports.",
			LiteralUserRequest: "Start PASS-003.",
			Constraints:        []string{"Do not create runs."},
			Source: IntentSourceAPI{
				CapturedFrom:       appplans.CapturedFromPlannerChat,
				CapturedBy:         "api-test",
				SourceArtifactPath: "handoffs/planner/pass-003.md",
			},
			RedactionStatus: appplans.RedactionStatusVerifiedNoSecrets,
		},
	})
	createResp := doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts", createBody, http.StatusCreated)
	if !createResp.Success || createResp.PlanAttempt == nil {
		t.Fatalf("expected create success with attempt, got %+v", createResp)
	}
	if createResp.PlanAttempt.WorkflowState != "review_packet_available" {
		t.Fatalf("unexpected workflowState %q", createResp.PlanAttempt.WorkflowState)
	}
	if got := countAttemptRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("create attempt should not create managed plans, got %d", got)
	}

	packetResp := doAttemptRequest(t, router, http.MethodGet, "/api/projects/relay/plan-attempts/attempt-http-1/intent-review-packet", nil, http.StatusOK)
	if packetResp.ReviewPacket == nil {
		t.Fatal("expected review packet")
	}
	if !packetResp.ReviewPacket.RetrievalSemantics.RetrievalOnly || packetResp.ReviewPacket.RetrievalSemantics.ModelCallPerformed || packetResp.ReviewPacket.RetrievalSemantics.StateMutated {
		t.Fatalf("unexpected retrieval semantics: %+v", packetResp.ReviewPacket.RetrievalSemantics)
	}
	if status := attemptStatus(t, st.DB(), "attempt-http-1"); status != appplans.PlanAttemptStatusDraft {
		t.Fatalf("get packet mutated status to %q", status)
	}

	reviewBody := mustJSON(t, SubmitIntentDriftReviewAPIRequest{DriftReview: DriftReviewInputAPI{
		IntentDriftReviewID:    "review-http-1",
		PlanAttemptID:          "attempt-http-1",
		IntentThreadID:         packetResp.ReviewPacket.IntentThreadID,
		RootIntentPacketID:     packetResp.ReviewPacket.RootIntentPacket.IntentPacketID,
		ReviewedIntentPacketID: packetResp.ReviewPacket.ReviewedIntentPacket.IntentPacketID,
		ReviewPacketHash:       packetResp.ReviewPacket.PacketHash,
		ReviewSource:           appplans.ReviewSourceExternal,
		SubmittedBy:            "api-test",
		SourceArtifactPath:     "reviews/review-http-1.json",
		OverallAlignment:       appplans.OverallAlignmentAligned,
		Confidence:             0.91,
		FindingsJSON:           json.RawMessage(`[]`),
		RecommendedAction:      appplans.RecommendedActionApprove,
		ApprovalGateStatus:     appplans.ApprovalGateStatusReady,
		InputHash:              sha256StringForAttemptTest("input"),
		OutputHash:             sha256StringForAttemptTest("output"),
	}})
	reviewResp := doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts/attempt-http-1/intent-drift-reviews", reviewBody, http.StatusOK)
	if !reviewResp.Success || reviewResp.DriftReview == nil {
		t.Fatalf("expected review success, got %+v", reviewResp)
	}
	if status := attemptStatus(t, st.DB(), "attempt-http-1"); status != appplans.PlanAttemptStatusDraft {
		t.Fatalf("review should not approve attempt, got status %q", status)
	}

	approveResp := doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts/attempt-http-1/approve", mustJSON(t, ApprovePlanAttemptAPIRequest{Approved: true, AcceptedDriftReviewID: "review-http-1"}), http.StatusOK)
	if !approveResp.Success || approveResp.PlanAttempt.Status != appplans.PlanAttemptStatusApproved {
		t.Fatalf("expected approved attempt, got %+v", approveResp)
	}
	if got := countAttemptRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("approve should not create managed plans, got %d", got)
	}

	submitResp := doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts/attempt-http-1/submit", []byte(`{}`), http.StatusCreated)
	if !submitResp.Success || submitResp.Plan == nil || submitResp.Plan.PlanID != "plan-attempt-http" {
		t.Fatalf("expected submit success with managed plan, got %+v", submitResp)
	}
	if got := countAttemptRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("submit should create one managed plan, got %d", got)
	}
}

func TestPlanAttemptHTTPBlockersPreserveCodes(t *testing.T) {
	_, _, router := newAttemptAPITestServer(t)

	resp := doAttemptRequest(t, router, http.MethodGet, "/api/projects/relay/plan-attempts/missing/intent-review-packet", nil, http.StatusNotFound)
	if resp.BlockerCode != string(appplans.BlockerUnknownAttempt) {
		t.Fatalf("expected unknown_attempt blocker, got %+v", resp)
	}

	rawPlan, planHash := attemptTestRawPlan(t, "plan-attempt-hash")
	createBody := mustJSON(t, CreatePlanAttemptWithIntentAPIRequest{
		PlanArtifactRef: PlanArtifactRefAPI{
			Path:         "handoffs/plans/hash.json",
			SHA256:       planHash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON: RawPlanJSONAPI{Content: rawPlan, ContentHash: sha256StringForAttemptTest("wrong")},
		IntentPacket: IntentPacketInputAPI{
			Summary:            "Hash mismatch.",
			LiteralUserRequest: "Reject this.",
			Constraints:        []string{},
			Source:             IntentSourceAPI{CapturedFrom: appplans.CapturedFromPlannerChat, CapturedBy: "api-test", SourceArtifactPath: "source.md"},
		},
	})
	resp = doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts", createBody, http.StatusConflict)
	if resp.BlockerCode != string(appplans.BlockerArtifactHashMismatch) {
		t.Fatalf("expected artifact_hash_mismatch blocker, got %+v", resp)
	}
}

func newAttemptAPITestServer(t *testing.T) (*Handler, *store.Store, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	h := NewHandler(appplans.NewService(st), appplans.NewOrchestratorWorkService(st))
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		MountRoutes(r, h)
	})
	return h, st, router
}

func doAttemptRequest(t *testing.T, router http.Handler, method, path string, body []byte, want int) PlanAttemptAPIResponse {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, want, rec.Code, rec.Body.String())
	}
	var resp PlanAttemptAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func attemptTestRawPlan(t *testing.T, planID string) (json.RawMessage, string) {
	t.Helper()
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-25T00:00:00Z",
			Title:         "Attempt transport plan",
			Goal:          "Exercise attempt transport flow.",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &appplans.MCPCapabilityProfile{
				ProfileID:            "attempt-api-tests",
				Mode:                 "submission_only",
				ContextBrokerEnabled: attemptBoolPtr(false),
			},
		},
		SourceIntent: appplans.SourceIntent{Summary: "Exercise attempt transport flow."},
		GlobalContextRules: &appplans.GlobalContextRules{
			DefaultSourceOfTruth:   "Relay managed plan records.",
			PlannerContextBoundary: "Transport tests validate backend behavior only.",
			ForbiddenContextDomains: []string{
				"GitHub issues",
			},
		},
		Passes: []appplans.PlanPassInput{{
			PassID:                 "PASS-001",
			Sequence:               1,
			Name:                   "Transport",
			Goal:                   "Add transport tests.",
			IntendedExecutionScope: []string{"HTTP and MCP"},
			NonGoals:               []string{"No UI"},
			Dependencies:           []string{},
			Status:                 "planned",
			PassType:               "backend_vertical_slice",
			ContextPlan: appplans.ContextPlan{
				RequiredRepositories: []string{"relay"},
				SeedSearchTerms: []appplans.ContextSearchTerm{
					{RepoID: "relay", Query: "plan attempts", Purpose: "Locate attempt transport flow.", Required: attemptBoolPtr(true)},
				},
				SeedFilesToRead: []appplans.ContextFileRead{
					{RepoID: "relay", Path: "internal/api/plans/attempt_handler.go", Purpose: "Validate transport handlers.", Required: attemptBoolPtr(true)},
				},
				ContextCoverageExpectations: []string{"Attempt transports remain action-separated."},
				BlockedIfMissing:            []string{"Attempt transport code cannot be located."},
			},
			SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
				RequireGitStatus:   attemptBoolPtr(true),
				RequireCommitSHA:   attemptBoolPtr(false),
				AllowDirtyWorktree: attemptBoolPtr(true),
			},
			HandoffReadinessCriteria: []string{"Transport behavior is covered."},
		}},
	}
	raw := mustJSON(t, plan)
	return raw, canonicalHashForAttemptTest(t, raw)
}

func attemptBoolPtr(v bool) *bool {
	return &v
}

func canonicalHashForAttemptTest(t *testing.T, raw []byte) string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal canonical hash input: %v", err)
	}
	canonical := mustJSON(t, doc)
	return sha256BytesForAttemptTest(canonical)
}

func sha256StringForAttemptTest(s string) string {
	return sha256BytesForAttemptTest([]byte(s))
}

func sha256BytesForAttemptTest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func countAttemptRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func attemptStatus(t *testing.T, db *sql.DB, planAttemptID string) string {
	t.Helper()
	var status string
	if err := db.QueryRow("SELECT status FROM plan_attempts WHERE plan_attempt_id = ?", planAttemptID).Scan(&status); err != nil {
		t.Fatalf("attempt status: %v", err)
	}
	return status
}
