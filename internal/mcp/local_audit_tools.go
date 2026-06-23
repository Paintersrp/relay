package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"relay/internal/auditor"
)

const localAuditPacketPreviewHardCap = 65536

var createLocalAuditSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["mode", "project_id"],
  "properties": {
    "mode": {
      "type": "string",
      "enum": ["recent_commit", "selected_pass_changes", "feature_slice", "full_repository"],
      "description": "Local audit mode. Evidence is local-only and bounded."
    },
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "repo_ids": {
      "type": "array",
      "description": "Optional registered repository IDs. recent_commit requires exactly one.",
      "items": { "type": "string", "minLength": 1 },
      "maxItems": 20
    },
    "plan_id": { "type": "string", "description": "Required for selected_pass_changes." },
    "pass_id": { "type": "string", "description": "Required for selected_pass_changes." },
    "source_snapshot_id": { "type": "string", "description": "Optional Relay source snapshot identifier." },
    "context_packet_id": { "type": "string", "description": "Optional Relay context packet identifier." },
    "title": { "type": "string", "description": "Optional operator title for artifacts." },
    "description": { "type": "string", "description": "Optional operator note; do not include sensitive values." },
    "paths": {
      "type": "array",
      "description": "Repository-relative slash paths for feature_slice only.",
      "items": { "type": "string", "minLength": 1 },
      "maxItems": 50
    },
    "search_terms": {
      "type": "array",
      "description": "Literal bounded search terms for feature_slice only; do not include sensitive values.",
      "items": { "type": "string", "minLength": 1 },
      "maxItems": 20
    },
    "diff_mode": {
      "type": "string",
      "enum": ["worktree", "staged", "recent_commit"],
      "description": "Diff evidence mode. selected_pass_changes allows worktree or staged; recent_commit uses recent_commit."
    },
    "max_files": { "type": "integer", "minimum": 1, "maximum": 1000 },
    "max_bytes": { "type": "integer", "minimum": 1, "maximum": 1048576 },
    "context_lines": { "type": "integer", "minimum": 0, "maximum": 5 }
  }
}`)

var getLocalAuditSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["audit_id"],
  "properties": {
    "audit_id": {
      "type": "string",
      "minLength": 1,
      "description": "Local audit identifier."
    },
    "include_packet_preview": {
      "type": "boolean",
      "description": "Include a bounded packet preview when true."
    },
    "max_bytes": {
      "type": "integer",
      "description": "Maximum preview bytes.",
      "minimum": 1,
      "maximum": 65536
    }
  }
}`)

var listProjectLocalAuditsSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "mode": {
      "type": "string",
      "enum": ["", "recent_commit", "selected_pass_changes", "feature_slice", "full_repository"],
      "description": "Optional local audit mode filter."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100
    }
  }
}`)

var (
	ToolCreateLocalAudit = ToolDefinition{
		Name:        "create_local_audit",
		Description: "Create a local-only audit from registered Relay project repositories using bounded read-only source evidence; no shell execution or mutation is exposed.",
		InputSchema: createLocalAuditSchema,
	}
	ToolGetLocalAudit = ToolDefinition{
		Name:        "get_local_audit",
		Description: "Return local audit metadata, artifact paths, blockers, warnings, and optional bounded packet preview.",
		InputSchema: getLocalAuditSchema,
	}
	ToolListProjectLocalAudits = ToolDefinition{
		Name:        "list_project_local_audits",
		Description: "List recent local audit records for a Relay project with an optional mode filter.",
		InputSchema: listProjectLocalAuditsSchema,
	}
)

type createLocalAuditArgs struct {
	Mode             string   `json:"mode"`
	ProjectID        string   `json:"project_id"`
	RepoIDs          []string `json:"repo_ids"`
	PlanID           string   `json:"plan_id"`
	PassID           string   `json:"pass_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	ContextPacketID  string   `json:"context_packet_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Paths            []string `json:"paths"`
	SearchTerms      []string `json:"search_terms"`
	DiffMode         string   `json:"diff_mode"`
	MaxFiles         int      `json:"max_files"`
	MaxBytes         int      `json:"max_bytes"`
	ContextLines     int      `json:"context_lines"`
}

type getLocalAuditArgs struct {
	AuditID              string `json:"audit_id"`
	IncludePacketPreview bool   `json:"include_packet_preview"`
	MaxBytes             int    `json:"max_bytes"`
}

type listProjectLocalAuditsArgs struct {
	ProjectID string `json:"project_id"`
	Mode      string `json:"mode"`
	Limit     int64  `json:"limit"`
}

func (s *Server) HandleCreateLocalAudit(rawArgs json.RawMessage) ToolCallResult {
	var args createLocalAuditArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if msg := validateLocalAuditToolArgs(args); msg != "" {
		return brokerToolErr("VALIDATION_ERROR", msg)
	}
	result, err := auditor.NewLocalAuditService(s.deps.Store).Create(context.Background(), auditor.LocalAuditInput{
		Mode:             args.Mode,
		ProjectID:        args.ProjectID,
		RepoIDs:          args.RepoIDs,
		PlanID:           args.PlanID,
		PassID:           args.PassID,
		SourceSnapshotID: args.SourceSnapshotID,
		ContextPacketID:  args.ContextPacketID,
		Title:            args.Title,
		Description:      args.Description,
		Paths:            args.Paths,
		SearchTerms:      args.SearchTerms,
		DiffMode:         args.DiffMode,
		MaxFiles:         args.MaxFiles,
		MaxBytes:         args.MaxBytes,
		ContextLines:     args.ContextLines,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return brokerToolErr("NOT_FOUND", err.Error())
		}
		return brokerToolErr("INTERNAL_ERROR", err.Error())
	}
	return brokerToolOK(ToolCreateLocalAudit.Name, result)
}

func (s *Server) HandleGetLocalAudit(rawArgs json.RawMessage) ToolCallResult {
	var args getLocalAuditArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	auditID := strings.TrimSpace(args.AuditID)
	if auditID == "" {
		return brokerToolErr("VALIDATION_ERROR", "audit_id is required")
	}
	result, err := auditor.NewLocalAuditService(s.deps.Store).Get(context.Background(), auditID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return brokerToolErr("NOT_FOUND", "local audit not found")
		}
		return brokerToolErr("INTERNAL_ERROR", err.Error())
	}
	payload := map[string]interface{}{
		"audit_id":           result.AuditID,
		"mode":               result.Mode,
		"status":             result.Status,
		"project_id":         result.ProjectID,
		"title":              result.Title,
		"manifest_path":      result.ManifestPath,
		"packet_path":        result.PacketPath,
		"input_summary_path": result.InputSummaryPath,
		"blockers":           result.Blockers,
		"warnings":           result.Warnings,
		"created_at":         result.CreatedAt,
		"completed_at":       result.CompletedAt,
	}
	if args.IncludePacketPreview && result.PacketPath != "" {
		maxBytes := args.MaxBytes
		if maxBytes <= 0 || maxBytes > localAuditPacketPreviewHardCap {
			maxBytes = 32768
		}
		preview, truncated, err := readBoundedPreview(result.PacketPath, maxBytes)
		if err != nil {
			return brokerToolErr("INTERNAL_ERROR", "failed to read packet preview")
		}
		payload["packet_preview"] = preview
		payload["packet_preview_truncated"] = truncated
	}
	return brokerToolOK(ToolGetLocalAudit.Name, payload)
}

func (s *Server) HandleListProjectLocalAudits(rawArgs json.RawMessage) ToolCallResult {
	var args listProjectLocalAuditsArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	projectID := strings.TrimSpace(args.ProjectID)
	if projectID == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id is required")
	}
	mode := strings.TrimSpace(args.Mode)
	if mode != "" && !validLocalAuditMode(mode) {
		return brokerToolErr("VALIDATION_ERROR", "invalid local audit mode")
	}
	limit := args.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := auditor.NewLocalAuditService(s.deps.Store).ListByProject(context.Background(), projectID, mode, limit)
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", err.Error())
	}
	return brokerToolOK(ToolListProjectLocalAudits.Name, map[string]interface{}{
		"project_id": projectID,
		"audits":     result,
	})
}

func validateLocalAuditToolArgs(args createLocalAuditArgs) string {
	mode := strings.TrimSpace(args.Mode)
	if !validLocalAuditMode(mode) {
		return "invalid local audit mode"
	}
	if strings.TrimSpace(args.ProjectID) == "" {
		return "project_id is required"
	}
	for _, p := range args.Paths {
		if invalidToolAuditPath(p) {
			return "paths must be repository-relative slash paths"
		}
	}
	switch mode {
	case string(auditor.LocalAuditModeRecentCommit):
		if len(nonEmptyToolStrings(args.RepoIDs)) != 1 {
			return "recent_commit requires exactly one repo_id"
		}
	case string(auditor.LocalAuditModeSelectedPassChanges):
		if strings.TrimSpace(args.PlanID) == "" || strings.TrimSpace(args.PassID) == "" {
			return "selected_pass_changes requires plan_id and pass_id"
		}
	case string(auditor.LocalAuditModeFeatureSlice):
		if len(nonEmptyToolStrings(args.Paths)) == 0 && len(nonEmptyToolStrings(args.SearchTerms)) == 0 {
			return "feature_slice requires paths or search_terms"
		}
	}
	return ""
}

func validLocalAuditMode(mode string) bool {
	switch mode {
	case string(auditor.LocalAuditModeRecentCommit),
		string(auditor.LocalAuditModeSelectedPassChanges),
		string(auditor.LocalAuditModeFeatureSlice),
		string(auditor.LocalAuditModeFullRepository):
		return true
	default:
		return false
	}
}

func invalidToolAuditPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return true
	}
	cleaned := path.Clean(value)
	return path.IsAbs(value) || filepath.IsAbs(value) || cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func nonEmptyToolStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func readBoundedPreview(filePath string, maxBytes int) (string, bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", false, err
	}
	if len(data) <= maxBytes {
		return string(data), false, nil
	}
	return string(data[:maxBytes]), true, nil
}
