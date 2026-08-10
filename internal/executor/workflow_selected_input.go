package executor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

// selectedInput is the verified artifact content selected for one adaptive
// executor invocation. For the prepared package path it is the exact
// ExecutionAssignment artifact and bytes; the invocation must transport those
// bytes verbatim, never a generated markdown restatement.
type selectedInput struct {
	Mode     ExecutionMode
	Content  []byte
	Artifact workflowstore.Artifact
	Path     string
}

func verifyInvocationCarriesInput(invocation ExecutorInvocation, selected selectedInput) error {
	if selected.Artifact.ArtifactID == "" || selected.Artifact.SHA256 == "" || strings.TrimSpace(selected.Path) == "" || len(selected.Content) == 0 {
		return fmt.Errorf("selected input identity is incomplete")
	}
	digest := sha256.Sum256(selected.Content)
	if hex.EncodeToString(digest[:]) != selected.Artifact.SHA256 || int64(len(selected.Content)) != selected.Artifact.SizeBytes {
		return fmt.Errorf("selected input bytes do not match artifact identity")
	}
	if invocation.Stdin != "" {
		if !bytes.Equal([]byte(invocation.Stdin), selected.Content) {
			return fmt.Errorf("executor invocation changed the selected input bytes")
		}
		if invocation.StdinSource != selected.Path {
			return fmt.Errorf("executor invocation stdin source does not identify the selected input")
		}
		if invocation.StdinBytes != len(selected.Content) {
			return fmt.Errorf("executor invocation stdin size does not match the selected input")
		}
		return nil
	}
	matches := 0
	for _, arg := range invocation.Args {
		if arg == selected.Path {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("executor invocation must reference exactly one selected input path")
	}
	if invocation.StdinBytes != 0 {
		return fmt.Errorf("path-based executor invocation must not supply stdin bytes")
	}
	if !neutralStdinSource(invocation.StdinSource) {
		return fmt.Errorf("executor invocation identifies an alternate input source")
	}
	return nil
}

func neutralStdinSource(source string) bool {
	source = strings.TrimSpace(source)
	return source == "" || source == "/dev/null" || strings.EqualFold(source, "NUL") || source == os.DevNull
}
