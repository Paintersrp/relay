// Package mcp implements the Model Context Protocol (MCP) using JSON-RPC 2.0
// over stdio. It exposes Relay run management tools to MCP clients such as
// Claude Desktop, Cursor, and compatible agents.
//
// Safety boundaries:
//   - No shell execution is exposed.
//   - No arbitrary file read/write is exposed.
//   - No git commit, push, branch, or worktree mutation is exposed.
//   - All artifact writes go through relay/internal/artifacts conventions.
//   - All run state changes use existing relay store and service behavior.
package mcp

import (
	"encoding/json"
	"fmt"
)

// JSONRPCVersion is the protocol version used for all messages.
const JSONRPCVersion = "2.0"

// MCPProtocolVersion is the MCP specification version this server declares.
const MCPProtocolVersion = "2024-11-05"

// Request is an inbound JSON-RPC 2.0 request from the MCP client.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is an outbound JSON-RPC 2.0 response to the MCP client.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// InitializeParams is the params for the MCP initialize call.
type InitializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ClientInfo      *ClientInfo     `json:"clientInfo,omitempty"`
}

// ClientInfo holds optional MCP client identification.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the response body for initialize.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// Capabilities describes what the MCP server supports.
type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability indicates tool listing support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// ServerInfo identifies the Relay MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolsListResult is the response body for tools/list.
type ToolsListResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// ToolsListParams is the params body for tools/list.
type ToolsListParams struct {
	Cursor      string   `json:"cursor,omitempty"`
	Query       string   `json:"query,omitempty"`
	IncludeTags []string `json:"include_tags,omitempty"`
}

// ToolDefinition describes an MCP tool exposed by the server.
type ToolDefinition struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  map[string]any  `json:"annotations,omitempty"`
	Meta         map[string]any  `json:"_meta,omitempty"`
}

// ToolCallParams is the params for tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResult is the response body for tools/call.
type ToolCallResult struct {
	Content           []ContentBlock `json:"content"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	Meta              any            `json:"_meta,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

// ContentBlock is a piece of content returned by a tool.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// okResponse builds a JSON-RPC success response.
func okResponse(id json.RawMessage, result interface{}) Response {
	return Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

// errResponse builds a JSON-RPC error response.
func errResponse(id json.RawMessage, code int, msg string) Response {
	return Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
}

// toolOK wraps a tool result as a successful ToolCallResult with text content.
func toolOK(text string) ToolCallResult {
	return ToolCallResult{
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

// toolErr wraps an error message as a tool-level error ToolCallResult.
func toolErr(msg string) ToolCallResult {
	return ToolCallResult{
		Content: []ContentBlock{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// toolBlockedResult wraps a bounded shared blocker envelope as a tool error.
func toolBlockedResult(tool string, blockers []MCPBlocker, metadata any) ToolCallResult {
	return toolBlockedJSON(tool, blockers, metadata)
}

// marshalTool marshals v as JSON for embedding in a ToolCallResult.
func marshalTool(v interface{}) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(b), nil
}
