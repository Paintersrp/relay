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
	CodeStringPatternMismatch                 = "CANONICAL_PACKET_STRING_PATTERN_MISMATCH"
	CodeFileTargetMismatch                    = "CANONICAL_PACKET_FILE_TARGET_MISMATCH"
	CodeBlockingUnresolvedQuestion            = "CANONICAL_PACKET_BLOCKING_UNRESOLVED_QUESTION"
	CodeMissingImplementationSteps            = "CANONICAL_PACKET_MISSING_IMPLEMENTATION_STEPS"
	CodeMissingCodeRequirements               = "CANONICAL_PACKET_MISSING_CODE_REQUIREMENTS"
	CodeMissingPassExitEvidence               = "CANONICAL_PACKET_MISSING_PASS_EXIT_EVIDENCE"
	CodeMissingValidationContract             = "CANONICAL_PACKET_MISSING_VALIDATION_CONTRACT"
)

type ValidationReport struct {
	Valid          bool              `json:"valid"`
	RepairEligible bool              `json:"repair_eligible"`
	Errors         []ValidationError `json:"errors"`
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
				Code:           mapSchemaErrorType(desc.Type()),
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
			issues = append(issues, ValidationError{Type: "input", Code: CodeMissingRequiredField, Message: fmt.Sprintf("required execution_payload field %q is missing", f), RepairEligible: false})
			continue
		}
		arrVal, ok := val.([]interface{})
		if !ok || len(arrVal) == 0 {
			issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: fmt.Sprintf("required execution_payload field %q is empty", f), RepairEligible: false})
		}
	}

	if val, ok := exec["validation_contract"]; !ok {
		issues = append(issues, ValidationError{Type: "input", Code: CodeMissingRequiredField, Message: "required execution_payload field \"validation_contract\" is missing", RepairEligible: false})
	} else if obj, ok := val.(map[string]interface{}); !ok || len(obj) == 0 {
		issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_EMPTY_FIELD", Message: "required execution_payload field \"validation_contract\" is empty", RepairEligible: false})
	}

	// 1. Scan for vague phrases in execution_payload
	bannedPhrases := []string{
		"decide best approach",
		"determine whether",
		"improve",
		"make it work",
		"wire as needed",
		"inspect and decide",
		"choose appropriate behavior",
		"if appropriate",
		"maybe",
		"optional enhancement",
	}

	var scanText func(val interface{})
	scanText = func(val interface{}) {
		switch v := val.(type) {
		case string:
			lower := strings.ToLower(v)
			for _, phrase := range bannedPhrases {
				if strings.Contains(lower, phrase) {
					issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_VAGUE_INTENT", Message: fmt.Sprintf("vague or decision-delegating phrase %q detected in execution_payload", phrase), RepairEligible: false})
				}
			}
		case map[string]interface{}:
			for _, val := range v {
				scanText(val)
			}
		case []interface{}:
			for _, item := range v {
				scanText(item)
			}
		}
	}
	scanText(exec)

	// 2. User-facing workflow validation: check for frontend file targets
	var targetExecutor string
	if meta, ok := packet["packet_meta"].(map[string]interface{}); ok {
		if te, ok := meta["target_executor"].(string); ok {
			targetExecutor = te
		}
	}

	if targetExecutor == "deepseek-v4-flash" {
		payloadBytes, _ := json.Marshal(exec)
		payloadStr := strings.ToLower(string(payloadBytes))

		userFacingRegex := regexp.MustCompile(`(?i)\b(ui|frontend|view|page|user-facing|workflow|render|display)\b`)
		hasUserFacingTerm := userFacingRegex.Match(payloadBytes)

		if hasUserFacingTerm {
			hasBackendOnlyDecision := false
			if plannerCtx, ok := packet["planner_context"].(map[string]interface{}); ok {
				plannerCtxBytes, _ := json.Marshal(plannerCtx)
				if strings.Contains(strings.ToLower(string(plannerCtxBytes)), "backend-only") {
					hasBackendOnlyDecision = true
				}
			}
			if strings.Contains(payloadStr, "backend-only") {
				hasBackendOnlyDecision = true
			}

			if !hasBackendOnlyDecision {
				hasFrontendFile := false
				if targets, ok := exec["file_targets"].([]interface{}); ok {
					for _, t := range targets {
						var path string
						if pathStr, ok := t.(string); ok {
							path = pathStr
						} else if targetObj, ok := t.(map[string]interface{}); ok {
							if pathStr, ok := targetObj["path"].(string); ok {
								path = pathStr
							}
						}
						lowerPath := strings.ToLower(path)
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
							hasFrontendFile = true
							break
						}
					}
				}

				if !hasFrontendFile {
					issues = append(issues, ValidationError{Type: "input", Code: CodeFileTargetMismatch, Message: "user-facing workflow requested but no frontend file targets specified (and backend-only sufficiency was not explicitly decided)", RepairEligible: false})
				}
			}
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
					title, _ := stepObj["title"].(string)
					insts, _ := stepObj["instructions"].(string)
					textToCheck := strings.ToLower(title + " " + insts)
					if strings.Contains(textToCheck, "decide") ||
						strings.Contains(textToCheck, "determine") ||
						strings.Contains(textToCheck, "choose") ||
						strings.Contains(textToCheck, "whether") {
						issues = append(issues, ValidationError{Type: "input", Code: "CANONICAL_PACKET_VAGUE_INTENT", Message: "inspect step instructions/title contain decision words delegating reasoning", RepairEligible: false})
					}
				}
			}
		}
	}

	return issues
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

func mapSchemaErrorType(t string) string {
	switch t {
	case "required":
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
