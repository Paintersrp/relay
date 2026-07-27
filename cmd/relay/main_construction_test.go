package main

import (
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"relay/internal/server"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

// startupSequence covers only: construct workflow server → check error → open listener.
// It does not include unrelated startup stages.
func startupSequence(
	store *workflowstore.Store,
	log *slog.Logger,
	ownerInstanceID string,
	sourceVaults *sourcevault.Manager,
	mcpHandlers []server.MCPHandler,
) (*server.Server, net.Listener, error) {
	s, err := constructRelayServer(store, log, ownerInstanceID, sourceVaults, mcpHandlers)
	if err != nil {
		return nil, nil, err
	}
	port := environmentOrDefault("PORT", "8080")
	l, err := listen("tcp", ":"+port)
	if err != nil {
		return nil, nil, err
	}
	return s, l, nil
}

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

func TestConstructionFailurePreventsListenerCreation(t *testing.T) {
	sentinelErr := errors.New("sentinel construction failure")
	listenerCalled := false

	origServer := newWorkflowServer
	origListen := listen
	t.Cleanup(func() {
		newWorkflowServer = origServer
		listen = origListen
	})

	newWorkflowServer = func(*workflowstore.Store, *slog.Logger, string, *sourcevault.Manager, []server.MCPHandler) (*server.Server, error) {
		return nil, sentinelErr
	}
	listen = func(network, address string) (net.Listener, error) {
		listenerCalled = true
		return nil, errors.New("listener should not be called")
	}

	gotServer, gotListener, err := startupSequence(nil, nil, "owner-test", nil, nil)

	// Sentinel must be discoverable.
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("sentinel error not discoverable: got %v", err)
	}
	// Listener creation must not have been attempted.
	if listenerCalled {
		t.Fatal("listener was called despite construction failure")
	}
	// No server or listener must be returned.
	if gotServer != nil {
		t.Fatal("non-nil server returned after construction failure")
	}
	if gotListener != nil {
		t.Fatal("non-nil listener returned after construction failure")
	}
}

func TestListenerFailureReturnedWhenConstructionSucceeds(t *testing.T) {
	listenerErr := errors.New("bind failed")

	origListen := listen
	t.Cleanup(func() { listen = origListen })
	listen = func(network, address string) (net.Listener, error) {
		return nil, listenerErr
	}

	// Use a real (nil-arg) construction path that short-circuits in newWorkflowServer.
	origServer := newWorkflowServer
	t.Cleanup(func() { newWorkflowServer = origServer })
	fakeServer := &server.Server{}
	newWorkflowServer = func(*workflowstore.Store, *slog.Logger, string, *sourcevault.Manager, []server.MCPHandler) (*server.Server, error) {
		return fakeServer, nil
	}

	gotServer, gotListener, err := startupSequence(nil, nil, "owner-test", nil, nil)

	if !errors.Is(err, listenerErr) {
		t.Fatalf("listener error not returned: got %v", err)
	}
	if gotServer != nil {
		t.Fatal("server returned after listener failure")
	}
	if gotListener != nil {
		t.Fatal("listener returned after listener failure")
	}
}
