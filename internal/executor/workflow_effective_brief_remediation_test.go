package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

func TestVerifyInvocationUsesEffectiveBriefAdapterTransports(t *testing.T) {
	selected := testEffectiveBriefInput(t)
	for _, adapter := range []AdapterID{AdapterOpenCodeGo, AdapterCodex, AdapterKiroCLI} {
		t.Run(string(adapter), func(t *testing.T) {
			invocation := ExecutorInvocation{
				Adapter:     adapter,
				Stdin:       string(selected.Content),
				StdinSource: selected.Path,
				StdinBytes:  len(selected.Content),
			}
			if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
				t.Fatal(err)
			}
		})
	}

	antigravity := &AntigravityAdapter{Config: AntigravityAdapterConfig{Binary: "antigravity", ApproveFlag: "none"}}
	invocation, err := antigravity.BuildInvocation(ExecutorAdapterRequest{
		RepoPath:     t.TempDir(),
		BriefContent: string(selected.Content),
		BriefPath:    selected.Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err != nil {
		t.Fatal(err)
	}
	invocation.StdinSource = filepath.Join(t.TempDir(), "alternate-brief.md")
	if err := verifyInvocationUsesEffectiveBrief(invocation, selected); err == nil {
		t.Fatal("expected alternate path-based stdin source to be rejected")
	}
}

func testEffectiveBriefInput(t *testing.T) effectiveBriefInput {
	t.Helper()
	content := []byte("# Executor Brief\n\nUse the selected effective input.\n")
	digest := sha256.Sum256(content)
	return effectiveBriefInput{
		Mode:    speccompiler.EffectiveBriefResidual,
		Content: content,
		Artifact: workflowstore.Artifact{
			ArtifactID: "artifact-effective-brief",
			SHA256:     hex.EncodeToString(digest[:]),
			SizeBytes:  int64(len(content)),
		},
		Path: filepath.Join(t.TempDir(), "executor-residual-brief.md"),
	}
}
