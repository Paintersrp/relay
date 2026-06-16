package validation

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}
