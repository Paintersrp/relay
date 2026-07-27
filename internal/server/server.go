package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/transport/mcpingress"
)

type Server struct {
	log       *slog.Logger
	mux       http.Handler
	mcpRoutes []MCPRouteDescriptor
	ingressMu sync.Mutex
	ingress   *mcpingress.Supervisor
}

func NewWorkflow(store *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults *sourcevault.Manager, mcpHandlers []MCPHandler) (*Server, error) {
	if len(mcpHandlers) != 3 {
		return nil, fmt.Errorf("construct MCP handler set: complete MCP handlers are required")
	}
	handler, routes, err := buildWorkflowRuntime(store, log, ownerInstanceID, sourceVaults, mcpHandlers)
	if err != nil {
		return nil, err
	}
	return &Server{log: log, mux: handler, mcpRoutes: routes}, nil
}

func (server *Server) Handler() http.Handler { return server.mux }
