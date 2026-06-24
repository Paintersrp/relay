package runs

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"relay/internal/api/shared"
	appruns "relay/internal/app/runs"
)

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

func brokerSafeArtifactPathForAPI(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return ""
	}
	return clean
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
	case "planner_handoff_provenance_json":
		return "handoff", "Planner Handoff Provenance"
	case "context_packet_json":
		return "handoff", "Context Packet (JSON)"
	case "context_packet_markdown":
		return "handoff", "Context Packet (Markdown)"
	case "context_coverage_report_json":
		return "validation", "Context Coverage Report (JSON)"
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
	case "validation_failure_acceptance_json":
		return "validation", "Validation Failure Acceptance (JSON)"
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
	case "audit_evidence_manifest_json":
		return "audit", "Audit Evidence Manifest (JSON)"
	case "audit_decision_json":
		return "audit", "Audit Decision (JSON)"
	case "audit_revision":
		return "audit", "Audit Revision"
	case "repair_request_json":
		return "validation", "Repair Request (JSON)"
	case "repair_prompt":
		return "prompt", "Repair Prompt"
	case "repair_output":
		return "result", "Repair Output"
	case "repaired_packet":
		return "handoff", "Repaired Packet"
	case "repair_validation_report":
		return "validation", "Repair Validation Report"
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

// buildArtifactDTO maps an app artifact view to a RelayArtifact DTO. When
// includePreview is false the preview field is omitted (intake response parity).
func buildArtifactDTO(idStr string, v appruns.ArtifactView, includePreview bool) RelayArtifact {
	k, l := mapArtifactKindAndLabel(v.Kind)
	dto := RelayArtifact{
		ID:          strconv.FormatInt(v.ID, 10),
		Label:       l,
		Path:        fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, v.Kind),
		Kind:        k,
		StorageKind: v.Kind,
		ContentURL:  fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, v.Kind),
		SizeHint:    v.SizeHint,
		CreatedAt:   shared.ParseAndFormatTime(v.CreatedAt),
		Status:      "ready",
		Filename:    filepath.Base(v.Path),
	}
	if includePreview {
		dto.Preview = v.Preview
	}
	return dto
}

// BuildArtifactDTOs builds artifact DTOs (with preview) from app artifact views.
func BuildArtifactDTOs(idStr string, views []appruns.ArtifactView) []RelayArtifact {
	result := make([]RelayArtifact, 0, len(views))
	for _, v := range views {
		result = append(result, buildArtifactDTO(idStr, v, true))
	}
	return result
}

// BuildArtifacts builds run artifact DTOs (with preview) from run details.
func BuildArtifacts(idStr string, d appruns.RunDetails) []RelayArtifact {
	result := make([]RelayArtifact, 0, len(d.ArtifactViews))
	for _, v := range d.ArtifactViews {
		result = append(result, buildArtifactDTO(idStr, v, true))
	}
	return result
}

// BuildIntakeArtifacts builds run artifact DTOs (without preview) for the intake
// response, preserving the legacy intake artifact shape.
func BuildIntakeArtifacts(idStr string, d appruns.RunDetails) []RelayArtifact {
	result := make([]RelayArtifact, 0, len(d.ArtifactViews))
	for _, v := range d.ArtifactViews {
		result = append(result, buildArtifactDTO(idStr, v, false))
	}
	return result
}

// BuildValidationResult builds the validation summary from run check data.
func BuildValidationResult(d appruns.RunDetails) RelayValidationResult {
	var result RelayValidationResult
	result.Issues = make([]RelayValidationIssue, 0)
	for _, c := range d.Checks {
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
	return RelayApprovalGate{Label: label, State: state, Note: note}
}

func buildLogPreview(d appruns.RunDetails) RelayLogPreview {
	var lines []string
	for _, e := range d.Events {
		tStr := shared.ParseAndFormatTime(e.CreatedAt)
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
	return RelayLogPreview{Lines: lines, Truncated: truncated}
}

// ResolveRunDisplayState derives the normalized status and display fields from a
// raw run status, preserving legacy status alias and severity behavior.
func ResolveRunDisplayState(rawStatus string) (status, activeStep, lifecycleState, state, statusSeverity string) {
	status = rawStatus
	if status == "ready" {
		status = "approved_for_prepare"
	} else if status == "needs_review" {
		status = "intake_needs_review"
	}

	activeStep = "intake"
	lifecycleState = "intake"
	state = "Intake Review"
	statusSeverity = "warning"

	switch status {
	case "draft", "needs_cleanup":
		activeStep, lifecycleState, state, statusSeverity = "intake", "intake", "Intake Review", "warning"
		if status == "needs_cleanup" {
			statusSeverity = "danger"
		}
	case "intake_needs_review", "intake_received":
		activeStep, lifecycleState, state, statusSeverity = "intake", "intake", "Intake Needs Review", "warning"
		if status == "intake_received" {
			state, statusSeverity = "Intake Received", "info"
		}
	case "validated":
		activeStep, lifecycleState, state, statusSeverity = "intake", "intake", "Intake Validated", "info"
	case "approved_for_prepare":
		activeStep, lifecycleState, state, statusSeverity = "prepare", "prepare", "Approved for Prepare", "success"
	case "packet_validated", "repair_validated":
		activeStep, lifecycleState, state, statusSeverity = "prepare", "prepare", "Packet Validated", "info"
	case "packet_validation_failed":
		activeStep, lifecycleState, state, statusSeverity = "prepare", "prepare", "Packet Validation Failed", "danger"
	case "brief_ready_for_review":
		activeStep, lifecycleState, state, statusSeverity = "prepare", "prepare", "Brief Ready for Review", "success"
	case "approved_for_executor":
		activeStep, lifecycleState, state, statusSeverity = "execute", "execute", "Approved for Executor", "success"
	case "executor_dispatched":
		activeStep, lifecycleState, state, statusSeverity = "execute", "execute", "Executor Dispatched", "info"
	case "local_validation_running":
		activeStep, lifecycleState, state, statusSeverity = "execute", "execute", "Local Validation Running", "info"
	case "executor_done":
		activeStep, lifecycleState, state, statusSeverity = "execute", "execute", "Executor Done", "success"
	case "executor_blocked":
		activeStep, lifecycleState, state, statusSeverity = "execute", "failed", "Executor Blocked", "danger"
	case "blocked":
		activeStep, lifecycleState, state, statusSeverity = "intake", "failed", "Blocked", "danger"
	case "agent_done":
		activeStep, lifecycleState, state, statusSeverity = "execute", "execute", "Agent Done", "success"
	case "agent_blocked":
		activeStep, lifecycleState, state, statusSeverity = "execute", "failed", "Agent Blocked", "danger"
	case "agent_result_needs_review":
		activeStep, lifecycleState, state, statusSeverity = "execute", "execute", "Result Needs Review", "warning"
	case "validation_passed", "validation_failed_accepted":
		activeStep, lifecycleState, state, statusSeverity = "audit", "audit", "Audit Review", "warning"
	case "validation_failed":
		activeStep, lifecycleState, state, statusSeverity = "audit", "failed", "Validation Failed", "danger"
	case "audit_ready", "audit_ready_for_review":
		activeStep, lifecycleState, state, statusSeverity = "audit", "audit", "Audit Ready", "warning"
	case "revision_required":
		activeStep, lifecycleState, state, statusSeverity = "audit", "audit", "Revision Required", "warning"
	case "accepted":
		activeStep, lifecycleState, state, statusSeverity = "audit", "audit", "Approved — Ready to Close", "success"
	case "accepted_with_warnings":
		activeStep, lifecycleState, state, statusSeverity = "audit", "audit", "Approved with Warnings", "warning"
	case "completed":
		activeStep, lifecycleState, state, statusSeverity = "audit", "completed", "Completed", "success"
	}
	return
}

func ensureRunPlanContext(c *RelayRunPlanContext) *RelayRunPlanContext {
	if c != nil {
		return c
	}
	return &RelayRunPlanContext{}
}

func hasRelayRunPlanContext(c *RelayRunPlanContext) bool {
	return c != nil && (c.PlanID != "" || c.PassID != "" || c.PlanRowID != "" || c.PassRowID != "")
}

// MapRunToRelayRun maps fully loaded run details to the RelayRun response DTO.
func MapRunToRelayRun(d appruns.RunDetails) RelayRun {
	run := d.Run
	idStr := strconv.FormatInt(run.ID, 10)

	status, activeStep, lifecycleState, state, statusSeverity := ResolveRunDisplayState(run.Status)

	latestExecutionStatus := ""
	if d.LatestExecution != nil {
		latestExecutionStatus = d.LatestExecution.Status
	}

	model := run.SelectedModel
	if model == "" {
		model = run.RecommendedModel
	}

	valResult := BuildValidationResult(d)
	relayArtifacts := BuildArtifacts(idStr, d)

	relayEvents := make([]RelayRunEvent, 0)
	for _, e := range d.Events {
		relayEvents = append(relayEvents, RelayRunEvent{
			ID:        strconv.FormatInt(e.ID, 10),
			RunID:     idStr,
			Kind:      mapEventKind(e.Level, e.Message),
			Message:   e.Message,
			CreatedAt: shared.ParseAndFormatTime(e.CreatedAt),
		})
	}

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

	planContext := buildPlanContext(d)
	provenance, sourceContext := buildProvenanceAndSourceContext(d, &planContext)

	if !hasRelayRunPlanContext(planContext) {
		planContext = nil
	}
	if sourceContext != nil && sourceContext.PlanID == "" && sourceContext.PassID == "" && sourceContext.ContextPacketID == "" && sourceContext.SourceSnapshotID == "" {
		sourceContext = nil
	}

	return RelayRun{
		ID:                    idStr,
		Name:                  run.Title,
		Repo:                  d.RepoName,
		Branch:                run.BranchName,
		Worktree:              d.Worktree,
		ActiveStep:            activeStep,
		Status:                status,
		LifecycleState:        lifecycleState,
		CreatedAt:             shared.ParseAndFormatTime(run.CreatedAt),
		UpdatedAt:             shared.ParseAndFormatTime(run.UpdatedAt),
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
		Executor:              run.ExecutorAdapter,
		ExecutorAdapter:       run.ExecutorAdapter,
		ValidationSummary:     valResult,
		ApprovalGate:          buildApprovalGate(activeStep, run.Status),
		LogPreview:            buildLogPreview(d),
		StepLabels:            stepLabels,
		LatestExecutionStatus: latestExecutionStatus,
		PlanContext:           planContext,
		Provenance:            provenance,
		SourceContext:         sourceContext,
	}
}

func buildPlanContext(d appruns.RunDetails) *RelayRunPlanContext {
	run := d.Run
	var planContext *RelayRunPlanContext
	if run.PlanRowID.Valid || run.PlanPassRowID.Valid {
		planContext = &RelayRunPlanContext{}
	}

	if run.PlanRowID.Valid {
		planContext = ensureRunPlanContext(planContext)
		planContext.PlanRowID = strconv.FormatInt(run.PlanRowID.Int64, 10)
		if d.Plan != nil {
			planContext.PlanID = d.Plan.PlanID
			planContext.PlanTitle = d.Plan.Title
			planContext.ProjectID = d.Plan.ProjectID
			planContext.ProjectRowID = strconv.FormatInt(d.Plan.ProjectRowID, 10)
		}
	}

	if run.PlanPassRowID.Valid {
		planContext = ensureRunPlanContext(planContext)
		planContext.PassRowID = strconv.FormatInt(run.PlanPassRowID.Int64, 10)
		if d.Pass != nil {
			planContext.PassID = d.Pass.PassID
			planContext.PassName = d.Pass.Name
			seq := d.Pass.Sequence
			planContext.PassSequence = &seq
			planContext.PassStatus = d.Pass.Status
			if planContext.PlanRowID == "" {
				planContext.PlanRowID = strconv.FormatInt(d.Pass.PlanRowID, 10)
			}
			if planContext.PlanID == "" || planContext.PlanTitle == "" || planContext.ProjectID == "" {
				if d.PassPlan != nil {
					if planContext.PlanID == "" {
						planContext.PlanID = d.PassPlan.PlanID
					}
					if planContext.PlanTitle == "" {
						planContext.PlanTitle = d.PassPlan.Title
					}
					planContext.ProjectID = d.PassPlan.ProjectID
					planContext.ProjectRowID = strconv.FormatInt(d.PassPlan.ProjectRowID, 10)
				}
			}
		}
	}

	return planContext
}

func buildProvenanceAndSourceContext(d appruns.RunDetails, planContextPtr **RelayRunPlanContext) (*RelayRunProvenance, *RelayRunSourceContext) {
	row := d.Provenance
	if row == nil {
		return nil, nil
	}

	plannerHandoffBytes := row.PlannerHandoffBytes
	provenance := &RelayRunProvenance{
		PlannerHandoffSHA256: row.PlannerHandoffSha256,
		PlannerHandoffBytes:  &plannerHandoffBytes,
		SourceArtifactPath:   row.SourceArtifactPath,
		Source:               row.Source,
		ClientTraceID:        row.ClientTraceID,
		PlanID:               row.PlanID,
		PassID:               row.PassID,
		ContextPacketID:      row.ContextPacketID,
		SourceSnapshotID:     row.SourceSnapshotID,
		ArtifactKind:         "planner_handoff_provenance_json",
	}
	sourceContext := &RelayRunSourceContext{
		PlanID:           row.PlanID,
		PassID:           row.PassID,
		ContextPacketID:  row.ContextPacketID,
		SourceSnapshotID: row.SourceSnapshotID,
		RecordedAt:       shared.ParseAndFormatTime(row.CreatedAt),
	}
	if row.ContextPacketID != "" && d.ContextPacket != nil {
		sourceContext.CoverageReportPath = brokerSafeArtifactPathForAPI(d.ContextPacket.CoverageReportPath)
		if sourceContext.SourceSnapshotID == "" {
			sourceContext.SourceSnapshotID = d.ContextPacket.SourceSnapshotID
		}
	}

	planContext := ensureRunPlanContext(*planContextPtr)
	if planContext.PlanID == "" {
		planContext.PlanID = row.PlanID
	}
	if planContext.PassID == "" {
		planContext.PassID = row.PassID
	}
	if planContext.PlanRowID == "" && row.PlanRowID.Valid {
		planContext.PlanRowID = strconv.FormatInt(row.PlanRowID.Int64, 10)
	}
	if planContext.PassRowID == "" && row.PlanPassRowID.Valid {
		planContext.PassRowID = strconv.FormatInt(row.PlanPassRowID.Int64, 10)
	}
	if planContext.SourceArtifactPath == "" {
		planContext.SourceArtifactPath = row.SourceArtifactPath
	}
	if planContext.ContextPacketID == "" {
		planContext.ContextPacketID = row.ContextPacketID
	}
	if planContext.SourceSnapshotID == "" {
		planContext.SourceSnapshotID = row.SourceSnapshotID
	}
	if planContext.PlannerHandoffSHA256 == "" {
		planContext.PlannerHandoffSHA256 = row.PlannerHandoffSha256
	}
	*planContextPtr = planContext

	return provenance, sourceContext
}
