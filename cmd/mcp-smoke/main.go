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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
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
	IsError bool `json:"isError,omitempty"`
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
			cmd:    cmd,
			stdin:  stdinPipe,
			stdout: bufio.NewScanner(stdoutPipe),
			nextID: 1,
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
	// 3. tools/list — verify exactly 6 approved tools, no unsafe tools
	// -------------------------------------------------------
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
	}
	if err := json.Unmarshal(resp.Result, &toolsList); err != nil {
		return h.fatal("tools/list parse", err)
	}

	approvedTools := map[string]bool{
		"submit_test_audit_packet":        true,
		"create_run_from_planner_handoff": true,
		"submit_planner_pass_plan":        true,
		"list_open_runs":                  true,
		"get_run_status":                  true,
		"submit_audit_packet":             true,
	}
	unsafeKeywords := []string{"exec", "shell", "read_file", "write_file", "git_commit", "git_push", "checkout", "reset", "branch"}

	h.check("tools/list count=6", len(toolsList.Tools) == 6)
	for _, tool := range toolsList.Tools {
		h.check("tools/list approved:"+tool.Name, approvedTools[tool.Name])
		for _, unsafe := range unsafeKeywords {
			lname := strings.ToLower(tool.Name)
			if strings.Contains(lname, unsafe) {
				h.failf("UNSAFE tool registered: %q contains keyword %q", tool.Name, unsafe)
			}
		}
	}
	// Verify every approved tool is present.
	registeredNames := map[string]bool{}
	for _, t := range toolsList.Tools {
		registeredNames[t.Name] = true
	}
	for name := range approvedTools {
		h.check("tools/list has:"+name, registeredNames[name])
	}

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

	// -------------------------------------------------------
	// 6. create_run_from_planner_handoff — create pass-associated run
	// -------------------------------------------------------
	handoffMarkdown := `---
title: Smoke Test Handoff
repo_target: smoke-test-repo
branch_context: main
---

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
	// 6. get_run_status — verify bounded snapshot for created run
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
	h.check("submit_audit_packet !isError", !auditResult.IsError)
	if len(auditResult.Content) > 0 {
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(auditResult.Content[0].Text), &out); err == nil {
			h.check("submit_audit_packet ok=true", out["ok"] == true)
			h.check("submit_audit_packet decision=revision_required", out["decision"] == "revision_required")
			h.check("submit_audit_packet status=revision_required", out["status"] == "revision_required")
		}
	}

	// -------------------------------------------------------
	// 9. Unknown tool — verify error response
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
	// 10. Invalid decision — verify tool-level error, no status mutation
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

func smokePlanFixture() string {
	return `{
  "plan_meta": {
    "plan_id": "mcp-smoke-plan",
    "schema_version": "1.0.0",
    "created_at": "2026-06-21T00:00:00Z",
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
      "intended_execution_scope": ["cmd/mcp-smoke/main.go"],
      "non_goals": ["No production data mutation"],
      "dependencies": [],
      "status": "planned"
    },
    {
      "pass_id": "PASS-002",
      "sequence": 2,
      "name": "Smoke pass two",
      "goal": "Provide dependency coverage.",
      "intended_execution_scope": ["docs/mcp.md"],
      "non_goals": ["No UI changes"],
      "dependencies": ["PASS-001"],
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
