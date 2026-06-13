package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
)

func TestStartValidationWritesPlannedCommandRows(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test validation progress rendering.

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
go env GOPATH
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	agentResultPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte("DONE"))
	if err != nil {
		t.Fatalf("write agent result: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", agentResultPath, "text/plain")

	launchRecorded := false
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		launchRecorded = true
	}

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.startValidation(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if !launchRecorded {
		t.Fatal("expected validation worker launch to be recorded")
	}

	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read validation progress: %v", err)
	}
	var vp pipeline.ValidationProgress
	if err := json.Unmarshal(progressData, &vp); err != nil {
		t.Fatalf("unmarshal validation progress: %v", err)
	}
	if vp.Status != "starting" {
		t.Fatalf("expected starting status, got %s", vp.Status)
	}
	if len(vp.Commands) != 2 {
		t.Fatalf("expected 2 planned commands, got %d", len(vp.Commands))
	}
	for i, cmd := range vp.Commands {
		if cmd.Index != i+1 {
			t.Errorf("command %d index = %d, want %d", i, cmd.Index, i+1)
		}
		if cmd.Status != "pending" {
			t.Errorf("command %d status = %s, want pending", i, cmd.Status)
		}
	}
}

func TestExecuteValidationUpdatesProgressAndFinalizesPass(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test validation progress rendering.

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
go env GOPATH
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	agentResultPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte("DONE"))
	if err != nil {
		t.Fatalf("write agent result: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", agentResultPath, "text/plain")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	repo, err := s.GetRepo(run.RepoID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	handoffData, _ := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	commands := pipeline.ExtractValidationCommands(string(handoffData), "")

	execID, acquired, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("try create execution: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire execution")
	}

	initialProgress := pipeline.NewValidationProgressFromCommands(repo.Path, commands)
	initData, _ := json.MarshalIndent(initialProgress, "", "  ")
	initPath, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), initData)
	s.CreateArtifact(runID, "validation_progress_json", initPath, "application/json")

	var mu sync.Mutex
	var snapshots []pipeline.ValidationProgress
	writeProgress := func(p pipeline.ValidationProgress) {
		snap := p
		snap.Commands = append([]pipeline.ValidationProgressCommand(nil), p.Commands...)
		mu.Lock()
		snapshots = append(snapshots, snap)
		mu.Unlock()

		data, _ := json.MarshalIndent(p, "", "  ")
		h.store.DeleteArtifactsByRunKind(runID, "validation_progress_json")
		pth, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), data)
		if pth != "" {
			s.CreateArtifact(runID, "validation_progress_json", pth, "application/json")
		}
	}

	h.executeValidation(runID, execID, repo.Path, commands, writeProgress)

	if len(snapshots) < 4 {
		t.Fatalf("expected multiple progress snapshots, got %d", len(snapshots))
	}

	first := snapshots[0]
	if first.Status != "running" {
		t.Fatalf("expected first snapshot running, got %s", first.Status)
	}
	if first.CurrentIndex != 0 {
		t.Fatalf("expected first snapshot to have no active command yet, got %d", first.CurrentIndex)
	}
	if len(first.Commands) != 2 {
		t.Fatalf("expected 2 rows in first snapshot, got %d", len(first.Commands))
	}
	for i, cmd := range first.Commands {
		if cmd.Status != "pending" {
			t.Fatalf("first snapshot command %d status = %s, want pending", i, cmd.Status)
		}
	}

	var sawRunning, sawResult bool
	for _, snap := range snapshots {
		if snap.CurrentIndex == 1 && len(snap.Commands) > 0 && snap.Commands[0].Status == "running" {
			sawRunning = true
		}
		if len(snap.Commands) == 2 && snap.Commands[0].Status == "pass" && snap.Commands[1].Status == "pending" {
			sawResult = true
		}
	}
	if !sawRunning {
		t.Fatal("expected a snapshot with the first command running")
	}
	if !sawResult {
		t.Fatal("expected a snapshot with the first command completed and second still pending")
	}

	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read validation progress: %v", err)
	}
	var finalProgress pipeline.ValidationProgress
	if err := json.Unmarshal(progressData, &finalProgress); err != nil {
		t.Fatalf("unmarshal final progress: %v", err)
	}
	if finalProgress.Status != "pass" {
		t.Fatalf("expected final status pass, got %s", finalProgress.Status)
	}
	if finalProgress.FinishedAt == "" {
		t.Fatal("expected finished_at on final progress")
	}
	if len(finalProgress.Commands) != 2 {
		t.Fatalf("expected 2 final command rows, got %d", len(finalProgress.Commands))
	}
	for i, cmd := range finalProgress.Commands {
		if cmd.Status != "pass" {
			t.Fatalf("final command %d status = %s, want pass", i, cmd.Status)
		}
		if cmd.CompletedAt == "" {
			t.Fatalf("final command %d missing completed_at", i)
		}
	}
}

func TestExecuteValidationUpdatesProgressAndFinalizesFail(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test validation failure rendering.

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
this-command-should-not-exist-xyz
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	agentResultPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte("DONE"))
	if err != nil {
		t.Fatalf("write agent result: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", agentResultPath, "text/plain")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	repo, err := s.GetRepo(run.RepoID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	handoffData, _ := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	commands := pipeline.ExtractValidationCommands(string(handoffData), "")

	execID, acquired, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("try create execution: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire execution")
	}

	initialProgress := pipeline.NewValidationProgressFromCommands(repo.Path, commands)
	initData, _ := json.MarshalIndent(initialProgress, "", "  ")
	initPath, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), initData)
	s.CreateArtifact(runID, "validation_progress_json", initPath, "application/json")

	writeProgress := func(p pipeline.ValidationProgress) {
		data, _ := json.MarshalIndent(p, "", "  ")
		h.store.DeleteArtifactsByRunKind(runID, "validation_progress_json")
		pth, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), data)
		if pth != "" {
			s.CreateArtifact(runID, "validation_progress_json", pth, "application/json")
		}
	}

	h.executeValidation(runID, execID, repo.Path, commands, writeProgress)

	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read validation progress: %v", err)
	}
	var finalProgress pipeline.ValidationProgress
	if err := json.Unmarshal(progressData, &finalProgress); err != nil {
		t.Fatalf("unmarshal final progress: %v", err)
	}
	if finalProgress.Status != "fail" {
		t.Fatalf("expected final status fail, got %s", finalProgress.Status)
	}
	if len(finalProgress.Commands) != 2 {
		t.Fatalf("expected 2 final command rows, got %d", len(finalProgress.Commands))
	}
	if finalProgress.Commands[0].Status != "pass" {
		t.Fatalf("expected first command pass, got %s", finalProgress.Commands[0].Status)
	}
	if finalProgress.Commands[1].Status != "fail" {
		t.Fatalf("expected second command fail, got %s", finalProgress.Commands[1].Status)
	}
	if finalProgress.Commands[1].ExitCode == 0 {
		t.Fatal("expected non-zero exit code on failed command")
	}
}
