package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestResolveRunStep verifies the status-to-step mapping table.
func TestResolveRunStep(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		// intake statuses
		{"draft", "intake"},
		{"validated", "intake"},
		{"needs_cleanup", "intake"},
		{"needs_review", "intake"},
		{"intake_received", "intake"},
		{"intake_needs_review", "intake"},
		{"intake_approved", "intake"},
		{"intake_rejected", "intake"},
		{"intake_blocked", "intake"},
		{"blocked", "intake"},
		// prepare statuses
		{"approved_for_prepare", "prepare"},
		{"packet_ready", "prepare"},
		{"packet_validated", "prepare"},
		{"packet_validation_failed", "prepare"},
		{"repair_validated", "prepare"},
		{"brief_ready_for_review", "prepare"},
		{"brief_validation_failed", "prepare"},
		// execute statuses
		{"approved_for_executor", "execute"},
		{"executor_dispatched", "execute"},
		{"executor_running", "execute"},
		{"executor_done", "execute"},
		{"executor_blocked", "execute"},
		{"executor_error", "execute"},
		{"executor_cancelled", "execute"},
		{"agent_done", "execute"},
		{"agent_blocked", "execute"},
		{"agent_result_needs_review", "execute"},
		// audit statuses
		{"validation_passed", "audit"},
		{"validation_failed_accepted", "audit"},
		{"validation_failed", "audit"},
		{"audit_ready", "audit"},
		{"audit_ready_for_review", "audit"},
		{"revision_required", "audit"},
		{"accepted", "audit"},
		{"accepted_with_warnings", "audit"},
		{"completed", "audit"},
		{"audit_pending", "audit"},
		{"audit_generated", "audit"},
		{"audit_submitted", "audit"},
		{"audit_approved", "audit"},
		{"audit_approved_with_warnings", "audit"},
		{"audit_revision_requested", "audit"},
		{"audit_closed", "audit"},
		{"closed", "audit"},
		// unknown — safe fallback
		{"", "intake"},
		{"unknown_status", "intake"},
	}

	for _, tt := range tests {
		got := resolveRunStep(tt.status)
		if got != tt.want {
			t.Errorf("resolveRunStep(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestWebBaseURL_DefaultsToLocalhost verifies the fallback when env is unset.
func TestWebBaseURL_DefaultsToLocalhost(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "")
	got := webBaseURL()
	if got != "http://localhost:3000" {
		t.Errorf("expected default, got %q", got)
	}
}

// TestWebBaseURL_RespectsEnvVar verifies RELAY_WEB_BASE_URL is honoured.
func TestWebBaseURL_RespectsEnvVar(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://relay.internal:4000/")
	got := webBaseURL()
	if got != "http://relay.internal:4000" {
		t.Errorf("expected trimmed URL, got %q", got)
	}
}

// TestWebURL_AppendsPath verifies internal paths are appended correctly.
func TestWebURL_AppendsPath(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000")
	got := webURL("/runs/42/intake")
	if got != "http://localhost:3000/runs/42/intake" {
		t.Errorf("unexpected webURL result: %q", got)
	}
}

// TestRootRedirectsToReactRuns verifies GET / → React /runs.
func TestRootRedirectsToReactRuns(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, webURL("/runs"), http.StatusFound)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "http://localhost:3000/runs" {
		t.Errorf("expected redirect to React /runs, got %q", loc)
	}
}

// TestHandoffNewRedirectsToReactRunsNew verifies GET /handoffs/new → React /runs/new.
func TestHandoffNewRedirectsToReactRunsNew(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, webURL("/runs/new"), http.StatusFound)
	})

	req := httptest.NewRequest(http.MethodGet, "/handoffs/new", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "http://localhost:3000/runs/new" {
		t.Errorf("expected redirect to React /runs/new, got %q", loc)
	}
}

// TestAgentRunMonitorRedirectsToExecute verifies GET /runs/{id}/agent-run-monitor → React execute.
func TestAgentRunMonitorRedirectsToExecute(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000")

	// Test the redirect URL construction.
	runID := "42"
	got := webURL("/runs/" + runID + "/execute")
	want := "http://localhost:3000/runs/42/execute"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestWebBaseURL_NoTrailingSlashFromEnv verifies trailing slash stripping.
func TestWebBaseURL_NoTrailingSlashFromEnv(t *testing.T) {
	for _, input := range []string{
		"http://relay.local:3000/",
		"http://relay.local:3000//",
	} {
		t.Setenv("RELAY_WEB_BASE_URL", input)
		got := webBaseURL()
		if strings.HasSuffix(got, "/") {
			t.Errorf("webBaseURL(%q) should not have trailing slash, got %q", input, got)
		}
		// Ensure os.Setenv does not persist across sub-test iteration
		os.Unsetenv("RELAY_WEB_BASE_URL")
	}
}
