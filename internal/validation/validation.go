package validation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

type ValidationError struct {
	Type           string `json:"type"` // "schema", "structural", "security", "state", "path", "input"
	Code           string `json:"code,omitempty"`
	Message        string `json:"message"`
	RepairEligible bool   `json:"repair_eligible"`
}

const (
	CodeJSONSyntax                 = "CANONICAL_PACKET_JSON_SYNTAX"
	CodeMissingRequiredField       = "CANONICAL_PACKET_MISSING_REQUIRED_FIELD"
	CodeInvalidEnum                = "CANONICAL_PACKET_INVALID_ENUM"
	CodeExtraProperty              = "CANONICAL_PACKET_EXTRA_PROPERTY"
	CodeInvalidType                = "CANONICAL_PACKET_INVALID_TYPE"
	CodeStringPatternMismatch      = "CANONICAL_PACKET_STRING_PATTERN_MISMATCH"
	CodeFileTargetMismatch         = "CANONICAL_PACKET_FILE_TARGET_MISMATCH"
	CodeBlockingUnresolvedQuestion = "CANONICAL_PACKET_BLOCKING_UNRESOLVED_QUESTION"
	CodeMissingImplementationSteps = "CANONICAL_PACKET_MISSING_IMPLEMENTATION_STEPS"
	CodeMissingCodeRequirements    = "CANONICAL_PACKET_MISSING_CODE_REQUIREMENTS"
	CodeMissingPassExitEvidence    = "CANONICAL_PACKET_MISSING_PASS_EXIT_EVIDENCE"
	CodeMissingValidationContract  = "CANONICAL_PACKET_MISSING_VALIDATION_CONTRACT"
	CodeMissingCompilerInput       = "PLANNER_HANDOFF_MISSING_COMPILER_INPUT"
)

type ValidationReport struct {
	Valid          bool              `json:"valid"`
	RepairEligible bool              `json:"repair_eligible"`
	Errors         []ValidationError `json:"errors"`
}

type executionTextItem struct {
	fieldPath       string
	text            string
	structuredScope bool
}

type phraseMatch struct {
	phrase string
	start  int
	end    int
}

// ValidatePacketJSON validates a raw canonical packet JSON string.
// schemaPath is the repo-relative path to the canonical_packet.schema.json file.
func ValidatePacketJSON(packetJSON []byte, schemaPath string) (*ValidationReport, error) {
	report := &ValidationReport{
		Valid:          true,
		RepairEligible: true,
		Errors:         []ValidationError{},
	}

	// 1. Sanity check: JSON unmarshal
	var raw interface{}
	if err := json.Unmarshal(packetJSON, &raw); err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, ValidationError{
			Type:           "structural",
			Code:           CodeJSONSyntax,
			Message:        fmt.Sprintf("Invalid JSON syntax: %v", err),
			RepairEligible: true, // Formatting/syntax errors are repair eligible
		})
		return report, nil
	}

	// 2. JSON Schema validation
	locatedPath := locateSchemaFile(schemaPath)
	schemaBytes, err := os.ReadFile(locatedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file %q: %w", locatedPath, err)
	}
	schemaStr := sanitizeSchemaRegexes(string(schemaBytes))

	schemaLoader := gojsonschema.NewStringLoader(schemaStr)
	documentLoader := gojsonschema.NewGoLoader(raw)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("schema validation error: %w", err)
	}

	if !result.Valid() {
		report.Valid = false
		for _, desc := range result.Errors() {
			report.Errors = append(report.Errors, ValidationError{
				Type:           "schema",
				Code:           mapSchemaErrorType(desc),
				Message:        desc.String(),
				RepairEligible: true, // Schema violations are repair eligible
			})
		}
	}

	// 3. Supplemental semantic & safety checks
	// Let's decode into map to scan it
	var packetMap map[string]interface{}
	_ = json.Unmarshal(packetJSON, &packetMap)

	// Scan for secrets
	secretIssues := scanForSecrets(raw)
	if len(secretIssues) > 0 {
		report.Valid = false
		report.RepairEligible = false // Secrets are never repair eligible
		for _, issue := range secretIssues {
			report.Errors = append(report.Errors, issue)
		}
	}

	// Scan for blocking unresolved questions
	blockingIssues := checkBlockingUnresolvedQuestions(packetMap)
	if len(blockingIssues) > 0 {
		report.Valid = false
		report.RepairEligible = false // explicit blocking: true is an intent failure, not repairable
		for _, issue := range blockingIssues {
			report.Errors = append(report.Errors, issue)
		}
	}

	// Safe path checks (CR5, S6)
	pathIssues := checkPaths(packetMap)
	if len(pathIssues) > 0 {
		report.Valid = false
		report.RepairEligible = false // Path traversal/absolute paths are intent/boundary violations, not repairable formatting issues
		for _, issue := range pathIssues {
			report.Errors = append(report.Errors, issue)
		}
	}

	// Required payload fields checks
	payloadIssues := checkRequiredPayloadFields(packetMap)
	if len(payloadIssues) > 0 {
		report.Valid = false
		report.RepairEligible = false // Missing product behavior or goals is not format repairable
		for _, issue := range payloadIssues {
			report.Errors = append(report.Errors, issue)
		}
	}

	report.RepairEligible = reportIsRepairEligible(report.Errors)

	return report, nil
}

func scanForSecrets(val interface{}) []ValidationError {
	var issues []ValidationError
	switch v := val.(type) {
	case string:
		if HasSecret(v) {
			issues = append(issues, ValidationError{Type: "security", Code: "CANONICAL_PACKET_SECURITY", Message: fmt.Sprintf("secret-like value detected in string: %s", truncateString(v, 30)), RepairEligible: false})
		}
	case map[string]interface{}:
		for _, val := range v {
			issues = append(issues, scanForSecrets(val)...)
		}
	case []interface{}:
		for _, item := range v {
			issues = append(issues, scanForSecrets(item)...)
		}
	}
	return issues
}

var secretRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_\-\.\~]+`),
	regexp.MustCompile(`(?i)jwt\s+[a-zA-Z0-9_\-\.\~]+`),
	regexp.MustCompile(`(?i)eyj[a-zA-Z0-9_\-\~]+\.eyj[a-zA-Z0-9_\-\~]+\.[a-zA-Z0-9_\-\~]+`),
	regexp.MustCompile(`(?i)(?:sig|signature|aws_secret_access_key|private_key|client_secret|client_id|access_token|refresh_token|password|passwd|api_key|apikey)=["']?[a-zA-Z0-9_\-\.\~\+\/]{16,}`),
}

func HasSecret(s string) bool {
	if strings.Contains(s, "-----BEGIN") {
		return true
	}
	for _, re := range secretRegexes {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func hasSecret(s string) bool {
	return HasSecret(s)
}

func truncateString(s string, limit int) string {
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

func checkPaths(packet map[string]interface{}) []ValidationError {
	var issues []ValidationError

	// Check file_targets in execution_payload
	if exec, ok := packet["execution_payload"].(map[string]interface{}); ok {
		if targets, ok := exec["file_targets"].([]interface{}); ok {
			for _, t := range targets {
				if pathStr, ok := t.(string); ok {
					if err := validatePathSafety(pathStr); err != nil {
						issues = append(issues, ValidationError{Type: "path", Code: "CANONICAL_PACKET_UNSAFE_PATH", Message: fmt.Sprintf("unsafe file target: %s (%v)", pathStr, err), RepairEligible: false})
					}
				} else if targetObj, ok := t.(map[string]interface{}); ok {
					if pathStr, ok := targetObj["path"].(string); ok {
						if err := validatePathSafety(pathStr); err != nil {
							issues = append(issues, ValidationError{Type: "path", Code: "CANONICAL_PACKET_UNSAFE_PATH", Message: fmt.Sprintf("unsafe file target: %s (%v)", pathStr, err), RepairEligible: false})
						}
					}
				}
			}
		}
	}

	// Check artifact_paths in packet_meta
	if meta, ok := packet["packet_meta"].(map[string]interface{}); ok {
		if paths, ok := meta["artifact_paths"].(map[string]interface{}); ok {
			for k, p := range paths {
				if pathStr, ok := p.(string); ok {
					if err := validatePathSafety(pathStr); err != nil {
						issues = append(issues, ValidationError{Type: "path", Code: "CANONICAL_PACKET_UNSAFE_PATH", Message: fmt.Sprintf("unsafe artifact path for %s: %s (%v)", k, pathStr, err), RepairEligible: false})
					}
				}
			}
		}
	}

	return issues
}

var unsafeMetaChars = []string{"*", "?", "[", "]", ";", "&", "|", "$", ">", "<", "(", ")", "`", "!"}

func validatePathSafety(p string) error {
	pClean := filepath.Clean(p)
	if filepath.IsAbs(pClean) || strings.HasPrefix(pClean, "/") || strings.HasPrefix(pClean, "\\") {
		return fmt.Errorf("absolute path not allowed")
	}
	if strings.Contains(pClean, "..") {
		return fmt.Errorf("parent directory traversal not allowed")
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("backslashes not allowed (use forward slashes)")
	}
	for _, mc := range unsafeMetaChars {
		if strings.Contains(p, mc) {
			return fmt.Errorf("shell metacharacter %q not allowed", mc)
		}
	}
	return nil
}

func checkRequiredPayloadFields(packet map[string]interface{}) []ValidationError {
	var issues []ValidationError

	exec, ok := packet["execution_payload"].(map[string]interface{})
	if !ok {
		return []ValidationError{{Type: "input", Code: "CANONICAL_PACKET_MISSING_PAYLOAD", Message: "execution_payload is missing or invalid", RepairEligible: false}}
	}

	requiredFields := []string{
		"goal",
		"scope",
		"executor_final_response_format",
	}

	for _, f := range requiredFields {
		val, ok := exec[f]
		if !ok {
			issues = append(issues, ValidationError{Type: "input", Code: CodeMissingRequiredField, Message: fmt.Sprintf("required execution_payload field %q is missing", f), RepairEligible: false})
			continue
		}
		strVal, ok := val.(string)
		if !ok || strings.TrimSpace(strVal) == "" {
			issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: fmt.Sprintf("required execution_payload field %q is empty", f), RepairEligible: false})
		}
	}

	// Validate expected_behavior (should be non-empty array of strings)
	if val, ok := exec["expected_behavior"]; !ok {
		issues = append(issues, ValidationError{Type: "input", Code: CodeMissingRequiredField, Message: "required execution_payload field \"expected_behavior\" is missing", RepairEligible: false})
	} else if arr, ok := val.([]interface{}); !ok || len(arr) == 0 {
		issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: "required execution_payload field \"expected_behavior\" is empty", RepairEligible: false})
	}

	// Validate completion_contract (should be object)
	if val, ok := exec["completion_contract"]; !ok {
		issues = append(issues, ValidationError{Type: "input", Code: CodeMissingRequiredField, Message: "required execution_payload field \"completion_contract\" is missing", RepairEligible: false})
	} else if obj, ok := val.(map[string]interface{}); !ok || len(obj) == 0 {
		issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: "required execution_payload field \"completion_contract\" is empty", RepairEligible: false})
	}

	// Check arrays are not empty
	requiredArrays := []string{
		"non_goals",
		"file_targets",
		"implementation_steps",
		"code_requirements",
	}

	for _, f := range requiredArrays {
		val, ok := exec[f]
		if !ok {
			code := CodeMissingRequiredField
			if f == "implementation_steps" {
				code = CodeMissingImplementationSteps
			} else if f == "code_requirements" {
				code = CodeMissingCodeRequirements
			}
			issues = append(issues, ValidationError{Type: "input", Code: code, Message: fmt.Sprintf("required execution_payload field %q is missing", f), RepairEligible: false})
			continue
		}
		arrVal, ok := val.([]interface{})
		if !ok || len(arrVal) == 0 {
			issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: fmt.Sprintf("required execution_payload field %q is empty", f), RepairEligible: false})
		}
	}

	if val, ok := exec["validation_contract"]; !ok {
		issues = append(issues, ValidationError{Type: "input", Code: CodeMissingValidationContract, Message: "required execution_payload field \"validation_contract\" is missing", RepairEligible: false})
	} else if obj, ok := val.(map[string]interface{}); !ok || len(obj) == 0 {
		issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: "required execution_payload field \"validation_contract\" is empty", RepairEligible: false})
	}

	// 1. Scan execution-critical fields for vague or decision-delegating intent.
	for _, item := range collectExecutionTextItems(exec) {
		if match, ok := findDecisionDelegatingPhrase(item.text); ok {
			if !isProhibitedExampleContext(item.text, match.start, match.end) {
				issues = append(issues, addVagueIntentIssue(match.phrase, item.fieldPath, "decision-delegating execution language is not allowed"))
			}
			continue
		}
		if phrase, ok := containsVagueQualityPhrase(item.text); ok {
			signalCount := groundingSignalCount(item.text, exec, item.fieldPath)
			if item.structuredScope && hasStructuredPayloadGrounding(exec) {
				signalCount = 2
			}
			if signalCount < 2 {
				issues = append(issues, addVagueIntentIssue(phrase, item.fieldPath, "fewer than two concrete grounding signals are present"))
			}
		}
	}

	// 2. User-facing workflow validation: check for frontend file targets
	var targetExecutor string
	if meta, ok := packet["packet_meta"].(map[string]interface{}); ok {
		if te, ok := meta["target_executor"].(string); ok {
			targetExecutor = te
		}
	}

	if targetExecutor == "deepseek-v4-flash" {
		if requiresFrontendTarget(exec, packet) {
			issues = append(issues, ValidationError{Type: "input", Code: CodeFileTargetMismatch, Message: "user-facing workflow requested but no frontend file targets specified (and backend-only sufficiency was not explicitly decided)", RepairEligible: false})
		}
	}

	// 3. Inspect action step validation
	if steps, ok := exec["implementation_steps"].([]interface{}); ok {
		for _, s := range steps {
			if stepObj, ok := s.(map[string]interface{}); ok {
				action, _ := stepObj["action"].(string)
				if action == "inspect" {
					tPaths, ok := stepObj["target_paths"].([]interface{})
					if !ok || len(tPaths) == 0 {
						issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_INVALID_STEP", Message: "inspect step action requires non-empty target_paths", RepairEligible: false})
					}
					fieldPath := stepFieldPath(stepObj, "implementation_steps")
					title, _ := stepObj["title"].(string)
					insts, _ := stepObj["instructions"].(string)
					criteriaText := strings.Join(stringSliceFromInterface(stepObj["acceptance_criteria"]), " ")
					textToCheck := strings.Join([]string{title, insts, criteriaText}, " ")
					if match, found := findDecisionDelegatingPhrase(textToCheck); found && !isProhibitedExampleContext(textToCheck, match.start, match.end) {
						issues = append(issues, addVagueIntentIssue(match.phrase, fieldPath, "inspect step contains decision-delegating execution language"))
					}
				}
			}
		}
	}

	return issues
}

func collectExecutionTextItems(exec map[string]interface{}) []executionTextItem {
	items := []executionTextItem{
		{fieldPath: "execution_payload.goal", text: stringFromInterface(exec["goal"]), structuredScope: true},
		{fieldPath: "execution_payload.scope", text: stringFromInterface(exec["scope"]), structuredScope: true},
	}

	if steps, ok := exec["implementation_steps"].([]interface{}); ok {
		for i, step := range steps {
			stepObj, ok := step.(map[string]interface{})
			if !ok {
				continue
			}
			parts := []string{
				stringFromInterface(stepObj["title"]),
				stringFromInterface(stepObj["action"]),
				stringFromInterface(stepObj["instructions"]),
				strings.Join(stringSliceFromInterface(stepObj["acceptance_criteria"]), " "),
				strings.Join(stringSliceFromInterface(stepObj["target_paths"]), " "),
			}
			items = append(items, executionTextItem{
				fieldPath: stepFieldPath(stepObj, fmt.Sprintf("execution_payload.implementation_steps[%d]", i)),
				text:      strings.Join(parts, " "),
			})
		}
	}

	if reqs, ok := exec["code_requirements"].([]interface{}); ok {
		for i, req := range reqs {
			if reqObj, ok := req.(map[string]interface{}); ok {
				fieldPath := fmt.Sprintf("execution_payload.code_requirements[%d].requirement", i)
				if id, ok := reqObj["id"].(string); ok && strings.TrimSpace(id) != "" {
					fieldPath = fmt.Sprintf("execution_payload.code_requirements[%s].requirement", id)
				}
				items = append(items, executionTextItem{fieldPath: fieldPath, text: stringFromInterface(reqObj["requirement"])})
			} else if text, ok := req.(string); ok {
				items = append(items, executionTextItem{fieldPath: fmt.Sprintf("execution_payload.code_requirements[%d]", i), text: text})
			}
		}
	}

	if expected, ok := exec["expected_behavior"].([]interface{}); ok {
		for i, behavior := range expected {
			items = append(items, executionTextItem{fieldPath: fmt.Sprintf("execution_payload.expected_behavior[%d]", i), text: stringFromInterface(behavior)})
		}
	}

	if contract, ok := exec["completion_contract"].(map[string]interface{}); ok {
		if doneWhen, ok := contract["done_when"].([]interface{}); ok {
			for i, done := range doneWhen {
				items = append(items, executionTextItem{fieldPath: fmt.Sprintf("execution_payload.completion_contract.done_when[%d]", i), text: stringFromInterface(done)})
			}
		}
	}

	return items
}

func containsVagueQualityPhrase(text string) (string, bool) {
	return containsPhrase(text, []string{
		"improve",
		"enhance",
		"make better",
		"clean up",
		"polish",
		"refine",
		"optimize",
		"streamline",
		"make it work",
	})
}

func containsDecisionDelegatingPhrase(text string) (string, bool) {
	match, ok := findDecisionDelegatingPhrase(text)
	return match.phrase, ok
}

func findDecisionDelegatingPhrase(text string) (phraseMatch, bool) {
	return findPhrase(text, []string{
		"decide best approach",
		"wire as needed",
		"inspect and decide",
		"choose appropriate behavior",
		"if appropriate",
		"maybe",
		"optional enhancement",
		strings.Join([]string{"determine", "whether"}, " "),
	})
}

func containsPhrase(text string, phrases []string) (string, bool) {
	match, ok := findPhrase(text, phrases)
	return match.phrase, ok
}

func findPhrase(text string, phrases []string) (phraseMatch, bool) {
	lower := strings.ToLower(text)
	for _, phrase := range phrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return phraseMatch{phrase: phrase, start: idx, end: idx + len(phrase)}, true
		}
	}
	return phraseMatch{}, false
}

func isProhibitedExampleContext(text string, matchStart int, matchEnd int) bool {
	if matchStart < 0 || matchEnd > len(text) || matchStart >= matchEnd {
		return false
	}
	lineStart := strings.LastIndex(text[:matchStart], "\n") + 1
	lineEndOffset := strings.Index(text[matchEnd:], "\n")
	lineEnd := len(text)
	if lineEndOffset >= 0 {
		lineEnd = matchEnd + lineEndOffset
	}

	currentLine := strings.ToLower(text[lineStart:lineEnd])
	localContext := currentLine
	if heading := nearestLocalHeadingOrLabel(text[:lineStart]); heading != "" {
		localContext = heading + " " + localContext
	}

	hasInvalidCue := containsAny(localContext, []string{
		"prohibited", "disallowed", "invalid", "banned", "forbidden", "not allowed", "do not use", "must not use", "reject", "blocked",
	})
	hasExampleCue := containsAny(localContext, []string{
		"example", "examples", "sample", "samples", "fixture", "fixtures", "anti-pattern", "anti-patterns",
	})
	hasLiteralCue := strings.Contains(currentLine, "`") || strings.Contains(currentLine, `"`) || strings.Contains(currentLine, "'")

	return hasInvalidCue && (hasExampleCue || hasLiteralCue)
}

func nearestLocalHeadingOrLabel(prefix string) string {
	lines := strings.Split(prefix, "\n")
	checked := 0
	for i := len(lines) - 1; i >= 0 && checked < 6; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		checked++
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "#") || strings.HasSuffix(line, ":") || strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") ||
			containsAny(lower, []string{"example", "sample", "fixture", "prohibited", "disallowed", "invalid", "banned", "forbidden"}) {
			return lower
		}
	}
	return ""
}

func requiresFrontendTarget(exec map[string]interface{}, packet map[string]interface{}) bool {
	if hasBackendOnlyDecision(exec, packet) {
		return false
	}
	if docsOrPolicyOnlyTargets(exec) && !coreIntentRequiresVisibleUI(exec) {
		return false
	}
	return coreIntentRequiresVisibleUI(exec) && !hasFrontendFileTarget(exec)
}

func hasBackendOnlyDecision(exec map[string]interface{}, packet map[string]interface{}) bool {
	payloadBytes, _ := json.Marshal(exec)
	if strings.Contains(strings.ToLower(string(payloadBytes)), "backend-only") {
		return true
	}
	if plannerCtx, ok := packet["planner_context"].(map[string]interface{}); ok {
		plannerCtxBytes, _ := json.Marshal(plannerCtx)
		return strings.Contains(strings.ToLower(string(plannerCtxBytes)), "backend-only")
	}
	return false
}

func coreIntentRequiresVisibleUI(exec map[string]interface{}) bool {
	parts := []string{
		stringFromInterface(exec["goal"]),
		stringFromInterface(exec["scope"]),
	}
	if expected, ok := exec["expected_behavior"].([]interface{}); ok {
		for _, behavior := range expected {
			parts = append(parts, stringFromInterface(behavior))
		}
	}
	text := strings.ToLower(strings.Join(parts, "\n"))
	visibleUIRegex := regexp.MustCompile(`\b(ui|frontend|view|page|user-facing|render|display)\b`)
	return visibleUIRegex.MatchString(text) || regexp.MustCompile(`\bvisible\s+(app\s+)?(behavior|workflow|surface)\b`).MatchString(text)
}

func docsOrPolicyOnlyTargets(exec map[string]interface{}) bool {
	paths := fileTargetPaths(exec)
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !isDocsOrPolicyArtifactPath(path) {
			return false
		}
	}
	return true
}

func isDocsOrPolicyArtifactPath(path string) bool {
	lowerPath := strings.TrimPrefix(strings.ToLower(filepath.ToSlash(path)), "./")
	if strings.HasPrefix(lowerPath, "docs/") ||
		strings.HasPrefix(lowerPath, "relay-contracts/policies/") ||
		strings.HasPrefix(lowerPath, "relay-contracts/contracts/") ||
		strings.HasPrefix(lowerPath, "relay-contracts/schema/") ||
		strings.HasPrefix(lowerPath, "relay-contracts/templates/") ||
		strings.HasPrefix(lowerPath, "relay-contracts/examples/") ||
		strings.HasPrefix(lowerPath, "relay-contracts/agents/") {
		return true
	}
	return strings.HasSuffix(lowerPath, ".md")
}

func hasFrontendFileTarget(exec map[string]interface{}) bool {
	for _, path := range fileTargetPaths(exec) {
		lowerPath := strings.ToLower(filepath.ToSlash(path))
		if strings.HasSuffix(lowerPath, ".templ") ||
			strings.HasSuffix(lowerPath, ".ts") ||
			strings.HasSuffix(lowerPath, ".tsx") ||
			strings.HasSuffix(lowerPath, ".js") ||
			strings.HasSuffix(lowerPath, ".jsx") ||
			strings.HasSuffix(lowerPath, ".html") ||
			strings.HasSuffix(lowerPath, ".gohtml") ||
			strings.HasSuffix(lowerPath, ".css") ||
			strings.Contains(lowerPath, "web/") ||
			strings.Contains(lowerPath, "templates/") {
			return true
		}
	}
	return false
}

func fileTargetPaths(exec map[string]interface{}) []string {
	var paths []string
	targets, ok := exec["file_targets"].([]interface{})
	if !ok {
		return paths
	}
	for _, target := range targets {
		if path, ok := target.(string); ok {
			paths = append(paths, path)
			continue
		}
		targetObj, ok := target.(map[string]interface{})
		if !ok {
			continue
		}
		if path, ok := targetObj["path"].(string); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func groundingSignalCount(text string, exec map[string]interface{}, fieldPath string) int {
	lower := strings.ToLower(text)
	count := 0

	if hasTargetOrSymbolSignal(text) {
		count++
	}
	if hasBoundedActionSignal(lower) {
		count++
	}
	if hasAcceptanceSignal(lower) {
		count++
	}
	if hasValidationSignal(lower) {
		count++
	}

	return count
}

func hasStructuredPayloadGrounding(exec map[string]interface{}) bool {
	return nonEmptyArray(exec["file_targets"]) &&
		hasGroundedImplementationStep(exec) &&
		hasGroundedCodeRequirement(exec) &&
		hasValidationCommands(exec)
}

func addVagueIntentIssue(phrase string, fieldPath string, reason string) ValidationError {
	return ValidationError{
		Type:           "input",
		Code:           "CANONICAL_PACKET_VAGUE_INTENT",
		Message:        fmt.Sprintf("vague or decision-delegating phrase %q in %s: %s", phrase, fieldPath, reason),
		RepairEligible: false,
	}
}

func hasTargetOrSymbolSignal(text string) bool {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b[\w./-]+\.(go|ts|tsx|js|jsx|css|html|templ|json|md|sql|yaml|yml)\b`),
		regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)?\b`),
		regexp.MustCompile(`\bCANONICAL_PACKET_[A-Z0-9_]+\b`),
		regexp.MustCompile(`\bFS-[A-Z0-9_-]+\b`),
		regexp.MustCompile(`\bV[0-9]+\b`),
	}
	for _, pattern := range patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func hasBoundedActionSignal(lower string) bool {
	return containsAnyWord(lower, []string{
		"replace", "remove", "parse", "classify", "allow", "reject", "return", "emit", "map", "preserve", "skip", "exclude", "include", "link", "mark", "block", "pass", "fail", "normalize", "validate", "verify",
	})
}

func hasAcceptanceSignal(lower string) bool {
	for _, cue := range []string{
		"returns", "emits", "contains", "does not contain", "passes when", "fails when", "exit code", "status", "accepted_with_warnings", "canonical_packet_vague_intent", "fs-targets",
	} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func hasValidationSignal(lower string) bool {
	if strings.Contains(lower, "go test") || strings.Contains(lower, "npm run") || strings.Contains(lower, "make ") {
		return true
	}
	return regexp.MustCompile(`\btest[A-Za-z0-9_]*\b`).MatchString(lower)
}

func containsAnyWord(lower string, words []string) bool {
	for _, word := range words {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(lower) {
			return true
		}
	}
	return false
}

func hasValidationCommands(exec map[string]interface{}) bool {
	contract, ok := exec["validation_contract"].(map[string]interface{})
	if !ok {
		return false
	}
	return nonEmptyArray(contract["commands"])
}

func hasGroundedImplementationStep(exec map[string]interface{}) bool {
	steps, ok := exec["implementation_steps"].([]interface{})
	if !ok {
		return false
	}
	for _, step := range steps {
		stepObj, ok := step.(map[string]interface{})
		if !ok {
			continue
		}
		text := strings.Join([]string{
			stringFromInterface(stepObj["title"]),
			stringFromInterface(stepObj["action"]),
			stringFromInterface(stepObj["instructions"]),
			strings.Join(stringSliceFromInterface(stepObj["acceptance_criteria"]), " "),
			strings.Join(stringSliceFromInterface(stepObj["target_paths"]), " "),
		}, " ")
		if groundingSignalCount(text, exec, "execution_payload.implementation_steps") >= 2 {
			return true
		}
	}
	return false
}

func hasGroundedCodeRequirement(exec map[string]interface{}) bool {
	reqs, ok := exec["code_requirements"].([]interface{})
	if !ok {
		return false
	}
	for _, req := range reqs {
		reqObj, ok := req.(map[string]interface{})
		if !ok {
			continue
		}
		text := strings.Join([]string{
			stringFromInterface(reqObj["requirement"]),
			strings.Join(stringSliceFromInterface(reqObj["applies_to"]), " "),
		}, " ")
		if groundingSignalCount(text, exec, "execution_payload.code_requirements") >= 2 {
			return true
		}
	}
	return false
}

func nonEmptyArray(value interface{}) bool {
	arr, ok := value.([]interface{})
	return ok && len(arr) > 0
}

func stringFromInterface(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func stringSliceFromInterface(value interface{}) []string {
	var out []string
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, v...)
	}
	return out
}

func stepFieldPath(stepObj map[string]interface{}, fallback string) string {
	if id, ok := stepObj["id"].(string); ok && strings.TrimSpace(id) != "" {
		return fmt.Sprintf("execution_payload.implementation_steps[%s]", id)
	}
	return fallback
}

func checkBlockingUnresolvedQuestions(packet map[string]interface{}) []ValidationError {
	var issues []ValidationError
	if plannerCtx, ok := packet["planner_context"].(map[string]interface{}); ok {
		if questions, ok := plannerCtx["unresolved_questions"].([]interface{}); ok {
			for _, q := range questions {
				if qObj, ok := q.(map[string]interface{}); ok {
					if blocking, ok := qObj["blocking"].(bool); ok && blocking {
						issues = append(issues, ValidationError{
							Type:           "input",
							Code:           CodeBlockingUnresolvedQuestion,
							Message:        "packet contains a blocking unresolved question, which stops execution",
							RepairEligible: false,
						})
					}
				}
			}
		}
	}
	return issues
}

func locateSchemaFile(p string) string {
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

func sanitizeSchemaRegexes(schemaContent string) string {
	// Remove lookaheads: (?!/) and other lookaheads that Go's RE2 doesn't support
	schemaContent = strings.ReplaceAll(schemaContent, `(?!/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(^|/)\\.\\.($|/))`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*\\\\)`, "")
	return schemaContent
}

func mapSchemaErrorType(desc gojsonschema.ResultError) string {
	t := desc.Type()
	switch t {
	case "required":
		if prop, ok := desc.Details()["property"].(string); ok {
			switch prop {
			case "implementation_steps":
				return CodeMissingImplementationSteps
			case "code_requirements":
				return CodeMissingCodeRequirements
			case "validation_contract":
				return CodeMissingValidationContract
			case "pass_exit_evidence":
				return CodeMissingPassExitEvidence
			}
		}
		return CodeMissingRequiredField
	case "enum":
		return CodeInvalidEnum
	case "additional_property", "additional_properties":
		return CodeExtraProperty
	case "type", "invalid_type":
		return CodeInvalidType
	case "pattern":
		return CodeStringPatternMismatch
	default:
		return "SCHEMA_VALIDATION_FAILED"
	}
}

func reportIsRepairEligible(errors []ValidationError) bool {
	if len(errors) == 0 {
		return false
	}
	for _, err := range errors {
		if err.Code == "" {
			return false
		}
		if !isRepairableValidationCode(err.Code) {
			return false
		}
		if !err.RepairEligible {
			return false
		}
	}
	return true
}

func isRepairableValidationCode(code string) bool {
	switch code {
	case CodeJSONSyntax, CodeMissingRequiredField, CodeInvalidEnum, CodeExtraProperty, CodeInvalidType, CodeStringPatternMismatch:
		return true
	case CodeMissingImplementationSteps, CodeMissingCodeRequirements, CodeMissingPassExitEvidence:
		return true
	default:
		return false
	}
}

func RedactSecrets(s string) string {
	for _, re := range secretRegexes {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	if strings.Contains(s, "-----BEGIN") {
		re := regexp.MustCompile(`(?s)-----BEGIN.*?-----END.*?-----`)
		s = re.ReplaceAllString(s, "[REDACTED KEY]")
	}
	return s
}

// ValidateReportJSON validates a validation report JSON against its schema and enforces failure taxonomy.
func ValidateReportJSON(reportJSON []byte, schemaPath string, taxonomyPath string) (bool, error) {
	schemaLoader := gojsonschema.NewReferenceLoader("file:///" + filepath.ToSlash(locateSchemaFile(schemaPath)))
	// We need to clean schema regexes because gojsonschema uses RE2
	schemaBytes, err := os.ReadFile(locateSchemaFile(schemaPath))
	if err == nil {
		clean := sanitizeSchemaRegexes(string(schemaBytes))
		schemaLoader = gojsonschema.NewStringLoader(clean)
	}

	documentLoader := gojsonschema.NewBytesLoader(reportJSON)
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return false, fmt.Errorf("schema validation error: %v", err)
	}
	if !result.Valid() {
		return false, nil
	}

	taxBytes, err := os.ReadFile(locateSchemaFile(taxonomyPath))
	if err != nil {
		return false, fmt.Errorf("failed to read taxonomy: %v", err)
	}
	var tax map[string]interface{}
	if err := json.Unmarshal(taxBytes, &tax); err != nil {
		return false, fmt.Errorf("failed to unmarshal taxonomy: %v", err)
	}

	codesList, ok := tax["codes"].([]interface{})
	if !ok {
		return false, fmt.Errorf("taxonomy codes not found")
	}
	validCodes := make(map[string]bool)
	for _, c := range codesList {
		if codeObj, ok := c.(map[string]interface{}); ok {
			if codeStr, ok := codeObj["code"].(string); ok {
				validCodes[codeStr] = true
			}
		}
	}

	var report ValidationReport
	if err := json.Unmarshal(reportJSON, &report); err != nil {
		return false, fmt.Errorf("failed to parse report: %v", err)
	}

	for _, e := range report.Errors {
		if e.Code == "" {
			return false, nil
		}
		if !validCodes[e.Code] {
			return false, nil
		}
	}

	if report.RepairEligible {
		for _, e := range report.Errors {
			if !e.RepairEligible {
				return false, nil
			}
		}
	}
	return true, nil
}

// ValidatePlannerHandoffJSON validates a planner handoff metadata JSON against its schema.
func ValidatePlannerHandoffJSON(handoffJSON []byte, schemaPath string) (bool, error) {
	schemaBytes, err := os.ReadFile(locateSchemaFile(schemaPath))
	var schemaLoader gojsonschema.JSONLoader
	if err == nil {
		clean := sanitizeSchemaRegexes(string(schemaBytes))
		schemaLoader = gojsonschema.NewStringLoader(clean)
	} else {
		schemaLoader = gojsonschema.NewReferenceLoader("file:///" + filepath.ToSlash(locateSchemaFile(schemaPath)))
	}

	documentLoader := gojsonschema.NewBytesLoader(handoffJSON)
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return false, fmt.Errorf("schema validation error: %v", err)
	}
	return result.Valid(), nil
}
