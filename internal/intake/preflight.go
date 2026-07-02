package intake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type HandoffPreflightIssueSeverity string

const (
	SeverityError   HandoffPreflightIssueSeverity = "error"
	SeverityWarning HandoffPreflightIssueSeverity = "warning"
)

type HandoffPreflightIssueLocation struct {
	Section  string `json:"section,omitempty"`
	Field    string `json:"field,omitempty"`
	LineHint int    `json:"line_hint,omitempty"`
}

type HandoffPreflightIssue struct {
	Code             string                         `json:"code"`
	Severity         HandoffPreflightIssueSeverity  `json:"severity"`
	Location         *HandoffPreflightIssueLocation `json:"location,omitempty"`
	Message          string                         `json:"message"`
	RepairGuidance   string                         `json:"repair_guidance"`
	BlocksSubmission bool                           `json:"blocks_submission"`
}

type HandoffPreflightInput struct {
	Markdown          string
	RepoTarget        string
	BranchContext     string
	PlanID            string
	PassID            string
	ContextPacketID   string
	SourceSnapshotID  string
	SourceMode        string
}

type HandoffPreflightResult struct {
	OK                    bool                       `json:"ok"`
	Status                string                     `json:"status"`
	IsCompileReady        bool                       `json:"is_compile_ready"`
	IssueCounts           map[string]int             `json:"issue_counts"`
	Issues                []HandoffPreflightIssue    `json:"issues"`
	SubmittedHandoffSHA256 string                    `json:"submitted_handoff_sha256"`
	ByteCount             int64                      `json:"byte_count"`
	SourceMode            string                     `json:"source_mode,omitempty"`
	PlanID                string                     `json:"plan_id,omitempty"`
	PassID                string                     `json:"pass_id,omitempty"`
	ContextPacketID       string                     `json:"context_packet_id,omitempty"`
	SourceSnapshotID      string                     `json:"source_snapshot_id,omitempty"`
	GeneratedAt           string                     `json:"generated_at"`
}

// ValidatePlannerHandoffForCompile performs deterministic compile-aware preflight
// on a Planner handoff. It checks handoff structure, compiler_input YAML,
// provenance, and managed plan/pass association consistency.
//
// This is a pure function: it does not open a transaction, perform durable writes,
// or dispatch executors. Callers that intend to create a run should call this first
// and only proceed when result.OK is true.
func ValidatePlannerHandoffForCompile(input HandoffPreflightInput) (HandoffPreflightResult, error) {
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	result := HandoffPreflightResult{
		OK:          true,
		Status:      "pending",
		GeneratedAt: generatedAt,
		IssueCounts: map[string]int{
			"error":   0,
			"warning": 0,
		},
		Issues: make([]HandoffPreflightIssue, 0),
	}

	markdown := strings.TrimSpace(input.Markdown)
	markdownBytes := []byte(input.Markdown)
	handoffHash := sha256.Sum256(markdownBytes)
	result.SubmittedHandoffSHA256 = hex.EncodeToString(handoffHash[:])
	result.ByteCount = int64(len(markdownBytes))
	result.SourceMode = input.SourceMode
	result.PlanID = input.PlanID
	result.PassID = input.PassID
	result.ContextPacketID = input.ContextPacketID
	result.SourceSnapshotID = input.SourceSnapshotID

	if markdown == "" {
		issue := newBlockingIssue("handoff_empty", "Handoff markdown is empty.", "Provide non-empty Planner handoff Markdown content.", &HandoffPreflightIssueLocation{Section: "handoff"})
		result.Issues = append(result.Issues, issue)
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
		result.IssueCounts["error"]++
		return result, nil
	}

	metadata, _, _, fmWarnings := ParseFrontmatter(markdown)

	hasFrontmatter := len(metadata) > 0
	for _, w := range fmWarnings {
		if w == "No frontmatter block found" || w == "Frontmatter block is missing a closing '---' delimiter" {
			hasFrontmatter = false
		}
	}

	if !hasFrontmatter {
		issue := newBlockingIssue("frontmatter_missing", "Handoff is missing a valid frontmatter block.", "Add a YAML-style frontmatter block delimited by --- containing at minimum repository and branch fields.", &HandoffPreflightIssueLocation{Section: "frontmatter"})
		result.Issues = append(result.Issues, issue)
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
		result.IssueCounts["error"]++
	}

	repo := firstNonEmpty(input.RepoTarget, metadata["repo"], metadata["repo_target"])
	if repo == "" {
		issue := newBlockingIssue("repository_target_missing", "No repository target found in arguments or frontmatter.", "Provide repo_target in tool arguments or set repo: or repo_target: in the handoff frontmatter.", &HandoffPreflightIssueLocation{Section: "frontmatter", Field: "repo_target"})
		result.Issues = append(result.Issues, issue)
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
		result.IssueCounts["error"]++
	}

	branch := firstNonEmpty(input.BranchContext, metadata["branch"], metadata["branch_context"])
	if branch == "" {
		issue := newBlockingIssue("branch_context_missing", "No branch context found in arguments or frontmatter.", "Provide branch_context in tool arguments or set branch: or branch_context: in the handoff frontmatter.", &HandoffPreflightIssueLocation{Section: "frontmatter", Field: "branch_context"})
		result.Issues = append(result.Issues, issue)
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
		result.IssueCounts["error"]++
	}

	decisionLogMissing := !hasXMLSection(markdown, "decision_log") && !hasMarkdownSection(markdown, "decision log")
	if decisionLogMissing {
		issue := newWarning("semantic_section_missing", "Required semantic section is missing: decision_log.", "Add a <decision_log> or ## Decision Log section listing key planning decisions.", &HandoffPreflightIssueLocation{Section: "decision_log"})
		result.Issues = append(result.Issues, issue)
		result.IssueCounts["warning"]++
	}

	constraintsMissing := !hasXMLSection(markdown, "constraints") && !hasMarkdownSection(markdown, "constraints")
	if constraintsMissing {
		issue := newWarning("semantic_section_missing", "Required semantic section is missing: constraints.", "Add a <constraints> or ## Constraints section listing implementation constraints.", &HandoffPreflightIssueLocation{Section: "constraints"})
		result.Issues = append(result.Issues, issue)
		result.IssueCounts["warning"]++
	}

	compilerInputMissing := !hasXMLSection(markdown, "compiler_input") && !hasMarkdownSection(markdown, "compiler input")
	if compilerInputMissing {
		issue := newBlockingIssue("compiler_input_missing", "Required section is missing: compiler_input.", "Add a <compiler_input> or ## Compiler Input section with a fenced YAML block containing goal, scope, file_targets, implementation_steps, code_requirements, validation_contract, and completion_contract.", &HandoffPreflightIssueLocation{Section: "compiler_input"})
		result.Issues = append(result.Issues, issue)
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
		result.IssueCounts["error"]++
	} else {
		compilerInputContent := extractSectionContent(markdown, "compiler_input")
		validateCompilerInputYAML(&result, compilerInputContent)
		validateCompilerInputFields(&result, compilerInputContent)
	}

	if input.PlanID != "" && input.PassID != "" {
		managedPlanPass := strings.TrimSpace(metadata["managed_plan_pass"])
		if managedPlanPass != "" && !strings.EqualFold(managedPlanPass, input.PassID) {
			issue := newBlockingIssue("managed_pass_mismatch", fmt.Sprintf("Handoff metadata managed_plan_pass %q does not match submitted pass_id %q.", managedPlanPass, input.PassID), "Correct managed_plan_pass in the handoff metadata or submit with the matching pass_id.", &HandoffPreflightIssueLocation{Section: "frontmatter", Field: "managed_plan_pass"})
			result.Issues = append(result.Issues, issue)
			result.OK = false
			result.Status = "blocked"
			result.IsCompileReady = false
			result.IssueCounts["error"]++
		}
	}

	if result.Status == "pending" {
		if result.OK {
			result.Status = "passed"
			result.IsCompileReady = true
		}
	}

	return result, nil
}

func validateCompilerInputYAML(result *HandoffPreflightResult, content string) {
	cleaned := strings.TrimSpace(content)
	if cleaned == "" {
		return
	}

	if !strings.Contains(cleaned, "compiler_input:") {
		return
	}

	lines := strings.Split(cleaned, "\n")
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		stripped = append(stripped, line)
	}
	strippedText := strings.Join(stripped, "\n")

	if !strings.Contains(strippedText, "compiler_input:") {
		return
	}

	var doc struct {
		CompilerInput struct{} `yaml:"compiler_input"`
	}
	if err := yaml.Unmarshal([]byte(strippedText), &doc); err != nil {
		issue := newBlockingIssue("compiler_input_yaml_invalid", "Fenced YAML inside compiler_input does not parse: "+err.Error(), "Fix the compiler_input YAML syntax. The section must contain valid YAML with a top-level compiler_input key.", &HandoffPreflightIssueLocation{Section: "compiler_input"})
		result.Issues = append(result.Issues, issue)
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
		result.IssueCounts["error"]++
	}
}

type compilerInputYAMLDoc struct {
	CompilerInput struct {
		Goal                interface{} `yaml:"goal"`
		Scope               interface{} `yaml:"scope"`
		FileTargets         interface{} `yaml:"file_targets"`
		ImplementationSteps interface{} `yaml:"implementation_steps"`
		CodeRequirements    interface{} `yaml:"code_requirements"`
		ValidationContract  interface{} `yaml:"validation_contract"`
		CompletionContract  interface{} `yaml:"completion_contract"`
	} `yaml:"compiler_input"`
}

func validateCompilerInputFields(result *HandoffPreflightResult, content string) {
	cleaned := strings.TrimSpace(content)
	if cleaned == "" {
		return
	}
	if !strings.Contains(cleaned, "compiler_input:") {
		return
	}

	lines := strings.Split(cleaned, "\n")
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		stripped = append(stripped, line)
	}
	strippedText := strings.Join(stripped, "\n")

	var doc compilerInputYAMLDoc
	if err := yaml.Unmarshal([]byte(strippedText), &doc); err != nil {
		return
	}

	ci := doc.CompilerInput

	checkStringField := func(value interface{}, fieldName, code, message string) {
		if isEmptyYAMLValue(value) {
			issue := newBlockingIssue(code, message, fmt.Sprintf("Add a %s field under compiler_input in the fenced YAML block.", fieldName), &HandoffPreflightIssueLocation{Section: "compiler_input", Field: fieldName})
			result.Issues = append(result.Issues, issue)
			result.OK = false
			result.Status = "blocked"
			result.IsCompileReady = false
			result.IssueCounts["error"]++
		}
	}

	checkListField := func(value interface{}, fieldName, code, message string) {
		if isEmptyYAMLValue(value) {
			issue := newBlockingIssue(code, message, fmt.Sprintf("Add at least one entry to %s under compiler_input in the fenced YAML block.", fieldName), &HandoffPreflightIssueLocation{Section: "compiler_input", Field: fieldName})
			result.Issues = append(result.Issues, issue)
			result.OK = false
			result.Status = "blocked"
			result.IsCompileReady = false
			result.IssueCounts["error"]++
		}
	}

	checkStringField(ci.Goal, "goal", "compiler_input_required_field_missing", "compiler_input.goal is missing or empty.")
	checkStringField(ci.Scope, "scope", "compiler_input_required_field_missing", "compiler_input.scope is missing or empty.")
	checkListField(ci.FileTargets, "file_targets", "compiler_input_list_empty", "compiler_input.file_targets is missing or empty.")
	checkListField(ci.ImplementationSteps, "implementation_steps", "compiler_input_list_empty", "compiler_input.implementation_steps is missing or empty.")
	checkStringField(ci.CodeRequirements, "code_requirements", "compiler_input_required_field_missing", "compiler_input.code_requirements is missing or empty.")
	checkStringField(ci.ValidationContract, "validation_contract", "compiler_input_required_field_missing", "compiler_input.validation_contract is missing or empty.")
	checkStringField(ci.CompletionContract, "completion_contract", "compiler_input_required_field_missing", "compiler_input.completion_contract is missing or empty.")
}

func isEmptyYAMLValue(val interface{}) bool {
	switch v := val.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	case map[interface{}]interface{}:
		return len(v) == 0
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return true
		}
		return string(encoded) == "null" || string(encoded) == "{}" || string(encoded) == "[]" || string(encoded) == `""`
	}
}

func hasXMLSection(content, tag string) bool {
	startTag := "<" + tag + ">"
	startIdx := strings.Index(content, startTag)
	if startIdx == -1 {
		return false
	}
	endTag := "</" + tag + ">"
	endIdx := strings.Index(content, endTag)
	return endIdx > startIdx
}

var markdownHeadingRE = regexp.MustCompile(`(?m)^#+\s+(.*?)$`)

func hasMarkdownSection(content, heading string) bool {
	normHeading := strings.ToLower(strings.TrimSpace(heading))
	normHeading = strings.TrimRight(normHeading, ":")
	matches := markdownHeadingRE.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			hText := strings.ToLower(strings.TrimSpace(match[1]))
			hText = strings.TrimRight(hText, ":")
			if hText == normHeading {
				return true
			}
		}
	}
	return false
}

func extractSectionContent(content, tag string) string {
	if val, ok := extractXMLSection(content, tag); ok {
		return val
	}
	if val, ok := extractMarkdownSectionForTag(content, tag); ok {
		return val
	}
	return ""
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

func extractMarkdownSectionForTag(content, heading string) (string, bool) {
	normHeading := strings.ToLower(strings.TrimSpace(heading))
	normHeading = strings.TrimRight(normHeading, ":")
	lines := strings.Split(content, "\n")
	var sectionLines []string
	found := false
	currentHeadingDepth := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			hText := strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			hText = strings.TrimRight(hText, ":")
			if found {
				if len(strings.TrimLeft(trimmed, "#")) < currentHeadingDepth {
					break
				}
				sectionLines = append(sectionLines, line)
				continue
			}
			if strings.HasPrefix(trimmed, "#") {
				hashCount := 0
				for _, r := range trimmed {
					if r == '#' {
						hashCount++
					} else {
						break
					}
				}
				if hText == normHeading || strings.Contains(hText, normHeading) {
					found = true
					currentHeadingDepth = hashCount
					continue
				}
			}
		}
		if found {
			sectionLines = append(sectionLines, line)
		}
	}
	return strings.TrimSpace(strings.Join(sectionLines, "\n")), found
}

func newBlockingIssue(code, message, repairGuidance string, location *HandoffPreflightIssueLocation) HandoffPreflightIssue {
	return HandoffPreflightIssue{
		Code:             code,
		Severity:         SeverityError,
		Location:         location,
		Message:          message,
		RepairGuidance:   repairGuidance,
		BlocksSubmission: true,
	}
}

func newWarning(code, message, repairGuidance string, location *HandoffPreflightIssueLocation) HandoffPreflightIssue {
	return HandoffPreflightIssue{
		Code:             code,
		Severity:         SeverityWarning,
		Location:         location,
		Message:          message,
		RepairGuidance:   repairGuidance,
		BlocksSubmission: false,
	}
}
