package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"relay/internal/app/plans"
	"relay/internal/artifacts"
	"relay/internal/speccompiler"
	"relay/internal/store"
	"relay/internal/validation"

	"gopkg.in/yaml.v3"
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
	if run.Status != "approved_for_prepare" && run.Status != "packet_validation_failed" {
		return nil, fmt.Errorf("run %d status is %q, but must be approved_for_prepare or packet_validation_failed to compile", runID, run.Status)
	}

	isRetry := run.Status == "packet_validation_failed"
	if isRetry {
		_, _ = c.store.CreateEvent(runID, "info", "Compile retry started")
		_ = c.store.DeleteChecksByRunKind(runID, "validation")
		_ = c.store.DeleteArtifactsByRunKind(runID, "canonical_packet")
		_ = c.store.DeleteArtifactsByRunKind(runID, "packet_validation_report")
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
		var issueStrs []string
		for _, issue := range parseIssues {
			issueStrs = append(issueStrs, issue.Message)
		}
		// If critical fields are missing, compile result fails
		result := &CompileResult{
			Success: false,
			RunID:   runID,
			Issues:  issueStrs,
		}
		// Write validation report for parse failures
		var valReport validation.ValidationReport
		valReport.Valid = false
		valReport.RepairEligible = true // Parse/formatting issues are repair-eligible
		for _, issue := range parseIssues {
			valReport.Errors = append(valReport.Errors, issue)
		}
		result.ValidationReport = &valReport
		_ = c.writeReport(runID, &valReport)

		// Update status to packet_validation_failed
		updatedRun, statusErr := c.store.UpdateRunStatus(runID, "packet_validation_failed")
		if statusErr != nil {
			return nil, fmt.Errorf("update run status to packet_validation_failed: %w", statusErr)
		}
		if syncErr := plans.NewRunLifecycleService(c.store).SyncAssociatedPassForRunStatus(updatedRun); syncErr != nil {
			return nil, fmt.Errorf("sync associated pass status: %w", syncErr)
		}
		_, _ = c.store.CreateCheck(runID, "validation", "fail", "Handoff parsing failed", "{}")
		var failMsg string
		if isRetry {
			failMsg = "Compile retry failed: " + strings.Join(issueStrs, "; ")
		} else {
			failMsg = "Compile failed: " + strings.Join(issueStrs, "; ")
		}
		_, _ = c.store.CreateEvent(runID, "warning", failMsg)

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
	report, err := validation.ValidatePacketJSON(packetBytes, "relay-contracts/schema/canonical_packet.schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to run packet validation: %w", err)
	}
	applyExecutionPayloadProjectionDiagnostics(report, packetBytes)

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
		updatedRun, statusErr := c.store.UpdateRunStatus(runID, "packet_validated")
		if statusErr != nil {
			return nil, fmt.Errorf("update run status to packet_validated: %w", statusErr)
		}
		if syncErr := plans.NewRunLifecycleService(c.store).SyncAssociatedPassForRunStatus(updatedRun); syncErr != nil {
			return nil, fmt.Errorf("sync associated pass status: %w", syncErr)
		}
		_ = c.store.DeleteChecksByRunKind(runID, "validation")
		_, _ = c.store.CreateCheck(runID, "validation", "pass", "Packet validation passed", "{}")
		var successMsg string
		if isRetry {
			successMsg = fmt.Sprintf("Compile retry completed: packet %s generated", packetID)
		} else {
			successMsg = fmt.Sprintf("Run compiled successfully: packet %s generated", packetID)
		}
		_, _ = c.store.CreateEvent(runID, "info", successMsg)
	} else {
		// S11: packet_validation_failed
		updatedRun, statusErr := c.store.UpdateRunStatus(runID, "packet_validation_failed")
		if statusErr != nil {
			return nil, fmt.Errorf("update run status to packet_validation_failed: %w", statusErr)
		}
		if syncErr := plans.NewRunLifecycleService(c.store).SyncAssociatedPassForRunStatus(updatedRun); syncErr != nil {
			return nil, fmt.Errorf("sync associated pass status: %w", syncErr)
		}
		_ = c.store.DeleteChecksByRunKind(runID, "validation")
		for _, e := range report.Errors {
			_, _ = c.store.CreateCheck(runID, "validation", "fail", e.Message, "{}")
		}
		var failMsg string
		if isRetry {
			failMsg = fmt.Sprintf("Compile retry failed: %d validation errors", len(report.Errors))
		} else {
			failMsg = fmt.Sprintf("Run compile failed: %d validation errors", len(report.Errors))
		}
		_, _ = c.store.CreateEvent(runID, "warning", failMsg)
	}

	return result, nil
}

func applyExecutionPayloadProjectionDiagnostics(report *validation.ValidationReport, packetBytes []byte) {
	_, diagnostics := speccompiler.ProjectExecutionPayload(packetBytes)
	for _, diagnostic := range diagnostics {
		report.Valid = false
		report.Errors = append(report.Errors, validation.ValidationError{
			Type:           "structural",
			Code:           diagnostic.Code,
			Message:        diagnostic.Message,
			RepairEligible: false,
		})
	}
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
) (map[string]interface{}, []validation.ValidationError) {
	var issues []validation.ValidationError

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
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "No safe task slug or packet ID could be derived", RepairEligible: true})
		return nil, issues
	}

	packetID = fmt.Sprintf("packet-%s-%s", createdDate, taskSlug)

	createdAt := parseCreatedAt(getString("created_at"))

	sourcePath := getString("intended_handoff_path")
	if sourcePath == "" {
		sourcePath = fmt.Sprintf("handoffs/planner/%s_%s.planner-handoff.md", createdDate, taskSlug)
	}
	intendedPath := getString("target_packet_path")
	if intendedPath == "" {
		intendedPath = getString("intended_packet_path")
	}
	if intendedPath == "" {
		intendedPath = fmt.Sprintf("handoffs/packets/%s_%s.canonical-packet.json", createdDate, taskSlug)
	}

	// Artifact Paths
	artifactPaths := map[string]string{
		"planner_handoff":   sourcePath,
		"canonical_packet":  intendedPath,
		"executor_brief":    fmt.Sprintf("handoffs/briefs/%s_%s.executor-brief.md", createdDate, taskSlug),
		"executor_result":   fmt.Sprintf("handoffs/results/%s_%s.executor-result.txt", createdDate, taskSlug),
		"validation_report": fmt.Sprintf("handoffs/validation/%s_%s.validation-report.json", createdDate, taskSlug),
		"audit_packet":      fmt.Sprintf("handoffs/audits/%s_%s.audit-packet.md", createdDate, taskSlug),
	}

	packetMeta := map[string]interface{}{
		"packet_id":        packetID,
		"protocol_version": "1.0.0",
		"schema_version":   "1.0.0",
		"created_at":       createdAt,
		"producer": map[string]interface{}{
			"kind":    "middleware",
			"name":    "relay-packet-compiler",
			"version": getString("compiler_version"),
		},
		"source_planner_handoff_path": sourcePath,
		"intended_packet_path":        intendedPath,
		"task_slug":                   taskSlug,
		"target_executor":             targetExecutor,
		"repo_target":                 repoTarget,
		"branch_context":              branchContext,
		"content_profile":             getString("content_profile"),
		"lifecycle_state":             "packet_created",
		"artifact_paths":              artifactPaths,
	}
	producer := packetMeta["producer"].(map[string]interface{})
	if version, ok := producer["version"].(string); !ok || strings.TrimSpace(version) == "" {
		producer["version"] = "1.0.0"
	}
	if profile, _ := packetMeta["content_profile"].(string); profile == "" {
		packetMeta["content_profile"] = "implementation_ready"
	}

	// 2. Parse planner_context
	snapshot, ok := getSection("context_snapshot", "context snapshot")
	if !ok || snapshot == "" {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Missing required section: context_snapshot", RepairEligible: true})
	}
	contextSnapshotArr := parseContextSnapshot(snapshot)

	userRequestSummary := ""
	if len(contextSnapshotArr) > 0 {
		userRequestSummary = contextSnapshotArr[0]
	} else {
		userRequestSummary = runTitle
	}

	// Extract briefText early so we can parse nested fields from it
	briefText, _ := getSection("compiler_input", "compiler input")

	decisionText, _ := getSection("decision_log", "decision log")
	decisionLog := parseDecisionLog(decisionText)
	if decisionLog == nil {
		decisionLog = []map[string]interface{}{}
	}

	constraintsText, _ := getSection("constraints", "constraints")
	constraints := parseConstraints(constraintsText)
	if constraints == nil {
		constraints = []map[string]interface{}{}
	}

	assumptionsText, _ := getSection("assumptions", "assumptions")
	assumptions := parseAssumptions(assumptionsText)
	if assumptions == nil {
		assumptions = []map[string]interface{}{}
	}

	factsText, _ := getSection("known_repo_facts", "known repo facts")
	knownRepoFacts := parseKnownRepoFacts(factsText)
	if knownRepoFacts == nil {
		knownRepoFacts = []map[string]interface{}{}
	}

	boundaryText, _ := getSection("pass_boundary", "pass boundary")
	boundary := parsePassBoundary(boundaryText)

	unresolvedText, _ := getSection("unresolved_questions", "unresolved questions")
	if unresolvedText == "" && briefText != "" {
		unresolvedText = extractSubsection(briefText, "unresolved questions")
	}
	if unresolvedText == "" {
		unresolvedText = extractSubsection(content, "unresolved questions")
	}
	unresolvedQuestions := parseUnresolvedQuestions(unresolvedText)
	if unresolvedQuestions == nil {
		unresolvedQuestions = []map[string]interface{}{}
	}

	rejectedText, _ := getSection("rejected_alternatives", "rejected alternatives")
	if rejectedText == "" && briefText != "" {
		rejectedText = extractSubsection(briefText, "rejected alternatives")
	}
	if rejectedText == "" {
		rejectedText = extractSubsection(content, "rejected alternatives")
	}
	rejectedAlternatives := parseRejectedAlternatives(rejectedText)
	if rejectedAlternatives == nil {
		rejectedAlternatives = []map[string]interface{}{}
	}

	riskText, _ := getSection("risk_register", "risk register")
	if riskText == "" && briefText != "" {
		riskText = extractSubsection(briefText, "risk register")
	}
	if riskText == "" {
		riskText = extractSubsection(content, "risk register")
	}
	riskRegister := parseRiskRegister(riskText)
	if len(riskRegister) == 0 {
		// Attempt to derive risks from audit priorities
		prioritiesText, _ := getSection("audit_priorities", "audit priorities")
		riskRegister = deriveRisksFromAudit(prioritiesText, constraints)
	}
	if riskRegister == nil {
		riskRegister = []map[string]interface{}{}
	}

	plannerContext := map[string]interface{}{
		"user_request_summary":  userRequestSummary,
		"context_snapshot":      contextSnapshotArr,
		"decision_log":          decisionLog,
		"constraints":           constraints,
		"assumptions":           assumptions,
		"known_repo_facts":      knownRepoFacts,
		"pass_boundary":         boundary,
		"unresolved_questions":  unresolvedQuestions,
		"rejected_alternatives": rejectedAlternatives,
		"risk_register":         riskRegister,
	}

	// 3. Parse execution_payload
	briefText, ok = getSection("compiler_input", "compiler input")
	var goal, scope, expectedBehaviorRaw, completionContractRaw string
	var nonGoals []string
	var fileTargets []map[string]interface{}
	var implementationSteps []map[string]interface{}
	var codeRequirements []map[string]interface{}
	var validationCmds []ValidationCommand
	validationMode := "commands"
	validationFailurePolicy := "block"
	var expectedBehavior []string
	var completionContract map[string]interface{}
	var executionPayloadMetadata map[string]interface{}
	structuredParsed := false
	executionSpecProjected := false

	if ok && briefText != "" {
		if parsed, parsedOK := parseStructuredCompilerInput(briefText); parsedOK {
			structuredParsed = true
			goal = parsed.Goal
			scope = parsed.Scope
			nonGoals = parsed.NonGoals
			fileTargets = parsed.FileTargets
			implementationSteps = parsed.ImplementationSteps
			codeRequirements = parsed.CodeRequirements
			validationCmds = parsed.ValidationCommands
			validationMode = parsed.ValidationMode
			validationFailurePolicy = parsed.ValidationFailurePolicy
			expectedBehavior = parsed.ExpectedBehavior
			completionContract = parsed.CompletionContract
		} else {
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
			fileTargets = parseFileTargetsStructured(targetsText)

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
			implementationSteps = parseImplementationSteps(stepsText)

			expectedBehaviorRaw = extractBriefSection(briefText, "expected behavior")
			if expectedBehaviorRaw == "" {
				expectedBehaviorRaw, _ = extractMarkdownSection(briefText, "expected behavior")
			}

			completionContractRaw = extractBriefSection(briefText, "completion requirements")
			if completionContractRaw == "" {
				completionContractRaw = extractBriefSection(briefText, "completion contract")
			}
			if completionContractRaw == "" {
				completionContractRaw, _ = extractMarkdownSection(briefText, "completion requirements")
			}
			if completionContractRaw == "" {
				completionContractRaw, _ = extractMarkdownSection(briefText, "completion contract")
			}
		}
	} else {
		if projected, projectionIssues, projectedOK := projectExecutionSpecCompatibility(content); projectedOK {
			executionSpecProjected = true
			goal = projected.Goal
			scope = projected.Scope
			nonGoals = projected.NonGoals
			fileTargets = projected.FileTargets
			implementationSteps = projected.ImplementationSteps
			codeRequirements = projected.CodeRequirements
			validationCmds = projected.ValidationCommands
			validationMode = projected.ValidationMode
			validationFailurePolicy = projected.ValidationFailurePolicy
			expectedBehavior = projected.ExpectedBehavior
			completionContract = projected.CompletionContract
			executionPayloadMetadata = projected.ExecutionPayloadMetadata
			if selectedPass := strings.TrimPrefix(projected.SelectedPassID, "PASS-"); selectedPass != projected.SelectedPassID {
				if passNum, err := strconv.Atoi(selectedPass); err == nil && passNum > 0 {
					boundary["current_pass"] = passNum
				}
			}
			if len(nonGoals) > 0 {
				boundary["out_of_scope_for_this_pass"] = nonGoals
			}
			if scope != "" {
				boundary["this_pass_scope"] = scope
			}
			plannerContext["pass_boundary"] = boundary
			for _, issue := range projectionIssues {
				issues = append(issues, issue)
			}
		} else {
			if len(projectionIssues) > 0 {
				for _, issue := range projectionIssues {
					issues = append(issues, issue)
				}
			} else {
				issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingCompilerInput, Message: "Missing required section: compiler_input", RepairEligible: false})
			}
			// Try parsing from top-level headings directly for legacy handoffs.
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
			fileTargets = parseFileTargetsStructured(targetsText)

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
			implementationSteps = parseImplementationSteps(stepsText)

			expectedBehaviorRaw = extractBriefSection(content, "expected behavior")
			if expectedBehaviorRaw == "" {
				expectedBehaviorRaw, _ = getSection("expected_behavior", "expected behavior")
			}
			completionContractRaw = extractBriefSection(content, "completion requirements")
			if completionContractRaw == "" {
				completionContractRaw = extractBriefSection(content, "completion contract")
			}
			if completionContractRaw == "" {
				completionContractRaw, _ = getSection("completion_contract", "completion contract")
			}
		}
	}

	// Parse code requirements (or provide default if empty)
	if !structuredParsed && !executionSpecProjected {
		reqText, _ := getSection("code_requirements", "code requirements")
		if len(reqText) == 0 {
			reqText, _ = getSection("code", "code requirements")
		}
		if len(reqText) == 0 && briefText != "" {
			reqText = extractSubsection(briefText, "code requirements")
		}
		if len(reqText) == 0 {
			reqText = extractSubsection(content, "code requirements")
		}
		codeRequirements = parseCodeRequirements(reqText)
	}
	if codeRequirements == nil {
		codeRequirements = []map[string]interface{}{}
	}

	// If required executable sections are missing, fail explicitly (CR4)
	if goal == "" {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Missing required execution section: goal", RepairEligible: true})
	}
	if scope == "" {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Missing required execution section: scope", RepairEligible: true})
	}
	if len(implementationSteps) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingImplementationSteps, Message: "Missing required execution section: implementation_steps", RepairEligible: true})
	}
	if len(codeRequirements) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingCodeRequirements, Message: "Missing required execution section: code_requirements", RepairEligible: true})
	}

	// Add targets from config (CR5)
	if cfgTargets, ok := runConfig["file_targets"].([]interface{}); ok {
		for _, t := range cfgTargets {
			if ts, ok := t.(string); ok {
				exists := false
				for _, ft := range fileTargets {
					if pathStr, ok := ft["path"].(string); ok && pathStr == ts {
						exists = true
						break
					}
				}
				if !exists {
					fileTargets = append(fileTargets, map[string]interface{}{
						"path":   ts,
						"role":   "primary",
						"action": "must_edit",
						"reason": "Targeted file from configuration.",
					})
				}
			}
		}
	}
	if len(fileTargets) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Missing required execution section: file_targets", RepairEligible: true})
	}

	// Parse validation expectations / commands
	if (!structuredParsed && !executionSpecProjected) || len(validationCmds) == 0 {
		valText, _ := getSection("validation_expectations", "validation expectations")
		if len(valText) == 0 {
			valText, _ = getSection("validation", "validation")
		}
		if strings.TrimSpace(valText) != "" {
			validationCmds = parseValidationCommands(valText)
		}
	}
	if len(validationCmds) == 0 {
		// Try load from config
		if cfgContract, ok := runConfig["validation_contract"].(map[string]interface{}); ok {
			if cfgCmds, ok := cfgContract["commands"].([]interface{}); ok {
				for _, c := range cfgCmds {
					if cmdMap, ok := c.(map[string]interface{}); ok {
						id, _ := cmdMap["id"].(string)
						if strings.TrimSpace(id) == "" {
							id = fmt.Sprintf("V%d", len(validationCmds)+1)
						}
						cmdStr, _ := cmdMap["command"].(string)
						reqVal, _ := cmdMap["required"].(bool)
						purp, _ := cmdMap["purpose"].(string)
						successSignal, _ := cmdMap["success_signal"].(string)
						if successSignal == "" {
							successSignal = "Command exits 0."
						}
						failureHandling, _ := cmdMap["failure_handling"].(string)
						if failureHandling == "" {
							if reqVal {
								failureHandling = "attempt_fix_once_then_block"
							} else {
								failureHandling = "report_if_fails"
							}
						}
						if cmdStr != "" {
							validationCmds = append(validationCmds, ValidationCommand{
								ID:              id,
								Command:         cmdStr,
								Required:        reqVal,
								Purpose:         purp,
								SuccessSignal:   successSignal,
								FailureHandling: failureHandling,
							})
						}
					}
				}
			}
		}
	}

	for i := range codeRequirements {
		apps, _ := codeRequirements[i]["applies_to"].([]string)
		var cleanApps []string
		for _, a := range apps {
			if a != "*" && a != "" {
				cleanApps = append(cleanApps, a)
			}
		}
		if len(cleanApps) == 0 {
			for _, ft := range fileTargets {
				if p, ok := ft["path"].(string); ok && p != "" {
					cleanApps = append(cleanApps, p)
				}
			}
		}
		if len(cleanApps) == 0 {
			cleanApps = []string{"unknown_path"}
		}
		codeRequirements[i]["applies_to"] = cleanApps
	}

	if len(expectedBehavior) == 0 {
		expectedBehavior = parseExpectedBehavior(expectedBehaviorRaw)
	}
	if completionContract == nil {
		completionContract = parseCompletionContract(completionContractRaw)
	}

	// Slices normalization before inserting to map
	if nonGoals == nil {
		nonGoals = []string{}
	}
	if fileTargets == nil {
		fileTargets = []map[string]interface{}{}
	}
	if implementationSteps == nil {
		implementationSteps = []map[string]interface{}{}
	}
	if codeRequirements == nil {
		codeRequirements = []map[string]interface{}{}
	}
	if validationCmds == nil {
		validationCmds = []ValidationCommand{}
	}
	if expectedBehavior == nil {
		expectedBehavior = []string{}
	}

	if len(validationCmds) == 0 {
		profile, _ := packetMeta["content_profile"].(string)
		if profile == "implementation_ready" {
			issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingValidationContract, Message: "Missing required execution section: validation_contract for executable profile", RepairEligible: false})
		}
	}

	validationContract := map[string]interface{}{
		"mode":           validationMode,
		"failure_policy": validationFailurePolicy,
		"commands":       validationCmds,
	}

	executionPayload := map[string]interface{}{
		"goal":                           goal,
		"scope":                          scope,
		"non_goals":                      nonGoals,
		"file_targets":                   fileTargets,
		"implementation_steps":           implementationSteps,
		"code_requirements":              codeRequirements,
		"validation_contract":            validationContract,
		"expected_behavior":              expectedBehavior,
		"completion_contract":            completionContract,
		"executor_final_response_format": "DONE_or_BLOCKED_strict_text",
	}
	for key, value := range executionPayloadMetadata {
		executionPayload[key] = value
	}

	// 4. Parse audit_seed (priorities, scope drift, risk checks)
	prioritiesText, _ := getSection("audit_priorities", "audit priorities")
	auditChecklist := parseAuditChecklist(prioritiesText)

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
		if pathStr, ok := ft["path"].(string); ok {
			fileScopeChecks = append(fileScopeChecks, fmt.Sprintf("Confirm that edits to %s only satisfy the goal.", pathStr))
		}
	}

	riskChecks := []string{"Confirm that no secret keys, credentials, or tokens were introduced."}
	for _, cs := range constraints {
		if stmt, ok := cs["statement"].(string); ok {
			riskChecks = append(riskChecks, fmt.Sprintf("Confirm constraint %q was respected.", stmt))
		}
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
	ID               string `json:"id"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Expected         string `json:"expected,omitempty"`
	Required         bool   `json:"required"`
	Purpose          string `json:"purpose"`
	SuccessSignal    string `json:"success_signal"`
	FailureHandling  string `json:"failure_handling"`
	Phase            string `json:"phase,omitempty"`
	Severity         string `json:"severity,omitempty"`
	MutationPolicy   string `json:"mutation_policy,omitempty"`
	WorktreePolicy   string `json:"worktree_policy,omitempty"`
}

type structuredCompilerInputResult struct {
	Goal                     string
	Scope                    string
	NonGoals                 []string
	FileTargets              []map[string]interface{}
	ImplementationSteps      []map[string]interface{}
	CodeRequirements         []map[string]interface{}
	ValidationCommands       []ValidationCommand
	ValidationMode           string
	ValidationFailurePolicy  string
	ExpectedBehavior         []string
	CompletionContract       map[string]interface{}
	SelectedPassID           string
	ExecutionPayloadMetadata map[string]interface{}
}

type structuredCompilerInputDocument struct {
	CompilerInput structuredCompilerInputYAML `yaml:"compiler_input"`
}

type structuredCompilerInputYAML struct {
	Goal                string                             `yaml:"goal"`
	Scope               string                             `yaml:"scope"`
	NonGoals            []string                           `yaml:"non_goals"`
	FileTargets         []structuredFileTargetYAML         `yaml:"file_targets"`
	ImplementationSteps []structuredImplementationStepYAML `yaml:"implementation_steps"`
	CodeRequirements    []structuredCodeRequirementYAML    `yaml:"code_requirements"`
	ValidationContract  structuredValidationContractYAML   `yaml:"validation_contract"`
	ExpectedBehavior    interface{}                        `yaml:"expected_behavior"`
	CompletionContract  structuredCompletionContractYAML   `yaml:"completion_contract"`
}

type structuredFileTargetYAML struct {
	Path      string `yaml:"path"`
	Role      string `yaml:"role"`
	Action    string `yaml:"action"`
	Reason    string `yaml:"reason"`
	Grounding string `yaml:"grounding"`
}

type structuredImplementationStepYAML struct {
	ID                 string   `yaml:"id"`
	Title              string   `yaml:"title"`
	Action             string   `yaml:"action"`
	TargetPaths        []string `yaml:"target_paths"`
	Instructions       string   `yaml:"instructions"`
	AcceptanceCriteria []string `yaml:"acceptance_criteria"`
}

type structuredCodeRequirementYAML struct {
	ID          string   `yaml:"id"`
	Requirement string   `yaml:"requirement"`
	AppliesTo   []string `yaml:"applies_to"`
}

type structuredValidationContractYAML struct {
	Mode          string                            `yaml:"mode"`
	FailurePolicy string                            `yaml:"failure_policy"`
	Commands      []structuredValidationCommandYAML `yaml:"commands"`
}

type structuredValidationCommandYAML struct {
	ID              string `yaml:"id"`
	Command         string `yaml:"command"`
	Required        *bool  `yaml:"required"`
	Purpose         string `yaml:"purpose"`
	SuccessSignal   string `yaml:"success_signal"`
	FailureHandling string `yaml:"failure_handling"`
}

type structuredCompletionContractYAML struct {
	DoneWhen            []string `yaml:"done_when"`
	BlockedWhen         []string `yaml:"blocked_when"`
	AllowedDiscretion   []string `yaml:"allowed_discretion"`
	ForbiddenDiscretion []string `yaml:"forbidden_discretion"`
}

type executionSpecCompatibilityDocument struct {
	ExecutionSpecID string `yaml:"execution_spec_id"`
	ProjectID       string `yaml:"project_id"`
	SourceAuthority struct {
		RepoTarget    string `yaml:"repo_target"`
		BranchContext string `yaml:"branch_context"`
	} `yaml:"source_authority"`
	SelectedPass struct {
		PassID       string   `yaml:"pass_id"`
		PassName     string   `yaml:"pass_name"`
		PassScope    string   `yaml:"pass_scope"`
		PassNonGoals []string `yaml:"pass_non_goals"`
	} `yaml:"selected_pass"`
	ExecutionPayload struct {
		Goal                        string                             `yaml:"goal"`
		Scope                       string                             `yaml:"scope"`
		NonGoals                    []string                           `yaml:"non_goals"`
		FileTargets                 []structuredFileTargetYAML         `yaml:"file_targets"`
		TargetSymbols               []executionSpecTargetSymbolYAML    `yaml:"target_symbols"`
		ImplementationSteps         []structuredImplementationStepYAML `yaml:"implementation_steps"`
		CodeRequirements            []structuredCodeRequirementYAML    `yaml:"code_requirements"`
		ExpectedBehavior            interface{}                        `yaml:"expected_behavior"`
		ValidationContract          executionSpecValidationYAML        `yaml:"validation_contract"`
		DeterministicOperations     []map[string]interface{}           `yaml:"deterministic_operations"`
		OperationGroups             []map[string]interface{}           `yaml:"operation_groups"`
		ChangedFilePolicy           interface{}                        `yaml:"changed_file_policy"`
		SourceGuards                interface{}                        `yaml:"source_guards"`
		ExecutionMode               map[string]interface{}             `yaml:"execution_mode"`
		ValidationCommands          []map[string]interface{}           `yaml:"validation_commands"`
		CompletionContract          structuredCompletionContractYAML   `yaml:"completion_contract"`
		ExecutorFinalResponseFormat string                             `yaml:"executor_final_response_format"`
	} `yaml:"execution_payload"`
	OpenQuestions []struct {
		ID       string `yaml:"id"`
		Question string `yaml:"question"`
		Blocking bool   `yaml:"blocking"`
	} `yaml:"open_questions"`
}

type executionSpecTargetSymbolYAML struct {
	Path       string `yaml:"path"`
	Symbol     string `yaml:"symbol"`
	SymbolType string `yaml:"symbol_type"`
	Action     string `yaml:"action"`
	Reason     string `yaml:"reason"`
}

type executionSpecValidationYAML struct {
	Mode          string                               `yaml:"mode"`
	FailurePolicy string                               `yaml:"failure_policy"`
	Commands      []executionSpecValidationCommandYAML `yaml:"commands"`
	ManualChecks  []executionSpecValidationCommandYAML `yaml:"manual_checks"`
}

type executionSpecValidationCommandYAML struct {
	ID               string `yaml:"id"`
	Command          string `yaml:"command"`
	CommandOrCheck   string `yaml:"command_or_check"`
	WorkingDirectory string `yaml:"working_directory"`
	Expected         string `yaml:"expected"`
	Required         *bool  `yaml:"required"`
	Purpose          string `yaml:"purpose"`
	SuccessSignal    string `yaml:"success_signal"`
	FailureHandling  string `yaml:"failure_handling"`
	Phase            string `yaml:"phase"`
	Severity         string `yaml:"severity"`
	MutationPolicy   string `yaml:"mutation_policy"`
	WorktreePolicy   string `yaml:"worktree_policy"`
}

func parseStructuredCompilerInput(text string) (*structuredCompilerInputResult, bool) {
	cleaned := stripCodeFences(text)
	if !strings.Contains(cleaned, "compiler_input:") {
		return nil, false
	}

	var doc structuredCompilerInputDocument
	if err := yaml.Unmarshal([]byte(cleaned), &doc); err != nil {
		return nil, false
	}

	input := doc.CompilerInput
	if strings.TrimSpace(input.Goal) == "" &&
		strings.TrimSpace(input.Scope) == "" &&
		len(input.FileTargets) == 0 &&
		len(input.ImplementationSteps) == 0 &&
		len(input.CodeRequirements) == 0 {
		return nil, false
	}

	result := &structuredCompilerInputResult{
		Goal:                    cleanParsedString(input.Goal),
		Scope:                   cleanParsedString(input.Scope),
		NonGoals:                cleanStringSlice(input.NonGoals),
		FileTargets:             mapStructuredFileTargets(input.FileTargets),
		ImplementationSteps:     mapStructuredImplementationSteps(input.ImplementationSteps),
		CodeRequirements:        mapStructuredCodeRequirements(input.CodeRequirements),
		ValidationCommands:      mapStructuredValidationCommands(input.ValidationContract.Commands),
		ValidationMode:          normalizeValidationMode(input.ValidationContract.Mode),
		ValidationFailurePolicy: normalizeValidationFailurePolicy(input.ValidationContract.FailurePolicy),
		ExpectedBehavior:        parseStructuredExpectedBehavior(input.ExpectedBehavior),
		CompletionContract:      mapStructuredCompletionContract(input.CompletionContract),
	}

	return result, true
}

// projectExecutionSpecCompatibility is a migration-only compatibility path for
// reviewed handoffs that embed a selected-pass Execution Spec instead of
// legacy structured compiler input.
func projectExecutionSpecCompatibility(content string) (*structuredCompilerInputResult, []validation.ValidationError, bool) {
	specText, ok := extractExecutionSpecBlock(content)
	if !ok {
		return nil, nil, false
	}
	var spec executionSpecCompatibilityDocument
	if err := yaml.Unmarshal([]byte(stripCodeFences(specText)), &spec); err != nil {
		return nil, []validation.ValidationError{{
			Type:           "structural",
			Code:           validation.CodeJSONSyntax,
			Message:        "Embedded Execution Spec could not be parsed",
			RepairEligible: true,
		}}, true
	}

	var issues []validation.ValidationError
	for _, q := range spec.OpenQuestions {
		if q.Blocking {
			msg := "Execution Spec has blocking open question"
			if q.ID != "" || q.Question != "" {
				msg = fmt.Sprintf("Execution Spec has blocking open question %s: %s", cleanParsedString(q.ID), cleanParsedString(q.Question))
			}
			issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: msg, RepairEligible: false})
		}
	}

	goal := cleanParsedString(spec.ExecutionPayload.Goal)
	scope := cleanParsedString(spec.ExecutionPayload.Scope)
	if scope == "" {
		scope = cleanParsedString(spec.SelectedPass.PassScope)
	}
	nonGoals := cleanStringSlice(spec.ExecutionPayload.NonGoals)
	if len(nonGoals) == 0 {
		nonGoals = cleanStringSlice(spec.SelectedPass.PassNonGoals)
	}
	fileTargets := mapStructuredFileTargets(spec.ExecutionPayload.FileTargets)
	implementationSteps := mapExecutionSpecImplementationSteps(spec.ExecutionPayload.ImplementationSteps, spec.ExecutionPayload.TargetSymbols)
	codeRequirements := mapExecutionSpecCodeRequirements(spec.ExecutionPayload.CodeRequirements)
	validationCmds := mapExecutionSpecValidationCommands(spec.ExecutionPayload.ValidationContract.Commands)
	if len(validationCmds) == 0 {
		validationCmds = mapExecutionSpecValidationCommands(spec.ExecutionPayload.ValidationContract.ManualChecks)
	}
	expectedBehavior := parseStructuredExpectedBehavior(spec.ExecutionPayload.ExpectedBehavior)
	completionContract := mapStructuredCompletionContract(spec.ExecutionPayload.CompletionContract)
	metadata := executionSpecPayloadMetadata(spec.ExecutionPayload)

	if goal == "" {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Execution Spec projection missing execution_payload.goal", RepairEligible: false})
	}
	if scope == "" {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Execution Spec projection missing execution_payload.scope", RepairEligible: false})
	}
	if len(fileTargets) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Execution Spec projection missing execution_payload.file_targets", RepairEligible: false})
	}
	if len(implementationSteps) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingImplementationSteps, Message: "Execution Spec projection missing execution_payload.implementation_steps", RepairEligible: false})
	}
	if len(codeRequirements) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingCodeRequirements, Message: "Execution Spec projection missing execution_payload.code_requirements", RepairEligible: false})
	}
	if len(validationCmds) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingValidationContract, Message: "Execution Spec projection missing execution_payload.validation_contract.commands", RepairEligible: false})
	}
	if len(expectedBehavior) == 0 {
		issues = append(issues, validation.ValidationError{Type: "structural", Code: validation.CodeMissingRequiredField, Message: "Execution Spec projection missing execution_payload.expected_behavior", RepairEligible: false})
	}

	return &structuredCompilerInputResult{
		Goal:                     goal,
		Scope:                    scope,
		NonGoals:                 nonGoals,
		FileTargets:              fileTargets,
		ImplementationSteps:      implementationSteps,
		CodeRequirements:         codeRequirements,
		ValidationCommands:       validationCmds,
		ValidationMode:           normalizeValidationMode(spec.ExecutionPayload.ValidationContract.Mode),
		ValidationFailurePolicy:  normalizeValidationFailurePolicy(spec.ExecutionPayload.ValidationContract.FailurePolicy),
		ExpectedBehavior:         expectedBehavior,
		CompletionContract:       completionContract,
		SelectedPassID:           cleanParsedString(spec.SelectedPass.PassID),
		ExecutionPayloadMetadata: metadata,
	}, issues, true
}

func executionSpecPayloadMetadata(payload struct {
	Goal                        string                             `yaml:"goal"`
	Scope                       string                             `yaml:"scope"`
	NonGoals                    []string                           `yaml:"non_goals"`
	FileTargets                 []structuredFileTargetYAML         `yaml:"file_targets"`
	TargetSymbols               []executionSpecTargetSymbolYAML    `yaml:"target_symbols"`
	ImplementationSteps         []structuredImplementationStepYAML `yaml:"implementation_steps"`
	CodeRequirements            []structuredCodeRequirementYAML    `yaml:"code_requirements"`
	ExpectedBehavior            interface{}                        `yaml:"expected_behavior"`
	ValidationContract          executionSpecValidationYAML        `yaml:"validation_contract"`
	DeterministicOperations     []map[string]interface{}           `yaml:"deterministic_operations"`
	OperationGroups             []map[string]interface{}           `yaml:"operation_groups"`
	ChangedFilePolicy           interface{}                        `yaml:"changed_file_policy"`
	SourceGuards                interface{}                        `yaml:"source_guards"`
	ExecutionMode               map[string]interface{}             `yaml:"execution_mode"`
	ValidationCommands          []map[string]interface{}           `yaml:"validation_commands"`
	CompletionContract          structuredCompletionContractYAML   `yaml:"completion_contract"`
	ExecutorFinalResponseFormat string                             `yaml:"executor_final_response_format"`
}) map[string]interface{} {
	metadata := make(map[string]interface{})
	if len(payload.DeterministicOperations) > 0 {
		metadata["deterministic_operations"] = payload.DeterministicOperations
	}
	if len(payload.OperationGroups) > 0 {
		metadata["operation_groups"] = payload.OperationGroups
	}
	if payload.ChangedFilePolicy != nil {
		metadata["changed_file_policy"] = payload.ChangedFilePolicy
	}
	if payload.SourceGuards != nil {
		metadata["source_guards"] = payload.SourceGuards
	}
	if len(payload.ExecutionMode) > 0 {
		metadata["execution_mode"] = payload.ExecutionMode
	}
	if len(payload.ValidationCommands) > 0 {
		metadata["validation_commands"] = payload.ValidationCommands
	}
	return metadata
}

func extractExecutionSpecBlock(content string) (string, bool) {
	if val, ok := extractXMLSection(content, "execution_spec"); ok {
		return val, true
	}
	if val, ok := extractMarkdownSection(content, "execution spec"); ok {
		return val, true
	}
	return "", false
}

func mapExecutionSpecImplementationSteps(steps []structuredImplementationStepYAML, symbols []executionSpecTargetSymbolYAML) []map[string]interface{} {
	result := mapStructuredImplementationSteps(steps)
	for i := range result {
		if i >= len(steps) {
			continue
		}
		originalID := strings.ToUpper(cleanParsedString(steps[i].ID))
		if originalID == "" {
			continue
		}
		instructions, _ := result[i]["instructions"].(string)
		if !strings.Contains(instructions, originalID) {
			result[i]["instructions"] = fmt.Sprintf("[%s] %s", originalID, instructions)
		}
	}
	symbolText := formatExecutionSpecTargetSymbols(symbols)
	if symbolText == "" {
		return result
	}
	for _, step := range result {
		instructions, _ := step["instructions"].(string)
		if strings.Contains(instructions, "Target symbols:") {
			continue
		}
		step["instructions"] = strings.TrimSpace(instructions + "\n\nTarget symbols: " + symbolText)
	}
	return result
}

func mapExecutionSpecCodeRequirements(reqs []structuredCodeRequirementYAML) []map[string]interface{} {
	var result []map[string]interface{}
	for i, req := range reqs {
		requirement := cleanParsedString(req.Requirement)
		if requirement == "" {
			continue
		}
		originalID := strings.ToUpper(cleanParsedString(req.ID))
		if originalID != "" && !strings.Contains(requirement, originalID) {
			requirement = fmt.Sprintf("[%s] %s", originalID, requirement)
		}
		appliesTo := cleanStringSlice(req.AppliesTo)
		if len(appliesTo) == 0 {
			appliesTo = []string{"*"}
		}
		result = append(result, map[string]interface{}{
			"id":          fmt.Sprintf("CR%d", i+1),
			"requirement": requirement,
			"applies_to":  appliesTo,
		})
	}
	return result
}

func mapExecutionSpecValidationCommands(commands []executionSpecValidationCommandYAML) []ValidationCommand {
	var result []ValidationCommand
	for i, command := range commands {
		cmd := cleanParsedString(command.Command)
		if cmd == "" {
			cmd = cleanParsedString(command.CommandOrCheck)
		}
		if cmd == "" {
			continue
		}
		required := true
		if command.Required != nil {
			required = *command.Required
		}
		originalID := strings.ToUpper(cleanParsedString(command.ID))
		purpose := defaultString(command.Purpose, "Validate implementation")
		if originalID != "" && !strings.Contains(purpose, originalID) {
			purpose = fmt.Sprintf("[%s] %s", originalID, purpose)
		}
		result = append(result, ValidationCommand{
			ID:               fmt.Sprintf("V%d", i+1),
			Command:          cmd,
			WorkingDirectory: cleanParsedString(command.WorkingDirectory),
			Expected:         cleanParsedString(command.Expected),
			Required:         required,
			Purpose:          purpose,
			SuccessSignal:    defaultString(command.SuccessSignal, "Command exits 0."),
			FailureHandling:  normalizeValidationFailureHandling(command.FailureHandling),
			Phase:            cleanParsedString(command.Phase),
			Severity:         cleanParsedString(command.Severity),
			MutationPolicy:   cleanParsedString(command.MutationPolicy),
			WorktreePolicy:   cleanParsedString(command.WorktreePolicy),
		})
	}
	return result
}

func formatExecutionSpecTargetSymbols(symbols []executionSpecTargetSymbolYAML) string {
	var parts []string
	for _, symbol := range symbols {
		path := cleanParsedString(symbol.Path)
		name := cleanParsedString(symbol.Symbol)
		if path == "" || name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s in %s", name, path))
	}
	return strings.Join(parts, "; ")
}

func mapStructuredFileTargets(targets []structuredFileTargetYAML) []map[string]interface{} {
	var result []map[string]interface{}
	for _, target := range targets {
		path := cleanParsedString(target.Path)
		if path == "" {
			continue
		}
		reason := cleanParsedString(target.Reason)
		if reason == "" {
			reason = cleanParsedString(target.Grounding)
		}
		if reason == "" {
			reason = "Targeted file for implementation."
		}
		result = append(result, map[string]interface{}{
			"path":   path,
			"role":   normalizeEnum(target.Role, []string{"primary", "supporting", "test", "docs", "config", "read_only", "generated_do_not_edit"}, "primary"),
			"action": normalizeEnum(target.Action, []string{"must_edit", "may_edit", "must_create", "may_create", "must_read", "do_not_edit", "generated_do_not_edit"}, "must_edit"),
			"reason": reason,
		})
	}
	return result
}

func mapStructuredImplementationSteps(steps []structuredImplementationStepYAML) []map[string]interface{} {
	var result []map[string]interface{}
	for i, step := range steps {
		instructions := cleanParsedString(step.Instructions)
		title := cleanParsedString(step.Title)
		if title == "" {
			title = deriveStepTitle(instructions, fmt.Sprintf("S%d", i+1))
		}
		if instructions == "" {
			instructions = title
		}
		criteria := cleanStringSlice(step.AcceptanceCriteria)
		if len(criteria) == 0 {
			criteria = []string{"Step verification passes."}
		}
		paths := cleanStringSlice(step.TargetPaths)
		if len(paths) == 0 {
			paths = []string{"unknown_path"}
		}
		result = append(result, map[string]interface{}{
			"id":                  normalizePatternID(step.ID, regexp.MustCompile(`^S[0-9]+$`), "S", i+1),
			"title":               title,
			"action":              normalizeEnum(step.Action, []string{"inspect", "modify", "create", "delete", "move", "rename", "test", "document", "configure", "verify"}, "modify"),
			"target_paths":        paths,
			"instructions":        instructions,
			"acceptance_criteria": criteria,
		})
	}
	return result
}

func mapStructuredCodeRequirements(reqs []structuredCodeRequirementYAML) []map[string]interface{} {
	var result []map[string]interface{}
	for i, req := range reqs {
		requirement := cleanParsedString(req.Requirement)
		if requirement == "" {
			continue
		}
		appliesTo := cleanStringSlice(req.AppliesTo)
		if len(appliesTo) == 0 {
			appliesTo = []string{"*"}
		}
		result = append(result, map[string]interface{}{
			"id":          normalizePatternID(req.ID, regexp.MustCompile(`^CR[0-9]+$`), "CR", i+1),
			"requirement": requirement,
			"applies_to":  appliesTo,
		})
	}
	return result
}

func mapStructuredValidationCommands(commands []structuredValidationCommandYAML) []ValidationCommand {
	var result []ValidationCommand
	for i, command := range commands {
		cmd := cleanParsedString(command.Command)
		if cmd == "" {
			continue
		}
		required := true
		if command.Required != nil {
			required = *command.Required
		}
		result = append(result, ValidationCommand{
			ID:              normalizePatternID(command.ID, regexp.MustCompile(`^V[0-9]+$`), "V", i+1),
			Command:         cmd,
			Required:        required,
			Purpose:         defaultString(command.Purpose, "Validate implementation"),
			SuccessSignal:   defaultString(command.SuccessSignal, "Command exits 0."),
			FailureHandling: normalizeValidationFailureHandling(command.FailureHandling),
		})
	}
	return result
}

func mapStructuredCompletionContract(contract structuredCompletionContractYAML) map[string]interface{} {
	doneWhen := cleanStringSlice(contract.DoneWhen)
	if len(doneWhen) == 0 {
		doneWhen = []string{"The implementation compiles, passes tests, and satisfies the user requirement."}
	}
	blockedWhen := cleanStringSlice(contract.BlockedWhen)
	if len(blockedWhen) == 0 {
		blockedWhen = []string{"Dependencies are missing or critical tests fail with no obvious fix."}
	}
	allowedDiscretion := cleanStringSlice(contract.AllowedDiscretion)
	if len(allowedDiscretion) == 0 {
		allowedDiscretion = []string{"Minor formatting and placement of helper functions."}
	}
	forbiddenDiscretion := cleanStringSlice(contract.ForbiddenDiscretion)
	if len(forbiddenDiscretion) == 0 {
		forbiddenDiscretion = []string{"Modifying logic outside of the specified file targets."}
	}
	return map[string]interface{}{
		"done_when":            doneWhen,
		"blocked_when":         blockedWhen,
		"allowed_discretion":   allowedDiscretion,
		"forbidden_discretion": forbiddenDiscretion,
	}
}

func parseStructuredExpectedBehavior(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, cleanParsedString(s))
			}
		}
		return cleanStringSlice(result)
	case []string:
		return cleanStringSlice(v)
	case string:
		return parseExpectedBehavior(v)
	default:
		return nil
	}
}

func cleanStringSlice(values []string) []string {
	var result []string
	for _, value := range values {
		cleaned := cleanParsedString(value)
		if cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func normalizePatternID(id string, pattern *regexp.Regexp, prefix string, idx int) string {
	id = strings.ToUpper(cleanParsedString(id))
	if pattern.MatchString(id) {
		return id
	}
	return fmt.Sprintf("%s%d", prefix, idx)
}

func deriveStepTitle(instructions, fallback string) string {
	for _, line := range strings.Split(instructions, "\n") {
		title := cleanParsedString(line)
		if title == "" {
			continue
		}
		if idx := strings.IndexAny(title, ".:;"); idx > 0 {
			title = strings.TrimSpace(title[:idx])
		}
		if len(title) > 96 {
			title = strings.TrimSpace(title[:96])
		}
		if title != "" {
			return title
		}
	}
	return fallback
}

func normalizeEnum(value string, allowed []string, fallback string) string {
	value = strings.ToLower(cleanParsedString(value))
	for _, allowedValue := range allowed {
		if value == allowedValue {
			return allowedValue
		}
	}
	return fallback
}

func normalizeValidationMode(value string) string {
	return normalizeEnum(value, []string{"commands", "manual", "external", "not_applicable", "deferred"}, "commands")
}

func normalizeValidationFailurePolicy(value string) string {
	return normalizeEnum(value, []string{"block", "warn", "report_only"}, "block")
}

func normalizeValidationFailureHandling(value string) string {
	return normalizeEnum(value, []string{"block_if_fails", "report_if_fails", "skip_if_command_unavailable", "attempt_fix_once_then_block"}, "attempt_fix_once_then_block")
}

func defaultString(value, fallback string) string {
	value = cleanParsedString(value)
	if value == "" {
		return fallback
	}
	return value
}

func parseValidationCommands(text string) []ValidationCommand {
	var cmds []ValidationCommand
	lines := strings.Split(text, "\n")
	var currentCmd *ValidationCommand
	valNum := 1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if (strings.HasPrefix(trimmed, "- V") || strings.HasPrefix(trimmed, "- v")) && strings.Contains(trimmed, ":") {
			if currentCmd != nil {
				cmds = append(cmds, *currentCmd)
			}
			currentCmd = &ValidationCommand{
				ID:              fmt.Sprintf("V%d", valNum),
				Required:        true,
				SuccessSignal:   "Command exits 0.",
				FailureHandling: "attempt_fix_once_then_block",
			}
			valNum++
			continue
		}

		if currentCmd == nil {
			if strings.Contains(trimmed, "command:") {
				currentCmd = &ValidationCommand{
					ID:              fmt.Sprintf("V%d", valNum),
					Required:        true,
					SuccessSignal:   "Command exits 0.",
					FailureHandling: "attempt_fix_once_then_block",
				}
				valNum++
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
			valLower := strings.ToLower(val)
			if strings.Contains(valLower, "block_if_fails") || strings.Contains(valLower, "block if fails") {
				currentCmd.FailureHandling = "block_if_fails"
			} else if strings.Contains(valLower, "report_if_fails") || strings.Contains(valLower, "report if fails") {
				currentCmd.FailureHandling = "report_if_fails"
			} else if strings.Contains(valLower, "skip_if_command_unavailable") || strings.Contains(valLower, "skip if command") {
				currentCmd.FailureHandling = "skip_if_command_unavailable"
			} else {
				currentCmd.FailureHandling = "attempt_fix_once_then_block"
			}
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
							ID:              fmt.Sprintf("V%d", valNum),
							Command:         cmdStr,
							Required:        true,
							Purpose:         "Validate implementation",
							SuccessSignal:   "Command exits 0.",
							FailureHandling: "attempt_fix_once_then_block",
						})
						valNum++
					}
				}
			}
		}
	}

	if len(cmds) == 0 {
		cmds = append(cmds, ValidationCommand{
			ID:              "V1",
			Command:         "go test ./...",
			Required:        true,
			Purpose:         "Run unit tests",
			SuccessSignal:   "Command exits 0.",
			FailureHandling: "attempt_fix_once_then_block",
		})
	}

	for i := range cmds {
		if cmds[i].ID == "" {
			cmds[i].ID = fmt.Sprintf("V%d", i+1)
		}
		if cmds[i].SuccessSignal == "" {
			cmds[i].SuccessSignal = "Command exits 0."
		}
		if cmds[i].FailureHandling == "" {
			cmds[i].FailureHandling = "attempt_fix_once_then_block"
		}
		if cmds[i].Purpose == "" {
			cmds[i].Purpose = "Validate implementation"
		}
	}

	return cmds
}

func slugify(s string) string {
	s = strings.ToLower(s)
	reg := regexp.MustCompile("[^a-z0-9]+")
	s = reg.ReplaceAllString(s, "-")
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

		if strings.HasSuffix(trimmed, ":") {
			name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(trimmed, ":")))
			if found {
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

func cleanParsedString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`+"`")
	return strings.TrimSpace(s)
}

func parseCreatedAt(s string) string {
	if s == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func parseContextSnapshot(text string) []string {
	var result []string
	paragraphs := strings.Split(text, "\n\n")
	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		result = []string{"No context snapshot provided."}
	}
	return result
}

func parseDecisionLog(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(D\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":        match[1],
				"summary":   strings.TrimSpace(match[2]),
				"rationale": "",
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "- rationale:") {
				current["rationale"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "- rationale:"))
			} else if strings.HasPrefix(lower, "rationale:") {
				current["rationale"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "rationale:"))
			} else if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
				val := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
				if current["rationale"] == "" {
					current["rationale"] = val
				}
			} else {
				if r, ok := current["rationale"].(string); ok && r != "" {
					current["rationale"] = r + " " + trimmed
				} else {
					current["rationale"] = trimmed
				}
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	if len(result) == 0 && strings.TrimSpace(text) != "" {
		result = append(result, map[string]interface{}{
			"id":        "D1",
			"summary":   strings.TrimSpace(text),
			"rationale": "As planned in the handoff.",
		})
	}
	for _, item := range result {
		item["summary"] = cleanParsedString(item["summary"].(string))
		item["rationale"] = cleanParsedString(item["rationale"].(string))
		if item["rationale"] == "" {
			item["rationale"] = "As planned in the handoff."
		}
	}
	return result
}

func parseConstraints(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(C\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":         match[1],
				"statement":  strings.TrimSpace(match[2]),
				"applies_to": []string{"executor"},
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var appsText string
			if strings.HasPrefix(lower, "- applies to:") {
				appsText = trimmed[len("- applies to:"):]
			} else if strings.HasPrefix(lower, "applies to:") {
				appsText = trimmed[len("applies to:"):]
			}
			if appsText != "" {
				appsText = strings.ToLower(appsText)
				var apps []string
				aliasMap := []struct {
					needle string
					value  string
				}{
					{needle: "planner_agent", value: "planner_agent"},
					{needle: "planner", value: "planner_agent"},
					{needle: "packet_compiler", value: "packet_compiler"},
					{needle: "compiler", value: "packet_compiler"},
					{needle: "packet_validator", value: "packet_validator"},
					{needle: "validator", value: "packet_validator"},
					{needle: "brief_renderer", value: "brief_renderer"},
					{needle: "renderer", value: "brief_renderer"},
					{needle: "executor", value: "executor"},
					{needle: "auditor_agent", value: "auditor_agent"},
					{needle: "auditor", value: "auditor_agent"},
					{needle: "relay_middleware", value: "relay_middleware"},
					{needle: "middleware", value: "relay_middleware"},
				}
				seen := map[string]bool{}
				for _, alias := range aliasMap {
					if strings.Contains(appsText, alias.needle) && !seen[alias.value] {
						apps = append(apps, alias.value)
						seen[alias.value] = true
					}
				}
				if len(apps) > 0 {
					current["applies_to"] = apps
				}
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	if len(result) == 0 && strings.TrimSpace(text) != "" {
		result = append(result, map[string]interface{}{
			"id":         "C1",
			"statement":  strings.TrimSpace(text),
			"applies_to": []string{"executor"},
		})
	}
	for _, item := range result {
		item["statement"] = cleanParsedString(item["statement"].(string))
	}
	return result
}

func parseAssumptions(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(AS\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":        match[1],
				"statement": strings.TrimSpace(match[2]),
				"if_false":  "block",
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var val string
			if strings.HasPrefix(lower, "- if false:") {
				val = trimmed[len("- if false:"):]
			} else if strings.HasPrefix(lower, "if false:") {
				val = trimmed[len("if false:"):]
			}
			if val != "" {
				val = strings.ToLower(strings.Trim(val, `"'`+"`"))
				if val == "block" || val == "revise_packet" || val == "continue_with_note" {
					current["if_false"] = val
				} else if strings.Contains(val, "block") {
					current["if_false"] = "block"
				} else if strings.Contains(val, "revise") {
					current["if_false"] = "revise_packet"
				} else if strings.Contains(val, "continue") {
					current["if_false"] = "continue_with_note"
				}
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	if len(result) == 0 && strings.TrimSpace(text) != "" {
		result = append(result, map[string]interface{}{
			"id":        "AS1",
			"statement": strings.TrimSpace(text),
			"if_false":  "block",
		})
	}
	for _, item := range result {
		item["statement"] = cleanParsedString(item["statement"].(string))
	}
	return result
}

func parseKnownRepoFacts(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(F\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":     match[1],
				"fact":   strings.TrimSpace(match[2]),
				"source": "unknown",
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var val string
			if strings.HasPrefix(lower, "- source:") {
				val = trimmed[len("- source:"):]
			} else if strings.HasPrefix(lower, "source:") {
				val = trimmed[len("source:"):]
			}
			if val != "" {
				val = strings.ToLower(strings.Trim(val, `"'`+"`"))
				if val == "user_provided" || val == "repo_inspection" || val == "prior_artifact" || val == "unknown" {
					current["source"] = val
				} else if strings.Contains(val, "user") {
					current["source"] = "user_provided"
				} else if strings.Contains(val, "inspection") || strings.Contains(val, "inspect") {
					current["source"] = "repo_inspection"
				} else if strings.Contains(val, "prior") || strings.Contains(val, "artifact") {
					current["source"] = "prior_artifact"
				}
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	if len(result) == 0 && strings.TrimSpace(text) != "" {
		result = append(result, map[string]interface{}{
			"id":     "F1",
			"fact":   strings.TrimSpace(text),
			"source": "unknown",
		})
	}
	for _, item := range result {
		item["fact"] = cleanParsedString(item["fact"].(string))
	}
	return result
}

func parseUnresolvedQuestions(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(Q\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":       match[1],
				"question": strings.TrimSpace(match[2]),
				"blocking": false,
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var val string
			if strings.HasPrefix(lower, "- blocking:") {
				val = trimmed[len("- blocking:"):]
			} else if strings.HasPrefix(lower, "blocking:") {
				val = trimmed[len("blocking:"):]
			}
			if val != "" {
				val = strings.ToLower(strings.Trim(val, `"'`+"`"))
				current["blocking"] = (val == "true" || val == "yes" || val == "1")
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	for _, item := range result {
		item["question"] = cleanParsedString(item["question"].(string))
	}
	return result
}

func parseRejectedAlternatives(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(RA\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":              match[1],
				"alternative":     strings.TrimSpace(match[2]),
				"reason_rejected": "",
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var val string
			if strings.HasPrefix(lower, "- reason rejected:") {
				val = trimmed[len("- reason rejected:"):]
			} else if strings.HasPrefix(lower, "reason rejected:") {
				val = trimmed[len("reason rejected:"):]
			} else if strings.HasPrefix(lower, "- reason:") {
				val = trimmed[len("- reason:"):]
			} else if strings.HasPrefix(lower, "reason:") {
				val = trimmed[len("reason:"):]
			}
			if val != "" {
				current["reason_rejected"] = val
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	for _, item := range result {
		item["alternative"] = cleanParsedString(item["alternative"].(string))
		item["reason_rejected"] = cleanParsedString(item["reason_rejected"].(string))
		if item["reason_rejected"] == "" {
			item["reason_rejected"] = "Not preferred for this pass."
		}
	}
	return result
}

func parseRiskRegister(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	re := regexp.MustCompile(`^\s*-\s*(R\d+)\s*:\s*(.*)$`)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":          match[1],
				"description": strings.TrimSpace(match[2]),
				"severity":    "low",
				"mitigation":  "Monitor behavior closely.",
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var sevVal string
			var mitVal string
			if strings.HasPrefix(lower, "- severity:") {
				sevVal = trimmed[len("- severity:"):]
			} else if strings.HasPrefix(lower, "severity:") {
				sevVal = trimmed[len("severity:"):]
			} else if strings.HasPrefix(lower, "- mitigation:") {
				mitVal = trimmed[len("- mitigation:"):]
			} else if strings.HasPrefix(lower, "mitigation:") {
				mitVal = trimmed[len("mitigation:"):]
			}

			if sevVal != "" {
				sevVal = strings.ToLower(cleanParsedString(sevVal))
				if sevVal == "low" || sevVal == "medium" || sevVal == "high" || sevVal == "critical" {
					current["severity"] = sevVal
				}
			}
			if mitVal != "" {
				current["mitigation"] = mitVal
			}
		}
	}
	if current != nil {
		result = append(result, current)
	}
	for _, item := range result {
		item["description"] = cleanParsedString(item["description"].(string))
		item["mitigation"] = cleanParsedString(item["mitigation"].(string))
	}
	return result
}

func deriveRisksFromAudit(prioritiesText string, constraints []map[string]interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	items := parseMarkdownList(prioritiesText)
	riskNum := 1
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), "risk") || strings.Contains(strings.ToLower(item), "flicker") || strings.Contains(strings.ToLower(item), "leak") {
			result = append(result, map[string]interface{}{
				"id":          fmt.Sprintf("R%d", riskNum),
				"description": item,
				"severity":    "medium",
				"mitigation":  "Perform verification checks during audit stage.",
			})
			riskNum++
		}
	}
	for _, c := range constraints {
		if stmt, ok := c["statement"].(string); ok {
			result = append(result, map[string]interface{}{
				"id":          fmt.Sprintf("R%d", riskNum),
				"description": fmt.Sprintf("Constraint violation risk for: %s", stmt),
				"severity":    "medium",
				"mitigation":  "Ensure code changes are precisely bounded.",
			})
			riskNum++
		}
	}
	return result
}

func parsePassBoundary(text string) map[string]interface{} {
	text = stripCodeFences(text)
	boundary := parseYAMLBlock(text)

	currentPass := 1
	if val, ok := boundary["current_pass"]; ok {
		currentPass = coerceToInt(val)
	}
	totalPlannedPasses := 1
	if val, ok := boundary["total_planned_passes"]; ok {
		totalPlannedPasses = coerceToInt(val)
	}

	var outOfScope []string
	if val, ok := boundary["out_of_scope_for_this_pass"]; ok {
		switch v := val.(type) {
		case []string:
			outOfScope = v
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					outOfScope = append(outOfScope, s)
				}
			}
		case string:
			if strings.TrimSpace(v) != "" {
				outOfScope = append(outOfScope, v)
			}
		}
	}
	if len(outOfScope) == 0 {
		outOfScope = []string{"Redesigning the page layout."}
	}

	thisPassScope := "Implementation of planned changes."
	if val, ok := boundary["this_pass_scope"].(string); ok && val != "" {
		thisPassScope = val
	}

	dependsOnPacketID := ""
	if val, ok := boundary["depends_on_packet_id"].(string); ok {
		dependsOnPacketID = val
	}

	return map[string]interface{}{
		"current_pass":               currentPass,
		"total_planned_passes":       totalPlannedPasses,
		"this_pass_scope":            thisPassScope,
		"out_of_scope_for_this_pass": outOfScope,
		"depends_on_packet_id":       dependsOnPacketID,
	}
}

func stripCodeFences(text string) string {
	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		cleanLines = append(cleanLines, line)
	}
	return strings.Join(cleanLines, "\n")
}

func coerceToInt(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 1
}

func parseFileTargetsStructured(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	pathRe := regexp.MustCompile("`([^`]+)`")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		var pathFound string
		if matches := pathRe.FindAllStringSubmatch(trimmed, -1); len(matches) > 0 {
			pathFound = strings.TrimSpace(matches[0][1])
		} else if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
			parts := strings.Fields(strings.TrimLeft(trimmed, "-*"))
			if len(parts) > 0 {
				p := parts[0]
				if p != "" && !strings.Contains(p, " ") && (strings.Contains(p, "/") || strings.Contains(p, ".")) {
					pathFound = p
				}
			}
		}

		if pathFound != "" && !strings.Contains(pathFound, " ") {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"path":   pathFound,
				"role":   "primary",
				"action": "must_edit",
				"reason": "Targeted file for implementation.",
			}
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var roleVal string
			var actVal string
			var reasonVal string

			if strings.HasPrefix(lower, "- role:") {
				roleVal = strings.TrimSpace(strings.TrimPrefix(trimmed, "- role:"))
			} else if strings.HasPrefix(lower, "role:") {
				roleVal = strings.TrimSpace(strings.TrimPrefix(trimmed, "role:"))
			} else if strings.HasPrefix(lower, "- action:") {
				actVal = strings.TrimSpace(strings.TrimPrefix(trimmed, "- action:"))
			} else if strings.HasPrefix(lower, "action:") {
				actVal = strings.TrimSpace(strings.TrimPrefix(trimmed, "action:"))
			} else if strings.HasPrefix(lower, "- reason:") {
				reasonVal = strings.TrimSpace(strings.TrimPrefix(trimmed, "- reason:"))
			} else if strings.HasPrefix(lower, "reason:") {
				reasonVal = strings.TrimSpace(strings.TrimPrefix(trimmed, "reason:"))
			}

			if roleVal != "" {
				roleVal = strings.ToLower(strings.Trim(roleVal, `"'`+"`"))
				allowedRoles := []string{"primary", "supporting", "test", "docs", "config", "read_only", "generated_do_not_edit"}
				for _, r := range allowedRoles {
					if roleVal == r || strings.Contains(roleVal, r) {
						current["role"] = r
						break
					}
				}
			}
			if actVal != "" {
				actVal = strings.ToLower(strings.Trim(actVal, `"'`+"`"))
				allowedActions := []string{"must_edit", "may_edit", "must_create", "may_create", "must_read", "do_not_edit", "generated_do_not_edit"}
				for _, a := range allowedActions {
					if actVal == a || strings.Contains(actVal, a) {
						current["action"] = a
						break
					}
				}
			}
			if reasonVal != "" {
				current["reason"] = cleanParsedString(reasonVal)
			}
		}
	}

	if current != nil {
		result = append(result, current)
	}

	if len(result) == 0 {
		strPaths := parseFileTargets(text)
		for _, sp := range strPaths {
			result = append(result, map[string]interface{}{
				"path":   sp,
				"role":   "primary",
				"action": "must_edit",
				"reason": "Targeted file for implementation.",
			})
		}
	}

	for _, item := range result {
		item["path"] = cleanParsedString(item["path"].(string))
		item["reason"] = cleanParsedString(item["reason"].(string))
	}

	return result
}

func parseImplementationSteps(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	stepNum := 1

	stepHeaderRe := regexp.MustCompile(`^\s*(\d+)\.\s*(.*)$`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if match := stepHeaderRe.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":                  fmt.Sprintf("S%d", stepNum),
				"title":               strings.TrimSpace(match[2]),
				"action":              "modify",
				"target_paths":        []string{},
				"instructions":        "",
				"acceptance_criteria": []string{},
			}
			stepNum++
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "- action:") {
				act := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "- action:")))
				act = strings.Trim(act, `"'`+"`")
				allowedActions := []string{"inspect", "modify", "create", "delete", "move", "rename", "test", "document", "configure", "verify"}
				for _, a := range allowedActions {
					if act == a || strings.Contains(act, a) {
						current["action"] = a
						break
					}
				}
			} else if strings.HasPrefix(lower, "- target_path") {
				colonIdx := strings.Index(trimmed, ":")
				if colonIdx > 0 {
					val := strings.TrimSpace(trimmed[colonIdx+1:])
					val = strings.Trim(val, `"'`+"`")
					if val != "" {
						if paths, ok := current["target_paths"].([]string); ok {
							current["target_paths"] = append(paths, val)
						}
					}
				}
			} else if strings.HasPrefix(trimmed, "-") && strings.Contains(trimmed, "/") && current != nil {
				if paths, ok := current["target_paths"].([]string); ok && len(paths) == 0 {
					val := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
					val = strings.Trim(val, `"'`+"`")
					if !strings.Contains(val, " ") && (strings.Contains(val, "/") || strings.Contains(val, ".")) {
						current["target_paths"] = append(paths, val)
					}
				}
			} else if strings.HasPrefix(lower, "- instructions:") {
				current["instructions"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "- instructions:"))
			} else if strings.HasPrefix(lower, "- acceptance_criteria:") || strings.HasPrefix(lower, "- acceptance criteria:") {
				val := strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					if acs, ok := current["acceptance_criteria"].([]string); ok {
						current["acceptance_criteria"] = append(acs, val)
					}
				}
			} else if strings.HasPrefix(trimmed, "-") && current != nil {
				val := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					if acs, ok := current["acceptance_criteria"].([]string); ok {
						lowVal := strings.ToLower(val)
						if !strings.HasPrefix(lowVal, "role:") && !strings.HasPrefix(lowVal, "action:") &&
							!strings.HasPrefix(lowVal, "target_path") && !strings.HasPrefix(lowVal, "instructions:") {
							current["acceptance_criteria"] = append(acs, val)
						}
					}
				}
			} else {
				if inst, ok := current["instructions"].(string); ok && inst != "" {
					current["instructions"] = inst + " " + trimmed
				} else {
					current["instructions"] = trimmed
				}
			}
		}
	}

	if current != nil {
		result = append(result, current)
	}

	if len(result) == 0 && strings.TrimSpace(text) != "" {
		rawSteps := parseMarkdownList(text)
		for i, rs := range rawSteps {
			result = append(result, map[string]interface{}{
				"id":                  fmt.Sprintf("S%d", i+1),
				"title":               rs,
				"action":              "modify",
				"target_paths":        []string{"unknown_path"},
				"instructions":        rs,
				"acceptance_criteria": []string{"Verify " + rs},
			})
		}
	}

	for _, item := range result {
		item["title"] = cleanParsedString(item["title"].(string))
		item["instructions"] = cleanParsedString(item["instructions"].(string))
		if item["instructions"] == "" {
			item["instructions"] = item["title"]
		}
		paths, _ := item["target_paths"].([]string)
		if len(paths) == 0 {
			item["target_paths"] = []string{"unknown_path"}
		}
		acs, _ := item["acceptance_criteria"].([]string)
		if len(acs) == 0 {
			item["acceptance_criteria"] = []string{"Step verification passes."}
		}
	}

	return result
}

func parseCodeRequirements(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	crNum := 1

	re := regexp.MustCompile(`^\s*-\s*(CR\d+)\s*:\s*(.*)$`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":          match[1],
				"requirement": strings.TrimSpace(match[2]),
				"applies_to":  []string{},
			}
			crNum++
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "- applies to:") {
				val := strings.TrimSpace(trimmed[len("- applies to:"):])
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					if apps, ok := current["applies_to"].([]string); ok {
						current["applies_to"] = append(apps, val)
					}
				}
			} else if strings.HasPrefix(trimmed, "-") && current != nil {
				val := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
				val = strings.Trim(val, `"'`+"`")
				if val != "" && !strings.Contains(val, " ") && (strings.Contains(val, "/") || strings.Contains(val, ".")) {
					if apps, ok := current["applies_to"].([]string); ok {
						current["applies_to"] = append(apps, val)
					}
				}
			}
		}
	}

	if current != nil {
		result = append(result, current)
	}

	if len(result) == 0 && strings.TrimSpace(text) != "" {
		rawReqs := parseMarkdownList(text)
		for i, rr := range rawReqs {
			result = append(result, map[string]interface{}{
				"id":          fmt.Sprintf("CR%d", i+1),
				"requirement": rr,
				"applies_to":  []string{"*"},
			})
		}
	}

	for _, item := range result {
		item["requirement"] = cleanParsedString(item["requirement"].(string))
		apps, _ := item["applies_to"].([]string)
		if len(apps) == 0 {
			item["applies_to"] = []string{"*"}
		}
	}

	return result
}

func parseExpectedBehavior(text string) []string {
	items := parseMarkdownList(text)
	if len(items) == 0 {
		return []string{"Implementation behavior matches the description."}
	}
	return items
}

func parseCompletionContract(text string) map[string]interface{} {
	doneWhen := []string{}
	blockedWhen := []string{}
	allowedDiscretion := []string{}
	forbiddenDiscretion := []string{}

	lines := strings.Split(text, "\n")
	var currentList *[]string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "done when") {
			currentList = &doneWhen
			idx := strings.Index(lower, "done when")
			if colonIdx := strings.Index(trimmed[idx:], ":"); colonIdx > 0 {
				val := strings.TrimSpace(trimmed[idx+colonIdx+1:])
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					*currentList = append(*currentList, val)
				}
			}
			continue
		} else if strings.Contains(lower, "blocked when") {
			currentList = &blockedWhen
			idx := strings.Index(lower, "blocked when")
			if colonIdx := strings.Index(trimmed[idx:], ":"); colonIdx > 0 {
				val := strings.TrimSpace(trimmed[idx+colonIdx+1:])
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					*currentList = append(*currentList, val)
				}
			}
			continue
		} else if strings.Contains(lower, "allowed discretion") {
			currentList = &allowedDiscretion
			idx := strings.Index(lower, "allowed discretion")
			if colonIdx := strings.Index(trimmed[idx:], ":"); colonIdx > 0 {
				val := strings.TrimSpace(trimmed[idx+colonIdx+1:])
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					*currentList = append(*currentList, val)
				}
			}
			continue
		} else if strings.Contains(lower, "forbidden discretion") {
			currentList = &forbiddenDiscretion
			idx := strings.Index(lower, "forbidden discretion")
			if colonIdx := strings.Index(trimmed[idx:], ":"); colonIdx > 0 {
				val := strings.TrimSpace(trimmed[idx+colonIdx+1:])
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					*currentList = append(*currentList, val)
				}
			}
			continue
		}

		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
			if currentList != nil {
				val := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
				val = strings.Trim(val, `"'`+"`")
				if val != "" {
					*currentList = append(*currentList, val)
				}
			}
		}
	}

	if len(doneWhen) == 0 {
		doneWhen = []string{"The implementation compiles, passes tests, and satisfies the user requirement."}
	}
	if len(blockedWhen) == 0 {
		blockedWhen = []string{"Dependencies are missing or critical tests fail with no obvious fix."}
	}
	if len(allowedDiscretion) == 0 {
		allowedDiscretion = []string{"Minor formatting and placement of helper functions."}
	}
	if len(forbiddenDiscretion) == 0 {
		forbiddenDiscretion = []string{"Modifying logic outside of the specified file targets."}
	}

	return map[string]interface{}{
		"done_when":            doneWhen,
		"blocked_when":         blockedWhen,
		"allowed_discretion":   allowedDiscretion,
		"forbidden_discretion": forbiddenDiscretion,
	}
}

func parseAuditChecklist(text string) []map[string]interface{} {
	var result []map[string]interface{}
	lines := strings.Split(text, "\n")
	var current map[string]interface{}
	acNum := 1

	re := regexp.MustCompile(`^\s*-\s*(A\d+)\s*:\s*(.*)$`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if match := re.FindStringSubmatch(line); match != nil {
			if current != nil {
				result = append(result, current)
			}
			current = map[string]interface{}{
				"id":                 match[1],
				"check":              strings.TrimSpace(match[2]),
				"severity_if_failed": "warning",
			}
			acNum++
		} else if current != nil {
			lower := strings.ToLower(trimmed)
			var val string
			if strings.HasPrefix(lower, "- severity_if_failed:") {
				val = strings.TrimSpace(strings.TrimPrefix(trimmed, "- severity_if_failed:"))
			} else if strings.HasPrefix(lower, "severity_if_failed:") {
				val = strings.TrimSpace(strings.TrimPrefix(trimmed, "severity_if_failed:"))
			}
			if val != "" {
				val = strings.ToLower(strings.Trim(val, `"'`+"`"))
				if val == "info" || val == "warning" || val == "error" || val == "blocker" {
					current["severity_if_failed"] = val
				}
			}
		}
	}

	if current != nil {
		result = append(result, current)
	}

	if len(result) == 0 && strings.TrimSpace(text) != "" {
		rawChecks := parseMarkdownList(text)
		for i, rc := range rawChecks {
			result = append(result, map[string]interface{}{
				"id":                 fmt.Sprintf("A%d", i+1),
				"check":              rc,
				"severity_if_failed": "warning",
			})
		}
	}

	if len(result) == 0 {
		result = append(result, map[string]interface{}{
			"id":                 "A1",
			"check":              "Confirm changes are exactly scoped to file targets.",
			"severity_if_failed": "warning",
		})
	}

	for _, item := range result {
		item["check"] = cleanParsedString(item["check"].(string))
	}

	return result
}

func extractSubsection(content, name string) string {
	lines := strings.Split(content, "\n")
	var sectionLines []string
	found := false
	normName := strings.ToLower(strings.TrimSpace(name))

	isMatch := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimLeft(trimmed, "#")
		trimmed = strings.TrimSpace(trimmed)
		if strings.HasPrefix(trimmed, "-") {
			trimmed = strings.TrimPrefix(trimmed, "-")
		} else if strings.HasPrefix(trimmed, "*") {
			trimmed = strings.TrimPrefix(trimmed, "*")
		}
		trimmed = strings.TrimSpace(trimmed)
		trimmed = strings.TrimSuffix(trimmed, ":")
		trimmed = strings.TrimSpace(trimmed)

		trimmedLower := strings.ToLower(trimmed)
		return trimmedLower == normName
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if found {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				isHeader := false
				if strings.HasPrefix(trimmed, "#") {
					isHeader = true
				} else {
					keywords := []string{
						"goal", "goals", "scope", "scopes", "non-goal", "non-goals",
						"file target", "file targets", "likely file target", "likely file targets",
						"implementation step", "implementation steps", "required implementation step", "required implementation steps",
						"code requirement", "code requirements", "expected behavior", "expected behaviors",
						"completion requirement", "completion requirements", "completion contract", "completion contracts",
						"rejected alternative", "rejected alternatives", "risk register", "risk registers",
						"unresolved question", "unresolved questions", "priority", "priorities", "checklist", "checklists",
						"validation expectation", "validation expectations",
					}
					for _, kw := range keywords {
						cleanLine := strings.TrimLeft(trimmed, "-* ")
						cleanLine = strings.TrimSuffix(cleanLine, ":")
						cleanLine = strings.TrimSpace(cleanLine)
						if strings.ToLower(cleanLine) == kw {
							isHeader = true
							break
						}
					}
				}
				if isHeader {
					break
				}
			}
			sectionLines = append(sectionLines, line)
		} else {
			if isMatch(line) {
				found = true
			}
		}
	}
	return strings.TrimSpace(strings.Join(sectionLines, "\n"))
}
