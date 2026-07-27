package main

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"relay/internal/server"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func TestRunPropagatesWorkflowServerConstructionError(t *testing.T) {
	want := errors.New("construction failed")
	original := newWorkflowServer
	t.Cleanup(func() { newWorkflowServer = original })
	newWorkflowServer = func(*workflowstore.Store, *slog.Logger, string, *sourcevault.Manager, []server.MCPHandler) (*server.Server, error) {
		return nil, want
	}

	workflowServer, err := constructRelayServer(nil, nil, "owner-test", nil, nil)
	if workflowServer != nil {
		t.Fatal("server was returned after construction failure")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped construction error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "construct Relay server") {
		t.Fatalf("error = %v", err)
	}
}
