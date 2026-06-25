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

func TestPlanAttemptMCPCreateAndSubmitBeforeApproval(t *testing.T) {
	st := setupTestStore(t)
	srv := NewServer(discardLogger(), &MCPDeps{Store: st, ToolProfile: ToolProfileRestricted})
	rawPlan, planHash := mcpAttemptRawPlan(t, "plan-attempt-mcp")

	createArgs := appplans.CreatePlanAttemptWithIntentRequest{
		ProjectID:     "relay",
		PlanAttemptID: "attempt-mcp-1",
		PlanArtifactRef: appplans.PlanArtifactRef{
			Path:         "handoffs/plans/attempt-mcp.json",
			SHA256:       planHash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON:     rawPlan,
		DriftReviewMode: appplans.DriftReviewModeManual,
		IntentPacket: appplans.IntentPacketInput{
			Summary:            "Expose MCP attempt tools.",
			LiteralUserRequest: "Start PASS-003.",
			Constraints:        []string{"Do not create runs."},
			Source: appplans.IntentSource{
				CapturedFrom:       appplans.CapturedFromPlannerChat,
				CapturedBy:         "mcp-test",
				SourceArtifactPath: "handoffs/planner/pass-003.md",
			},
		},
	}
	createResult := srv.HandleCreatePlanAttemptWithIntent(mcpAttemptJSON(t, createArgs))
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

	submitResult := srv.HandleSubmitPlanAttempt(mcpAttemptJSON(t, appplans.SubmitPlanAttemptRequest{
		ProjectID:     "relay",
		PlanAttemptID: "attempt-mcp-1",
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
		},
		"source_intent": map[string]any{"summary": "Exercise MCP attempt flow."},
		"passes": []any{
			map[string]any{
				"pass_id":                  "PASS-001",
				"sequence":                 1,
				"name":                     "MCP transport",
				"goal":                     "Add MCP transport tests.",
				"intended_execution_scope": []string{"MCP"},
				"non_goals":                []string{"No UI"},
				"dependencies":             []string{},
				"status":                   "planned",
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
