package auditor

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

// writeArtifact writes a file via artifacts.Write and creates the matching store artifact record.
func writeArtifact(t *testing.T, s *store.Store, runID int64, kind, filename string, content []byte, mimeType string) string {
	t.Helper()
	path, err := artifacts.Write(runID, kind, filename, content)
	if err != nil {
		t.Fatalf("write artifact %s/%s: %v", kind, filename, err)
	}
	_, err = s.CreateArtifact(runID, kind, path, mimeType)
	if err != nil {
		t.Fatalf("create artifact record %s: %v", kind, err)
	}
	return path
}

func TestService_Generate_Gating(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	artifacts.SetBaseDir(dir)

	repo, err := s.CreateRepo("test-repo", filepath.Join(dir, "repo"))
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	svc := NewService(s)

	t.Run("reject validation_failed", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run Failed Validation", "validation_failed", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Write validation artifacts to make sure it's rejected solely on status
		validationJsonPath, _ := artifacts.Write(run.ID, "validation_run_json", "validation_run.json", []byte(`{}`))
		s.CreateArtifact(run.ID, "validation_run_json", validationJsonPath, "application/json")
		stdoutPath, _ := artifacts.Write(run.ID, "validation_stdout", "validation.stdout", []byte(`out`))
		s.CreateArtifact(run.ID, "validation_stdout", stdoutPath, "text/plain")
		stderrPath, _ := artifacts.Write(run.ID, "validation_stderr", "validation.stderr", []byte(`err`))
		s.CreateArtifact(run.ID, "validation_stderr", stderrPath, "text/plain")

		_, err = svc.Generate(run.ID)
		if err == nil {
			t.Fatal("expected error for validation_failed run status, got nil")
		}
		expectedErrSub := "rerun validation or accept failed validation"
		if !strings.Contains(err.Error(), expectedErrSub) {
			t.Errorf("expected error message to contain %q, got %q", expectedErrSub, err.Error())
		}
	})

	t.Run("validation_failed_accepted requires validation_run_json and validation_failure_acceptance_json", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run Accepted Failed Validation", "validation_failed_accepted", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Missing validation_run_json
		_, err = svc.Generate(run.ID)
		if err == nil {
			t.Fatal("expected error when validation_run_json is missing")
		}

		// Write validation_run_json but missing validation_failure_acceptance_json
		jsonPath, _ := artifacts.Write(run.ID, "validation_run_json", "validation_run.json", []byte(`{}`))
		s.CreateArtifact(run.ID, "validation_run_json", jsonPath, "application/json")

		_, err = svc.Generate(run.ID)
		if err == nil {
			t.Fatal("expected error when validation_failure_acceptance_json is missing")
		}

		// Write validation_failure_acceptance_json
		writeArtifact(t, s, run.ID, "validation_failure_acceptance_json", "validation_failure_acceptance.json", []byte(`{"reason":"accepted","notes":"manual override"}`), "application/json")

		// Write executor result to allow general auditor collection to pass
		writeArtifact(t, s, run.ID, "executor_result", "executor_result.txt", []byte("STATUS: DONE\nBuild status: pass\nTest status: pass\nCount of LOC changed: 1\n"), "text/plain")

		// Write canonical packet so collector doesn't fail on reading it; record through CreateArtifact
		pktData := []byte(`{"execution_payload": {"goal": "test", "scope": "test", "non_goals": [], "file_targets": []}, "audit_seed": {"audit_checklist": []}}`)
		writeArtifact(t, s, run.ID, "canonical_packet", "canonical_packet.json", pktData, "application/json")

		// Write validation stdout/stderr so collectValidationResults has evidence
		writeArtifact(t, s, run.ID, "validation_stdout", "validation.stdout", []byte("ok  \tpkg/foo\n"), "text/plain")
		writeArtifact(t, s, run.ID, "validation_stderr", "validation.stderr", []byte(""), "text/plain")

		// Write git diff and changed files for collection completeness
		writeArtifact(t, s, run.ID, "git_diff_name_status", "git_diff_name_status.txt", []byte("M\tpkg/foo.go\n"), "text/plain")
		writeArtifact(t, s, run.ID, "git_diff_patch", "git_diff.patch", []byte("diff --git a/pkg/foo.go b/pkg/foo.go\n+// comment\n"), "text/plain")

		result, err := svc.Generate(run.ID)
		if err != nil {
			t.Fatalf("expected success with all artifacts, got error: %v", err)
		}
		if result == nil || result.RunID != run.ID {
			t.Fatal("expected non-nil GeneratedAudit with matching RunID")
		}

		// Assert audit_input_summary and audit_packet artifacts were created in store
		summaryArts, err := s.ListArtifactsByRunKind(run.ID, "audit_input_summary")
		if err != nil || len(summaryArts) == 0 {
			t.Fatal("expected audit_input_summary artifact in store")
		}
		packetArts, err := s.ListArtifactsByRunKind(run.ID, "audit_packet")
		if err != nil || len(packetArts) == 0 {
			t.Fatal("expected audit_packet artifact in store")
		}
		manifestArts, err := s.ListArtifactsByRunKind(run.ID, "audit_evidence_manifest_json")
		if err != nil || len(manifestArts) == 0 {
			t.Fatal("expected audit_evidence_manifest_json artifact in store")
		}

		// Read generated audit content from disk
		summaryContent, err := os.ReadFile(summaryArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_input_summary: %v", err)
		}
		packetContent, err := os.ReadFile(packetArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_packet: %v", err)
		}

		// Assert acceptance evidence reference in evidence map
		if !strings.Contains(string(summaryContent), "acceptance") && !strings.Contains(string(summaryContent), "Acceptance") {
			t.Error("audit input summary should include acceptance evidence in Evidence Map")
		}
		if !strings.Contains(string(packetContent), "Validation Failure Acceptance") {
			t.Error("audit packet should include Validation Failure Acceptance section")
		}

		// Assert validation evidence references — the validation_run_json artifact kind is used
		if !strings.Contains(string(packetContent), "validation_run_json") && !strings.Contains(string(packetContent), "validation_stdout") {
			t.Error("audit packet should reference validation artifact kind")
		}
	})

	t.Run("validation_passed generates audit artifacts with evidence references", func(t *testing.T) {
		run, err := s.CreateRun(repo.ID, "Run Validation Passed", "validation_passed", "gpt-4o", "gpt-4o", "main")
		if err != nil {
			t.Fatalf("failed to create run: %v", err)
		}

		// Write canonical_packet with required validation commands
		pktData := []byte(`{"execution_payload": {"goal": "test goal", "scope": "test scope", "non_goals": [], "file_targets": [], "validation_commands": [{"id": "V1", "command": "go test ./...", "required": true, "purpose": "Run tests", "success_signal": "ok", "failure_handling": "block"}]}, "audit_seed": {"audit_checklist": []}}`)
		writeArtifact(t, s, run.ID, "canonical_packet", "canonical_packet.json", pktData, "application/json")

		// Write validation artifacts
		jsonData := []byte(`{"runId":1,"status":"pass","commands":[{"id":"V1","command":"go test ./...","required":true,"status":"pass","exitCode":0,"stdoutKind":"validation_stdout","stderrKind":"validation_stderr"}]}`)
		writeArtifact(t, s, run.ID, "validation_run_json", "validation_run.json", jsonData, "application/json")
		writeArtifact(t, s, run.ID, "validation_stdout", "validation.stdout", []byte("ok  \tpkg/foo\n"), "text/plain")
		writeArtifact(t, s, run.ID, "validation_stderr", "validation.stderr", []byte(""), "text/plain")

		// Write executor result
		writeArtifact(t, s, run.ID, "executor_result", "executor_result.txt", []byte("STATUS: DONE\nBuild status: pass\nTest status: pass\nCount of LOC changed: 1\n"), "text/plain")

		// Write git diff and changed files for collection completeness
		writeArtifact(t, s, run.ID, "git_diff_name_status", "git_diff_name_status.txt", []byte("M\tpkg/foo.go\n"), "text/plain")
		writeArtifact(t, s, run.ID, "git_diff_patch", "git_diff.patch", []byte("diff --git a/pkg/foo.go b/pkg/foo.go\n+// comment\n"), "text/plain")

		result, err := svc.Generate(run.ID)
		if err != nil {
			t.Fatalf("expected success for validation_passed, got error: %v", err)
		}
		if result == nil || result.RunID != run.ID {
			t.Fatal("expected non-nil GeneratedAudit with matching RunID")
		}

		// Assert audit_input_summary and audit_packet artifacts were created in store
		summaryArts, err := s.ListArtifactsByRunKind(run.ID, "audit_input_summary")
		if err != nil || len(summaryArts) == 0 {
			t.Fatal("expected audit_input_summary artifact in store")
		}
		packetArts, err := s.ListArtifactsByRunKind(run.ID, "audit_packet")
		if err != nil || len(packetArts) == 0 {
			t.Fatal("expected audit_packet artifact in store")
		}
		manifestArts, err := s.ListArtifactsByRunKind(run.ID, "audit_evidence_manifest_json")
		if err != nil || len(manifestArts) == 0 {
			t.Fatal("expected audit_evidence_manifest_json artifact in store")
		}

		// Read generated audit content from disk
		summaryContent, err := os.ReadFile(summaryArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_input_summary: %v", err)
		}
		packetContent, err := os.ReadFile(packetArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_packet: %v", err)
		}

		// Assert validation evidence references in audit input summary
		if !strings.Contains(string(summaryContent), "V1") {
			t.Error("audit input summary should reference V1 validation command")
		}

		// Assert validation evidence references in audit packet
		if !strings.Contains(string(packetContent), "V1") {
			t.Error("audit packet should reference V1 validation command")
		}
		if !strings.Contains(string(packetContent), "validation_run_json") && !strings.Contains(string(packetContent), "validation_stdout") {
			t.Error("audit packet should reference validation artifact kind")
		}

		manifestContent, err := os.ReadFile(manifestArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_evidence_manifest_json: %v", err)
		}
		var manifest AuditEvidenceManifest
		if err := json.Unmarshal(manifestContent, &manifest); err != nil {
			t.Fatalf("unmarshal audit_evidence_manifest_json: %v", err)
		}
		if manifest.SchemaVersion != "1.0.0" {
			t.Fatalf("expected schema_version 1.0.0, got %q", manifest.SchemaVersion)
		}
		if !manifest.LocalOnly {
			t.Fatal("expected local_only=true")
		}
		if strings.Contains(string(manifestContent), "STATUS: DONE") {
			t.Fatal("manifest should not contain raw executor output")
		}
		if strings.Contains(string(manifestContent), "diff --git") {
			t.Fatal("manifest should not contain raw diff content")
		}
	})
}

// ---------------------------------------------------------------------------
// isNestedCheckoutMarker helper tests
// ---------------------------------------------------------------------------

func TestIsNestedCheckoutMarker(t *testing.T) {
	cases := []struct {
		path        string
		targets     []string
		expected    bool
		description string
	}{
		{
			path:        "relay-contracts",
			targets:     []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
			expected:    true,
			description: "nested checkout marker detected",
		},
		{
			path:        "internal/server/routes.go",
			targets:     []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
			expected:    false,
			description: "normal file with extension not a marker",
		},
		{
			path:        "",
			targets:     []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
			expected:    false,
			description: "empty path not a marker",
		},
		{
			path:        "relay-contracts",
			targets:     []string{"docs/mcp.md"},
			expected:    false,
			description: "path with no matching target prefix not a marker",
		},
		{
			path:        "relay-contracts/file.go",
			targets:     []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
			expected:    false,
			description: "file inside nested dir not a marker (has extension)",
		},
		{
			path:        "relay-contracts",
			targets:     nil,
			expected:    false,
			description: "no file targets means no marker",
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got := isNestedCheckoutMarker(tc.path, tc.targets)
			if got != tc.expected {
				t.Errorf("isNestedCheckoutMarker(%q, %v) = %v, want %v", tc.path, tc.targets, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Nested checkout marker regression tests
// ---------------------------------------------------------------------------

// TestNestedCheckout_CollapsedMarker_NoFalseFail verifies that a collapsed nested
// checkout marker (e.g. "M	relay-contracts") with no expanded nested evidence does
// not produce a file-scope failure.
func TestNestedCheckout_CollapsedMarker_NoFalseFail(t *testing.T) {
	ev := &Evidence{
		RunID: 500, RunTitle: "nested collapsed", RunStatus: "executor_done",
		Packet: PacketMetadata{
			FileTargets: []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts"},
			},
			ImplementationFiles:    nil,                       // filtered as nested marker
			NestedCheckoutMarkers:  []ChangedFileEntry{{Status: "M", Path: "relay-contracts"}},
			NestedCheckoutFiles:    nil,
			NestedEvidenceGap:      true,
			RawArtifactPath:        "/fake/path",
			SourceKind:             "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	for _, r := range ev.FileScopeResults {
		if r.Result == CheckFail && strings.Contains(r.Rationale, "relay-contracts") {
			t.Errorf("file scope must not name relay-contracts as out-of-scope when it is only a nested marker: %s", r.Rationale)
		}
		if r.Result == CheckFail {
			t.Errorf("collapsed nested marker must not produce file-scope failure, got %q: %s", r.ID, r.Rationale)
		}
		if r.Result == CheckUnknown && !strings.Contains(r.Rationale, "nested") {
			t.Errorf("collapsed nested marker should produce unknown with nested-evidence rationale, got: %s", r.Rationale)
		}
	}
}

// TestNestedCheckout_ExpandedEvidencePassing verifies that expanded nested changed
// files that match targets produce a file-scope pass.
func TestNestedCheckout_ExpandedEvidencePassing(t *testing.T) {
	ev := &Evidence{
		RunID: 501, RunTitle: "nested expanded pass", RunStatus: "executor_done",
		Packet: PacketMetadata{
			FileTargets: []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts"},
			},
			ImplementationFiles:   nil,
			NestedCheckoutMarkers: []ChangedFileEntry{{Status: "M", Path: "relay-contracts"}},
			NestedCheckoutFiles: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts/contracts/intent_drift_review_contract.md"},
			},
			NestedEvidenceGap:      false,
			RawArtifactPath:        "/fake/path",
			SourceKind:             "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	foundPass := false
	for _, r := range ev.FileScopeResults {
		if r.ID == "FS-TARGETS" && r.Result == CheckPass {
			foundPass = true
		}
	}
	if !foundPass {
		t.Errorf("expected FS-TARGETS pass for expanded nested file matching target, got: %+v", ev.FileScopeResults)
	}
}

// TestNestedCheckout_ExpandedEvidenceFailing verifies that expanded nested changed
// files that do NOT match targets produce a file-scope failure naming the nested file.
func TestNestedCheckout_ExpandedEvidenceFailing(t *testing.T) {
	ev := &Evidence{
		RunID: 502, RunTitle: "nested expanded fail", RunStatus: "executor_done",
		Packet: PacketMetadata{
			FileTargets: []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts"},
			},
			ImplementationFiles:   nil,
			NestedCheckoutMarkers: []ChangedFileEntry{{Status: "M", Path: "relay-contracts"}},
			NestedCheckoutFiles: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts/schema/planner_pass_plan.schema.json"},
			},
			NestedEvidenceGap:      false,
			RawArtifactPath:        "/fake/path",
			SourceKind:             "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	foundFailNamingNested := false
	for _, r := range ev.FileScopeResults {
		if r.Result == CheckFail && strings.Contains(r.Rationale, "relay-contracts/schema/planner_pass_plan.schema.json") {
			foundFailNamingNested = true
		}
	}
	if !foundFailNamingNested {
		t.Errorf("expected file-scope failure naming nested out-of-scope file, got: %+v", ev.FileScopeResults)
	}
}

// TestNestedCheckout_UnrelatedParentDrift verifies that a real out-of-scope parent
// file change still produces a file-scope failure even when file targets are nested.
func TestNestedCheckout_UnrelatedParentDrift(t *testing.T) {
	ev := &Evidence{
		RunID: 503, RunTitle: "parent drift", RunStatus: "executor_done",
		Packet: PacketMetadata{
			FileTargets: []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts"},
				{Status: "M", Path: "internal/server/routes.go"},
			},
			ImplementationFiles: []ChangedFileEntry{
				{Status: "M", Path: "internal/server/routes.go"},
			},
			NestedCheckoutMarkers:  []ChangedFileEntry{{Status: "M", Path: "relay-contracts"}},
			NestedCheckoutFiles:    nil,
			NestedEvidenceGap:      true,
			RawArtifactPath:        "/fake/path",
			SourceKind:             "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)

	foundFailNamingRoutes := false
	for _, r := range ev.FileScopeResults {
		if r.Result == CheckFail && strings.Contains(r.Rationale, "internal/server/routes.go") {
			foundFailNamingRoutes = true
		}
	}
	if !foundFailNamingRoutes {
		t.Errorf("expected file-scope failure naming internal/server/routes.go, got: %+v", ev.FileScopeResults)
	}
}

// TestNestedCheckout_ValidationSeparation verifies that validation evidence behavior
// remains separate from nested scope behavior.
func TestNestedCheckout_ValidationSeparation(t *testing.T) {
	ev := &Evidence{
		RunID: 504, RunTitle: "validation separation", RunStatus: "executor_done",
		Packet: PacketMetadata{
			FileTargets: []string{"relay-contracts/contracts/intent_drift_review_contract.md"},
		},
		ChangedFiles: ChangedFilesEvidence{
			Present: true,
			Files: []ChangedFileEntry{
				{Status: "M", Path: "relay-contracts"},
			},
			NestedCheckoutMarkers:  []ChangedFileEntry{{Status: "M", Path: "relay-contracts"}},
			NestedEvidenceGap:      true,
			RawArtifactPath:        "/fake/path",
			SourceKind:             "git_diff_name_status",
		},
	}
	c := &Collector{}
	c.evaluateFileScopeResults(ev)
	c.evaluateChecklistResults(ev)

	// Nested gap should produce unknown, not fail
	for _, r := range ev.FileScopeResults {
		if r.Result == CheckFail {
			t.Errorf("collapsed nested marker should not produce file-scope failure, got %q: %s", r.ID, r.Rationale)
		}
	}

	// Validation evidence should be separate; no validation available -> unknown
	if len(ev.ValidationResults) > 0 {
		t.Errorf("expected no validation results, got %d", len(ev.ValidationResults))
	}
}
