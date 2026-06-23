package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"relay/internal/plans"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

// newNextAuditWorkTestServer creates a minimal test router with the
// GetNextAuditWork route registered.
func newNextAuditWorkTestServer(t *testing.T) (*APIHandler, *store.Store, http.Handler) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	// Seed default project.
	if _, err := st.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("st.CreateProject: %v", err)
	}

	apiH := NewAPIHandler(st, logger)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		r.Post("/plans/validate", apiH.ValidatePlan)
		r.Post("/plans", apiH.SubmitPlan)
		r.Get("/projects/{projectId}/plans/{planId}/next-audit-work", apiH.GetNextAuditWork)
	})

	return apiH, st, router
}

func TestGetNextAuditWork_ConflictingAliasesReturns400(t *testing.T) {
	t.Parallel()

	_, _, router := newNextAuditWorkTestServer(t)

	// passId and pass_id both set but different.
	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plans/api-plan/next-audit-work?passId=PASS-001&pass_id=PASS-002", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plans.NextAuditWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected ok=false")
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != plans.BlockerUnsafeRequest {
		t.Fatalf("expected unsafe_request blocker, got: %+v", resp.Blockers)
	}
}

func TestGetNextAuditWork_UnknownProjectReturns200WithBlocker(t *testing.T) {
	t.Parallel()

	_, _, router := newNextAuditWorkTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/no-such-project/plans/api-plan/next-audit-work", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plans.NextAuditWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatalf("expected ok=false")
	}
	if len(resp.Blockers) == 0 || resp.Blockers[0].Code != plans.BlockerUnknownProject {
		t.Fatalf("expected unknown_project blocker, got: %+v", resp.Blockers)
	}
}

func TestGetNextAuditWork_SuccessReturns200WithOKTrue(t *testing.T) {
	t.Parallel()

	_, st, router := newNextAuditWorkTestServer(t)
	seedNextPassWorkPlan(t, router, "api-plan-001")

	plan, err := st.GetPlanByPlanID("api-plan-001")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}

	pass, err := st.GetPlanPassByPassID(plan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("GetPlanPassByPassID: %v", err)
	}

	repo, err := st.CreateRepo("test-repo", t.TempDir())
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Create run with status audit_ready.
	run, err := st.CreateRunWithAssociation(
		repo.ID,
		"run 1",
		"audit_ready",
		"", "", "", "main",
		sql.NullInt64{Int64: plan.ID, Valid: true},
		sql.NullInt64{Int64: pass.ID, Valid: true},
	)
	if err != nil {
		t.Fatalf("CreateRunWithAssociation: %v", err)
	}

	// Set pass to audit_ready.
	if _, err := st.UpdatePlanPassStatus(pass.ID, "audit_ready"); err != nil {
		t.Fatalf("UpdatePlanPassStatus: %v", err)
	}

	// Create required artifacts.
	if _, err := st.CreateArtifact(run.ID, "audit_packet", "packet.md", "text/markdown"); err != nil {
		t.Fatalf("CreateArtifact packet: %v", err)
	}
	if _, err := st.CreateArtifact(run.ID, "audit_evidence_manifest_json", "manifest.json", "application/json"); err != nil {
		t.Fatalf("CreateArtifact manifest: %v", err)
	}

	// Make the request using both types of aliases to verify they work.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/projects/relay/plans/api-plan-001/next-audit-work?pass_id=PASS-001&runId=%d", run.ID), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp plans.NextAuditWorkResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected ok=true, got blockers: %+v", resp.Blockers)
	}
	if resp.SelectedRun == nil {
		t.Fatal("expected selected_run to be non-nil")
	}
	if resp.SelectedRun.RunID != strconv.FormatInt(run.ID, 10) {
		t.Fatalf("expected run ID %d, got %s", run.ID, resp.SelectedRun.RunID)
	}
}
