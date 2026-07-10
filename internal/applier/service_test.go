package applier

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/speccompiler"
)

type failingEvidenceWriter struct{}

func (failingEvidenceWriter) WriteEvidence(context.Context, EvidenceFile) (EvidenceArtifact, error) {
	return EvidenceArtifact{}, errors.New("disk unavailable")
}

func TestApplyNotAttemptedWithoutDeterministicOperations(t *testing.T) {
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeNotAttempted {
		t.Fatalf("expected not_attempted, got %s", result.Outcome)
	}
	if len(result.Ledger.Entries) != 0 {
		t.Fatalf("not-attempted result should not create ledger entries: %+v", result.Ledger.Entries)
	}
}

func TestApplyCreateAndReplaceWritesEvidence(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "internal/example/config.go"), "const enabled = false\n")
	writer := &MemoryEvidenceWriter{}
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{
				ID: "op-replace", Kind: "replace", Mode: "exact", Paths: []string{"internal/example/config.go"}, ExpectedOccurrences: 1,
				Payload: rawJSON(t, map[string]any{"old_text": "const enabled = false\n", "new_text": "const enabled = true\n"}),
			},
			{
				ID: "op-create", Kind: "create", Mode: "exact", Paths: []string{"internal/example/new.go"},
				Payload: rawJSON(t, map[string]any{"content": "package example\n"}),
			},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection, EvidenceWriter: writer})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeCompleted {
		t.Fatalf("expected completed, got %s: %+v", result.Outcome, result)
	}
	if got := string(mustRead(t, filepath.Join(root, "internal/example/config.go"))); got != "const enabled = true\n" {
		t.Fatalf("replace did not apply: %q", got)
	}
	if got := string(mustRead(t, filepath.Join(root, "internal/example/new.go"))); got != "package example\n" {
		t.Fatalf("create did not apply: %q", got)
	}
	if len(writer.Files) != 3 {
		t.Fatalf("expected ledger, result, and changed-file evidence, got %d files", len(writer.Files))
	}
	if len(result.ImplementationResult.CompletedOperations) != 2 {
		t.Fatalf("expected completed operation evidence, got %+v", result.ImplementationResult)
	}
}

func TestApplyRejectsUnsafePathBeforeMutation(t *testing.T) {
	root := t.TempDir()
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{ID: "op-unsafe", Kind: "create", Mode: "exact", Paths: []string{"../escape.go"}, Payload: rawJSON(t, map[string]any{"content": "package main\n"})},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeBlocked || result.FailurePacket == nil {
		t.Fatalf("expected blocked failure packet, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "..", "escape.go")); err == nil {
		t.Fatalf("unsafe operation created an escaped file")
	}
}

func TestApplyRejectsGitMetadataPathBeforeMutation(t *testing.T) {
	root := t.TempDir()
	gitHead := filepath.Join(root, ".git", "HEAD")
	mustWrite(t, gitHead, "ref: refs/heads/main\n")
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{
				ID: "op-git-head", Kind: "replace", Mode: "exact", Paths: []string{".git/HEAD"}, ExpectedOccurrences: 1,
				Payload: rawJSON(t, map[string]any{"old_text": "ref: refs/heads/main\n", "new_text": "ref: refs/heads/changed\n"}),
			},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeBlocked || result.FailurePacket == nil {
		t.Fatalf("expected blocked Git metadata operation, got %+v", result)
	}
	if got := string(mustRead(t, gitHead)); got != "ref: refs/heads/main\n" {
		t.Fatalf("git metadata changed: %q", got)
	}
}

func TestApplyOccurrenceMismatchBlocks(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config.txt"), "alpha\n")
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{ID: "op-replace", Kind: "replace", Mode: "exact", Paths: []string{"config.txt"}, ExpectedOccurrences: 2, Payload: rawJSON(t, map[string]any{"old_text": "alpha\n", "new_text": "beta\n"})},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeBlocked {
		t.Fatalf("expected blocked, got %+v", result)
	}
	if got := string(mustRead(t, filepath.Join(root, "config.txt"))); got != "alpha\n" {
		t.Fatalf("mismatched operation mutated file: %q", got)
	}
}

func TestApplyUnsupportedResidualPreservesExecutorAuthority(t *testing.T) {
	root := t.TempDir()
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{ID: "op-command", Kind: "run_command", Mode: "exact", Paths: []string{"config.txt"}, OnFailure: "residual"},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeNotAttempted {
		t.Fatalf("expected not_attempted for all-residual work, got %+v", result)
	}
	if len(result.ImplementationResult.ResidualOperations) != 1 || !result.ImplementationResult.ModelExecutorRequired {
		t.Fatalf("residual evidence missing: %+v", result.ImplementationResult)
	}
}

func TestApplyAtomicGroupFailurePreventsGroupMutation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a\n")
	projection := speccompiler.ExecutionPayloadProjection{
		OperationGroups: []speccompiler.ProjectedOperationGroup{
			{ID: "g1", Atomic: true},
		},
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{ID: "op-good", Kind: "replace", Mode: "exact", Paths: []string{"a.txt"}, Group: "g1", ExpectedOccurrences: 1, Payload: rawJSON(t, map[string]any{"old_text": "a\n", "new_text": "b\n"})},
			{ID: "op-bad", Kind: "replace", Mode: "exact", Paths: []string{"missing.txt"}, Group: "g1", ExpectedOccurrences: 1, Payload: rawJSON(t, map[string]any{"old_text": "x", "new_text": "y"})},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeBlocked {
		t.Fatalf("expected blocked atomic group, got %+v", result)
	}
	if got := string(mustRead(t, filepath.Join(root, "a.txt"))); got != "a\n" {
		t.Fatalf("atomic group mutated a ready member after group failure: %q", got)
	}
}

func TestApplyDependencyFailureSkipsDependentOperation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a\n")
	projection := speccompiler.ExecutionPayloadProjection{DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
		{ID: "op-bad", Kind: "replace", Mode: "exact", Paths: []string{"missing.txt"}, ExpectedOccurrences: 1, Payload: rawJSON(t, map[string]any{"old_text": "x", "new_text": "y"})},
		{ID: "op-dependent", Kind: "replace", Mode: "exact", Paths: []string{"a.txt"}, DependsOn: []string{"op-bad"}, ExpectedOccurrences: 1, Payload: rawJSON(t, map[string]any{"old_text": "a\n", "new_text": "b\n"})},
	}}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeBlocked {
		t.Fatalf("expected blocked dependency chain, got %+v", result)
	}
	if got := string(mustRead(t, filepath.Join(root, "a.txt"))); got != "a\n" {
		t.Fatalf("dependent operation mutated after dependency failure: %q", got)
	}
}

func TestApplyEvidenceWriterFailureBecomesEnvironmentFailure(t *testing.T) {
	root := t.TempDir()
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{ID: "op-create", Kind: "create", Mode: "exact", Paths: []string{"new.txt"}, Payload: rawJSON(t, map[string]any{"content": "new\n"})},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection, EvidenceWriter: failingEvidenceWriter{}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeBlocked || result.ImplementationResult.FailureClass != FailureClassEnvironment {
		t.Fatalf("expected environment failure after evidence write failure, got %+v", result)
	}
}

func TestApplyDoesNotMutateGitMetadata(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	projection := speccompiler.ExecutionPayloadProjection{
		DeterministicOperations: []speccompiler.ProjectedDeterministicOperation{
			{ID: "op-create", Kind: "create", Mode: "exact", Paths: []string{"new.txt"}, Payload: rawJSON(t, map[string]any{"content": "new\n"})},
		},
	}
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Outcome != OutcomeCompleted {
		t.Fatalf("expected completed, got %+v", result)
	}
	if got := string(mustRead(t, filepath.Join(root, ".git", "HEAD"))); got != "ref: refs/heads/main\n" {
		t.Fatalf("git metadata changed: %q", got)
	}
}

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw JSON: %v", err)
	}
	return data
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
