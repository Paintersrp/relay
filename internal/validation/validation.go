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
	Message        string `json:"message"`
	RepairEligible bool   `json:"repair_eligible"`
}

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
			report.Errors = append(report.Errors, ValidationError{
				Type:           "security",
				Message:        issue,
				RepairEligible: false,
			})
		}
	}

	// Safe path checks (CR5, S6)
	pathIssues := checkPaths(packetMap)
	if len(pathIssues) > 0 {
		report.Valid = false
		report.RepairEligible = false // Path traversal/absolute paths are intent/boundary violations, not repairable formatting issues
		for _, issue := range pathIssues {
			report.Errors = append(report.Errors, ValidationError{
				Type:           "path",
				Message:        issue,
				RepairEligible: false,
			})
		}
	}

	// Required payload fields checks
	payloadIssues := checkRequiredPayloadFields(packetMap)
	if len(payloadIssues) > 0 {
		report.Valid = false
		report.RepairEligible = false // Missing product behavior or goals is not format repairable
		for _, issue := range payloadIssues {
			report.Errors = append(report.Errors, ValidationError{
				Type:           "input",
				Message:        issue,
				RepairEligible: false,
			})
		}
	}

	return report, nil
}

func scanForSecrets(val interface{}) []string {
	var issues []string
	switch v := val.(type) {
	case string:
		if HasSecret(v) {
			issues = append(issues, fmt.Sprintf("secret-like value detected in string: %s", truncateString(v, 30)))
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

func checkPaths(packet map[string]interface{}) []string {
	var issues []string

	// Check file_targets in execution_payload
	if exec, ok := packet["execution_payload"].(map[string]interface{}); ok {
		if targets, ok := exec["file_targets"].([]interface{}); ok {
			for _, t := range targets {
				if pathStr, ok := t.(string); ok {
					if err := validatePathSafety(pathStr); err != nil {
						issues = append(issues, fmt.Sprintf("unsafe file target: %s (%v)", pathStr, err))
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
						issues = append(issues, fmt.Sprintf("unsafe artifact path for %s: %s (%v)", k, pathStr, err))
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

func checkRequiredPayloadFields(packet map[string]interface{}) []string {
	var issues []string

	exec, ok := packet["execution_payload"].(map[string]interface{})
	if !ok {
		return []string{"execution_payload is missing or invalid"}
	}

	requiredFields := []string{
		"goal",
		"scope",
		"executor_final_response_format",
	}

	for _, f := range requiredFields {
		val, ok := exec[f]
		if !ok {
			issues = append(issues, fmt.Sprintf("required execution_payload field %q is missing", f))
			continue
		}
		strVal, ok := val.(string)
		if !ok || strings.TrimSpace(strVal) == "" {
			issues = append(issues, fmt.Sprintf("required execution_payload field %q is empty", f))
		}
	}

	// Validate expected_behavior (should be non-empty array of strings)
	if val, ok := exec["expected_behavior"]; !ok {
		issues = append(issues, "required execution_payload field \"expected_behavior\" is missing")
	} else if arr, ok := val.([]interface{}); !ok || len(arr) == 0 {
		issues = append(issues, "required execution_payload field \"expected_behavior\" is empty")
	}

	// Validate completion_contract (should be object)
	if val, ok := exec["completion_contract"]; !ok {
		issues = append(issues, "required execution_payload field \"completion_contract\" is missing")
	} else if obj, ok := val.(map[string]interface{}); !ok || len(obj) == 0 {
		issues = append(issues, "required execution_payload field \"completion_contract\" is empty")
	}

	// Check arrays are not empty
	requiredArrays := []string{
		"non_goals",
		"file_targets",
		"implementation_steps",
		"code_requirements",
		"validation_commands",
	}

	for _, f := range requiredArrays {
		val, ok := exec[f]
		if !ok {
			issues = append(issues, fmt.Sprintf("required execution_payload field %q is missing", f))
			continue
		}
		arrVal, ok := val.([]interface{})
		if !ok || len(arrVal) == 0 {
			issues = append(issues, fmt.Sprintf("required execution_payload field %q is empty", f))
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
