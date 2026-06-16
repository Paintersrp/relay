package compiler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/validation"
	"testing"
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

	t.Run("Successful Compilation", func(t *testing.T) {
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
		// Read example markdown from fixtures
		exampleBytes, err := os.ReadFile("d:/Code/relay/docs/phase5_examples_fixtures/planner_handoff.example.md")
		if err != nil {
			t.Fatalf("failed to read planner_handoff fixture: %v", err)
		}

		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", exampleBytes)
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !res.Success {
			t.Fatalf("expected compilation success, got issues: %v", res.Issues)
		}

		if res.PacketID != "packet-2026-06-16-fix-overflow-stale-ui" {
			t.Errorf("expected packet ID 'packet-2026-06-16-fix-overflow-stale-ui', got %q", res.PacketID)
		}

		// Check database status
		updatedRun, _ := s.GetRun(run.ID)
		if updatedRun.Status != "packet_validated" {
			t.Errorf("expected status 'packet_validated', got %q", updatedRun.Status)
		}

		// Check check was created
		checks, _ := s.ListChecksByRun(run.ID)
		foundPass := false
		for _, ch := range checks {
			if ch.Kind == "validation" && ch.Status == "pass" {
				foundPass = true
			}
		}
		if !foundPass {
			t.Error("expected pass check for validation")
		}

		// Check artifacts were registered
		arts, _ := s.ListArtifactsByRun(run.ID)
		foundPacket := false
		foundReport := false
		for _, a := range arts {
			if a.Kind == "canonical_packet" {
				foundPacket = true
			}
			if a.Kind == "packet_validation_report" {
				foundReport = true
			}
		}
		if !foundPacket {
			t.Error("expected canonical_packet artifact registered")
		}
		if !foundReport {
			t.Error("expected packet_validation_report artifact registered")
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
		exampleBytes, _ := os.ReadFile("d:/Code/relay/docs/phase5_examples_fixtures/planner_handoff.example.md")
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
