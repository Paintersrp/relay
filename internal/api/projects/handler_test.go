package projects

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"relay/internal/api/shared"
	appprojects "relay/internal/app/projects"
	"relay/internal/store"
	"relay/internal/store/generated"

	"github.com/go-chi/chi/v5"
)

func TestProjectAPIFlow(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	createBody := []byte(`{"project_id":"relay","name":"Relay","description":"Registry test","status":"active"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp ProjectAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createResp.Success || createResp.Project == nil {
		t.Fatalf("expected created project response, got %+v", createResp)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var listResp ProjectAPIResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listResp.Count != 1 || len(listResp.Projects) != 1 {
		t.Fatalf("expected one project, got %+v", listResp)
	}

	repoBody := []byte(`{"repo_id":"relay","role":"primary","local_path":"D:\\Code\\relay","allowed_roots":["internal"],"ignored_globs":["node_modules/**"],"max_file_size_bytes":262144,"include_untracked":true}`)
	repoReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/repositories", bytes.NewReader(repoBody))
	repoReq.Header.Set("Content-Type", "application/json")
	repoRec := httptest.NewRecorder()
	router.ServeHTTP(repoRec, repoReq)
	if repoRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", repoRec.Code, repoRec.Body.String())
	}

	var repoResp ProjectAPIResponse
	if err := json.NewDecoder(repoRec.Body).Decode(&repoResp); err != nil {
		t.Fatalf("decode repo response: %v", err)
	}
	if repoResp.Repository == nil || repoResp.Repository.Role != "primary" {
		t.Fatalf("expected primary repo response, got %+v", repoResp)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/projects/relay", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var getResp ProjectAPIResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.Project == nil {
		t.Fatal("expected project payload")
	}
	if len(getResp.Project.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %+v", getResp.Project.Repositories)
	}
	if getResp.Project.Repositories[0].RepoID != "relay" {
		t.Fatalf("expected repoId relay, got %+v", getResp.Project.Repositories[0])
	}
}

func TestProjectAPIRejectsInvalidRepositoryConfig(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	createBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	invalidBody := []byte("{\"repo_id\":\"relay\",\"role\":\"invalid\",\"local_path\":\"D:\\\\Code\\\\relay\\nnope\"}")
	req := httptest.NewRequest(http.MethodPost, "/api/projects/relay/repositories", bytes.NewReader(invalidBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp shared.ErrorShape
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR, got %+v", errResp)
	}
}

func newProjectAPITestServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	h := NewHandler(appprojects.NewService(st))
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		MountRoutes(r, h)
	})

	return router, st
}

func planSeedCountTables() []string {
	return []string{"intent_packets", "plan_attempts", "plans", "plan_passes", "runs"}
}

func planSeedTableCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, tbl := range planSeedCountTables() {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + tbl).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		counts[tbl] = count
	}
	return counts
}

func assertPlanSeedCountsEqual(t *testing.T, before, after map[string]int, msg string) {
	t.Helper()
	for _, tbl := range planSeedCountTables() {
		if before[tbl] != after[tbl] {
			t.Errorf("%s: expected %s count to remain %d, got %d", msg, tbl, before[tbl], after[tbl])
		}
	}
}

func assertPlanSeedCounts(t *testing.T, got map[string]int, want map[string]int, msg string) {
	t.Helper()
	for _, tbl := range planSeedCountTables() {
		if got[tbl] != want[tbl] {
			t.Errorf("%s: expected %s count %d, got %d", msg, tbl, want[tbl], got[tbl])
		}
	}
}

func TestPlanSeedAPIFlow(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	// 1. Create project
	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", projRec.Code, projRec.Body.String())
	}

	// 2. Create Plan Seed
	createSeedBody := []byte(`{
		"title": "Build Test Suite",
		"quick_context": "We need complete unit and integration tests.",
		"priority": "high",
		"constraints": ["Run locally", "Speedy execution"],
		"non_goals": ["Integration with external CI"],
		"tags": ["testing", "qa"],
		"source_label": "manual-trigger"
	}`)
	seedReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(createSeedBody))
	seedReq.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", seedRec.Code, seedRec.Body.String())
	}

	var seedResp ProjectAPIResponse
	if err := json.NewDecoder(seedRec.Body).Decode(&seedResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !seedResp.Success || seedResp.PlanSeed == nil {
		t.Fatalf("expected plan seed response, got %+v", seedResp)
	}
	seedID := seedResp.PlanSeed.SeedID
	if seedResp.PlanSeed.Status != "captured" {
		t.Errorf("expected status 'captured', got %q", seedResp.PlanSeed.Status)
	}
	if seedResp.PlanSeed.SourceType != "manual" {
		t.Errorf("expected sourceType 'manual', got %q", seedResp.PlanSeed.SourceType)
	}
	if seedResp.PlanSeed.SourceLabel != "manual-trigger" {
		t.Errorf("expected sourceLabel 'manual-trigger', got %q", seedResp.PlanSeed.SourceLabel)
	}

	// 3. List Plan Seeds
	listReq := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plan-seeds", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	var listResp ProjectAPIResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listResp.Count != 1 || len(listResp.PlanSeeds) != 1 {
		t.Fatalf("expected 1 seed in list, got %+v", listResp)
	}

	// 4. Get Plan Seed
	getReq := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plan-seeds/"+seedID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getResp ProjectAPIResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.PlanSeed == nil || getResp.PlanSeed.SeedID != seedID {
		t.Fatalf("expected seed with ID %q, got %+v", seedID, getResp.PlanSeed)
	}
	if getResp.PlanSeed.PlanAttemptID != "" || getResp.PlanSeed.ManagedPlanID != "" {
		t.Errorf("expected empty linkage fields, got: attempt=%q, plan=%q", getResp.PlanSeed.PlanAttemptID, getResp.PlanSeed.ManagedPlanID)
	}

	// 5. Update Plan Seed
	updateBody := []byte(`{
		"title": "Build Test Suite (Updated)",
		"quick_context": "Updated description.",
		"priority": "normal",
		"constraints": ["Run locally only"],
		"non_goals": ["CI"],
		"tags": ["testing"]
	}`)
	updateReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/update", bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	router.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp ProjectAPIResponse
	if err := json.NewDecoder(updateRec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.PlanSeed.Title != "Build Test Suite (Updated)" || updateResp.PlanSeed.Priority != "normal" {
		t.Errorf("expected updated title and priority, got %+v", updateResp.PlanSeed)
	}
	// Check source label is still manual-trigger (not overridden/cleared by empty input in request)
	if updateResp.PlanSeed.SourceLabel != "manual-trigger" {
		t.Errorf("expected sourceLabel to remain 'manual-trigger', got %q", updateResp.PlanSeed.SourceLabel)
	}

	// 6. Defer Plan Seed
	deferBody := []byte(`{"defer_reason": "Waiting for dependency X"}`)
	deferReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/defer", bytes.NewReader(deferBody))
	deferReq.Header.Set("Content-Type", "application/json")
	deferRec := httptest.NewRecorder()
	router.ServeHTTP(deferRec, deferReq)
	if deferRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deferRec.Code, deferRec.Body.String())
	}
	var deferResp ProjectAPIResponse
	if err := json.NewDecoder(deferRec.Body).Decode(&deferResp); err != nil {
		t.Fatalf("decode defer response: %v", err)
	}
	if deferResp.PlanSeed.Status != "deferred" || deferResp.PlanSeed.DeferReason != "Waiting for dependency X" {
		t.Errorf("expected deferred status and reason, got %+v", deferResp.PlanSeed)
	}

	// 7. Reject Plan Seed
	rejectBody := []byte(`{"reject_reason": "Out of scope"}`)
	rejectReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/reject", bytes.NewReader(rejectBody))
	rejectReq.Header.Set("Content-Type", "application/json")
	rejectRec := httptest.NewRecorder()
	router.ServeHTTP(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rejectRec.Code, rejectRec.Body.String())
	}
	var rejectResp ProjectAPIResponse
	if err := json.NewDecoder(rejectRec.Body).Decode(&rejectResp); err != nil {
		t.Fatalf("decode reject response: %v", err)
	}
	if rejectResp.PlanSeed.Status != "rejected" || rejectResp.PlanSeed.RejectReason != "Out of scope" {
		t.Errorf("expected rejected status and reason, got %+v", rejectResp.PlanSeed)
	}
}

func TestPlanSeedAPIRejectsUnknownProjectAndWrongProject(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	// Create project relay
	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", projRec.Code, projRec.Body.String())
	}

	// Create plan seed under relay
	createSeedBody := []byte(`{
		"title": "A Seed",
		"quick_context": "Some context"
	}`)
	seedReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(createSeedBody))
	seedReq.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", seedRec.Code)
	}
	var seedResp ProjectAPIResponse
	_ = json.NewDecoder(seedRec.Body).Decode(&seedResp)
	seedID := seedResp.PlanSeed.SeedID

	// Create project other
	createOtherBody := []byte(`{"project_id":"other","name":"Other","status":"active"}`)
	otherReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createOtherBody))
	otherReq.Header.Set("Content-Type", "application/json")
	otherRec := httptest.NewRecorder()
	router.ServeHTTP(otherRec, otherReq)
	if otherRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", otherRec.Code)
	}

	// Check 404 for wrong project id on seed route
	getReq := httptest.NewRequest(http.MethodGet, "/api/projects/other/plan-seeds/"+seedID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrong project seed route, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// Check 404 for unknown project id on list route
	listReq := httptest.NewRequest(http.MethodGet, "/api/projects/missing/plan-seeds", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing project list route, got %d: %s", listRec.Code, listRec.Body.String())
	}

	// Check 404 for unknown project id on create route
	createReq := httptest.NewRequest(http.MethodPost, "/api/projects/missing/plan-seeds", bytes.NewReader(createSeedBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing project create route, got %d: %s", createRec.Code, createRec.Body.String())
	}
}

func TestPlanSeedAPIRejectsInvalidInputAndSecretLikeQuickContext(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	// Create project relay
	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", projRec.Code)
	}

	// Missing title/quick_context
	invalidBody := []byte(`{"title":"","quick_context":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(invalidBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp shared.ErrorShape
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", errResp.Error)
	}

	// Secret-like value in quick_context
	secretBody := []byte(`{
		"title": "Valid Title",
		"quick_context": "Contains a secret: ghp_1234567890abcdef"
	}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(secretBody))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for secret, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var errResp2 shared.ErrorShape
	_ = json.NewDecoder(rec2.Body).Decode(&errResp2)
	if errResp2.Error != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR for secret, got %s", errResp2.Error)
	}
}

func TestPlanSeedAPIPlanningContextAndCreateAttemptFromSeed(t *testing.T) {
	t.Parallel()

	router, st := newProjectAPITestServer(t)

	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", projRec.Code, projRec.Body.String())
	}

	initialCounts := planSeedTableCounts(t, st.DB())

	createSeedBody := []byte(`{
		"title": "Bridge Seed",
		"quick_context": "Create a reviewed draft attempt.",
		"constraints": ["stay scoped"],
		"non_goals": ["no managed plan"]
	}`)
	seedReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(createSeedBody))
	seedReq.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", seedRec.Code, seedRec.Body.String())
	}
	var seedResp ProjectAPIResponse
	if err := json.NewDecoder(seedRec.Body).Decode(&seedResp); err != nil {
		t.Fatalf("decode seed response: %v", err)
	}
	seedID := seedResp.PlanSeed.SeedID

	// Seed creation has no side effects on intent/plan/run tables.
	assertPlanSeedCountsEqual(t, initialCounts, planSeedTableCounts(t, st.DB()), "after seed creation")

	// 1. GET planning-context leaves counts unchanged.
	t.Run("planning context is read-only", func(t *testing.T) {
		beforeContext := planSeedTableCounts(t, st.DB())
		contextReq := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plan-seeds/"+seedID+"/planning-context", nil)
		contextRec := httptest.NewRecorder()
		router.ServeHTTP(contextRec, contextReq)
		if contextRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", contextRec.Code, contextRec.Body.String())
		}
		var contextResp PlanSeedPlanningContextAPIResponse
		if err := json.NewDecoder(contextRec.Body).Decode(&contextResp); err != nil {
			t.Fatalf("decode context response: %v", err)
		}
		if !contextResp.Success || !contextResp.PlanningContext.RetrievalSemantics.RetrievalOnly || contextResp.PlanningContext.RetrievalSemantics.StateMutated {
			t.Fatalf("unexpected planning context response: %+v", contextResp)
		}
		assertPlanSeedCountsEqual(t, beforeContext, planSeedTableCounts(t, st.DB()), "after planning context")
	})

	// 2. Unknown project/seed returns 404.
	t.Run("unknown project and seed", func(t *testing.T) {
		for _, path := range []string{
			"/api/projects/missing/plan-seeds/" + seedID + "/planning-context",
			"/api/projects/relay/plan-seeds/seed-missing/planning-context",
			"/api/projects/missing/plan-seeds/" + seedID + "/plan-attempts",
			"/api/projects/relay/plan-seeds/seed-missing/plan-attempts",
		} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if bytes.Contains([]byte(path), []byte("plan-attempts")) {
				req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"planner_pass_plan_json":{},"source_artifact_path":"x"}`)))
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("expected 404 for %s, got %d: %s", path, rec.Code, rec.Body.String())
			}
		}
	})

	// 3. Invalid attempt payloads are rejected with no side effects.
	t.Run("invalid attempt payloads rejected", func(t *testing.T) {
		beforeInvalid := planSeedTableCounts(t, st.DB())
		cases := []struct {
			name string
			body string
		}{
			{"unknown field", `{"planner_pass_plan_json":{},"source_artifact_path":"handoffs/packets/plan.json","extra":true}`},
			{"missing source_artifact_path", `{"planner_pass_plan_json":{}}`},
			{"missing planner_pass_plan_json", `{"source_artifact_path":"handoffs/packets/plan.json"}`},
			{"invalid planner_pass_plan_json", `{"planner_pass_plan_json":"not-object","source_artifact_path":"handoffs/packets/plan.json"}`},
			{"non-object planner_pass_plan_json", `{"planner_pass_plan_json":[],"source_artifact_path":"handoffs/packets/plan.json"}`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/plan-attempts", bytes.NewReader([]byte(tc.body)))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
				}
			})
		}
		assertPlanSeedCountsEqual(t, beforeInvalid, planSeedTableCounts(t, st.DB()), "after invalid attempt payloads")
	})

	// 4. Successful attempt creates exactly one intent packet and one plan attempt, zero plans/passes/runs.
	t.Run("create draft attempt from seed", func(t *testing.T) {
		beforeAttempt := planSeedTableCounts(t, st.DB())
		createAttemptBody := []byte(`{
			"planner_pass_plan_json": {
				"plan_meta": {"plan_id": "plan-http"},
				"source_intent": {},
				"passes": []
			},
			"source_artifact_path": "handoffs/packets/plan-http.json"
		}`)
		attemptReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/plan-attempts", bytes.NewReader(createAttemptBody))
		attemptReq.Header.Set("Content-Type", "application/json")
		attemptRec := httptest.NewRecorder()
		router.ServeHTTP(attemptRec, attemptReq)
		if attemptRec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", attemptRec.Code, attemptRec.Body.String())
		}
		var attemptResp PlanSeedAttemptAPIResponse
		if err := json.NewDecoder(attemptRec.Body).Decode(&attemptResp); err != nil {
			t.Fatalf("decode attempt response: %v", err)
		}
		if !attemptResp.Success || attemptResp.Seed == nil || attemptResp.PlanAttempt == nil || attemptResp.IntentPacket == nil {
			t.Fatalf("expected successful attempt response, got %+v", attemptResp)
		}
		if attemptResp.Seed.Status != "planned" || attemptResp.Seed.PlanAttemptID != attemptResp.PlanAttempt.PlanAttemptID {
			t.Fatalf("expected seed linked to draft attempt, got %+v", attemptResp)
		}
		if attemptResp.PlanAttempt.Status != "draft" {
			t.Fatalf("expected draft attempt, got %+v", attemptResp.PlanAttempt)
		}

		assertPlanSeedCounts(t, planSeedTableCounts(t, st.DB()), map[string]int{
			"intent_packets": beforeAttempt["intent_packets"] + 1,
			"plan_attempts":  beforeAttempt["plan_attempts"] + 1,
			"plans":          beforeAttempt["plans"],
			"plan_passes":    beforeAttempt["plan_passes"],
			"runs":           beforeAttempt["runs"],
		}, "after successful attempt")

		// Verify intent packet preserves constraints and non-goals.
		ip, err := generated.New(st.DB()).GetIntentPacketByID(t.Context(), attemptResp.IntentPacket.IntentPacketID)
		if err != nil {
			t.Fatalf("get intent packet: %v", err)
		}
		var constraints []string
		if err := json.Unmarshal([]byte(ip.ConstraintsJson), &constraints); err != nil {
			t.Fatalf("decode intent constraints: %v", err)
		}
		assertStringSlice(t, constraints, []string{"stay scoped", "Non-goal: no managed plan"})

		// 5. Duplicate attempt is blocked and creates no additional rows.
		beforeDuplicate := planSeedTableCounts(t, st.DB())
		duplicateReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/plan-attempts", bytes.NewReader(createAttemptBody))
		duplicateReq.Header.Set("Content-Type", "application/json")
		duplicateRec := httptest.NewRecorder()
		router.ServeHTTP(duplicateRec, duplicateReq)
		if duplicateRec.Code != http.StatusConflict {
			t.Fatalf("expected 409 duplicate blocker, got %d: %s", duplicateRec.Code, duplicateRec.Body.String())
		}
		var duplicateResp PlanSeedAttemptAPIResponse
		if err := json.NewDecoder(duplicateRec.Body).Decode(&duplicateResp); err != nil {
			t.Fatalf("decode duplicate response: %v", err)
		}
		if duplicateResp.Success || duplicateResp.BlockerCode != appprojects.PlanSeedBlockerSeedAlreadyPlanned {
			t.Fatalf("expected duplicate blocker, got %+v", duplicateResp)
		}
		assertPlanSeedCountsEqual(t, beforeDuplicate, planSeedTableCounts(t, st.DB()), "after duplicate attempt")
	})
}

func TestPlanSeedAPIDeferredAndRejectedBlockAttemptCreation(t *testing.T) {
	t.Parallel()

	router, st := newProjectAPITestServer(t)

	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", projRec.Code, projRec.Body.String())
	}

	createSeed := func(title, lifecyclePath, reason string) string {
		body := []byte(`{"title": "` + title + `", "quick_context": "ctx"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp ProjectAPIResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		seedID := resp.PlanSeed.SeedID
		if lifecyclePath != "" {
			lifeBody := []byte(`{"` + lifecyclePath + `_reason": "` + reason + `"}`)
			lifeReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/"+lifecyclePath, bytes.NewReader(lifeBody))
			lifeReq.Header.Set("Content-Type", "application/json")
			lifeRec := httptest.NewRecorder()
			router.ServeHTTP(lifeRec, lifeReq)
			if lifeRec.Code != http.StatusOK {
				t.Fatalf("expected 200 %s, got %d: %s", lifecyclePath, lifeRec.Code, lifeRec.Body.String())
			}
		}
		return seedID
	}

	attemptBody := []byte(`{"planner_pass_plan_json":{"plan_meta":{"plan_id":"x"}},"source_artifact_path":"a.json"}`)
	for _, tc := range []struct {
		name        string
		lifecycle   string
		blockerCode string
	}{
		{"deferred blocks attempt", "defer", appprojects.PlanSeedBlockerSeedNotExpandable},
		{"rejected blocks attempt", "reject", appprojects.PlanSeedBlockerSeedNotExpandable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seedID := createSeed(tc.name, tc.lifecycle, "blocked")
			before := planSeedTableCounts(t, st.DB())
			req := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/plan-attempts", bytes.NewReader(attemptBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
			}
			var resp PlanSeedAttemptAPIResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Success || resp.BlockerCode != tc.blockerCode {
				t.Fatalf("expected %s blocker, got %+v", tc.blockerCode, resp)
			}
			assertPlanSeedCountsEqual(t, before, planSeedTableCounts(t, st.DB()), "after blocked attempt")
		})
	}
}

func TestPlanSeedAPIPartialUpdatePreservesOmittedFieldsAndClearsExplicitLists(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", projRec.Code, projRec.Body.String())
	}

	createSeedBody := []byte(`{
		"title": "Original title",
		"quick_context": "Original context",
		"priority": "normal",
		"constraints": ["keep-constraint"],
		"non_goals": ["keep-nongoal"],
		"tags": ["clear-me"]
	}`)
	seedReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(createSeedBody))
	seedReq.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", seedRec.Code, seedRec.Body.String())
	}
	var seedResp ProjectAPIResponse
	if err := json.NewDecoder(seedRec.Body).Decode(&seedResp); err != nil {
		t.Fatalf("decode seed response: %v", err)
	}
	seedID := seedResp.PlanSeed.SeedID

	updatePlanSeedAPI(t, router, seedID, `{"priority":"high"}`, func(seed *ProjectAPIPlanSeed) {
		if seed.Title != "Original title" || seed.QuickContext != "Original context" || seed.Priority != "high" {
			t.Fatalf("expected priority-only update to preserve scalars, got %+v", seed)
		}
		assertStringSlice(t, seed.Constraints, []string{"keep-constraint"})
		assertStringSlice(t, seed.NonGoals, []string{"keep-nongoal"})
		assertStringSlice(t, seed.Tags, []string{"clear-me"})
	})

	updatePlanSeedAPI(t, router, seedID, `{"title":"Title only"}`, func(seed *ProjectAPIPlanSeed) {
		if seed.Title != "Title only" || seed.QuickContext != "Original context" {
			t.Fatalf("expected title-only update to preserve context, got %+v", seed)
		}
	})

	updatePlanSeedAPI(t, router, seedID, `{"quick_context":"Context only"}`, func(seed *ProjectAPIPlanSeed) {
		if seed.Title != "Title only" || seed.QuickContext != "Context only" {
			t.Fatalf("expected context-only update to preserve title, got %+v", seed)
		}
	})

	updatePlanSeedAPI(t, router, seedID, `{"tags":[]}`, func(seed *ProjectAPIPlanSeed) {
		if len(seed.Tags) != 0 {
			t.Fatalf("expected tags to be cleared, got %+v", seed.Tags)
		}
		assertStringSlice(t, seed.Constraints, []string{"keep-constraint"})
		assertStringSlice(t, seed.NonGoals, []string{"keep-nongoal"})
	})
}

func TestPlanSeedAPIUpdateDoesNotAllowStatusMutation(t *testing.T) {
	t.Parallel()

	router, _ := newProjectAPITestServer(t)

	// Create project relay
	createProjBody := []byte(`{"project_id":"relay","name":"Relay","status":"active"}`)
	projReq := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(createProjBody))
	projReq.Header.Set("Content-Type", "application/json")
	projRec := httptest.NewRecorder()
	router.ServeHTTP(projRec, projReq)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", projRec.Code)
	}

	// Create plan seed under relay
	createSeedBody := []byte(`{
		"title": "A Seed",
		"quick_context": "Some context"
	}`)
	seedReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds", bytes.NewReader(createSeedBody))
	seedReq.Header.Set("Content-Type", "application/json")
	seedRec := httptest.NewRecorder()
	router.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", seedRec.Code)
	}
	var seedResp ProjectAPIResponse
	_ = json.NewDecoder(seedRec.Body).Decode(&seedResp)
	seedID := seedResp.PlanSeed.SeedID

	for name, body := range map[string]string{
		"status":        `{"status":"rejected"}`,
		"seed_id":       `{"seed_id":"seed-other"}`,
		"planAttemptId": `{"planAttemptId":"attempt-123"}`,
		"managedPlanId": `{"managedPlanId":"plan-123"}`,
		"plan_attempt":  `{"plan_attempt_id":"attempt-123"}`,
		"managed_plan":  `{"managed_plan_id":"plan-123"}`,
		"project_id":    `{"project_id":"relay"}`,
		"source_label":  `{"source_label":"forbidden"}`,
		"defer_reason":  `{"defer_reason":"forbidden"}`,
		"reject_reason": `{"reject_reason":"forbidden"}`,
		"planned_at":    `{"planned_at":"2026-06-26T00:00:00Z"}`,
		"source_type":   `{"source_type":"manual"}`,
		"source_ref_id": `{"source_ref_id":"ref-123"}`,
	} {
		t.Run(name, func(t *testing.T) {
			updateReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/update", bytes.NewReader([]byte(body)))
			updateReq.Header.Set("Content-Type", "application/json")
			updateRec := httptest.NewRecorder()
			router.ServeHTTP(updateRec, updateReq)
			if updateRec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", updateRec.Code, updateRec.Body.String())
			}
		})
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plan-seeds/"+seedID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getResp ProjectAPIResponse
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.PlanSeed.Status != "captured" {
		t.Errorf("expected status to remain 'captured', got %q", getResp.PlanSeed.Status)
	}
	if getResp.PlanSeed.PlanAttemptID != "" || getResp.PlanSeed.ManagedPlanID != "" {
		t.Errorf("expected linkage to remain blank, got attempt=%q, plan=%q", getResp.PlanSeed.PlanAttemptID, getResp.PlanSeed.ManagedPlanID)
	}
}

func updatePlanSeedAPI(t *testing.T, router http.Handler, seedID string, body string, assert func(seed *ProjectAPIPlanSeed)) {
	t.Helper()

	updateReq := httptest.NewRequest(http.MethodPost, "/api/projects/relay/plan-seeds/"+seedID+"/update", bytes.NewReader([]byte(body)))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	router.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp ProjectAPIResponse
	if err := json.NewDecoder(updateRec.Body).Decode(&updateResp); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResp.PlanSeed == nil {
		t.Fatalf("expected plan seed response, got %+v", updateResp)
	}
	assert(updateResp.PlanSeed)
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
