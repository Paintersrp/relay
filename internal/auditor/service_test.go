package auditor

import (
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

		// Read generated audit content from disk
		summaryContent, err := os.ReadFile(summaryArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_input_summary: %v", err)
		}
		packetContent, err := os.ReadFile(packetArts[0].Path)
		if err != nil {
			t.Fatalf("read audit_packet: %v", err)
		}

		// Assert acceptance evidence reference
		if !strings.Contains(string(summaryContent), "Validation Failure Acceptance") {
			t.Error("audit input summary should include Validation Failure Acceptance section")
		}
		if !strings.Contains(string(packetContent), "Validation Failure Acceptance") {
			t.Error("audit packet should include Validation Failure Acceptance section")
		}

		// Assert validation evidence references
		if !strings.Contains(string(packetContent), "validation_stdout") {
			t.Error("audit packet should reference validation_stdout")
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
		if !strings.Contains(string(packetContent), "validation_stdout") {
			t.Error("audit packet should reference validation_stdout")
		}
	})
}
