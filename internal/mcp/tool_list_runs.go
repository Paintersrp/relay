package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// listRunsInput is the expected input for list_open_runs.
type listRunsInput struct {
	// Limit caps the number of runs returned. Default 10, max 25.
	Limit int `json:"limit,omitempty"`
}

// runSummary is a bounded snapshot of a single run returned by list_open_runs.
// Full artifact contents and logs are never included.
type runSummary struct {
	RunID          string `json:"run_id"`
	Title          string `json:"title"`
	Repo           string `json:"repo"`
	Branch         string `json:"branch"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycle_state"`
	UpdatedAt      string `json:"updated_at"`
	ReviewURL      string `json:"review_url"`
}

// listRunsOutput is the structured success payload for list_open_runs.
type listRunsOutput struct {
	OK    bool         `json:"ok"`
	Tool  string       `json:"tool"`
	Runs  []runSummary `json:"runs"`
	Count int          `json:"count"`
}

// HandleListOpenRuns implements the list_open_runs MCP tool.
func (s *Server) HandleListOpenRuns(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolErr("DEPENDENCY_ERROR: MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	var input listRunsInput
	_ = json.Unmarshal(rawArgs, &input) // ignore parse errors; use defaults

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}

	runs, err := s.deps.Store.ListRecentRunsWithRepo(limit)
	if err != nil {
		return toolErr(fmt.Sprintf("STORE_ERROR: list runs: %s", err))
	}

	summaries := make([]runSummary, 0, len(runs))
	for _, r := range runs {
		// Exclude terminal completed runs from the open-runs view.
		if r.Status == "completed" {
			continue
		}
		idStr := strconv.FormatInt(r.ID, 10)
		summaries = append(summaries, runSummary{
			RunID:          idStr,
			Title:          r.Title,
			Repo:           r.RepoName,
			Branch:         r.BranchName,
			Status:         r.Status,
			LifecycleState: lifecycleStateFromStatus(r.Status),
			UpdatedAt:      r.UpdatedAt,
			ReviewURL:      fmt.Sprintf("/runs/%s/intake", idStr),
		})
	}

	result := listRunsOutput{
		OK:    true,
		Tool:  "list_open_runs",
		Runs:  summaries,
		Count: len(summaries),
	}

	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

// lifecycleStateFromStatus derives a display lifecycle state from a canonical run status.
func lifecycleStateFromStatus(status string) string {
	switch status {
	case "draft", "intake_received", "intake_needs_review", "validated", "approved_for_prepare":
		return "intake"
	case "packet_validated", "repair_validated", "packet_validation_failed", "brief_ready_for_review":
		return "prepare"
	case "approved_for_executor", "executor_dispatched", "executor_done", "executor_blocked",
		"agent_done", "agent_blocked", "agent_result_needs_review":
		return "execute"
	case "audit_ready", "audit_ready_for_review", "revision_required",
		"accepted", "accepted_with_warnings",
		"validation_passed", "validation_failed_accepted":
		return "audit"
	case "completed":
		return "completed"
	case "blocked", "validation_failed":
		return "failed"
	default:
		return "intake"
	}
}

// listRunsSchema is the JSON Schema for list_open_runs.
var listRunsSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "limit": {
      "type": "number",
      "description": "Maximum number of runs to return. Default 10, max 25.",
      "minimum": 1,
      "maximum": 25
    }
  }
}`)

// ToolListOpenRuns is the ToolDefinition for list_open_runs.
var ToolListOpenRuns = ToolDefinition{
	Name: "list_open_runs",
	Description: "List recent non-terminal Relay runs. Returns a bounded summary of up to 25 runs " +
		"(run_id, title, repo, branch, status, lifecycle_state, updated_at, review_url). " +
		"Use this to check what runs are currently active before deciding whether to submit a new handoff " +
		"or perform an audit handback. Does not return artifact contents or logs.",
	InputSchema: listRunsSchema,
}
