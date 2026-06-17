package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/auditor"
	"relay/internal/compiler"
	"relay/internal/events"
	"relay/internal/executor"
	"relay/internal/intake"
	"relay/internal/renderer"
	"relay/internal/store"
	"relay/internal/store/generated"

	"github.com/go-chi/chi/v5"
)

type APIHandler struct {
	store    *store.Store
	log      *slog.Logger
	eventHub *events.Hub
}

func NewAPIHandler(s *store.Store, log *slog.Logger, hub ...*events.Hub) *APIHandler {
	var eventHub *events.Hub
	if len(hub) > 0 {
		eventHub = hub[0]
	}
	return &APIHandler{
		store:    s,
		log:      log,
		eventHub: eventHub,
	}
}

// CORS middleware for local frontend development origins
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isAllowedCORSOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, MCP-Protocol-Version, Authorization")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Shared Models matching TypeScript contract

type RelayRun struct {
	ID                    string                `json:"id"`
	Name                  string                `json:"name"`
	Repo                  string                `json:"repo"`
	Branch                string                `json:"branch"`
	ActiveStep            string                `json:"activeStep"`     // "intake" | "prepare" | "execute" | "audit"
	Status                string                `json:"status"`         // canonical workflow state for action gating
	LifecycleState        string                `json:"lifecycleState"` // "intake" | "prepare" | "execute" | "audit" | "completed" | "failed"
	CreatedAt             string                `json:"createdAt"`      // ISO-8601
	UpdatedAt             string                `json:"updatedAt"`      // ISO-8601
	Summary               string                `json:"summary"`
	Model                 string                `json:"model"`
	RiskLevel             string                `json:"riskLevel"` // "low" | "medium" | "high" | "critical"
	Validation            RelayValidationResult `json:"validation"`
	Artifacts             []RelayArtifact       `json:"artifacts"`
	LatestEvents          []RelayRunEvent       `json:"latestEvents"`
	StatusSeverity        string                `json:"statusSeverity"` // "neutral" | "info" | "success" | "warning" | "danger"
	State                 string                `json:"state"`
	Title                 string                `json:"title"`
	PacketID              string                `json:"packetId"`
	Worktree              string                `json:"worktree,omitempty"`
	Executor              string                `json:"executor"`
	ValidationSummary     RelayValidationResult `json:"validationSummary"`
	ApprovalGate          RelayApprovalGate     `json:"approvalGate"`
	LogPreview            RelayLogPreview       `json:"logPreview"`
	StepLabels            map[string]string     `json:"stepLabels"`
	LatestExecutionStatus string                `json:"latestExecutionStatus,omitempty"` // active execution phase: "starting" | "running" | "completed" | "failed" | ""
}

type RelayValidationResult struct {
	Errors   int                    `json:"errors"`
	Warnings int                    `json:"warnings"`
	Passed   int                    `json:"passed"`
	Issues   []RelayValidationIssue `json:"issues"`
}

type RelayValidationIssue struct {
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Code     string `json:"code"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
}

type RelayArtifact struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Kind        string `json:"kind"` // "prompt" | "handoff" | "result" | "audit" | "validation" | "diff"
	StorageKind string `json:"storageKind,omitempty"`
	ContentURL  string `json:"contentUrl,omitempty"`
	SizeHint    string `json:"sizeHint,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Status      string `json:"status"`
	Filename    string `json:"filename"`
	Preview     string `json:"preview,omitempty"`
}

type RelayRunEvent struct {
	ID        string                 `json:"id"`
	RunID     string                 `json:"runId"`
	Kind      string                 `json:"kind"` // "log" | "status_change" | "artifact_created" | "validation_run" | "step_transition"
	Message   string                 `json:"message"`
	CreatedAt string                 `json:"createdAt"` // ISO-8601
	Details   map[string]interface{} `json:"details,omitempty"`
}

type RelayApprovalGate struct {
	Label string `json:"label"`
	State string `json:"state"` // "pending" | "approved" | "rejected" | "skipped"
	Note  string `json:"note,omitempty"`
}

type RelayLogPreview struct {
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

type RelayApiErrorShape struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message"`
	Code    string                 `json:"code,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// Helpers for mappings

func parseAndFormatTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, value, time.UTC); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return value
}

func mapEventKind(level, message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "status changed") || strings.Contains(lower, "status:") {
		return "status_change"
	}
	if strings.Contains(lower, "artifact") || strings.Contains(lower, "saved") || strings.Contains(lower, "generated") {
		return "artifact_created"
	}
	if strings.Contains(lower, "validation") || strings.Contains(lower, "check") {
		return "validation_run"
	}
	if strings.Contains(lower, "step") || strings.Contains(lower, "transition") {
		return "step_transition"
	}
	switch level {
	case "log":
		return "log"
	case "status_change":
		return "status_change"
	case "artifact_created":
		return "artifact_created"
	case "validation_run":
		return "validation_run"
	case "step_transition":
		return "step_transition"
	default:
		return "log"
	}
}

func mapArtifactKindAndLabel(kind string) (string, string) {
	switch kind {
	case "original_handoff":
		return "handoff", "Original Handoff"
	case "planner_handoff":
		return "handoff", "Planner Handoff"
	case "parsed_frontmatter":
		return "handoff", "Parsed Frontmatter"
	case "run_config":
		return "handoff", "Run Configuration"
	case "intake_validation_report":
		return "validation", "Intake Validation Report"
	case "agent_prompt":
		return "prompt", "Compiled Prompt Brief"
	case "opencode_handoff_packet":
		return "handoff", "OpenCode Handoff Packet"
	case "agent_result_raw":
		return "result", "Agent Result (Raw)"
	case "agent_result_json":
		return "result", "Agent Result (JSON)"
	case "validation_stdout":
		return "validation", "Validation Output (Stdout)"
	case "validation_stderr":
		return "validation", "Validation Output (Stderr)"
	case "validation_run_json":
		return "validation", "Validation Report (JSON)"
	case "opencode_stdout":
		return "result", "OpenCode Output (Stdout)"
	case "opencode_stderr":
		return "result", "OpenCode Output (Stderr)"
	case "opencode_combined":
		return "result", "OpenCode Output (Combined)"
	case "opencode_dry_run_json":
		return "validation", "OpenCode Dry Run (JSON)"
	case "opencode_cli_check_json":
		return "validation", "OpenCode CLI Check (JSON)"
	case "audit_handoff":
		return "audit", "Audit Handoff"
	case "audit_patch":
		return "diff", "Audit Patch"
	case "git_status":
		return "diff", "Git Status"
	case "git_diff_stat":
		return "diff", "Git Diff Stat"
	case "git_diff_patch":
		return "diff", "Git Diff Patch"
	case "git_diff_name_status":
		return "diff", "Git Diff Name Status"
	case "git_commit_suggestion":
		return "audit", "Git Commit Suggestion"
	case "commit_message_text":
		return "audit", "Commit Message (Text)"
	case "commit_suggestion_json":
		return "audit", "Commit Suggestion (JSON)"
	case "audit_input_summary":
		return "audit", "Audit Input Summary"
	default:
		lower := strings.ToLower(kind)
		if strings.Contains(lower, "diff") || strings.Contains(lower, "patch") || strings.Contains(lower, "status") {
			return "diff", strings.Title(strings.ReplaceAll(kind, "_", " "))
		}
		if strings.Contains(lower, "validation") || strings.Contains(lower, "check") {
			return "validation", strings.Title(strings.ReplaceAll(kind, "_", " "))
		}
		if strings.Contains(lower, "prompt") {
			return "prompt", strings.Title(strings.ReplaceAll(kind, "_", " "))
		}
		if strings.Contains(lower, "handoff") {
			return "handoff", strings.Title(strings.ReplaceAll(kind, "_", " "))
		}
		if strings.Contains(lower, "audit") {
			return "audit", strings.Title(strings.ReplaceAll(kind, "_", " "))
		}
		return "result", strings.Title(strings.ReplaceAll(kind, "_", " "))
	}
}

func getFileSizeHint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	size := info.Size()
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	} else {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

func buildValidationResult(checks []generated.Check) RelayValidationResult {
	var result RelayValidationResult
	result.Issues = make([]RelayValidationIssue, 0)
	for _, c := range checks {
		switch c.Status {
		case "fail", "error", "block":
			result.Errors++
		case "warn", "warning":
			result.Warnings++
		case "pass", "passed":
			result.Passed++
		}
		if c.Kind == "validation" || c.Kind == "validation_run" {
			severity := "info"
			switch c.Status {
			case "fail", "error", "block":
				severity = "error"
			case "warn", "warning":
				severity = "warning"
			}
			result.Issues = append(result.Issues, RelayValidationIssue{
				Severity: severity,
				Code:     c.Kind,
				Message:  c.Summary,
			})
		}
	}
	return result
}

func buildApprovalGate(activeStep string, status string) RelayApprovalGate {
	label := "Intake Review"
	state := "pending"
	note := "Gate status resolved from active step and status"
	switch activeStep {
	case "intake":
		label = "Intake Review"
		if status == "validated" || status == "approved_for_prepare" || status == "ready" {
			state = "approved"
		} else if status == "blocked" {
			state = "rejected"
			note = "Intake blocked"
		} else {
			state = "pending"
		}
	case "prepare":
		label = "Brief Review"
		state = "pending"
	case "execute":
		label = "Execution"
		state = "approved"
	case "audit":
		label = "Audit Review"
		if status == "completed" || status == "accepted" || status == "accepted_with_warnings" {
			state = "approved"
		} else {
			state = "pending"
		}
	}
	return RelayApprovalGate{
		Label: label,
		State: state,
		Note:  note,
	}
}

func buildLogPreview(events []generated.Event) RelayLogPreview {
	var lines []string
	for _, e := range events {
		tStr := parseAndFormatTime(e.CreatedAt)
		if parsed, err := time.Parse(time.RFC3339, tStr); err == nil {
			tStr = parsed.Format("15:04:05")
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", tStr, e.Message))
	}
	truncated := false
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
		truncated = true
	}
	if lines == nil {
		lines = make([]string, 0)
	}
	return RelayLogPreview{
		Lines:     lines,
		Truncated: truncated,
	}
}

func (h *APIHandler) mapRunToRelayRun(run generated.Run, repoName string) RelayRun {
	idStr := strconv.FormatInt(run.ID, 10)

	// Fetch run dependencies from DB
	artifacts, _ := h.store.ListArtifactsByRun(run.ID)
	checks, _ := h.store.ListChecksByRun(run.ID)
	events, _ := h.store.ListEventsByRun(run.ID)
	latestExec, _ := h.store.GetLatestAgentExecutionByRun(run.ID)

	worktree := ""
	for _, art := range artifacts {
		if art.Kind == "run_config" {
			if data, err := os.ReadFile(art.Path); err == nil {
				var cfg map[string]interface{}
				if err := json.Unmarshal(data, &cfg); err == nil {
					if wt, ok := cfg["worktree"].(string); ok {
						worktree = wt
					}
				}
			}
		}
	}

	// Status is canonical workflow state for action gating
	status := run.Status

	// Documented legacy aliases — normalize a few historic statuses
	if status == "ready" {
		status = "approved_for_prepare"
	} else if status == "needs_review" {
		status = "intake_needs_review"
	}

	// Latest execution status exposed separately, never overwrites canonical status
	latestExecutionStatus := ""
	if latestExec != nil {
		latestExecutionStatus = latestExec.Status
	}

	// Derive display/helper fields from canonical status
	activeStep := "intake"
	lifecycleState := "intake"
	state := "Intake Review"
	statusSeverity := "warning"

	switch status {
	case "draft", "needs_cleanup":
		activeStep = "intake"
		lifecycleState = "intake"
		state = "Intake Review"
		statusSeverity = "warning"
		if status == "needs_cleanup" {
			statusSeverity = "danger"
		}
	case "intake_needs_review", "intake_received":
		activeStep = "intake"
		lifecycleState = "intake"
		state = "Intake Needs Review"
		statusSeverity = "warning"
		if status == "intake_received" {
			state = "Intake Received"
			statusSeverity = "info"
		}
	case "validated":
		activeStep = "intake"
		lifecycleState = "intake"
		state = "Intake Validated"
		statusSeverity = "info"
	case "approved_for_prepare":
		activeStep = "prepare"
		lifecycleState = "prepare"
		state = "Approved for Prepare"
		statusSeverity = "success"
	case "packet_validated", "repair_validated":
		activeStep = "prepare"
		lifecycleState = "prepare"
		state = "Packet Validated"
		statusSeverity = "info"
	case "packet_validation_failed":
		activeStep = "prepare"
		lifecycleState = "prepare"
		state = "Packet Validation Failed"
		statusSeverity = "danger"
	case "brief_ready_for_review":
		activeStep = "prepare"
		lifecycleState = "prepare"
		state = "Brief Ready for Review"
		statusSeverity = "success"
	case "approved_for_executor":
		activeStep = "execute"
		lifecycleState = "execute"
		state = "Approved for Executor"
		statusSeverity = "success"
	case executor.StatusExecutorDispatched:
		activeStep = "execute"
		lifecycleState = "execute"
		state = "Executor Dispatched"
		statusSeverity = "info"
	case executor.StatusExecutorDone:
		activeStep = "execute"
		lifecycleState = "execute"
		state = "Executor Done"
		statusSeverity = "success"
	case executor.StatusExecutorBlocked:
		activeStep = "execute"
		lifecycleState = "failed"
		state = "Executor Blocked"
		statusSeverity = "danger"
	case "blocked":
		activeStep = "intake"
		lifecycleState = "failed"
		state = "Blocked"
		statusSeverity = "danger"
	case "agent_done":
		activeStep = "execute"
		lifecycleState = "execute"
		state = "Agent Done"
		statusSeverity = "success"
	case "agent_blocked":
		activeStep = "execute"
		lifecycleState = "failed"
		state = "Agent Blocked"
		statusSeverity = "danger"
	case "agent_result_needs_review":
		activeStep = "execute"
		lifecycleState = "execute"
		state = "Result Needs Review"
		statusSeverity = "warning"
	case "validation_passed", "validation_failed_accepted":
		activeStep = "audit"
		lifecycleState = "audit"
		state = "Audit Review"
		statusSeverity = "warning"
	case "validation_failed":
		activeStep = "audit"
		lifecycleState = "failed"
		state = "Validation Failed"
		statusSeverity = "danger"
	case "audit_ready", "audit_ready_for_review":
		activeStep = "audit"
		lifecycleState = "audit"
		state = "Audit Ready"
		statusSeverity = "warning"
	case "revision_required":
		activeStep = "audit"
		lifecycleState = "audit"
		state = "Revision Required"
		statusSeverity = "warning"
	case "accepted":
		activeStep = "audit"
		lifecycleState = "audit"
		state = "Approved — Ready to Close"
		statusSeverity = "success"
	case "accepted_with_warnings":
		activeStep = "audit"
		lifecycleState = "audit"
		state = "Approved with Warnings"
		statusSeverity = "warning"
	case "completed":
		activeStep = "audit"
		lifecycleState = "completed"
		state = "Completed"
		statusSeverity = "success"
	}

	model := run.SelectedModel
	if model == "" {
		model = run.RecommendedModel
	}

	valResult := buildValidationResult(checks)

	// Map artifacts
	relayArtifacts := make([]RelayArtifact, 0)
	for _, art := range artifacts {
		k, l := mapArtifactKindAndLabel(art.Kind)
		filename := filepath.Base(art.Path)
		sizeHint := getFileSizeHint(art.Path)

		preview := ""
		if art.MimeType == "text/plain" || art.MimeType == "application/json" || art.MimeType == "text/markdown" {
			if data, err := os.ReadFile(art.Path); err == nil {
				if len(data) > 500 {
					preview = string(data[:500]) + "..."
				} else {
					preview = string(data)
				}
			}
		}

		relayArtifacts = append(relayArtifacts, RelayArtifact{
			ID:          strconv.FormatInt(art.ID, 10),
			Label:       l,
			Path:        fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, art.Kind),
			Kind:        k,
			StorageKind: art.Kind,
			ContentURL:  fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, art.Kind),
			SizeHint:    sizeHint,
			CreatedAt:   parseAndFormatTime(art.CreatedAt),
			Status:      "ready",
			Filename:    filename,
			Preview:     preview,
		})
	}

	// Map latest events
	relayEvents := make([]RelayRunEvent, 0)
	for _, e := range events {
		relayEvents = append(relayEvents, RelayRunEvent{
			ID:        strconv.FormatInt(e.ID, 10),
			RunID:     idStr,
			Kind:      mapEventKind(e.Level, e.Message),
			Message:   e.Message,
			CreatedAt: parseAndFormatTime(e.CreatedAt),
		})
	}

	// Step labels
	stepLabels := map[string]string{
		"intake":  "Intake / Configure",
		"prepare": "Compile / Render",
		"execute": "Execute",
		"audit":   "Audit / Close",
	}

	packetID := ""
	if run.Title != "" {
		packetID = "packet-" + idStr
	}

	return RelayRun{
		ID:                    idStr,
		Name:                  run.Title,
		Repo:                  repoName,
		Branch:                run.BranchName,
		Worktree:              worktree,
		ActiveStep:            activeStep,
		Status:                status,
		LifecycleState:        lifecycleState,
		CreatedAt:             parseAndFormatTime(run.CreatedAt),
		UpdatedAt:             parseAndFormatTime(run.UpdatedAt),
		Summary:               "Orchestration run: " + run.Title,
		Model:                 model,
		RiskLevel:             "medium",
		Validation:            valResult,
		Artifacts:             relayArtifacts,
		LatestEvents:          relayEvents,
		StatusSeverity:        statusSeverity,
		State:                 state,
		Title:                 run.Title,
		PacketID:              packetID,
		Executor:              "deepseek-v4-flash",
		ValidationSummary:     valResult,
		ApprovalGate:          buildApprovalGate(activeStep, run.Status),
		LogPreview:            buildLogPreview(events),
		StepLabels:            stepLabels,
		LatestExecutionStatus: latestExecutionStatus,
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, errStr, msg string) {
	writeJSON(w, status, RelayApiErrorShape{
		Error:   errStr,
		Message: msg,
	})
}

// Handlers implementation

// GET /api/runs
func (h *APIHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.ListRecentRunsWithRepo(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list recent runs")
		return
	}

	result := make([]RelayRun, 0)
	for _, run := range runs {
		storeRun := generated.Run{
			ID:               run.ID,
			RepoID:           run.RepoID,
			Title:            run.Title,
			Status:           run.Status,
			RecommendedModel: run.RecommendedModel,
			SelectedModel:    run.SelectedModel,
			BranchName:       run.BranchName,
			BaseCommit:       run.BaseCommit,
			HeadCommit:       run.HeadCommit,
			CreatedAt:        run.CreatedAt,
			UpdatedAt:        run.UpdatedAt,
		}
		result = append(result, h.mapRunToRelayRun(storeRun, run.RepoName))
	}
	writeJSON(w, http.StatusOK, result)
}

// GET /api/runs/{id}
func (h *APIHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	repoName := "Unknown Repo"
	if repo, err := h.store.GetRepo(run.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}

	relayRun := h.mapRunToRelayRun(*run, repoName)
	writeJSON(w, http.StatusOK, relayRun)
}

// GET /api/runs/{id}/artifacts
func (h *APIHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	if _, err := h.store.GetRun(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	artifacts, err := h.store.ListArtifactsByRun(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list artifacts")
		return
	}

	result := make([]RelayArtifact, 0)
	for _, art := range artifacts {
		k, l := mapArtifactKindAndLabel(art.Kind)
		filename := filepath.Base(art.Path)
		sizeHint := getFileSizeHint(art.Path)

		preview := ""
		if art.MimeType == "text/plain" || art.MimeType == "application/json" || art.MimeType == "text/markdown" {
			if data, err := os.ReadFile(art.Path); err == nil {
				if len(data) > 500 {
					preview = string(data[:500]) + "..."
				} else {
					preview = string(data)
				}
			}
		}

		result = append(result, RelayArtifact{
			ID:          strconv.FormatInt(art.ID, 10),
			Label:       l,
			Path:        fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, art.Kind),
			Kind:        k,
			StorageKind: art.Kind,
			ContentURL:  fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, art.Kind),
			SizeHint:    sizeHint,
			CreatedAt:   parseAndFormatTime(art.CreatedAt),
			Status:      "ready",
			Filename:    filename,
			Preview:     preview,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// GET /api/runs/{id}/artifacts/{kind}
func (h *APIHandler) GetArtifactContent(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}
	if kind == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Artifact kind is required")
		return
	}

	artifacts, err := h.store.ListArtifactsByRunKind(id, kind)
	if err != nil || len(artifacts) == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("No artifact found for run %s kind %s", idStr, kind))
		return
	}

	art := artifacts[len(artifacts)-1]
	data, err := os.ReadFile(art.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to read artifact file")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// GET /api/runs/{id}/events
func (h *APIHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	if _, err := h.store.GetRun(id); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	events, err := h.store.ListEventsByRun(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list events")
		return
	}

	result := make([]RelayRunEvent, 0)
	for _, e := range events {
		result = append(result, RelayRunEvent{
			ID:        strconv.FormatInt(e.ID, 10),
			RunID:     idStr,
			Kind:      mapEventKind(e.Level, e.Message),
			Message:   e.Message,
			CreatedAt: parseAndFormatTime(e.CreatedAt),
		})
	}

	if len(result) > 100 {
		result = result[len(result)-100:]
	}

	writeJSON(w, http.StatusOK, result)
}

// POST /api/intake/planner-handoff DTOs
type PlannerHandoffIntakeRequest struct {
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	HandoffPath string `json:"handoffPath"`
	PacketID    string `json:"packetId,omitempty"`
	Name        string `json:"name,omitempty"`

	// S3 fields
	PlannerHandoffMarkdown string `json:"planner_handoff_markdown"`
	RunID                  string `json:"run_id,omitempty"`
	RepoTarget             string `json:"repo_target,omitempty"`
	BranchContext          string `json:"branch_context,omitempty"`
	Source                 string `json:"source,omitempty"`
}

type PlannerHandoffIntakeResponse struct {
	Success        bool                  `json:"success"`
	RunID          string                `json:"runId"`
	RunIDSnake     string                `json:"run_id"`
	Status         string                `json:"status"`
	LifecycleState string                `json:"lifecycleState,omitempty"`
	CreatedAt      string                `json:"createdAt,omitempty"`
	ReviewURL      string                `json:"review_url"`
	Artifacts      []RelayArtifact       `json:"artifacts,omitempty"`
	Validation     RelayValidationResult `json:"validation"`
}

// Helpers for intake

func resolveRepo(s *store.Store, repoNameOrPath string) (*store.Repo, error) {
	if repoNameOrPath == "" {
		return nil, fmt.Errorf("repo is required")
	}
	// Try by exact name
	repo, err := s.GetRepoByName(repoNameOrPath)
	if err == nil && repo != nil {
		return repo, nil
	}
	// Try by exact path
	repo, err = s.GetRepoByPath(repoNameOrPath)
	if err == nil && repo != nil {
		return repo, nil
	}
	// Try base name of path
	baseName := filepath.Base(repoNameOrPath)
	repo, err = s.GetRepoByName(baseName)
	if err == nil && repo != nil {
		return repo, nil
	}
	// Clean path and try path
	normalized := filepath.Clean(repoNameOrPath)
	repo, err = s.GetRepoByPath(normalized)
	if err == nil && repo != nil {
		return repo, nil
	}
	// Try listing repos and matching by name
	repos, err := s.ListRepos()
	if err == nil {
		for _, r := range repos {
			if strings.EqualFold(r.Name, repoNameOrPath) || strings.EqualFold(r.Name, baseName) {
				return &r, nil
			}
		}
	}
	// If not found, create new repo
	return s.CreateRepo(baseName, repoNameOrPath)
}

func deriveRunTitleFromMarkdown(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return "Untitled Run"
}

// POST /api/intake/planner-handoff
func (h *APIHandler) IntakePlannerHandoff(w http.ResponseWriter, r *http.Request) {
	var req PlannerHandoffIntakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	markdown := req.PlannerHandoffMarkdown
	if strings.TrimSpace(markdown) == "" && req.HandoffPath != "" {
		data, err := os.ReadFile(req.HandoffPath)
		if err == nil {
			markdown = string(data)
		}
	}

	if strings.TrimSpace(markdown) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Planner handoff markdown is empty or missing")
		return
	}

	metadata, _, _, _ := intake.ParseFrontmatter(markdown)
	warnings, blockers := intake.ValidateHandoffText(markdown)

	if len(blockers) > 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", strings.Join(blockers, "; "))
		return
	}

	repoTarget := req.Repo
	if repoTarget == "" {
		repoTarget = req.RepoTarget
	}
	if repoTarget == "" {
		repoTarget = metadata["repo"]
	}
	if repoTarget == "" {
		repoTarget = metadata["repo_target"]
	}
	if repoTarget == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "No repository target found in request or frontmatter")
		return
	}

	repo, err := resolveRepo(h.store, repoTarget)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to resolve repository: "+err.Error())
		return
	}

	branchContext := req.Branch
	if branchContext == "" {
		branchContext = req.BranchContext
	}
	if branchContext == "" {
		branchContext = metadata["branch"]
	}
	if branchContext == "" {
		branchContext = metadata["branch_context"]
	}
	if branchContext == "" {
		branchContext = "main"
	}

	title := req.Name
	if title == "" {
		title = req.PacketID
	}
	if title == "" {
		title = metadata["title"]
	}
	if title == "" {
		title = deriveRunTitleFromMarkdown(markdown)
	}

	recommendedModel := metadata["recommended_model"]
	if recommendedModel == "" {
		recommendedModel = "deepseek-v4-flash"
	}
	selectedModel := recommendedModel

	var run *generated.Run
	isNew := false

	runIDStr := req.RunID
	if runIDStr != "" {
		runIDInt, err := strconv.ParseInt(runIDStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run_id format")
			return
		}
		run, err = h.store.GetRun(runIDInt)
		if err != nil || run == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %s not found", runIDStr))
			return
		}
	} else {
		isNew = true
		status := "intake_received"
		if len(warnings) > 0 {
			status = "intake_needs_review"
		}
		r, err := h.store.CreateRun(repo.ID, title, status, recommendedModel, selectedModel, branchContext)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create run: "+err.Error())
			return
		}
		run = r
	}

	if !isNew {
		status := "intake_received"
		if len(warnings) > 0 {
			status = "intake_needs_review"
		}
		updatedRun, err := h.store.UpdateRunStatus(run.ID, status)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run status: "+err.Error())
			return
		}
		if recommendedModel != "" {
			_, _ = h.store.UpdateRunModel(run.ID, recommendedModel, recommendedModel)
		}
		if branchContext != "" {
			_, _ = h.store.UpdateRunBranch(run.ID, branchContext, "", "")
		}
		run = updatedRun
	}

	_ = h.store.DeleteChecksByRunKind(run.ID, "validation")
	if len(warnings) > 0 {
		for _, w := range warnings {
			_, _ = h.store.CreateCheck(run.ID, "validation", "warning", w, "{}")
		}
	} else {
		_, _ = h.store.CreateCheck(run.ID, "validation", "pass", "Intake validation successful", "{}")
	}

	_ = h.store.DeleteArtifactsByRunKind(run.ID, "planner_handoff")
	_ = h.store.DeleteArtifactsByRunKind(run.ID, "parsed_frontmatter")
	_ = h.store.DeleteArtifactsByRunKind(run.ID, "run_config")
	_ = h.store.DeleteArtifactsByRunKind(run.ID, "intake_validation_report")

	path, err := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", []byte(markdown))
	if err == nil {
		_, _ = h.store.CreateArtifact(run.ID, "planner_handoff", path, "text/markdown")
	}

	fmJSON, _ := json.MarshalIndent(metadata, "", "  ")
	path, err = artifacts.Write(run.ID, "parsed_frontmatter", "parsed_frontmatter.json", fmJSON)
	if err == nil {
		_, _ = h.store.CreateArtifact(run.ID, "parsed_frontmatter", path, "application/json")
	}

	sourceStr := req.Source
	if sourceStr == "" {
		sourceStr = "api"
	}
	configMap := map[string]string{
		"repo_target":    repo.Path,
		"branch_context": branchContext,
		"source":         sourceStr,
		"created_from":   "intake_endpoint",
	}
	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	path, err = artifacts.Write(run.ID, "run_config", "run_config.json", configJSON)
	if err == nil {
		_, _ = h.store.CreateArtifact(run.ID, "run_config", path, "application/json")
	}

	dbChecks, _ := h.store.ListChecksByRun(run.ID)
	valResult := buildValidationResult(dbChecks)

	report := map[string]interface{}{
		"status":   run.Status,
		"errors":   valResult.Errors,
		"warnings": valResult.Warnings,
		"passed":   valResult.Passed,
		"issues":   valResult.Issues,
	}
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	path, err = artifacts.Write(run.ID, "intake_validation_report", "intake_validation_report.json", reportJSON)
	if err == nil {
		_, _ = h.store.CreateArtifact(run.ID, "intake_validation_report", path, "application/json")
	}

	_, _ = h.store.CreateEvent(run.ID, "info", "Handoff intake receipt: planner handoff registered")

	dbArtifacts, _ := h.store.ListArtifactsByRun(run.ID)
	relayArtifacts := make([]RelayArtifact, 0)
	for _, art := range dbArtifacts {
		k, l := mapArtifactKindAndLabel(art.Kind)
		filename := filepath.Base(art.Path)
		sizeHint := getFileSizeHint(art.Path)
		relayArtifacts = append(relayArtifacts, RelayArtifact{
			ID:          strconv.FormatInt(art.ID, 10),
			Label:       l,
			Path:        fmt.Sprintf("/api/runs/%d/artifacts/%s", run.ID, art.Kind),
			Kind:        k,
			StorageKind: art.Kind,
			ContentURL:  fmt.Sprintf("/api/runs/%d/artifacts/%s", run.ID, art.Kind),
			SizeHint:    sizeHint,
			CreatedAt:   parseAndFormatTime(art.CreatedAt),
			Status:      "ready",
			Filename:    filename,
		})
	}

	runIDStrOutput := strconv.FormatInt(run.ID, 10)
	reviewURL := fmt.Sprintf("/runs/%s/intake", runIDStrOutput)
	mappedRun := h.mapRunToRelayRun(*run, repo.Name)

	response := PlannerHandoffIntakeResponse{
		Success:        true,
		RunID:          runIDStrOutput,
		RunIDSnake:     runIDStrOutput,
		Status:         mappedRun.Status,
		LifecycleState: mappedRun.LifecycleState,
		CreatedAt:      mappedRun.CreatedAt,
		ReviewURL:      reviewURL,
		Artifacts:      relayArtifacts,
		Validation:     valResult,
	}
	writeJSON(w, http.StatusOK, response)
}

// POST /api/runs/{id}/approve-intake
func (h *APIHandler) ApproveIntake(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	// Validate current status allows Step 1 review action
	if run.Status != "intake_received" && run.Status != "intake_needs_review" {
		writeError(w, http.StatusConflict, "CONFLICT", fmt.Sprintf("Run status is %q, cannot approve/review in this state", run.Status))
		return
	}

	type ApproveIntakeOverrides struct {
		Model              string `json:"model"`
		Repo               string `json:"repo"`
		Branch             string `json:"branch"`
		Worktree           string `json:"worktree"`
		ValidationCommands string `json:"validationCommands"`
	}
	type ApproveIntakeRequest struct {
		Action    string                 `json:"action"` // "approve", "needs_revision", "blocked"
		Notes     string                 `json:"notes"`
		Overrides ApproveIntakeOverrides `json:"overrides"`
	}

	var req ApproveIntakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	// Validate action
	if req.Action != "approve" && req.Action != "needs_revision" && req.Action != "blocked" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid decision action %q", req.Action))
		return
	}

	// Apply overrides if provided and different
	var updatedRun *generated.Run = run

	// 1. Repo override
	if req.Overrides.Repo != "" {
		newRepo, err := resolveRepo(h.store, req.Overrides.Repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to resolve repository: "+err.Error())
			return
		}
		if newRepo.ID != run.RepoID {
			updatedRun, err = h.store.UpdateRunRepo(run.ID, newRepo.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run repository: "+err.Error())
				return
			}
		}
	}

	// 2. Model override
	if req.Overrides.Model != "" && req.Overrides.Model != run.SelectedModel {
		updatedRun, err = h.store.UpdateRunModel(run.ID, run.RecommendedModel, req.Overrides.Model)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run model: "+err.Error())
			return
		}
	}

	// 3. Branch override
	if req.Overrides.Branch != "" && req.Overrides.Branch != run.BranchName {
		updatedRun, err = h.store.UpdateRunBranch(run.ID, req.Overrides.Branch, run.BaseCommit, run.HeadCommit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run branch: "+err.Error())
			return
		}
	}

	// 4. Persistence of overrides and notes in run_config.json
	configMap := make(map[string]interface{})
	if data, err := artifacts.Read(run.ID, "run_config", "run_config.json"); err == nil {
		_ = json.Unmarshal(data, &configMap)
	}
	if req.Overrides.Repo != "" {
		configMap["repo_target"] = req.Overrides.Repo
	}
	if req.Overrides.Branch != "" {
		configMap["branch_context"] = req.Overrides.Branch
	}
	if req.Overrides.Worktree != "" {
		configMap["worktree"] = req.Overrides.Worktree
	}
	if req.Overrides.Model != "" {
		configMap["model"] = req.Overrides.Model
	}
	if req.Overrides.ValidationCommands != "" {
		configMap["validation_commands"] = req.Overrides.ValidationCommands
	}
	configMap["notes"] = req.Notes
	configMap["decision"] = req.Action
	configMap["reviewed_at"] = time.Now().UTC().Format(time.RFC3339)

	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	_ = h.store.DeleteArtifactsByRunKind(run.ID, "run_config")
	if path, err := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON); err == nil {
		_, _ = h.store.CreateArtifact(run.ID, "run_config", path, "application/json")
	}

	// Update run status based on action
	nextStatus := "intake_needs_review"
	eventMessage := "Intake needs revision"
	if req.Action == "approve" {
		nextStatus = "approved_for_prepare"
		eventMessage = "Intake approved"
	} else if req.Action == "blocked" {
		nextStatus = "blocked"
		eventMessage = "Intake blocked"
	}

	if req.Notes != "" {
		eventMessage = fmt.Sprintf("%s: %s", eventMessage, req.Notes)
	}

	updatedRun, err = h.store.UpdateRunStatus(run.ID, nextStatus)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run status: "+err.Error())
		return
	}

	_, _ = h.store.CreateEvent(run.ID, "status_change", eventMessage)

	repoName := "Unknown Repo"
	if repo, err := h.store.GetRepo(updatedRun.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}

	mappedRun := h.mapRunToRelayRun(*updatedRun, repoName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          strconv.FormatInt(run.ID, 10),
		"status":         mappedRun.Status,
		"lifecycleState": mappedRun.LifecycleState,
		"updatedAt":      mappedRun.UpdatedAt,
	})
}

// POST /api/runs/{id}/prepare
func (h *APIHandler) PrepareRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	if run.Status != "approved_for_prepare" && run.Status != "packet_validation_failed" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":            "CONFLICT",
			"message":          fmt.Sprintf("Run status is %q, must be approved_for_prepare or packet_validation_failed to compile", run.Status),
			"currentStatus":    run.Status,
			"requiredStatuses": []string{"approved_for_prepare", "packet_validation_failed"},
		})
		return
	}

	comp := compiler.New(h.store)
	res, err := comp.CompileApprovedRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if !res.Success {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"success":          false,
			"runId":            idStr,
			"status":           "packet_validation_failed",
			"lifecycleState":   "prepare",
			"packetId":         res.PacketID,
			"issues":           res.Issues,
			"validationReport": res.ValidationReport,
		})
		return
	}

	repoName := "Unknown Repo"
	if repo, err := h.store.GetRepo(run.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}

	mappedRun := h.mapRunToRelayRun(*run, repoName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"runId":            idStr,
		"packetId":         res.PacketID,
		"status":           mappedRun.Status,
		"lifecycleState":   mappedRun.LifecycleState,
		"validationReport": res.ValidationReport,
	})
}

// POST /api/runs/{id}/render-brief
func (h *APIHandler) RenderBrief(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	rend := renderer.New(h.store)
	res, err := rend.RenderExecutorBrief(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if !res.Success {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"success": false,
			"runId":   idStr,
			"issues":  res.Issues,
		})
		return
	}

	run, err := h.store.GetRun(id)
	if err == nil && run != nil {
		repoName := "Unknown Repo"
		if repo, err := h.store.GetRepo(run.RepoID); err == nil && repo != nil {
			repoName = repo.Name
		}
		mappedRun := h.mapRunToRelayRun(*run, repoName)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"runId":          idStr,
			"status":         mappedRun.Status,
			"lifecycleState": mappedRun.LifecycleState,
			"updatedAt":      mappedRun.UpdatedAt,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"runId":   idStr,
	})
}

// POST /api/runs/{id}/approve-brief
func (h *APIHandler) ApproveBrief(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	rend := renderer.New(h.store)
	res, err := rend.ApproveExecutorBrief(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if !res.Success {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"success": false,
			"runId":   idStr,
			"issues":  res.Issues,
		})
		return
	}

	run, err := h.store.GetRun(id)
	if err == nil && run != nil {
		repoName := "Unknown Repo"
		if repo, err := h.store.GetRepo(run.RepoID); err == nil && repo != nil {
			repoName = repo.Name
		}
		mappedRun := h.mapRunToRelayRun(*run, repoName)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"runId":          idStr,
			"status":         mappedRun.Status,
			"lifecycleState": mappedRun.LifecycleState,
			"updatedAt":      mappedRun.UpdatedAt,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"runId":   idStr,
	})
}

// POST /api/runs/{id}/execute
func (h *APIHandler) ExecuteRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Action = "start"
	}

	switch req.Action {
	case "start":
		params := &executor.DispatchParams{
			Store:    h.store,
			Log:      h.log,
			EventHub: h.eventHub,
			RunID:    id,
		}

		result, err := executor.DispatchBrief(params)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"success": false,
				"runId":   idStr,
				"error":   err.Error(),
			})
			return
		}

		_ = result

		run, _ := h.store.GetRun(id)
		repoName := "Unknown Repo"
		if repo, err := h.store.GetRepo(run.RepoID); err == nil && repo != nil {
			repoName = repo.Name
		}
		mappedRun := h.mapRunToRelayRun(*run, repoName)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"runId":          idStr,
			"status":         mappedRun.Status,
			"lifecycleState": mappedRun.LifecycleState,
			"updatedAt":      mappedRun.UpdatedAt,
		})

	case "cancel":
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Cancel action is not yet available for executor dispatch")

	case "recover":
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Recover action is not yet available for executor dispatch")

	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Unknown execute action %q", req.Action))
	}
}

// POST /api/runs/{id}/audit
func (h *APIHandler) GenerateAudit(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	svc := auditor.NewService(h.store)
	result, err := svc.Generate(id)
	if err != nil {
		writeError(w, http.StatusConflict, "CONFLICT", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         result.Status,
		"inputSummary":   result.InputSummary,
		"auditPacket":    result.AuditPacket,
		"decision":       result.Decision,
		"warnings":       result.Warnings,
		"lifecycleState": "audit",
	})
}

// POST /api/runs/{id}/audit/submit
func (h *APIHandler) SubmitAuditPacket(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	var req struct {
		AuditPacketMarkdown string `json:"audit_packet_markdown"`
		Decision            string `json:"decision"`
		Notes               string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	subSvc := auditor.NewSubmissionService(h.store)
	input := auditor.ManualAuditSubmission{
		RunID:               id,
		AuditPacketMarkdown: req.AuditPacketMarkdown,
		Decision:            auditor.Decision(req.Decision),
		Notes:               req.Notes,
	}

	result, err := subSvc.SubmitManual(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"runId":       idStr,
		"auditPacket": result.AuditPacket,
		"decision":    result.Decision,
		"updatedAt":   result.CreatedAt.Format(time.RFC3339),
	})
}

// POST /api/runs/{id}/audit/approve
func (h *APIHandler) ApproveAudit(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	if run.Status != "audit_ready" && run.Status != "audit_ready_for_review" && run.Status != "accepted_with_warnings" {
		writeError(w, http.StatusConflict, "CONFLICT", fmt.Sprintf("Run status is %q, must be audit_ready to approve", run.Status))
		return
	}

	var req struct {
		Decision string `json:"decision"`
		Notes    string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	if req.Decision != "accepted" && req.Decision != "accepted_with_warnings" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid audit decision %q: must be accepted or accepted_with_warnings", req.Decision))
		return
	}

	nextStatus := req.Decision
	eventMsg := "Audit approved"
	if req.Decision == "accepted_with_warnings" {
		eventMsg = "Audit approved with warnings"
	}
	if req.Notes != "" {
		eventMsg = fmt.Sprintf("%s: %s", eventMsg, req.Notes)
	}

	updatedRun, err := h.store.UpdateRunStatus(id, nextStatus)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run status")
		return
	}

	_, _ = h.store.CreateEvent(id, "status_change", eventMsg)

	repoName := "Unknown Repo"
	if repo, err := h.store.GetRepo(updatedRun.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}
	mappedRun := h.mapRunToRelayRun(*updatedRun, repoName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         mappedRun.Status,
		"lifecycleState": mappedRun.LifecycleState,
		"state":          mappedRun.State,
		"updatedAt":      mappedRun.UpdatedAt,
	})
}

// POST /api/runs/{id}/audit/request-revision
func (h *APIHandler) RequestAuditRevision(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	if run.Status != "audit_ready" && run.Status != "audit_ready_for_review" {
		writeError(w, http.StatusConflict, "CONFLICT", fmt.Sprintf("Run status is %q, cannot request revision in this state", run.Status))
		return
	}

	var req struct {
		Notes  string `json:"notes"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Notes = ""
		req.Reason = ""
	}

	eventMsg := "Audit revision requested"
	if req.Reason != "" {
		eventMsg = fmt.Sprintf("%s: %s", eventMsg, req.Reason)
	}
	if req.Notes != "" {
		eventMsg = fmt.Sprintf("%s (%s)", eventMsg, req.Notes)
	}

	_, _ = h.store.CreateEvent(id, "status_change", eventMsg)

	// Persist revision details as an artifact for durable evidence
	revisionData := fmt.Sprintf("# Audit Revision Request\n\n- Run ID: %d\n- Reason: %s\n- Notes: %s\n- Requested: %s\n",
		id, req.Reason, req.Notes, time.Now().UTC().Format(time.RFC3339))
	revisionPath, revErr := artifacts.Write(id, "audit_revision", "audit_revision_request.md", []byte(revisionData))
	if revErr == nil {
		_, _ = h.store.CreateArtifact(id, "audit_revision", revisionPath, "text/markdown")
	}

	// Transition run status to revision_required
	updatedRun, err := h.store.UpdateRunStatus(id, "revision_required")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update run status")
		return
	}

	repoName := "Unknown Repo"
	if repo, err := h.store.GetRepo(updatedRun.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}
	mappedRun := h.mapRunToRelayRun(*updatedRun, repoName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         mappedRun.Status,
		"lifecycleState": mappedRun.LifecycleState,
		"updatedAt":      mappedRun.UpdatedAt,
	})
}

// POST /api/runs/{id}/audit/prepare-commit-message
func (h *APIHandler) PrepareCommitMessage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	if run.Status != "accepted" && run.Status != "accepted_with_warnings" {
		writeError(w, http.StatusConflict, "CONFLICT", fmt.Sprintf("Run status is %q, must be accepted or accepted_with_warnings to prepare commit message", run.Status))
		return
	}

	storeArts, _ := h.store.ListArtifactsByRun(id)

	changedFiles := "No changed files detected."
	for _, art := range storeArts {
		if art.Kind == "git_diff_name_status" || art.Kind == "git_diff_stat" || art.Kind == "git_status" {
			if data, readErr := os.ReadFile(art.Path); readErr == nil {
				changedFiles = string(data)
				break
			}
		}
	}

	title := run.Title
	if title == "" {
		title = "Untitled Run"
	}

	msgContent := fmt.Sprintf(`Commit Title: feat: %s

Commit Body:

%s

---
Prepared by Relay — review before committing.
`, title, changedFiles)

	path, err := artifacts.Write(id, "commit_message_text", "commit_message.txt", []byte(msgContent))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to write commit message artifact: "+err.Error())
		return
	}

	_, _ = h.store.CreateArtifact(id, "commit_message_text", path, "text/plain")
	_, _ = h.store.CreateEvent(id, "info", "Commit message prepared")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"runId":         idStr,
		"commitMessage": msgContent,
		"artifactPath":  path,
		"artifactKind":  "commit_message_text",
	})
}

// POST /api/runs/{id}/audit/close
func (h *APIHandler) CloseRun(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid run ID format")
		return
	}

	run, err := h.store.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Run with ID %d not found", id))
		return
	}

	if run.Status != "accepted" && run.Status != "accepted_with_warnings" {
		writeError(w, http.StatusConflict, "CONFLICT", fmt.Sprintf("Run status is %q, must be accepted or accepted_with_warnings to close", run.Status))
		return
	}

	updatedRun, err := h.store.UpdateRunStatus(id, "completed")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to close run")
		return
	}

	_, _ = h.store.CreateEvent(id, "status_change", "Run closed")

	repoName := "Unknown Repo"
	if repo, err := h.store.GetRepo(updatedRun.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}
	mappedRun := h.mapRunToRelayRun(*updatedRun, repoName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"runId":          idStr,
		"status":         mappedRun.Status,
		"lifecycleState": mappedRun.LifecycleState,
		"updatedAt":      mappedRun.UpdatedAt,
	})
}
