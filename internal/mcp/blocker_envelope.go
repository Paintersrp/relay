package mcp

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"relay/internal/pathsafety"
)

const (
	MCPBlockerUnknownResource          = "unknown_resource"
	MCPBlockerUnknownRepository        = "unknown_repository"
	MCPBlockerAliasAmbiguous           = "alias_ambiguous"
	MCPBlockerSourceSnapshotStale      = "source_snapshot_stale"
	MCPBlockerDirtyWorktree            = "dirty_worktree"
	MCPBlockerRequiredContextMissing   = "required_context_missing"
	MCPBlockerRequiredContextTruncated = "required_context_truncated"
	MCPBlockerBlockedPath              = "blocked_path"
	MCPBlockerRedactionFailed          = "redaction_failed"
	MCPBlockerSchemaMismatch           = "schema_mismatch"
	MCPBlockerExpectedHashMismatch     = "expected_hash_mismatch"
	MCPBlockerToolUnavailable          = "tool_unavailable"
	MCPBlockerToolSchemaStale          = "tool_schema_stale"
	MCPBlockerUnsafeRequest            = "unsafe_request"
	MCPBlockerFileReferenceInvalid     = "file_reference_invalid"
	MCPBlockerUnsafeDownloadTarget     = "unsafe_download_target"
	MCPBlockerFileDownloadFailed       = "file_download_failed"
	MCPBlockerFileDownloadStatus       = "file_download_status"
	MCPBlockerFileDownloadEmpty        = "file_download_empty"
	MCPBlockerFileDownloadTooLarge     = "file_download_too_large"
)

const (
	maxBlockerEvidence   = 8
	maxBlockerActions    = 8
	maxBlockerMessageLen = 300
	maxBlockerDetailLen  = 300
)

type MCPBlockerEvidence struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type MCPBlocker struct {
	Code        string               `json:"code"`
	Message     string               `json:"message"`
	Recoverable bool                 `json:"recoverable"`
	Evidence    []MCPBlockerEvidence `json:"evidence"`
	NextActions []string             `json:"next_actions"`
}

type MCPBlockedResponse struct {
	OK          bool         `json:"ok"`
	Tool        string       `json:"tool"`
	Status      string       `json:"status"`
	Blockers    []MCPBlocker `json:"blockers"`
	GeneratedAt string       `json:"generated_at"`
	Metadata    any          `json:"metadata,omitempty"`
}

type SubmittedArtifactIdentity struct {
	ArtifactKind string `json:"artifact_kind"`
	DisplayName  string `json:"display_name"`
	ByteCount    int64  `json:"byte_count"`
}

type ExactSubmissionProvenance struct {
	SubmittedSHA256  string                    `json:"submitted_sha256"`
	ExpectedSHA256   string                    `json:"expected_sha256,omitempty"`
	SHAMatchStatus   string                    `json:"sha_match_status"`
	SourceMode       string                    `json:"source_mode"`
	ArtifactIdentity SubmittedArtifactIdentity `json:"artifact_identity"`
}

var safeRefRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@+-]{0,127}$`)
var lowerHashRE = regexp.MustCompile(`^[a-f0-9]{32,128}$`)

func newMCPBlocker(code, message string, recoverable bool, evidence []MCPBlockerEvidence, nextActions []string) MCPBlocker {
	code = sanitizeBlockerCode(code)
	if code == "" {
		code = MCPBlockerUnsafeRequest
	}
	blocker := MCPBlocker{
		Code:        code,
		Message:     sanitizeBoundedText(message, maxBlockerMessageLen),
		Recoverable: recoverable,
		Evidence:    sanitizeBlockerEvidence(evidence),
		NextActions: sanitizeNextActions(nextActions),
	}
	if blocker.Message == "" {
		blocker.Message = "request is blocked"
	}
	if len(blocker.NextActions) == 0 {
		blocker.NextActions = []string{"Correct the blocker and retry the tool."}
	}
	return blocker
}

func toolBlocked(tool string, blockers []MCPBlocker, metadata any) ToolCallResult {
	if len(blockers) == 0 {
		blockers = []MCPBlocker{newMCPBlocker(MCPBlockerUnsafeRequest, "request is blocked", false, nil, nil)}
	}
	if len(blockers) > maxBlockerEvidence {
		blockers = blockers[:maxBlockerEvidence]
	}
	resp := MCPBlockedResponse{
		OK:          false,
		Tool:        sanitizeToolName(tool),
		Status:      "blocked",
		Blockers:    blockers,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Metadata:    metadata,
	}
	text := blockedSummaryText(resp.Tool, blockers[0])
	if data, err := json.Marshal(resp); err == nil {
		text = string(data)
	}
	return ToolCallResult{
		Content:           []ContentBlock{{Type: "text", Text: text}},
		StructuredContent: resp,
		IsError:           true,
	}
}

func toolBlockedJSON(tool string, blockers []MCPBlocker, metadata any) ToolCallResult {
	res := toolBlocked(tool, blockers, metadata)
	if _, err := json.Marshal(res.StructuredContent); err != nil {
		return toolErr(`{"ok":false,"status":"blocked","blockers":[{"code":"unsafe_request","message":"failed to marshal blocked response","recoverable":false,"evidence":[],"next_actions":["Retry after the operator inspects the tool response."]}]}`)
	}
	return res
}

func blockedSummaryText(tool string, blocker MCPBlocker) string {
	action := "Correct the blocker and retry the tool."
	if len(blocker.NextActions) > 0 {
		action = blocker.NextActions[0]
	}
	return sanitizeBoundedText(tool, 80) + " blocked: " + sanitizeBoundedText(blocker.Code, 80) + ". " + sanitizeBoundedText(action, 180)
}

func sanitizeBlockerEvidence(items []MCPBlockerEvidence) []MCPBlockerEvidence {
	out := make([]MCPBlockerEvidence, 0, minIntMCP(len(items), maxBlockerEvidence))
	for _, item := range items {
		if len(out) >= maxBlockerEvidence {
			break
		}
		kind := sanitizeEvidenceKind(item.Kind)
		ref := sanitizeEvidenceRef(kind, item.Ref)
		detail := sanitizeBoundedText(item.Detail, maxBlockerDetailLen)
		if kind == "" || (ref == "" && detail == "") {
			continue
		}
		out = append(out, MCPBlockerEvidence{Kind: kind, Ref: ref, Detail: detail})
	}
	if out == nil {
		return []MCPBlockerEvidence{}
	}
	return out
}

func sanitizeNextActions(items []string) []string {
	out := make([]string, 0, minIntMCP(len(items), maxBlockerActions))
	for _, item := range items {
		if len(out) >= maxBlockerActions {
			break
		}
		item = sanitizeBoundedText(item, maxBlockerDetailLen)
		if item != "" {
			out = append(out, item)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func sanitizeToolName(s string) string {
	s = strings.TrimSpace(s)
	if safeRefRE.MatchString(s) {
		return s
	}
	return "unknown_tool"
}

func sanitizeEvidenceKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || strings.ContainsAny(s, "/\\:\x00\r\n\t ") {
		return ""
	}
	return sanitizeBoundedText(s, 64)
}

func sanitizeEvidenceRef(kind, ref string) string {
	if containsControlChars(ref) {
		return ""
	}
	ref = sanitizeBoundedText(ref, 160)
	if ref == "" {
		return ""
	}
	if kind == "path" {
		slash, ok := pathsafety.NormalizeRepoRelativePath(ref, true)
		if !ok || slash == "" {
			return ""
		}
		return slash
	}
	if lowerHashRE.MatchString(ref) || safeRefRE.MatchString(ref) {
		return ref
	}
	return ""
}

func containsControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func sanitizeBlockerCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "_")
	if code == "" {
		return ""
	}
	for _, r := range code {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return sanitizeBoundedText(code, 80)
}

func sanitizeBoundedText(s string, limit int) string {
	s = strings.TrimSpace(stripControlChars(s))
	if s == "" {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return strings.TrimSpace(s[:limit])
}

func stripControlChars(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' {
			b.WriteRune(' ')
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func safeArtifactDisplayName(name, fallback string) string {
	name = pathsafety.SafeDisplayBaseName(name, fallback)
	name = sanitizeBoundedText(name, 120)
	if name == "" {
		return fallback
	}
	return name
}

func minIntMCP(a, b int) int {
	if a < b {
		return a
	}
	return b
}
