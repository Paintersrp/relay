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
	schemaBytes, err := os.ReadFile(locateSchemaFile("relay-contracts/schema/canonical_packet.schema.json"))
	if err != nil {
		t.Fatalf("failed to read real schema for test: %v", err)
	}
	if err := os.WriteFile(schemaPath, schemaBytes, 0644); err != nil {
		t.Fatalf("failed to write test schema: %v", err)
	}

	validPacketBytes, err := os.ReadFile(locateSchemaFile("relay-contracts/examples/canonical_packet.valid.example.json"))
	if err != nil {
		t.Fatalf("failed to read valid packet example: %v", err)
	}
	var validPacket map[string]interface{}
	if err := json.Unmarshal(validPacketBytes, &validPacket); err != nil {
		t.Fatalf("failed to parse valid packet example: %v", err)
	}
	cloneValidPacket := func() map[string]interface{} {
		packetBytes, err := json.Marshal(validPacket)
		if err != nil {
			t.Fatalf("failed to clone valid packet: %v", err)
		}
		var cloned map[string]interface{}
		if err := json.Unmarshal(packetBytes, &cloned); err != nil {
			t.Fatalf("failed to parse cloned packet: %v", err)
		}
		return cloned
	}
	hasErrorCode := func(report *ValidationReport, code string) bool {
		for _, e := range report.Errors {
			if e.Code == code {
				return true
			}
		}
		return false
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

	t.Run("Vague Intent Grounding", func(t *testing.T) {
		tests := []struct {
			name      string
			mutate    func(map[string]interface{})
			wantValid bool
		}{
			{
				name: "grounded goal containing improve passes",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["goal"] = "Improve CANONICAL_PACKET_VAGUE_INTENT validation by allowing grounded execution_payload.goal text."
					exec["scope"] = "Replace literal phrase blocking in internal/validation/validation.go while preserving validation report status behavior."
					exec["file_targets"] = []interface{}{map[string]interface{}{"path": "internal/validation/validation.go", "role": "primary", "action": "must_edit", "reason": "validator logic"}}
					exec["code_requirements"] = []interface{}{map[string]interface{}{"id": "CR1", "requirement": "Return CANONICAL_PACKET_VAGUE_INTENT only when grounding signals are missing.", "applies_to": []interface{}{"internal/validation/validation.go"}}}
					exec["validation_contract"] = map[string]interface{}{"mode": "commands", "failure_policy": "block", "commands": []interface{}{map[string]interface{}{"id": "V1", "command": "go test ./internal/validation", "required": true, "purpose": "Verify grounded vague-intent validation.", "success_signal": "Command exits 0.", "failure_handling": "block_if_fails"}}}
				},
				wantValid: true,
			},
			{
				name: "ungrounded goal containing improve fails",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["goal"] = "Improve the UI."
					exec["validation_contract"] = map[string]interface{}{"mode": "commands"}
				},
				wantValid: false,
			},
			{
				name: "grounded implementation step containing improve passes",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["implementation_steps"] = []interface{}{map[string]interface{}{
						"id":                  "S1",
						"title":               "Update validator",
						"action":              "modify",
						"target_paths":        []interface{}{"internal/validation/validation.go"},
						"instructions":        "Improve CANONICAL_PACKET_VAGUE_INTENT handling by replacing recursive payload scanning with field-aware checks.",
						"acceptance_criteria": []interface{}{"Validation report does not contain CANONICAL_PACKET_VAGUE_INTENT for grounded improve wording."},
					}}
				},
				wantValid: true,
			},
			{
				name: "decision delegating implementation step fails",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["implementation_steps"] = []interface{}{map[string]interface{}{
						"id":                  "S1",
						"title":               "Pick behavior",
						"action":              "modify",
						"target_paths":        []interface{}{"internal/validation/validation.go"},
						"instructions":        "Decide best approach.",
						"acceptance_criteria": []interface{}{"done"},
					}}
				},
				wantValid: false,
			},
			{
				name: "non goals with decision language pass",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["non_goals"] = []interface{}{"Do not decide best approach for unrelated compiler behavior."}
					exec["goal"] = "Validate CANONICAL_PACKET_VAGUE_INTENT handling for concrete execution fields."
				},
				wantValid: true,
			},
			{
				name: "grounded implementation step containing wire as needed fails",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["implementation_steps"] = []interface{}{map[string]interface{}{
						"id":                  "S1",
						"title":               "Wire component",
						"action":              "modify",
						"target_paths":        []interface{}{"internal/validation/validation.go"},
						"instructions":        joinPhrase("wire", "as", "needed"),
						"acceptance_criteria": []interface{}{"go test ./internal/validation"},
					}}
				},
				wantValid: false,
			},
			{
				name: "grounded goal containing determine whether fails",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["goal"] = joinPhrase("determine", "whether") + " the validator works."
					exec["scope"] = "Update internal/validation/validation.go and run go test ./internal/validation"
					exec["file_targets"] = []interface{}{map[string]interface{}{"path": "internal/validation/validation.go", "role": "primary", "action": "must_edit", "reason": "validator logic"}}
					exec["validation_contract"] = map[string]interface{}{"mode": "commands", "failure_policy": "block", "commands": []interface{}{map[string]interface{}{"id": "V1", "command": "go test ./internal/validation", "required": true, "purpose": "Verify validation.", "success_signal": "Command exits 0.", "failure_handling": "block_if_fails"}}}
				},
				wantValid: false,
			},
			{
				name: "grounded code requirement containing decide best approach fails",
				mutate: func(packet map[string]interface{}) {
					exec := packet["execution_payload"].(map[string]interface{})
					exec["code_requirements"] = []interface{}{map[string]interface{}{
						"id":          "CR1",
						"requirement": "The developer should " + joinPhrase("decide", "best", "approach") + " for code changes.",
						"applies_to":  []interface{}{"internal/validation/validation.go"},
					}}
				},
				wantValid: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				packet := cloneValidPacket()
				tt.mutate(packet)
				packetJSON, _ := json.Marshal(packet)
				report, err := ValidatePacketJSON(packetJSON, schemaPath)
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if tt.wantValid && !report.Valid {
					t.Fatalf("expected valid report, got errors: %v", report.Errors)
				}
				if !tt.wantValid {
					if report.Valid {
						t.Fatalf("expected invalid report")
					}
					if !hasErrorCode(report, "CANONICAL_PACKET_VAGUE_INTENT") {
						t.Fatalf("expected CANONICAL_PACKET_VAGUE_INTENT, got %v", report.Errors)
					}
				}
			})
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
				"path":   "internal/validation/validation.go",
				"role":   "primary",
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
				"path":   "internal/validation/validation.go",
				"role":   "primary",
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

	t.Run("Policy Docs Prohibited Examples Do Not Require Frontend Target", func(t *testing.T) {
		packet := cloneValidPacket()
		exec := packet["execution_payload"].(map[string]interface{})
		prohibitedSample := joinPhrase("wire", "as", "needed")
		exec["goal"] = "Update artifact naming policy examples for invalid executor delegation wording."
		exec["scope"] = "Patch relay-contracts/policies/artifact_naming_policy.md only; no runtime behavior changes are in scope."
		exec["file_targets"] = []interface{}{
			map[string]interface{}{
				"path":   "relay-contracts/policies/artifact_naming_policy.md",
				"role":   "primary",
				"action": "must_edit",
				"reason": "policy text",
			},
		}
		exec["implementation_steps"] = []interface{}{map[string]interface{}{
			"id":                  "S1",
			"title":               "Document prohibited examples",
			"action":              "modify",
			"target_paths":        []interface{}{"relay-contracts/policies/artifact_naming_policy.md"},
			"instructions":        "Add policy text under a prohibited examples heading.",
			"acceptance_criteria": []interface{}{"Policy text lists invalid sample language without changing runtime behavior."},
		}}
		exec["code_requirements"] = []interface{}{map[string]interface{}{
			"id": "CR1",
			"requirement": strings.Join([]string{
				"## Prohibited examples:",
				"- The invalid sample `" + prohibitedSample + "` documents language that policy authors must not use.",
			}, "\n"),
			"applies_to": []interface{}{"relay-contracts/policies/artifact_naming_policy.md"},
		}}
		exec["expected_behavior"] = []interface{}{
			"Policy docs describe prohibited executor-delegating examples as invalid samples.",
			"No product runtime changes are required.",
		}

		packetJSON, _ := json.Marshal(packet)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if hasErrorCode(report, "CANONICAL_PACKET_VAGUE_INTENT") {
			t.Fatalf("expected no vague intent issue, got %v", report.Errors)
		}
		if hasErrorCode(report, CodeFileTargetMismatch) {
			t.Fatalf("expected no frontend target mismatch, got %v", report.Errors)
		}
		if !report.Valid {
			t.Fatalf("expected valid report, got errors: %v", report.Errors)
		}
	})

	t.Run("Direct Delegation Same Phrase Still Fails", func(t *testing.T) {
		packet := cloneValidPacket()
		exec := packet["execution_payload"].(map[string]interface{})
		exec["implementation_steps"] = []interface{}{map[string]interface{}{
			"id":                  "S1",
			"title":               "Delegate implementation",
			"action":              "modify",
			"target_paths":        []interface{}{"internal/validation/validation.go"},
			"instructions":        "Update internal/validation/validation.go and " + joinPhrase("wire", "as", "needed") + ".",
			"acceptance_criteria": []interface{}{"go test ./internal/validation passes."},
		}}

		packetJSON, _ := json.Marshal(packet)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Fatalf("expected invalid report")
		}
		if !hasErrorCode(report, "CANONICAL_PACKET_VAGUE_INTENT") {
			t.Fatalf("expected CANONICAL_PACKET_VAGUE_INTENT, got %v", report.Errors)
		}
	})

	t.Run("Visible UI Workflow Without Frontend Target Fails", func(t *testing.T) {
		packet := cloneValidPacket()
		exec := packet["execution_payload"].(map[string]interface{})
		exec["goal"] = "Add a visible app UI page for reviewing canonical packet validation results."
		exec["scope"] = "Implement the user-facing review page workflow."
		exec["file_targets"] = []interface{}{
			map[string]interface{}{
				"path":   "internal/validation/validation.go",
				"role":   "primary",
				"action": "must_edit",
				"reason": "server target should be insufficient for a UI page task",
			},
		}
		exec["expected_behavior"] = []interface{}{"The app displays validation results in a visible UI page."}

		packetJSON, _ := json.Marshal(packet)
		report, err := ValidatePacketJSON(packetJSON, schemaPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if report.Valid {
			t.Fatalf("expected invalid report")
		}
		if !hasErrorCode(report, CodeFileTargetMismatch) {
			t.Fatalf("expected %s, got %v", CodeFileTargetMismatch, report.Errors)
		}
	})

	t.Run("Inspect Step Grounding", func(t *testing.T) {
		tests := []struct {
			name      string
			step      map[string]interface{}
			wantValid bool
		}{
			{
				name: "concrete inspect verification passes",
				step: map[string]interface{}{
					"id":                  "S1",
					"title":               "Verify validator path",
					"action":              "inspect",
					"target_paths":        []interface{}{"internal/validation/validation.go"},
					"instructions":        "Verify CANONICAL_PACKET_VAGUE_INTENT handling returns errors only for ungrounded execution fields.",
					"acceptance_criteria": []interface{}{"Inspection output contains target function names and validation status."},
				},
				wantValid: true,
			},
			{
				name: "decision delegating inspect fails",
				step: map[string]interface{}{
					"id":                  "S1",
					"title":               "Inspect and decide",
					"action":              "inspect",
					"target_paths":        []interface{}{"internal/validation/validation.go"},
					"instructions":        "Inspect and decide best approach.",
					"acceptance_criteria": []interface{}{"done"},
				},
				wantValid: false,
			},
			{
				name: "grounded inspect step with inspect and decide fails",
				step: map[string]interface{}{
					"id":                  "S1",
					"title":               joinPhrase("inspect", "and", "decide"),
					"action":              "inspect",
					"target_paths":        []interface{}{"internal/validation/validation.go"},
					"instructions":        "Check validator implementation details.",
					"acceptance_criteria": []interface{}{"All checks pass successfully."},
				},
				wantValid: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				packet := cloneValidPacket()
				exec := packet["execution_payload"].(map[string]interface{})
				exec["implementation_steps"] = []interface{}{tt.step}
				packetJSON, _ := json.Marshal(packet)
				report, err := ValidatePacketJSON(packetJSON, schemaPath)
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if tt.wantValid && !report.Valid {
					t.Fatalf("expected valid report, got errors: %v", report.Errors)
				}
				if !tt.wantValid {
					if report.Valid {
						t.Fatalf("expected invalid report")
					}
					if !hasErrorCode(report, "CANONICAL_PACKET_VAGUE_INTENT") {
						t.Fatalf("expected CANONICAL_PACKET_VAGUE_INTENT, got %v", report.Errors)
					}
				}
			})
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
				"id":       "Q1",
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
				"id":       "Q2",
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
	canonicalSchemaPath := locateSchemaFile("relay-contracts/schema/canonical_packet.schema.json")
	plannerSchemaPath := locateSchemaFile("relay-contracts/schema/planner_handoff_manifest.schema.json")
	reportSchemaPath := locateSchemaFile("relay-contracts/schema/validation_report.schema.json")
	taxonomyPath := locateSchemaFile("relay-contracts/schema/middleware_failure_codes.json")

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
				"content_profile": "implementation_ready",
				"target_packet_path": "handoffs/packets/test.json",
				"target_executor": "deepseek-v4-flash",
				"canonical_packet_schema_path": "schema/canonical_packet.schema.json"
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
				"content_profile": "implementation_ready",
				"target_packet_path": "handoffs/packets/test.json",
				"target_executor": "deepseek-v4-flash",
				"canonical_packet_schema_path": "schema/canonical_packet.schema.json"
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
				"content_profile": "implementation_ready",
				"target_packet_path": "handoffs/packets/test.json",
				"target_executor": "deepseek-v4-flash",
				"canonical_packet_schema_path": "schema/canonical_packet.schema.json"
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
	validPacketBytes, _ := os.ReadFile(locateSchemaFile("relay-contracts/examples/canonical_packet.valid.example.json"))
	var validPacket map[string]interface{}
	json.Unmarshal(validPacketBytes, &validPacket)

	t.Run("Canonical packet with producer packet-maker fails", func(t *testing.T) {
		invalidPacket := make(map[string]interface{})
		for k, v := range validPacket {
			invalidPacket[k] = v
		}
		meta := make(map[string]interface{})
		for k, v := range validPacket["packet_meta"].(map[string]interface{}) {
			meta[k] = v
		}
		producer := make(map[string]interface{})
		if original, ok := meta["producer"].(map[string]interface{}); ok {
			for k, v := range original {
				producer[k] = v
			}
		}
		producer["kind"] = "packet-maker"
		meta["producer"] = producer
		invalidPacket["packet_meta"] = meta

		packetJSON, _ := json.Marshal(invalidPacket)
		report, _ := ValidatePacketJSON(packetJSON, canonicalSchemaPath)
		if report.Valid {
			t.Error("expected canonical packet with producer packet-maker to fail")
		}
	})
}

func joinPhrase(parts ...string) string {
	return strings.Join(parts, " ")
}
