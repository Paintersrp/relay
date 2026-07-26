package server

import (
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

func NewWorkflow(store *workflowstore.Store, log *slog.Logger, ownerInstanceID string, sourceVaults *sourcevault.Manager, mcpHandlers []MCPHandler) *Server {
	if len(mcpHandlers) != 3 {
		panic("complete MCP handlers are required")
	}
	handler, routes := buildWorkflowRuntime(store, log, ownerInstanceID, sourceVaults, mcpHandlers)
	return &Server{log: log, mux: handler, mcpRoutes: routes}
}

func (server *Server) Handler() http.Handler { return server.mux }
