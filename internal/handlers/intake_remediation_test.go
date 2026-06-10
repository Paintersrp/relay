package handlers

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
)

func TestGenerateIntakeRemediationHandoffCreatesArtifact(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handoffText := `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md
- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Agent final output requirement

Return DONE or BLOCKED.
`
	runID := newTestHandoff(t, s, handoffText)

	// Run intake validation first so checks/events exist
	h.validateHandoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), runID)

	// Generate remediation handoff
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateIntakeRemediationHandoff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	hasRemediation := false
	for _, a := range artifactsList {
		if a.Kind == "intake_remediation_handoff" {
			hasRemediation = true
			break
		}
	}
	if !hasRemediation {
		t.Fatal("expected intake_remediation_handoff artifact after generation")
	}

	// Verify artifact content
	data, err := artifacts.Read(runID, "intake_remediation_handoff", pipeline.ArtifactFilename("intake_remediation_handoff"))
	if err != nil {
		t.Fatalf("read remediation handoff: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Intake Review findings") {
		t.Fatal("expected Intake Review findings section in remediation handoff")
	}
}

func TestGenerateIntakeRemediationHandoffIncludesAllWarnings(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Handoff with no validation commands (triggers warning)
	handoffText := `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Agent final output requirement

Return DONE or BLOCKED.
`
	runID := newTestHandoff(t, s, handoffText)

	h.validateHandoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateIntakeRemediationHandoff(w, req, runID)

	data, err := artifacts.Read(runID, "intake_remediation_handoff", pipeline.ArtifactFilename("intake_remediation_handoff"))
	if err != nil {
		t.Fatalf("read remediation handoff: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "No validation commands") {
		t.Fatal("expected warning about missing validation commands in remediation handoff")
	}
}

func TestGenerateIntakeRemediationHandoffNoopWhenNoWarnings(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handoffText := `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	runID := newTestHandoff(t, s, handoffText)

	h.validateHandoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateIntakeRemediationHandoff(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	for _, a := range artifactsList {
		if a.Kind == "intake_remediation_handoff" {
			t.Fatal("did not expect intake_remediation_handoff artifact when no warnings/blockers")
		}
	}
}

func TestGenerateIntakeRemediationHandoffDoesNotOverwriteOriginal(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handoffText := `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Agent final output requirement

Return DONE or BLOCKED.
`
	runID := newTestHandoff(t, s, handoffText)

	// Read original handoff content before generating remediation
	origData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		t.Fatalf("read original handoff: %v", err)
	}
	origContent := string(origData)

	h.validateHandoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateIntakeRemediationHandoff(w, req, runID)

	// Read original handoff again - should be unchanged
	origData2, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		t.Fatalf("read original handoff after generation: %v", err)
	}
	if string(origData2) != origContent {
		t.Fatal("original handoff content changed after generating remediation handoff")
	}
}

func TestGenerateIntakeRemediationHandoffMissingValidationWarningAddsCommandsSection(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handoffText := `# Test Handoff

## Goal

Do a thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Agent final output requirement

Return DONE or BLOCKED.
`
	runID := newTestHandoff(t, s, handoffText)

	h.validateHandoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateIntakeRemediationHandoff(w, req, runID)

	data, err := artifacts.Read(runID, "intake_remediation_handoff", pipeline.ArtifactFilename("intake_remediation_handoff"))
	if err != nil {
		t.Fatalf("read remediation handoff: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Relay validation commands") {
		t.Fatal("expected ## Relay validation commands section when missing validation commands")
	}
}

func TestIsMissingAgentExecutionsSchemaError(t *testing.T) {
	tests := []struct {
		err      string
		expected bool
	}{
		{"no such table: agent_executions", true},
		{"no such table: agent_executions (1)", true},
		{"pq: no such table: agent_executions", true},
		{"table not found", false},
		{"syntax error", false},
		{"", false},
	}
	for _, tt := range tests {
		var err error
		if tt.err != "" {
			err = &testError{msg: tt.err}
		}
		got := isMissingAgentExecutionsSchemaError(err)
		if got != tt.expected {
			t.Errorf("isMissingAgentExecutionsSchemaError(%q) = %v, want %v", tt.err, got, tt.expected)
		}
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }
