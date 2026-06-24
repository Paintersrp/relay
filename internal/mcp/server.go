// Package mcp provides the MCP server entry point and tool registry.
// It serves the MCP JSON-RPC 2.0 protocol over stdio.
//
// Usage (from cmd/mcpserver):
//
//	deps := &mcp.MCPDeps{Store: store, Log: log}
//	srv := mcp.NewServer(log, deps)
//	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
//	    log.Error("mcp serve", "error", err)
//	}
//
// Safety boundaries:
//   - No shell execution is exposed.
//   - No arbitrary file read/write is exposed.
//   - No git commit, push, branch, or worktree mutation is exposed.
//   - All artifact writes go through relay/internal/artifacts conventions.
//   - All run state changes use existing relay store and service behavior.
//   - Tool descriptions explicitly note that Relay does not read chat messages.
//   - Callers must not pass secrets, tokens, auth headers, or private keys as tool arguments.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"relay/internal/plans"
)

// Server is the MCP stdio server. It reads newline-delimited JSON-RPC 2.0
// requests from r and writes responses to w.
type Server struct {
	log   *slog.Logger
	deps  *MCPDeps
	tools []ToolDefinition
}

// NewServer constructs an MCP server with the full Pass 16 tool set registered.
// deps may be nil for tests that only need protocol-level behavior (no real tools).
func NewServer(log *slog.Logger, deps ...*MCPDeps) *Server {
	var d *MCPDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	s := &Server{log: log, deps: d}
	s.tools = []ToolDefinition{
		// Pass 13A feasibility tool — preserved for backward compatibility.
		ToolSubmitTestAuditPacket,
		// Pass 16 real tools.
		ToolCreateRunFromPlannerHandoff,
		ToolSubmitPlannerPassPlan,
		ToolListOpenRuns,
		ToolGetRunStatus,
		ToolSubmitAuditPacket,
	}
	if s.contextBrokerEnabled() {
		s.tools = append(s.tools, contextBrokerToolDefinitions()...)
	}
	return s
}

func (s *Server) contextBrokerEnabled() bool {
	if s == nil {
		return false
	}
	profile := ToolProfileLocalOperator
	if s.deps != nil && strings.TrimSpace(string(s.deps.ToolProfile)) != "" {
		profile = s.deps.ToolProfile
	}
	return profile.ContextBrokerEnabled()
}

// Serve reads JSON-RPC 2.0 requests from r and writes responses to w until r
// is closed. Each request and response is a single line of JSON (no Content-Length
// framing required by this transport; the MCP client must send one JSON object per line).
//
// Notifications (JSON-RPC messages with no "id" field) are dispatched but produce
// no response line, per the JSON-RPC 2.0 specification.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		resp, skip := s.handleLineWithSkip(line)
		if skip {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("encode response: %w", err)
		}
	}
	return scanner.Err()
}

// handleLine dispatches a single JSON-RPC 2.0 request line (used in tests).
func (s *Server) handleLine(line []byte) Response {
	resp, _ := s.handleLineWithSkip(line)
	return resp
}

// handleLineWithSkip dispatches a single JSON-RPC 2.0 request line.
// skip is true for notifications that must not produce a response.
func (s *Server) handleLineWithSkip(line []byte) (resp Response, skip bool) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(line, &envelope); err != nil {
		return errResponse(nil, CodeParseError, "parse error: "+err.Error()), false
	}

	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return errResponse(nil, CodeInvalidRequest, "invalid request: "+err.Error()), false
	}

	if s.log != nil {
		s.log.Debug("mcp request", "method", req.Method)
	}

	if _, hasID := envelope["id"]; !hasID {
		if s.log != nil {
			s.log.Debug("mcp notification ignored", "method", req.Method)
		}
		return Response{}, true
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req), false
	case "tools/list":
		return s.handleToolsList(req), false
	case "tools/call":
		return s.handleToolsCall(req), false
	case "ping":
		return okResponse(req.ID, map[string]string{}), false
	default:
		return errResponse(req.ID, CodeMethodNotFound, "method not found: "+req.Method), false
	}
}

// handleInitialize responds to the MCP initialize handshake.
func (s *Server) handleInitialize(req Request) Response {
	result := InitializeResult{
		ProtocolVersion: MCPProtocolVersion,
		Capabilities: Capabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo{
			Name:    "relay-mcp",
			Version: "0.2.0",
		},
	}
	return okResponse(req.ID, result)
}

// handleToolsList returns the list of registered tools.
func (s *Server) handleToolsList(req Request) Response {
	return okResponse(req.ID, ToolsListResult{Tools: s.tools})
}

// handleToolsCall dispatches a tools/call request to the matching handler.
func (s *Server) handleToolsCall(req Request) Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
	}

	args := params.Arguments
	if args == nil {
		args = json.RawMessage("{}")
	}

	var result ToolCallResult
	switch params.Name {
	case "submit_test_audit_packet":
		result = HandleSubmitTestAuditPacket(args)
	case "create_run_from_planner_handoff":
		result = s.HandleCreateRunFromPlannerHandoff(args)
	case "submit_planner_pass_plan":
		result = s.HandleSubmitPlannerPassPlan(args)
	case "list_open_runs":
		result = s.HandleListOpenRuns(args)
	case "get_run_status":
		result = s.HandleGetRunStatus(args)
	case "submit_audit_packet":
		result = s.HandleSubmitAuditPacket(args)
	case "get_project":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetProject(args)
	case "get_plan":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetPlan(args)
	case "get_pass":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetPass(args)
	case "get_pass_context":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetPassContext(args)
	case plans.NextPassWorkTool:
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetNextPassWork(args)
	case plans.NextAuditWorkTool:
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetNextAuditWork(args)
	case "create_source_snapshot":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleCreateSourceSnapshot(args)
	case "list_project_files":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleListProjectFiles(args)
	case "search_project_files":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleSearchProjectFiles(args)
	case "read_project_file":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleReadProjectFile(args)
	case "get_repository_git_status":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetRepositoryGitStatus(args)
	case "get_repository_recent_commit":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetRepositoryRecentCommit(args)
	case "list_repository_changed_files":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleListRepositoryChangedFiles(args)
	case "get_repository_diff":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetRepositoryDiff(args)
	case "create_context_packet":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleCreateContextPacket(args)
	case "get_context_packet":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetContextPacket(args)
	case "create_local_audit":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleCreateLocalAudit(args)
	case "get_local_audit":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetLocalAudit(args)
	case "list_project_local_audits":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleListProjectLocalAudits(args)
	case "search_project_context_memory":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleSearchProjectContextMemory(args)
	case "list_project_context_records":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleListProjectContextRecords(args)
	case "get_project_context_record":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleGetProjectContextRecord(args)
	case "create_project_context_record":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleCreateProjectContextRecord(args)
	case "supersede_project_context_record":
		if !s.contextBrokerEnabled() {
			return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
		}
		result = s.HandleSupersedeProjectContextRecord(args)
	default:
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
	}

	return okResponse(req.ID, result)
}
