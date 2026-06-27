// Command plan-seed-smoke is an executable smoke harness for the Plan Seed HTTP API.
//
// It spins up an isolated temporary SQLite store and chi router, exercises Plan
// Seed capture, planning-context retrieval, and draft attempt registration, and
// verifies that the bridge creates only an intent packet and a plan attempt.
//
// Usage:
//
//	go run ./cmd/plan-seed-smoke
//	make plan-seed-smoke
//
// The harness exits 0 on full pass, nonzero on any mismatch. It never touches
// production data/relay.sqlite.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	projectsapi "relay/internal/api/projects"
	"relay/internal/api/shared"
	appprojects "relay/internal/app/projects"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

type harness struct {
	router http.Handler
	pass   int
	fail   int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nPLAN SEED SMOKE FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	tmpDir, err := os.MkdirTemp("", "relay-plan-seed-smoke-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if _, err := st.CreateProject("relay", "Relay", "Smoke test project", "active", ""); err != nil {
		return fmt.Errorf("create smoke project: %w", err)
	}

	h := &harness{router: newProjectRouter(st)}

	// -------------------------------------------------------
	// 1. Create a captured Plan Seed.
	// -------------------------------------------------------
	createSeedBody := []byte(`{
		"title": "Smoke Seed",
		"quick_context": "Verify plan seed bridge side effects.",
		"constraints": ["stay scoped"],
		"non_goals": ["no managed plan"],
		"priority": "high"
	}`)
	rec := h.post("/api/projects/relay/plan-seeds", createSeedBody)
	h.check("create seed status 201", rec.Code == http.StatusCreated)

	var seedResp projectsapi.ProjectAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &seedResp); err != nil {
		return fmt.Errorf("decode seed response: %w", err)
	}
	h.check("create seed success true", seedResp.Success)
	h.check("create seed returned seed", seedResp.PlanSeed != nil)
	if seedResp.PlanSeed == nil {
		return fmt.Errorf("no seed created")
	}
	seedID := seedResp.PlanSeed.SeedID
	h.check("seed status captured", seedResp.PlanSeed.Status == appprojects.PlanSeedStatusCaptured)

	// -------------------------------------------------------
	// 2. Snapshot baseline table counts.
	// -------------------------------------------------------
	beforeContext := tableCounts(st.DB())

	// -------------------------------------------------------
	// 3. Fetch planning context and assert read-only semantics.
	// -------------------------------------------------------
	rec = h.get("/api/projects/relay/plan-seeds/" + seedID + "/planning-context")
	h.check("planning context status 200", rec.Code == http.StatusOK)

	var contextResp projectsapi.PlanSeedPlanningContextAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &contextResp); err != nil {
		return fmt.Errorf("decode planning context response: %w", err)
	}
	h.check("planning context success true", contextResp.Success)
	h.check("planning context retrievalOnly true", contextResp.PlanningContext.RetrievalSemantics.RetrievalOnly)
	h.check("planning context stateMutated false", !contextResp.PlanningContext.RetrievalSemantics.StateMutated)
	h.check("planning context did not mutate counts", countsEqual(beforeContext, tableCounts(st.DB())))

	// -------------------------------------------------------
	// 4. Register a draft attempt from reviewed Plan JSON.
	// -------------------------------------------------------
	createAttemptBody := []byte(`{
		"planner_pass_plan_json": {
			"plan_meta": {"plan_id": "plan-seed-smoke"},
			"source_intent": {},
			"passes": []
		},
		"source_artifact_path": "handoffs/packets/plan-seed-smoke.json"
	}`)
	rec = h.post("/api/projects/relay/plan-seeds/"+seedID+"/plan-attempts", createAttemptBody)
	h.check("create attempt status 201", rec.Code == http.StatusCreated)

	var attemptResp projectsapi.PlanSeedAttemptAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &attemptResp); err != nil {
		return fmt.Errorf("decode attempt response: %w", err)
	}
	h.check("create attempt success true", attemptResp.Success)
	h.check("create attempt returned seed", attemptResp.Seed != nil)
	h.check("create attempt returned plan attempt", attemptResp.PlanAttempt != nil)
	h.check("create attempt returned intent packet", attemptResp.IntentPacket != nil)
	h.check("seed status planned", attemptResp.Seed != nil && attemptResp.Seed.Status == appprojects.PlanSeedStatusPlanned)
	h.check("seed planAttemptId matches attempt", attemptResp.Seed != nil && attemptResp.PlanAttempt != nil && attemptResp.Seed.PlanAttemptID == attemptResp.PlanAttempt.PlanAttemptID)

	// -------------------------------------------------------
	// 5. Assert final table counts.
	// -------------------------------------------------------
	finalCounts := tableCounts(st.DB())
	h.check("final intent_packets count 1", finalCounts["intent_packets"] == 1)
	h.check("final plan_attempts count 1", finalCounts["plan_attempts"] == 1)
	h.check("final plans count 0", finalCounts["plans"] == 0)
	h.check("final plan_passes count 0", finalCounts["plan_passes"] == 0)
	h.check("final runs count 0", finalCounts["runs"] == 0)

	// -------------------------------------------------------
	// 6. Duplicate attempt is blocked and creates no rows.
	// -------------------------------------------------------
	beforeDuplicate := tableCounts(st.DB())
	rec = h.post("/api/projects/relay/plan-seeds/"+seedID+"/plan-attempts", createAttemptBody)
	h.check("duplicate attempt status 409", rec.Code == http.StatusConflict)

	var duplicateResp projectsapi.PlanSeedAttemptAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &duplicateResp); err != nil {
		return fmt.Errorf("decode duplicate response: %w", err)
	}
	h.check("duplicate attempt success false", !duplicateResp.Success)
	h.check("duplicate attempt blocker SEED_ALREADY_PLANNED", duplicateResp.BlockerCode == appprojects.PlanSeedBlockerSeedAlreadyPlanned)
	h.check("duplicate attempt created no rows", countsEqual(beforeDuplicate, tableCounts(st.DB())))

	// -------------------------------------------------------
	// Summary
	// -------------------------------------------------------
	fmt.Printf("\n=== Plan Seed Smoke Results ===\n")
	fmt.Printf("PASS: %d\n", h.pass)
	fmt.Printf("FAIL: %d\n", h.fail)

	if h.fail > 0 {
		return fmt.Errorf("%d check(s) failed", h.fail)
	}
	fmt.Println("ALL CHECKS PASSED")
	return nil
}

func newProjectRouter(st *store.Store) http.Handler {
	h := projectsapi.NewHandler(appprojects.NewService(st))
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(shared.CORSMiddleware)
		projectsapi.MountRoutes(r, h)
	})
	return r
}

func (h *harness) post(path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *harness) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *harness) check(name string, ok bool) {
	if ok {
		h.pass++
		fmt.Printf("  ✓ %s\n", name)
	} else {
		h.fail++
		fmt.Printf("  ✗ FAIL: %s\n", name)
	}
}

func tableCounts(db *sql.DB) map[string]int {
	counts := make(map[string]int)
	for _, tbl := range []string{"intent_packets", "plan_attempts", "plans", "plan_passes", "runs"} {
		var count int
		query := "SELECT COUNT(*) FROM " + tbl
		if err := db.QueryRow(query).Scan(&count); err != nil {
			counts[tbl] = -1
			continue
		}
		counts[tbl] = count
	}
	return counts
}

func countsEqual(a, b map[string]int) bool {
	for _, tbl := range []string{"intent_packets", "plan_attempts", "plans", "plan_passes", "runs"} {
		if a[tbl] != b[tbl] {
			return false
		}
	}
	return true
}
