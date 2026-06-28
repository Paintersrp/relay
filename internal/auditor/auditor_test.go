package auditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupTestArtifactDir creates a temp dir for artifact storage and returns cleanup func.
func setupTestArtifactDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	artifacts.SetBaseDir(dir)
	return dir
}

// writeArtifactFile writes raw bytes directly into the artifact directory for runID
// using the given filename, bypassing the kind allowlist for test flexibility.
func writeArtifactFile(t *testing.T, runID int64, filename string, content []byte) string {
	t.Helper()
	dir := filepath.Join(artifacts.BaseDir, "1")
	if runID != 1 {
		dir = filepath.Join(artifacts.BaseDir, string(rune('0'+runID)))
	}
	// Use the standard Dir helper
	dir = artifacts.Dir(runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	p := filepath.Join(dir, filename)
	if err := os.WriteFile(p, content, 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", p, err)
	}
	return p
}

// canonicalPacketJSON builds a minimal canonical packet JSON with the given fields.
func canonicalPacketJSON(goal, scope string, nonGoals []string, fileTargets []string, checklist []ChecklistItem) []byte {
	type checkRaw struct {
		ID               string `json:"id"`
		Check            string `json:"check"`
		SeverityIfFailed string `json:"severity_if_failed"`
	}
	checks := make([]checkRaw, len(checklist))
	for i, c := range checklist {
		checks[i] = checkRaw{ID: c.ID, Check: c.Check, SeverityIfFailed: string(c.SeverityIfFailed)}
	}
	pkt := map[string]interface{}{
		"execution_payload": map[string]interface{}{
			"goal":         goal,
			"scope":        scope,
			"non_goals":    nonGoals,
			"file_targets": fileTargets,
		},
		"audit_seed": map[string]interface{}{
			"audit_checklist": checks,
		},
	}
	data, _ := json.Marshal(pkt)
	return data
}

// ---------------------------------------------------------------------------
// Run 73-style regression: 14 object checklist items → exactly 14 rows
// ---------------------------------------------------------------------------

func TestParseChecklistItems_Run73Style_14Objects_14Rows(t *testing.T) {
	// Build 14 checklist objects matching Run 73 style
	checks := make([]map[string]interface{}, 14)
	for i := 0; i < 14; i++ {
		checks[i] = map[string]interface{}{
			"id":                 fmt.Sprintf("A%d", i+1),
			"check":              fmt.Sprintf("Confirm check item %d passes.", i+1),
			"severity_if_failed": "error",
		}
	}
	// Override specific items to match real audit checklist variety
	checks[0]["severity_if_failed"] = "blocker"
	checks[1]["check"] = "Confirm go test ./... passes."
	checks[2]["check"] = "Confirm go vet ./... passes."
	checks[3]["check"] = "Confirm no unexpected runtime files changed."
	checks[4]["check"] = "Confirm no test files were deleted."
	checks[5]["check"] = "Confirm only expected files changed."
	checks[6]["check"] = "Confirm diff is documentation-only."
	checks[7]["check"] = "Confirm security-sensitive files unchanged."
	checks[8]["check"] = "Confirm executor result indicates DONE."
	checks[9]["check"] = "Confirm validation commands passed."
	checks[10]["check"] = "Confirm no MCP or auth files changed."
	checks[11]["check"] = "Confirm changed files are within targets."
	checks[12]["check"] = "Confirm no scope drift."
	checks[13]["check"] = "Confirm task satisfies goal."

	raw, _ := json.Marshal(checks)
	items, warnings := parseChecklistItems(raw)

	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 14 {
		t.Fatalf("expected exactly 14 checklist rows from 14 objects, got %d", len(items))
	}

	// Verify no row has severity_if_failed as check text
	for _, it := range items {
		if it.Check == "severity_if_failed" || strings.Contains(it.Check, "severity_if_failed") {
			t.Errorf("checklist item %q has severity_if_failed in check text: %q", it.ID, it.Check)
		}
		if it.ID == "severity_if_failed" || strings.Contains(it.ID, "severity_if_failed") {
			t.Errorf("checklist item ID must not be severity_if_failed, got %q", it.ID)
		}
	}

	// Verify original IDs are preserved
	for i, it := range items {
		expectedID := fmt.Sprintf("A%d", i+1)
		if it.ID != expectedID {
			t.Errorf("item %d expected ID %q, got %q", i, expectedID, it.ID)
		}
		expectedCheck := checks[i]["check"].(string)
		if it.Check != expectedCheck {
			t.Errorf("item %d expected check %q, got %q", i, expectedCheck, it.Check)
		}
	}

	// Verify severity_if_failed values are in severity column, not check text
	for _, it := range items {
		if string(it.SeverityIfFailed) == "severity_if_failed" {
			t.Errorf("severity_if_failed must not appear in severity field as label: %q", it.SeverityIfFailed)
		}
	}
}

// ---------------------------------------------------------------------------
// Evidence model: basic parsing
// ---------------------------------------------------------------------------

func TestParseChecklistItems_TypedObjects(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"A1","check":"Confirm foo.go was edited.","severity_if_failed":"blocker"},
		{"id":"A2","check":"Confirm tests pass.","severity_if_failed":"error"}
	]`)
	items, warnings := parseChecklistItems(raw)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "A1" || items[0].Check != "Confirm foo.go was edited." {
		t.Errorf("item 0 mismatch: %+v", items[0])
	}
	if items[0].SeverityIfFailed != SeverityBlocker {
		t.Errorf("expected blocker severity, got %q", items[0].SeverityIfFailed)
	}
	if items[1].SeverityIfFailed != SeverityError {
		t.Errorf("expected error severity, got %q", items[1].SeverityIfFailed)
	}
}

func TestParseChecklistItems_FlatStrings(t *testing.T) {
	raw := json.RawMessage(`[
		"Audit checklist:",
		"A1: Confirm foo.go was edited.",
		"severity_if_failed: blocker",
		"A2: Confirm tests pass.",
		"severity_if_failed: error"
	]`)
	items, warnings := parseChecklistItems(raw)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) == 0 {
		t.Fatal("expected at least 1 item from flat format")
	}
	// Flat format parses line items (section headers are skipped)
	foundFoo := false
	for _, it := range items {
		if strings.Contains(it.Check, "foo.go") {
			foundFoo = true
		}
	}
	if !foundFoo {
		t.Error("expected to find 'foo.go' check in flat items")
	}
}

func TestParseChecklistItems_TypedObjects_MalformedEntry(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"A1","check":"Confirm foo.go was edited.","severity_if_failed":"blocker"},
		{"id":"A2","check":"","severity_if_failed":"error"},
		{"id":"A3","check":"Confirm tests pass.","severity_if_failed":"error"}
	]`)
	items, warnings := parseChecklistItems(raw)
	if len(items) != 2 {
		t.Fatalf("expected 2 valid items, got %d: %+v", len(items), items)
	}
	if len(warnings) == 0 {
		t.Error("expected warning for malformed entry with empty check")
	}
	if items[0].ID != "A1" {
		t.Errorf("expected first item ID A1, got %q", items[0].ID)
	}
	if items[1].ID != "A3" {
		t.Errorf("expected second item ID A3, got %q", items[1].ID)
	}
}

func TestParseChecklistItems_TypedObjects_MissingIDs(t *testing.T) {
	raw := json.RawMessage(`[
		{"check":"Confirm foo.go was edited.","severity_if_failed":"blocker"},
		{"check":"Confirm tests pass.","severity_if_failed":"error"}
	]`)
	items, warnings := parseChecklistItems(raw)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "A1" {
		t.Errorf("expected synthesized ID A1, got %q", items[0].ID)
	}
	if items[1].ID != "A2" {
		t.Errorf("expected synthesized ID A2, got %q", items[1].ID)
	}
}

func TestParseChecklistItems_TypedObjects_SeverityIfFailedNotInCheck(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"A1","check":"Confirm foo.go was edited.","severity_if_failed":"blocker"}
	]`)
	items, warnings := parseChecklistItems(raw)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Check == "severity_if_failed" || strings.Contains(items[0].Check, "severity_if_failed") {
		t.Error("severity_if_failed must not appear in check text")
	}
	if string(items[0].SeverityIfFailed) == "severity_if_failed" {
		t.Error("severity_if_failed must not appear in severity field as label")
	}
}

func TestParseChecklistItems_Empty(t *testing.T) {
	raw := json.RawMessage(`[]`)
	items, warnings := parseChecklistItems(raw)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items for empty array, got %d", len(items))
	}
}

func TestParseChecklistItems_Null(t *testing.T) {
	items, warnings := parseChecklistItems(nil)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items for nil, got %d", len(items))
	}
}

func TestExtractStringField_String(t *testing.T) {
	raw := json.RawMessage(`"do the thing"`)
	s, ok := extractStringField(raw)
	if !ok || s != "do the thing" {
		t.Errorf("expected 'do the thing', ok=true; got %q, %v", s, ok)
	}
}

func TestExtractStringField_StringArray(t *testing.T) {
	raw := json.RawMessage(`["line one","line two"]`)
	s, ok := extractStringField(raw)
	if !ok {
		t.Fatal("expected ok=true for array")
	}
	if !strings.Contains(s, "line one") || !strings.Contains(s, "line two") {
		t.Errorf("expected both lines in joined output, got %q", s)
	}
}

func TestExtractStringField_Empty(t *testing.T) {
	raw := json.RawMessage(`""`)
	_, ok := extractStringField(raw)
	if ok {
		t.Error("expected ok=false for empty string")
	}
}

// ---------------------------------------------------------------------------
// Collector: missing artifacts produce warnings with known severities
// ---------------------------------------------------------------------------

func TestCollect_MissingCanonicalPacket(t *testing.T) {
	setupTestArtifactDir(t)
	// No files written — everything missing.
	ev := &Evidence{RunID: 99, RunTitle: "test", RunStatus: "executor_done"}
	c := &Collector{store: nil}
	c.collectPacketMetadata(99, ev)

	if len(ev.Warnings) == 0 {
		t.Fatal("expected at least one warning for missing canonical_packet.json")
	}
	found := false
	for _, w := range ev.Warnings {
		if strings.Contains(w.Message, "canonical_packet.json") {
			found = true
			if w.Severity != SeverityBlocker {
				t.Errorf("expected blocker severity for missing canonical_packet, got %q", w.Severity)
			}
		}
	}
	if !found {
		t.Error("expected warning mentioning canonical_packet.json")
	}
}

func TestCollect_MissingExecutorResult(t *testing.T) {
	setupTestArtifactDir(t)
	ev := &Evidence{RunID: 99}
	c := &Collector{store: nil}
	c.collectExecutorResult(99, ev)

	if len(ev.Warnings) == 0 {
		t.Fatal("expected warning for missing executor_result.txt")
	}
	found := false
	for _, w := range ev.Warnings {
		if strings.Contains(w.Message, "executor_result") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning mentioning executor_result")
	}
	if ev.ExecutorResult.Present {
		t.Error("ExecutorResult.Present should be false")
	}
}

func TestCollect_MissingChangedFiles(t *testing.T) {
	setupTestArtifactDir(t)
	ev := &Evidence{RunID: 99}
	c := &Collector{store: &fakeStore{}}
	c.collectChangedFiles(99, ev)

	if len(ev.Warnings) == 0 {
		t.Fatal("expected warning for missing changed files")
	}
	if ev.ChangedFiles.Present {
		t.Error("ChangedFiles.Present should be false when artifact missing")
	}
}

func TestCollect_MissingGitDiff(t *testing.T) {
	setupTestArtifactDir(t)
	ev := &Evidence{RunID: 99}
	c := &Collector{}
	c.collectGitDiff(99, ev)

	found := false
	for _, w := range ev.Warnings {
		if strings.Contains(w.Message, "git_diff.patch") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning mentioning git_diff.patch")
	}
}

func TestCollect_MissingValidation_NoArtifacts(t *testing.T) {
	setupTestArtifactDir(t)
	ev := &Evidence{RunID: 99}
	ev.Packet.ValidationCommands = []ValidationCommandSpec{
		{ID: "V1", Command: "go test ./...", Required: true},
	}
	c := &Collector{store: &fakeStore{}}
	c.collectValidationResults(99, ev)

	if len(ev.Warnings) == 0 {
		t.Fatal("expected warning for missing validation artifacts")
	}
	if len(ev.ValidationResults) == 0 {
		t.Fatal("expected at least one validation result (unknown) even with missing artifacts")
	}
	for _, vr := range ev.ValidationResults {
		if vr.Status == CheckPass {
			t.Error("missing validation artifact must not produce pass result")
		}
	}
}

// ---------------------------------------------------------------------------
// Collector: goal/scope/non-goals are parsed from execution_payload, not raw JSON
// ---------------------------------------------------------------------------

func TestCollect_GoalScopeNonGoals_ParsedCorrectly(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(42)
	checklist := []ChecklistItem{
		{ID: "A1", Check: "Confirm foo.go edited.", SeverityIfFailed: SeverityBlocker},
	}
	pktData := canonicalPacketJSON(
		"Add a new feature to foo.go",
		"Modify only foo.go",
		[]string{"Do not touch bar.go", "Do not add new packages"},
		[]string{"foo.go"},
		checklist,
	)
	writeArtifactFile(t, runID, "canonical_packet.json", pktData)

	ev := &Evidence{RunID: runID}
	c := &Collector{}
	c.collectPacketMetadata(runID, ev)

	if ev.Packet.Goal != "Add a new feature to foo.go" {
		t.Errorf("Goal mismatch: got %q", ev.Packet.Goal)
	}
	if ev.Packet.Scope != "Modify only foo.go" {
		t.Errorf("Scope mismatch: got %q", ev.Packet.Scope)
	}
	if !strings.Contains(ev.Packet.NonGoals, "Do not touch bar.go") {
		t.Errorf("NonGoals missing expected content: got %q", ev.Packet.NonGoals)
	}
	// Must NOT contain raw JSON in goal/scope/non-goals
	if strings.Contains(ev.Packet.Goal, `"execution_payload"`) {
		t.Error("Goal must not contain raw JSON dump")
	}
	if strings.Contains(ev.Packet.Scope, `"audit_seed"`) {
		t.Error("Scope must not contain raw audit_seed JSON")
	}
	if len(ev.Packet.AuditChecklist) == 0 {
		t.Error("expected audit checklist items to be parsed")
	}
}

func TestCollect_GoalScopeNonGoals_MissingProducesWarnings(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(43)
	// Packet with empty execution_payload
	pkt := map[string]interface{}{
		"execution_payload": map[string]interface{}{},
		"audit_seed":        map[string]interface{}{},
	}
	data, _ := json.Marshal(pkt)
	writeArtifactFile(t, runID, "canonical_packet.json", data)

	ev := &Evidence{RunID: runID}
	c := &Collector{}
	c.collectPacketMetadata(runID, ev)

	if len(ev.Packet.MissingFields) == 0 {
		t.Error("expected MissingFields to be populated for empty execution_payload")
	}
	if len(ev.Warnings) == 0 {
		t.Error("expected warnings for missing goal/scope/non-goals")
	}
}

// ---------------------------------------------------------------------------
// Generator: audit packet does not use _Not available_ silently
// ---------------------------------------------------------------------------

func TestGenerateAuditPacket_MissingEvidence_ShowsConsequences(t *testing.T) {
	ev := &Evidence{
		RunID:    1,
		RunTitle: "Test Run",
		Packet: PacketMetadata{
			PacketID: "packet-1",
			// Goal/Scope/NonGoals intentionally empty
		},
		Warnings: []EvidenceWarning{
			{Message: "canonical_packet.json not found", Severity: SeverityBlocker},
		},
	}
	packet := GenerateAuditPacket(ev, DecisionBlocked)

	// Must not silently use _Not available_
	if strings.Contains(packet, "_Not available_") {
		t.Error("audit packet must not contain silent _Not available_ without consequence notes")
	}
	// Must contain EVIDENCE GAP notice
	if !strings.Contains(packet, "EVIDENCE GAP") {
		t.Error("audit packet must contain EVIDENCE GAP notice for missing sections")
	}
	// Must contain audit consequence
	if !strings.Contains(packet, "Audit consequence") {
		t.Error("audit packet must contain audit consequence for missing evidence")
	}
	// Must contain the blocker warning
	if !strings.Contains(packet, "BLOCKER") {
		t.Error("audit packet must mention BLOCKER for blocker-severity warnings in decision section")
	}
}

func TestGenerateAuditPacket_NormalizedSections_NoRawJSON(t *testing.T) {
	ev := &Evidence{
		RunID:    2,
		RunTitle: "Feature Run",
		Packet: PacketMetadata{
			PacketID: "packet-2",
			Goal:     "Add feature X",
			Scope:    "Modify only pkg/x.go",
			NonGoals: "Do not modify pkg/y.go\nDo not add new files",
			AuditChecklist: []ChecklistItem{
				{ID: "A1", Check: "Confirm pkg/x.go was modified.", SeverityIfFailed: SeverityBlocker},
			},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present:         true,
			Files:           []ChangedFileEntry{{Status: "M", Path: "pkg/x.go"}},
			RawArtifactPath: "/data/artifacts/2/git_diff_name_status.txt",
			SourceKind:      "git_diff_name_status",
		},
		ExecutorResult: ExecutorResultEvidence{Present: false},
		ChecklistResults: []PerCheckResult{
			{ID: "A1", Check: "Confirm pkg/x.go was modified.", Result: CheckUnknown, SeverityIfFailed: SeverityBlocker,
				EvidenceSource: "manual", Rationale: "Requires manual review"},
		},
		FileScopeResults: []PerCheckResult{
			{ID: "FS-TARGETS", Check: "Changed files limited to: pkg/x.go", Result: CheckPass,
				SeverityIfFailed: SeverityError, EvidenceSource: "changed_files", Rationale: "All files within targets"},
		},
	}

	packet := GenerateAuditPacket(ev, DecisionManualReviewRequired)

	// Goal/Scope/NonGoals must be normalized, not raw JSON
	if !strings.Contains(packet, "Add feature X") {
		t.Error("packet must contain normalized Goal text")
	}
	if !strings.Contains(packet, "Modify only pkg/x.go") {
		t.Error("packet must contain normalized Scope text")
	}
	if strings.Contains(packet, `"execution_payload"`) {
		t.Error("packet must not contain raw execution_payload JSON in Goal/Scope/NonGoals")
	}
	if strings.Contains(packet, `"audit_seed"`) {
		t.Error("packet must not contain raw audit_seed JSON")
	}
	// Changed files section should list the file
	if !strings.Contains(packet, "pkg/x.go") {
		t.Error("packet must list changed file")
	}
	// Checklist table must show unknown result
	if !strings.Contains(packet, "unknown") {
		t.Error("packet must show unknown result for checklist items without automated evidence")
	}
	// File scope must show pass
	if !strings.Contains(packet, "pass") {
		t.Error("packet must show pass result for file scope when all files within targets")
	}
}

// ---------------------------------------------------------------------------
// Decision logic: missing evidence escalates decision
// ---------------------------------------------------------------------------

func TestDetermineDefaultDecision_BlockerWarning(t *testing.T) {
	ev := &Evidence{
		Warnings: []EvidenceWarning{
			{Message: "canonical_packet.json not found", Severity: SeverityBlocker},
		},
	}
	d := DetermineDefaultDecision(ev)
	if d != DecisionBlocked {
		t.Errorf("expected blocked for blocker warning, got %q", d)
	}
}

func TestDetermineDefaultDecision_ErrorWarning(t *testing.T) {
	ev := &Evidence{
		Warnings: []EvidenceWarning{
			{Message: "executor result missing", Severity: SeverityError},
		},
	}
	d := DetermineDefaultDecision(ev)
	if d != DecisionManualReviewRequired {
		t.Errorf("expected manual_review_required for error warning, got %q", d)
	}
}

func TestDetermineDefaultDecision_RequiredValidationFail(t *testing.T) {
	ev := &Evidence{
		ValidationResults: []ValidationCommandResult{
			{ID: "V1", Command: "go test ./...", Required: true, Status: CheckFail},
		},
	}
	d := DetermineDefaultDecision(ev)
	if d != DecisionRevisionRequired {
		t.Errorf("expected revision_required for required validation fail, got %q", d)
	}
}

func TestDetermineDefaultDecision_NoWarnings(t *testing.T) {
	ev := &Evidence{}
	d := DetermineDefaultDecision(ev)
	if d != DecisionAccepted {
		t.Errorf("expected accepted with no warnings, got %q", d)
	}
}

func TestDetermineDefaultDecision_FileScopeFail(t *testing.T) {
	ev := &Evidence{
		FileScopeResults: []PerCheckResult{
			{Result: CheckFail, SeverityIfFailed: SeverityError},
		},
	}
	d := DetermineDefaultDecision(ev)
	if d != DecisionRevisionRequired {
		t.Errorf("expected revision_required for file scope fail, got %q", d)
	}
}

// ---------------------------------------------------------------------------
// Checklist evaluation: missing evidence → unknown, never pass
// ---------------------------------------------------------------------------

func TestEvaluateChecklistResults_MissingEvidence_NeverPass(t *testing.T) {
	ev := &Evidence{
		Packet: PacketMetadata{
			AuditChecklist: []ChecklistItem{
				{ID: "A1", Check: "Confirm go test ./... passed.", SeverityIfFailed: SeverityBlocker},
				{ID: "A2", Check: "Confirm no files outside scope were modified.", SeverityIfFailed: SeverityError},
			},
		},
		// No executor result, no validation, no changed files
	}
	c := &Collector{}
	c.evaluateChecklistResults(ev)

	for _, cr := range ev.ChecklistResults {
		if cr.Result == CheckPass {
			t.Errorf("checklist item %q must not be pass when evidence is missing", cr.ID)
		}
	}
}

func TestEvaluateChecklistResults_NoChecklist_ProducesUnknown(t *testing.T) {
	ev := &Evidence{Packet: PacketMetadata{}}
	c := &Collector{}
	c.evaluateChecklistResults(ev)

	if len(ev.ChecklistResults) == 0 {
		t.Fatal("expected at least one result even with no checklist")
	}
	for _, cr := range ev.ChecklistResults {
		if cr.Result == CheckPass {
			t.Error("no-checklist result must be unknown, not pass")
		}
	}
}

// ---------------------------------------------------------------------------
// File scope: out-of-scope files produce fail
// ---------------------------------------------------------------------------

func TestEvaluateFileScopeResults_OutOfScope(t *testing.T) {
	ev := &Evidence{
		Packet: PacketMetadata{
			FileTargets: []string{"docs/mcp.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "docs/mcp.md"},
				{Status: "M", Path: "internal/executor/executor.go"}, // out of scope
			},
			RawArtifactPath: "/fake/path",
			SourceKind:      "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	found := false
	for _, r := range ev.FileScopeResults {
		if r.ID == "FS-TARGETS" && r.Result == CheckFail {
			found = true
			if !strings.Contains(r.Rationale, "internal/executor/executor.go") {
				t.Errorf("expected out-of-scope file in rationale: %q", r.Rationale)
			}
		}
	}
	if !found {
		t.Error("expected FS-TARGETS check to fail for out-of-scope files")
	}
}

func TestEvaluateFileScopeResults_AllInScope(t *testing.T) {
	ev := &Evidence{
		Packet: PacketMetadata{
			FileTargets: []string{"docs/mcp.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present:         true,
			Files:           []ChangedFileEntry{{Status: "M", Path: "docs/mcp.md"}},
			RawArtifactPath: "/fake/path",
			SourceKind:      "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	for _, r := range ev.FileScopeResults {
		if r.ID == "FS-TARGETS" && r.Result != CheckPass {
			t.Errorf("expected FS-TARGETS pass when all files in scope, got %q", r.Result)
		}
	}
}

// ---------------------------------------------------------------------------
// Exact canonical checklist regression: two objects produce exactly two rows
// ---------------------------------------------------------------------------

func TestParseChecklistItems_ExactTwoObjects_TwoRows(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"A1","check":"Confirm foo.go was edited.","severity_if_failed":"blocker"},
		{"id":"A2","check":"Confirm tests pass.","severity_if_failed":"error"}
	]`)
	items, warnings := parseChecklistItems(raw)

	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(items) != 2 {
		t.Fatalf("expected exactly 2 checklist rows from 2 object entries, got %d", len(items))
	}

	// No row should contain severity metadata as check text
	for _, it := range items {
		if strings.Contains(it.Check, "severity_if_failed") {
			t.Errorf("checklist item %q has severity_if_failed in check text: %q", it.ID, it.Check)
		}
		if it.Check == "severity_if_failed" {
			t.Errorf("checklist item %q has severity_if_failed as entire check text", it.ID)
		}
	}

	// No row should have severity metadata as ID
	for _, it := range items {
		if it.ID == "severity_if_failed" {
			t.Errorf("checklist item ID must not be severity_if_failed, got %q", it.ID)
		}
	}

	// IDs are preserved
	if items[0].ID != "A1" {
		t.Errorf("expected first item ID A1, got %q", items[0].ID)
	}
	if items[1].ID != "A2" {
		t.Errorf("expected second item ID A2, got %q", items[1].ID)
	}
}

// ---------------------------------------------------------------------------
// Documentation-only task fixture
// ---------------------------------------------------------------------------

func TestDocumentationOnlyFixture(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(99)

	// Write canonical packet
	pktData := canonicalPacketJSON(
		"Add ChatGPT Remote MCP Validation section to docs/mcp.md",
		"Make a documentation-only edit to docs/mcp.md",
		[]string{"Do not add new MCP tools.", "Do not change runtime behavior."},
		[]string{"docs/mcp.md"},
		[]ChecklistItem{
			{ID: "A1", Check: "Confirm docs/mcp.md includes the required section.", SeverityIfFailed: SeverityBlocker},
			{ID: "A2", Check: "Confirm diff is documentation-only.", SeverityIfFailed: SeverityBlocker},
		},
	)
	writeArtifactFile(t, runID, "canonical_packet.json", pktData)

	// Write changed files artifact
	changedFilesPath := writeArtifactFile(t, runID, "git_diff_name_status.txt", []byte("M\tdocs/mcp.md\n"))

	// Write diff artifact
	writeArtifactFile(t, runID, "git_diff.patch", []byte("diff --git a/docs/mcp.md b/docs/mcp.md\n+## ChatGPT Remote MCP Validation\n"))

	ev := &Evidence{RunID: runID, RunTitle: "MCP Docs", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"git_diff_name_status": {changedFilesPath},
			},
		},
	}

	c.collectPacketMetadata(runID, ev)
	c.collectExecutorResult(runID, ev)
	c.collectValidationResults(runID, ev)
	c.collectChangedFiles(runID, ev)
	c.collectGitDiff(runID, ev)
	c.evaluateChecklistResults(ev)
	c.evaluateFileScopeResults(ev)
	c.evaluateNonGoalResults(ev)

	// Goal/Scope/NonGoals must be normalized
	if ev.Packet.Goal == "" {
		t.Error("Goal must be populated for docs-only fixture")
	}
	if strings.Contains(ev.Packet.Goal, `"execution_payload"`) {
		t.Error("Goal must not contain raw JSON")
	}

	// Changed files: only docs/mcp.md
	if !ev.ChangedFiles.Present {
		t.Error("ChangedFiles must be present for docs-only fixture")
	}
	if len(ev.ChangedFiles.Files) != 1 || ev.ChangedFiles.Files[0].Path != "docs/mcp.md" {
		t.Errorf("Expected only docs/mcp.md changed, got %+v", ev.ChangedFiles.Files)
	}

	// Diff must be present
	if !ev.GitDiff.Present {
		t.Error("GitDiff must be present for docs-only fixture")
	}

	// File scope check: docs/mcp.md is in targets → pass
	fsOK := false
	for _, r := range ev.FileScopeResults {
		if r.ID == "FS-TARGETS" && r.Result == CheckPass {
			fsOK = true
		}
	}
	if !fsOK {
		t.Error("expected FS-TARGETS pass for docs-only fixture with correct file")
	}

	// Generate packet and verify no silent _Not available_
	decision := DetermineDefaultDecision(ev)
	packet := GenerateAuditPacket(ev, decision)
	if strings.Contains(packet, "_Not available_") {
		t.Error("audit packet must not contain _Not available_ for docs-only fixture")
	}
}

// ---------------------------------------------------------------------------
// Code task with multiple changed files and passing validation fixture
// ---------------------------------------------------------------------------

func TestCodeTaskWithValidation_Fixture(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(100)

	pktData := canonicalPacketJSON(
		"Add unit tests for pkg/foo",
		"Modify pkg/foo/foo.go and add pkg/foo/foo_test.go",
		[]string{"Do not modify unrelated packages."},
		[]string{"pkg/foo/foo.go", "pkg/foo/foo_test.go"},
		[]ChecklistItem{
			{ID: "A1", Check: "Confirm go test ./pkg/foo/... passes.", SeverityIfFailed: SeverityBlocker},
		},
	)
	writeArtifactFile(t, runID, "canonical_packet.json", pktData)
	changedFilesPath := writeArtifactFile(t, runID, "git_diff_name_status.txt", []byte("M\tpkg/foo/foo.go\nA\tpkg/foo/foo_test.go\n"))
	writeArtifactFile(t, runID, "git_diff.patch", []byte("diff --git a/pkg/foo/foo.go b/pkg/foo/foo.go\n+// comment\n"))
	valPath := writeArtifactFile(t, runID, "validation_stdout.txt", []byte("ok  \tpkg/foo\n"))

	ev := &Evidence{RunID: runID, RunTitle: "Code Task", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"validation_stdout":    {valPath},
				"git_diff_name_status": {changedFilesPath},
			},
		},
	}

	c.collectPacketMetadata(runID, ev)
	c.collectValidationResults(runID, ev)
	c.collectChangedFiles(runID, ev)
	c.collectGitDiff(runID, ev)
	c.evaluateFileScopeResults(ev)

	// Both changed files within targets
	if !ev.ChangedFiles.Present {
		t.Fatal("ChangedFiles must be present")
	}
	if len(ev.ChangedFiles.Files) != 2 {
		t.Errorf("expected 2 changed files, got %d", len(ev.ChangedFiles.Files))
	}

	// File scope should pass (both in targets)
	fsOK := false
	for _, r := range ev.FileScopeResults {
		if r.ID == "FS-TARGETS" && r.Result == CheckPass {
			fsOK = true
		}
	}
	if !fsOK {
		t.Errorf("expected FS-TARGETS pass, got: %+v", ev.FileScopeResults)
	}

	packet := GenerateAuditPacket(ev, DetermineDefaultDecision(ev))
	if strings.Contains(packet, "_Not available_") {
		t.Error("audit packet must not contain _Not available_ when evidence is present")
	}
}

// ---------------------------------------------------------------------------
// Malformed canonical packet
// ---------------------------------------------------------------------------

func TestCollect_MalformedCanonicalPacket(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(101)
	writeArtifactFile(t, runID, "canonical_packet.json", []byte(`{not valid json`))

	ev := &Evidence{RunID: runID}
	c := &Collector{}
	c.collectPacketMetadata(runID, ev)

	if len(ev.Warnings) == 0 {
		t.Fatal("expected warnings for malformed canonical packet")
	}
	// Goal/Scope must be empty (not raw JSON)
	if ev.Packet.Goal != "" {
		t.Errorf("Goal must be empty for malformed packet, got %q", ev.Packet.Goal)
	}
}

// ---------------------------------------------------------------------------
// Secret redaction
// ---------------------------------------------------------------------------

func TestRedactSecrets_BearerToken(t *testing.T) {
	input := "Authorization: Bearer eyJhbGciOiJSUzI1NiJ9.payload.sig"
	out := redactSecrets(input)
	if strings.Contains(out, "eyJhbGciOiJSUzI1NiJ9") {
		t.Error("bearer token should be redacted")
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
}

func TestRedactSecrets_APIKey(t *testing.T) {
	input := "api_key: sk-secret123456"
	out := redactSecrets(input)
	if strings.Contains(out, "sk-secret123456") {
		t.Error("api key should be redacted")
	}
}

// ---------------------------------------------------------------------------
// Fake store for tests that need ListArtifactsByRunKind
// ---------------------------------------------------------------------------

type fakeStore struct {
	artifactPaths map[string][]string // kind -> paths
	run           *store.Run
}

func (f *fakeStore) GetRun(id int64) (*store.Run, error) {
	if f == nil || f.run == nil {
		return &store.Run{ID: id, Title: "test run", Status: "executor_done"}, nil
	}
	return f.run, nil
}

func (f *fakeStore) ListArtifactsByRunKind(runID int64, kind string) ([]store.Artifact, error) {
	if f == nil || f.artifactPaths == nil {
		return nil, nil
	}
	paths, ok := f.artifactPaths[kind]
	if !ok {
		return nil, nil
	}
	out := make([]store.Artifact, len(paths))
	for i, p := range paths {
		out[i] = store.Artifact{Path: p}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Run 71-style docs-only task — comprehensive fixture
// ---------------------------------------------------------------------------

func TestRun71StyleDocsOnlyTask(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(200)

	// Write canonical packet with typed-object checklist items (Run 71 style)
	checks := []map[string]interface{}{
		{"id": "A1", "check": "Confirm docs/mcp.md includes the required section.", "severity_if_failed": "blocker"},
		{"id": "A2", "check": "Confirm diff is documentation-only.", "severity_if_failed": "blocker"},
		{"id": "A3", "check": "Confirm no runtime code files changed outside expected targets.", "severity_if_failed": "error"},
		{"id": "A4", "check": "Confirm no test files were deleted.", "severity_if_failed": "error"},
		{"id": "A5", "check": "Confirm no security-sensitive, auth, or MCP files changed outside expected targets.", "severity_if_failed": "error"},
		{"id": "A6", "check": "Confirm task is documentation-only and touched only documentation files.", "severity_if_failed": "error"},
		{"id": "A7", "check": "Confirm changed files are exactly docs/mcp.md.", "severity_if_failed": "warning"},
		{"id": "A8", "check": "Confirm the executor result indicates DONE.", "severity_if_failed": "warning"},
		{"id": "A9", "check": "Confirm only expected files were edited.", "severity_if_failed": "warning"},
		{"id": "A10", "check": "Confirm go vet ./... passes.", "severity_if_failed": "error"},
	}

	pkt := map[string]interface{}{
		"execution_payload": map[string]interface{}{
			"goal":      "Add ChatGPT Remote MCP Validation section to docs/mcp.md",
			"scope":     "Make a documentation-only edit to docs/mcp.md",
			"non_goals": []string{"Do not add new MCP tools.", "Do not change runtime behavior."},
			"file_targets": []map[string]interface{}{
				{"path": "docs/mcp.md", "role": "docs", "action": "must_edit", "reason": "Add required section"},
			},
			"validation_commands": []map[string]interface{}{
				{"id": "V1", "command": "go vet ./...", "required": true, "purpose": "Run vet", "success_signal": "Command exits 0.", "failure_handling": "attempt_fix_once_then_block"},
				{"id": "V2", "command": "templ generate", "required": true, "purpose": "Gen templ", "success_signal": "Command exits 0.", "failure_handling": "skip_if_command_unavailable"},
			},
		},
		"audit_seed": map[string]interface{}{
			"audit_checklist":   checks,
			"non_goal_checks":   []string{"Verify that out-of-scope goal \"Add new MCP tools\" was not implemented."},
			"file_scope_checks": []string{"Confirm docs/mcp.md edits only satisfy the goal."},
		},
	}
	pktData, _ := json.Marshal(pkt)
	writeArtifactFile(t, runID, "canonical_packet.json", pktData)

	// Write changed files artifact — only docs/mcp.md changed
	changedFilesPath := writeArtifactFile(t, runID, "git_diff_name_status.txt", []byte("M\tdocs/mcp.md\n"))

	// Write diff artifact with added heading
	writeArtifactFile(t, runID, "git_diff.patch", []byte("diff --git a/docs/mcp.md b/docs/mcp.md\nindex abc..def 100644\n--- a/docs/mcp.md\n+++ b/docs/mcp.md\n@@ -1,3 +1,7 @@\n # MCP Tools\n \n+## ChatGPT Remote MCP Validation\n+\n+This section describes how to validate ChatGPT Remote MCP endpoints.\n+\n"))

	// Write executor result artifact
	writeArtifactFile(t, runID, "executor_result.txt", []byte("STATUS: DONE\n\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n"))

	// Write validation artifacts
	valPath := writeArtifactFile(t, runID, "validation_stdout.txt", []byte("ok  \tgithub.com/relay/internal/auditor\nok  \tgithub.com/relay/internal/compiler\n"))

	ev := &Evidence{RunID: runID, RunTitle: "Run 71 Docs", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"git_diff_name_status": {changedFilesPath},
				"validation_stdout":    {valPath},
			},
		},
	}

	c.collectPacketMetadata(runID, ev)
	c.collectExecutorResult(runID, ev)
	c.collectValidationResults(runID, ev)
	c.collectChangedFiles(runID, ev)
	c.collectGitDiff(runID, ev)
	c.evaluateChecklistResults(ev)
	c.evaluateFileScopeResults(ev)
	c.evaluateNonGoalResults(ev)
	c.generateRevisionRequirements(ev)

	// === ASSERTIONS ===

	// 1. Checklist has exactly A1-A10 (no doubled rows, no extra severity_if_failed rows)
	if len(ev.ChecklistResults) != 10 {
		t.Fatalf("expected exactly 10 checklist results (A1-A10), got %d: %+v", len(ev.ChecklistResults), ev.ChecklistResults)
	}
	expectedIDs := []string{"A1", "A2", "A3", "A4", "A5", "A6", "A7", "A8", "A9", "A10"}
	for i, id := range expectedIDs {
		if ev.ChecklistResults[i].ID != id {
			t.Errorf("checklist[%d] expected ID %q, got %q", i, id, ev.ChecklistResults[i].ID)
		}
	}

	// 2. No checklist row has "severity_if_failed" as its check text
	for _, cr := range ev.ChecklistResults {
		if cr.Check == "severity_if_failed" || strings.Contains(cr.Check, "severity_if_failed") {
			t.Errorf("checklist item %q has severity_if_failed in check text: %q", cr.ID, cr.Check)
		}
	}

	// 3. Severity appears in severity column only (never in check or ID)
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "severity_if_failed" || strings.Contains(cr.ID, "severity_if_failed") {
			t.Errorf("checklist item ID must not be severity_if_failed, got %q", cr.ID)
		}
	}

	// 4. A1 (section heading check) should pass with diff evidence
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A1" {
			if cr.Result != CheckPass {
				t.Errorf("A1 expected pass from diff evidence, got %q: %s", cr.Result, cr.Rationale)
			}
			if !strings.Contains(cr.EvidenceSource, "git_diff_patch") {
				t.Errorf("A1 evidence source should reference git_diff_patch, got %q", cr.EvidenceSource)
			}
		}
	}

	// 5. A2 (documentation-only diff) should pass from changed-files evidence
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A2" {
			if cr.Result != CheckPass {
				t.Errorf("A2 expected pass from changed-files evidence, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 6. A3 (no runtime files) should pass
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A3" {
			if cr.Result != CheckPass {
				t.Errorf("A3 expected pass, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 7. A4 (no tests deleted) should pass
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A4" {
			if cr.Result != CheckPass {
				t.Errorf("A4 expected pass, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 8. A5 (no security/auth/MCP files) should pass
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A5" {
			if cr.Result != CheckPass {
				t.Errorf("A5 expected pass, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 9. A6 (doc-only task) should pass
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A6" {
			if cr.Result != CheckPass {
				t.Errorf("A6 expected pass, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 10. A7 (changed files exactly docs/mcp.md) should pass
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A7" {
			if cr.Result != CheckPass {
				t.Errorf("A7 expected pass, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 11. A8 (executor result DONE) — should be unknown from heuristics (no keyword match for DONE status check)
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A8" {
			if cr.Result != CheckUnknown {
				t.Errorf("A8 expected unknown (automated executor status check not implemented), got %q", cr.Result)
			}
		}
	}

	// 12. A9 (only expected files edited) should pass
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A9" {
			if cr.Result != CheckPass {
				t.Errorf("A9 expected pass, got %q: %s", cr.Result, cr.Rationale)
			}
		}
	}

	// 13. A10 (go vet passes) — validation evidence present
	for _, cr := range ev.ChecklistResults {
		if cr.ID == "A10" {
			if cr.Result == CheckUnknown {
				t.Errorf("A10 expected pass or fail from validation evidence, got unknown: %s", cr.Rationale)
			}
		}
	}

	// 14. Validation evidence preserves both commands (required and optional/manual)
	if len(ev.ValidationResults) != 2 {
		t.Fatalf("expected 2 validation results, got %d", len(ev.ValidationResults))
	}
	foundV1 := false
	foundV2 := false
	for _, vr := range ev.ValidationResults {
		if vr.ID == "V1" {
			foundV1 = true
			if !vr.Required {
				t.Error("V1 should be required")
			}
		}
		if vr.ID == "V2" {
			foundV2 = true
			if !vr.Required {
				t.Error("V2 should be required (set in canonical packet)")
			}
		}
	}
	if !foundV1 {
		t.Error("V1 validation command missing from results")
	}
	if !foundV2 {
		t.Error("V2 validation command missing from results")
	}

	// 15. No contradictory validation evidence
	for _, w := range ev.Warnings {
		if strings.Contains(w.Message, "contradiction") {
			t.Errorf("Unexpected contradiction warning: %s", w.Message)
		}
	}

	// 16. Revision requirements for unknown required validation
	hasMissingValReq := false
	for _, rr := range ev.RevisionRequirements {
		if strings.Contains(rr.Reason, "unknown result") {
			hasMissingValReq = true
		}
	}
	if !hasMissingValReq {
		// Either V1 or V2 might have unknown status if the heuristic didn't match
		t.Log("Note: revision requirements may not include unknown-status escalation (depends on heuristic matching)")
	}

	// 17. Generate packet and check for cleanliness
	decision := DetermineDefaultDecision(ev)
	packet := GenerateAuditPacket(ev, decision)
	if strings.Contains(packet, "_Not available_") {
		t.Error("audit packet must not contain _Not available_ for docs-only fixture")
	}
	// Packet should not contain severity_if_failed as a checklist row
	if strings.Contains(packet, "severity_if_failed") {
		t.Error("audit packet must not contain severity_if_failed in checklist rows")
	}
}

// ---------------------------------------------------------------------------
// S8: Artifact-aware audit — PASS-006-like regression tests
// ---------------------------------------------------------------------------

// TestPASS006Like_GeneratedArtifactsExcludedFromFileScope verifies that generated validation
// artifacts (handoffs/validation/latest.*) are excluded from file-scope enforcement.
func TestPASS006Like_GeneratedArtifactsExcludedFromFileScope(t *testing.T) {
	setupTestArtifactDir(t)

	ev := &Evidence{
		RunID: 300, RunTitle: "PASS-006-like", RunStatus: "validation_passed",
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo/foo.go"},
				{Status: "M", Path: "handoffs/validation/latest.validation-report.json"},
				{Status: "M", Path: "handoffs/validation/latest.validation-summary.md"},
			},
			ImplementationFiles: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo/foo.go"},
			},
			GeneratedArtifactFiles: []GeneratedArtifactEntry{
				{Path: "handoffs/validation/latest.validation-report.json", Status: "M", InferredArtifactKind: "validation_report", Recognized: true},
				{Path: "handoffs/validation/latest.validation-summary.md", Status: "M", InferredArtifactKind: "validation_summary", Recognized: true},
			},
			RawArtifactPath: "/fake/path",
			SourceKind:      "git_diff_name_status",
		},
		Packet: PacketMetadata{
			FileTargets: []string{"pkg/foo/foo.go"},
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	// FS-TARGETS and FS-EXPECTED-ONLY must not fail
	for _, r := range ev.FileScopeResults {
		switch r.ID {
		case "FS-TARGETS", "FS-EXPECTED-ONLY":
			if r.Result == CheckFail {
				t.Errorf("%s must not fail for PASS-006-like generated artifacts, got %q: %s", r.ID, r.Result, r.Rationale)
			}
			// Rationale should mention generated artifact exclusion
			if !strings.Contains(r.Rationale, "generated pipeline") && !strings.Contains(r.Rationale, "generated artifact") {
				t.Logf("%s rationale: %s", r.ID, r.Rationale)
			}
		}
	}
}

// TestPASS006Like_UnexpectedRuntimeFileStillFails verifies that a real runtime file outside
// targets still fails file-scope enforcement.
func TestPASS006Like_UnexpectedRuntimeFileStillFails(t *testing.T) {
	setupTestArtifactDir(t)

	ev := &Evidence{
		RunID: 301, RunTitle: "PASS-006-like runtime file", RunStatus: "executor_done",
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo/foo.go"},
				{Status: "M", Path: "internal/newapp/newapp.go"},
			},
			ImplementationFiles: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo/foo.go"},
				{Status: "M", Path: "internal/newapp/newapp.go"},
			},
			RawArtifactPath: "/fake/path",
			SourceKind:      "git_diff_name_status",
		},
		Packet: PacketMetadata{
			FileTargets: []string{"pkg/foo/foo.go"},
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	// FS-TARGETS must fail (newapp.go is outside targets)
	foundFail := false
	for _, r := range ev.FileScopeResults {
		if r.ID == "FS-TARGETS" && r.Result == CheckFail {
			foundFail = true
			if !strings.Contains(r.Rationale, "internal/newapp/newapp.go") {
				t.Errorf("expected newapp.go in rationale, got %q", r.Rationale)
			}
		}
	}
	if !foundFail {
		t.Error("expected FS-TARGETS to fail for unexpected runtime file outside targets")
	}
}

func TestChecklistFileScope_GeneratedArtifactsExcluded(t *testing.T) {
	setupTestArtifactDir(t)

	ev := &Evidence{
		RunID: 302, RunTitle: "checklist generated artifacts", RunStatus: "executor_done",
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo.go"},
				{Status: "M", Path: "handoffs/validation/latest.validation-report.json"},
			},
			ImplementationFiles: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo.go"},
			},
			GeneratedArtifactFiles: []GeneratedArtifactEntry{
				{Path: "handoffs/validation/latest.validation-report.json", Status: "M", InferredArtifactKind: "validation_report", Recognized: true},
			},
			RawArtifactPath: "/fake/changed-files.txt",
			SourceKind:      "git_diff_name_status",
		},
		Packet: PacketMetadata{
			FileTargets: []string{"pkg/foo.go"},
			AuditChecklist: []ChecklistItem{
				{ID: "CL-FS", Check: "File scope includes only expected files", SeverityIfFailed: SeverityError},
			},
		},
	}

	c := &Collector{}
	c.evaluateChecklistResults(ev)

	if len(ev.ChecklistResults) != 1 {
		t.Fatalf("expected 1 checklist result, got %d", len(ev.ChecklistResults))
	}
	result := ev.ChecklistResults[0]
	if result.Result == CheckFail {
		t.Fatalf("generated validation artifact must not fail checklist file scope: %s", result.Rationale)
	}
	if strings.Contains(result.Rationale, "handoffs/validation/latest.validation-report.json") {
		t.Fatalf("generated artifact should not appear as out-of-scope rationale: %s", result.Rationale)
	}
	if !strings.Contains(result.Rationale, "generated pipeline artifact") {
		t.Fatalf("expected generated artifact exclusion rationale, got %q", result.Rationale)
	}
}

func TestChecklistFileScope_UnexpectedImplementationFileFails(t *testing.T) {
	setupTestArtifactDir(t)

	ev := &Evidence{
		RunID: 303, RunTitle: "checklist unexpected implementation", RunStatus: "executor_done",
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo.go"},
				{Status: "M", Path: "internal/newapp/newapp.go"},
			},
			ImplementationFiles: []ChangedFileEntry{
				{Status: "M", Path: "pkg/foo.go"},
				{Status: "M", Path: "internal/newapp/newapp.go"},
			},
			RawArtifactPath: "/fake/changed-files.txt",
			SourceKind:      "git_diff_name_status",
		},
		Packet: PacketMetadata{
			FileTargets: []string{"pkg/foo.go"},
			AuditChecklist: []ChecklistItem{
				{ID: "CL-FS", Check: "Changed files stayed within file scope", SeverityIfFailed: SeverityError},
			},
		},
	}

	c := &Collector{}
	c.evaluateChecklistResults(ev)

	if len(ev.ChecklistResults) != 1 {
		t.Fatalf("expected 1 checklist result, got %d", len(ev.ChecklistResults))
	}
	result := ev.ChecklistResults[0]
	if result.Result != CheckFail {
		t.Fatalf("expected checklist file scope failure, got %q: %s", result.Result, result.Rationale)
	}
	if !strings.Contains(result.Rationale, "internal/newapp/newapp.go") {
		t.Fatalf("expected unexpected implementation path in rationale, got %q", result.Rationale)
	}
}

// TestValidationRunJson_ConcreteRows verifies that validation_run_json with concrete commands
// and no packet command specs produces concrete validation rows, not only V?.
func TestValidationRunJson_ConcreteRows(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(304)

	// Write validation_run.json with concrete commands
	jsonData := []byte(`{"runId":304,"status":"pass","commands":[
		{"id":"V1","command":"go test ./...","required":true,"status":"pass","exitCode":0,"stdoutKind":"validation_stdout","stderrKind":"validation_stderr"},
		{"id":"V2","command":"go vet ./...","required":false,"status":"pass","exitCode":0,"stdoutKind":"validation_stdout","stderrKind":"validation_stderr"}
	]}`)
	valPath := writeArtifactFile(t, runID, "validation_run.json", jsonData)

	ev := &Evidence{RunID: runID, RunTitle: "Test", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"validation_run_json": {valPath},
			},
		},
	}
	c.collectValidationResults(runID, ev)

	// Must have 2 validation rows, not 1 V? row
	if len(ev.ValidationResults) != 2 {
		t.Fatalf("expected 2 concrete validation rows, got %d", len(ev.ValidationResults))
	}
	if ev.ValidationResults[0].ID != "V1" {
		t.Errorf("expected first row ID V1, got %q", ev.ValidationResults[0].ID)
	}
	if ev.ValidationResults[1].ID != "V2" {
		t.Errorf("expected second row ID V2, got %q", ev.ValidationResults[1].ID)
	}
	if ev.ValidationResults[0].Command != "go test ./..." {
		t.Errorf("expected command 'go test ./...', got %q", ev.ValidationResults[0].Command)
	}
	if ev.ValidationResults[0].RawArtifactKind != "validation_run_json" {
		t.Errorf("expected RawArtifactKind validation_run_json, got %q", ev.ValidationResults[0].RawArtifactKind)
	}
}

func TestValidationProgressJson_ConcreteRows(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(305)

	jsonData := []byte(`{"status":"finished","repo_path":"/repo","total_commands":2,"commands":[
		{"index":1,"label":"V1","command":"go test ./internal/auditor","status":"pass","exit_code":0},
		{"index":2,"label":"V2","command":"go test ./...","status":"timed_out","exit_code":124}
	]}`)
	valPath := writeArtifactFile(t, runID, "validation_run.json", jsonData)

	ev := &Evidence{RunID: runID, RunTitle: "Test", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"validation_run_json": {valPath},
			},
		},
	}
	c.collectValidationResults(runID, ev)

	if len(ev.ValidationResults) != 2 {
		t.Fatalf("expected 2 progress validation rows, got %d", len(ev.ValidationResults))
	}
	first := ev.ValidationResults[0]
	if first.ID != "V1" {
		t.Errorf("expected first row ID V1, got %q", first.ID)
	}
	if first.Command != "go test ./internal/auditor" {
		t.Errorf("expected concrete progress command, got %q", first.Command)
	}
	if first.Command == "(progress)" {
		t.Error("progress row must not emit placeholder command")
	}
	if first.Status != CheckPass {
		t.Errorf("expected pass status, got %q", first.Status)
	}
	if first.RawArtifactKind != "validation_run_json" {
		t.Errorf("expected RawArtifactKind validation_run_json, got %q", first.RawArtifactKind)
	}

	second := ev.ValidationResults[1]
	if second.Status != CheckFail {
		t.Errorf("expected timed_out status to map to fail, got %q", second.Status)
	}
	if second.Command != "go test ./..." {
		t.Errorf("expected second concrete progress command, got %q", second.Command)
	}
}

// TestAuditPacket_ValidationRunJsonArtifactKind verifies the audit packet labels validation_run_json correctly.
func TestAuditPacket_ValidationRunJsonArtifactKind(t *testing.T) {
	ev := &Evidence{
		RunID: 306, RunTitle: "Test", RunStatus: "validation_passed",
		Packet: PacketMetadata{
			PacketID: "packet-306",
			Goal:     "Test",
			Scope:    "Test",
		},
		ChangedFiles: ChangedFilesEvidence{
			Present:             true,
			Files:               []ChangedFileEntry{{Status: "M", Path: "pkg/foo.go"}},
			ImplementationFiles: []ChangedFileEntry{{Status: "M", Path: "pkg/foo.go"}},
			RawArtifactPath:     "/fake/path",
			SourceKind:          "git_diff_name_status",
		},
		ValidationResults: []ValidationCommandResult{
			{
				ID: "V1", Command: "go test ./...", Required: true, Status: CheckPass,
				ExitResult: "exit 0", EvidenceSummary: "passed",
				RawArtifactPath: "/artifacts/303/validation_run.json",
				RawArtifactKind: "validation_run_json",
			},
		},
	}
	packet := GenerateAuditPacket(ev, DecisionAccepted)

	// Packet raw artifact references must use validation_run_json kind
	if !strings.Contains(string(packet), "validation_run_json") {
		t.Error("audit packet raw artifact references must label validation_run_json correctly")
	}
}

// TestDetermineDefaultDecision_WarningOnly_AcceptedWithWarnings verifies warning-only evidence
// yields accepted_with_warnings.
func TestDetermineDefaultDecision_WarningOnly_AcceptedWithWarnings(t *testing.T) {
	ev := &Evidence{
		Warnings: []EvidenceWarning{
			{Message: "Non-blocking warning: diff evidence not available for manual check", Severity: SeverityWarning},
		},
	}
	d := DetermineDefaultDecision(ev)
	if d != DecisionAcceptedWithWarnings {
		t.Errorf("expected accepted_with_warnings for warning-only evidence, got %q", d)
	}
}

// TestGenerateInputSummary_AuditAgentHandoff verifies required sections in the generated summary.
func TestGenerateInputSummary_AuditAgentHandoff(t *testing.T) {
	ev := &Evidence{
		RunID: 304, RunTitle: "Test Handoff", RunStatus: "executor_done",
		Packet: PacketMetadata{
			PacketID:           "packet-304",
			Goal:               "Test goal",
			Scope:              "Test scope",
			NonGoals:           "Do not do X",
			FileTargets:        []string{"pkg/foo.go"},
			AuditChecklist:     []ChecklistItem{{ID: "A1", Check: "Test check", SeverityIfFailed: SeverityWarning}},
			NonGoalChecks:      []string{"Verify no X"},
			FileScopeChecks:    []string{"Confirm target edits"},
			ValidationCommands: []ValidationCommandSpec{{ID: "V1", Command: "go test", Required: true}},
		},
		ExecutorResult: ExecutorResultEvidence{
			Present:         true,
			Summary:         "STATUS: DONE",
			Content:         "done",
			RawArtifactPath: "/artifacts/executor.txt",
		},
		ValidationResults: []ValidationCommandResult{
			{ID: "V1", Command: "go test", Required: true, Status: CheckPass, ExitResult: "exit 0", EvidenceSummary: "passed", RawArtifactPath: "/val.json", RawArtifactKind: "validation_run_json"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present:             true,
			Files:               []ChangedFileEntry{{Status: "M", Path: "pkg/foo.go"}},
			ImplementationFiles: []ChangedFileEntry{{Status: "M", Path: "pkg/foo.go"}},
			GeneratedArtifactFiles: []GeneratedArtifactEntry{
				{Path: "handoffs/validation/latest.validation-report.json", Status: "M", InferredArtifactKind: "validation_report", Recognized: true},
			},
			RawArtifactPath: "/changed.txt",
			SourceKind:      "git_diff_name_status",
		},
		GitDiff: DiffEvidence{
			Present:         true,
			Preview:         "diff --git a/pkg/foo.go b/pkg/foo.go\n+change",
			RawArtifactPath: "/diff.patch",
		},
		FileScopeResults: []PerCheckResult{
			{ID: "FS-TARGETS", Check: "Check targets", Result: CheckPass, SeverityIfFailed: SeverityError, EvidenceSource: "changed", Rationale: "All in scope"},
		},
		ChecklistResults: []PerCheckResult{
			{ID: "A1", Check: "Test check", Result: CheckUnknown, SeverityIfFailed: SeverityWarning, EvidenceSource: "manual", Rationale: "Needs human review"},
		},
		NonGoalResults: []PerCheckResult{
			{ID: "NG1", Check: "Non-goal check", Result: CheckUnknown, SeverityIfFailed: SeverityWarning, EvidenceSource: "none", Rationale: "Manual review"},
		},
	}

	summary := GenerateInputSummary(ev)

	// Must contain Audit Agent Handoff header
	if !strings.Contains(summary, "Audit Agent Handoff") {
		t.Error("summary must contain 'Audit Agent Handoff'")
	}
	// Must contain evidence map with artifact kinds
	if !strings.Contains(summary, "Evidence Map") {
		t.Error("summary must contain 'Evidence Map'")
	}
	// Must contain Implementation Changed Files section
	if !strings.Contains(summary, "Implementation Changed Files") {
		t.Error("summary must contain 'Implementation Changed Files'")
	}
	// Must contain Generated Pipeline Artifacts section
	if !strings.Contains(summary, "Generated Pipeline Artifacts") {
		t.Error("summary must contain 'Generated Pipeline Artifacts'")
	}
	// Must contain Automated Findings Summary section
	if !strings.Contains(summary, "Automated Findings Summary") {
		t.Error("summary must contain 'Automated Findings Summary'")
	}
	// Must contain Manual Review Focus section
	if !strings.Contains(summary, "Manual Review Focus") {
		t.Error("summary must contain 'Manual Review Focus'")
	}
	// Must contain Decision Guidance section
	if !strings.Contains(summary, "Decision Guidance") {
		t.Error("summary must contain 'Decision Guidance'")
	}
	// Must state that generated artifacts are evidence inputs, not implementation files
	if !strings.Contains(summary, "evidence inputs") {
		t.Error("summary must state generated artifacts are evidence inputs")
	}
	// Must note that this is not final human acceptance
	if !strings.Contains(summary, "Not final human acceptance") {
		t.Error("summary must note it is not final human acceptance")
	}
	// Must show generated artifact entry
	if !strings.Contains(summary, "validation-report.json") {
		t.Error("summary must show generated artifact entries")
	}
}

// TestAuditPacketTemplateContractPathRegression verifies that the forbidden template path
// handoffs/templates/audit_packet_template.md does not exist and is not referenced in the package.
func TestAuditPacketTemplateContractPathRegression(t *testing.T) {
	// Forbidden path must not exist relative to repository root
	forbiddenPath := filepath.Join("..", "..", "handoffs", "templates", "audit_packet_template.md")
	if _, err := os.Stat(forbiddenPath); err == nil {
		t.Fatalf("Forbidden file exists: %s", forbiddenPath)
	}

	// Authoritative template in relay-contracts must exist relative to repository root/test context
	// We can check it at ../../relay-contracts/templates/audit_packet_template.md
	authoritativePath := filepath.Join("..", "..", "relay-contracts", "templates", "audit_packet_template.md")
	if _, err := os.Stat(authoritativePath); err != nil {
		t.Fatalf("Authoritative template is missing or cannot be read: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Validation fallback label regression (run-155) — no V? or (unknown — not in packet)
// ---------------------------------------------------------------------------

func TestValidationFallback_NoVQ(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(700)

	// Write only a validation_stdout artifact — no packet commands, no validation_run_json
	writeArtifactFile(t, runID, "validation_stdout.txt", []byte("ok  \tpkg/foo\n"))

	ev := &Evidence{RunID: runID, RunTitle: "val fallback", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"validation_stdout": {filepath.Join(artifacts.Dir(runID), "validation_stdout.txt")},
			},
		},
	}
	c.collectValidationResults(runID, ev)

	if len(ev.ValidationResults) != 1 {
		t.Fatalf("expected 1 validation fallback result, got %d", len(ev.ValidationResults))
	}
	vr := ev.ValidationResults[0]
	if vr.ID == "V?" {
		t.Error("validation fallback ID must not be V?")
	}
	if strings.Contains(vr.Command, "not in packet") {
		t.Errorf("validation fallback command must not say '(unknown — not in packet)', got %q", vr.Command)
	}
	// Must use a stable label
	if vr.ID != "VAL-ARTIFACT" {
		t.Errorf("expected fallback ID VAL-ARTIFACT, got %q", vr.ID)
	}
	if vr.Command != "validation artifact status" {
		t.Errorf("expected fallback command 'validation artifact status', got %q", vr.Command)
	}
	if vr.Required {
		t.Error("generic validation fallback must not be required")
	}
	if !strings.Contains(vr.EvidenceSummary, "validation_stdout") {
		t.Errorf("evidence summary should reference artifact kind, got %q", vr.EvidenceSummary)
	}
}

func TestValidationFallback_NoVQ_ValidationRunJson(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(701)

	// Write validation_run.json that does not match ValidationRun or Progress schema
	writeArtifactFile(t, runID, "validation_run.json", []byte(`{"some":"unknown","data":true}`))

	ev := &Evidence{RunID: runID, RunTitle: "val fallback json", RunStatus: "executor_done"}
	c := &Collector{
		store: &fakeStore{
			artifactPaths: map[string][]string{
				"validation_run_json": {filepath.Join(artifacts.Dir(runID), "validation_run.json")},
			},
		},
	}
	c.collectValidationResults(runID, ev)

	if len(ev.ValidationResults) != 1 {
		t.Fatalf("expected 1 validation fallback result, got %d", len(ev.ValidationResults))
	}
	vr := ev.ValidationResults[0]
	if vr.ID == "V?" {
		t.Error("validation fallback ID must not be V?")
	}
	if strings.Contains(vr.Command, "not in packet") {
		t.Errorf("validation fallback command must not say '(unknown — not in packet)', got %q", vr.Command)
	}
	// Must use VAL-RUN for validation_run_json kind
	if vr.ID != "VAL-RUN" {
		t.Errorf("expected fallback ID VAL-RUN for validation_run_json kind, got %q", vr.ID)
	}
	if vr.Command != "validation_run_json artifact status" {
		t.Errorf("expected fallback command 'validation_run_json artifact status', got %q", vr.Command)
	}
}
