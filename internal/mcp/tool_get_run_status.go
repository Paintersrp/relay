package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// getRunStatusInput is the expected input for get_run_status.
type getRunStatusInput struct {
	// RunID is the numeric Relay run identifier. Required.
	RunID string `json:"run_id"`
}

// eventSummary is a bounded event entry returned by get_run_status.
type eventSummary struct {
	Message   string `json:"message"`
	Level     string `json:"level"`
	CreatedAt string `json:"created_at"`
}

// runStatusOutput is the bounded snapshot returned by get_run_status.
// Full artifact contents and logs are never included.
type runStatusOutput struct {
	OK                   bool           `json:"ok"`
	Tool                 string         `json:"tool"`
	RunID                string         `json:"run_id"`
	Title                string         `json:"title"`
	Repo                 string         `json:"repo"`
	Branch               string         `json:"branch"`
	Status               string         `json:"status"`
	LifecycleState       string         `json:"lifecycle_state"`
	ActiveStep           string         `json:"active_step"`
	ArtifactKinds        []string       `json:"artifact_kinds"`
	LatestEventSummaries []eventSummary `json:"latest_event_summaries"`
	ReviewURL            string         `json:"review_url"`
}

// HandleGetRunStatus implements the get_run_status MCP tool.
func (s *Server) HandleGetRunStatus(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolErr("DEPENDENCY_ERROR: MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	var input getRunStatusInput
	if err := json.Unmarshal(rawArgs, &input); err != nil {
		return toolErr(fmt.Sprintf("invalid arguments: %s", err))
	}

	if input.RunID == "" {
		return toolErr("VALIDATION_ERROR: run_id is required and must not be empty")
	}

	runIDInt, err := strconv.ParseInt(input.RunID, 10, 64)
	if err != nil {
		return toolErr(fmt.Sprintf("VALIDATION_ERROR: run_id must be a numeric string, got %q", input.RunID))
	}

	run, err := s.deps.Store.GetRun(runIDInt)
	if err != nil {
		return toolErr(fmt.Sprintf("NOT_FOUND: run %q not found: %s", input.RunID, err))
	}

	// Look up repo name from the store.
	repoName := strconv.FormatInt(run.RepoID, 10)
	if repo, rerr := s.deps.Store.GetRepo(run.RepoID); rerr == nil && repo != nil {
		repoName = repo.Name
	}

	// Collect artifact kinds (bounded: only kinds, not contents).
	dbArtifacts, _ := s.deps.Store.ListArtifactsByRun(runIDInt)
	kindSet := map[string]bool{}
	artifactKinds := []string{}
	for _, a := range dbArtifacts {
		if !kindSet[a.Kind] {
			kindSet[a.Kind] = true
			artifactKinds = append(artifactKinds, a.Kind)
		}
	}

	// Collect latest 10 events (bounded: message, level, timestamp only).
	dbEvents, _ := s.deps.Store.ListEventsByRun(runIDInt)
	start := 0
	if len(dbEvents) > 10 {
		start = len(dbEvents) - 10
	}
	eventSummaries := make([]eventSummary, 0, 10)
	for _, e := range dbEvents[start:] {
		eventSummaries = append(eventSummaries, eventSummary{
			Message:   e.Message,
			Level:     e.Level,
			CreatedAt: e.CreatedAt,
		})
	}

	lifecycleState := lifecycleStateFromStatus(run.Status)
	activeStep := activeStepFromStatus(run.Status)
	idStr := strconv.FormatInt(run.ID, 10)

	result := runStatusOutput{
		OK:                   true,
		Tool:                 "get_run_status",
		RunID:                idStr,
		Title:                run.Title,
		Repo:                 repoName,
		Branch:               run.BranchName,
		Status:               run.Status,
		LifecycleState:       lifecycleState,
		ActiveStep:           activeStep,
		ArtifactKinds:        artifactKinds,
		LatestEventSummaries: eventSummaries,
		ReviewURL:            fmt.Sprintf("/runs/%s/intake", idStr),
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

// activeStepFromStatus derives the workflow step label from a canonical run status.
func activeStepFromStatus(status string) string {
	switch status {
	case "draft", "intake_received", "intake_needs_review", "validated", "approved_for_prepare", "blocked":
		return "intake"
	case "packet_validated", "repair_validated", "packet_validation_failed", "brief_ready_for_review":
		return "prepare"
	case "approved_for_executor", "executor_dispatched", "executor_done", "executor_blocked",
		"agent_done", "agent_blocked", "agent_result_needs_review":
		return "execute"
	case "audit_ready", "audit_ready_for_review", "revision_required",
		"accepted", "accepted_with_warnings",
		"validation_passed", "validation_failed_accepted", "validation_failed", "completed":
		return "audit"
	default:
		return "intake"
	}
}

// getRunStatusSchema is the JSON Schema for get_run_status.
var getRunStatusSchema = json.RawMessage(`{
  "type": "object",
  "required": ["run_id"],
  "properties": {
    "run_id": {
      "type": "string",
      "description": "Numeric Relay run identifier (e.g., '42'). Obtain from list_open_runs or create_run_from_planner_handoff."
    }
  }
}`)

// ToolGetRunStatus is the ToolDefinition for get_run_status.
var ToolGetRunStatus = ToolDefinition{
	Name: "get_run_status",
	Description: "Get a bounded status snapshot for a specific Relay run. " +
		"Returns run_id, title, repo, branch, status, lifecycle_state, active_step, " +
		"artifact_kinds (names only), and the latest 10 event summaries. " +
		"Use this before deciding the next chat-derived handback action (e.g., submit_audit_packet). " +
		"Does not return full artifact contents, logs, diffs, or secrets.",
	InputSchema: getRunStatusSchema,
}
