package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/validation"
	"testing"
	"time"
)

func TestCompiler(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Setup base dir for artifacts in test
	artifacts.SetBaseDir(filepath.Join(dir, "artifacts"))

	s, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	repo, err := s.CreateRepo("test-repo", filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	c := New(s)

	t.Run("Unknown run error", func(t *testing.T) {
		_, err := c.CompileApprovedRun(context.Background(), 999999)
		if err == nil {
			t.Error("expected error for unknown run")
		}
	})

	t.Run("Wrong run status error", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Test Run Wrong Status", "draft", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
		_, err = c.CompileApprovedRun(context.Background(), run.ID)
		if err == nil {
			t.Error("expected error for wrong run status")
		}
	})

	t.Run("Missing planner_handoff.md error", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Test Run Missing Inputs", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Write run_config but not planner_handoff
		configMap := map[string]string{
			"repo_target":    repo.Path,
			"branch_context": "main",
		}
		configJSON, _ := json.Marshal(configMap)
		path, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", path, "application/json")

		_, err = c.CompileApprovedRun(context.Background(), run.ID)
		if err == nil {
			t.Error("expected error for missing planner_handoff.md")
		}
	})

	t.Run("Successful Compilation - Fully Schema Validated", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Fix Overflow Stale UI", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Write config
		configMap := map[string]interface{}{
			"repo_target":    repo.Path,
			"branch_context": "main",
			"file_targets":   []string{"src/ui/overflowPage.ts"},
		}
		configJSON, _ := json.Marshal(configMap)
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		// Write metadata
		metaMap := map[string]string{
			"handoff_id":      "planner-handoff-2026-06-15-fix-overflow-stale-ui",
			"target_executor": "deepseek-v4-flash",
		}
		metaJSON, _ := json.Marshal(metaMap)
		metaPath, _ := artifacts.Write(run.ID, "parsed_frontmatter", "parsed_frontmatter.json", metaJSON)
		_, _ = s.CreateArtifact(run.ID, "parsed_frontmatter", metaPath, "application/json")

		// Write planner_handoff.md
		exampleBytes, err := os.ReadFile("d:/Code/relay/internal/compiler/testdata/formal_planner_handoff.md")
		if err != nil {
			t.Fatalf("failed to read formal_planner_handoff fixture: %v", err)
		}

		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", exampleBytes)
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(res.Issues) > 0 {
			t.Fatalf("expected no compilation issues, got: %v", res.Issues)
		}

		if !res.Success {
			t.Fatalf("expected res.Success to be true, got validation errors: %+v", res.ValidationReport.Errors)
		}

		expectedPacketID := fmt.Sprintf("packet-%s-fix-overflow-stale-ui", time.Now().Format("2006-01-02"))
		if res.PacketID != expectedPacketID {
			t.Errorf("expected packet ID %q, got %q", expectedPacketID, res.PacketID)
		}

		// Check database status
		updatedRun, _ := s.GetRun(run.ID)
		if updatedRun.Status != "packet_validated" {
			t.Errorf("expected status 'packet_validated', got %q", updatedRun.Status)
		}

		// Read packet
		packetBytes, err := artifacts.Read(run.ID, "canonical_packet", "canonical_packet.json")
		if err != nil {
			t.Fatalf("failed to read packet: %v", err)
		}
		var packet map[string]interface{}
		if err := json.Unmarshal(packetBytes, &packet); err != nil {
			t.Fatalf("failed to parse packet: %v", err)
		}

		// Assert packet_meta fields populated
		meta, ok := packet["packet_meta"].(map[string]interface{})
		if !ok {
			t.Fatal("missing packet_meta")
		}
		for _, field := range []string{"protocol_version", "schema_version", "created_at", "created_by_agent", "source_planner_handoff_path", "intended_packet_path"} {
			if val, ok := meta[field].(string); !ok || val == "" {
				t.Errorf("missing or empty metadata field: %s", field)
			}
		}

		// Assert artifact paths check
		artPaths, ok := meta["artifact_paths"].(map[string]interface{})
		if !ok {
			t.Fatal("missing artifact_paths")
		}
		if val, ok := artPaths["planner_handoff"].(string); !ok || val == "" {
			t.Error("missing planner_handoff in artifact_paths")
		}

		// Assert expected_behavior is list of strings
		exec, ok := packet["execution_payload"].(map[string]interface{})
		if !ok {
			t.Fatal("missing execution_payload")
		}
		eb, ok := exec["expected_behavior"].([]interface{})
		if !ok || len(eb) == 0 {
			t.Errorf("expected_behavior must be non-empty list of strings, got %+v", exec["expected_behavior"])
		}

		// Assert completion_contract is object
		cc, ok := exec["completion_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("completion_contract is not an object")
		}
		for _, field := range []string{"done_when", "blocked_when", "allowed_discretion", "forbidden_discretion"} {
			if _, ok := cc[field].([]interface{}); !ok {
				t.Errorf("missing list in completion_contract: %s", field)
			}
		}

		// Assert validation commands have ID
		cmds, ok := exec["validation_commands"].([]interface{})
		if !ok || len(cmds) == 0 {
			t.Fatal("validation_commands is empty or not a list")
		}
		for i, cmdVal := range cmds {
			cmdMap, ok := cmdVal.(map[string]interface{})
			if !ok {
				t.Fatalf("validation command %d is not map", i)
			}
			id, _ := cmdMap["id"].(string)
			if id == "" {
				t.Errorf("validation command %d missing ID", i)
			}
			successSignal, _ := cmdMap["success_signal"].(string)
			if successSignal == "" {
				t.Errorf("validation command %d missing success_signal", i)
			}
		}

		// Assert audit checklist has objects
		audit, ok := packet["audit_seed"].(map[string]interface{})
		if !ok {
			t.Fatal("missing audit_seed")
		}
		checklist, ok := audit["audit_checklist"].([]interface{})
		if !ok || len(checklist) == 0 {
			t.Fatal("audit_checklist is empty or not a list")
		}
		for i, itemVal := range checklist {
			itemMap, ok := itemVal.(map[string]interface{})
			if !ok {
				t.Fatalf("audit check %d is not a map", i)
			}
			id, _ := itemMap["id"].(string)
			check, _ := itemMap["check"].(string)
			severity, _ := itemMap["severity_if_failed"].(string)
			if id == "" || check == "" || severity == "" {
				t.Errorf("audit check %d missing fields: id=%q check=%q severity=%q", i, id, check, severity)
			}
		}
	})

	t.Run("TestPassBoundaryCoercesIntegers", func(t *testing.T) {
		boundaryText := "```yaml\ncurrent_pass: \"2\"\ntotal_planned_passes: \"3\"\nthis_pass_scope: scope\nout_of_scope_for_this_pass:\n  - none\n```"
		boundary := parsePassBoundary(boundaryText)
		
		cp, ok := boundary["current_pass"].(int)
		if !ok || cp != 2 {
			t.Errorf("expected current_pass = 2 (int), got %v", boundary["current_pass"])
		}
		
		tpp, ok := boundary["total_planned_passes"].(int)
		if !ok || tpp != 3 {
			t.Errorf("expected total_planned_passes = 3 (int), got %v", boundary["total_planned_passes"])
		}
	})

	t.Run("Failed Validation - Unsafe Paths", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run with Unsafe Paths", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Config with unsafe targets
		configMap := map[string]interface{}{
			"repo_target":    repo.Path,
			"branch_context": "main",
			"file_targets":   []string{"../escape.go"},
		}
		configJSON, _ := json.Marshal(configMap)
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		// Write planner_handoff.md
		exampleBytes, _ := os.ReadFile("d:/Code/relay/internal/compiler/testdata/formal_planner_handoff.md")
		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", exampleBytes)
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if res.Success {
			t.Error("expected compilation failure due to unsafe path")
		}

		// Check status is packet_validation_failed
		updatedRun, _ := s.GetRun(run.ID)
		if updatedRun.Status != "packet_validation_failed" {
			t.Errorf("expected status 'packet_validation_failed', got %q", updatedRun.Status)
		}

		// Validation report should be stored and mark repair_eligible = false
		var report validation.ValidationReport
		reportData, err := artifacts.Read(run.ID, "packet_validation_report", "packet_validation_report.json")
		if err != nil {
			t.Fatalf("failed to read report artifact: %v", err)
		}
		_ = json.Unmarshal(reportData, &report)

		if report.RepairEligible {
			t.Error("unsafe path errors must make report non-repair-eligible")
		}
	})
}
