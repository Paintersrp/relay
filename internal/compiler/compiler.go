package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/validation"
)

type Compiler struct {
	store *store.Store
}

func New(s *store.Store) *Compiler {
	return &Compiler{store: s}
}

type CompileResult struct {
	Success          bool                         `json:"success"`
	RunID            int64                        `json:"run_id"`
	PacketID         string                       `json:"packet_id"`
	ValidationReport *validation.ValidationReport `json:"validation_report"`
	Issues           []string                     `json:"issues"`
}

// CompileApprovedRun compiles a run that is in "approved_for_prepare" state.
func (c *Compiler) CompileApprovedRun(ctx context.Context, runID int64) (*CompileResult, error) {
	// 1. Load run
	run, err := c.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("failed to load run %d: %w", runID, err)
	}
	if run == nil {
		return nil, fmt.Errorf("run %d not found", runID)
	}

	// 2. Enforce state (CR1)
	if run.Status != "approved_for_prepare" {
		return nil, fmt.Errorf("run %d status is %q, but must be approved_for_prepare to compile", runID, run.Status)
	}

	// 3. Load compile inputs (CR2)
	handoffBytes, err := artifacts.Read(runID, "planner_handoff", "planner_handoff.md")
	if err != nil {
		return nil, fmt.Errorf("failed to load planner_handoff.md: %w", err)
	}
	handoffText := string(handoffBytes)

	configBytes, err := artifacts.Read(runID, "run_config", "run_config.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load run_config.json: %w", err)
	}

	var runConfig map[string]interface{}
	if err := json.Unmarshal(configBytes, &runConfig); err != nil {
		return nil, fmt.Errorf("failed to parse run_config.json: %w", err)
	}

	// Optional parsed_frontmatter
	var frontmatter map[string]string
	fmBytes, err := artifacts.Read(runID, "parsed_frontmatter", "parsed_frontmatter.json")
	if err == nil {
		_ = json.Unmarshal(fmBytes, &frontmatter)
	}

	// 4. Parse content
	packetMap, parseIssues := c.parseHandoff(handoffText, runConfig, frontmatter, run.Title)
	if len(parseIssues) > 0 {
		// If critical fields are missing, compile result fails
		result := &CompileResult{
			Success: false,
			RunID:   runID,
			Issues:  parseIssues,
		}
		// Write validation report for parse failures
		var valReport validation.ValidationReport
		valReport.Valid = false
		valReport.RepairEligible = true // Parse/formatting issues are repair-eligible
		for _, issue := range parseIssues {
			valReport.Errors = append(valReport.Errors, validation.ValidationError{
				Type:           "structural",
				Message:        issue,
				RepairEligible: true,
			})
		}
		result.ValidationReport = &valReport
		_ = c.writeReport(runID, &valReport)

		// Update status to packet_validation_failed
		_, _ = c.store.UpdateRunStatus(runID, "packet_validation_failed")
		_, _ = c.store.CreateCheck(runID, "validation", "fail", "Handoff parsing failed", "{}")
		_, _ = c.store.CreateEvent(runID, "warning", "Compile failed: "+strings.Join(parseIssues, "; "))

		return result, nil
	}

	// Marshal compiled packet
	packetBytes, err := json.MarshalIndent(packetMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal compiled packet: %w", err)
	}

	// Write canonical_packet.json temporarily as a draft so validator can inspect it
	// (Avoid silent overwrite: we write it now, and register if valid, or register as failed draft if invalid)
	packetPath, err := artifacts.Write(runID, "canonical_packet", "canonical_packet.json", packetBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to write canonical packet artifact: %w", err)
	}

	// 5. Validate packet
	report, err := validation.ValidatePacketJSON(packetBytes, "handoffs/schema/canonical_packet.schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to run packet validation: %w", err)
	}

	// Register canonical_packet in store
	_ = c.store.DeleteArtifactsByRunKind(runID, "canonical_packet")
	_, _ = c.store.CreateArtifact(runID, "canonical_packet", packetPath, "application/json")

	// Write and register validation report (S10)
	_ = c.writeReport(runID, report)

	packetID := ""
	if meta, ok := packetMap["packet_meta"].(map[string]interface{}); ok {
		if pid, ok := meta["packet_id"].(string); ok {
			packetID = pid
		}
	}

	result := &CompileResult{
		Success:          report.Valid,
		RunID:            runID,
		PacketID:         packetID,
		ValidationReport: report,
	}

	if report.Valid {
		// Update lifecycle state in DB to packet_validated (CR8, S11)
		_, _ = c.store.UpdateRunStatus(runID, "packet_validated")
		_ = c.store.DeleteChecksByRunKind(runID, "validation")
		_, _ = c.store.CreateCheck(runID, "validation", "pass", "Packet validation passed", "{}")
		_, _ = c.store.CreateEvent(runID, "info", fmt.Sprintf("Run compiled successfully: packet %s generated", packetID))
	} else {
		// S11: packet_validation_failed
		_, _ = c.store.UpdateRunStatus(runID, "packet_validation_failed")
		_ = c.store.DeleteChecksByRunKind(runID, "validation")
		for _, e := range report.Errors {
			_, _ = c.store.CreateCheck(runID, "validation", "fail", e.Message, "{}")
		}
		_, _ = c.store.CreateEvent(runID, "warning", fmt.Sprintf("Run compile failed: %d validation errors", len(report.Errors)))
	}

	return result, nil
}

func (c *Compiler) writeReport(runID int64, report *validation.ValidationReport) error {
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	reportPath, err := artifacts.Write(runID, "packet_validation_report", "packet_validation_report.json", reportBytes)
	if err != nil {
		return err
	}
	_ = c.store.DeleteArtifactsByRunKind(runID, "packet_validation_report")
	_, _ = c.store.CreateArtifact(runID, "packet_validation_report", reportPath, "application/json")
	return nil
}

func (c *Compiler) parseHandoff(
	content string,
	runConfig map[string]interface{},
	frontmatter map[string]string,
	runTitle string,
) (map[string]interface{}, []string) {
	var issues []string

	// Helper to extract section using XML tags first, falling back to Markdown headings
	getSection := func(tagName, mdHeading string) (string, bool) {
		if val, ok := extractXMLSection(content, tagName); ok {
			return val, true
		}
		if val, ok := extractMarkdownSection(content, mdHeading); ok {
			return val, true
		}
		return "", false
	}

	// 1. Parse Metadata & derive Task Slug and Packet ID
	createdDate := time.Now().Format("2006-01-02")
	var taskSlug string
	var packetID string
	var targetExecutor = "deepseek-v4-flash"
	var repoTarget string
	var branchContext string

	// Extract metadata from markdown yaml block if present
	var mdMeta map[string]interface{}
	if metaText, ok := getSection("handoff_meta", "artifact metadata"); ok {
		mdMeta = parseYAMLBlock(metaText)
	} else if strings.Contains(content, "## Artifact Metadata") {
		// Fallback direct extraction
		if yamlText, ok := extractMarkdownSection(content, "artifact metadata"); ok {
			mdMeta = parseYAMLBlock(yamlText)
		}
	}

	// Merge config, frontmatter, and parsed markdown metadata
	getString := func(key string) string {
		if mdMeta != nil {
			if v, ok := mdMeta[key].(string); ok && v != "" {
				return v
			}
		}
		if frontmatter != nil {
			if v, ok := frontmatter[key]; ok && v != "" {
				return v
			}
		}
		if v, ok := runConfig[key].(string); ok && v != "" {
			return v
		}
		return ""
	}

	repoTarget = getString("repo")
	if repoTarget == "" {
		repoTarget = getString("repo_target")
	}
	branchContext = getString("branch")
	if branchContext == "" {
		branchContext = getString("branch_context")
	}
	targetExecutor = getString("target_executor")
	if targetExecutor == "" {
		targetExecutor = "deepseek-v4-flash"
	}

	// Determine Task Slug
	if handoffID := getString("handoff_id"); handoffID != "" {
		re := regexp.MustCompile(`^planner-handoff-(?:\d{4}-\d{2}-\d{2}-)?`)
		taskSlug = re.ReplaceAllString(handoffID, "")
	}
	if taskSlug == "" {
		// Slugify runTitle
		taskSlug = slugify(runTitle)
	}
	if taskSlug == "" {
		issues = append(issues, "No safe task slug or packet ID could be derived")
		return nil, issues
	}

	packetID = fmt.Sprintf("packet-%s-%s", createdDate, taskSlug)

	// Artifact Paths
	artifactPaths := map[string]string{
		"canonical_packet":  fmt.Sprintf("handoffs/packets/%s_%s.canonical-packet.json", createdDate, taskSlug),
		"executor_brief":    fmt.Sprintf("handoffs/briefs/%s_%s.executor-brief.md", createdDate, taskSlug),
		"executor_result":   fmt.Sprintf("handoffs/results/%s_%s.executor-result.txt", createdDate, taskSlug),
		"validation_report": fmt.Sprintf("handoffs/validation/%s_%s.validation-report.json", createdDate, taskSlug),
		"audit_packet":      fmt.Sprintf("handoffs/audits/%s_%s.audit-packet.md", createdDate, taskSlug),
	}

	packetMeta := map[string]interface{}{
		"packet_id":       packetID,
		"task_slug":       taskSlug,
		"target_executor": targetExecutor,
		"repo_target":     repoTarget,
		"branch_context":  branchContext,
		"lifecycle_state": "packet_created",
		"artifact_paths":  artifactPaths,
	}

	// 2. Parse planner_context
	snapshot, ok := getSection("context_snapshot", "context snapshot")
	if !ok || snapshot == "" {
		issues = append(issues, "Missing required section: context_snapshot")
	}

	decisionText, _ := getSection("decision_log", "decision log")
	decisionLog := parseMarkdownList(decisionText)

	constraintsText, _ := getSection("constraints", "constraints")
	constraints := parseMarkdownList(constraintsText)

	assumptionsText, _ := getSection("assumptions", "assumptions")
	assumptions := parseMarkdownList(assumptionsText)

	factsText, _ := getSection("known_repo_facts", "known repo facts")
	knownRepoFacts := parseMarkdownList(factsText)

	boundaryText, _ := getSection("pass_boundary", "pass boundary")
	boundary := parseYAMLBlock(boundaryText)

	unresolvedText, _ := getSection("unresolved_questions", "unresolved questions")
	unresolvedQuestions := parseMarkdownList(unresolvedText)

	plannerContext := map[string]interface{}{
		"context_snapshot":     snapshot,
		"decision_log":         decisionLog,
		"constraints":          constraints,
		"assumptions":          assumptions,
		"known_repo_facts":     knownRepoFacts,
		"pass_boundary":        boundary,
		"unresolved_questions": unresolvedQuestions,
	}

	// 3. Parse execution_payload
	briefText, ok := getSection("packet_maker_brief", "packet maker brief")
	var goal, scope, expectedBehavior, completionContract string
	var nonGoals, fileTargets, implementationSteps, codeRequirements []string
	if ok && briefText != "" {
		goal = extractBriefSection(briefText, "goal")
		if goal == "" {
			goal, _ = extractMarkdownSection(briefText, "goal")
		}
		scope = extractBriefSection(briefText, "scope")
		if scope == "" {
			scope, _ = extractMarkdownSection(briefText, "scope")
		}

		nonGoalsText := extractBriefSection(briefText, "non-goals")
		if nonGoalsText == "" {
			nonGoalsText = extractBriefSection(briefText, "non_goals")
		}
		if nonGoalsText == "" {
			nonGoalsText, _ = extractMarkdownSection(briefText, "non-goals")
		}
		nonGoals = parseMarkdownList(nonGoalsText)

		targetsText := extractBriefSection(briefText, "likely file targets")
		if targetsText == "" {
			targetsText = extractBriefSection(briefText, "file targets")
		}
		if targetsText == "" {
			targetsText, _ = extractMarkdownSection(briefText, "likely file targets")
		}
		if targetsText == "" {
			targetsText, _ = extractMarkdownSection(briefText, "file targets")
		}
		fileTargets = parseFileTargets(targetsText)

		stepsText := extractBriefSection(briefText, "required implementation steps")
		if stepsText == "" {
			stepsText = extractBriefSection(briefText, "implementation steps")
		}
		if stepsText == "" {
			stepsText, _ = extractMarkdownSection(briefText, "required implementation steps")
		}
		if stepsText == "" {
			stepsText, _ = extractMarkdownSection(briefText, "implementation steps")
		}
		implementationSteps = parseMarkdownList(stepsText)

		expectedBehavior = extractBriefSection(briefText, "expected behavior")
		if expectedBehavior == "" {
			expectedBehavior, _ = extractMarkdownSection(briefText, "expected behavior")
		}

		completionContract = extractBriefSection(briefText, "completion requirements")
		if completionContract == "" {
			completionContract = extractBriefSection(briefText, "completion contract")
		}
		if completionContract == "" {
			completionContract, _ = extractMarkdownSection(briefText, "completion requirements")
		}
		if completionContract == "" {
			completionContract, _ = extractMarkdownSection(briefText, "completion contract")
		}
	} else {
		// Try parsing from top-level headings directly
		goal = extractBriefSection(content, "goal")
		if goal == "" {
			goal, _ = getSection("goal", "goal")
		}
		scope = extractBriefSection(content, "scope")
		if scope == "" {
			scope, _ = getSection("scope", "scope")
		}

		nonGoalsText := extractBriefSection(content, "non-goals")
		if nonGoalsText == "" {
			nonGoalsText = extractBriefSection(content, "non_goals")
		}
		if nonGoalsText == "" {
			nonGoalsText, _ = getSection("non_goals", "non-goals")
		}
		nonGoals = parseMarkdownList(nonGoalsText)

		targetsText := extractBriefSection(content, "likely file targets")
		if targetsText == "" {
			targetsText = extractBriefSection(content, "file targets")
		}
		if targetsText == "" {
			targetsText, _ = getSection("file_targets", "likely file targets")
		}
		if targetsText == "" {
			targetsText, _ = getSection("file_targets", "file targets")
		}
		fileTargets = parseFileTargets(targetsText)

		stepsText := extractBriefSection(content, "required implementation steps")
		if stepsText == "" {
			stepsText = extractBriefSection(content, "implementation steps")
		}
		if stepsText == "" {
			stepsText, _ = getSection("implementation_steps", "required implementation steps")
		}
		if stepsText == "" {
			stepsText, _ = getSection("implementation_steps", "implementation steps")
		}
		implementationSteps = parseMarkdownList(stepsText)

		expectedBehavior = extractBriefSection(content, "expected behavior")
		if expectedBehavior == "" {
			expectedBehavior, _ = getSection("expected_behavior", "expected behavior")
		}
		completionContract = extractBriefSection(content, "completion requirements")
		if completionContract == "" {
			completionContract = extractBriefSection(content, "completion contract")
		}
		if completionContract == "" {
			completionContract, _ = getSection("completion_contract", "completion contract")
		}
	}

	// If required executable sections are missing, fail explicitly (CR4)
	if goal == "" {
		issues = append(issues, "Missing required execution section: goal")
	}
	if scope == "" {
		issues = append(issues, "Missing required execution section: scope")
	}
	if len(implementationSteps) == 0 {
		issues = append(issues, "Missing required execution section: implementation_steps")
	}

	// Add targets from config (CR5)
	if cfgTargets, ok := runConfig["file_targets"].([]interface{}); ok {
		for _, t := range cfgTargets {
			if ts, ok := t.(string); ok {
				exists := false
				for _, ft := range fileTargets {
					if ft == ts {
						exists = true
						break
					}
				}
				if !exists {
					fileTargets = append(fileTargets, ts)
				}
			}
		}
	}
	if len(fileTargets) == 0 {
		issues = append(issues, "Missing required execution section: file_targets")
	}

	// Parse validation expectations / commands
	valText, _ := getSection("validation_expectations", "validation expectations")
	if len(valText) == 0 {
		valText, _ = getSection("validation", "validation")
	}
	validationCmds := parseValidationCommands(valText)
	if len(validationCmds) == 0 {
		// Try load from config
		if cfgCmds, ok := runConfig["validation_commands"].([]interface{}); ok {
			for _, c := range cfgCmds {
				if cmdMap, ok := c.(map[string]interface{}); ok {
					cmdStr, _ := cmdMap["command"].(string)
					reqVal, _ := cmdMap["required"].(bool)
					purp, _ := cmdMap["purpose"].(string)
					if cmdStr != "" {
						validationCmds = append(validationCmds, ValidationCommand{
							Command:  cmdStr,
							Required: reqVal,
							Purpose:  purp,
						})
					}
				}
			}
		}
	}

	// Parse code requirements (or provide default if empty)
	reqText, _ := getSection("code_requirements", "code requirements")
	codeRequirements = parseMarkdownList(reqText)
	if len(codeRequirements) == 0 {
		codeRequirements = []string{"Preserve all existing code structure and comments unless explicitly asked to modify."}
	}

	// Default response format
	respFormatText, _ := getSection("executor_final_response_format", "executor final response format")
	if respFormatText == "" {
		respFormatText = "Output only the completed file edits and validation outcomes matching the brief contract."
	}

	executionPayload := map[string]interface{}{
		"goal":                           goal,
		"scope":                          scope,
		"non_goals":                      nonGoals,
		"file_targets":                   fileTargets,
		"implementation_steps":           implementationSteps,
		"code_requirements":              codeRequirements,
		"validation_commands":            validationCmds,
		"expected_behavior":              expectedBehavior,
		"completion_contract":            completionContract,
		"executor_final_response_format": respFormatText,
	}

	// 4. Parse audit_seed (priorities, scope drift, risk checks)
	prioritiesText, _ := getSection("audit_priorities", "audit priorities")
	auditChecklist := parseMarkdownList(prioritiesText)
	if len(auditChecklist) == 0 {
		auditChecklist = []string{"Confirm changes are exactly scoped to file targets."}
	}

	// Generate grounded checks
	scopeDriftChecks := []string{"Verify that no edits were made to files outside target files."}
	nonGoalChecks := []string{}
	for _, ng := range nonGoals {
		nonGoalChecks = append(nonGoalChecks, fmt.Sprintf("Verify that out-of-scope goal %q was not implemented.", ng))
	}
	if len(nonGoalChecks) == 0 {
		nonGoalChecks = []string{"Verify that no unrelated improvements or styling refactors were performed."}
	}

	fileScopeChecks := []string{}
	for _, ft := range fileTargets {
		fileScopeChecks = append(fileScopeChecks, fmt.Sprintf("Confirm that edits to %s only satisfy the goal.", ft))
	}

	riskChecks := []string{"Confirm that no secret keys, credentials, or tokens were introduced."}
	for _, cs := range constraints {
		riskChecks = append(riskChecks, fmt.Sprintf("Confirm constraint %q was respected.", cs))
	}

	valExpectations := []string{}
	for _, vc := range validationCmds {
		valExpectations = append(valExpectations, fmt.Sprintf("Confirm command %q ran and passed.", vc.Command))
	}

	manualReviewChecklist := []string{"Review git diff to verify zero unrelated code formatting churn."}

	auditSeed := map[string]interface{}{
		"audit_checklist":         auditChecklist,
		"scope_drift_checks":      scopeDriftChecks,
		"non_goal_checks":         nonGoalChecks,
		"file_scope_checks":       fileScopeChecks,
		"risk_checks":             riskChecks,
		"validation_expectations": valExpectations,
		"manual_review_checklist": manualReviewChecklist,
	}

	result := map[string]interface{}{
		"packet_meta":       packetMeta,
		"planner_context":   plannerContext,
		"execution_payload": executionPayload,
		"audit_seed":        auditSeed,
	}

	return result, issues
}

func extractXMLSection(content, tag string) (string, bool) {
	startTag := "<" + tag + ">"
	endTag := "</" + tag + ">"
	startIdx := strings.Index(content, startTag)
	if startIdx == -1 {
		return "", false
	}
	endIdx := strings.Index(content, endTag)
	if endIdx == -1 {
		return "", false
	}
	return strings.TrimSpace(content[startIdx+len(startTag) : endIdx]), true
}

func extractMarkdownSection(content, heading string) (string, bool) {
	lines := strings.Split(content, "\n")
	var sectionLines []string
	found := false
	normHeading := strings.ToLower(strings.TrimSpace(heading))

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			hText := strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			// Remove punctuation from header text to facilitate matches
			hText = strings.ReplaceAll(hText, ":", "")
			if found {
				break
			}
			if hText == normHeading || strings.Contains(hText, normHeading) {
				found = true
				continue
			}
		}
		if found {
			sectionLines = append(sectionLines, line)
		}
	}
	return strings.TrimSpace(strings.Join(sectionLines, "\n")), found
}

func parseYAMLBlock(text string) map[string]interface{} {
	result := make(map[string]interface{})
	lines := strings.Split(text, "\n")
	var currentKey string
	var currentList []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "-") {
			if currentKey != "" {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				val = strings.Trim(val, `"'`)
				currentList = append(currentList, val)
				result[currentKey] = currentList
			}
			continue
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) < 1 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
			currentKey = key
			currentList = []string{}
			result[currentKey] = currentList
			continue
		}

		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		result[key] = val
		currentKey = ""
	}
	return result
}

func parseMarkdownList(text string) []string {
	lines := strings.Split(text, "\n")
	var items []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
			item := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
			if item != "" {
				items = append(items, item)
			}
		} else if idx := strings.Index(trimmed, "."); idx > 0 && idx < 5 {
			numStr := trimmed[:idx]
			isNumeric := true
			for _, char := range numStr {
				if char < '0' || char > '9' {
					isNumeric = false
					break
				}
			}
			if isNumeric {
				item := strings.TrimSpace(trimmed[idx+1:])
				if item != "" {
					items = append(items, item)
				}
			}
		}
	}
	if len(items) == 0 && len(text) > 0 {
		for _, line := range lines {
			t := strings.TrimSpace(line)
			if t != "" {
				items = append(items, t)
			}
		}
	}
	return items
}

func parseFileTargets(text string) []string {
	lines := strings.Split(text, "\n")
	var targets []string
	pathRe := regexp.MustCompile("`([^`]+)`")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		matches := pathRe.FindAllStringSubmatch(trimmed, -1)
		for _, m := range matches {
			if len(m) > 1 {
				p := strings.TrimSpace(m[1])
				if p != "" && !strings.Contains(p, " ") {
					targets = append(targets, p)
				}
			}
		}
		if len(matches) == 0 {
			if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
				parts := strings.Fields(strings.TrimLeft(trimmed, "-*"))
				if len(parts) > 0 {
					p := parts[0]
					if p != "" && !strings.Contains(p, " ") && (strings.Contains(p, "/") || strings.Contains(p, ".")) {
						targets = append(targets, p)
					}
				}
			}
		}
	}
	return targets
}

type ValidationCommand struct {
	Command         string `json:"command"`
	Required        bool   `json:"required"`
	Purpose         string `json:"purpose"`
	SuccessSignal   string `json:"success_signal,omitempty"`
	FailureHandling string `json:"failure_handling,omitempty"`
}

func parseValidationCommands(text string) []ValidationCommand {
	var cmds []ValidationCommand
	lines := strings.Split(text, "\n")
	var currentCmd *ValidationCommand

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "- V") && strings.Contains(trimmed, ":") {
			if currentCmd != nil {
				cmds = append(cmds, *currentCmd)
			}
			currentCmd = &ValidationCommand{Required: true}
			continue
		}

		if currentCmd == nil {
			if strings.Contains(trimmed, "command:") {
				currentCmd = &ValidationCommand{Required: true}
			} else {
				continue
			}
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "`\"'")

		switch {
		case strings.Contains(key, "command"):
			currentCmd.Command = val
		case strings.Contains(key, "required"):
			currentCmd.Required = (val == "true" || strings.ToLower(val) == "yes")
		case strings.Contains(key, "purpose"):
			currentCmd.Purpose = val
		case strings.Contains(key, "success signal") || strings.Contains(key, "success_signal"):
			currentCmd.SuccessSignal = val
		case strings.Contains(key, "failure handling") || strings.Contains(key, "failure_handling"):
			currentCmd.FailureHandling = val
		}
	}

	if currentCmd != nil {
		cmds = append(cmds, *currentCmd)
	}

	if len(cmds) == 0 && len(text) > 0 {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if idx := strings.Index(trimmed, "`"); idx >= 0 {
				endIdx := strings.Index(trimmed[idx+1:], "`")
				if endIdx >= 0 {
					cmdStr := trimmed[idx+1 : idx+1+endIdx]
					if cmdStr != "" {
						cmds = append(cmds, ValidationCommand{
							Command:  cmdStr,
							Required: true,
							Purpose:  "Validate implementation",
						})
					}
				}
			}
		}
	}

	return cmds
}

func slugify(s string) string {
	s = strings.ToLower(s)
	// Replace non-alphanumeric characters with "-"
	reg := regexp.MustCompile("[^a-z0-9]+")
	s = reg.ReplaceAllString(s, "-")
	// Trim leading/trailing dashes
	s = strings.Trim(s, "-")
	return s
}

func extractBriefSection(content, sectionName string) string {
	lines := strings.Split(content, "\n")
	var sectionLines []string
	found := false
	normSectionName := strings.ToLower(strings.TrimSpace(sectionName))

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Section markers are lines ending with a colon, e.g. "Goal:" or "Non-goals:"
		if strings.HasSuffix(trimmed, ":") {
			name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(trimmed, ":")))
			if found {
				// We already found our section, another section header ends it
				break
			}
			if name == normSectionName || strings.Contains(name, normSectionName) {
				found = true
				continue
			}
		}

		if found {
			sectionLines = append(sectionLines, line)
		}
	}
	return strings.TrimSpace(strings.Join(sectionLines, "\n"))
}
