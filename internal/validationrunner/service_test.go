package validationrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

type sealedStore struct {
	runStatus string
	repoPath  string
	artifacts []sealedArtifact
	events    []sealedEvent
	checks    []sealedCheck
	run       *store.Run
}

type sealedArtifact struct {
	runID    int64
	kind     string
	path     string
	mimeType string
}

type sealedEvent struct {
	level   string
	message string
}

type sealedCheck struct {
	kind        string
	status      string
	summary     string
	detailsJSON string
}

func (s *sealedStore) GetRun(id int64) (*store.Run, error) {
	if s.run == nil {
		s.run = &store.Run{ID: id, RepoID: 1, Title: "test", Status: s.runStatus}
	}
	return s.run, nil
}

func (s *sealedStore) GetRepo(id int64) (*store.Repo, error) {
	if s.repoPath == "" {
		s.repoPath = os.TempDir()
	}
	return &store.Repo{ID: id, Name: "test-repo", Path: s.repoPath}, nil
}

func (s *sealedStore) CreateArtifact(runID int64, kind, path, mimeType string) (*store.Artifact, error) {
	s.artifacts = append(s.artifacts, sealedArtifact{runID: runID, kind: kind, path: path, mimeType: mimeType})
	return &store.Artifact{}, nil
}

func (s *sealedStore) CreateEvent(runID int64, level, message string) (*store.Event, error) {
	s.events = append(s.events, sealedEvent{level: level, message: message})
	return &store.Event{}, nil
}

func (s *sealedStore) CreateCheck(runID int64, kind, status, summary, detailsJSON string) (*store.Check, error) {
	s.checks = append(s.checks, sealedCheck{kind: kind, status: status, summary: summary, detailsJSON: detailsJSON})
	return &store.Check{}, nil
}

func (s *sealedStore) UpdateRunStatus(id int64, status string) (*store.Run, error) {
	s.runStatus = status
	if s.run == nil {
		s.run = &store.Run{ID: id, RepoID: 1, Title: "test", Status: status}
	} else {
		s.run.Status = status
	}
	return s.run, nil
}

func (s *sealedStore) ListArtifactsByRunKind(runID int64, kind string) ([]store.Artifact, error) {
	return nil, nil
}

func (s *sealedStore) DeleteArtifactsByRunKind(runID int64, kind string) error {
	return nil
}

func mustCanonicalPacket(t *testing.T, commands []map[string]interface{}) {
	t.Helper()
	pkt := map[string]interface{}{
		"execution_payload": map[string]interface{}{
			"goal":                "test",
			"scope":               "test",
			"validation_commands": commands,
		},
	}
	data, _ := json.Marshal(pkt)
	err := os.MkdirAll(artifacts.Dir(1), 0755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	err = os.WriteFile(filepath.Join(artifacts.Dir(1), "canonical_packet.json"), data, 0644)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestService_RunValidation_NoCommands(t *testing.T) {
	artifacts.SetBaseDir(t.TempDir())
	mustCanonicalPacket(t, nil)

	svc := &Service{
		store: &sealedStore{runStatus: "executor_done"},
	}

	result, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation error: %v", err)
	}
	if result.Status != StatusPassed {
		t.Errorf("expected StatusPassed for no commands, got %s", result.Status)
	}
}

func TestService_RunValidation_Pass(t *testing.T) {
	artifacts.SetBaseDir(t.TempDir())
	mustCanonicalPacket(t, []map[string]interface{}{
		{"id": "V1", "command": "echo hello", "required": true, "purpose": "test"},
	})

	svc := &Service{
		store: &sealedStore{runStatus: "executor_done"},
	}

	result, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation error: %v", err)
	}
	if result.Status != StatusPassed {
		t.Errorf("expected StatusPassed, got %s", result.Status)
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected 1 command result, got %d", len(result.Commands))
	}
	if result.Commands[0].Status != "pass" {
		t.Errorf("expected command status pass, got %s", result.Commands[0].Status)
	}
	if result.Commands[0].StdoutKind != ArtifactKindStdout {
		t.Error("expected StdoutKind to be set")
	}
	if result.Commands[0].StderrKind != ArtifactKindStderr {
		t.Error("expected StderrKind to be set")
	}
}

func TestService_RunValidation_Fail(t *testing.T) {
	artifacts.SetBaseDir(t.TempDir())
	mustCanonicalPacket(t, []map[string]interface{}{
		{"id": "V1", "command": "exit 1", "required": true, "purpose": "test"},
	})

	svc := &Service{
		store: &sealedStore{runStatus: "executor_done"},
	}

	result, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Errorf("expected StatusFailed, got %s", result.Status)
	}
}

func TestService_RunValidation_StructuredJSONArtifact(t *testing.T) {
	baseDir := t.TempDir()
	artifacts.SetBaseDir(baseDir)
	mustCanonicalPacket(t, []map[string]interface{}{
		{"id": "V1", "command": "echo hello", "required": true, "purpose": "test"},
	})

	svc := &Service{
		store: &sealedStore{runStatus: "executor_done"},
	}

	_, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation: %v", err)
	}

	jsonPath := filepath.Join(baseDir, "1", validationRunFilename)
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("validation_run.json not found: %v", err)
	}
	data, _ := os.ReadFile(jsonPath)
	var vr ValidationRun
	if err := json.Unmarshal(data, &vr); err != nil {
		t.Fatalf("unmarshal validation_run.json: %v", err)
	}
	if vr.RunID != 1 {
		t.Errorf("expected RunID 1, got %d", vr.RunID)
	}
	if len(vr.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(vr.Commands))
	}
}

func TestService_RunValidation_EmptyStreamsPersisted(t *testing.T) {
	baseDir := t.TempDir()
	artifacts.SetBaseDir(baseDir)
	mustCanonicalPacket(t, []map[string]interface{}{
		{"id": "V1", "command": "cd .", "required": true, "purpose": "test"},
	})

	svc := &Service{
		store: &sealedStore{runStatus: "executor_done"},
	}

	_, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation: %v", err)
	}

	stdoutPath := filepath.Join(baseDir, "1", "validation_stdout_V1.txt")
	stderrPath := filepath.Join(baseDir, "1", "validation_stderr_V1.txt")

	if _, err := os.Stat(stdoutPath); err != nil {
		t.Errorf("stdout artifact should exist even when empty: %v", err)
	} else {
		data, _ := os.ReadFile(stdoutPath)
		if !strings.Contains(string(data), "[empty output]") {
			t.Logf("stdout content: %q", string(data))
		}
	}
	if _, err := os.Stat(stderrPath); err != nil {
		t.Errorf("stderr artifact should exist even when empty: %v", err)
	}
}

func TestService_RunValidation_AllArtifactKinds(t *testing.T) {
	baseDir := t.TempDir()
	artifacts.SetBaseDir(baseDir)
	mustCanonicalPacket(t, []map[string]interface{}{
		{"id": "V1", "command": "echo hello", "required": true, "purpose": "test"},
	})

	st := &sealedStore{runStatus: "executor_done"}
	svc := &Service{store: st}

	_, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation: %v", err)
	}

	hasStdout := false
	hasStderr := false
	hasJSON := false
	for _, a := range st.artifacts {
		switch a.kind {
		case ArtifactKindStdout:
			hasStdout = true
		case ArtifactKindStderr:
			hasStderr = true
		case ArtifactKindJSON:
			hasJSON = true
		}
	}
	if !hasStdout {
		t.Error("expected stdout artifact to be created")
	}
	if !hasStderr {
		t.Error("expected stderr artifact to be created")
	}
	if !hasJSON {
		t.Error("expected JSON artifact to be created")
	}
}

func TestService_HasValidationArtifacts_Pass(t *testing.T) {
	baseDir := t.TempDir()
	artifacts.SetBaseDir(baseDir)
	mustCanonicalPacket(t, []map[string]interface{}{
		{"id": "V1", "command": "echo hello", "required": true, "purpose": "test"},
	})

	svc := &Service{
		store: &sealedStore{runStatus: "executor_done"},
	}

	// Before running validation, no artifacts should exist
	svc2 := &Service{store: &sealedStore{}}
	if svc2.HasValidationArtifacts(1) {
		t.Error("expected HasValidationArtifacts to be false before validation")
	}

	_, err := svc.RunValidation(context.Background(), 1)
	if err != nil {
		t.Fatalf("RunValidation: %v", err)
	}
}
