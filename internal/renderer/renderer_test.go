package renderer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

func TestRenderer(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

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

	rend := New(s)

	validPacket := map[string]interface{}{
		"packet_meta": map[string]interface{}{
			"packet_id":       "packet-2026-06-16-test-render",
			"task_slug":       "test-render",
			"target_executor": "deepseek-v4-flash",
			"repo_target":     "Paintersrp/relay",
			"branch_context":  "main",
			"content_profile": "implementation_ready",
			"lifecycle_state": "packet_created",
			"artifact_paths": map[string]interface{}{
				"canonical_packet":  "handoffs/packets/test.json",
				"validation_report": "handoffs/validation/test.json",
				"executor_brief":    "handoffs/briefs/test.md",
				"executor_result":   "handoffs/results/test.txt",
				"audit_packet":      "handoffs/audits/test.md",
				"planner_handoff":   "handoffs/planner/test.md",
			},
			"protocol_version": "1.0.0",
			"schema_version":   "1.0.0",
			"created_at":       "2026-06-16T21:50:42Z",
			"producer": map[string]interface{}{
				"kind":    "middleware",
				"name":    "relay-packet-compiler",
				"version": "1.0.0",
			},
			"source_planner_handoff_path": "handoffs/planner/test.md",
			"intended_packet_path":        "handoffs/packets/test.json",
			"model_routing": map[string]interface{}{
				"planner_model":        "gpt-4o",
				"compiler_version":     "gpt-4o",
				"recommended_executor": "deepseek-v4-flash",
				"routing_reason":       "test",
			},
		},
		"planner_context": map[string]interface{}{
			"user_request_summary": "test",
			"context_snapshot":     []interface{}{"fileA.go"},
			"decision_log": []interface{}{
				map[string]interface{}{"id": "D1", "summary": "dec", "rationale": "rat"},
			},
			"assumptions": []interface{}{},
			"constraints": []interface{}{
				map[string]interface{}{"id": "C1", "statement": "const", "applies_to": []interface{}{"renderer"}},
			},
			"rejected_alternatives": []interface{}{},
			"known_repo_facts":      []interface{}{},
			"pass_boundary": map[string]interface{}{
				"current_pass":               1,
				"total_planned_passes":       1,
				"this_pass_scope":            "test",
				"out_of_scope_for_this_pass": []interface{}{},
			},
			"unresolved_questions": []interface{}{},
		},
		"execution_payload": map[string]interface{}{
			"goal":      "Implement a test goal",
			"scope":     "Scope of test",
			"non_goals": []interface{}{"Not doing X"},
			"file_targets": []interface{}{
				map[string]interface{}{
					"path":   "internal/renderer/renderer.go",
					"role":   "primary",
					"action": "must_edit",
					"reason": "needed",
				},
			},
			"implementation_steps": []interface{}{
				map[string]interface{}{
					"id":                  "S1",
					"title":               "Step 1",
					"action":              "create",
					"target_paths":        []interface{}{"internal/renderer/renderer.go"},
					"instructions":        "write code",
					"acceptance_criteria": []interface{}{"it compiles"},
				},
			},
			"code_requirements": []interface{}{},
			"validation_contract": map[string]interface{}{
				"mode":           "commands",
				"failure_policy": "block",
				"commands": []interface{}{
					map[string]interface{}{
						"id":               "V1",
						"command":          "go test ./...",
						"required":         true,
						"purpose":          "Verify test suite",
						"success_signal":   "PASS",
						"failure_handling": "block_if_fails",
					},
				},
			},
			"expected_behavior": []interface{}{"It works"},
			"completion_contract": map[string]interface{}{
				"done_when":            []interface{}{"it passes tests"},
				"blocked_when":         []interface{}{"build fails"},
				"allowed_discretion":   []interface{}{},
				"forbidden_discretion": []interface{}{"no cheating"},
			},
			"executor_final_response_format": "DONE_or_BLOCKED_strict_text",
			"implementation_contract": []interface{}{
				map[string]interface{}{
					"section_name": "artifact_contract_table",
					"summary":      "Keep the executor brief and validation report artifacts aligned.",
					"details":      []interface{}{"Smoke fixture remains a canonical-packet-shaped validation example."},
					"required":     true,
				},
			},
			"pass_exit_evidence": []interface{}{
				map[string]interface{}{
					"requirement":         "Smoke packet uses the current producer and validation_contract fields.",
					"observable_evidence": "packet_meta.producer and execution_payload.validation_contract are present in the packet JSON.",
					"evidence_source":     "artifact_output",
					"acceptance_check":    "The smoke packet validates except for the intentionally omitted audit_seed.",
					"failure_meaning":     "The smoke fixture has drifted from the current canonical packet shape.",
				},
			},
		},
		"audit_seed": map[string]interface{}{
			"audit_checklist": []interface{}{
				map[string]interface{}{"id": "A1", "check": "check", "severity_if_failed": "error"},
			},
			"scope_drift_checks":      []interface{}{},
			"non_goal_checks":         []interface{}{},
			"file_scope_checks":       []interface{}{},
			"risk_checks":             []interface{}{},
			"validation_expectations": []interface{}{},
			"manual_review_checklist": []interface{}{},
		},
	}

	t.Run("Invalid run ID", func(t *testing.T) {
		_, err := rend.RenderExecutorBrief(context.Background(), 9999)
		if err == nil {
			t.Fatal("expected error for invalid run ID")
		}
	})

	t.Run("Wrong status blocking", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run wrong status", "draft", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no go error, got %v", err)
		}
		if res.Success {
			t.Error("expected render to fail due to wrong state")
		}
		if len(res.Issues) == 0 || !strings.Contains(res.Issues[0], "invalid run status") {
			t.Errorf("expected state error issue, got: %v", res.Issues)
		}

		// Check validation report was written
		exists := artifacts.Exists(run.ID, "brief_validation_report", "brief_validation_report.json")
		if !exists {
			t.Error("expected brief_validation_report.json to be written")
		}
		reportBytes, _ := artifacts.Read(run.ID, "brief_validation_report", "brief_validation_report.json")
		var report BriefValidationReport
		_ = json.Unmarshal(reportBytes, &report)
		if report.Status != "failed" {
			t.Errorf("expected report status failed, got %s", report.Status)
		}
	})

	t.Run("Missing canonical packet", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run missing packet", "packet_validated", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}
		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no go error, got %v", err)
		}
		if res.Success {
			t.Error("expected render to fail due to missing packet")
		}
		if len(res.Issues) == 0 || !strings.Contains(res.Issues[0], "failed to read canonical_packet.json") {
			t.Errorf("expected missing packet issue, got: %v", res.Issues)
		}
	})

	t.Run("Malformed canonical packet", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run malformed packet", "packet_validated", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		path, err := artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", []byte("{invalid json"))
		if err != nil {
			t.Fatalf("failed to write malformed packet: %v", err)
		}
		_, _ = s.CreateArtifact(run.ID, "canonical_packet", path, "application/json")

		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no go error, got %v", err)
		}
		if res.Success {
			t.Error("expected render to fail due to malformed packet")
		}
	})

	t.Run("Missing validation contract", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run missing validation contract", "packet_validated", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		badPacket := make(map[string]interface{})
		for k, v := range validPacket {
			badPacket[k] = v
		}
		execPayload := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			execPayload[k] = v
		}
		delete(execPayload, "validation_contract")
		badPacket["execution_payload"] = execPayload

		packetBytes, _ := json.Marshal(badPacket)
		path, _ := artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", packetBytes)
		_, _ = s.CreateArtifact(run.ID, "canonical_packet", path, "application/json")

		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.Success {
			t.Fatal("expected render to fail due to missing validation contract")
		}
		found := false
		for _, issue := range res.Issues {
			if strings.Contains(issue, "validation_contract") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected validation_contract issue, got %v", res.Issues)
		}
	})

	t.Run("Malformed validation contract", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run malformed validation contract", "packet_validated", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		badPacket := make(map[string]interface{})
		for k, v := range validPacket {
			badPacket[k] = v
		}
		execPayload := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			execPayload[k] = v
		}
		execPayload["validation_contract"] = map[string]interface{}{
			"mode":           "commands",
			"failure_policy": "block",
			"commands":       []interface{}{},
		}
		badPacket["execution_payload"] = execPayload

		packetBytes, _ := json.Marshal(badPacket)
		path, _ := artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", packetBytes)
		_, _ = s.CreateArtifact(run.ID, "canonical_packet", path, "application/json")

		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.Success {
			t.Fatal("expected render to fail due to malformed validation contract")
		}
		found := false
		for _, issue := range res.Issues {
			if strings.Contains(issue, "validation_contract") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected validation_contract issue, got %v", res.Issues)
		}
	})

	t.Run("Successful Render", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run successful render", "packet_validated", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		packetBytes, _ := json.Marshal(validPacket)
		path, _ := artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", packetBytes)
		_, _ = s.CreateArtifact(run.ID, "canonical_packet", path, "application/json")

		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !res.Success {
			t.Fatalf("expected success, got issues: %v", res.Issues)
		}

		// Verify artifact writes
		if !artifacts.Exists(run.ID, "executor_brief", "executor_brief.md") {
			t.Error("executor_brief.md was not written")
		}
		if !artifacts.Exists(run.ID, "brief_validation_report", "brief_validation_report.json") {
			t.Error("brief_validation_report.json was not written")
		}

		// Verify state update
		updatedRun, _ := s.GetRun(run.ID)
		if updatedRun.Status != "brief_ready_for_review" {
			t.Errorf("expected status brief_ready_for_review, got %s", updatedRun.Status)
		}

		// Check exclusion of planner_context and audit_seed in rendered brief
		briefBytes, _ := artifacts.Read(run.ID, "executor_brief", "executor_brief.md")
		briefText := string(briefBytes)
		if strings.Contains(briefText, "planner_context") || strings.Contains(briefText, "audit_seed") {
			t.Error("rendered brief contains planner_context or audit_seed")
		}
	})

	t.Run("Validation commands split required and optional executor-local sections", func(t *testing.T) {
		packet := make(map[string]interface{})
		for k, v := range validPacket {
			packet[k] = v
		}
		execPayload := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			execPayload[k] = v
		}
		execPayload["validation_contract"] = map[string]interface{}{
			"mode":           "commands",
			"failure_policy": "block",
			"commands": []interface{}{
				map[string]interface{}{
					"id":               "V1",
					"command":          "go test ./internal/renderer",
					"required":         true,
					"purpose":          "Affected-area renderer regression coverage.",
					"success_signal":   "Command exits 0.",
					"failure_handling": "attempt_fix_once_then_block",
				},
				map[string]interface{}{
					"id":               "V2",
					"command":          "make validate",
					"required":         false,
					"purpose":          "Optional executor-local check.",
					"success_signal":   "Command exits 0 and writes full validation evidence.",
					"failure_handling": "report_if_fails",
				},
			},
		}
		packet["execution_payload"] = execPayload

		templateBytes, err := os.ReadFile(locateTemplateFile("handoffs/templates/executor_brief_template.md"))
		if err != nil {
			t.Fatalf("failed to read executor brief template: %v", err)
		}
		briefText, err := renderTemplate(string(templateBytes), packet)
		if err != nil {
			t.Fatalf("failed to render template: %v", err)
		}

		// Required commands must appear in the required section.
		requiredHeading := strings.Index(briefText, "Required Executor validation commands:")
		if requiredHeading == -1 {
			t.Fatalf("expected 'Required Executor validation commands:' heading, got:\n%s", briefText)
		}

		// Optional executor-local commands must appear in the optional section.
		optionalHeading := strings.Index(briefText, "Optional executor-local validation commands:")
		if optionalHeading == -1 {
			t.Fatalf("expected 'Optional executor-local validation commands:' heading, got:\n%s", briefText)
		}

		// The required section must precede the optional section.
		if requiredHeading >= optionalHeading {
			t.Fatalf("required section must come before optional section; requiredAt=%d optionalAt=%d", requiredHeading, optionalHeading)
		}

		requiredSection := briefText[requiredHeading:optionalHeading]
		optionalSection := briefText[optionalHeading:]

		// Required command in required section.
		if !strings.Contains(requiredSection, "go test ./internal/renderer") {
			t.Fatalf("required command missing from required section:\n%s", requiredSection)
		}
		// Optional command must not appear in required section.
		if strings.Contains(requiredSection, "make validate") {
			t.Fatalf("optional command rendered as required:\n%s", requiredSection)
		}
		// Optional command in optional section.
		if !strings.Contains(optionalSection, "make validate") {
			t.Fatalf("optional command missing from optional executor-local section:\n%s", optionalSection)
		}

		// Optional section must not use finalization/closeout/audit/hook/advisory wording.
		forbiddenOptionalWords := []string{
			"advisory/final",
			"Advisory/final",
			"finalization",
			"closeout",
			"hook evidence",
			"audit evidence",
			"not pass-local Executor-required work",
		}
		for _, word := range forbiddenOptionalWords {
			if strings.Contains(optionalSection, word) {
				t.Errorf("optional section contains forbidden ownership wording %q:\n%s", word, optionalSection)
			}
		}

		// The brief must not contain any of the forbidden classification terms anywhere.
		forbiddenBriefWords := []string{
			"Advisory/final validation evidence",
			"advisory/final validation evidence",
		}
		for _, word := range forbiddenBriefWords {
			if strings.Contains(briefText, word) {
				t.Errorf("rendered brief contains stale advisory/final wording %q", word)
			}
		}
	})

	t.Run("Validation Failure - Secret Detected", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run secret validation fail", "packet_validated", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		badPacket := make(map[string]interface{})
		for k, v := range validPacket {
			badPacket[k] = v
		}
		execPayload := make(map[string]interface{})
		for k, v := range validPacket["execution_payload"].(map[string]interface{}) {
			execPayload[k] = v
		}
		// Inject a secret key
		execPayload["goal"] = "Goal containing aws_secret_access_key='abcdef1234567890abcdef'"
		badPacket["execution_payload"] = execPayload

		packetBytes, _ := json.Marshal(badPacket)
		path, _ := artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", packetBytes)
		_, _ = s.CreateArtifact(run.ID, "canonical_packet", path, "application/json")

		res, err := rend.RenderExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if res.Success {
			t.Error("expected render to fail due to secret detection")
		}

		reportBytes, _ := artifacts.Read(run.ID, "brief_validation_report", "brief_validation_report.json")
		var report BriefValidationReport
		_ = json.Unmarshal(reportBytes, &report)
		if report.Status != "failed" {
			t.Errorf("expected report status failed, got %s", report.Status)
		}
		foundSecretIssue := false
		for _, issue := range report.Issues {
			if issue.Code == "SENSITIVE_DATA_DETECTED" {
				foundSecretIssue = true
			}
		}
		if !foundSecretIssue {
			t.Errorf("expected SENSITIVE_DATA_DETECTED issue, got %v", report.Issues)
		}
	})

	t.Run("Approve Brief Flow", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run approve brief", "brief_ready_for_review", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Write valid brief and report
		briefPath, _ := artifacts.Write(run.ID, "executor_brief", "executor_brief.md", []byte("rendered brief"))
		_, _ = s.CreateArtifact(run.ID, "executor_brief", briefPath, "text/markdown")

		report := BriefValidationReport{
			SchemaVersion: "1.0.0",
			RunID:         run.ID,
			ArtifactName:  "executor_brief.md",
			Status:        "passed",
			CreatedAt:     "2026-06-16T21:50:42Z",
		}
		repBytes, _ := json.Marshal(report)
		repPath, _ := artifacts.Write(run.ID, "brief_validation_report", "brief_validation_report.json", repBytes)
		_, _ = s.CreateArtifact(run.ID, "brief_validation_report", repPath, "application/json")

		res, err := rend.ApproveExecutorBrief(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !res.Success {
			t.Fatalf("expected approval success, got issues: %v", res.Issues)
		}

		// Check database status
		updatedRun, _ := s.GetRun(run.ID)
		if updatedRun.Status != "approved_for_executor" {
			t.Errorf("expected status approved_for_executor, got %s", updatedRun.Status)
		}

		// Check run_config.json contains brief_approval_decision
		configBytes, err := artifacts.Read(run.ID, "run_config", "run_config.json")
		if err != nil {
			t.Fatalf("failed to read run_config: %v", err)
		}
		var configMap map[string]interface{}
		_ = json.Unmarshal(configBytes, &configMap)
		if configMap["brief_approval_decision"] != "approved" {
			t.Errorf("expected brief_approval_decision = approved, got %v", configMap["brief_approval_decision"])
		}
	})
}
