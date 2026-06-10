package handlers

import (
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
)

func TestReplaceOriginalHandoffRerunsIntakeReview(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Handoff missing validation commands (will trigger intake warning)
	oldHandoff := `# Test Handoff

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
	runID := newTestHandoff(t, s, oldHandoff)

	// Run intake validation first to create checks
	h.validateHandoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), runID)

	// Verify initial state: validation check exists with warning about missing commands
	checks, err := s.ListChecksByRun(runID)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	hasWarn := false
	for _, c := range checks {
		if c.Kind == "validation" && c.Status == "warn" {
			hasWarn = true
			break
		}
	}
	if !hasWarn {
		t.Fatal("expected validation warn check for missing validation commands")
	}

	// New handoff with validation commands
	newHandoff := `# Test Handoff

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

	// Replace handoff via direct handler call
	form := url.Values{
		"action":       {"replace-original-handoff"},
		"handoff_text": {newHandoff},
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.replaceOriginalHandoff(w, req, runID)

	// Should redirect (303)
	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}

	// Verify original handoff artifact content is new text
	data, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		t.Fatalf("read original handoff: %v", err)
	}
	if string(data) != newHandoff {
		t.Fatalf("original handoff content was not replaced.\nExpected:\n%s\n\nGot:\n%s", newHandoff, string(data))
	}

	// Verify validation checks no longer report missing commands (should be pass now)
	checks, err = s.ListChecksByRun(runID)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	hasWarn = false
	hasPass := false
	for _, c := range checks {
		if c.Kind == "validation" && c.Status == "warn" {
			hasWarn = true
		}
		if c.Kind == "validation" && c.Status == "pass" {
			hasPass = true
		}
	}
	if hasWarn {
		t.Fatal("validation warn check should have been cleared after replacement")
	}
	if !hasPass {
		t.Fatal("expected validation pass check after replacement with valid handoff")
	}
}

func TestReplaceOriginalHandoffClearsGeneratedArtifacts(t *testing.T) {
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

	// Create downstream generated artifacts (simulating a completed intake cycle)
	// Note: handoff_validation_json excluded because validateHandoff recreates it
	cleanableKinds := []string{
		"agent_prompt",
		"ready_prompt",
		"opencode_handoff_packet",
		"opencode_dry_run_json",
	}
	for _, kind := range cleanableKinds {
		p, err := artifacts.Write(runID, kind, pipeline.ArtifactFilename(kind), []byte("stale "+kind))
		if err != nil {
			t.Fatalf("write %s artifact: %v", kind, err)
		}
		_, err = s.CreateArtifact(runID, kind, p, "text/plain")
		if err != nil {
			t.Fatalf("create %s artifact record: %v", kind, err)
		}
	}

	// Create stale checks
	s.CreateCheck(runID, "validation", "warn", "stale check", "{}")
	s.CreateCheck(runID, "validation_run", "fail", "stale validation", "{}")

	// Replace handoff via direct handler call
	form := url.Values{
		"action":       {"replace-original-handoff"},
		"handoff_text": {"# New Handoff\n\n## Goal\n\nFixed.\n\n## Agent final output requirement\n\nDONE or BLOCKED.\n"},
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.replaceOriginalHandoff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}

	// Verify stale artifacts are gone
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	for _, kind := range cleanableKinds {
		for _, a := range artifactsList {
			if a.Kind == kind {
				t.Fatalf("stale artifact %s was not deleted", kind)
			}
		}
	}

	// Verify original handoff still exists
	hasOriginal := false
	for _, a := range artifactsList {
		if a.Kind == "original_handoff" {
			hasOriginal = true
			break
		}
	}
	if !hasOriginal {
		t.Fatal("original_handoff artifact was deleted")
	}

	// Verify stale checks are cleared
	checks, err := s.ListChecksByRun(runID)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	for _, c := range checks {
		if c.Kind == "validation_run" {
			t.Fatal("stale validation_run check was not deleted")
		}
	}
}

func TestReplaceOriginalHandoffRejectsEmptyText(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handoffText := `# Original Handoff

## Goal

Do a thing.

## Agent final output requirement

Return DONE or BLOCKED.
`
	runID := newTestHandoff(t, s, handoffText)

	// Submit empty handoff text
	form := url.Values{
		"action":       {"replace-original-handoff"},
		"handoff_text": {""},
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.replaceOriginalHandoff(w, req, runID)

	// Should redirect (303) with a warning event, not 400
	if w.Code != 303 {
		t.Fatalf("expected 303 redirect for empty text, got %d", w.Code)
	}

	// Verify original handoff content unchanged
	data, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		t.Fatalf("read original handoff: %v", err)
	}
	if string(data) != handoffText {
		t.Fatal("original handoff content changed after empty replacement attempt")
	}

	// Verify a warning event was created
	events, err := s.ListEventsByRun(runID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	hasWarn := false
	for _, e := range events {
		if e.Level == "warn" && strings.Contains(e.Message, "empty") {
			hasWarn = true
			break
		}
	}
	if !hasWarn {
		t.Fatal("expected warning event about empty handoff text")
	}
}
