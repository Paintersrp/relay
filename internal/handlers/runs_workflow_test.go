package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"

	"github.com/go-chi/chi/v5"
)

func TestRunWorkflowHappyPathSmoke(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Handoff with all required sections and a deterministic validation command.
	// The command is never actually executed — we stub the worker in step 6.
	handoffText := `# Test Handoff

## Goal

Do the thing.

## Scope

- README.md

## Do not change

- Nothing.

## Task checklist

- [ ] Implement the feature

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`

	// =========================================================================
	// Step 1 — Create a representative handoff/run with a temp repo path
	// =========================================================================
	runID := newTestHandoff(t, s, handoffText)
	if runID == 0 {
		t.Fatal("expected non-zero run ID")
	}

	// =========================================================================
	// Step 2 — Validate handoff
	// =========================================================================
	t.Run("validate-handoff", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		h.validateHandoff(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=prompt") {
			t.Errorf("expected redirect to step=prompt, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=prompt" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=prompt, got %q", runID, pus)
		}

		// handoff_validation_json artifact exists
		if !artifacts.Exists(runID, "handoff_validation_json", pipeline.ArtifactFilename("handoff_validation_json")) {
			t.Error("expected handoff_validation_json artifact")
		}

		// Validation checks exist
		checks, err := s.ListChecksByRun(runID)
		if err != nil {
			t.Fatalf("list checks: %v", err)
		}
		hasValidationCheck := false
		for _, c := range checks {
			if c.Kind == "validation" {
				hasValidationCheck = true
				break
			}
		}
		if !hasValidationCheck {
			t.Error("expected validation check")
		}
	})

	// =========================================================================
	// Step 3 — Prepare prompt / agent prompt generation
	// =========================================================================
	t.Run("prepare-prompt", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		h.preparePrompt(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=prompt") {
			t.Errorf("expected redirect to step=prompt, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=prompt" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=prompt, got %q", runID, pus)
		}

		if !artifacts.Exists(runID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt")) {
			t.Error("expected agent_prompt artifact")
		}
	})

	// =========================================================================
	// Step 4 — Generate OpenCode packet
	// =========================================================================
	t.Run("generate-opencode-packet", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		h.generateOpenCodePacket(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=handoff") {
			t.Errorf("expected redirect to step=handoff, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=handoff" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=handoff, got %q", runID, pus)
		}

		if !artifacts.Exists(runID, "opencode_handoff_packet", pipeline.ArtifactFilename("opencode_handoff_packet")) {
			t.Error("expected opencode_handoff_packet artifact")
		}
	})

	// =========================================================================
	// Step 5 — Submit agent result
	// =========================================================================
	t.Run("submit-agent-result", func(t *testing.T) {
		form := url.Values{}
		form.Set("agent_result_text", "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n")
		req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.submitAgentResult(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=validation") {
			t.Errorf("expected redirect to step=validation, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=validation" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=validation, got %q", runID, pus)
		}

		if !artifacts.Exists(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw")) {
			t.Error("expected agent_result_raw artifact")
		}
		if !artifacts.Exists(runID, "agent_result_json", pipeline.ArtifactFilename("agent_result_json")) {
			t.Error("expected agent_result_json artifact")
		}

		checks, err := s.ListChecksByRun(runID)
		if err != nil {
			t.Fatalf("list checks: %v", err)
		}
		hasAgentCheck := false
		for _, c := range checks {
			if c.Kind == "agent_result" {
				hasAgentCheck = true
				break
			}
		}
		if !hasAgentCheck {
			t.Error("expected agent_result check")
		}

		run, err := s.GetRun(runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status != "agent_done" {
			t.Errorf("expected run status agent_done, got %s", run.Status)
		}
	})

	// =========================================================================
	// Step 6 — Start validation (worker stubbed; redirect-only assertion)
	// =========================================================================
	var launched bool
	h.launchValidation = func(fn func()) {
		launched = true
	}

	t.Run("start-validation", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		h.startValidation(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=validation") {
			t.Errorf("expected redirect to step=validation, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=validation" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=validation, got %q", runID, pus)
		}

		if !launched {
			t.Fatal("expected validation worker to be scheduled")
		}

		// DB-backed execution should exist in starting state
		exec, err := s.GetActiveValidationExecutionByRun(runID)
		if err != nil {
			t.Fatalf("get active execution: %v", err)
		}
		if exec == nil {
			t.Fatal("expected a DB-backed validation execution to exist")
		}
		if exec.Status != "starting" {
			t.Errorf("expected execution status starting, got %s", exec.Status)
		}

		// validation_progress_json artifact should exist
		if !artifacts.Exists(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json")) {
			t.Error("expected validation_progress_json artifact")
		}
	})

	// Seed validation pass evidence since the worker was stubbed.
	// This simulates what the background worker would produce.
	seedValidationPass(t, s, runID)

	// Seed git diff evidence so the downstream audit/commit steps can proceed.
	seedGitDiffEvidence(t, s, runID)

	// =========================================================================
	// Step 7 — Generate audit handoff
	// =========================================================================
	t.Run("generate-audit-handoff", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		h.generateAuditHandoff(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=audit") {
			t.Errorf("expected redirect to step=audit, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=audit" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=audit, got %q", runID, pus)
		}

		if !artifacts.Exists(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff")) {
			t.Error("expected audit_handoff artifact")
		}

		// Verify content is non-empty
		data, err := artifacts.Read(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff"))
		if err != nil {
			t.Fatalf("read audit_handoff: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty audit_handoff content")
		}
	})

	// =========================================================================
	// Step 8 — Prepare git commit suggestion
	// =========================================================================
	t.Run("prepare-git-commit", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		h.prepareGitCommit(w, req, runID)

		if w.Code != 303 {
			t.Fatalf("expected 303 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasSuffix(loc, "?step=commit") {
			t.Errorf("expected redirect to step=commit, got %s", loc)
		}
		pus := w.Header().Get("HX-Push-Url")
		if pus != "/runs/"+itoa(runID)+"?step=commit" {
			t.Errorf("expected HX-Push-Url /runs/%d?step=commit, got %q", runID, pus)
		}

		if !artifacts.Exists(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text")) {
			t.Error("expected commit_message_text artifact")
		}
		if !artifacts.Exists(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json")) {
			t.Error("expected commit_suggestion_json artifact")
		}

		events, err := s.ListEventsByRun(runID)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		found := false
		for _, ev := range events {
			if ev.Message == "Git commit suggestion prepared" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected event 'Git commit suggestion prepared'")
		}

		// Verify commit JSON is valid
		data, err := artifacts.Read(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json"))
		if err != nil {
			t.Fatalf("read commit_suggestion_json: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty commit_suggestion_json")
		}
		if !json.Valid(data) {
			t.Error("expected valid JSON in commit_suggestion_json")
		}
	})

	// =========================================================================
	// Step 9 — Lightweight artifact preview smoke
	// Agent prompt was generated in step 3; verify it renders via Preview.
	// =========================================================================
	t.Run("artifact-preview", func(t *testing.T) {
		ah := NewArtifactsHandler(s)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", itoa(runID))
		rctx.URLParams.Add("kind", "agent_prompt")

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"/artifacts/agent_prompt/preview", nil)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

		ah.Preview(w, r)

		resp := w.Result()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected Content-Type to contain text/html, got %q", ct)
		}
		if !strings.Contains(string(body), `id="run-artifact-preview"`) {
			t.Errorf("expected id=\"run-artifact-preview\" in preview response")
		}
	})

	// Verify the overall run status is validation_passed after the full workflow.
	t.Run("final-run-status", func(t *testing.T) {
		run, err := s.GetRun(runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if run.Status != "validation_passed" {
			t.Errorf("expected final run status validation_passed, got %s", run.Status)
		}
	})
}
