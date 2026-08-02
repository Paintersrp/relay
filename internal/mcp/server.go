// Package mcp serves role-specific Relay JSON-RPC tool registries over HTTP.
// It exposes no shell, arbitrary file, or git-mutation tooling, and no legacy
// compatibility surface. File-bearing tools retrieve one bounded HTTPS
// canonical artifact and verify exact bytes before persistence.
package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const toolsListPageSize = 16

// Server is an MCP JSON-RPC application bound to one compiled role surface.
type Server struct {
	log               *slog.Logger
	tools             []ToolDefinition
	surfaceHandlers   map[string]surfaceDispatch
	completeToolsList bool
}

// toolRegistered checks if a tool is in the registry.
func (s *Server) toolRegistered(name string) bool {
	for _, tool := range s.tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// handleLine dispatches a single JSON-RPC 2.0 request line.
func (s *Server) handleLine(line []byte) Response {
	resp, _ := s.handleLineWithSkip(line)
	return resp
}

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

func (s *Server) handleInitialize(req Request) Response {
	result := InitializeResult{
		ProtocolVersion: MCPProtocolVersion,
		Capabilities:    Capabilities{Tools: &ToolsCapability{ListChanged: false}},
		ServerInfo:      ServerInfo{Name: "relay-mcp", Version: "0.2.0"},
	}
	return okResponse(req.ID, result)
}

func (s *Server) handleToolsList(req Request) Response {
	params, _, err := parseToolsListParams(req.Params)
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
	if s.completeToolsList {
		if params.Cursor != "" {
			return errResponse(req.ID, CodeInvalidParams, "invalid params: cursor is not supported")
		}
		return okResponse(req.ID, ToolsListResult{Tools: filtered})
	}
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
		if _, ok := seen[tag]; !ok {
			seen[tag] = struct{}{}
			cleanTags = append(cleanTags, tag)
		}
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
	case lower == "get_run_artifact" || strings.Contains(lower, "audit"):
		add("audit")
	case strings.Contains(lower, "plan"):
		add("plan")
	case strings.Contains(lower, "run"):
		add("run")
	}
	if strings.Contains(lower, "test") || strings.Contains(lower, "validation") {
		add("test")
	}
	return tags
}

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

	if s.surfaceHandlers != nil {
		result, err := s.dispatchSurfaceTool(params.Name, args)
		if err != nil {
			return errResponse(req.ID, CodeInvalidParams, err.Error())
		}
		return okResponse(req.ID, result)
	}

	return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
}
