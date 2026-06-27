package mcp

import (
	"encoding/json"
	"testing"
)

func TestServerToolsListIncludesPlanSeedTools(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected tools/list error: %v", resp.Error)
	}

	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	expectedPlanSeedTools := []string{
		"create_plan_seed",
		"list_plan_seeds",
		"get_plan_seed",
		"update_plan_seed",
		"defer_plan_seed",
		"reject_plan_seed",
	}

	registeredNames := map[string]bool{}
	for _, tool := range list.Tools {
		registeredNames[tool.Name] = true
	}

	for _, name := range expectedPlanSeedTools {
		if !registeredNames[name] {
			t.Errorf("expected tool %q not found in tools/list", name)
		}
	}

	// Verify future-pass/forbidden tools are absent
	forbiddenTools := []string{
		"get_plan_seed_planning_context",
		"create_plan_attempt_from_seed",
		"link_plan_seed_result",
	}
	for _, name := range forbiddenTools {
		if registeredNames[name] {
			t.Errorf("forbidden tool %q was advertised in tools/list", name)
		}
	}
}

func TestHandleCreateListGetUpdateDeferRejectPlanSeed(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// 1. Create a Plan Seed
	createArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"title":         "Initial title",
		"quick_context": "Initial context",
		"priority":      "normal",
		"tags":          []string{"tag-1"},
		"constraints":   []string{"constraint-1"},
		"non_goals":     []string{"non-goal-1"},
		"source_label":  "mcp-manual",
	})
	res := srv.HandleCreatePlanSeed(createArgs)
	if res.IsError {
		t.Fatalf("create_plan_seed failed: %s", res.Content[0].Text)
	}

	var out planSeedToolOutput
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !out.OK || out.Seed == nil {
		t.Fatalf("expected ok create seed result, got %+v", out)
	}
	seedID := out.Seed.SeedID
	if out.Seed.Status != "captured" {
		t.Errorf("expected status captured, got %q", out.Seed.Status)
	}
	if out.Seed.SourceType != "mcp" {
		t.Errorf("expected source_type mcp, got %q", out.Seed.SourceType)
	}
	if out.Seed.SourceLabel != "mcp-manual" {
		t.Errorf("expected source_label mcp-manual, got %q", out.Seed.SourceLabel)
	}

	// 2. List Plan Seeds
	listArgs, _ := json.Marshal(map[string]any{
		"project_id": "relay",
		"status":     "captured",
	})
	listRes := srv.HandleListPlanSeeds(listArgs)
	if listRes.IsError {
		t.Fatalf("list_plan_seeds failed: %s", listRes.Content[0].Text)
	}
	var listOut planSeedToolOutput
	_ = json.Unmarshal([]byte(listRes.Content[0].Text), &listOut)
	if listOut.Count != 1 || len(listOut.Seeds) != 1 || listOut.Seeds[0].SeedID != seedID {
		t.Fatalf("expected list to return our seed, got %+v", listOut)
	}

	// 3. Get Plan Seed
	getArgs, _ := json.Marshal(map[string]any{
		"project_id": "relay",
		"seed_id":    seedID,
	})
	getRes := srv.HandleGetPlanSeed(getArgs)
	if getRes.IsError {
		t.Fatalf("get_plan_seed failed: %s", getRes.Content[0].Text)
	}
	var getOut planSeedToolOutput
	_ = json.Unmarshal([]byte(getRes.Content[0].Text), &getOut)
	if getOut.Seed == nil || getOut.Seed.SeedID != seedID {
		t.Fatalf("expected get_plan_seed to return our seed, got %+v", getOut)
	}

	// 4. Update Plan Seed
	updateArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"seed_id":       seedID,
		"title":         "Updated title",
		"quick_context": "Updated context",
		"priority":      "high",
		"tags":          []string{"tag-1", "tag-2"},
	})
	updateRes := srv.HandleUpdatePlanSeed(updateArgs)
	if updateRes.IsError {
		t.Fatalf("update_plan_seed failed: %s", updateRes.Content[0].Text)
	}
	var updateOut planSeedToolOutput
	_ = json.Unmarshal([]byte(updateRes.Content[0].Text), &updateOut)
	if updateOut.Seed == nil || updateOut.Seed.Title != "Updated title" || updateOut.Seed.Priority != "high" {
		t.Fatalf("expected updated title/priority, got %+v", updateOut)
	}
	// Verify tags length is updated
	if len(updateOut.Seed.Tags) != 2 {
		t.Errorf("expected 2 tags, got %+v", updateOut.Seed.Tags)
	}
	// Verify source_label remains intact
	if updateOut.Seed.SourceLabel != "mcp-manual" {
		t.Errorf("expected source_label to remain mcp-manual, got %q", updateOut.Seed.SourceLabel)
	}

	// 5. Defer Plan Seed
	deferArgs, _ := json.Marshal(map[string]any{
		"project_id":   "relay",
		"seed_id":      seedID,
		"defer_reason": "waiting",
	})
	deferRes := srv.HandleDeferPlanSeed(deferArgs)
	if deferRes.IsError {
		t.Fatalf("defer_plan_seed failed: %s", deferRes.Content[0].Text)
	}
	var deferOut planSeedToolOutput
	_ = json.Unmarshal([]byte(deferRes.Content[0].Text), &deferOut)
	if deferOut.Seed == nil || deferOut.Seed.Status != "deferred" || deferOut.Seed.DeferReason != "waiting" {
		t.Fatalf("expected deferred status and reason, got %+v", deferOut)
	}

	// 6. Reject Plan Seed
	rejectArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"seed_id":       seedID,
		"reject_reason": "invalid",
	})
	rejectRes := srv.HandleRejectPlanSeed(rejectArgs)
	if rejectRes.IsError {
		t.Fatalf("reject_plan_seed failed: %s", rejectRes.Content[0].Text)
	}
	var rejectOut planSeedToolOutput
	_ = json.Unmarshal([]byte(rejectRes.Content[0].Text), &rejectOut)
	if rejectOut.Seed == nil || rejectOut.Seed.Status != "rejected" || rejectOut.Seed.RejectReason != "invalid" {
		t.Fatalf("expected rejected status and reason, got %+v", rejectOut)
	}
}

func TestPlanSeedMCPRejectsUnknownProjectAndSecretLikeInput(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// Unknown project
	args, _ := json.Marshal(map[string]any{
		"project_id":    "missing",
		"title":         "Valid Title",
		"quick_context": "Valid context",
	})
	res := srv.HandleCreatePlanSeed(args)
	if !res.IsError {
		t.Fatal("expected error for missing project")
	}
	var errOut planSeedToolErrorOutput
	_ = json.Unmarshal([]byte(res.Content[0].Text), &errOut)
	if errOut.Error != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND error, got %s", errOut.Error)
	}

	// Secret-like value in quick_context
	secretArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"title":         "Valid Title",
		"quick_context": "sk-1234567890abcdef",
	})
	res2 := srv.HandleCreatePlanSeed(secretArgs)
	if !res2.IsError {
		t.Fatal("expected error for secret input")
	}
	var errOut2 planSeedToolErrorOutput
	_ = json.Unmarshal([]byte(res2.Content[0].Text), &errOut2)
	if errOut2.Error != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", errOut2.Error)
	}
}

func TestPlanSeedMCPUpdateDoesNotAcceptStatusOrLinkageFields(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// Create a seed
	createArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"title":         "Title",
		"quick_context": "Context",
	})
	res := srv.HandleCreatePlanSeed(createArgs)
	if res.IsError {
		t.Fatalf("create failed: %s", res.Content[0].Text)
	}
	var out planSeedToolOutput
	_ = json.Unmarshal([]byte(res.Content[0].Text), &out)
	seedID := out.Seed.SeedID

	// Try updating seed with status or linkage fields (which are not in the update schema/arguments struct).
	// Because we use brokerDecodeStrict (which disallows unknown fields), this must fail with a json decoding/validation error.
	badUpdateArgs, _ := json.Marshal(map[string]any{
		"project_id":      "relay",
		"seed_id":         seedID,
		"title":           "New Title",
		"status":          "rejected",
		"plan_attempt_id": "attempt-123",
	})

	updateRes := srv.HandleUpdatePlanSeed(badUpdateArgs)
	if !updateRes.IsError {
		t.Fatal("expected update to fail due to forbidden fields")
	}

	var errOut planSeedToolErrorOutput
	_ = json.Unmarshal([]byte(updateRes.Content[0].Text), &errOut)
	if errOut.Error != "VALIDATION_ERROR" || !contains(errOut.Message, "json: unknown field") {
		t.Errorf("expected validation error mentioning unknown field, got: %+v", errOut)
	}
}

func TestPlanSeedMCPNoPlanOrRunSideEffects(t *testing.T) {
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)

	// Tables to check counts around Plan Seed operations
	tables := []string{"intent_packets", "plan_attempts", "plans", "plan_passes", "runs"}

	getCounts := func() map[string]int {
		counts := make(map[string]int)
		for _, tbl := range tables {
			counts[tbl] = countTableRows(t, deps.Store.DB(), tbl)
		}
		return counts
	}

	assertCountsEqual := func(before, after map[string]int, msg string) {
		for _, tbl := range tables {
			if before[tbl] != after[tbl] {
				t.Errorf("%s: table %s row count changed from %d to %d", msg, tbl, before[tbl], after[tbl])
			}
		}
	}

	// Capture initial counts
	initialCounts := getCounts()

	// 1. Create seed
	createArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"title":         "Title",
		"quick_context": "Context",
	})
	res := srv.HandleCreatePlanSeed(createArgs)
	if res.IsError {
		t.Fatalf("create failed: %s", res.Content[0].Text)
	}
	var out planSeedToolOutput
	_ = json.Unmarshal([]byte(res.Content[0].Text), &out)
	seedID := out.Seed.SeedID

	afterCreate := getCounts()
	assertCountsEqual(initialCounts, afterCreate, "after create_plan_seed")

	// 2. Update seed
	updateArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"seed_id":       seedID,
		"title":         "New title",
		"quick_context": "New context",
	})
	updateRes := srv.HandleUpdatePlanSeed(updateArgs)
	if updateRes.IsError {
		t.Fatalf("update failed: %s", updateRes.Content[0].Text)
	}

	afterUpdate := getCounts()
	assertCountsEqual(initialCounts, afterUpdate, "after update_plan_seed")

	// 3. Defer seed
	deferArgs, _ := json.Marshal(map[string]any{
		"project_id":   "relay",
		"seed_id":      seedID,
		"defer_reason": "wait",
	})
	deferRes := srv.HandleDeferPlanSeed(deferArgs)
	if deferRes.IsError {
		t.Fatalf("defer failed: %s", deferRes.Content[0].Text)
	}

	afterDefer := getCounts()
	assertCountsEqual(initialCounts, afterDefer, "after defer_plan_seed")

	// 4. Reject seed
	rejectArgs, _ := json.Marshal(map[string]any{
		"project_id":    "relay",
		"seed_id":       seedID,
		"reject_reason": "reject",
	})
	rejectRes := srv.HandleRejectPlanSeed(rejectArgs)
	if rejectRes.IsError {
		t.Fatalf("reject failed: %s", rejectRes.Content[0].Text)
	}

	afterReject := getCounts()
	assertCountsEqual(initialCounts, afterReject, "after reject_plan_seed")
}
