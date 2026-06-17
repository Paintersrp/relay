package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// HTTPHandler wraps an MCP Server and handles JSON-RPC requests over HTTP POST.
type HTTPHandler struct {
	server      *Server
	log         *slog.Logger
	authToken   string
	disableAuth bool
}

// NewHTTPHandler creates a new HTTPHandler around an MCP Server.
// It retrieves the bearer token from the environment variable RELAY_MCP_AUTH_TOKEN.
func NewHTTPHandler(srv *Server, log *slog.Logger) *HTTPHandler {
	token := os.Getenv("RELAY_MCP_AUTH_TOKEN")
	disableAuth := os.Getenv("RELAY_MCP_DISABLE_AUTH") == "true"
	return &HTTPHandler{
		server:      srv,
		log:         log,
		authToken:   token,
		disableAuth: disableAuth,
	}
}

// ServeHTTP implements http.Handler. It handles MCP JSON-RPC requests via HTTP POST.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Only POST is supported.", http.StatusMethodNotAllowed)
		return
	}

	// Enforce bearer token auth if not explicitly disabled.
	if !h.disableAuth {
		if h.authToken == "" {
			h.log.Error("mcp http auth failed: RELAY_MCP_AUTH_TOKEN is not configured on the server")
			http.Error(w, "Unauthorized: server auth token not configured", http.StatusUnauthorized)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			http.Error(w, "Unauthorized: invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]
		if token != h.authToken {
			// Warn without logging the actual token value for security
			h.log.Warn("mcp http auth failed: invalid bearer token received")
			http.Error(w, "Unauthorized: invalid bearer token", http.StatusUnauthorized)
			return
		}
	}

	// Read request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("mcp http read body error", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// Dispatch JSON-RPC request.
	resp, skip := h.server.handleLineWithSkip(body)
	if skip {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Error("mcp http write response error", "error", err)
	}
}
