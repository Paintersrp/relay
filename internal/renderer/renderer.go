package renderer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"relay/internal/app/plans"
	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/validation"
)

type Renderer struct {
	store *store.Store
}

func New(s *store.Store) *Renderer {
	return &Renderer{store: s}
}

type RenderResult struct {
	Success bool     `json:"success"`
	RunID   int64    `json:"run_id"`
	Issues  []string `json:"issues"`
}

type BriefValidationReport struct {
	SchemaVersion string                 `json:"schema_version"`
	RunID         int64                  `json:"run_id"`
	ArtifactName  string                 `json:"artifact_name"`
	Status        string                 `json:"status"` // "passed" or "failed"
	Issues        []BriefValidationIssue `json:"issues"`
	CreatedAt     string                 `json:"created_at"`
}

type BriefValidationIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // "error", "warning"
	Message  string `json:"message"`
}

func (r *Renderer) RenderExecutorBrief(ctx context.Context, runID int64) (*RenderResult, error) {
	// 1. Load run
	run, err := r.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("failed to load run %d: %w", runID, err)
	}
	if run == nil {
		return nil, fmt.Errorf("run %d not found", runID)
	}

	// 2. Enforce state (CR4)
	if run.Status != "packet_validated" && run.Status != "repair_validated" {
		issues := []string{fmt.Sprintf("invalid run status %q: must be packet_validated or repair_validated", run.Status)}
		_ = r.writeReport(runID, false, issues)
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  issues,
		}, nil
	}

	// 3. Load canonical_packet.json
	packetBytes, err := artifacts.Read(runID, "canonical_packet", "canonical_packet.json")
	if err != nil {
		issues := []string{fmt.Sprintf("failed to read canonical_packet.json: %v", err)}
		_ = r.writeReport(runID, false, issues)
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  issues,
		}, nil
	}

	// 4. Parse canonical_packet
	var packet map[string]interface{}
	if err := json.Unmarshal(packetBytes, &packet); err != nil {
		issues := []string{fmt.Sprintf("malformed canonical_packet.json: %v", err)}
		_ = r.writeReport(runID, false, issues)
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  issues,
		}, nil
	}

	// 5. Parse/validate required renderer inputs
	issues := validateRequiredRendererInputs(packet)
	if len(issues) > 0 {
		_ = r.writeReport(runID, false, issues)
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  issues,
		}, nil
	}

	// 6. Render the template
	templatePath := locateTemplateFile("handoffs/templates/executor_brief_template.md")
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read executor brief template from %s: %w", templatePath, err)
	}

	renderedBrief, err := renderTemplate(string(templateBytes), packet)
	if err != nil {
		renderIssues := []string{fmt.Sprintf("template render error: %v", err)}
		_ = r.writeReport(runID, false, renderIssues)
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  renderIssues,
		}, nil
	}

	// 7. Validate the rendered brief
	briefIssues := validateRenderedBrief(renderedBrief)
	if len(briefIssues) > 0 {
		_ = r.writeReport(runID, false, briefIssues)
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  briefIssues,
		}, nil
	}

	// 8. Write executor_brief.md only after successful render/validation (CR5)
	briefPath, err := artifacts.Write(runID, "executor_brief", "executor_brief.md", []byte(renderedBrief))
	if err != nil {
		return nil, fmt.Errorf("failed to write executor_brief.md: %w", err)
	}

	_ = r.store.DeleteArtifactsByRunKind(runID, "executor_brief")
	if _, err := r.store.CreateArtifact(runID, "executor_brief", briefPath, "text/markdown"); err != nil {
		return nil, fmt.Errorf("failed to register executor_brief artifact: %w", err)
	}

	// 9. Write brief_validation_report.json for successful outcome (CR6)
	if err := r.writeReport(runID, true, nil); err != nil {
		return nil, fmt.Errorf("failed to write brief validation report: %w", err)
	}

	// 10. Advance lifecycle/state to brief_ready_for_review only after success (CR7)
	updatedRun, err := r.store.UpdateRunStatus(runID, "brief_ready_for_review")
	if err != nil {
		return nil, fmt.Errorf("failed to update run status to brief_ready_for_review: %w", err)
	}
	if err := plans.NewRunLifecycleService(r.store).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		return nil, fmt.Errorf("sync associated pass status: %w", err)
	}

	// Create event and check
	_ = r.store.DeleteChecksByRunKind(runID, "brief_validation")
	_, _ = r.store.CreateCheck(runID, "brief_validation", "pass", "Brief validation passed", "{}")
	_, _ = r.store.CreateEvent(runID, "info", "Executor brief rendered and validated successfully")

	return &RenderResult{
		Success: true,
		RunID:   runID,
	}, nil
}

func (r *Renderer) ApproveExecutorBrief(ctx context.Context, runID int64) (*RenderResult, error) {
	// 1. Load run
	run, err := r.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("failed to load run %d: %w", runID, err)
	}
	if run == nil {
		return nil, fmt.Errorf("run %d not found", runID)
	}

	// 2. Require status brief_ready_for_review
	if run.Status != "brief_ready_for_review" {
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  []string{fmt.Sprintf("run status is %q, but must be brief_ready_for_review to approve", run.Status)},
		}, nil
	}

	// 3. Check for valid brief artifacts
	if !artifacts.Exists(runID, "executor_brief", "executor_brief.md") {
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  []string{"executor_brief.md artifact is missing"},
		}, nil
	}

	reportBytes, err := artifacts.Read(runID, "brief_validation_report", "brief_validation_report.json")
	if err != nil {
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  []string{"brief_validation_report.json artifact is missing"},
		}, nil
	}

	var report BriefValidationReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  []string{fmt.Sprintf("malformed brief_validation_report.json: %v", err)},
		}, nil
	}

	if report.Status != "passed" {
		return &RenderResult{
			Success: false,
			RunID:   runID,
			Issues:  []string{"cannot approve: brief validation status is not passed"},
		}, nil
	}

	// 4. Record approval in run_config.json
	configMap := make(map[string]interface{})
	if data, err := artifacts.Read(runID, "run_config", "run_config.json"); err == nil {
		_ = json.Unmarshal(data, &configMap)
	}
	configMap["brief_approved_at"] = time.Now().UTC().Format(time.RFC3339)
	configMap["brief_approval_decision"] = "approved"

	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	_ = r.store.DeleteArtifactsByRunKind(runID, "run_config")
	if path, err := artifacts.Write(runID, "run_config", "run_config.json", configJSON); err == nil {
		_, _ = r.store.CreateArtifact(runID, "run_config", path, "application/json")
	}

	// 5. Advance lifecycle to approved_for_executor (CR9)
	updatedRun, err := r.store.UpdateRunStatus(runID, "approved_for_executor")
	if err != nil {
		return nil, fmt.Errorf("failed to update run status to approved_for_executor: %w", err)
	}
	if err := plans.NewRunLifecycleService(r.store).SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		return nil, fmt.Errorf("sync associated pass status: %w", err)
	}

	_, _ = r.store.CreateEvent(runID, "status_change", "Executor brief approved")

	return &RenderResult{
		Success: true,
		RunID:   runID,
	}, nil
}

func (r *Renderer) writeReport(runID int64, valid bool, issues []string) error {
	status := "passed"
	if !valid {
		status = "failed"
	}

	reportIssues := make([]BriefValidationIssue, 0)
	for _, issue := range issues {
		code := "BRIEF_VALIDATION_FAILED"
		switch {
		case strings.Contains(issue, "invalid run status"):
			code = "BRIEF_PACKET_INVALID"
		case strings.Contains(issue, "missing required section"):
			code = "BRIEF_REQUIRED_SECTION_MISSING"
		case strings.Contains(issue, "empty or unrendered"):
			code = "BRIEF_EXECUTION_PAYLOAD_MISSING"
		case strings.Contains(issue, "forbidden context marker"):
			code = "BRIEF_FORBIDDEN_CONTEXT_RENDERED"
		case strings.Contains(issue, "sensitive data"):
			code = "SENSITIVE_DATA_DETECTED"
		case strings.Contains(issue, "is missing"), strings.Contains(issue, "is empty"), strings.Contains(issue, "invalid"):
			if strings.Contains(issue, "packet_meta") || strings.Contains(issue, "execution_payload") || strings.Contains(issue, "validation_contract") {
				code = "RENDERER_REQUIRED_FIELD_MISSING"
			}
		}
		reportIssues = append(reportIssues, BriefValidationIssue{
			Code:     code,
			Severity: "error",
			Message:  issue,
		})
	}

	report := BriefValidationReport{
		SchemaVersion: "1.0.0",
		RunID:         runID,
		ArtifactName:  "executor_brief.md",
		Status:        status,
		Issues:        reportIssues,
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	reportPath, err := artifacts.Write(runID, "brief_validation_report", "brief_validation_report.json", reportBytes)
	if err != nil {
		return err
	}

	_ = r.store.DeleteArtifactsByRunKind(runID, "brief_validation_report")
	_, err = r.store.CreateArtifact(runID, "brief_validation_report", reportPath, "application/json")
	return err
}

func validateRequiredRendererInputs(packet map[string]interface{}) []string {
	var issues []string

	meta, ok := packet["packet_meta"].(map[string]interface{})
	if !ok {
		issues = append(issues, "packet_meta is missing or invalid")
	} else {
		requiredMetaFields := []string{"packet_id", "repo_target", "branch_context", "target_executor"}
		for _, f := range requiredMetaFields {
			val, exists := meta[f]
			if !exists {
				issues = append(issues, fmt.Sprintf("packet_meta.%s is missing", f))
			} else if s, ok := val.(string); !ok || strings.TrimSpace(s) == "" {
				issues = append(issues, fmt.Sprintf("packet_meta.%s is empty", f))
			}
		}
	}

	exec, ok := packet["execution_payload"].(map[string]interface{})
	if !ok {
		issues = append(issues, "execution_payload is missing or invalid")
	} else {
		requiredPayloadFields := []string{"goal", "scope", "validation_contract"}
		for _, f := range requiredPayloadFields {
			val, exists := exec[f]
			if !exists {
				issues = append(issues, fmt.Sprintf("execution_payload.%s is missing", f))
			} else if s, ok := val.(string); !ok || strings.TrimSpace(s) == "" {
				if f == "validation_contract" {
					if obj, ok := val.(map[string]interface{}); !ok || len(obj) == 0 {
						issues = append(issues, "execution_payload.validation_contract is missing or invalid")
					}
				} else {
					issues = append(issues, fmt.Sprintf("execution_payload.%s is empty", f))
				}
			}
		}

		if contract, ok := exec["validation_contract"].(map[string]interface{}); ok && len(contract) > 0 {
			mode, _ := contract["mode"].(string)
			failurePolicy, _ := contract["failure_policy"].(string)
			if strings.TrimSpace(mode) == "" {
				issues = append(issues, "execution_payload.validation_contract.mode is missing")
			}
			if strings.TrimSpace(failurePolicy) == "" {
				issues = append(issues, "execution_payload.validation_contract.failure_policy is missing")
			}

			switch mode {
			case "commands":
				if commands, ok := contract["commands"].([]interface{}); !ok || len(commands) == 0 {
					issues = append(issues, "execution_payload.validation_contract.commands is missing or empty")
				}
			case "manual":
				if checks, ok := contract["manual_checks"].([]interface{}); !ok || len(checks) == 0 {
					issues = append(issues, "execution_payload.validation_contract.manual_checks is missing or empty")
				}
			case "external":
				if evidence, ok := contract["required_evidence"].([]interface{}); !ok || len(evidence) == 0 {
					issues = append(issues, "execution_payload.validation_contract.required_evidence is missing or empty")
				}
			case "not_applicable":
				if reason, ok := contract["not_applicable_reason"].(string); !ok || strings.TrimSpace(reason) == "" {
					issues = append(issues, "execution_payload.validation_contract.not_applicable_reason is missing")
				}
			case "deferred":
				deferredReason, reasonOK := contract["deferred_reason"].(string)
				deferredOwner, ownerOK := contract["deferred_owner"].(string)
				if !reasonOK || strings.TrimSpace(deferredReason) == "" {
					issues = append(issues, "execution_payload.validation_contract.deferred_reason is missing")
				}
				if !ownerOK || strings.TrimSpace(deferredOwner) == "" {
					issues = append(issues, "execution_payload.validation_contract.deferred_owner is missing")
				}
			default:
				issues = append(issues, fmt.Sprintf("execution_payload.validation_contract.mode %q is invalid", mode))
			}
		}
	}

	return issues
}

func validateRenderedBrief(brief string) []string {
	var issues []string

	// 1. Validate required sections
	requiredHeadings := []string{
		"## Task",
		"## Scope",
		"## Do not change",
		"## File targets",
		"## Implementation steps",
		"## Expected behavior",
		"## Validation",
		"## DONE",
		"## BLOCKED",
		"## Final response format",
	}

	for _, heading := range requiredHeadings {
		if !strings.Contains(brief, heading) {
			issues = append(issues, fmt.Sprintf("missing required section: %q", heading))
		}
	}

	// 2. Validate non-empty goal and scope content
	if idxTask := strings.Index(brief, "## Task"); idxTask != -1 {
		rest := brief[idxTask+len("## Task"):]
		if idxNext := strings.Index(rest, "## "); idxNext != -1 {
			taskContent := strings.TrimSpace(rest[:idxNext])
			if taskContent == "" || strings.HasPrefix(taskContent, "{{") {
				issues = append(issues, "Task section is empty or unrendered")
			}
		}
	}

	if idxScope := strings.Index(brief, "## Scope"); idxScope != -1 {
		rest := brief[idxScope+len("## Scope"):]
		if idxNext := strings.Index(rest, "## "); idxNext != -1 {
			scopeContent := strings.TrimSpace(rest[:idxNext])
			if scopeContent == "" || strings.HasPrefix(scopeContent, "{{") {
				issues = append(issues, "Scope section is empty or unrendered")
			}
		}
	}

	// 3. Absence of forbidden planner/audit markers
	forbiddenMarkers := []string{
		"planner_context",
		"audit_seed",
		"rejected_alternatives",
		"risk_register",
		"validation_commands",
		"future-pass",
		"approval rationale",
	}
	for _, marker := range forbiddenMarkers {
		if strings.Contains(strings.ToLower(brief), marker) {
			issues = append(issues, fmt.Sprintf("forbidden context marker detected: %q", marker))
		}
	}

	// 4. Absence of obvious unredacted secrets
	if validation.HasSecret(brief) {
		issues = append(issues, "sensitive data / secret detected in rendered brief")
	}

	return issues
}

func locateTemplateFile(p string) string {
	if _, err := os.Stat(p); err == nil {
		return p
	}
	dir := "."
	for i := 0; i < 5; i++ {
		tryPath := filepath.Join(dir, p)
		if _, err := os.Stat(tryPath); err == nil {
			return tryPath
		}
		// Also map handoffs/ to relay-contracts/
		if strings.HasPrefix(p, "handoffs/") {
			tryMapped := filepath.Join(dir, strings.Replace(p, "handoffs/", "relay-contracts/", 1))
			if _, err := os.Stat(tryMapped); err == nil {
				return tryMapped
			}
		}
		dir = filepath.Join(dir, "..")
	}
	return p
}

// Custom Mustache Engine implementation

type Node interface {
	Eval(stack ContextStack) (string, error)
}

type TextNode string

func (t TextNode) Eval(stack ContextStack) (string, error) {
	return string(t), nil
}

type VarNode string

func (v VarNode) Eval(stack ContextStack) (string, error) {
	val := stack.Resolve(string(v))
	if val == nil {
		return "", nil
	}
	return fmt.Sprintf("%v", val), nil
}

type SectionNode struct {
	Path     string
	Children []Node
}

func (s *SectionNode) Eval(stack ContextStack) (string, error) {
	val := stack.Resolve(s.Path)
	if val == nil {
		return "", nil
	}
	switch v := val.(type) {
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			for _, child := range s.Children {
				res, err := child.Eval(stack.Push(item))
				if err != nil {
					return "", err
				}
				sb.WriteString(res)
			}
		}
		return sb.String(), nil
	case bool:
		if v {
			var sb strings.Builder
			for _, child := range s.Children {
				res, err := child.Eval(stack)
				if err != nil {
					return "", err
				}
				sb.WriteString(res)
			}
			return sb.String(), nil
		}
		return "", nil
	default:
		var sb strings.Builder
		for _, child := range s.Children {
			res, err := child.Eval(stack.Push(v))
			if err != nil {
				return "", err
			}
			sb.WriteString(res)
		}
		return sb.String(), nil
	}
}

type ContextStack []interface{}

func (s ContextStack) Push(ctx interface{}) ContextStack {
	return append(s, ctx)
}

func (s ContextStack) Resolve(path string) interface{} {
	if path == "." {
		if len(s) > 0 {
			return s[len(s)-1]
		}
		return nil
	}
	for i := len(s) - 1; i >= 0; i-- {
		val := resolveInValue(s[i], path)
		if val != nil {
			return val
		}
	}
	return nil
}

func resolveInValue(val interface{}, path string) interface{} {
	if path == "" {
		return val
	}
	parts := strings.Split(path, ".")
	curr := val
	for _, part := range parts {
		if part == "" {
			continue
		}
		if m, ok := curr.(map[string]interface{}); ok {
			curr, ok = m[part]
			if !ok {
				return nil
			}
		} else {
			return nil
		}
	}
	return curr
}

func parseTemplate(tmpl string) ([]Node, error) {
	var nodes []Node
	remaining := tmpl
	for {
		startIdx := strings.Index(remaining, "{{")
		if startIdx == -1 {
			if len(remaining) > 0 {
				nodes = append(nodes, TextNode(remaining))
			}
			break
		}
		if startIdx > 0 {
			nodes = append(nodes, TextNode(remaining[:startIdx]))
		}
		remaining = remaining[startIdx+2:]
		endIdx := strings.Index(remaining, "}}")
		if endIdx == -1 {
			return nil, fmt.Errorf("unclosed mustache tag")
		}
		tag := strings.TrimSpace(remaining[:endIdx])
		remaining = remaining[endIdx+2:]

		if strings.HasPrefix(tag, "#") {
			path := strings.TrimSpace(tag[1:])
			closeTag := "/" + path
			depth := 1
			scan := remaining
			innerLen := 0
			foundClose := false
			for {
				nextStart := strings.Index(scan, "{{")
				if nextStart == -1 {
					break
				}
				nextEnd := strings.Index(scan[nextStart+2:], "}}")
				if nextEnd == -1 {
					break
				}
				nextTag := strings.TrimSpace(scan[nextStart+2 : nextStart+2+nextEnd])
				if nextTag == "#"+path {
					depth++
				} else if nextTag == closeTag {
					depth--
					if depth == 0 {
						innerLen += nextStart
						foundClose = true
						break
					}
				}
				scan = scan[nextStart+2+nextEnd+2:]
				innerLen += nextStart + 2 + nextEnd + 2
			}
			if !foundClose {
				return nil, fmt.Errorf("unclosed section tag: #%s", path)
			}
			sectionBody := remaining[:innerLen]
			remaining = remaining[innerLen+len(closeTag)+4:]

			children, err := parseTemplate(sectionBody)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, &SectionNode{
				Path:     path,
				Children: children,
			})
		} else if strings.HasPrefix(tag, "/") {
			return nil, fmt.Errorf("unexpected close tag: %s", tag)
		} else {
			nodes = append(nodes, VarNode(tag))
		}
	}
	return nodes, nil
}

func renderTemplate(tmpl string, packet map[string]interface{}) (string, error) {
	nodes, err := parseTemplate(tmpl)
	if err != nil {
		return "", err
	}

	createdDate := time.Now().Format("2006-01-02")
	var taskSlug string
	if meta, ok := packet["packet_meta"].(map[string]interface{}); ok {
		if cd, ok := meta["created_at"].(string); ok && len(cd) >= 10 {
			createdDate = cd[:10]
		}
		if ts, ok := meta["task_slug"].(string); ok {
			taskSlug = ts
		}
	}

	renderCtx := map[string]interface{}{
		"packet_meta":       packet["packet_meta"],
		"execution_payload": packet["execution_payload"],
		"YYYY-MM-DD":        createdDate,
		"short-task-name":   taskSlug,
	}

	if exec, ok := packet["execution_payload"].(map[string]interface{}); ok {
		if contract, ok := exec["validation_contract"].(map[string]interface{}); ok {
			mode, _ := contract["mode"].(string)
			renderCtx["validation_contract_is_commands"] = mode == "commands"
			renderCtx["validation_contract_is_manual"] = mode == "manual"
			renderCtx["validation_contract_is_external"] = mode == "external"
			renderCtx["validation_contract_is_not_applicable"] = mode == "not_applicable"
			renderCtx["validation_contract_is_deferred"] = mode == "deferred"
			if mode == "commands" {
				requiredCommands, optionalCommands := splitValidationCommands(contract["commands"])
				renderCtx["required_validation_commands"] = requiredCommands
				renderCtx["optional_validation_commands"] = optionalCommands
				renderCtx["has_required_validation_commands"] = len(requiredCommands) > 0
				renderCtx["has_optional_validation_commands"] = len(optionalCommands) > 0
			}
		}
	}

	stack := ContextStack{renderCtx}

	var sb strings.Builder
	for _, node := range nodes {
		res, err := node.Eval(stack)
		if err != nil {
			return "", err
		}
		sb.WriteString(res)
	}
	return sb.String(), nil
}

func splitValidationCommands(raw interface{}) ([]interface{}, []interface{}) {
	commands, ok := raw.([]interface{})
	if !ok {
		return nil, nil
	}
	requiredCommands := make([]interface{}, 0, len(commands))
	optionalCommands := make([]interface{}, 0, len(commands))
	for _, command := range commands {
		required := false
		if commandMap, ok := command.(map[string]interface{}); ok {
			required, _ = commandMap["required"].(bool)
		}
		if required {
			requiredCommands = append(requiredCommands, command)
		} else {
			optionalCommands = append(optionalCommands, command)
		}
	}
	return requiredCommands, optionalCommands
}
