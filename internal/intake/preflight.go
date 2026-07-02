package intake

import (
	"crypto/sha256"
	"encoding/hex"
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
	Markdown         string
	RepoTarget       string
	BranchContext    string
	PlanID           string
	PassID           string
	ContextPacketID  string
	SourceSnapshotID string
	SourceMode       string
}

type HandoffPreflightResult struct {
	OK                     bool                    `json:"ok"`
	Status                 string                  `json:"status"`
	IsCompileReady         bool                    `json:"is_compile_ready"`
	IssueCounts            map[string]int          `json:"issue_counts"`
	Issues                 []HandoffPreflightIssue `json:"issues"`
	SubmittedHandoffSHA256 string                  `json:"submitted_handoff_sha256"`
	ByteCount              int64                   `json:"byte_count"`
	SourceMode             string                  `json:"source_mode,omitempty"`
	PlanID                 string                  `json:"plan_id,omitempty"`
	PassID                 string                  `json:"pass_id,omitempty"`
	ContextPacketID        string                  `json:"context_packet_id,omitempty"`
	SourceSnapshotID       string                  `json:"source_snapshot_id,omitempty"`
	GeneratedAt            string                  `json:"generated_at"`
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
		addPreflightIssue(&result, issue)
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
		addPreflightIssue(&result, issue)
	}

	repo := firstNonEmpty(input.RepoTarget, metadata["repo"], metadata["repo_target"])
	if repo == "" {
		issue := newBlockingIssue("repository_target_missing", "No repository target found in arguments or frontmatter.", "Provide repo_target in tool arguments or set repo: or repo_target: in the handoff frontmatter.", &HandoffPreflightIssueLocation{Section: "frontmatter", Field: "repo_target"})
		addPreflightIssue(&result, issue)
	}

	branch := firstNonEmpty(input.BranchContext, metadata["branch"], metadata["branch_context"])
	if branch == "" {
		issue := newBlockingIssue("branch_context_missing", "No branch context found in arguments or frontmatter.", "Provide branch_context in tool arguments or set branch: or branch_context: in the handoff frontmatter.", &HandoffPreflightIssueLocation{Section: "frontmatter", Field: "branch_context"})
		addPreflightIssue(&result, issue)
	}

	if strings.TrimSpace(input.PassID) != "" && strings.TrimSpace(input.PlanID) == "" {
		issue := newBlockingIssue("managed_plan_missing", "pass_id requires plan_id for managed pass association.", "Provide the Relay plan_id that owns the selected pass, or omit pass_id for a standalone run.", &HandoffPreflightIssueLocation{Field: "plan_id"})
		addPreflightIssue(&result, issue)
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
		addPreflightIssue(&result, issue)
	} else {
		parsed, issue := parseCompilerInputSection(markdown)
		if issue != nil {
			addPreflightIssue(&result, *issue)
		} else {
			validateCompilerInputFields(&result, parsed.Doc)
		}
	}

	if input.PlanID != "" && input.PassID != "" {
		managedPlanPass := strings.TrimSpace(metadata["managed_plan_pass"])
		if managedPlanPass != "" && !strings.EqualFold(managedPlanPass, input.PassID) {
			issue := newBlockingIssue("managed_pass_mismatch", fmt.Sprintf("Handoff metadata managed_plan_pass %q does not match submitted pass_id %q.", managedPlanPass, input.PassID), "Correct managed_plan_pass in the handoff metadata or submit with the matching pass_id.", &HandoffPreflightIssueLocation{Section: "frontmatter", Field: "managed_plan_pass"})
			addPreflightIssue(&result, issue)
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

type compilerInputYAMLDoc struct {
	CompilerInput *compilerInputBody `yaml:"compiler_input"`
}

type compilerInputBody struct {
	Goal                any `yaml:"goal"`
	Scope               any `yaml:"scope"`
	FileTargets         any `yaml:"file_targets"`
	ImplementationSteps any `yaml:"implementation_steps"`
	CodeRequirements    any `yaml:"code_requirements"`
	ValidationContract  any `yaml:"validation_contract"`
	CompletionContract  any `yaml:"completion_contract"`
}

type parsedCompilerInput struct {
	Present bool
	Doc     compilerInputYAMLDoc
}

func parseCompilerInputSection(content string) (parsedCompilerInput, *HandoffPreflightIssue) {
	section, ok := extractCompilerInputSection(content)
	if !ok {
		issue := newBlockingIssue("compiler_input_missing", "Required section is missing: compiler_input.", "Add a <compiler_input> or ## Compiler Input section with a fenced YAML block containing goal, scope, file_targets, implementation_steps, code_requirements, validation_contract, and completion_contract.", &HandoffPreflightIssueLocation{Section: "compiler_input"})
		return parsedCompilerInput{}, &issue
	}

	yamlText := stripMarkdownFences(section)
	if strings.TrimSpace(yamlText) == "" {
		issue := newBlockingIssue("compiler_input_yaml_invalid", "compiler_input section is empty.", "Add valid YAML with a top-level compiler_input object.", &HandoffPreflightIssueLocation{Section: "compiler_input"})
		return parsedCompilerInput{Present: true}, &issue
	}

	var doc compilerInputYAMLDoc
	if err := yaml.Unmarshal([]byte(yamlText), &doc); err != nil {
		issue := newBlockingIssue("compiler_input_yaml_invalid", "Fenced YAML inside compiler_input does not parse: "+err.Error(), "Fix the compiler_input YAML syntax. The section must contain valid YAML with a top-level compiler_input object.", &HandoffPreflightIssueLocation{Section: "compiler_input"})
		return parsedCompilerInput{Present: true}, &issue
	}
	if doc.CompilerInput == nil {
		issue := newBlockingIssue("compiler_input_required_field_missing", "compiler_input YAML must contain a non-empty top-level compiler_input object.", "Add a top-level compiler_input mapping containing goal, scope, file_targets, implementation_steps, code_requirements, validation_contract, and completion_contract.", &HandoffPreflightIssueLocation{Section: "compiler_input"})
		return parsedCompilerInput{Present: true}, &issue
	}
	return parsedCompilerInput{Present: true, Doc: doc}, nil
}

func validateCompilerInputFields(result *HandoffPreflightResult, doc compilerInputYAMLDoc) {
	ci := doc.CompilerInput
	if ci == nil {
		return
	}

	checkScalarField := func(value any, fieldName, message string) {
		if !isNonEmptyScalar(value) {
			issue := newBlockingIssue("compiler_input_required_field_missing", message, fmt.Sprintf("Add a non-empty scalar %s field under compiler_input in the fenced YAML block.", fieldName), &HandoffPreflightIssueLocation{Section: "compiler_input", Field: fieldName})
			addPreflightIssue(result, issue)
		}
	}

	checkListField := func(value any, fieldName, message string) {
		if !isNonEmptyList(value) {
			issue := newBlockingIssue("compiler_input_list_empty", message, fmt.Sprintf("Add at least one entry to %s under compiler_input in the fenced YAML block.", fieldName), &HandoffPreflightIssueLocation{Section: "compiler_input", Field: fieldName})
			addPreflightIssue(result, issue)
		}
	}

	checkMapField := func(value any, fieldName, message string) {
		if !isNonEmptyMap(value) {
			issue := newBlockingIssue("compiler_input_required_field_missing", message, fmt.Sprintf("Add a non-empty mapping for %s under compiler_input in the fenced YAML block.", fieldName), &HandoffPreflightIssueLocation{Section: "compiler_input", Field: fieldName})
			addPreflightIssue(result, issue)
		}
	}

	checkScalarField(ci.Goal, "goal", "compiler_input.goal is missing, empty, or not a scalar value.")
	checkScalarField(ci.Scope, "scope", "compiler_input.scope is missing, empty, or not a scalar value.")
	checkListField(ci.FileTargets, "file_targets", "compiler_input.file_targets is missing, empty, or not a list.")
	checkListField(ci.ImplementationSteps, "implementation_steps", "compiler_input.implementation_steps is missing, empty, or not a list.")
	checkListField(ci.CodeRequirements, "code_requirements", "compiler_input.code_requirements is missing, empty, or not a list.")
	checkMapField(ci.ValidationContract, "validation_contract", "compiler_input.validation_contract is missing, empty, or not a mapping.")
	checkMapField(ci.CompletionContract, "completion_contract", "compiler_input.completion_contract is missing, empty, or not a mapping.")
}

func isNonEmptyScalar(val any) bool {
	switch v := val.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any, map[string]any, map[any]any:
		return false
	default:
		return true
	}
}

func isNonEmptyList(val any) bool {
	v, ok := val.([]any)
	return ok && len(v) > 0
}

func isNonEmptyMap(val any) bool {
	switch v := val.(type) {
	case map[string]any:
		return len(v) > 0
	case map[any]any:
		return len(v) > 0
	default:
		return false
	}
}

func stripMarkdownFences(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		stripped = append(stripped, line)
	}
	return strings.TrimSpace(strings.Join(stripped, "\n"))
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
	if val, ok := extractCompilerInputSection(content); ok {
		return val
	}
	return ""
}

func extractCompilerInputSection(content string) (string, bool) {
	if val, ok := extractXMLSection(content, "compiler_input"); ok {
		return val, ok
	}
	if val, ok := extractMarkdownSectionForTag(content, "compiler input"); ok {
		return val, ok
	}
	return extractMarkdownSectionForTag(content, "compiler_input")
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

func addPreflightIssue(result *HandoffPreflightResult, issue HandoffPreflightIssue) {
	result.Issues = append(result.Issues, issue)
	result.IssueCounts[string(issue.Severity)]++
	if issue.BlocksSubmission {
		result.OK = false
		result.Status = "blocked"
		result.IsCompileReady = false
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
