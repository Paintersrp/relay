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
		acceptancePath, _ := artifacts.Write(run.ID, "validation_failure_acceptance_json", "validation_failure_acceptance.json", []byte(`{}`))
		s.CreateArtifact(run.ID, "validation_failure_acceptance_json", acceptancePath, "application/json")

		// Write executor result to allow general auditor collection to pass
		execResultPath, _ := artifacts.Write(run.ID, "executor_result", "executor_result.txt", []byte("STATUS: DONE\nBuild status: pass\nTest status: pass\nCount of LOC changed: 1\n"))
		s.CreateArtifact(run.ID, "executor_result", execResultPath, "text/plain")

		// Write canonical packet so collector doesn't fail on reading it
		pktData := []byte(`{"execution_payload": {"goal": "test", "scope": "test", "non_goals": [], "file_targets": []}, "audit_seed": {"audit_checklist": []}}`)
		artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", pktData)

		_, err = svc.Generate(run.ID)
		if err != nil {
			t.Fatalf("expected success with both artifacts, got error: %v", err)
		}
	})
}
