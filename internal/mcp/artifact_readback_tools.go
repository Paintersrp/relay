package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"relay/internal/artifacts"
)

const (
	BlockerUnsafeRequest          = "unsafe_request"
	BlockerUnknownRun             = "unknown_run"
	BlockerArtifactKindNotAllowed = "artifact_kind_not_allowed"
	BlockerArtifactNotFound       = "artifact_not_found"
	BlockerUnsafeArtifactPath     = "unsafe_artifact_path"
	BlockerArtifactOversized      = "artifact_oversized"
	BlockerArtifactBinary         = "artifact_binary_or_unsupported"
	BlockerArtifactReadFailed     = "artifact_read_failed"
	BlockerRedactionBlocked       = "artifact_redaction_blocked"
)

const (
	ViewModeMetadataOnly   = "metadata_only"
	ViewModeSummary        = "summary"
	ViewModeErrors         = "errors"
	ViewModeBoundedExcerpt = "bounded_excerpt"
)

const (
	HashStatusComputedFull     = "computed_full"
	HashStatusOmittedByRequest = "omitted_by_request"
	HashStatusOmittedOversized = "omitted_oversized"
	HashStatusUnavailable      = "unavailable"
)

const (
	RedactionNotRequired = "not_required"
	RedactionRedacted    = "redacted"
	RedactionBlocked     = "blocked"
)

const (
	defaultMaxBytes          = 12000
	hardMaxBytes             = 65536
	maxErrorItems            = 100
	maxSummaryKeys           = 50
	maxSummaryStringLen      = 200
	maxExcerptLineCount      = 200
	nulByteProbeSize         = 512
	maxRedactionBytePreview  = 256
	oversizedContentHashSkip = 16 * 1024 * 1024
)

var readbackAllowedKinds = map[string]bool{
	"validation_run_json":                true,
	"validation_stdout":                  true,
	"validation_stderr":                  true,
	"validation_progress_json":           true,
	"validation_failure_acceptance_json": true,
	"packet_validation_report":           true,
	"brief_validation_report":            true,
	"repair_validation_report":           true,
	"intake_validation_report":           true,
	"canonical_packet":                   true,
	"executor_brief":                     true,
	"executor_result":                    true,
	"executor_stdout":                    true,
	"executor_stderr":                    true,
	"command_log":                        true,
	"audit_packet":                       true,
	"audit_input_summary":                true,
	"audit_evidence_manifest_json":       true,
	"audit_decision_json":                true,
	"audit_revision":                     true,
	"git_status_text":                    true,
	"git_diff_stat":                      true,
	"git_diff_name_status":               true,
	"git_diff_patch":                     true,
	"git_diff_numstat":                   true,
	"planner_handoff":                    true,
	"planner_handoff_provenance_json":    true,
	"parsed_frontmatter":                 true,
	"run_config":                         true,
	"opencode_stdout":                    true,
	"opencode_stderr":                    true,
	"opencode_combined_log":              true,
	"opencode_lifecycle_diagnostic_json": true,
	"codex_last_message":                 true,
	"context_packet_json":                true,
	"context_packet_markdown":            true,
	"context_coverage_report_json":       true,
}

var viewModes = map[string]bool{
	ViewModeMetadataOnly:   true,
	ViewModeSummary:        true,
	ViewModeErrors:         true,
	ViewModeBoundedExcerpt: true,
}

type getRunArtifactInput struct {
	RunID              string `json:"run_id"`
	ArtifactKind       string `json:"artifact_kind"`
	ViewMode           string `json:"view_mode"`
	MaxBytes           *int   `json:"max_bytes,omitempty"`
	IncludeContentHash *bool  `json:"include_content_hash,omitempty"`
}

type artifactReadbackResult struct {
	ArtifactID        int64  `json:"artifact_id"`
	Kind              string `json:"kind"`
	MimeType          string `json:"mime_type"`
	SizeBytes         int64  `json:"size_bytes"`
	CreatedAt         string `json:"created_at"`
	ContentHash       string `json:"content_hash,omitempty"`
	ContentHashStatus string `json:"content_hash_status"`
	ArtifactRef       string `json:"artifact_ref"`
}

type getRunArtifactBlockers struct {
	OK       bool              `json:"ok"`
	Tool     string            `json:"tool"`
	RunID    string            `json:"run_id"`
	Blockers []artifactBlocker `json:"blockers"`
}

type artifactBlocker struct {
	Code        string                   `json:"code"`
	Message     string                   `json:"message"`
	Recoverable bool                     `json:"recoverable"`
	Evidence    []artifactBlockerEvidence `json:"evidence"`
	NextActions []artifactNextAction      `json:"next_actions"`
}

type artifactBlockerEvidence struct {
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	Value string `json:"value,omitempty"`
}

type artifactNextAction struct {
	Action      string `json:"action"`
	Description string `json:"description"`
}

type getRunArtifactOutput struct {
	OK              bool                    `json:"ok"`
	Tool            string                  `json:"tool"`
	RunID           string                  `json:"run_id"`
	ArtifactKind    string                  `json:"artifact_kind"`
	ViewMode        string                  `json:"view_mode"`
	Artifact        *artifactReadbackResult `json:"artifact,omitempty"`
	Content         any                     `json:"content,omitempty"`
	RedactionStatus string                  `json:"redaction_status"`
	Truncated       bool                    `json:"truncated"`
	ReturnedBytes   int                     `json:"returned_bytes"`
	MaxBytes        int                     `json:"max_bytes"`
	Blockers        []artifactBlocker       `json:"blockers"`
	GeneratedAt     string                  `json:"generated_at"`
}

var getRunArtifactSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["run_id", "artifact_kind", "view_mode"],
  "properties": {
    "run_id": {
      "type": "string",
      "minLength": 1,
      "description": "Numeric Relay run identifier."
    },
    "artifact_kind": {
      "type": "string",
      "minLength": 1,
      "description": "Registered artifact kind. Must be an eligible readback kind."
    },
    "view_mode": {
      "type": "string",
      "enum": ["metadata_only", "summary", "errors", "bounded_excerpt"],
      "description": "View mode controlling content extraction and bounding."
    },
    "max_bytes": {
      "type": "integer",
      "minimum": 1,
      "maximum": 65536,
      "description": "Maximum bytes for content-returning modes. Default 12000, hard cap 65536."
    },
    "include_content_hash": {
      "type": "boolean",
      "description": "Include SHA-256 content hash in artifact metadata. Default true for content-returning modes."
    }
  }
}`)

var ToolGetRunArtifact = ToolDefinition{
	Name:        "get_run_artifact",
	Description: "Read bounded metadata, summaries, error diagnostics, or excerpts from registered run artifacts by run_id and artifact_kind. Does not accept arbitrary file paths, shell commands, or URL reads. All content is bounded and sensitivity-filtered.",
	InputSchema: getRunArtifactSchema,
}

var errPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(error|err|fail|failure|block|blocker|warn|warning|exception|panic|timeout|denied|reject|refuse|invalid|unsafe|unknown|not.?found|crash|abort|fatal|broken|missing|skip|skipped|deadline|exceeded|throttle|rate.?limit|locked|unauthorized|forbidden|unexpected|unsupported)\b`),
}

func isReadbackAllowedKind(kind string) bool {
	if !artifacts.IsAllowedKind(kind) {
		return false
	}
	return readbackAllowedKinds[kind]
}

func clampMaxBytes(raw *int) int {
	if raw == nil {
		return defaultMaxBytes
	}
	v := *raw
	if v < 1 {
		return 1
	}
	if v > hardMaxBytes {
		return hardMaxBytes
	}
	return v
}

func clampBool(b *bool, fallback bool) bool {
	if b == nil {
		return fallback
	}
	return *b
}

func isBinaryContent(data []byte, mimeType string) bool {
	if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "video/") || strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "application/octet-stream") {
		return true
	}
	if !utf8.Valid(data) {
		return true
	}
	probe := data
	if len(probe) > nulByteProbeSize {
		probe = probe[:nulByteProbeSize]
	}
	for _, b := range probe {
		if b == 0 {
			return true
		}
	}
	return false
}

func computeFullArtifactHash(path string, size int64, includeHash bool) (string, string, error) {
	if !includeHash {
		return "", HashStatusOmittedByRequest, nil
	}
	if size > oversizedContentHashSkip {
		return "", HashStatusOmittedOversized, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", HashStatusUnavailable, err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), HashStatusComputedFull, nil
}

func redactContent(data []byte) ([]byte, string) {
	if len(data) == 0 {
		return data, RedactionNotRequired
	}

	s := string(data)

	redacted := s
	hit := false

	redacted = redactPEMBlocks(redacted, &hit)
	redacted = redactBearerTokens(redacted, &hit)
	redacted = redactBase64SecretStrings(redacted, &hit)
	redacted = redactURLPasswords(redacted, &hit)
	redacted = redactKeyValueSecrets(redacted, &hit)
	redacted = redactCredentialsJSONLines(redacted, &hit)

	if hit {
		confirm := []byte(redacted)
		if !utf8.Valid(confirm) {
			return nil, RedactionBlocked
		}
		if isBinaryContent(confirm, "") {
			return nil, RedactionBlocked
		}
		if len([]byte(redacted)) > len(data)*2+1024 {
			return nil, RedactionBlocked
		}
		return confirm, RedactionRedacted
	}

	return data, RedactionNotRequired
}

var (
	rePEM        = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----\n(?:[A-Za-z0-9+/=\n\r]{40,}\n)?-----END [A-Z0-9 ]+-----`)
	reBearer     = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+/=]{20,}`)
	reBase64Long = regexp.MustCompile(`[A-Za-z0-9+/=]{40,}`)
	reURLPass    = regexp.MustCompile(`(?i)(https?://)([^:@\s]+):([^@\s]+)@`)
	reKVSecret   = regexp.MustCompile(`(?i)(password|passwd|secret|token|key|apikey|api_key|auth)\s*[:=]\s*\S+`)
	reJSONSecret = regexp.MustCompile(`"(password|passwd|secret|token|key|apikey|api_key|auth)":\s*"([^"\\]|\\.)*"`)
)

func redactPEMBlocks(s string, hit *bool) string {
	if rePEM.MatchString(s) {
		*hit = true
		s = rePEM.ReplaceAllString(s, "[REDACTED_PEM_BLOCK]")
		if !utf8.ValidString(s) {
			return s
		}
	}
	return s
}

func redactBearerTokens(s string, hit *bool) string {
	if reBearer.MatchString(s) {
		*hit = true
		s = reBearer.ReplaceAllString(s, "[REDACTED_BEARER_TOKEN]")
		if !utf8.ValidString(s) {
			return s
		}
	}
	return s
}

func redactBase64SecretStrings(s string, hit *bool) string {
	if reBase64Long.MatchString(s) {
		*hit = true
		s = reBase64Long.ReplaceAllStringFunc(s, func(match string) string {
			return "[REDACTED_BASE64]"
		})
		if !utf8.ValidString(s) {
			return s
		}
	}
	return s
}

func redactURLPasswords(s string, hit *bool) string {
	if reURLPass.MatchString(s) {
		*hit = true
		s = reURLPass.ReplaceAllString(s, "$1$2:[REDACTED_URL_PASSWORD]@")
		if !utf8.ValidString(s) {
			return s
		}
	}
	return s
}

func redactKeyValueSecrets(s string, hit *bool) string {
	if reKVSecret.MatchString(s) {
		*hit = true
		s = reKVSecret.ReplaceAllString(s, "$1=[REDACTED]")
		if !utf8.ValidString(s) {
			return s
		}
	}
	return s
}

func redactCredentialsJSONLines(s string, hit *bool) string {
	if reJSONSecret.MatchString(s) {
		*hit = true
		s = reJSONSecret.ReplaceAllString(s, `"$1":"[REDACTED]"`)
		if !utf8.ValidString(s) {
			return s
		}
	}
	return s
}

func validateArtifactPath(storedPath string, runID int64) (string, error) {
	storedPath = strings.TrimSpace(storedPath)
	if storedPath == "" {
		return "", fmt.Errorf("empty stored artifact path")
	}
	if filepath.IsAbs(storedPath) {
		if !artifacts.RunDirContains(runID, storedPath) {
			return "", fmt.Errorf("absolute path outside run artifact directory")
		}
		return storedPath, nil
	}
	cleaned := filepath.Clean(storedPath)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("path contains traversal element")
	}
	if strings.ContainsAny(cleaned, "\x00\r\n;&|$<>`") {
		return "", fmt.Errorf("path contains unsafe characters")
	}
	fullPath := filepath.Join(artifacts.Dir(runID), cleaned)
	if !artifacts.RunDirContains(runID, fullPath) {
		return "", fmt.Errorf("resolved path outside run artifact directory")
	}
	return fullPath, nil
}

func readAndStatArtifact(artifactPath string, maxBytes int) ([]byte, int64, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return nil, 0, err
	}
	size := info.Size()

	readLimit := int64(maxBytes)
	if readLimit > hardMaxBytes {
		readLimit = hardMaxBytes
	}

	var data []byte
	if size <= readLimit {
		data, err = os.ReadFile(artifactPath)
	} else {
		f, ferr := os.Open(artifactPath)
		if ferr != nil {
			return nil, size, ferr
		}
		defer f.Close()
		buf := make([]byte, readLimit)
		n, rerr := f.Read(buf)
		if rerr != nil && rerr.Error() != "EOF" {
			return nil, size, rerr
		}
		data = buf[:n]
	}
	if err != nil {
		return nil, size, err
	}

	return data, size, nil
}

func buildArtifactRef(runID int64, artifactKind string) string {
	return fmt.Sprintf("run:%d:%s", runID, artifactKind)
}

func buildMetadataResult(artifactID int64, size int64, mimeType, createdAt string, contentHash, hashStatus, ref string) artifactReadbackResult {
	return artifactReadbackResult{
		ArtifactID:        artifactID,
		Kind:              mimeType,
		SizeBytes:         size,
		CreatedAt:         createdAt,
		ContentHash:       contentHash,
		ContentHashStatus: hashStatus,
		ArtifactRef:       ref,
	}
}

func extractSummaryJSON(data []byte) string {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Sprintf("{\"error\":\"invalid JSON\",\"size_bytes\":%d}", len(data))
	}

	switch v := parsed.(type) {
	case map[string]any:
		return buildJSONObjectSummary(v)
	case []any:
		return buildJSONArraySummary(v)
	default:
		return fmt.Sprintf("{\"type\":\"%T\",\"size_bytes\":%d}", parsed, len(data))
	}
}

func buildJSONObjectSummary(obj map[string]any) string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	type summaryEntry struct {
		Key   string `json:"key"`
		Count int    `json:"count,omitempty"`
		Kind  string `json:"kind,omitempty"`
	}
	entries := make([]summaryEntry, 0, len(keys))
	for _, k := range keys {
		if len(entries) >= maxSummaryKeys {
			break
		}
		entry := summaryEntry{Key: k}
		switch val := obj[k].(type) {
		case []any:
			entry.Count = len(val)
			entry.Kind = "array"
		case map[string]any:
			entry.Kind = "object"
		case string:
			entry.Kind = "string"
		case float64:
			entry.Kind = "number"
		case bool:
			entry.Kind = "boolean"
		default:
			entry.Kind = fmt.Sprintf("%T", val)
		}
		entries = append(entries, entry)
	}

	payload := map[string]any{
		"top_level_keys": entries,
		"total_keys":     len(keys),
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func buildJSONArraySummary(arr []any) string {
	counts := make(map[string]int)
	for _, item := range arr {
		switch v := item.(type) {
		case map[string]any:
			counts["object"]++
		case string:
			counts["string"]++
		case float64:
			counts["number"]++
		case bool:
			counts["boolean"]++
		default:
			_ = v
			counts["other"]++
		}
	}
	b, _ := json.Marshal(map[string]any{
		"array_length":  len(arr),
		"element_kinds": counts,
	})
	return string(b)
}

func extractSummaryText(data []byte) []byte {
	s := string(data)
	lines := strings.Split(s, "\n")

	nonEmpty := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}

	var result strings.Builder

	for i, line := range nonEmpty {
		if i >= 5 {
			break
		}
		if strings.HasPrefix(line, "#") {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}

	if result.Len() == 0 && len(nonEmpty) > 0 {
		first := nonEmpty[0]
		if len(first) > maxSummaryStringLen {
			first = first[:maxSummaryStringLen] + "..."
		}
		result.WriteString(first)
	}

	if result.Len() == 0 {
		result.WriteString(fmt.Sprintf("(text artifact, %d bytes, %d lines)", len(s), len(lines)))
	}

	return []byte(result.String())
}

func extractErrorsJSON(data []byte, maxBytes int) ([]byte, bool) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return []byte(fmt.Sprintf(`{"error":"invalid JSON","message":%q}`, err.Error())), false
	}

	errors := findErrorNodes(parsed, maxBytes)
	if len(errors) == 0 {
		return []byte(`[]`), false
	}
	b, _ := json.Marshal(errors)
	return b, len(errors) >= maxErrorItems
}

func findErrorNodes(v any, remainingBytes int) []map[string]any {
	var results []map[string]any

	switch val := v.(type) {
	case map[string]any:
		for k, vv := range val {
			if matchErrorKey(k) {
				entry := map[string]any{"key": k}
				switch sv := vv.(type) {
				case string:
					entry["value"] = boundedString(sv, 500)
				case []any:
					entry["count"] = len(sv)
				default:
					j, _ := json.Marshal(vv)
					s := string(j)
					entry["value"] = boundedString(s, 500)
				}
				results = append(results, entry)
				remainingBytes -= 200
			}
			if remainingBytes <= 0 || len(results) >= maxErrorItems {
				return results
			}
			childResults := findErrorNodes(vv, remainingBytes)
			results = append(results, childResults...)
			remainingBytes -= len(childResults) * 200
			if remainingBytes <= 0 || len(results) >= maxErrorItems {
				break
			}
		}
	case []any:
		for _, item := range val {
			if remainingBytes <= 0 || len(results) >= maxErrorItems {
				break
			}
			childResults := findErrorNodes(item, remainingBytes)
			results = append(results, childResults...)
			remainingBytes -= len(childResults) * 200
		}
	case string:
		for _, re := range errPatterns {
			if re.MatchString(val) {
				results = append(results, map[string]any{
					"pattern": "error_match",
					"value":   boundedString(val, 500),
				})
				break
			}
		}
	}
	return results
}

func matchErrorKey(k string) bool {
	lower := strings.ToLower(k)
	errorKeys := []string{
		"error", "err", "fail", "failure", "blocker", "blockers",
		"warning", "warnings", "exception", "panic", "timeout",
		"denied", "reject", "invalid", "unsafe",
		"fatal", "broken", "missing", "not_found",
	}
	if lower == "status" {
		return false
	}
	for _, ek := range errorKeys {
		if lower == ek || strings.Contains(lower, ek) {
			return true
		}
	}
	return false
}

func extractErrorsText(data []byte, maxBytes int) ([]byte, bool) {
	lines := strings.Split(string(data), "\n")
	var matching []string

	for _, line := range lines {
		if len(matching) >= maxErrorItems {
			break
		}
		for _, re := range errPatterns {
			if re.MatchString(line) {
				matching = append(matching, boundedString(line, 500))
				break
			}
		}
	}

	if len(matching) == 0 {
		return []byte(`[]`), false
	}

	result := map[string]any{
		"status":         "errors_found",
		"error_lines":    matching,
		"total_lines":    len(lines),
		"matching_lines": len(matching),
	}
	b, _ := json.Marshal(result)
	return b, len(b) > maxBytes
}

func boundedString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func (s *Server) HandleGetRunArtifact(rawArgs json.RawMessage) ToolCallResult {
	if s.deps == nil || s.deps.Store == nil {
		return toolErr("DEPENDENCY_ERROR: MCP server is not connected to a Relay store")
	}

	var input getRunArtifactInput
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return buildArtifactBlockerResponse("", newArtifactBlocker(
			BlockerUnsafeRequest,
			"invalid or unsafe arguments: "+err.Error(),
			false,
			[]artifactBlockerEvidence{{Kind: "argument", Value: "request_payload"}},
			[]artifactNextAction{{Action: "retry_with_valid_arguments", Description: "retry with valid bounded arguments"}},
		))
	}

	runIDStr := strings.TrimSpace(input.RunID)
	if runIDStr == "" {
		return buildArtifactBlockerResponse("", newArtifactBlocker(
			BlockerUnsafeRequest,
			"run_id is required and must not be empty",
			false,
			[]artifactBlockerEvidence{{Kind: "argument", Value: "run_id"}},
			[]artifactNextAction{{Action: "retry_with_valid_arguments", Description: "retry with valid bounded arguments"}},
		))
	}

	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil || runID <= 0 {
		return buildArtifactBlockerResponse("", newArtifactBlocker(
			BlockerUnsafeRequest,
			fmt.Sprintf("run_id must be a positive integer, got %q", runIDStr),
			false,
			[]artifactBlockerEvidence{{Kind: "argument", Value: "run_id"}},
			[]artifactNextAction{{Action: "retry_with_valid_arguments", Description: "retry with valid bounded arguments"}},
		))
	}

	artifactKind := strings.TrimSpace(input.ArtifactKind)
	if artifactKind == "" {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerUnsafeRequest,
			"artifact_kind is required and must not be empty",
			false,
			[]artifactBlockerEvidence{{Kind: "argument", Value: "artifact_kind"}},
			[]artifactNextAction{{Action: "retry_with_valid_arguments", Description: "retry with valid bounded arguments"}},
		))
	}

	viewMode := strings.TrimSpace(input.ViewMode)
	if !viewModes[viewMode] {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerUnsafeRequest,
			fmt.Sprintf("view_mode must be one of: metadata_only, summary, errors, bounded_excerpt, got %q", viewMode),
			false,
			[]artifactBlockerEvidence{{Kind: "argument", Value: "view_mode"}},
			[]artifactNextAction{{Action: "retry_with_valid_arguments", Description: "retry with valid bounded arguments"}},
		))
	}

	if !isReadbackAllowedKind(artifactKind) {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerArtifactKindNotAllowed,
			fmt.Sprintf("artifact kind %q is not eligible for readback", artifactKind),
			true,
			[]artifactBlockerEvidence{{Kind: "artifact_kind", ID: artifactKind}},
			[]artifactNextAction{{Action: "select_allowed_kind", Description: "choose an eligible readback kind"}},
		))
	}

	maxBytes := clampMaxBytes(input.MaxBytes)
	includeHash := clampBool(input.IncludeContentHash, viewMode != ViewModeMetadataOnly)

	run, err := s.deps.Store.GetRun(runID)
	if err != nil {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerUnknownRun,
			fmt.Sprintf("run %d not found", runID),
			true,
			[]artifactBlockerEvidence{{Kind: "run_id", ID: runIDStr}},
			[]artifactNextAction{{Action: "verify_run_id", Description: "verify registered run ID"}},
		))
	}

	artifactsList, err := s.deps.Store.ListArtifactsByRunKind(runID, artifactKind)
	if err != nil || len(artifactsList) == 0 {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerArtifactNotFound,
			fmt.Sprintf("artifact kind %q not found for run %d", artifactKind, runID),
			true,
			[]artifactBlockerEvidence{{Kind: "run_id", ID: runIDStr}, {Kind: "artifact_kind", ID: artifactKind}},
			[]artifactNextAction{{Action: "request_registered_kind", Description: "request an existing registered artifact kind for that run"}},
		))
	}
	dbArtifact := artifactsList[0]

	if dbArtifact.RunID != runID {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerArtifactNotFound,
			"artifact run_id mismatch",
			true,
			[]artifactBlockerEvidence{{Kind: "run_id", ID: runIDStr}, {Kind: "artifact_kind", ID: artifactKind}},
			[]artifactNextAction{{Action: "request_registered_kind", Description: "request an existing registered artifact kind for that run"}},
		))
	}

	artifactPath, err := validateArtifactPath(dbArtifact.Path, runID)
	if err != nil {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerUnsafeArtifactPath,
			err.Error(),
			false,
			[]artifactBlockerEvidence{{Kind: "run_id", ID: runIDStr}, {Kind: "artifact_kind", ID: artifactKind}},
			[]artifactNextAction{{Action: "repair_path_containment", Description: "operator must repair artifact registry/path containment"}},
		))
	}

	fileInfo, err := os.Stat(artifactPath)
	if err != nil {
		return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
			BlockerArtifactNotFound,
			"artifact file not accessible",
			true,
			[]artifactBlockerEvidence{{Kind: "run_id", ID: runIDStr}, {Kind: "artifact_kind", ID: artifactKind}},
			[]artifactNextAction{{Action: "request_registered_kind", Description: "request an existing registered artifact kind for that run"}},
		))
	}
	size := fileInfo.Size()
	mimeType := dbArtifact.MimeType
	createdAt := dbArtifact.CreatedAt
	artifactID := dbArtifact.ID
	ref := buildArtifactRef(runID, artifactKind)

	var data []byte
	contentModes := viewMode == ViewModeBoundedExcerpt || viewMode == ViewModeSummary || viewMode == ViewModeErrors

	contentHash, hashStatus, hashErr := computeFullArtifactHash(artifactPath, size, includeHash)
	if hashErr != nil {
		hashStatus = HashStatusUnavailable
		contentHash = ""
	}

	if contentModes {
		var readErr error
		data, _, readErr = readAndStatArtifact(artifactPath, maxBytes)
		if readErr != nil {
			return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
				BlockerArtifactReadFailed,
				"failed to read artifact: "+readErr.Error(),
				true,
				[]artifactBlockerEvidence{{Kind: "run_id", ID: runIDStr}, {Kind: "artifact_kind", ID: artifactKind}},
				[]artifactNextAction{{Action: "verify_readability", Description: "verify artifact exists and is readable"}},
			))
		}

		if isBinaryContent(data, mimeType) {
			_ = run
			return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
				BlockerArtifactBinary,
				fmt.Sprintf("artifact is binary or unsupported (mime: %s)", mimeType),
				true,
				[]artifactBlockerEvidence{{Kind: "mime_type", Value: mimeType}, {Kind: "artifact_kind", ID: artifactKind}},
				[]artifactNextAction{{Action: "metadata_only_or_text", Description: "use metadata-only or request a text/JSON artifact"}},
			))
		}
	}

	var redactedData []byte
	redactionStatus := RedactionNotRequired

	if data != nil && contentModes {
		redactedData, redactionStatus = redactContent(data)
		if redactionStatus == RedactionBlocked {
			return buildArtifactBlockerResponse(runIDStr, newArtifactBlocker(
				BlockerRedactionBlocked,
				"artifact content contains high-risk sensitive material that cannot be safely redacted in bounded form",
				false,
				[]artifactBlockerEvidence{{Kind: "artifact_kind", ID: artifactKind}, {Kind: "redaction_status", Value: redactionStatus}},
				[]artifactNextAction{{Action: "operator_intervention", Description: "operator intervention required"}},
			))
		}
	}

	out := getRunArtifactOutput{
		OK:              true,
		Tool:            "get_run_artifact",
		RunID:           runIDStr,
		ArtifactKind:    artifactKind,
		ViewMode:        viewMode,
		RedactionStatus: redactionStatus,
		MaxBytes:        maxBytes,
		Blockers:        []artifactBlocker{},
	}

	out.Artifact = &artifactReadbackResult{
		ArtifactID:        artifactID,
		Kind:              artifactKind,
		MimeType:          mimeType,
		SizeBytes:         size,
		CreatedAt:         createdAt,
		ContentHash:       contentHash,
		ContentHashStatus: hashStatus,
		ArtifactRef:       ref,
	}

	if contentModes {
		out.Content = applyViewMode(viewMode, redactedData, maxBytes, size, &out)
	} else {
		out.Content = nil
	}

	if out.Content != nil && out.ReturnedBytes == 0 {
		b, _ := json.Marshal(out.Content)
		out.ReturnedBytes = len(b)
	}

	text, err := marshalTool(out)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolOK(text)
}

func applyViewMode(viewMode string, data []byte, maxBytes int, fileSize int64, out *getRunArtifactOutput) any {
	switch viewMode {
	case ViewModeBoundedExcerpt:
		content := string(data)
		if fileSize > int64(len(content)) {
			out.Truncated = true
		}
		if len(content) > maxBytes {
			content = content[:maxBytes]
			out.Truncated = true
		}
		out.ReturnedBytes = len(content)
		return map[string]any{"excerpt": content}

	case ViewModeSummary:
		isJSON := false
		var check any
		if json.Unmarshal(data, &check) == nil {
			isJSON = true
		}
		var summary string
		if isJSON {
			summary = extractSummaryJSON(data)
		} else {
			summaryBytes := extractSummaryText(data)
			summary = string(summaryBytes)
		}
		if len(summary) > maxBytes {
			summary = summary[:maxBytes]
			out.Truncated = true
		}
		out.ReturnedBytes = len(summary)
		return map[string]any{"summary": summary}

	case ViewModeErrors:
		var errs []byte
		var truncated bool
		isJSON := false
		var check any
		if json.Unmarshal(data, &check) == nil {
			isJSON = true
		}
		if isJSON {
			errs, truncated = extractErrorsJSON(data, maxBytes)
		} else {
			errs, truncated = extractErrorsText(data, maxBytes)
		}
		if truncated {
			out.Truncated = true
		}
		if len(errs) == 0 || string(errs) == "[]" {
			return map[string]any{
				"errors": []any{},
				"status": "no_errors_found",
			}
		}
		var parsedErrs any
		json.Unmarshal(errs, &parsedErrs)
		result := map[string]any{"errors": parsedErrs}
		if !isJSON {
			result["status"] = "errors_found"
		}
		eb, _ := json.Marshal(result)
		if len(eb) > maxBytes {
			eb = eb[:maxBytes]
			out.Truncated = true
		}
		out.ReturnedBytes = len(eb)
		return result

	default:
		return nil
	}
}

func newArtifactBlocker(code, message string, recoverable bool, evidence []artifactBlockerEvidence, nextActions []artifactNextAction) artifactBlocker {
	if evidence == nil {
		evidence = []artifactBlockerEvidence{}
	}
	if nextActions == nil {
		nextActions = []artifactNextAction{}
	}
	return artifactBlocker{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
		Evidence:    evidence,
		NextActions: nextActions,
	}
}

func buildArtifactBlockerResponse(runID string, blocker artifactBlocker) ToolCallResult {
	result := getRunArtifactBlockers{
		OK:    false,
		Tool:  "get_run_artifact",
		RunID: runID,
		Blockers: []artifactBlocker{
			blocker,
		},
	}
	text, err := marshalTool(result)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return toolErr(text)
}

func newIntPtr(n int) *int    { return &n }
func newBoolPtr(b bool) *bool { return &b }
