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
//   - No arbitrary file read/write is exposed; file-based run submission reads
//     only the single MCP-supplied planner handoff file parameter for intake.
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
	"strconv"
	"strings"

	appplans "relay/internal/app/plans"
)

const toolsListPageSize = 50

// Server is the MCP stdio server. It reads newline-delimited JSON-RPC 2.0
// requests from r and writes responses to w.
type Server struct {
	log   *slog.Logger
	deps  *MCPDeps
	tools []ToolDefinition
}

// NewServer constructs an MCP server with the profile-appropriate tool set registered.
// deps may be nil for tests that only need protocol-level behavior (no real tools).
func NewServer(log *slog.Logger, deps ...*MCPDeps) *Server {
	var d *MCPDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	s := &Server{log: log, deps: d}

	profile := s.activeProfile()
	switch profile {
	case ToolProfileRestricted:
		// restricted keeps the base tool surface without broker/refactor tools.
	default:
		// local-operator includes everything.
	}

	s.tools = []ToolDefinition{
		// Pass 13A feasibility tool — preserved for backward compatibility.
		ToolSubmitTestAuditPacket,
		// Pass 16 real tools.
		ToolCreateRunFromPlannerHandoff,
		ToolCreateRunFromPlannerHandoffFile,
		ToolSubmitPlannerPassPlan,
		ToolListOpenRuns,
		ToolGetRunStatus,
		ToolSubmitAuditPacket,
	}
	s.tools = append(s.tools, planAttemptToolDefinitions()...)
	s.tools = append(s.tools, planSeedToolDefinitions()...)
	if s.contextBrokerEnabled() {
		s.tools = append(s.tools, contextBrokerToolDefinitions()...)
		s.tools = append(s.tools, refactorBacklogToolDefinitions()...)
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

func (s *Server) activeProfile() ToolProfile {
	if s.deps == nil || strings.TrimSpace(string(s.deps.ToolProfile)) == "" {
		return ToolProfileLocalOperator
	}
	return s.deps.ToolProfile
}

func (s *Server) toolRegistered(name string) bool {
	for _, tool := range s.tools {
		if tool.Name == name {
			return true
		}
	}
	return false
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

// handleToolsList returns a bounded, pageable list of registered tools.
func (s *Server) handleToolsList(req Request) Response {
	params, paramKeys, err := parseToolsListParams(req.Params)
	if err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
	}
	start := 0
	if params.Cursor != "" {
		start, err = strconv.Atoi(params.Cursor)
		if err != nil || start < 0 {
			return errResponse(req.ID, CodeInvalidParams, "invalid params: invalid cursor")
		}
	}

	filtered := filterToolsList(s.tools, params)
	if start > len(filtered) {
		return errResponse(req.ID, CodeInvalidParams, "invalid params: invalid cursor")
	}
	end := start + toolsListPageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	result := ToolsListResult{Tools: append([]ToolDefinition(nil), filtered[start:end]...)}
	if end < len(filtered) {
		result.NextCursor = strconv.Itoa(end)
	}
	if s.log != nil {
		approxBytes := 0
		if b, err := json.Marshal(result); err == nil {
			approxBytes = len(b)
		}
		s.log.Debug("mcp tools/list",
			"method", req.Method,
			"param_keys", paramKeys,
			"has_cursor", params.Cursor != "",
			"has_query", strings.TrimSpace(params.Query) != "",
			"has_include_tags", len(params.IncludeTags) > 0,
			"registered_count", len(s.tools),
			"filtered_count", len(filtered),
			"returned_count", len(result.Tools),
			"has_next_cursor", result.NextCursor != "",
			"approx_response_bytes", approxBytes,
		)
	}
	return okResponse(req.ID, result)
}

func parseToolsListParams(raw json.RawMessage) (ToolsListParams, []string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return ToolsListParams{}, nil, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ToolsListParams{}, nil, err
	}
	keys := make([]string, 0, len(envelope))
	for key := range envelope {
		keys = append(keys, key)
	}
	var params ToolsListParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ToolsListParams{}, keys, err
	}
	params.Query = strings.TrimSpace(params.Query)
	cleanTags := make([]string, 0, len(params.IncludeTags))
	seen := map[string]struct{}{}
	for _, tag := range params.IncludeTags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		cleanTags = append(cleanTags, tag)
	}
	params.IncludeTags = cleanTags
	return params, keys, nil
}

func filterToolsList(tools []ToolDefinition, params ToolsListParams) []ToolDefinition {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	tags := map[string]struct{}{}
	for _, tag := range params.IncludeTags {
		tags[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	if query == "" && len(tags) == 0 {
		return append([]ToolDefinition(nil), tools...)
	}
	out := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		toolTags := toolTagsByName(tool.Name)
		if len(tags) > 0 && !toolHasAnyTag(toolTags, tags) {
			continue
		}
		if query != "" && !toolMatchesQuery(tool, toolTags, query) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func toolMatchesQuery(tool ToolDefinition, tags []string, query string) bool {
	if strings.Contains(strings.ToLower(tool.Name), query) || strings.Contains(strings.ToLower(tool.Description), query) {
		return true
	}
	for _, tag := range tags {
		if strings.Contains(tag, query) {
			return true
		}
	}
	return false
}

func toolHasAnyTag(toolTags []string, wanted map[string]struct{}) bool {
	for _, tag := range toolTags {
		if _, ok := wanted[tag]; ok {
			return true
		}
	}
	return false
}

func toolTagsByName(name string) []string {
	lower := strings.ToLower(name)
	tags := []string{}
	add := func(tag string) {
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}
	switch {
	case strings.Contains(lower, "audit"):
		add("audit")
	case strings.Contains(lower, "planner") || strings.Contains(lower, "plan_attempt") || strings.Contains(lower, "plan_seed"):
		add("planner")
		add("plan")
	case strings.Contains(lower, "run"):
		add("run")
	}
	if strings.Contains(lower, "plan") {
		add("plan")
	}
	if strings.Contains(lower, "pass") {
		add("planner")
	}
	if strings.Contains(lower, "context") {
		add("context")
	}
	if strings.Contains(lower, "source") || strings.Contains(lower, "repository") || strings.Contains(lower, "project_file") {
		add("source")
	}
	if strings.Contains(lower, "refactor") {
		add("refactor")
	}
	if strings.Contains(lower, "test") || strings.Contains(lower, "validation") {
		add("test")
	}
	return tags
}

// handleToolsCall dispatches a tools/call request to the matching handler.
func (s *Server) handleToolsCall(req Request) Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
	}

	if !s.toolRegistered(params.Name) {
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
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
	case "create_run_from_planner_handoff_file":
		result = s.HandleCreateRunFromPlannerHandoffFile(args)
	case "submit_planner_pass_plan":
		result = s.HandleSubmitPlannerPassPlan(args)
	case "list_open_runs":
		result = s.HandleListOpenRuns(args)
	case "get_run_status":
		result = s.HandleGetRunStatus(args)
	case "submit_audit_packet":
		result = s.HandleSubmitAuditPacket(args)
	case toolCreatePlanAttemptWithIntent:
		result = s.HandleCreatePlanAttemptWithIntent(args)
	case toolGetPlanIntentReviewPacket:
		result = s.HandleGetPlanIntentReviewPacket(args)
	case toolSubmitIntentDriftReview:
		result = s.HandleSubmitIntentDriftReview(args)
	case toolRevisePlanAttempt:
		result = s.HandleRevisePlanAttempt(args)
	case toolVoidPlanAttempt:
		result = s.HandleVoidPlanAttempt(args)
	case toolApprovePlanAttempt:
		result = s.HandleApprovePlanAttempt(args)
	case toolSubmitPlanAttempt:
		result = s.HandleSubmitPlanAttempt(args)
	case toolCreatePlanSeed:
		result = s.HandleCreatePlanSeed(args)
	case toolListPlanSeeds:
		result = s.HandleListPlanSeeds(args)
	case toolGetPlanSeed:
		result = s.HandleGetPlanSeed(args)
	case toolGetPlanSeedPlanningContext:
		result = s.HandleGetPlanSeedPlanningContext(args)
	case toolCreatePlanAttemptFromSeed:
		result = s.HandleCreatePlanAttemptFromSeed(args)
	case toolUpdatePlanSeed:
		result = s.HandleUpdatePlanSeed(args)
	case toolDeferPlanSeed:
		result = s.HandleDeferPlanSeed(args)
	case toolRejectPlanSeed:
		result = s.HandleRejectPlanSeed(args)
	case "get_project":
		result = s.HandleGetProject(args)
	case "get_plan":
		result = s.HandleGetPlan(args)
	case "get_pass":
		result = s.HandleGetPass(args)
	case "get_pass_context":
		result = s.HandleGetPassContext(args)
	case appplans.NextPassWorkTool:
		result = s.HandleGetNextPassWork(args)
	case appplans.NextAuditWorkTool:
		result = s.HandleGetNextAuditWork(args)
	case "create_source_snapshot":
		result = s.HandleCreateSourceSnapshot(args)
	case "list_project_files":
		result = s.HandleListProjectFiles(args)
	case "search_project_files":
		result = s.HandleSearchProjectFiles(args)
	case "read_project_file":
		result = s.HandleReadProjectFile(args)
	case "resolve_project_repository":
		result = s.HandleResolveProjectRepository(args)
	case "get_repository_git_status":
		result = s.HandleGetRepositoryGitStatus(args)
	case "get_repository_recent_commit":
		result = s.HandleGetRepositoryRecentCommit(args)
	case "list_repository_changed_files":
		result = s.HandleListRepositoryChangedFiles(args)
	case "get_repository_diff":
		result = s.HandleGetRepositoryDiff(args)
	case "create_context_packet":
		result = s.HandleCreateContextPacket(args)
	case "get_context_packet":
		result = s.HandleGetContextPacket(args)
	case "create_local_audit":
		result = s.HandleCreateLocalAudit(args)
	case "get_local_audit":
		result = s.HandleGetLocalAudit(args)
	case "list_project_local_audits":
		result = s.HandleListProjectLocalAudits(args)
	case "search_project_context_memory":
		result = s.HandleSearchProjectContextMemory(args)
	case "list_project_context_records":
		result = s.HandleListProjectContextRecords(args)
	case "get_project_context_record":
		result = s.HandleGetProjectContextRecord(args)
	case "create_project_context_record":
		result = s.HandleCreateProjectContextRecord(args)
	case "supersede_project_context_record":
		result = s.HandleSupersedeProjectContextRecord(args)
	case "list_refactor_discovery_tasks":
		result = s.HandleListRefactorDiscoveryTasks(args)
	case "get_refactor_discovery_task":
		result = s.HandleGetRefactorDiscoveryTask(args)
	case "create_refactor_discovery_task":
		result = s.HandleCreateRefactorDiscoveryTask(args)
	case "update_refactor_discovery_task":
		result = s.HandleUpdateRefactorDiscoveryTask(args)
	case "complete_refactor_discovery_task":
		result = s.HandleCompleteRefactorDiscoveryTask(args)
	case "close_refactor_discovery_task":
		result = s.HandleCloseRefactorDiscoveryTask(args)
	case "supersede_refactor_discovery_task":
		result = s.HandleSupersedeRefactorDiscoveryTask(args)
	case "list_refactor_candidates":
		result = s.HandleListRefactorCandidates(args)
	case "get_refactor_candidate":
		result = s.HandleGetRefactorCandidate(args)
	case "search_refactor_candidates":
		result = s.HandleSearchRefactorCandidates(args)
	case "create_refactor_candidate":
		result = s.HandleCreateRefactorCandidate(args)
	case "update_refactor_candidate":
		result = s.HandleUpdateRefactorCandidate(args)
	case "defer_refactor_candidate":
		result = s.HandleDeferRefactorCandidate(args)
	case "reject_refactor_candidate":
		result = s.HandleRejectRefactorCandidate(args)
	case "supersede_refactor_candidate":
		result = s.HandleSupersedeRefactorCandidate(args)
	case "suggest_refactor_candidate_placement":
		result = s.HandleSuggestRefactorCandidatePlacement(args)
	case "promote_refactor_candidate_to_plan":
		result = s.HandlePromoteRefactorCandidateToPlan(args)
	case "generate_refactor_only_plan":
		result = s.HandleGenerateRefactorOnlyPlan(args)
	default:
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
	}

	return okResponse(req.ID, result)
}
