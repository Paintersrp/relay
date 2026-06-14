package handlers

import (
	"context"
	"encoding/json"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/views"
)

const openCodeLifecycleDiagnosticArtifactKind = "opencode_lifecycle_diagnostic_json"

type processProbeResult struct {
	Alive bool
	Known bool
	Error string
}

var probeProcessAliveFunc = probeProcessAlive

type openCodeLifecycleDiagnostic struct {
	Version int `json:"version"`

	RunID       int64  `json:"run_id"`
	ExecutionID int64  `json:"execution_id"`
	Command     string `json:"command,omitempty"`
	WorkDir     string `json:"work_dir,omitempty"`
	Model       string `json:"model,omitempty"`
	Agent       string `json:"agent,omitempty"`

	PID int `json:"pid,omitempty"`

	CreatedAt                 string `json:"created_at,omitempty"`
	CommandPreparedAt         string `json:"command_prepared_at,omitempty"`
	CommandStartCalledAt      string `json:"command_start_called_at,omitempty"`
	CommandStartReturnedAt    string `json:"command_start_returned_at,omitempty"`
	CommandStartError         string `json:"command_start_error,omitempty"`
	StdoutReaderStartedAt     string `json:"stdout_reader_started_at,omitempty"`
	StderrReaderStartedAt     string `json:"stderr_reader_started_at,omitempty"`
	StdoutReaderDoneAt        string `json:"stdout_reader_done_at,omitempty"`
	StderrReaderDoneAt        string `json:"stderr_reader_done_at,omitempty"`
	StdoutReaderError         string `json:"stdout_reader_error,omitempty"`
	StderrReaderError         string `json:"stderr_reader_error,omitempty"`
	StdoutChunkCount          int64  `json:"stdout_chunk_count"`
	StderrChunkCount          int64  `json:"stderr_chunk_count"`
	StdoutByteCount           int64  `json:"stdout_byte_count"`
	StderrByteCount           int64  `json:"stderr_byte_count"`
	LastStdoutChunkAt         string `json:"last_stdout_chunk_at,omitempty"`
	LastStderrChunkAt         string `json:"last_stderr_chunk_at,omitempty"`
	LastAnyChunkAt            string `json:"last_any_chunk_at,omitempty"`
	LastActivityText          string `json:"last_activity_text,omitempty"`
	WaitStartedAt             string `json:"wait_started_at,omitempty"`
	WaitReturnedAt            string `json:"wait_returned_at,omitempty"`
	WaitError                 string `json:"wait_error,omitempty"`
	ProcessState              string `json:"process_state,omitempty"`
	ExitCode                  *int   `json:"exit_code,omitempty"`
	FinalResultParseStartedAt string `json:"final_result_parse_started_at,omitempty"`
	FinalResultParseEndedAt   string `json:"final_result_parse_ended_at,omitempty"`
	FinalResultStatus         string `json:"final_result_status,omitempty"`
	FinalResultRawPreview     string `json:"final_result_raw_preview,omitempty"`
	StoreFinalizeStartedAt    string `json:"store_finalize_started_at,omitempty"`
	StoreFinalizeEndedAt      string `json:"store_finalize_ended_at,omitempty"`
	StoreFinalizeError        string `json:"store_finalize_error,omitempty"`
	LatestStoreStatus         string `json:"latest_store_status,omitempty"`
	LatestStoreFinishedAt     string `json:"latest_store_finished_at,omitempty"`
	SelectedExecutionID       int64  `json:"selected_execution_id,omitempty"`
	ControlPresent            bool   `json:"control_present"`
	ControlDone               bool   `json:"control_done"`
	ProcessAlive              *bool  `json:"process_alive,omitempty"`
	ProcessProbeAt            string `json:"process_probe_at,omitempty"`
	ProcessProbeErr           string `json:"process_probe_error,omitempty"`
	LastLifecycleComputedAt   string `json:"last_lifecycle_computed_at,omitempty"`
	LastLifecycleState        string `json:"last_lifecycle_state,omitempty"`
	DiagnosticClassification  string `json:"diagnostic_classification,omitempty"`
	DiagnosticSummary         string `json:"diagnostic_summary,omitempty"`
}

func readOpenCodeLifecycleDiagnostic(id int64) (openCodeLifecycleDiagnostic, bool) {
	data, err := artifacts.Read(id, openCodeLifecycleDiagnosticArtifactKind, pipeline.ArtifactFilename(openCodeLifecycleDiagnosticArtifactKind))
	if err != nil {
		return openCodeLifecycleDiagnostic{}, false
	}
	var diag openCodeLifecycleDiagnostic
	if err := json.Unmarshal(data, &diag); err != nil {
		return openCodeLifecycleDiagnostic{}, false
	}
	if diag.Version == 0 {
		diag.Version = 1
	}
	return diag, true
}

func (h *RunsHandler) writeOpenCodeLifecycleDiagnostic(ctx context.Context, runID, execID int64, mutate func(*openCodeLifecycleDiagnostic)) {
	if h == nil {
		return
	}

	diag, ok := readOpenCodeLifecycleDiagnostic(runID)
	now := executionTimestampNow()
	if !ok {
		diag = openCodeLifecycleDiagnostic{
			Version:                 1,
			RunID:                   runID,
			ExecutionID:             execID,
			CreatedAt:               now,
			CommandPreparedAt:       now,
			LastLifecycleComputedAt: now,
		}
	}
	if diag.Version == 0 {
		diag.Version = 1
	}
	if diag.RunID == 0 {
		diag.RunID = runID
	}
	if diag.ExecutionID == 0 {
		diag.ExecutionID = execID
	}
	if diag.CreatedAt == "" {
		diag.CreatedAt = now
	}
	if diag.CommandPreparedAt == "" {
		diag.CommandPreparedAt = now
	}

	if mutate != nil {
		mutate(&diag)
	}

	diag.LastLifecycleComputedAt = now
	diag.DiagnosticClassification, diag.DiagnosticSummary = classifyOpenCodeLifecycleDiagnostic(diag)

	data, err := json.MarshalIndent(diag, "", "  ")
	if err != nil {
		if h.log != nil {
			h.log.Warn("marshal opencode lifecycle diagnostic", "run_id", runID, "exec_id", execID, "error", err)
		}
		return
	}

	path, err := artifacts.Write(runID, openCodeLifecycleDiagnosticArtifactKind, pipeline.ArtifactFilename(openCodeLifecycleDiagnosticArtifactKind), data)
	if err != nil {
		if h.log != nil {
			h.log.Warn("write opencode lifecycle diagnostic", "run_id", runID, "exec_id", execID, "error", err)
		}
		return
	}

	records, err := h.store.ListArtifactsByRunKind(runID, openCodeLifecycleDiagnosticArtifactKind)
	if err == nil && len(records) == 0 {
		if _, err := h.store.CreateArtifact(runID, openCodeLifecycleDiagnosticArtifactKind, path, "application/json"); err != nil && h.log != nil {
			h.log.Warn("record opencode lifecycle diagnostic artifact", "run_id", runID, "exec_id", execID, "error", err)
		}
	} else if err != nil && h.log != nil {
		h.log.Warn("check opencode lifecycle diagnostic artifact record", "run_id", runID, "exec_id", execID, "error", err)
	}
}

func applyOpenCodeLifecycleDiagnosticToPreviews(previews *views.RunPreviews, diag openCodeLifecycleDiagnostic) {
	if previews == nil {
		return
	}

	previews.HasOpenCodeLifecycleDiagnostic = true
	previews.OpenCodeLifecycleDiagnosticClassification = diag.DiagnosticClassification
	previews.OpenCodeLifecycleDiagnosticSummary = diag.DiagnosticSummary
	previews.OpenCodeLifecycleDiagnosticPID = diag.PID
	previews.OpenCodeLifecycleDiagnosticWaitStartedAt = diag.WaitStartedAt
	previews.OpenCodeLifecycleDiagnosticWaitReturnedAt = diag.WaitReturnedAt
	previews.OpenCodeLifecycleDiagnosticStoreFinalizedAt = diag.StoreFinalizeEndedAt
	previews.OpenCodeLifecycleDiagnosticSelectedExecutionID = diag.SelectedExecutionID
	previews.OpenCodeLifecycleDiagnosticLatestStoreStatus = diag.LatestStoreStatus
	previews.OpenCodeLifecycleDiagnosticLatestStoreFinishedAt = diag.LatestStoreFinishedAt
	if diag.LastAnyChunkAt != "" {
		previews.OpenCodeLifecycleDiagnosticLastChunkAt = diag.LastAnyChunkAt
	} else if diag.LastStdoutChunkAt != "" {
		previews.OpenCodeLifecycleDiagnosticLastChunkAt = diag.LastStdoutChunkAt
	} else if diag.LastStderrChunkAt != "" {
		previews.OpenCodeLifecycleDiagnosticLastChunkAt = diag.LastStderrChunkAt
	}
	if diag.ProcessAlive != nil {
		if *diag.ProcessAlive {
			previews.OpenCodeLifecycleDiagnosticProcessAlive = "alive"
		} else {
			previews.OpenCodeLifecycleDiagnosticProcessAlive = "not alive"
		}
	} else if diag.ProcessProbeErr != "" {
		previews.OpenCodeLifecycleDiagnosticProcessAlive = "unknown: " + diag.ProcessProbeErr
	} else {
		previews.OpenCodeLifecycleDiagnosticProcessAlive = "unknown"
	}
}

func classifyOpenCodeLifecycleDiagnostic(diag openCodeLifecycleDiagnostic) (string, string) {
	if strings.TrimSpace(diag.CommandStartError) != "" {
		return "process_start_failed", "OpenCode failed to start: " + strings.TrimSpace(diag.CommandStartError)
	}
	if strings.TrimSpace(diag.CommandStartReturnedAt) == "" {
		return "not_started", "OpenCode has not begun execution yet."
	}
	if strings.TrimSpace(diag.WaitStartedAt) != "" && strings.TrimSpace(diag.WaitReturnedAt) == "" {
		if diag.ProcessAlive != nil {
			if *diag.ProcessAlive {
				return "process_alive_waiting", "Relay is waiting for cmd.Wait and the process is still alive."
			}
			return "process_exited_wait_blocked", "The process appears to have exited, but Relay is still waiting for cmd.Wait or stream completion."
		}
		return "unknown", "Relay is waiting for cmd.Wait, but process liveness is not known yet."
	}
	if strings.TrimSpace(diag.WaitReturnedAt) != "" && strings.TrimSpace(diag.StoreFinalizeEndedAt) == "" {
		return "wait_returned_finalize_missing", "cmd.Wait returned, but Relay has not persisted the terminal execution state yet."
	}
	if strings.TrimSpace(diag.StoreFinalizeEndedAt) != "" && strings.TrimSpace(diag.LatestStoreFinishedAt) != "" {
		if openCodeLifecycleStateIsRunningLike(diag.LastLifecycleState) {
			return "finalized_ui_stale", "Relay persisted the terminal execution state, but the UI still reflects a running lifecycle state."
		}
		return "completed", "Relay persisted the terminal execution state and the lifecycle view is aligned."
	}
	return "unknown", "Relay has some lifecycle evidence, but the stuck point is not yet clear."
}

func openCodeLifecycleStateIsRunningLike(state string) bool {
	switch strings.TrimSpace(state) {
	case "running", "waiting_response", "active_streaming", "active_output", "running_no_output":
		return true
	default:
		return false
	}
}

func openCodeDiagnosticTextPreview(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func probeProcessAlive(pid int) processProbeResult {
	if pid <= 0 {
		return processProbeResult{Known: false, Error: "invalid pid"}
	}
	return probeProcessAlivePlatform(pid)
}
