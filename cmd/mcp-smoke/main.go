// Command mcp-smoke is an executable smoke harness for the Relay MCP stdio server.
//
// It spawns the relay-mcpserver binary as a subprocess, drives it over stdin/stdout
// using newline-delimited JSON-RPC 2.0, and asserts each expected behavior.
//
// Usage:
//
//	go run ./cmd/mcp-smoke         (from repo root, after make mcp-build)
//	make mcp-smoke                 (builds then runs)
//
// The harness exits 0 on full pass, nonzero on any mismatch.
// It uses temp directories for the DB and artifacts so production data is never touched.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"relay/internal/store"
)

// --- JSON-RPC types (minimal, local to this harness) ---

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent map[string]interface{} `json:"structuredContent,omitempty"`
	IsError           bool                   `json:"isError,omitempty"`
}

// --- harness state ---

type harness struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	nextID    int
	pass      int
	fail      int
	httpURL   string
	httpToken string
	dbPath    string
	// repoRoot is the path to the isolated git fixture used for context-broker smoke.
	repoRoot string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nSMOKE FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	httpURL := os.Getenv("RELAY_MCP_URL")
	httpToken := os.Getenv("RELAY_MCP_AUTH_TOKEN")

	var h *harness

	if httpURL != "" {
		fmt.Printf("Running smoke test in HTTP mode targeting %s\n", httpURL)
		h = &harness{
			nextID:    1,
			httpURL:   httpURL,
			httpToken: httpToken,
		}
	} else {
		fmt.Println("Running smoke test in stdio mode")
		// Create isolated temp directories for this smoke run.
		tmpDir, err := os.MkdirTemp("", "relay-mcp-smoke-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		dbPath := filepath.Join(tmpDir, "relay.sqlite")
		artifactsDir := filepath.Join(tmpDir, "artifacts")
		if err := os.MkdirAll(artifactsDir, 0755); err != nil {
			return fmt.Errorf("create artifacts dir: %w", err)
		}

		// Pre-seed the database with a test project and isolated git fixture
		// before starting the MCP server.
		repoRoot, err := seedTestDatabase(dbPath)
		if err != nil {
			return fmt.Errorf("seed test database: %w", err)
		}

		// Locate the mcpserver binary.
		binaryName := "relay-mcpserver"
		if runtime.GOOS == "windows" {
			binaryName = "relay-mcpserver.exe"
		}
		binaryPath := filepath.Join("bin", binaryName)
		if _, err := os.Stat(binaryPath); err != nil {
			return fmt.Errorf("MCP binary not found at %q — run 'make mcp-build' first: %w", binaryPath, err)
		}

		// Launch the subprocess.
		cmd := exec.Command(binaryPath)
		cmd.Env = append(os.Environ(),
			"RELAY_DB_PATH="+dbPath,
			"RELAY_ARTIFACTS_DIR="+artifactsDir,
		)
		cmd.Stderr = os.Stderr

		stdinPipe, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe: %w", err)
		}
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("stdout pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start mcpserver: %w", err)
		}
		defer func() {
			_ = stdinPipe.Close()
			_ = cmd.Wait()
		}()

		h = &harness{
			cmd:      cmd,
			stdin:    stdinPipe,
			stdout:   bufio.NewScanner(stdoutPipe),
			nextID:   1,
			dbPath:   dbPath,
			repoRoot: repoRoot,
		}
		h.stdout.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

		// Give the server a moment to initialize.
		time.Sleep(200 * time.Millisecond)
	}

	// -------------------------------------------------------
	// 1. Initialize handshake
	// -------------------------------------------------------
	resp, err := h.call("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "mcp-smoke", "version": "0.1.0"},
		"capabilities":    map[string]interface{}{},
	})
	if err != nil {
		return h.fatal("initialize", err)
	}
	if resp.Error != nil {
		return h.fatal("initialize", fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message))
	}
	h.check("initialize", resp.Error == nil)

	// Send initialized notification (no response expected).
	if err := h.notify("initialized", nil); err != nil {
		return h.fatal("initialized notify", err)
	}

	// -------------------------------------------------------
	// 2. Ping
	// -------------------------------------------------------
	resp, err = h.call("ping", nil)
	if err != nil {
		return h.fatal("ping", err)
	}
	h.check("ping", resp.Error == nil)

	// -------------------------------------------------------
	// 3. tools/list — verify tool inventory based on profile
	// -------------------------------------------------------

	// Test with default (local-operator) profile first
	resp, err = h.call("tools/list", nil)
	if err != nil {
		return h.fatal("tools/list", err)
	}
	if resp.Error != nil {
		return h.fatal("tools/list", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var toolsList struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(resp.Result, &toolsList); err != nil {
		return h.fatal("tools/list parse", err)
	}

	coreTools := map[string]bool{
		"submit_test_audit_packet":             true,
		"create_run_from_planner_handoff":      true,
		"create_run_from_planner_handoff_file": true,
		"submit_planner_pass_plan":             true,
		"list_open_runs":                       true,
		"get_run_status":                       true,
		"submit_audit_packet":                  true,
		// Plan v2 attempt / intent / seed tools
		"create_plan_attempt_with_intent": true,
		"get_plan_intent_review_packet":   true,
		"submit_intent_drift_review":      true,
		"revise_plan_attempt":             true,
		"void_plan_attempt":               true,
		"approve_plan_attempt":            true,
		"submit_plan_attempt":             true,
		"create_plan_seed":                true,
		"list_plan_seeds":                 true,
		"get_plan_seed":                   true,
		"get_plan_seed_planning_context":  true,
		"create_plan_attempt_from_seed":   true,
		"update_plan_seed":                true,
		"defer_plan_seed":                 true,
		"reject_plan_seed":                true,
	}

	contextBrokerTools := map[string]bool{
		"get_project":                      true,
		"get_plan":                         true,
		"get_pass":                         true,
		"get_pass_context":                 true,
		"get_next_pass_work":               true,
		"get_next_audit_work":              true,
		"create_source_snapshot":           true,
		"list_project_files":               true,
		"search_project_files":             true,
		"read_project_file":                true,
		"get_repository_git_status":        true,
		"get_repository_recent_commit":     true,
		"list_repository_changed_files":    true,
		"get_repository_diff":              true,
		"create_context_packet":            true,
		"get_context_packet":               true,
		"create_local_audit":               true,
		"get_local_audit":                  true,
		"list_project_local_audits":        true,
		"search_project_context_memory":    true,
		"list_project_context_records":     true,
		"get_project_context_record":       true,
		"create_project_context_record":    true,
		"supersede_project_context_record": true,
		// Refactor discovery tools
		"list_refactor_discovery_tasks":    true,
		"get_refactor_discovery_task":      true,
		"create_refactor_discovery_task":   true,
		"update_refactor_discovery_task":   true,
		"complete_refactor_discovery_task": true,
		"close_refactor_discovery_task":    true,
		"supersede_refactor_discovery_task": true,
		// Refactor backlog tools (candidate and plan generation)
		"list_refactor_candidates":             true,
		"get_refactor_candidate":               true,
		"search_refactor_candidates":           true,
		"create_refactor_candidate":            true,
		"update_refactor_candidate":            true,
		"defer_refactor_candidate":             true,
		"reject_refactor_candidate":            true,
		"supersede_refactor_candidate":         true,
		"suggest_refactor_candidate_placement": true,
		"promote_refactor_candidate_to_plan":   true,
		"generate_refactor_only_plan":          true,
		// Context-gathering workflow tools
		"resolve_project_repository": true,
		"get_run_artifact":           true,
		"validate_planner_handoff_for_compile": true,
		"prepare_handoff_context": true,
	}

	unsafeKeywords := []string{"exec", "shell", "write_file", "git_commit", "git_push", "checkout", "reset", "branch"}

	// Discover the full tool inventory across all bounded pages.
	allTools := make(map[string]bool)
	seenCursors := make(map[string]bool)
	pageCount := 0
	const maxPages = 5
	cursor := toolsList.NextCursor
	for _, tool := range toolsList.Tools {
		allTools[tool.Name] = true
	}
	pageCount++
	for cursor != "" {
		if pageCount >= maxPages {
			h.failf("tools/list pagination exceeded max pages (%d) — possible cursor loop", maxPages)
			break
		}
		if seenCursors[cursor] {
			h.failf("tools/list duplicate cursor %q — non-terminating pagination", cursor)
			break
		}
		seenCursors[cursor] = true

		resp, err = h.call("tools/list", map[string]interface{}{
			"cursor": cursor,
		})
		if err != nil {
			return h.fatal("tools/list page", err)
		}
		if resp.Error != nil {
			return h.fatal("tools/list page", fmt.Errorf("RPC error: %s", resp.Error.Message))
		}
		var pageList struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Result, &pageList); err != nil {
			return h.fatal("tools/list page parse", err)
		}
		for _, tool := range pageList.Tools {
			allTools[tool.Name] = true
		}
		cursor = pageList.NextCursor
		pageCount++
	}

	// Validate discovered tools: each must be in the approved set.
	hasNextPassWork := false
	hasNextAudit := false
	for name := range allTools {
		isApproved := coreTools[name] || contextBrokerTools[name]
		h.check("tools/list approved:"+name, isApproved)

		if name == "get_next_pass_work" {
			hasNextPassWork = true
		}
		if name == "get_next_audit_work" {
			hasNextAudit = true
		}

		for _, unsafe := range unsafeKeywords {
			lname := strings.ToLower(name)
			if strings.Contains(lname, unsafe) {
				h.failf("UNSAFE tool registered: %q contains keyword %q", name, unsafe)
			}
		}
	}
	h.check("tools/list has get_next_pass_work", hasNextPassWork)
	h.check("tools/list has get_next_audit_work", hasNextAudit)

	// Assert required streamlined workflow tool names are present.
	requiredStreamlinedNames := []string{
		"resolve_project_repository",
		"create_source_snapshot",
		"get_next_pass_work",
		"prepare_handoff_context",
		"get_run_artifact",
		"validate_planner_handoff_for_compile",
		"create_run_from_planner_handoff_file",
	}
	for _, name := range requiredStreamlinedNames {
		h.check("tools/list has "+name, allTools[name])
	}

	h.check("tools/list discovered pages", pageCount >= 1)
	// -------------------------------------------------------
	// 4. submit_test_audit_packet — sentinel artifact at run ID 0
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "submit_test_audit_packet",
		"arguments": map[string]string{
			"run_id":                "mcp-test",
			"audit_packet_markdown": "# Smoke Test Packet\n\nThis is the Pass 16 smoke test.",
			"decision":              "accepted",
		},
	})
	if err != nil {
		return h.fatal("submit_test_audit_packet", err)
	}
	if resp.Error != nil {
		return h.fatal("submit_test_audit_packet", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}
	var testAuditResult toolCallResult
	if err := json.Unmarshal(resp.Result, &testAuditResult); err != nil {
		return h.fatal("submit_test_audit_packet parse", err)
	}
	h.check("submit_test_audit_packet !isError", !testAuditResult.IsError)
	if len(testAuditResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(testAuditResult.Content[0].Text), &out); err == nil {
			h.check("submit_test_audit_packet ok=true", out["ok"] == true)
			if path, ok := out["artifact_path"].(string); ok {
				sentinelPath := path
				_, statErr := os.Stat(sentinelPath)
				h.check("submit_test_audit_packet sentinel artifact exists", statErr == nil)
				fmt.Printf("  sentinel artifact: %s\n", sentinelPath)
			}
		}
	}

	// -------------------------------------------------------
	// 5. submit_planner_pass_plan — create managed plan/pass records
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "submit_planner_pass_plan",
		"arguments": map[string]interface{}{
			"planner_pass_plan_json": smokePlanFixture(),
			"source":                 "mcp_smoke_test",
			"unmanaged_acknowledged": true,
		},
	})
	if err != nil {
		return h.fatal("submit_planner_pass_plan", err)
	}
	if resp.Error != nil {
		return h.fatal("submit_planner_pass_plan", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var planResult toolCallResult
	if err := json.Unmarshal(resp.Result, &planResult); err != nil {
		return h.fatal("submit_planner_pass_plan parse", err)
	}
	h.check("submit_planner_pass_plan !isError", !planResult.IsError)

	var smokePlanID string
	if len(planResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(planResult.Content[0].Text), &out); err == nil {
			h.check("submit_planner_pass_plan ok=true", out["ok"] == true)
			h.check("submit_planner_pass_plan tool=submit_planner_pass_plan", out["tool"] == "submit_planner_pass_plan")
			if planID, ok := out["plan_id"].(string); ok {
				smokePlanID = planID
				h.check("submit_planner_pass_plan plan_id=mcp-smoke-plan", planID == "mcp-smoke-plan")
			}
			if passCount, ok := out["pass_count"].(float64); ok {
				h.check("submit_planner_pass_plan pass_count=2", int(passCount) == 2)
			}
			foundPassIDs := map[string]bool{}
			if passes, ok := out["passes"].([]interface{}); ok {
				for _, p := range passes {
					if pm, ok := p.(map[string]interface{}); ok {
						if pid, ok := pm["pass_id"].(string); ok {
							foundPassIDs[pid] = true
						}
					}
				}
			}
			h.check("submit_planner_pass_plan has PASS-001", foundPassIDs["PASS-001"])
			h.check("submit_planner_pass_plan has PASS-002", foundPassIDs["PASS-002"])
		}
	}

	if smokePlanID == "" {
		return fmt.Errorf("submit_planner_pass_plan did not return a plan_id; cannot continue smoke test")
	}
	fmt.Printf("  created plan_id: %s\n", smokePlanID)
	if h.dbPath != "" {
		if err := seedSmokeContextPacket(h.dbPath, smokePlanID, "PASS-001", "packet-smoke-plan-pass-001", "snap-smoke-base", "created", "2026-06-28T00:05:00Z"); err != nil {
			return h.fatal("seed smoke context packet", err)
		}
	}

	// -------------------------------------------------------
	// 6. get_next_pass_work - verify actionable structuredContent for selected pass
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "get_next_pass_work",
		"arguments": map[string]interface{}{
			"project_id": "relay",
			"plan_id":    smokePlanID,
		},
	})
	if err != nil {
		return h.fatal("get_next_pass_work actionable", err)
	}
	if resp.Error != nil {
		return h.fatal("get_next_pass_work actionable", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}
	var actionableNextPassWorkResult toolCallResult
	if err := json.Unmarshal(resp.Result, &actionableNextPassWorkResult); err != nil {
		return h.fatal("get_next_pass_work actionable parse", err)
	}
	h.check("get_next_pass_work actionable !isError", !actionableNextPassWorkResult.IsError)
	if out := actionableNextPassWorkResult.StructuredContent; out != nil {
		h.check("get_next_pass_work actionable ok=true", out["ok"] == true)
		h.check("get_next_pass_work readiness handoff authoring", out["readiness_state"] == "ready_for_handoff_authoring")
		if packet, ok := out["handoff_work"].(map[string]interface{}); ok {
			h.check("get_next_pass_work handoff_work plan_id", packet["plan_id"] == smokePlanID)
			h.check("get_next_pass_work handoff_work pass_id", packet["pass_id"] == "PASS-001")
			h.check("get_next_pass_work handoff_work action", packet["suggested_authoring_action"] == "draft_planner_handoff")
		} else {
			h.check("get_next_pass_work handoff_work present", false)
		}
		if actions, ok := out["next_actions"].([]interface{}); ok && len(actions) > 0 {
			if action, ok := actions[0].(map[string]interface{}); ok {
				h.check("get_next_pass_work next_action has tool", action["tool"] == "draft_planner_handoff")
				if args, ok := action["arguments"].(map[string]interface{}); ok {
					h.check("get_next_pass_work next_action plan_id", args["plan_id"] == smokePlanID)
					h.check("get_next_pass_work next_action pass_id", args["pass_id"] == "PASS-001")
				} else {
					h.check("get_next_pass_work next_action arguments object", false)
				}
				h.check("get_next_pass_work next_action not run submission", action["tool"] != "create_run_from_planner_handoff")
			}
		} else {
			h.check("get_next_pass_work next_actions present", false)
		}
	}

	// -------------------------------------------------------
	// 6. create_run_from_planner_handoff — create pass-associated run
	// -------------------------------------------------------
	handoffMarkdown := `---
title: Smoke Test Handoff
repo_target: smoke-test-repo
branch_context: main
---

<compiler_input>
` + "```yaml" + `
compiler_input:
  goal: Test.
  scope: Test.
  file_targets:
    - path: test.go
  implementation_steps:
    - id: S1
      title: Step
      action: modify
      instructions: Run.
  code_requirements:
    - id: CR1
      requirement: Test.
  validation_contract:
    mode: commands
    failure_policy: block
  completion_contract:
    done_when:
      - Done.
` + "```" + `
</compiler_input>

# Smoke Test Handoff

This is a synthetic handoff created by the managed-plan MCP smoke harness.

## Context

Validates that create_run_from_planner_handoff can associate a new run to a plan pass.
`
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "create_run_from_planner_handoff",
		"arguments": map[string]interface{}{
			"planner_handoff_markdown": handoffMarkdown,
			"repo_target":              "smoke-test-repo",
			"branch_context":           "main",
			"source":                   "mcp_smoke_test",
			"plan_id":                  smokePlanID,
			"pass_id":                  "PASS-001",
			"source_snapshot_id":       "snap-smoke-base",
		},
	})
	if err != nil {
		return h.fatal("create_run_from_planner_handoff", err)
	}
	if resp.Error != nil {
		return h.fatal("create_run_from_planner_handoff", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var createResult toolCallResult
	if err := json.Unmarshal(resp.Result, &createResult); err != nil {
		return h.fatal("create_run_from_planner_handoff parse", err)
	}
	h.check("create_run_from_planner_handoff !isError", !createResult.IsError)

	var createdRunID string
	if len(createResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(createResult.Content[0].Text), &out); err == nil {
			h.check("create_run_from_planner_handoff ok=true", out["ok"] == true)
			if runID, ok := out["run_id"].(float64); ok {
				createdRunID = strconv.FormatInt(int64(runID), 10)
				h.check("create_run_from_planner_handoff run_id non-zero", int64(runID) > 0)
			}
			h.check("create_run_from_planner_handoff has status", out["status"] != nil && out["status"] != "")
			h.check("create_run_from_planner_handoff plan_id returned", out["plan_id"] == smokePlanID)
			h.check("create_run_from_planner_handoff pass_id returned", out["pass_id"] == "PASS-001")
		}
	}

	if createdRunID == "" {
		return fmt.Errorf("create_run_from_planner_handoff did not return a run_id; cannot continue smoke test")
	}
	fmt.Printf("  created run_id: %s\n", createdRunID)

	// -------------------------------------------------------
	// 6b. create_run_from_planner_handoff_file — create standalone run from exact file bytes
	// -------------------------------------------------------
	if h.dbPath != "" {
		fileHandoffBytes := []byte("---\ntitle: Smoke Test File Handoff\nrepo_target: smoke-test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Smoke Test File Handoff\n\nThis synthetic handoff validates exact file-byte MCP run submission.\n")
		fileSum := sha256.Sum256(fileHandoffBytes)
		fileSHA := hex.EncodeToString(fileSum[:])
		fileDir, err := os.MkdirTemp("", "relay-mcp-smoke-handoff-*")
		if err != nil {
			return h.fatal("create_run_from_planner_handoff_file tempdir", err)
		}
		defer os.RemoveAll(fileDir)
		filePath := filepath.Join(fileDir, "reviewed-handoff.md")
		if err := os.WriteFile(filePath, fileHandoffBytes, 0644); err != nil {
			return h.fatal("create_run_from_planner_handoff_file write fixture", err)
		}

		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "create_run_from_planner_handoff_file",
			"arguments": map[string]interface{}{
				"planner_handoff_file": filePath,
				"expected_sha256":      fileSHA,
				"repo_target":          "smoke-test-repo",
				"branch_context":       "main",
				"source":               "mcp_smoke_test_file",
			},
		})
		if err != nil {
			return h.fatal("create_run_from_planner_handoff_file", err)
		}
		if resp.Error != nil {
			return h.fatal("create_run_from_planner_handoff_file", fmt.Errorf("RPC error: %s", resp.Error.Message))
		}

		var createFileResult toolCallResult
		if err := json.Unmarshal(resp.Result, &createFileResult); err != nil {
			return h.fatal("create_run_from_planner_handoff_file parse", err)
		}
		h.check("create_run_from_planner_handoff_file !isError", !createFileResult.IsError)
		if len(createFileResult.Content) > 0 {
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(createFileResult.Content[0].Text), &out); err == nil {
				h.check("create_run_from_planner_handoff_file ok=true", out["ok"] == true)
				h.check("create_run_from_planner_handoff_file submitted SHA", out["submitted_handoff_sha256"] == fileSHA)
				h.check("create_run_from_planner_handoff_file sha_match=true", out["sha_match"] == true)
				h.check("create_run_from_planner_handoff_file source_mode", out["source_mode"] == "file_parameter")
				if runID, ok := out["run_id"].(float64); ok {
					h.check("create_run_from_planner_handoff_file run_id non-zero", int64(runID) > 0)
				}
			}
		}
	}

	// -------------------------------------------------------
	// 6c. get_run_status — verify bounded snapshot for created run
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name":      "get_run_status",
		"arguments": map[string]string{"run_id": createdRunID},
	})
	if err != nil {
		return h.fatal("get_run_status", err)
	}
	if resp.Error != nil {
		return h.fatal("get_run_status", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var statusResult toolCallResult
	if err := json.Unmarshal(resp.Result, &statusResult); err != nil {
		return h.fatal("get_run_status parse", err)
	}
	h.check("get_run_status !isError", !statusResult.IsError)
	if len(statusResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(statusResult.Content[0].Text), &out); err == nil {
			h.check("get_run_status ok=true", out["ok"] == true)
			h.check("get_run_status has run_id", out["run_id"] == createdRunID)
			h.check("get_run_status has status", out["status"] != nil)
			h.check("get_run_status has lifecycle_state", out["lifecycle_state"] != nil)
		}
	}

	// -------------------------------------------------------
	// 7. list_open_runs — verify created run appears
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name":      "list_open_runs",
		"arguments": map[string]interface{}{"limit": 25},
	})
	if err != nil {
		return h.fatal("list_open_runs", err)
	}
	if resp.Error != nil {
		return h.fatal("list_open_runs", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var listResult toolCallResult
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		return h.fatal("list_open_runs parse", err)
	}
	h.check("list_open_runs !isError", !listResult.IsError)
	if len(listResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(listResult.Content[0].Text), &out); err == nil {
			h.check("list_open_runs ok=true", out["ok"] == true)
			found := false
			if runs, ok := out["runs"].([]interface{}); ok {
				for _, r := range runs {
					if rm, ok := r.(map[string]interface{}); ok {
						if rm["run_id"] == createdRunID {
							found = true
							break
						}
					}
				}
			}
			h.check("list_open_runs contains created run", found)
		}
	}

	// -------------------------------------------------------
	// 8. submit_audit_packet — exercise against created run
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "submit_audit_packet",
		"arguments": map[string]interface{}{
			"run_id":                createdRunID,
			"audit_packet_markdown": "# MCP Smoke Audit\n\nThis audit was submitted by the Pass 16 smoke harness.",
			"decision":              "revision_required",
			"notes":                 "Smoke test submission",
		},
	})
	if err != nil {
		return h.fatal("submit_audit_packet", err)
	}
	if resp.Error != nil {
		return h.fatal("submit_audit_packet", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var auditResult toolCallResult
	if err := json.Unmarshal(resp.Result, &auditResult); err != nil {
		return h.fatal("submit_audit_packet parse", err)
	}

	// The smoke test creates a run but doesn't execute it, so audit submission
	// will fail with STATE_ERROR. This is expected behavior since audits require
	// the run to be in audit_ready status (which requires executor completion).
	// We check that we get a proper error response (not a crash).
	if auditResult.IsError {
		h.check("submit_audit_packet returns structured error", len(auditResult.Content) > 0)
		// Skip the remaining audit checks since the run isn't audit-ready
	} else {
		h.check("submit_audit_packet !isError", !auditResult.IsError)
		if len(auditResult.Content) > 0 {
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(auditResult.Content[0].Text), &out); err == nil {
				h.check("submit_audit_packet ok=true", out["ok"] == true)
				h.check("submit_audit_packet decision=revision_required", out["decision"] == "revision_required")
				h.check("submit_audit_packet status=revision_required", out["status"] == "revision_required")
			}
		}
	}

	// -------------------------------------------------------
	// 9. get_next_pass_work — verify structured blocker for unknown project
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "get_next_pass_work",
		"arguments": map[string]interface{}{
			"project_id": "nonexistent-project",
			"plan_id":    "nonexistent-plan",
		},
	})
	if err != nil {
		return h.fatal("get_next_pass_work", err)
	}
	if resp.Error != nil {
		return h.fatal("get_next_pass_work", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var nextPassWorkResult toolCallResult
	if err := json.Unmarshal(resp.Result, &nextPassWorkResult); err != nil {
		return h.fatal("get_next_pass_work parse", err)
	}
	h.check("get_next_pass_work isError=true for unknown project", nextPassWorkResult.IsError)
	out := nextPassWorkResult.StructuredContent
	if out != nil {
		h.check("get_next_pass_work ok=false", out["ok"] == false)
		h.check("get_next_pass_work tool=get_next_pass_work", out["tool"] == "get_next_pass_work")
		if blockers, ok := out["blockers"].([]interface{}); ok && len(blockers) > 0 {
			if blocker, ok := blockers[0].(map[string]interface{}); ok {
				code := blocker["code"]
				h.check("get_next_pass_work blocker is unknown_resource or unknown_project", code == "unknown_resource" || code == "unknown_project")
			}
		}
	}

	// -------------------------------------------------------
	// 10. get_next_audit_work — verify structured blocker for unknown project
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "get_next_audit_work",
		"arguments": map[string]interface{}{
			"project_id": "nonexistent-project",
			"plan_id":    "nonexistent-plan",
		},
	})
	if err != nil {
		return h.fatal("get_next_audit_work", err)
	}
	if resp.Error != nil {
		return h.fatal("get_next_audit_work", fmt.Errorf("RPC error: %s", resp.Error.Message))
	}

	var nextAuditWorkResult toolCallResult
	if err := json.Unmarshal(resp.Result, &nextAuditWorkResult); err != nil {
		return h.fatal("get_next_audit_work parse", err)
	}
	h.check("get_next_audit_work isError=true for unknown project", nextAuditWorkResult.IsError)
	if len(nextAuditWorkResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(nextAuditWorkResult.Content[0].Text), &out); err == nil {
			h.check("get_next_audit_work ok=false", out["ok"] == false)
			h.check("get_next_audit_work tool=get_next_audit_work", out["tool"] == "get_next_audit_work")
			if blockers, ok := out["blockers"].([]interface{}); ok && len(blockers) > 0 {
				if blocker, ok := blockers[0].(map[string]interface{}); ok {
					h.check("get_next_audit_work blocker code=unknown_project", blocker["code"] == "unknown_project")
				}
			}
		}
	}

	// -------------------------------------------------------
	// 11. Unknown tool — verify error response
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name":      "nonexistent_tool_xyz",
		"arguments": map[string]interface{}{},
	})
	if err != nil {
		return h.fatal("unknown tool call", err)
	}
	h.check("unknown tool returns error", resp.Error != nil)

	// -------------------------------------------------------
	// 12. Invalid decision — verify tool-level error, no status mutation
	// -------------------------------------------------------
	resp, err = h.call("tools/call", map[string]interface{}{
		"name": "submit_audit_packet",
		"arguments": map[string]interface{}{
			"run_id":                createdRunID,
			"audit_packet_markdown": "# Bad decision test",
			"decision":              "auto_approve",
		},
	})
	if err != nil {
		return h.fatal("invalid decision call", err)
	}
	// Should get a successful RPC response with isError=true in the tool result.
	var badDecisionResult toolCallResult
	if resp.Error == nil {
		if err := json.Unmarshal(resp.Result, &badDecisionResult); err == nil {
			h.check("invalid decision isError=true", badDecisionResult.IsError)
			if len(badDecisionResult.Content) > 0 {
				h.check("invalid decision mentions VALIDATION_ERROR",
					strings.Contains(badDecisionResult.Content[0].Text, "VALIDATION_ERROR"))
			}
		}
	} else {
		// An RPC-level error is also acceptable.
		h.check("invalid decision error returned", true)
	}

	// -------------------------------------------------------
	// 13. Context packet usability regression checks (stdio-only)
	// -------------------------------------------------------
	if h.dbPath != "" {
		fmt.Println("Running context packet usability checks...")
		// Submit the required-context plan
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "submit_planner_pass_plan",
			"arguments": map[string]interface{}{
				"planner_pass_plan_json": smokePlanRequiredContextFixture(),
				"source":                 "mcp_smoke_test_usability",
				"unmanaged_acknowledged": true,
			},
		})
		if err != nil {
			return h.fatal("submit_planner_pass_plan usability", err)
		}
		var planUsabilityResult toolCallResult
		if err := json.Unmarshal(resp.Result, &planUsabilityResult); err != nil {
			return h.fatal("submit_planner_pass_plan usability parse", err)
		}
		h.check("submit_planner_pass_plan usability !isError", !planUsabilityResult.IsError)

		// Open store to seed snapshot and context packet
		discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
		dbStore, err := store.Open(h.dbPath, discardLogger)
		if err != nil {
			return h.fatal("open store for seeding usability", err)
		}

		proj, err := dbStore.GetProjectByProjectID("relay")
		if err != nil {
			dbStore.Close()
			return h.fatal("usability lookup project", err)
		}

		// Seed source snapshot "snap-smoke-1"
		snap, err := dbStore.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
			SourceSnapshotID: "snap-smoke-1",
			ProjectRowID:     proj.ID,
			ProjectID:        proj.ProjectID,
			SnapshotKind:     "clean_commit",
			Status:           "created",
			CompletedAt:      "2026-06-28T00:00:00Z",
			SummaryJSON:      "{}",
		})
		if err != nil {
			dbStore.Close()
			return h.fatal("usability seed snapshot", err)
		}

		// Seed unusable context packet (status blocked)
		_, err = dbStore.CreateContextPacket(store.CreateContextPacketParams{
			ContextPacketID:     "packet-smoke-unusable",
			ProjectRowID:        proj.ID,
			ProjectID:           proj.ProjectID,
			PlanID:              "mcp-smoke-plan-req-ctx",
			PassID:              "PASS-001",
			TaskSlug:            "slug",
			SourceSnapshotRowID: snap.ID,
			SourceSnapshotID:    "snap-smoke-1",
			Status:              "blocked",
			BlockedSeedCount:    0,
			MissingSeedCount:    0,
			CompletedAt:         "2026-06-28T12:00:00Z",
			PacketJSONPath:      "/artifacts/context/packet-smoke-unusable.json",
			CoverageReportPath:  "/artifacts/context/packet-smoke-unusable-coverage.json",
		})
		if err != nil {
			dbStore.Close()
			return h.fatal("usability seed unusable packet", err)
		}
		dbStore.Close()

		// Call get_next_pass_work and assert blocked
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "get_next_pass_work",
			"arguments": map[string]interface{}{
				"project_id": "relay",
				"plan_id":    "mcp-smoke-plan-req-ctx",
			},
		})
		if err != nil {
			return h.fatal("get_next_pass_work usability blocked", err)
		}
		var mcpBlockedResult toolCallResult
		if err := json.Unmarshal(resp.Result, &mcpBlockedResult); err != nil {
			return h.fatal("get_next_pass_work usability blocked parse", err)
		}
		h.check("get_next_pass_work usability blocked isError=true", mcpBlockedResult.IsError)
		out := mcpBlockedResult.StructuredContent
		if out != nil {
			h.check("get_next_pass_work usability blocked ok=false", out["ok"] == false)
			h.check("get_next_pass_work usability blocked readiness state present", out["readiness_state"] != nil && out["readiness_state"] != "" || out["blockers"] != nil)
			h.check("get_next_pass_work usability blocked handoff_work is nil", out["handoff_work"] == nil)
			h.check("get_next_pass_work usability blocked has failure report", out["acquisition_failure_report"] != nil || out["blockers"] != nil)

			// Backend retry exhaustion should not route callers to manual packet creation.
			if nextActions, ok := out["next_actions"].([]interface{}); ok {
				for _, act := range nextActions {
					if m, ok := act.(map[string]interface{}); ok {
						if m["tool"] == "create_context_packet" {
							h.failf("did not expect create_context_packet tool action after backend retry failure")
						}
						if m["tool"] == "draft_planner_handoff" {
							h.failf("did not expect draft_planner_handoff tool action when blocked")
						}
					}
				}
			}
		} else {
			h.check("get_next_pass_work usability blocked structuredContent present", false)
		}

		// Open store again to write usable context packet (status created)
		dbStore, err = store.Open(h.dbPath, discardLogger)
		if err != nil {
			return h.fatal("open store for seeding usability usable", err)
		}
		_, err = dbStore.CreateContextPacket(store.CreateContextPacketParams{
			ContextPacketID:     "packet-smoke-usable",
			ProjectRowID:        proj.ID,
			ProjectID:           proj.ProjectID,
			PlanID:              "mcp-smoke-plan-req-ctx",
			PassID:              "PASS-001",
			TaskSlug:            "slug",
			SourceSnapshotRowID: snap.ID,
			SourceSnapshotID:    "snap-smoke-1",
			Status:              "created",
			BlockedSeedCount:    0,
			MissingSeedCount:    0,
			CompletedAt:         "2026-06-28T13:00:00Z", // later makes it latest
			PacketJSONPath:      "/artifacts/context/packet-smoke-usable.json",
			CoverageReportPath:  "/artifacts/context/packet-smoke-usable-coverage.json",
		})
		if err != nil {
			dbStore.Close()
			return h.fatal("usability seed usable packet", err)
		}
		dbStore.Close()

		// Call get_next_pass_work and assert ready
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "get_next_pass_work",
			"arguments": map[string]interface{}{
				"project_id": "relay",
				"plan_id":    "mcp-smoke-plan-req-ctx",
			},
		})
		if err != nil {
			return h.fatal("get_next_pass_work usability ready", err)
		}
		var mcpReadyResult toolCallResult
		if err := json.Unmarshal(resp.Result, &mcpReadyResult); err != nil {
			return h.fatal("get_next_pass_work usability ready parse", err)
		}
		h.check("get_next_pass_work usability ready !isError", !mcpReadyResult.IsError)
		out = mcpReadyResult.StructuredContent
		if out != nil {
			h.check("get_next_pass_work usability ready ok=true", out["ok"] == true)
			h.check("get_next_pass_work usability ready readiness state", out["readiness_state"] == "ready_for_handoff_authoring")
			h.check("get_next_pass_work usability ready handoff_work non-nil", out["handoff_work"] != nil)

			var foundDraft bool
			if nextActions, ok := out["next_actions"].([]interface{}); ok {
				for _, act := range nextActions {
					if m, ok := act.(map[string]interface{}); ok {
						if m["tool"] == "draft_planner_handoff" {
							foundDraft = true
						}
					}
				}
			}
			h.check("get_next_pass_work usability actions has draft_planner_handoff", foundDraft)
		} else {
			h.check("get_next_pass_work usability ready structuredContent present", false)
		}
	}

	// -------------------------------------------------------
	// 14. Streamlined workflow smoke — repository resolution, freshness,
	//     required context bundle, prepared handoff context, bounded
	//     artifact readback, compile preflight, and exact file submission.
	// -------------------------------------------------------
	if h.repoRoot != "" {
		fmt.Println("Running streamlined workflow smoke...")

		// Ensure the git fixture is properly registered as a project repository.
		discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
		dbStore, err := store.Open(h.dbPath, discardLogger)
		if err != nil {
			return h.fatal("open store for streamlined smoke", err)
		}
		proj, pErr := dbStore.GetProjectByProjectID("relay")
		if pErr != nil {
			dbStore.Close()
			return h.fatal("lookup project for streamlined smoke", pErr)
		}
		_, repoErr := dbStore.UpsertProjectRepository(store.UpsertProjectRepositoryParams{
			ProjectRowID:     proj.ID,
			RepoID:           "smoke-test-repo",
			Role:             "primary",
			LocalPath:        h.repoRoot,
			DefaultBranch:    "main",
			AllowedRootsJSON: `["."]`,
			MaxFileSizeBytes: 1048576,
			Enabled:          1,
		})
		if repoErr != nil {
			dbStore.Close()
			return h.fatal("upsert project repo for streamlined smoke", repoErr)
		}
		dbStore.Close()

		// 14a. resolve_project_repository with canonical ID and accepted alias.
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "resolve_project_repository",
			"arguments": map[string]interface{}{
				"project_id": "relay",
				"repo_id":    "smoke-test-repo",
			},
		})
		if err != nil {
			return h.fatal("resolve_project_repository canonical", err)
		}
		if resp.Error != nil {
			return h.fatal("resolve_project_repository canonical", fmt.Errorf("RPC error: %s", resp.Error.Message))
		}
		var resolveRes toolCallResult
		if err := json.Unmarshal(resp.Result, &resolveRes); err != nil {
			return h.fatal("resolve_project_repository parse", err)
		}
		h.check("resolve_project_repository canonical !isError", !resolveRes.IsError)
		var resolveContent map[string]interface{}
		if len(resolveRes.Content) > 0 {
			if err := json.Unmarshal([]byte(resolveRes.Content[0].Text), &resolveContent); err == nil {
				h.check("resolve_project_repository canonical ok=true", resolveContent["ok"] == true)
				if result, ok := resolveContent["result"].(map[string]interface{}); ok {
					h.check("resolve_project_repository canonical_repo_id", result["canonical_repo_id"] == "smoke-test-repo")
				}
			}
		}
		rawResolve, _ := json.Marshal(resolveRes)
		resolveText := strings.ToLower(string(rawResolve))
		h.check("resolve_project_repository no local path leak", !strings.Contains(resolveText, "local_path") && !strings.Contains(strings.ToLower(h.repoRoot), "relay-smoke-git") || !strings.Contains(resolveText, strings.ToLower(h.repoRoot)))

		// 14b. create_source_snapshot and assert structured freshness_report.
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "create_source_snapshot",
			"arguments": map[string]interface{}{
				"project_id":            "relay",
				"repo_ids":              []string{"smoke-test-repo"},
				"include_file_metadata": true,
				"max_files_per_repo":    200,
			},
		})
		if err != nil {
			return h.fatal("create_source_snapshot streamlined", err)
		}
		if resp.Error != nil {
			return h.fatal("create_source_snapshot streamlined", fmt.Errorf("RPC error: %s", resp.Error.Message))
		}
		var snapRes toolCallResult
		if err := json.Unmarshal(resp.Result, &snapRes); err != nil {
			return h.fatal("create_source_snapshot parse", err)
		}
		h.check("create_source_snapshot streamlined !isError", !snapRes.IsError)
		var snapContent map[string]interface{}
		if len(snapRes.Content) > 0 {
			if err := json.Unmarshal([]byte(snapRes.Content[0].Text), &snapContent); err == nil {
				h.check("create_source_snapshot ok=true", snapContent["ok"] == true)
			}
		}
		// Freshness report is inside the result envelope.
		if snapResult, ok := snapContent["result"].(map[string]interface{}); ok {
			if freshReport, ok := snapResult["freshness_report"].(map[string]interface{}); ok {
				h.check("create_source_snapshot freshness status=fresh", freshReport["status"] == "fresh")
				h.check("create_source_snapshot reusable_for_handoff=true", freshReport["reusable_for_handoff"] == true)
				h.check("create_source_snapshot freshness source_snapshot_id", freshReport["source_snapshot_id"] != nil)
			} else {
				h.check("create_source_snapshot freshness_report present", false)
			}
		} else {
			h.check("create_source_snapshot freshness_report present", false)
		}
		rawSnap, _ := json.Marshal(snapRes)
		h.check("create_source_snapshot no local path leak", !strings.Contains(string(rawSnap), "local_path") && !strings.Contains(string(rawSnap), h.repoRoot))

		// 14c. create_run_from_planner_handoff_file — exact file submission with matching hash.
		fileHandoffBytes := []byte("---\ntitle: Streamlined Smoke File Handoff\nrepo_target: smoke-test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Streamlined Smoke Exact File Handoff\n\nValidates exact file-byte provenance for the streamlined workflow smoke.\n")
		fileSum := sha256.Sum256(fileHandoffBytes)
		fileSHA := hex.EncodeToString(fileSum[:])
		fileDir, err := os.MkdirTemp("", "relay-smoke-handoff-*")
		if err != nil {
			return h.fatal("streamlined handoff tempdir", err)
		}
		defer os.RemoveAll(fileDir)
		filePath := filepath.Join(fileDir, "reviewed-handoff.md")
		if err := os.WriteFile(filePath, fileHandoffBytes, 0644); err != nil {
			return h.fatal("streamlined handoff write fixture", err)
		}
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "create_run_from_planner_handoff_file",
			"arguments": map[string]interface{}{
				"planner_handoff_file": filePath,
				"expected_sha256":      fileSHA,
				"repo_target":          "smoke-test-repo",
				"branch_context":       "main",
				"source":               "mcp_smoke_streamlined",
			},
		})
		if err != nil {
			return h.fatal("create_run_from_planner_handoff_file streamlined", err)
		}
		if resp.Error != nil {
			return h.fatal("create_run_from_planner_handoff_file streamlined", fmt.Errorf("RPC error: %s", resp.Error.Message))
		}
		var createFileRes toolCallResult
		if err := json.Unmarshal(resp.Result, &createFileRes); err != nil {
			return h.fatal("create_run_from_planner_handoff_file parse", err)
		}
		h.check("create_run_from_planner_handoff_file streamlined !isError", !createFileRes.IsError)
		if len(createFileRes.Content) > 0 {
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(createFileRes.Content[0].Text), &out); err == nil {
				h.check("create_run_from_planner_handoff_file streamlined ok=true", out["ok"] == true)
				h.check("create_run_from_planner_handoff_file streamlined sha_match=true", out["sha_match"] == true)
				h.check("create_run_from_planner_handoff_file streamlined source_mode=file_parameter", out["source_mode"] == "file_parameter")
				h.check("create_run_from_planner_handoff_file streamlined provenance sha", out["provenance"] != nil)
			}
		}

		// 14d. create_run_from_planner_handoff_file — hash mismatch blocks before durable writes.
		var mismatchFileBytes = []byte("---\ntitle: Streamlined Mismatch Handoff\nrepo_target: smoke-test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Mismatch\n")
		mismatchPath := filepath.Join(fileDir, "mismatch-handoff.md")
		if err := os.WriteFile(mismatchPath, mismatchFileBytes, 0644); err != nil {
			return h.fatal("mismatch handoff write fixture", err)
		}
		wrongSHA := strings.Repeat("00", 32)
		// Count rows before the call.
		rowsBefore := countSmokeRows(h.dbPath)
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "create_run_from_planner_handoff_file",
			"arguments": map[string]interface{}{
				"planner_handoff_file": mismatchPath,
				"expected_sha256":      wrongSHA,
				"repo_target":          "smoke-test-repo",
				"source":               "mcp_smoke_mismatch",
			},
		})
		if err != nil {
			return h.fatal("create_run_from_planner_handoff_file mismatch", err)
		}
		var mismatchRes toolCallResult
		if err := json.Unmarshal(resp.Result, &mismatchRes); err != nil {
			return h.fatal("mismatch parse", err)
		}
		h.check("mismatch submission isError=true", mismatchRes.IsError)
		rowsAfter := countSmokeRows(h.dbPath)
		h.check("mismatch no new runs", rowsAfter["runs"] == rowsBefore["runs"])
		h.check("mismatch no new artifacts", rowsAfter["artifacts"] == rowsBefore["artifacts"])
		h.check("mismatch no new provenance", rowsAfter["run_submission_provenance"] == rowsBefore["run_submission_provenance"])
		h.check("mismatch no new events", rowsAfter["events"] == rowsBefore["events"])
		if mismatchRes.StructuredContent != nil {
			if scBlockers, ok := mismatchRes.StructuredContent["blockers"].([]interface{}); ok && len(scBlockers) > 0 {
				if firstBlocker, ok := scBlockers[0].(map[string]interface{}); ok {
					h.check("mismatch blocker code=expected_hash_mismatch", firstBlocker["code"] == "expected_hash_mismatch")
					h.check("mismatch blocker recoverable=true", firstBlocker["recoverable"] == true)
				}
			}
		}
		mismatchJSON, _ := json.Marshal(mismatchRes)
		mismatchText := string(mismatchJSON)
		h.check("mismatch evidence no local path", !strings.Contains(mismatchText, fileDir) && !strings.Contains(mismatchText, mismatchPath))

		// 14e. validate_planner_handoff_for_compile — valid fixture returns compile-ready.
		validHandoffMarkdown := "---\ntitle: Streamlined Validation Handoff\nrepo_target: smoke-test-repo\nbranch_context: main\n---\n\n<compiler_input>\n```yaml\ncompiler_input:\n  goal: Test.\n  scope: Test.\n  file_targets:\n    - path: test.go\n  implementation_steps:\n    - id: S1\n      title: Step\n      action: modify\n      instructions: Run.\n  code_requirements:\n    - id: CR1\n      requirement: Test.\n  validation_contract:\n    mode: commands\n    failure_policy: block\n  completion_contract:\n    done_when:\n      - Done.\n```\n</compiler_input>\n\n# Streamlined Validation Handoff\n\nCompile-ready.\n"
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "validate_planner_handoff_for_compile",
			"arguments": map[string]interface{}{
				"planner_handoff_markdown": validHandoffMarkdown,
				"repo_target":              "smoke-test-repo",
			},
		})
		if err != nil {
			return h.fatal("validate_planner_handoff_for_compile valid", err)
		}
		if resp.Error != nil {
			return h.fatal("validate_planner_handoff_for_compile valid", fmt.Errorf("RPC error: %s", resp.Error.Message))
		}
		var validateRes toolCallResult
		if err := json.Unmarshal(resp.Result, &validateRes); err != nil {
			return h.fatal("validate_planner_handoff_for_compile parse", err)
		}
		h.check("validate_planner_handoff_for_compile valid !isError", !validateRes.IsError)
		if sc, ok := validateRes.StructuredContent["ok"].(bool); ok {
			h.check("validate_planner_handoff_for_compile valid ok=true", sc)
		}
		if isReady, ok := validateRes.StructuredContent["is_compile_ready"].(bool); ok {
			h.check("validate_planner_handoff_for_compile valid is_compile_ready=true", isReady)
		}
		h.check("validate_planner_handoff_for_compile valid has submitted_handoff_sha256", validateRes.StructuredContent["submitted_handoff_sha256"] != nil)
		h.check("validate_planner_handoff_for_compile valid has byte_count", validateRes.StructuredContent["byte_count"] != nil)

		// 14f. validate_planner_handoff_for_compile — malformed fixture returns blocked.
		rowsBeforePreflight := countSmokeRows(h.dbPath)
		resp, err = h.call("tools/call", map[string]interface{}{
			"name": "validate_planner_handoff_for_compile",
			"arguments": map[string]interface{}{
				"planner_handoff_markdown": "---\nbroken frontmatter: missing closing dash\n",
				"repo_target":              "smoke-test-repo",
			},
		})
		if err != nil {
			return h.fatal("validate_planner_handoff_for_compile malformed", err)
		}
		var malformedRes toolCallResult
		if err := json.Unmarshal(resp.Result, &malformedRes); err != nil {
			return h.fatal("malformed parse", err)
		}
		h.check("validate_planner_handoff_for_compile malformed isError=true", malformedRes.IsError)
		rowsAfterPreflight := countSmokeRows(h.dbPath)
		h.check("malformed preflight no new runs", rowsAfterPreflight["runs"] == rowsBeforePreflight["runs"])
		h.check("malformed preflight no new artifacts", rowsAfterPreflight["artifacts"] == rowsBeforePreflight["artifacts"])
		h.check("malformed preflight no new provenance", rowsAfterPreflight["run_submission_provenance"] == rowsBeforePreflight["run_submission_provenance"])
		h.check("malformed preflight no new events", rowsAfterPreflight["events"] == rowsBeforePreflight["events"])
		if sc, ok := malformedRes.StructuredContent["status"]; ok {
			h.check("malformed preflight status=blocked", sc == "blocked")
		}
	}

	// -------------------------------------------------------
	// Summary
	// -------------------------------------------------------
	fmt.Printf("\n=== MCP Smoke Results ===\n")
	fmt.Printf("PASS: %d\n", h.pass)
	fmt.Printf("FAIL: %d\n", h.fail)

	if h.fail > 0 {
		return fmt.Errorf("%d check(s) failed", h.fail)
	}
	fmt.Println("ALL CHECKS PASSED")
	return nil
}

func countSmokeRows(dbPath string) map[string]int {
	out := map[string]int{}
	dbStore, err := store.Open(dbPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return out
	}
	defer dbStore.Close()
	for _, table := range []string{"runs", "artifacts", "run_submission_provenance", "events"} {
		var count int
		if err := dbStore.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err == nil {
			out[table] = count
		}
	}
	return out
}

func smokePlanFixture() string {
	return `{
  "plan_meta": {
    "plan_id": "mcp-smoke-plan",
    "schema_version": "2.0.0",
    "created_at": "2026-06-21T00:00:00Z",
    "project_id": "relay",
    "title": "MCP Smoke Managed Plan",
    "goal": "Verify managed plan MCP submission smoke coverage.",
    "repo_target": "smoke-test-repo",
    "branch_context": "main",
    "status": "active"
  },
  "source_intent": {
    "summary": "Synthetic smoke plan for managed-plan MCP coverage."
  },
  "passes": [
    {
      "pass_id": "PASS-001",
      "sequence": 1,
      "name": "Smoke pass one",
      "goal": "Create a pass-associated smoke run.",
      "pass_type": "backend_vertical_slice",
      "intended_execution_scope": ["cmd/mcp-smoke/main.go"],
      "non_goals": ["No production data mutation"],
      "dependencies": [],
      "context_plan": {
        "required_repositories": ["smoke-test-repo"],
        "context_coverage_expectations": ["basic_smoke_coverage"],
        "blocked_if_missing": ["none"],
        "seed_files_to_read": [
          {
            "repo_id": "smoke-test-repo",
            "path": "cmd/mcp-smoke/main.go",
            "purpose": "smoke test entry point",
            "required": false
          }
        ],
        "seed_search_terms": [
          {
            "repo_id": "smoke-test-repo",
            "query": "smoke",
            "purpose": "locate smoke test code",
            "required": false
          }
        ]
      },
      "source_snapshot_requirements": {
        "require_git_status": false,
        "require_commit_sha": false,
        "allow_dirty_worktree": true
      },
      "handoff_readiness_criteria": ["smoke_test_pass_ready"],
      "status": "planned"
    },
    {
      "pass_id": "PASS-002",
      "sequence": 2,
      "name": "Smoke pass two",
      "goal": "Provide dependency coverage.",
      "pass_type": "documentation",
      "intended_execution_scope": ["docs/mcp.md"],
      "non_goals": ["No UI changes"],
      "dependencies": ["PASS-001"],
      "context_plan": {
        "required_repositories": ["smoke-test-repo"],
        "context_coverage_expectations": ["doc_coverage"],
        "blocked_if_missing": ["none"],
        "seed_files_to_read": [
          {
            "repo_id": "smoke-test-repo",
            "path": "docs/mcp.md",
            "purpose": "MCP documentation",
            "required": false
          }
        ],
        "seed_search_terms": [
          {
            "repo_id": "smoke-test-repo",
            "query": "MCP",
            "purpose": "locate MCP docs",
            "required": false
          }
        ]
      },
      "source_snapshot_requirements": {
        "require_git_status": false,
        "require_commit_sha": false,
        "allow_dirty_worktree": true
      },
      "handoff_readiness_criteria": ["docs_ready"],
      "status": "planned"
    }
  ]
}`
}

func smokePlanRequiredContextFixture() string {
	return `{
  "plan_meta": {
    "plan_id": "mcp-smoke-plan-req-ctx",
    "schema_version": "2.0.0",
    "created_at": "2026-06-21T00:00:00Z",
    "project_id": "relay",
    "title": "MCP Smoke Required Context Plan",
    "goal": "Verify context usability in smoke harness.",
    "repo_target": "smoke-test-repo",
    "branch_context": "main",
    "status": "active"
  },
  "source_intent": {
    "summary": "Synthetic smoke plan for required context coverage."
  },
  "passes": [
    {
      "pass_id": "PASS-001",
      "sequence": 1,
      "name": "Context pass",
      "goal": "Exercise required context usability.",
      "pass_type": "backend_vertical_slice",
      "intended_execution_scope": ["cmd/mcp-smoke/main.go"],
      "non_goals": ["No production data mutation"],
      "dependencies": [],
      "context_plan": {
        "required_repositories": ["smoke-test-repo"],
        "context_coverage_expectations": ["basic_smoke_coverage"],
        "blocked_if_missing": ["none"],
        "seed_files_to_read": [
          {
            "repo_id": "smoke-test-repo",
            "path": "cmd/mcp-smoke/main.go",
            "purpose": "smoke test entry point",
            "required": true
          }
        ],
        "seed_search_terms": [
          {
            "repo_id": "smoke-test-repo",
            "query": "smoke",
            "purpose": "locate smoke test code",
            "required": true
          }
        ]
      },
      "source_snapshot_requirements": {
        "require_git_status": false,
        "require_commit_sha": false,
        "allow_dirty_worktree": true
      },
      "handoff_readiness_criteria": ["smoke_test_pass_ready"],
      "status": "planned"
    }
  ]
}`
}

// --- harness helpers ---

func (h *harness) call(method string, params interface{}) (*rpcResponse, error) {
	id := h.nextID
	h.nextID++

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if h.httpURL != "" {
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		httpReq, err := http.NewRequest("POST", h.httpURL, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
		if h.httpToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+h.httpToken)
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("do request: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("HTTP error status %d: %s", resp.StatusCode, string(body))
		}
		var rpcResp rpcResponse
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &rpcResp, nil
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := h.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response with timeout via a goroutine.
	type result struct {
		resp *rpcResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if !h.stdout.Scan() {
			ch <- result{err: fmt.Errorf("no response from server (EOF or scan error)")}
			return
		}
		var resp rpcResponse
		if err := json.Unmarshal(h.stdout.Bytes(), &resp); err != nil {
			ch <- result{err: fmt.Errorf("unmarshal response: %w", err)}
			return
		}
		ch <- result{resp: &resp}
	}()

	select {
	case r := <-ch:
		return r.resp, r.err
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response to %q", method)
	}
}

func (h *harness) notify(method string, params interface{}) error {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}

	if h.httpURL != "" {
		data, err := json.Marshal(req)
		if err != nil {
			return err
		}
		httpReq, err := http.NewRequest("POST", h.httpURL, bytes.NewReader(data))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
		if h.httpToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+h.httpToken)
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("HTTP notification error status %d: %s", resp.StatusCode, string(body))
		}
		return nil
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = h.stdin.Write(append(data, '\n'))
	return err
}

func (h *harness) check(name string, ok bool) {
	if ok {
		h.pass++
		fmt.Printf("  ✓ %s\n", name)
	} else {
		h.fail++
		fmt.Printf("  ✗ FAIL: %s\n", name)
	}
}

func (h *harness) failf(format string, args ...interface{}) {
	h.fail++
	fmt.Printf("  ✗ FAIL: "+format+"\n", args...)
}

func (h *harness) fatal(context string, err error) error {
	h.fail++
	return fmt.Errorf("%s: %w", context, err)
}

// seedTestDatabase opens the SQLite database and creates a test project
// and an isolated git repository fixture so that Plan v2 validation and
// context-broker tools can succeed.
func seedTestDatabase(dbPath string) (string, error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s, err := store.Open(dbPath, logger)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	if _, err := s.CreateProject("relay", "Relay", "Smoke Test Project", "active", ""); err != nil {
		return "", fmt.Errorf("create project: %w", err)
	}

	repoRoot, err := createSmokeGitRepo()
	if err != nil {
		return "", fmt.Errorf("create git repo: %w", err)
	}
	if _, err := s.CreateRepo("smoke-test-repo", repoRoot); err != nil {
		return "", fmt.Errorf("create smoke repo: %w", err)
	}

	proj, err := s.GetProjectByProjectID("relay")
	if err != nil {
		return "", fmt.Errorf("lookup project: %w", err)
	}
	if _, err := s.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: "snap-smoke-base",
		ProjectRowID:     proj.ID,
		ProjectID:        proj.ProjectID,
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-28T00:00:00Z",
		SummaryJSON:      "{}",
	}); err != nil {
		return "", fmt.Errorf("create source snapshot: %w", err)
	}

	return repoRoot, nil
}

func createSmokeGitRepo() (string, error) {
	dir, err := os.MkdirTemp("", "relay-smoke-git-*")
	if err != nil {
		return "", err
	}
	if out, err := runGit(dir, "init", "-b", "main"); err != nil {
		return "", fmt.Errorf("git init: %w\n%s", err, string(out))
	}
	if out, err := runGit(dir, "config", "user.name", "Relay Smoke"); err != nil {
		return "", fmt.Errorf("git config name: %w\n%s", err, string(out))
	}
	if out, err := runGit(dir, "config", "user.email", "smoke@relay.test"); err != nil {
		return "", fmt.Errorf("git config email: %w\n%s", err, string(out))
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Smoke Repository\n\nDeterministic smoke fixture.\n"), 0644); err != nil {
		return "", fmt.Errorf("write README: %w", err)
	}
	if out, err := runGit(dir, "add", "."); err != nil {
		return "", fmt.Errorf("git add: %w\n%s", err, string(out))
	}
	if out, err := runGit(dir, "commit", "-m", "Initial smoke commit"); err != nil {
		return "", fmt.Errorf("git commit: %w\n%s", err, string(out))
	}
	return dir, nil
}

func runGit(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, err
	}
	return out, nil
}

func seedSmokeContextPacket(dbPath, planID, passID, packetID, snapshotID, status, completedAt string) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.Open(dbPath, logger)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	proj, err := s.GetProjectByProjectID("relay")
	if err != nil {
		return fmt.Errorf("lookup project: %w", err)
	}
	snap, err := s.GetSourceSnapshotByID(snapshotID)
	if err != nil {
		return fmt.Errorf("lookup source snapshot %q: %w", snapshotID, err)
	}
	if _, err := s.CreateContextPacket(store.CreateContextPacketParams{
		ContextPacketID:     packetID,
		ProjectRowID:        proj.ID,
		ProjectID:           proj.ProjectID,
		PlanID:              planID,
		PassID:              passID,
		TaskSlug:            "smoke",
		SourceSnapshotRowID: snap.ID,
		SourceSnapshotID:    snap.SourceSnapshotID,
		Status:              status,
		BlockedSeedCount:    0,
		MissingSeedCount:    0,
		CompletedAt:         completedAt,
		PacketJSONPath:      "/artifacts/context/" + packetID + ".json",
		CoverageReportPath:  "/artifacts/context/" + packetID + "-coverage.json",
	}); err != nil {
		return fmt.Errorf("create context packet: %w", err)
	}
	return nil
}
