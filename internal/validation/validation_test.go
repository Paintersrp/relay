package validation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	// Create temporary schema file for tests
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "test_schema.json")

	// Read existing schema
	schemaBytes, err := os.ReadFile(locateSchemaFile("handoffs/schema/canonical_packet.schema.json"))
	if err != nil {
		t.Fatalf("failed to read real schema for test: %v", err)
	}
	if err := os.WriteFile(schemaPath, schemaBytes, 0644); err != nil {
		t.Fatalf("failed to write test schema: %v", err)
	}

	validPacketBytes, err := os.ReadFile(locateSchemaFile("handoffs/examples/canonical_packet.valid.example.json"))
	if err != nil {
		t.Fatalf("failed to read valid packet example: %v", err)
	}
	var validPacket map[string]interface{}
	if err := json.Unmarshal(validPacketBytes, &validPacket); err != nil {
		t.Fatalf("failed to parse valid packet example: %v", err)
	}

	t.Run("Valid Packet", func(t *testing.T) {
		packetJSON, _ := json.Marshal(validPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !report.Valid {
			t.Errorf("expected valid report, got invalid. Errors: %v", report.Errors)
		}
	})

	t.Run("Invalid JSON syntax", func(t *testing.T) {
		report, err := ValidatePacketJSON([]byte(`{"packet_meta": {`), schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report")
		}
		if len(report.Errors) == 0 || report.Errors[0].Type != "structural" {
			t.Errorf("expected structural error, got %v", report.Errors)
		}
		if !report.RepairEligible {
			t.Error("expected syntax error to be repair eligible")
		}
	})

	t.Run("Schema Validation Failure", func(t *testing.T) {
		// Remove required section
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		delete(invalidPacket, "audit_seed")

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report")
		}
		if len(report.Errors) == 0 || report.Errors[0].Type != "schema" {
			t.Errorf("expected schema error, got %v", report.Errors)
		}
		if !report.RepairEligible {
			t.Error("expected schema error to be repair eligible")
		}
	})

	t.Run("Secret Detected", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		invalidPacket["planner_context"] = map[string]interface{}{
			"context_snapshot": "This is a Bearer token: Bearer abcdef123456",
		}

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report")
		}
		if report.RepairEligible {
			t.Error("secrets should make report non-repair-eligible")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "security" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected security error, got %v", report.Errors)
		}
	})

	t.Run("Unsafe Path Traversal", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		exec := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			exec[k] = v
		}
		exec["file_targets"] = []string{"../outside.go"}
		invalidPacket["execution_payload"] = exec

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report")
		}
		if report.RepairEligible {
			t.Error("unsafe paths should make report non-repair-eligible")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "path" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected path error, got %v", report.Errors)
		}
	})

	t.Run("Missing Required execution_payload Field", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		exec := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			exec[k] = v
		}
		exec["goal"] = ""
		invalidPacket["execution_payload"] = exec

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report")
		}
		if report.RepairEligible {
			t.Error("missing required payload fields should make report non-repair-eligible")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "input" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected input error, got %v", report.Errors)
		}
	})

	t.Run("Vague Phrasing Detected", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		exec := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			exec[k] = v
		}
		exec["goal"] = "Please improve the UI and decide best approach to wire as needed."
		invalidPacket["execution_payload"] = exec

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report due to vague phrasing")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "input" && strings.Contains(e.Message, "vague or decision-delegating phrase") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected vague phrase input error, got %v", report.Errors)
		}
	})

	t.Run("User Facing Workflow No Frontend File Fail", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		exec := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			exec[k] = v
		}
		exec["goal"] = "Fix user-facing UI route workflow behavior."
		// Targets are backend only
		exec["file_targets"] = []interface{}{
			map[string]interface{}{
				"path": "internal/validation/validation.go",
				"role": "primary",
				"action": "must_edit",
				"reason": "test",
			},
		}
		invalidPacket["execution_payload"] = exec

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report since user-facing task has no frontend file targets")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "input" && strings.Contains(e.Message, "no frontend file targets specified") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected frontend target error, got %v", report.Errors)
		}
	})

	t.Run("User Facing Workflow No Frontend File Explicit Backend Only Pass", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		exec := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			exec[k] = v
		}
		exec["goal"] = "Fix user-facing UI route workflow behavior (backend-only suffices)."
		exec["file_targets"] = []interface{}{
			map[string]interface{}{
				"path": "internal/validation/validation.go",
				"role": "primary",
				"action": "must_edit",
				"reason": "test",
			},
		}
		invalidPacket["execution_payload"] = exec

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !report.Valid {
			t.Errorf("expected passing report since backend-only is explicitly allowed, got errors: %v", report.Errors)
		}
	})

	t.Run("Inspect Step Decision Words Fail", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		exec := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			exec[k] = v
		}
		exec["implementation_steps"] = []interface{}{
			map[string]interface{}{
				"id": "S1",
				"title": "Decide what to do.",
				"action": "inspect",
				"target_paths": []interface{}{"internal/validation/validation.go"},
				"instructions": "Determine whether we should change logic.",
				"acceptance_criteria": []interface{}{"done"},
			},
		}
		invalidPacket["execution_payload"] = exec

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report due to decision words in inspect step")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "input" && strings.Contains(e.Message, "contain decision words") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected decision words error, got %v", report.Errors)
		}
	})

	t.Run("Blocking Unresolved Question Fails", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		plannerCtx := make(map[string]interface{})
		if originalCtx, ok := validPacket["planner_context"].(map[string]interface{}); ok {
			for k, v := range originalCtx {
				plannerCtx[k] = v
			}
		}
		plannerCtx["unresolved_questions"] = []interface{}{
			map[string]interface{}{
				"id": "Q1",
				"question": "Is this a test?",
				"blocking": true,
			},
		}
		invalidPacket["planner_context"] = plannerCtx

		packetJSON, _ := json.Marshal(invalidPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Error("expected invalid report due to blocking unresolved question")
		}
		if report.RepairEligible {
			t.Error("blocking unresolved question should make report non-repair-eligible")
		}
		found := false
		for _, e := range report.Errors {
			if e.Type == "input" && e.Code == CodeBlockingUnresolvedQuestion {
				found = true
			}
		}
		if !found {
			t.Errorf("expected blocking unresolved question error, got %v", report.Errors)
		}
	})

	t.Run("Non-Blocking Unresolved Question Passes", func(t *testing.T) {
		testPacket := make(map[string]interface{})
		for k, v := range validPacket {
			testPacket[k] = v
		}
		plannerCtx := make(map[string]interface{})
		if originalCtx, ok := validPacket["planner_context"].(map[string]interface{}); ok {
			for k, v := range originalCtx {
				plannerCtx[k] = v
			}
		}
		plannerCtx["unresolved_questions"] = []interface{}{
			map[string]interface{}{
				"id": "Q2",
				"question": "Just a thought?",
				"blocking": false,
			},
		}
		testPacket["planner_context"] = plannerCtx

		packetJSON, _ := json.Marshal(testPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !report.Valid {
			t.Errorf("expected valid report for non-blocking unresolved question, got errors: %v", report.Errors)
		}
	})

	t.Run("Empty Unresolved Questions Passes", func(t *testing.T) {
		testPacket := make(map[string]interface{})
		for k, v := range validPacket {
			testPacket[k] = v
		}
		plannerCtx := make(map[string]interface{})
		if originalCtx, ok := validPacket["planner_context"].(map[string]interface{}); ok {
			for k, v := range originalCtx {
				plannerCtx[k] = v
			}
		}
		plannerCtx["unresolved_questions"] = []interface{}{}
		testPacket["planner_context"] = plannerCtx

		packetJSON, _ := json.Marshal(testPacket)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !report.Valid {
			t.Errorf("expected valid report for empty unresolved questions, got errors: %v", report.Errors)
		}
	})
}

func TestSchemaAuditCriteria(t *testing.T) {
	// Paths
	canonicalSchemaPath := locateSchemaFile("handoffs/schema/canonical_packet.schema.json")
	plannerSchemaPath := locateSchemaFile("handoffs/schema/planner_handoff.schema.json")
	reportSchemaPath := locateSchemaFile("handoffs/schema/validation_report.schema.json")
	taxonomyPath := locateSchemaFile("handoffs/schema/middleware_failure_codes.json")

	// 1-3. Planner Handoff Tests
	t.Run("Planner handoff containing packet_maker_brief fails", func(t *testing.T) {
		validHandoff := []byte(`{
			"handoff_meta": {
				"handoff_id": "planner-handoff-2026-06-18-test",
				"schema_version": "1.0.0",
				"created_at": "2026-06-18T00:00:00Z",
				"planner_agent": "planner",
				"intended_handoff_path": "handoffs/planner/test.md",
				"task_slug": "test",
				"content_profile": "summary_only",
				"target_packet_path": "handoffs/packets/test.json",
				"target_executor": "deepseek-v4-flash",
				"canonical_packet_schema_path": "handoffs/schema/canonical_packet.schema.json"
			},
			"required_sections": {
				"context_snapshot": {"required": true, "tag_name": "context", "purpose": "test"},
				"decision_log": {"required": true, "tag_name": "log", "purpose": "test"},
				"constraints": {"required": true, "tag_name": "con", "purpose": "test"},
				"assumptions": {"required": true, "tag_name": "assm", "purpose": "test"},
				"pass_boundary": {"required": true, "tag_name": "pass", "purpose": "test"},
				"compiler_input": {"required": true, "tag_name": "comp", "purpose": "test"},
				"validation_expectations": {"required": true, "tag_name": "val", "purpose": "test"},
				"audit_priorities": {"required": true, "tag_name": "aud", "purpose": "test"},
				"packet_maker_brief": {"required": true, "tag_name": "pmb", "purpose": "test"}
			}
		}`)
		valid, _ := ValidatePlannerHandoffJSON(validHandoff, plannerSchemaPath)
		if valid {
			t.Error("expected Planner handoff containing packet_maker_brief to fail")
		}
	})

	t.Run("Planner handoff containing compiler_directives fails", func(t *testing.T) {
		validHandoff := []byte(`{
			"handoff_meta": {
				"handoff_id": "planner-handoff-2026-06-18-test",
				"schema_version": "1.0.0",
				"created_at": "2026-06-18T00:00:00Z",
				"planner_agent": "planner",
				"intended_handoff_path": "handoffs/planner/test.md",
				"task_slug": "test",
				"content_profile": "summary_only",
				"target_packet_path": "handoffs/packets/test.json",
				"target_executor": "deepseek-v4-flash",
				"canonical_packet_schema_path": "handoffs/schema/canonical_packet.schema.json"
			},
			"required_sections": {
				"context_snapshot": {"required": true, "tag_name": "context", "purpose": "test"},
				"decision_log": {"required": true, "tag_name": "log", "purpose": "test"},
				"constraints": {"required": true, "tag_name": "con", "purpose": "test"},
				"assumptions": {"required": true, "tag_name": "assm", "purpose": "test"},
				"pass_boundary": {"required": true, "tag_name": "pass", "purpose": "test"},
				"compiler_input": {"required": true, "tag_name": "comp", "purpose": "test"},
				"validation_expectations": {"required": true, "tag_name": "val", "purpose": "test"},
				"audit_priorities": {"required": true, "tag_name": "aud", "purpose": "test"},
				"compiler_directives": {"required": true, "tag_name": "cd", "purpose": "test"}
			}
		}`)
		valid, _ := ValidatePlannerHandoffJSON(validHandoff, plannerSchemaPath)
		if valid {
			t.Error("expected Planner handoff containing compiler_directives to fail")
		}
	})

	t.Run("Planner handoff missing compiler_input fails", func(t *testing.T) {
		validHandoff := []byte(`{
			"handoff_meta": {
				"handoff_id": "planner-handoff-2026-06-18-test",
				"schema_version": "1.0.0",
				"created_at": "2026-06-18T00:00:00Z",
				"planner_agent": "planner",
				"intended_handoff_path": "handoffs/planner/test.md",
				"task_slug": "test",
				"content_profile": "summary_only",
				"target_packet_path": "handoffs/packets/test.json",
				"target_executor": "deepseek-v4-flash",
				"canonical_packet_schema_path": "handoffs/schema/canonical_packet.schema.json"
			},
			"required_sections": {
				"context_snapshot": {"required": true, "tag_name": "context", "purpose": "test"},
				"decision_log": {"required": true, "tag_name": "log", "purpose": "test"},
				"constraints": {"required": true, "tag_name": "con", "purpose": "test"},
				"assumptions": {"required": true, "tag_name": "assm", "purpose": "test"},
				"pass_boundary": {"required": true, "tag_name": "pass", "purpose": "test"},
				"validation_expectations": {"required": true, "tag_name": "val", "purpose": "test"},
				"audit_priorities": {"required": true, "tag_name": "aud", "purpose": "test"}
			}
		}`)
		valid, _ := ValidatePlannerHandoffJSON(validHandoff, plannerSchemaPath)
		if valid {
			t.Error("expected Planner handoff missing compiler_input to fail")
		}
	})

	// 4-6. Validation Report Tests
	t.Run("Validation report issue without code fails", func(t *testing.T) {
		report := []byte(`{
			"valid": false,
			"repair_eligible": false,
			"errors": [
				{
					"type": "input",
					"message": "Missing code"
				}
			]
		}`)
		valid, _ := ValidateReportJSON(report, reportSchemaPath, taxonomyPath)
		if valid {
			t.Error("expected validation report issue without code to fail")
		}
	})

	t.Run("Validation report issue with unknown code fails", func(t *testing.T) {
		report := []byte(`{
			"valid": false,
			"repair_eligible": false,
			"errors": [
				{
					"type": "input",
					"code": "SOME_MADE_UP_CODE",
					"message": "Bad code"
				}
			]
		}`)
		valid, _ := ValidateReportJSON(report, reportSchemaPath, taxonomyPath)
		if valid {
			t.Error("expected validation report issue with unknown code to fail")
		}
	})

	t.Run("Report with repair_allowed true and uncoded issue fails", func(t *testing.T) {
		report := []byte(`{
			"valid": false,
			"repair_eligible": true,
			"errors": [
				{
					"type": "input",
					"message": "Uncoded issue",
					"repair_eligible": true
				}
			]
		}`)
		valid, _ := ValidateReportJSON(report, reportSchemaPath, taxonomyPath)
		if valid {
			t.Error("expected report with repair_allowed true and uncoded issue to fail")
		}
	})

	// 7-8. Canonical Packet Tests
	validPacketBytes, _ := os.ReadFile(locateSchemaFile("handoffs/examples/canonical_packet.valid.example.json"))
	var validPacket map[string]interface{}
	json.Unmarshal(validPacketBytes, &validPacket)

	t.Run("Canonical packet containing packet_maker_model fails", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		invalidPacket["model_routing"] = map[string]interface{}{
			"planner_model": "gpt-4o",
			"packet_maker_model": "gpt-4o",
		}

		packetJSON, _ := json.Marshal(invalidPacket)
		report, _ := ValidatePacketJSON(packetJSON, canonicalSchemaPath)
		if report.Valid {
			t.Error("expected canonical packet containing packet_maker_model to fail")
		}
	})

	t.Run("Canonical packet with producer packet-maker fails", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		meta := make(map[string]interface{})
		for k, v := range validPacket["packet_meta"].(map[string]interface{}) {
			meta[k] = v
		}
		meta["producer_kind"] = "packet-maker"
		invalidPacket["packet_meta"] = meta

		packetJSON, _ := json.Marshal(invalidPacket)
		report, _ := ValidatePacketJSON(packetJSON, canonicalSchemaPath)
		if report.Valid {
			t.Error("expected canonical packet with producer packet-maker to fail")
		}
	})
}
