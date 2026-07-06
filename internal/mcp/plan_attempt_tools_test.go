package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	appplans "relay/internal/app/plans"
	"relay/internal/store"
)

func baseToolNamesForTest() []string {
	return []string{
		"submit_test_audit_packet",
		"create_run_from_planner_handoff",
		"create_run_from_planner_handoff_file",
		"validate_planner_handoff_for_compile",
		"submit_planner_pass_plan",
		"list_open_runs",
		"get_run_status",
		"submit_audit_packet",
		toolCreatePlanAttemptWithIntent,
		toolGetPlanIntentReviewPacket,
		toolSubmitIntentDriftReview,
		toolRevisePlanAttempt,
		toolVoidPlanAttempt,
		toolApprovePlanAttempt,
		toolSubmitPlanAttempt,
		toolCreatePlanSeed,
		toolListPlanSeeds,
		toolGetPlanSeed,
		toolGetPlanSeedPlanningContext,
		toolCreatePlanAttemptFromSeed,
		toolUpdatePlanSeed,
		toolDeferPlanSeed,
		toolRejectPlanSeed,
	}
}

func TestPlanAttemptMCPToolsListIncludesAttemptActions(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileRestricted})
	names := toolNames(srv.tools)
	for _, want := range []string{
		toolCreatePlanAttemptWithIntent,
		toolGetPlanIntentReviewPacket,
		toolSubmitIntentDriftReview,
		toolRevisePlanAttempt,
		toolVoidPlanAttempt,
		toolApprovePlanAttempt,
		toolSubmitPlanAttempt,
	} {
		if !hasNameForPlanAttemptTest(names, want) {
			t.Fatalf("expected tools/list to include %q; got %v", want, names)
		}
	}
}

// TestPlanAttemptMCPSubmitSchemaIsNotByIDSchema verifies ToolSubmitPlanAttempt
// uses planAttemptSubmitSchema and not the bare ID-only schema.
func TestPlanAttemptMCPSubmitSchemaIsNotByIDSchema(t *testing.T) {
	schema := ToolSubmitPlanAttempt.InputSchema
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatalf("unmarshal submit schema: %v", err)
	}
	required, ok := s["required"].([]any)
	if !ok {
		t.Fatalf("expected required array in submit schema, got %T", s["required"])
	}
	wantFields := []string{"submission_confirmed", "reviewed_plan_json_artifact_sha256"}
	for _, want := range wantFields {
		found := false
		for _, r := range required {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in submit schema required fields, got %v", want, required)
		}
	}
}

// T6: MCP create_plan_attempt_with_intent with raw_plan_json.content/content_hash wrapper shape succeeds.
func TestPlanAttemptMCPCreateAcceptsWrapperShape(t *testing.T) {
	st := setupTestStore(t)
	srv := NewServer(discardLogger(), &MCPDeps{Store: st, ToolProfile: ToolProfileRestricted})

	rawPlanContent, planHash := mcpAttemptRawPlan(t, "plan-attempt-mcp-wrapper")

	// Build the MCP-shaped request using the wrapper form
	mcpArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-mcp-wrapper-1",
		"plan_artifact_ref": map[string]any{
			"path":          "handoffs/plans/attempt-mcp-wrapper.json",
			"sha256":        planHash,
			"artifact_kind": "planner-pass-plan-json",
		},
		"raw_plan_json": map[string]any{
			"content":      json.RawMessage(rawPlanContent),
			"content_hash": planHash,
		},
		"drift_review_mode": appplans.DriftReviewModeManual,
		"intent_packet": map[string]any{
			"summary":              "MCP wrapper shape test.",
			"literal_user_request": "Test wrapper semantics.",
			"constraints":          []string{"No runs."},
			"source": map[string]any{
				"captured_from":        appplans.CapturedFromPlannerChat,
				"captured_by":          "mcp-test",
				"source_artifact_path": "handoffs/planner/pass-003a.md",
			},
		},
	}

	createResult := srv.HandleCreatePlanAttemptWithIntent(mcpAttemptJSON(t, mcpArgs))
	if createResult.IsError {
		t.Fatalf("create_plan_attempt_with_intent returned error: %s", createResult.Content[0].Text)
	}
	var createOut planAttemptToolOutput
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &createOut); err != nil {
		t.Fatalf("decode create output: %v", err)
	}
	if !createOut.OK {
		t.Fatalf("expected create ok, got %+v", createOut)
	}
	if got := mcpAttemptCountRows(t, st, "plans"); got != 0 {
		t.Fatalf("create attempt should not create managed plans, got %d", got)
	}
	if got := mcpAttemptCountRows(t, st, "plan_attempts"); got != 1 {
		t.Fatalf("expected 1 plan_attempts row, got %d", got)
	}
}

// T7: MCP create_plan_attempt_with_intent with wrong raw_plan_json.content_hash blocks and creates no row.
func TestPlanAttemptMCPCreateBlocksWrongContentHash(t *testing.T) {
	st := setupTestStore(t)
	srv := NewServer(discardLogger(), &MCPDeps{Store: st, ToolProfile: ToolProfileRestricted})

	rawPlanContent, _ := mcpAttemptRawPlan(t, "plan-attempt-mcp-wrong-hash")
	wrongHash := mcpAttemptSHA256([]byte("definitely-not-the-canonical-content"))

	mcpArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-mcp-wrong-hash",
		"plan_artifact_ref": map[string]any{
			"path":          "handoffs/plans/attempt-mcp-wrong-hash.json",
			"sha256":        wrongHash, // deliberately mismatched
			"artifact_kind": "planner-pass-plan-json",
		},
		"raw_plan_json": map[string]any{
			"content":      json.RawMessage(rawPlanContent),
			"content_hash": wrongHash, // wrong hash for the content
		},
		"drift_review_mode": appplans.DriftReviewModeManual,
		"intent_packet": map[string]any{
			"summary":              "Wrong hash test.",
			"literal_user_request": "Reject this.",
			"constraints":          []string{},
			"source": map[string]any{
				"captured_from":        appplans.CapturedFromPlannerChat,
				"captured_by":          "mcp-test",
				"source_artifact_path": "source.md",
			},
		},
	}

	createResult := srv.HandleCreatePlanAttemptWithIntent(mcpAttemptJSON(t, mcpArgs))
	if !createResult.IsError {
		t.Fatal("expected create_plan_attempt_with_intent with wrong content_hash to return a tool error")
	}
	var out planAttemptToolOutput
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &out); err != nil {
		t.Fatalf("decode create error output: %v", err)
	}
	if out.BlockerCode != string(appplans.BlockerArtifactHashMismatch) {
		t.Fatalf("expected artifact_hash_mismatch blocker, got %+v", out)
	}
	if got := mcpAttemptCountRows(t, st, "plan_attempts"); got != 0 {
		t.Fatalf("expected 0 plan_attempts after blocked create, got %d", got)
	}
}

// T8: MCP submit_plan_attempt after approval gates work correctly.
// Missing confirmation blocks; correct confirmation + hash succeeds.
func TestPlanAttemptMCPSubmitAfterApprovalGates(t *testing.T) {
	st := setupTestStore(t)
	srv := NewServer(discardLogger(), &MCPDeps{Store: st, ToolProfile: ToolProfileRestricted})

	rawPlanContent, planHash := mcpAttemptRawPlan(t, "plan-attempt-mcp-submit")

	// Create attempt via wrapper shape
	mcpCreateArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-mcp-submit-1",
		"plan_artifact_ref": map[string]any{
			"path":          "handoffs/plans/attempt-mcp-submit.json",
			"sha256":        planHash,
			"artifact_kind": "planner-pass-plan-json",
		},
		"raw_plan_json": map[string]any{
			"content":      json.RawMessage(rawPlanContent),
			"content_hash": planHash,
		},
		"drift_review_mode": appplans.DriftReviewModeDisabled,
		"intent_packet": map[string]any{
			"summary":              "MCP submit gate test.",
			"literal_user_request": "Test submit gates.",
			"constraints":          []string{"No runs."},
			"source": map[string]any{
				"captured_from":        appplans.CapturedFromPlannerChat,
				"captured_by":          "mcp-test",
				"source_artifact_path": "handoffs/planner/pass-003a.md",
			},
		},
	}
	createResult := srv.HandleCreatePlanAttemptWithIntent(mcpAttemptJSON(t, mcpCreateArgs))
	if createResult.IsError {
		t.Fatalf("create: %s", createResult.Content[0].Text)
	}

	// Approve
	approveResult := srv.HandleApprovePlanAttempt(mcpAttemptJSON(t, appplans.ApprovePlanAttemptRequest{
		ProjectID:     "relay",
		PlanAttemptID: "attempt-mcp-submit-1",
		Approved:      true,
	}))
	if approveResult.IsError {
		t.Fatalf("approve: %s", approveResult.Content[0].Text)
	}

	// Submit without confirmation — must block
	missingConfirmResult := srv.HandleSubmitPlanAttempt(mcpAttemptJSON(t, appplans.SubmitPlanAttemptRequest{
		ProjectID:                      "relay",
		PlanAttemptID:                  "attempt-mcp-submit-1",
		SubmissionConfirmed:            false,
		ReviewedPlanJSONArtifactSHA256: planHash,
	}))
	if !missingConfirmResult.IsError {
		t.Fatal("expected submit without confirmation to be a tool error")
	}
	var blockedOut planAttemptToolOutput
	if err := json.Unmarshal([]byte(missingConfirmResult.Content[0].Text), &blockedOut); err != nil {
		t.Fatalf("decode blocked output: %v", err)
	}
	if blockedOut.BlockerCode != string(appplans.BlockerApprovalRequired) {
		t.Fatalf("expected approval_required blocker, got %+v", blockedOut)
	}
	if got := mcpAttemptCountRows(t, st, "plans"); got != 0 {
		t.Fatalf("expected 0 plans after blocked submit, got %d", got)
	}

	// Submit with correct confirmation and hash — must succeed
	submitResult := srv.HandleSubmitPlanAttempt(mcpAttemptJSON(t, appplans.SubmitPlanAttemptRequest{
		ProjectID:                      "relay",
		PlanAttemptID:                  "attempt-mcp-submit-1",
		SubmissionConfirmed:            true,
		ReviewedPlanJSONArtifactSHA256: planHash,
	}))
	if submitResult.IsError {
		t.Fatalf("expected submit success, got: %s", submitResult.Content[0].Text)
	}
	var submitOut planAttemptToolOutput
	if err := json.Unmarshal([]byte(submitResult.Content[0].Text), &submitOut); err != nil {
		t.Fatalf("decode submit output: %v", err)
	}
	if !submitOut.OK {
		t.Fatalf("expected submit ok, got %+v", submitOut)
	}
	if got := mcpAttemptCountRows(t, st, "plans"); got != 1 {
		t.Fatalf("expected 1 plan after successful submit, got %d", got)
	}
}

// TestPlanAttemptMCPCreateAndSubmitBeforeApproval is kept from PASS-003 but
// updated to use the MCP wrapper shape for create and the new gate fields for submit.
func TestPlanAttemptMCPCreateAndSubmitBeforeApproval(t *testing.T) {
	st := setupTestStore(t)
	srv := NewServer(discardLogger(), &MCPDeps{Store: st, ToolProfile: ToolProfileRestricted})
	rawPlan, planHash := mcpAttemptRawPlan(t, "plan-attempt-mcp")

	// Use MCP wrapper shape for create (T6 coverage)
	mcpCreateArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-mcp-1",
		"plan_artifact_ref": map[string]any{
			"path":          "handoffs/plans/attempt-mcp.json",
			"sha256":        planHash,
			"artifact_kind": "planner-pass-plan-json",
		},
		"raw_plan_json": map[string]any{
			"content":      json.RawMessage(rawPlan),
			"content_hash": planHash,
		},
		"drift_review_mode": appplans.DriftReviewModeManual,
		"intent_packet": map[string]any{
			"summary":              "Expose MCP attempt tools.",
			"literal_user_request": "Start PASS-003.",
			"constraints":          []string{"Do not create runs."},
			"source": map[string]any{
				"captured_from":        appplans.CapturedFromPlannerChat,
				"captured_by":          "mcp-test",
				"source_artifact_path": "handoffs/planner/pass-003.md",
			},
		},
	}
	createResult := srv.HandleCreatePlanAttemptWithIntent(mcpAttemptJSON(t, mcpCreateArgs))
	if createResult.IsError {
		t.Fatalf("create_plan_attempt_with_intent returned error: %s", createResult.Content[0].Text)
	}
	var createOut planAttemptToolOutput
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &createOut); err != nil {
		t.Fatalf("decode create output: %v", err)
	}
	if !createOut.OK {
		t.Fatalf("expected create ok, got %+v", createOut)
	}
	if got := mcpAttemptCountRows(t, st, "plans"); got != 0 {
		t.Fatalf("create attempt should not create managed plans, got %d", got)
	}

	// Submit before approval (still requires gate fields, still blocked by approval_required)
	submitResult := srv.HandleSubmitPlanAttempt(mcpAttemptJSON(t, appplans.SubmitPlanAttemptRequest{
		ProjectID:                      "relay",
		PlanAttemptID:                  "attempt-mcp-1",
		SubmissionConfirmed:            true,
		ReviewedPlanJSONArtifactSHA256: planHash,
	}))
	if !submitResult.IsError {
		t.Fatal("expected submit_plan_attempt before approval to be a structured tool error")
	}
	var submitOut planAttemptToolOutput
	if err := json.Unmarshal([]byte(submitResult.Content[0].Text), &submitOut); err != nil {
		t.Fatalf("decode submit blocker: %v", err)
	}
	if submitOut.Status != "blocked" || submitOut.BlockerCode != string(appplans.BlockerApprovalRequired) {
		t.Fatalf("expected approval_required blocker, got %+v", submitOut)
	}
}

func TestPlanAttemptMCPDependencyMissingReturnsToolError(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileRestricted})
	result := srv.HandleGetPlanIntentReviewPacket(mcpAttemptJSON(t, appplans.GetPlanIntentReviewPacketRequest{
		ProjectID:     "relay",
		PlanAttemptID: "attempt-missing",
	}))
	if !result.IsError {
		t.Fatal("expected dependency-missing tool error")
	}
	var out planAttemptToolOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("decode dependency error: %v", err)
	}
	if out.BlockerCode != "dependency_error" {
		t.Fatalf("expected dependency_error, got %+v", out)
	}
}

func hasNameForPlanAttemptTest(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func mcpAttemptRawPlan(t *testing.T, planID string) (json.RawMessage, string) {
	t.Helper()
	// Build a plan that satisfies the plan validator so that submit can succeed.
	// This mirrors the structure used in the HTTP handler test (attemptTestRawPlan).
	boolTrue := true
	boolFalse := false
	plan := map[string]any{
		"plan_meta": map[string]any{
			"plan_id":        planID,
			"schema_version": "2.0.0",
			"created_at":     "2026-06-25T00:00:00Z",
			"title":          "MCP attempt plan",
			"goal":           "Exercise MCP attempt flow.",
			"repo_target":    "Paintersrp/relay",
			"branch_context": "main",
			"status":         "active",
			"project_id":     "relay",
			"project_context": map[string]any{
				"primary_project":         "relay",
				"primary_repository":      "relay",
				"contract_repository":     "relay-specs",
				"github_role":             "repo_host_and_origin_only",
				"excluded_github_domains": []string{"issues"},
				"local_first_assumption":  "Relay is the local source of context.",
			},
			"mcp_capability_profile": map[string]any{
				"profile_id":             "mcp-test-profile",
				"mode":                   "submission_only",
				"context_broker_enabled": false,
			},
		},
		"source_intent": map[string]any{"summary": "Exercise MCP attempt flow."},
		"global_context_rules": map[string]any{
			"default_source_of_truth":   "Relay managed plan records.",
			"planner_context_boundary":  "Transport tests validate backend behavior only.",
			"forbidden_context_domains": []string{"GitHub issues"},
		},
		"passes": []any{
			map[string]any{
				"pass_id":                  "PASS-001",
				"sequence":                 1,
				"name":                     "MCP transport",
				"goal":                     "Add MCP transport tests.",
				"intended_execution_scope": []string{"internal/mcp"},
				"non_goals":                []string{"No UI"},
				"dependencies":             []string{},
				"status":                   "planned",
				"pass_type":                "backend_vertical_slice",
				"risk_level":               "low",
				"context_plan": map[string]any{
					"required_repositories": []string{"relay"},
					"seed_search_terms": []any{
						map[string]any{
							"repo_id":  "relay",
							"query":    "plan attempts",
							"purpose":  "Locate attempt transport flow.",
							"required": &boolTrue,
						},
					},
					"seed_files_to_read": []any{
						map[string]any{
							"repo_id":  "relay",
							"path":     "internal/mcp/plan_attempt_tools.go",
							"purpose":  "Validate MCP handlers.",
							"required": &boolTrue,
						},
					},
					"context_coverage_expectations": []string{"MCP handlers remain action-separated."},
					"blocked_if_missing":            []string{"MCP transport code cannot be located."},
				},
				"source_snapshot_requirements": map[string]any{
					"require_git_status":   &boolTrue,
					"require_commit_sha":   &boolFalse,
					"allow_dirty_worktree": &boolTrue,
				},
				"handoff_readiness_criteria": []string{"MCP transport behavior is covered."},
				"context_budget": map[string]any{
					"max_files":          12,
					"max_bytes":          131072,
					"max_search_results": 40,
					"max_context_lines":  600,
				},
			},
		},
	}
	raw := mcpAttemptJSON(t, plan)
	return raw, mcpAttemptCanonicalHash(t, raw)
}

func mcpAttemptCanonicalHash(t *testing.T, raw []byte) string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal canonical hash input: %v", err)
	}
	canonical := mcpAttemptJSON(t, doc)
	return mcpAttemptSHA256(canonical)
}

func mcpAttemptSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mcpAttemptJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func mcpAttemptCountRows(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var count int
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestPlanAttemptMCPWorkflowActions(t *testing.T) {
	st := setupTestStore(t)
	srv := NewServer(discardLogger(), &MCPDeps{Store: st, ToolProfile: ToolProfileRestricted})

	rawPlan, planHash := mcpAttemptRawPlan(t, "plan-workflow")

	// 1. Create original attempt
	createArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-orig",
		"plan_artifact_ref": map[string]any{
			"path":          "handoffs/plans/workflow.json",
			"sha256":        planHash,
			"artifact_kind": "planner-pass-plan-json",
		},
		"raw_plan_json": map[string]any{
			"content":      json.RawMessage(rawPlan),
			"content_hash": planHash,
		},
		"drift_review_mode": appplans.DriftReviewModeDisabled,
		"intent_packet": map[string]any{
			"summary":              "Original request",
			"literal_user_request": "Original request text",
			"constraints":          []string{},
			"source": map[string]any{
				"captured_from":        "planner_chat",
				"captured_by":          "tester",
				"source_artifact_path": "source.md",
			},
		},
	}
	createRes := srv.HandleCreatePlanAttemptWithIntent(mcpAttemptJSON(t, createArgs))
	if createRes.IsError {
		t.Fatalf("create failed: %s", createRes.Content[0].Text)
	}

	// 2. Get review packet
	getArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-orig",
	}
	getRes := srv.HandleGetPlanIntentReviewPacket(mcpAttemptJSON(t, getArgs))
	if getRes.IsError {
		t.Fatalf("get packet failed: %s", getRes.Content[0].Text)
	}
	var getOut planAttemptToolOutput
	if err := json.Unmarshal([]byte(getRes.Content[0].Text), &getOut); err != nil {
		t.Fatalf("unmarshal get packet: %v", err)
	}
	if !getOut.OK || getOut.ReviewPacket == nil {
		t.Fatalf("expected ok review packet, got %+v", getOut)
	}
	if !getOut.ReviewPacket.RetrievalSemantics.RetrievalOnly || getOut.ReviewPacket.RetrievalSemantics.StateMutated {
		t.Fatalf("unexpected retrieval semantics: %+v", getOut.ReviewPacket.RetrievalSemantics)
	}

	// 3. Submit drift review (evidence only)
	reviewArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-orig",
		"drift_review": map[string]any{
			"intent_drift_review_id":    "intent-drift-review-2026-06-25-external-mcp",
			"plan_attempt_id":           "attempt-orig",
			"intent_thread_id":          getOut.ReviewPacket.IntentThreadID,
			"root_intent_packet_id":     getOut.ReviewPacket.RootIntentPacket.IntentPacketID,
			"reviewed_intent_packet_id": getOut.ReviewPacket.ReviewedIntentPacket.IntentPacketID,
			"review_packet_hash":        getOut.ReviewPacket.PacketHash,
			"review_source":             "external",
			"submitted_by":              "tester",
			"source_artifact_path":      "reviews/external.json",
			"overall_alignment":         "aligned",
			"confidence":                0.95,
			"findings_json":             json.RawMessage(`[]`),
			"recommended_action":        "approve",
			"approval_gate_status":      "ready",
			"model_metadata_json":       json.RawMessage(`{"model":"fake"}`),
			"input_hash":                planHash,
			"output_hash":               planHash,
		},
	}
	reviewRes := srv.HandleSubmitIntentDriftReview(mcpAttemptJSON(t, reviewArgs))
	if reviewRes.IsError {
		t.Fatalf("submit review failed: %s", reviewRes.Content[0].Text)
	}
	if got := mcpAttemptCountRows(t, st, "plans"); got != 0 {
		t.Fatalf("expected 0 plans after review submission, got %d", got)
	}

	// 4. Revise plan attempt
	reviseArgs := map[string]any{
		"project_id":           "relay",
		"plan_attempt_id":      "attempt-orig",
		"new_plan_attempt_id":  "attempt-revised",
		"new_intent_packet_id": "intent-revised",
		"plan_artifact_ref": map[string]any{
			"path":          "handoffs/plans/workflow-revised.json",
			"sha256":        planHash,
			"artifact_kind": "planner-pass-plan-json",
		},
		"raw_plan_json": map[string]any{
			"content":      json.RawMessage(rawPlan),
			"content_hash": planHash,
		},
		"new_intent_packet": map[string]any{
			"summary":              "Revision request",
			"literal_user_request": "Revision request text",
			"constraints":          []string{},
			"source": map[string]any{
				"captured_from": "revision_notes",
				"captured_by":   "tester",
			},
		},
	}
	reviseRes := srv.HandleRevisePlanAttempt(mcpAttemptJSON(t, reviseArgs))
	if reviseRes.IsError {
		t.Fatalf("revise failed: %s", reviseRes.Content[0].Text)
	}

	// Verify old attempt status is superseded
	var oldAttempt store.PlanAttempt
	if err := st.DB().QueryRow("SELECT status, replacement_plan_attempt_id FROM plan_attempts WHERE plan_attempt_id = 'attempt-orig'").Scan(&oldAttempt.Status, &oldAttempt.ReplacementPlanAttemptID); err != nil {
		t.Fatalf("load old attempt: %v", err)
	}
	if oldAttempt.Status != "superseded" || oldAttempt.ReplacementPlanAttemptID.String != "attempt-revised" {
		t.Fatalf("old attempt not superseded correctly: %+v", oldAttempt)
	}

	// 5. Void plan attempt
	voidArgs := map[string]any{
		"project_id":      "relay",
		"plan_attempt_id": "attempt-revised",
	}
	voidRes := srv.HandleVoidPlanAttempt(mcpAttemptJSON(t, voidArgs))
	if voidRes.IsError {
		t.Fatalf("void failed: %s", voidRes.Content[0].Text)
	}

	// Verify revised attempt is voided
	var revisedAttempt store.PlanAttempt
	if err := st.DB().QueryRow("SELECT status, replacement_plan_attempt_id FROM plan_attempts WHERE plan_attempt_id = 'attempt-revised'").Scan(&revisedAttempt.Status, &revisedAttempt.ReplacementPlanAttemptID); err != nil {
		t.Fatalf("load revised attempt: %v", err)
	}
	if revisedAttempt.Status != "voided" || revisedAttempt.ReplacementPlanAttemptID.Valid {
		t.Fatalf("revised attempt not voided correctly: %+v", revisedAttempt)
	}
	if got := mcpAttemptCountRows(t, st, "plans"); got != 0 {
		t.Fatalf("expected 0 plans after voiding, got %d", got)
	}
}
