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
	"strings"
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
		exampleBytes, err := os.ReadFile(filepath.Join("testdata", "formal_planner_handoff.md"))
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
		for _, field := range []string{"protocol_version", "schema_version", "created_at", "source_planner_handoff_path", "intended_packet_path"} {
			if val, ok := meta[field].(string); !ok || val == "" {
				t.Errorf("missing or empty metadata field: %s", field)
			}
		}
		producer, ok := meta["producer"].(map[string]interface{})
		if !ok {
			t.Fatal("producer is missing or not an object")
		}
		for _, field := range []string{"kind", "name", "version"} {
			if val, ok := producer[field].(string); !ok || val == "" {
				t.Errorf("missing or empty producer field: %s", field)
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

		// Assert validation contract commands have ID
		contract, ok := exec["validation_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("validation_contract is missing or not an object")
		}
		cmds, ok := contract["commands"].([]interface{})
		if !ok || len(cmds) == 0 {
			t.Fatal("validation_contract.commands is empty or not a list")
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
		exampleBytes, _ := os.ReadFile(filepath.Join("testdata", "formal_planner_handoff.md"))
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

	t.Run("Current template YAML-only compiler_input", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Current Template Compiler Input", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		configMap := map[string]interface{}{
			"repo_target":    repo.Path,
			"branch_context": "main",
		}
		configJSON, _ := json.Marshal(configMap)
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		exampleBytes, err := os.ReadFile(filepath.Join("testdata", "current_template_compiler_input_handoff.md"))
		if err != nil {
			t.Fatalf("failed to read current-template fixture: %v", err)
		}
		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", exampleBytes)
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(res.Issues) > 0 {
			t.Fatalf("expected no parse issues, got: %v", res.Issues)
		}
		if !res.Success {
			t.Fatalf("expected validation success, got errors: %+v", res.ValidationReport.Errors)
		}

		packetBytes, err := artifacts.Read(run.ID, "canonical_packet", "canonical_packet.json")
		if err != nil {
			t.Fatalf("failed to read packet: %v", err)
		}
		var packet map[string]interface{}
		if err := json.Unmarshal(packetBytes, &packet); err != nil {
			t.Fatalf("failed to parse packet: %v", err)
		}

		exec, ok := packet["execution_payload"].(map[string]interface{})
		if !ok {
			t.Fatal("missing execution_payload")
		}
		if got := exec["goal"]; got != "Compile structured compiler input YAML into a canonical packet." {
			t.Fatalf("goal was not populated from YAML, got %q", got)
		}
		nonGoals, ok := exec["non_goals"].([]interface{})
		if !ok || len(nonGoals) != 1 || nonGoals[0] != "Do not change the canonical packet schema." {
			t.Fatalf("non_goals were not mapped from YAML: %+v", exec["non_goals"])
		}

		targets, ok := exec["file_targets"].([]interface{})
		if !ok || len(targets) != 1 {
			t.Fatalf("expected one YAML file target, got %+v", exec["file_targets"])
		}
		target := targets[0].(map[string]interface{})
		if target["path"] != "internal/compiler/compiler.go" || target["reason"] != "Owns compiler input parsing." {
			t.Fatalf("file target was not mapped from YAML: %+v", target)
		}
		if _, ok := target["grounding"]; ok {
			t.Fatalf("template-only grounding field leaked into canonical packet: %+v", target)
		}

		steps, ok := exec["implementation_steps"].([]interface{})
		if !ok || len(steps) != 1 {
			t.Fatalf("expected one YAML implementation step, got %+v", exec["implementation_steps"])
		}
		step := steps[0].(map[string]interface{})
		if step["id"] != "S1" || step["title"] == "" || step["instructions"] == "" {
			t.Fatalf("implementation step was not normalized from YAML: %+v", step)
		}
		targetPaths, ok := step["target_paths"].([]interface{})
		if !ok || len(targetPaths) != 1 || targetPaths[0] != "internal/compiler/compiler.go" {
			t.Fatalf("implementation step target paths mismatch: %+v", step["target_paths"])
		}

		reqs, ok := exec["code_requirements"].([]interface{})
		if !ok || len(reqs) != 1 {
			t.Fatalf("expected one YAML code requirement, got %+v", exec["code_requirements"])
		}
		req := reqs[0].(map[string]interface{})
		if req["id"] != "CR1" || req["requirement"] == "" {
			t.Fatalf("code requirement was not normalized from YAML: %+v", req)
		}

		contract, ok := exec["validation_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("validation_contract missing")
		}
		cmds, ok := contract["commands"].([]interface{})
		if !ok || len(cmds) != 2 {
			t.Fatalf("expected two YAML validation commands, got %+v", contract["commands"])
		}
		requiredCmd := cmds[0].(map[string]interface{})
		if requiredCmd["id"] != "V1" || requiredCmd["command"] != "go test ./internal/compiler" || requiredCmd["required"] != true || requiredCmd["failure_handling"] != "block_if_fails" {
			t.Fatalf("required validation command was not mapped from YAML: %+v", requiredCmd)
		}
		if requiredCmd["purpose"] != "Verify compiler parser regression coverage." ||
			requiredCmd["success_signal"] != "Command exits 0." {
			t.Fatalf("required validation command metadata was not preserved: %+v", requiredCmd)
		}
		advisoryCmd := cmds[1].(map[string]interface{})
		if advisoryCmd["id"] != "V2" || advisoryCmd["command"] != "make validate" || advisoryCmd["required"] != false {
			t.Fatalf("advisory validation command required flag was not preserved: %+v", advisoryCmd)
		}
		if advisoryCmd["purpose"] != "Advisory final/full validation compatibility evidence." ||
			advisoryCmd["success_signal"] != "Command exits 0 and writes full validation evidence." ||
			advisoryCmd["failure_handling"] != "report_if_fails" {
			t.Fatalf("advisory validation command metadata was not preserved: %+v", advisoryCmd)
		}
		for _, cmdVal := range cmds {
			cmd := cmdVal.(map[string]interface{})
			if cmd["command"] == "make validate-fast" {
				t.Fatalf("unexpected default full/fast validation injection: %+v", cmds)
			}
		}

		completion, ok := exec["completion_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("completion_contract missing")
		}
		doneWhen, ok := completion["done_when"].([]interface{})
		if !ok || len(doneWhen) != 1 || doneWhen[0] != "The YAML-only fixture compiles successfully." {
			t.Fatalf("completion done_when was not mapped from YAML: %+v", completion)
		}
		for _, field := range []string{"blocked_when", "allowed_discretion", "forbidden_discretion"} {
			values, ok := completion[field].([]interface{})
			if !ok || len(values) == 0 {
				t.Fatalf("completion_contract field %s missing defaults/content: %+v", field, completion)
			}
		}
	})

	t.Run("Execution Spec compatibility projection", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Runtime Compatibility Path", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		configMap := map[string]interface{}{
			"repo_target":    "Paintersrp/relay",
			"branch_context": "main",
		}
		configJSON, _ := json.Marshal(configMap)
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		exampleBytes, err := os.ReadFile(filepath.Join("testdata", "execution_spec_compiler_input_handoff.md"))
		if err != nil {
			t.Fatalf("failed to read Execution Spec fixture: %v", err)
		}
		if strings.Contains(string(exampleBytes), "<compiler_input>") || strings.Contains(string(exampleBytes), "compiler_input:") {
			t.Fatal("Execution Spec fixture must not contain structured compiler input")
		}
		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", exampleBytes)
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(res.Issues) > 0 {
			t.Fatalf("expected no parse issues, got: %v", res.Issues)
		}
		if !res.Success {
			t.Fatalf("expected validation success, got errors: %+v", res.ValidationReport.Errors)
		}

		packetBytes, err := artifacts.Read(run.ID, "canonical_packet", "canonical_packet.json")
		if err != nil {
			t.Fatalf("failed to read packet: %v", err)
		}
		var packet map[string]interface{}
		if err := json.Unmarshal(packetBytes, &packet); err != nil {
			t.Fatalf("failed to parse packet: %v", err)
		}

		exec, ok := packet["execution_payload"].(map[string]interface{})
		if !ok {
			t.Fatal("missing execution_payload")
		}
		allowedExecKeys := map[string]bool{
			"goal": true, "scope": true, "non_goals": true, "file_targets": true,
			"implementation_steps": true, "code_requirements": true, "validation_contract": true,
			"expected_behavior": true, "completion_contract": true, "executor_final_response_format": true,
		}
		for key := range exec {
			if !allowedExecKeys[key] {
				t.Fatalf("Execution Spec-only key leaked into execution_payload: %s", key)
			}
		}
		for _, forbidden := range []string{"target_symbols", "source_requirements", "source_design", "traceability", "vague_instruction_lint", "open_questions"} {
			if _, ok := exec[forbidden]; ok {
				t.Fatalf("forbidden Execution Spec field leaked into execution_payload: %s", forbidden)
			}
		}

		steps, ok := exec["implementation_steps"].([]interface{})
		if !ok || len(steps) < 2 {
			t.Fatalf("expected projected implementation steps, got %+v", exec["implementation_steps"])
		}
		firstStep := steps[0].(map[string]interface{})
		if firstStep["id"] != "S1" {
			t.Fatalf("expected EXEC-001 to normalize to S1, got %+v", firstStep)
		}
		if !strings.Contains(firstStep["instructions"].(string), "EXEC-001") {
			t.Fatalf("expected original EXEC-001 label in human-readable text: %+v", firstStep)
		}
		if !strings.Contains(firstStep["instructions"].(string), "projectExecutionSpecCompatibility in internal/compiler/compiler.go") {
			t.Fatalf("expected concise target symbol text in step instructions: %+v", firstStep)
		}

		reqs, ok := exec["code_requirements"].([]interface{})
		if !ok || len(reqs) < 2 {
			t.Fatalf("expected projected code requirements, got %+v", exec["code_requirements"])
		}
		firstReq := reqs[0].(map[string]interface{})
		if firstReq["id"] != "CR1" {
			t.Fatalf("expected CR-001 to normalize to CR1, got %+v", firstReq)
		}
		if !strings.Contains(firstReq["requirement"].(string), "CR-001") {
			t.Fatalf("expected original CR-001 label in requirement text: %+v", firstReq)
		}

		contract := exec["validation_contract"].(map[string]interface{})
		cmds, ok := contract["commands"].([]interface{})
		if !ok || len(cmds) != 1 {
			t.Fatalf("expected one projected validation command, got %+v", contract["commands"])
		}
		firstCmd := cmds[0].(map[string]interface{})
		if firstCmd["id"] != "V1" || firstCmd["command"] != "go test ./internal/compiler ./internal/renderer" {
			t.Fatalf("expected V-001 to normalize to V1 with command_or_check mapped, got %+v", firstCmd)
		}
		if !strings.Contains(firstCmd["purpose"].(string), "V-001") {
			t.Fatalf("expected original V-001 label in validation purpose: %+v", firstCmd)
		}

		auditBytes, _ := json.Marshal(packet["audit_seed"])
		auditText := string(auditBytes)
		for _, forbidden := range []string{
			"execution_spec_id", "traceability_boundary", "source_requirements",
			"source_design", "requirements_record_id", "design_record_id",
			"Mechanical under-specification", "prior chat",
		} {
			if strings.Contains(auditText, forbidden) {
				t.Fatalf("audit seed contains wholesale upstream artifact marker %q: %s", forbidden, auditText)
			}
		}
	})

	t.Run("Execution Spec blocking open question blocks compilation", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Runtime Compatibility Blocking Question", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
		configJSON, _ := json.Marshal(map[string]interface{}{
			"repo_target":    "Paintersrp/relay",
			"branch_context": "main",
		})
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		exampleBytes, err := os.ReadFile(filepath.Join("testdata", "execution_spec_compiler_input_handoff.md"))
		if err != nil {
			t.Fatalf("failed to read Execution Spec fixture: %v", err)
		}
		blockingHandoff := strings.Replace(string(exampleBytes), `"open_questions": [],`, `"open_questions": [
    {
      "id": "OQ-001",
      "question": "Which executable behavior should be selected?",
      "blocking": true,
      "affected_area": "execution_payload",
      "resolution_needed": "Planner must resolve behavior before execution."
    }
  ],`, 1)
		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", []byte(blockingHandoff))
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.Success {
			t.Fatal("expected blocking open question to fail compilation")
		}
		found := false
		for _, issue := range res.Issues {
			if strings.Contains(issue, "blocking open question") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected blocking open question issue, got %+v", res.Issues)
		}
	})

	t.Run("RunConfig fallback preserves required:false and report_if_fails", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run config fallback optional command", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Provide validation_contract in runConfig with a required:false/report_if_fails command.
		configMap := map[string]interface{}{
			"repo_target":    repo.Path,
			"branch_context": "main",
			"validation_contract": map[string]interface{}{
				"mode":           "commands",
				"failure_policy": "block",
				"commands": []interface{}{
					map[string]interface{}{
						"id":               "V7",
						"command":          "go test ./internal/renderer",
						"required":         true,
						"purpose":          "Verify renderer.",
						"success_signal":   "Command exits 0.",
						"failure_handling": "attempt_fix_once_then_block",
					},
					map[string]interface{}{
						"id":               "V8",
						"command":          "go test ./internal/validation",
						"required":         false,
						"purpose":          "Optional executor-local check only if validation package files were edited.",
						"success_signal":   "Command exits 0 when validation package was touched.",
						"failure_handling": "report_if_fails",
					},
				},
			},
		}
		configJSON, _ := json.Marshal(configMap)
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		// Write a minimal handoff that produces no validation commands of its own.
		handoffText := `# Planner Handoff

<context_snapshot>
Minimal handoff for runConfig fallback validation test.
</context_snapshot>

<decision_log>
- D1: Use runConfig fallback for validation commands.
  Rationale: Tests optional command metadata preservation.
</decision_log>

<constraints>
- C1: Respect target boundary.
  Applies to: executor
</constraints>

<pass_boundary>
current_pass: 1
total_planned_passes: 1
this_pass_scope: scope
out_of_scope_for_this_pass:
  - none
</pass_boundary>

<compiler_input>
Goal:
Verify that optional validation commands from runConfig are preserved in the compiled packet.

Scope:
Compiler fallback path for validation_contract commands in run_config.json.

Non-goals:
- Do not change the canonical packet schema.

Likely file targets:
- ` + "`internal/compiler/compiler.go`" + `
  role: primary
  action: must_edit
  reason: Owns runConfig fallback validation command handling.

Required implementation steps:
1. Preserve optional command metadata from runConfig.
   action: modify
   target_paths:
     - internal/compiler/compiler.go
   instructions: ensure required:false and report_if_fails survive
   acceptance_criteria:
     - required:false remains false in compiled packet

Code requirements:
- CR1: The runConfig fallback must preserve required and failure_handling per command.
  Applies to:
    - internal/compiler/compiler.go

Expected behavior:
- required:false commands remain optional in compiled execution_payload.validation_contract.commands.

Completion requirements:
- DONE when optional commands compile with required:false and report_if_fails preserved.
- BLOCKED when compiler promotes optional commands to required or blocking.
</compiler_input>
`
		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", []byte(handoffText))
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(res.Issues) > 0 {
			t.Fatalf("expected no parse issues, got: %v", res.Issues)
		}
		if !res.Success {
			t.Fatalf("expected compilation success, got errors: %+v", res.ValidationReport.Errors)
		}

		packetBytes, err := artifacts.Read(run.ID, "canonical_packet", "canonical_packet.json")
		if err != nil {
			t.Fatalf("failed to read packet: %v", err)
		}
		var packet map[string]interface{}
		if err := json.Unmarshal(packetBytes, &packet); err != nil {
			t.Fatalf("failed to parse packet: %v", err)
		}

		exec, ok := packet["execution_payload"].(map[string]interface{})
		if !ok {
			t.Fatal("missing execution_payload")
		}
		contract, ok := exec["validation_contract"].(map[string]interface{})
		if !ok {
			t.Fatal("missing validation_contract")
		}
		cmds, ok := contract["commands"].([]interface{})
		if !ok || len(cmds) == 0 {
			t.Fatal("validation_contract.commands is empty or not a list")
		}

		// Find the optional command and assert required:false and report_if_fails are preserved.
		foundOptional := false
		for _, cmdVal := range cmds {
			cmdMap, ok := cmdVal.(map[string]interface{})
			if !ok {
				continue
			}
			if cmdMap["command"] == "go test ./internal/validation" {
				foundOptional = true
				if id, _ := cmdMap["id"].(string); id != "V8" {
					t.Errorf("optional command id was not preserved, got %q: %+v", id, cmdMap)
				}
				if reqVal, _ := cmdMap["required"].(bool); reqVal {
					t.Errorf("optional command was promoted to required:true in compiled packet: %+v", cmdMap)
				}
				if purpose, _ := cmdMap["purpose"].(string); purpose != "Optional executor-local check only if validation package files were edited." {
					t.Errorf("optional command purpose was not preserved, got %q: %+v", purpose, cmdMap)
				}
				if fh, _ := cmdMap["failure_handling"].(string); fh != "report_if_fails" {
					t.Errorf("optional command failure_handling was not preserved as report_if_fails, got %q: %+v", fh, cmdMap)
				}
				if ss, _ := cmdMap["success_signal"].(string); ss != "Command exits 0 when validation package was touched." {
					t.Errorf("optional command success_signal was not preserved, got %q: %+v", ss, cmdMap)
				}
			}
		}
		if !foundOptional {
			t.Errorf("optional validation command 'go test ./internal/validation' was not found in compiled packet commands: %+v", cmds)
		}

		// Find the required command and confirm it remains required.
		foundRequired := false
		for _, cmdVal := range cmds {
			cmdMap, ok := cmdVal.(map[string]interface{})
			if !ok {
				continue
			}
			if cmdMap["command"] == "go test ./internal/renderer" {
				foundRequired = true
				if id, _ := cmdMap["id"].(string); id != "V7" {
					t.Errorf("required command id was not preserved, got %q: %+v", id, cmdMap)
				}
				if reqVal, _ := cmdMap["required"].(bool); !reqVal {
					t.Errorf("required command was demoted to required:false in compiled packet: %+v", cmdMap)
				}
				if fh, _ := cmdMap["failure_handling"].(string); fh != "attempt_fix_once_then_block" {
					t.Errorf("required command failure_handling was not preserved, got %q: %+v", fh, cmdMap)
				}
			}
		}
		if !foundRequired {
			t.Errorf("required validation command 'go test ./internal/renderer' was not found in compiled packet commands: %+v", cmds)
		}
	})

	t.Run("Nested field extraction inside compiler_input", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run with Nested Fields", "approved_for_prepare", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		configMap := map[string]interface{}{
			"repo_target":    repo.Path,
			"branch_context": "main",
			"file_targets":   []string{"src/compiler.go"},
		}
		configJSON, _ := json.Marshal(configMap)
		configPath, _ := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
		_, _ = s.CreateArtifact(run.ID, "run_config", configPath, "application/json")

		handoffText := `# Planner Handoff

<context_snapshot>
This is the snapshot
</context_snapshot>

<decision_log>
- D1: Use a custom compiler test.
  Rationale: To verify nested field extraction.
</decision_log>

<constraints>
- C1: Respect target boundary.
  Applies to: executor
</constraints>

<pass_boundary>
current_pass: 1
total_planned_passes: 1
this_pass_scope: scope
out_of_scope_for_this_pass:
  - none
</pass_boundary>

<validation_expectations>
- V1:
  command: go test ./internal/compiler
  required: true
  purpose: Verify nested field extraction.
  success_signal: Command exits 0.
  failure_handling: attempt_fix_once_then_block
</validation_expectations>

<compiler_input>
Goal:
Test nested parser.

Scope:
Verification scope.

Non-goals:
- Do not build production app.

Likely file targets:
- ` + "`" + `src/compiler.go` + "`" + `
  role: primary
  action: must_edit
  reason: testing

Required implementation steps:
1. First step.
   action: modify
   target_paths:
     - src/compiler.go
   instructions: test it
   acceptance_criteria:
     - passes

Expected behavior:
- Works as expected.

Completion requirements:
- DONE when it works.
- BLOCKED when stuck.

Rejected alternatives:
- RA1: Avoid fixing the parser.
  Reason rejected: Required for the task.

Risk register:
- R1: Broken compiler logic.
  Severity: high
  Description: Might introduce syntax issues.
  Mitigation: Add comprehensive regression tests.

Code requirements:
- CR1: The parser must extract nested fields.
  Applies to:
    - src/compiler.go
</compiler_input>
`
		handoffPath, _ := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", []byte(handoffText))
		_, _ = s.CreateArtifact(run.ID, "planner_handoff", handoffPath, "text/markdown")

		res, err := c.CompileApprovedRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(res.Issues) > 0 {
			t.Fatalf("expected no issues, got: %v", res.Issues)
		}

		if !res.Success {
			t.Fatalf("expected validation success, got errors: %+v", res.ValidationReport.Errors)
		}

		// Read packet
		packetBytes, err := artifacts.Read(run.ID, "canonical_packet", "canonical_packet.json")
		if err != nil {
			t.Fatalf("failed to read packet: %v", err)
		}
		var packet map[string]interface{}
		_ = json.Unmarshal(packetBytes, &packet)

		// Assert rejected alternatives extracted
		pCtx, _ := packet["planner_context"].(map[string]interface{})
		rej, ok := pCtx["rejected_alternatives"].([]interface{})
		if !ok || len(rej) == 0 {
			t.Fatalf("rejected_alternatives was not extracted or empty: %+v", pCtx["rejected_alternatives"])
		}
		rMap := rej[0].(map[string]interface{})
		if rMap["id"] != "RA1" || rMap["alternative"] != "Avoid fixing the parser." || rMap["reason_rejected"] != "Required for the task." {
			t.Errorf("rejected_alternatives content mismatch: %+v", rej)
		}

		// Assert risk register extracted
		risks, ok := pCtx["risk_register"].([]interface{})
		if !ok || len(risks) == 0 {
			t.Fatalf("risk_register was not extracted or empty: %+v", pCtx["risk_register"])
		}
		riskMap := risks[0].(map[string]interface{})
		if riskMap["id"] != "R1" || riskMap["severity"] != "high" || riskMap["description"] != "Broken compiler logic." || riskMap["mitigation"] != "Add comprehensive regression tests." {
			t.Errorf("risk_register content mismatch: %+v", risks)
		}

		// Assert code requirements extracted
		exec, _ := packet["execution_payload"].(map[string]interface{})
		cr, ok := exec["code_requirements"].([]interface{})
		if !ok || len(cr) == 0 {
			t.Fatalf("code_requirements was not extracted or empty: %+v", exec["code_requirements"])
		}
		crMap := cr[0].(map[string]interface{})
		if crMap["id"] != "CR1" || crMap["requirement"] != "The parser must extract nested fields." {
			t.Errorf("code_requirements content mismatch: %+v", cr)
		}
	})
}
