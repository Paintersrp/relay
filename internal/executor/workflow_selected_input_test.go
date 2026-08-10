package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestVerifyInvocationCarriesInputAdapterTransports(t *testing.T) {
	selected := testSelectedInput(t)
	for _, adapter := range []AdapterID{AdapterOpenCodeGo, AdapterCodex, AdapterKiroCLI} {
		t.Run(string(adapter), func(t *testing.T) {
			invocation := ExecutorInvocation{
				Adapter:     adapter,
				Stdin:       string(selected.Content),
				StdinSource: selected.Path,
				StdinBytes:  len(selected.Content),
			}
			if err := verifyInvocationCarriesInput(invocation, selected); err != nil {
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
	if err := verifyInvocationCarriesInput(invocation, selected); err != nil {
		t.Fatal(err)
	}
	invocation.StdinSource = filepath.Join(t.TempDir(), "alternate-input.json")
	if err := verifyInvocationCarriesInput(invocation, selected); err == nil {
		t.Fatal("expected alternate path-based stdin source to be rejected")
	}
}

func testSelectedInput(t *testing.T) selectedInput {
	t.Helper()
	content := []byte("{\"schema_version\":\"1.0\"}\n")
	digest := sha256.Sum256(content)
	return selectedInput{
		Mode:    ExecutionModeAbsent,
		Content: content,
		Artifact: workflowstore.Artifact{
			ArtifactID: "artifact-selected-input",
			SHA256:     hex.EncodeToString(digest[:]),
			SizeBytes:  int64(len(content)),
		},
		Path: filepath.Join(t.TempDir(), "execution-assignment.json"),
	}
}
