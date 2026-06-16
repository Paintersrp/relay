// Package mcp provides the MCP server entry point and tool registry.
// It serves the MCP JSON-RPC 2.0 protocol over stdio.
//
// Usage (from cmd/relay or a standalone binary):
//
//	srv := mcp.NewServer(log)
//	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
//	    log.Error("mcp serve", "error", err)
//	}
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// Server is the MCP stdio server. It reads newline-delimited JSON-RPC 2.0
// requests from r and writes responses to w.
type Server struct {
	log   *slog.Logger
	tools []ToolDefinition
}

// NewServer constructs an MCP server with the default tool set registered.
func NewServer(log *slog.Logger) *Server {
	s := &Server{log: log}
	// Register Pass 13A tool only. Pass 13B tools are not registered here
	// because target-client feasibility has not been confirmed. See mcp.md.
	s.tools = []ToolDefinition{
		ToolSubmitTestAuditPacket,
	}
	return s
}

// Serve reads JSON-RPC 2.0 requests from r and writes responses to w until r
// is closed. Each request and response is a single line of JSON (no Content-Length
// framing required by this transport; the MCP client must send one JSON object per line).
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		resp := s.handleLine(line)
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("encode response: %w", err)
		}
	}
	return scanner.Err()
}

// handleLine dispatches a single JSON-RPC 2.0 request line.
func (s *Server) handleLine(line []byte) Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return errResponse(nil, CodeParseError, "parse error: "+err.Error())
	}

	s.log.Debug("mcp request", "method", req.Method)

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		// Notification; no response required but we return a no-op.
		return okResponse(req.ID, nil)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "ping":
		return okResponse(req.ID, map[string]string{})
	default:
		return errResponse(req.ID, CodeMethodNotFound, "method not found: "+req.Method)
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
			Version: "0.1.0",
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
	default:
		return errResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("unknown tool: %q", params.Name))
	}

	return okResponse(req.ID, result)
}
