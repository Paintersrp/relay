package audits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"
)

func workflowAuditAttemptResult(raw string) WorkflowAuditAttemptResult {
	var source struct {
		ExitCode                 int    `json:"exit_code"`
		TimedOut                 bool   `json:"timed_out"`
		TerminationVerified      bool   `json:"termination_verified"`
		CleanupPending           bool   `json:"cleanup_pending"`
		PendingTerminalStatus    string `json:"pending_terminal_status"`
		Error                    string `json:"error"`
		NormalizedStatus         string `json:"normalized_status"`
		BlockerText              string `json:"blocker_text"`
		EffectiveBriefArtifactID string `json:"effective_brief_artifact_id"`
		EffectiveBriefSHA256     string `json:"effective_brief_sha256"`
		EffectiveBriefMode       string `json:"effective_brief_mode"`
		StdoutTruncated          bool   `json:"stdout_truncated"`
		StderrTruncated          bool   `json:"stderr_truncated"`
		StdoutBytes              int64  `json:"stdout_bytes"`
		StderrBytes              int64  `json:"stderr_bytes"`
	}
	if json.Unmarshal([]byte(raw), &source) != nil {
		return WorkflowAuditAttemptResult{}
	}
	return WorkflowAuditAttemptResult{
		ExitCode: source.ExitCode, TimedOut: source.TimedOut, TerminationVerified: source.TerminationVerified,
		CleanupPending: source.CleanupPending, PendingTerminalStatus: source.PendingTerminalStatus,
		Error: executor.RedactSensitiveText(source.Error), NormalizedStatus: source.NormalizedStatus,
		BlockerText: executor.RedactSensitiveText(source.BlockerText), EffectiveBriefArtifactID: source.EffectiveBriefArtifactID,
		EffectiveBriefSHA256: source.EffectiveBriefSHA256, EffectiveBriefMode: source.EffectiveBriefMode,
		StdoutTruncated: source.StdoutTruncated, StderrTruncated: source.StderrTruncated,
		StdoutBytes: source.StdoutBytes, StderrBytes: source.StderrBytes,
	}
}

func readWorkflowArtifact(store *workflowstore.Store, artifact workflowstore.Artifact, maxBytes int) ([]byte, error) {
	path, err := workflowArtifactPath(store, artifact)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("artifact %s exceeds %d bytes", artifact.ArtifactID, maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != artifact.SizeBytes {
		return nil, fmt.Errorf("artifact %s size does not match metadata", artifact.ArtifactID)
	}
	if sha256HexBytes(data) != artifact.SHA256 {
		return nil, fmt.Errorf("artifact %s SHA-256 does not match metadata", artifact.ArtifactID)
	}
	return data, nil
}

func workflowArtifactPath(store *workflowstore.Store, artifact workflowstore.Artifact) (string, error) {
	root := store.ArtifactStore().Root()
	absolute := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes workflow artifact root")
	}
	return absolute, nil
}
