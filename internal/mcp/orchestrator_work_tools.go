package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	appplans "relay/internal/app/plans"
	"relay/internal/contextpackets"
	"relay/internal/sources"
)

// sourceSnapshotAdapter adapts sources.Service to the
// appplans.sourceSnapshotAcquirer interface.
type sourceSnapshotAdapter struct {
	svc *sources.Service
}

func (a *sourceSnapshotAdapter) CreateSourceSnapshot(ctx context.Context, projectID string, repoIDs []string, includeFileMetadata bool) (string, string, int, error) {
	result, err := a.svc.CreateSourceSnapshot(ctx, sources.SourceSnapshotInput{
		ProjectID:           projectID,
		RepoIDs:             repoIDs,
		IncludeFileMetadata: includeFileMetadata,
	})
	if err != nil {
		return "", "", 0, err
	}
	includedCount := 0
	for _, repo := range result.Repositories {
		includedCount += repo.IncludedFileCount
	}
	return result.SourceSnapshotID, result.Status, includedCount, nil
}

func (a *sourceSnapshotAdapter) GetSourceSnapshotFreshness(ctx context.Context, projectID string, sourceSnapshotID string) (appplans.SourceFreshnessReport, error) {
	report, err := a.svc.GetSourceSnapshotFreshness(ctx, projectID, sourceSnapshotID)
	if err != nil {
		return appplans.SourceFreshnessReport{}, err
	}
	return appplans.SourceFreshnessReport{
		Status:             report.Status,
		ReusableForHandoff: report.ReusableForHandoff,
		SourceSnapshotID:   report.SourceSnapshotID,
		AgeSeconds:         report.AgeSeconds,
		MaxAgeSeconds:      report.MaxAgeSeconds,
		RepositoryReports:  sourceFreshnessRepos(report.RepositoryReports),
		Warnings:           sourceFreshnessBlockers(report.Warnings),
		Blockers:           sourceFreshnessBlockers(report.Blockers),
		NextActions:        sourceFreshnessNextActions(report.NextActions),
	}, nil
}

func sourceFreshnessRepos(reports []sources.RepositoryFreshnessReport) []appplans.RepositoryFreshnessReport {
	out := make([]appplans.RepositoryFreshnessReport, 0, len(reports))
	for _, report := range reports {
		out = append(out, appplans.RepositoryFreshnessReport{
			CapturedDirty: report.CapturedDirty,
			CurrentDirty:  report.CurrentDirty,
		})
	}
	return out
}

func sourceFreshnessBlockers(blockers []sources.SourceBlocker) []appplans.SourceBlocker {
	out := make([]appplans.SourceBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		out = append(out, appplans.SourceBlocker{
			RepoID:      blocker.RepoID,
			Code:        blocker.Code,
			Message:     blocker.Message,
			Recoverable: blocker.Recoverable,
		})
	}
	return out
}

func sourceFreshnessNextActions(actions []sources.SourceFreshnessNextAction) []appplans.SourceFreshnessNextAction {
	out := make([]appplans.SourceFreshnessNextAction, 0, len(actions))
	for _, action := range actions {
		out = append(out, appplans.SourceFreshnessNextAction{
			Action: action.Action,
			Reason: action.Reason,
		})
	}
	return out
}

// contextPacketAdapter adapts contextpackets.Service to the
// appplans.contextPacketAcquirer interface.
type contextPacketAdapter struct {
	svc *contextpackets.Service
}

func (a *contextPacketAdapter) CreateContextPacket(ctx context.Context, input appplans.CtxPacketInput) (*appplans.CtxPacketResult, error) {
	seedFiles := make([]contextpackets.ContextSeedFile, 0, len(input.SeedFiles))
	for _, sf := range input.SeedFiles {
		seedFiles = append(seedFiles, contextpackets.ContextSeedFile{
			RepoID:    sf.RepoID,
			Path:      sf.Path,
			LineStart: sf.LineStart,
			LineEnd:   sf.LineEnd,
			Reason:    sf.Reason,
			Required:  sf.Required,
			MaxBytes:  sf.MaxBytes,
		})
	}
	seedSearches := make([]contextpackets.ContextSeedSearch, 0, len(input.SeedSearches))
	for _, ss := range input.SeedSearches {
		seedSearches = append(seedSearches, contextpackets.ContextSeedSearch{
			RepoIDs:      ss.RepoIDs,
			Pattern:      ss.Pattern,
			Required:     ss.Required,
			MaxResults:   ss.MaxResults,
			Reason:       ss.Reason,
			ContextLines: ss.ContextLines,
		})
	}
	result, err := a.svc.CreateContextPacket(ctx, contextpackets.ContextPacketInput{
		ProjectID:        input.ProjectID,
		PlanID:           input.PlanID,
		PassID:           input.PassID,
		TaskSlug:         input.TaskSlug,
		SourceSnapshotID: input.SourceSnapshotID,
		SeedFiles:        seedFiles,
		SeedSearches:     seedSearches,
		IncludeInventory: input.IncludeInventory,
		MaxSources:       input.MaxSources,
		MaxTotalBytes:    input.MaxTotalBytes,
	})
	if err != nil {
		return nil, err
	}
	return &appplans.CtxPacketResult{
		ContextPacketID:    result.ContextPacketID,
		Status:             result.Status,
		CoverageReportPath: result.CoverageReportPath,
		BlockedSeedCount:   result.BlockedSeedCount,
		MissingSeedCount:   result.MissingSeedCount,
		Truncated:          result.Truncated,
		SourceSnapshotID:   result.SourceSnapshotID,
		SourceCount:        result.SourceCount,
		Summary: appplans.CtxPacketSummary{
			SourceCount:                   result.Summary.SourceCount,
			CoveredSeedCount:              result.Summary.CoveredSeedCount,
			BlockedSeedCount:              result.Summary.BlockedSeedCount,
			MissingSeedCount:              result.Summary.MissingSeedCount,
			Truncated:                     result.Summary.Truncated,
			RequiredContextTruncated:      result.Summary.RequiredContextTruncated,
			RequiredSearchNonExhaustive:   result.Summary.RequiredSearchNonExhaustive,
			OptionalSearchTruncated:       result.Summary.OptionalSearchTruncated,
			MaxSources:                    result.Summary.MaxSources,
			MaxTotalBytes:                 result.Summary.MaxTotalBytes,
			TotalSourceBytes:              result.Summary.TotalSourceBytes,
			InventoryIncluded:             result.Summary.InventoryIncluded,
			OptionalInventoryTruncated:    result.Summary.OptionalInventoryTruncated,
			PacketSourceLimitTruncated:    result.Summary.PacketSourceLimitTruncated,
			PacketTotalByteLimitTruncated: result.Summary.PacketTotalByteLimitTruncated,
		},
		Coverage: mapContextCoverageEntries(result.Coverage),
		LimitHit: result.LimitHit,
	}, nil
}

func mapContextCoverageEntries(entries []contextpackets.ContextCoverageEntry) []appplans.CtxCoverageEntry {
	out := make([]appplans.CtxCoverageEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, appplans.CtxCoverageEntry{
			SeedID:          entry.SeedID,
			SeedType:        entry.SeedType,
			Required:        entry.Required,
			Status:          entry.Status,
			Path:            entry.Path,
			Pattern:         entry.Pattern,
			Reason:          entry.Reason,
			SourceIDs:       append([]string(nil), entry.SourceIDs...),
			Truncated:       entry.Truncated,
			TruncationClass: entry.TruncationClass,
			Blockers:        mapSourceBlockers(entry.Blockers),
			MissingCause:    entry.MissingCause,
		})
	}
	return out
}

func mapSourceBlockers(blockers []sources.SourceBlocker) []appplans.CtxSourceBlocker {
	out := make([]appplans.CtxSourceBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		out = append(out, appplans.CtxSourceBlocker{
			RepoID:  blocker.RepoID,
			Code:    blocker.Code,
			Message: blocker.Message,
		})
	}
	return out
}

// ----------------------------------------------------------------------------
// Tool schemas -- orchestrator work packet retrieval tools.
// ----------------------------------------------------------------------------

var getNextPassWorkSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "minLength": 1,
      "description": "Optional Relay pass identifier to request a specific eligible pass. Omitted to select the earliest eligible pass."
    }
  }
}`)

var getNextPassWorkOutputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["ok", "tool", "context_ready", "blockers", "local_preview_hint"],
  "properties": {
    "ok": {"type": "boolean"},
    "tool": {"type": "string", "const": "get_next_pass_work"},
    "project_id": {"type": "string"},
    "plan_id": {"type": "string"},
    "selected_pass": {
      "type": "object",
      "additionalProperties": false,
      "required": ["pass_id", "sequence", "name", "status"],
      "properties": {
        "pass_id": {"type": "string"},
        "sequence": {"type": "integer"},
        "name": {"type": "string"},
        "status": {"type": "string"}
      }
    },
    "readiness_state": {"type": "string"},
    "source_snapshot_id": {"type": "string"},
    "context_packet_id": {"type": "string"},
    "context_ready": {"type": "boolean"},
    "required_context_bundle": {
      "type": "object",
      "additionalProperties": true,
      "description": "Metadata-only required context bundle with manifest metadata, required files/searches, readiness criteria, budget guidance, and safe blockers. Never includes raw source contents."
    },
    "handoff_work": {
      "type": "object",
      "additionalProperties": true,
      "description": "Bounded Planner handoff-authoring packet for the selected pass. Present only when ready for handoff authoring."
    },
    "handoff_authoring_packet": {
      "type": "object",
      "additionalProperties": true,
      "description": "Alias of handoff_work for clients that prefer explicit authoring semantics."
    },
    "blockers": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["code", "message", "recoverable", "evidence", "next_actions"],
        "properties": {
          "code": {"type": "string"},
          "message": {"type": "string"},
          "recoverable": {"type": "boolean"},
          "evidence": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["kind"],
              "properties": {
                "kind": {"type": "string"},
                "ref": {"type": "string"},
                "detail": {"type": "string"}
              }
            }
          },
          "next_actions": {"type": "array", "items": {"type": "string"}}
        }
      }
    },
    "next_actions": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["description"],
        "properties": {
          "tool": {"type": "string"},
          "description": {"type": "string"},
          "arguments": {
            "type": "object",
            "additionalProperties": true
          },
          "depends_on": {"type": "string"},
          "argument_bindings": {
            "type": "object",
            "additionalProperties": {"type": "string"}
          }
        }
      }
    },
    "local_preview_hint": {"type": "string"},
    "acquisition_summary": {
      "type": "object",
      "additionalProperties": false,
      "required": ["source_snapshot_acquired", "context_packet_created"],
      "properties": {
        "source_snapshot_acquired": {"type": "boolean"},
        "source_snapshot_id": {"type": "string"},
        "context_packet_created": {"type": "boolean"},
        "context_packet_id": {"type": "string"},
        "context_packet_status": {"type": "string"}
      }
    },
    "acquisition_failure_report": {
      "type": "object",
      "additionalProperties": true,
      "description": "Bounded terminal context acquisition diagnostics. Present only when readiness_state is context_acquisition_failed."
    }
  }
}`)

var prepareHandoffContextSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_id", "pass_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "minLength": 1,
      "description": "Explicit Relay pass identifier to prepare; this tool never selects an arbitrary next pass."
    }
  }
}`)

var prepareHandoffContextOutputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["ok", "tool", "project_id", "plan_id", "pass_id", "readiness_state", "repo_heads", "required_coverage", "optional_coverage", "blockers", "recommended_next_action", "lower_level_recovery_actions"],
  "properties": {
    "ok": {"type": "boolean"},
    "tool": {"type": "string", "const": "prepare_handoff_context"},
    "project_id": {"type": "string"},
    "plan_id": {"type": "string"},
    "pass_id": {"type": "string"},
    "readiness_state": {"type": "string"},
    "source_snapshot_id": {"type": "string"},
    "context_packet_id": {"type": "string"},
    "repo_heads": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["repo_id", "dirty", "changed_file_count", "git_status_available"],
        "properties": {
          "repo_id": {"type": "string"},
          "branch": {"type": "string"},
          "head_sha": {"type": "string"},
          "dirty": {"type": "boolean"},
          "changed_file_count": {"type": "integer"},
          "git_status_available": {"type": "boolean"}
        }
      }
    },
    "required_coverage": {"type": "object", "additionalProperties": true},
    "optional_coverage": {"type": "object", "additionalProperties": true},
    "freshness_report": {"type": "object", "additionalProperties": true},
    "required_context_bundle": {
      "type": "object",
      "additionalProperties": true,
      "description": "Metadata-only bundle. Never includes raw source, context packet content, logs, local paths, or generated handoff text."
    },
    "bundle_unavailable": {"type": "object", "additionalProperties": true},
    "blockers": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["code", "message", "recoverable", "evidence", "next_actions"],
        "properties": {
          "code": {"type": "string"},
          "message": {"type": "string"},
          "recoverable": {"type": "boolean"},
          "evidence": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["kind"],
              "properties": {
                "kind": {"type": "string"},
                "ref": {"type": "string"},
                "detail": {"type": "string"}
              }
            }
          },
          "next_actions": {"type": "array", "items": {"type": "string"}}
        }
      }
    },
    "warnings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["code", "message", "recoverable", "evidence", "next_actions"],
        "properties": {
          "code": {"type": "string"},
          "message": {"type": "string"},
          "recoverable": {"type": "boolean"},
          "evidence": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["kind"],
              "properties": {
                "kind": {"type": "string"},
                "ref": {"type": "string"},
                "detail": {"type": "string"}
              }
            }
          },
          "next_actions": {"type": "array", "items": {"type": "string"}}
        }
      }
    },
    "recommended_next_action": {"type": "string"},
    "lower_level_recovery_actions": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
    "acquisition_summary": {"type": "object", "additionalProperties": true},
    "acquisition_failure_report": {"type": "object", "additionalProperties": true}
  }
}`)

var getNextAuditWorkSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "plan_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "minLength": 1,
      "description": "Optional Relay pass identifier to scope the audit work selection."
    },
    "run_id": {
      "type": "string",
      "minLength": 1,
      "description": "Optional Relay run identifier to select a specific run for audit."
    }
  }
}`)

// ----------------------------------------------------------------------------
// Tool definitions.
// ----------------------------------------------------------------------------

var ToolGetNextPassWork = ToolDefinition{
	Name:         appplans.NextPassWorkTool,
	Description:  "Return the next eligible project-scoped plan pass work packet for Planner handoff creation. Performs bounded source snapshot and context packet artifact creation when they are missing or stale. Includes deterministic planner_jumpstart guidance plus a metadata-only required_context_bundle with manifest metadata, required files/searches, readiness criteria, and budget guidance when a pass is selected. Does NOT create runs, submit plans, generate handoffs, dispatch executors, mutate git, run shell commands, or expose arbitrary filesystem access.",
	InputSchema:  getNextPassWorkSchema,
	OutputSchema: getNextPassWorkOutputSchema,
	Annotations: map[string]any{
		"readOnlyHint":    false,
		"destructiveHint": false,
	},
}

var ToolPrepareHandoffContext = ToolDefinition{
	Name:         appplans.PrepareHandoffContextTool,
	Description:  "Prepare bounded source/context readiness diagnostics for one explicit managed pass before Planner handoff authoring. May create or reuse source snapshot and context packet artifacts through existing services. Does NOT create runs, submit plans, generate handoffs, dispatch executors, mutate git, run shell commands, call GitHub/models, expose arbitrary filesystem access, or return raw source/context content.",
	InputSchema:  prepareHandoffContextSchema,
	OutputSchema: prepareHandoffContextOutputSchema,
	Annotations: map[string]any{
		"readOnlyHint":    false,
		"destructiveHint": false,
	},
}

var ToolGetNextAuditWork = ToolDefinition{
	Name:        appplans.NextAuditWorkTool,
	Description: "Return the next audit-ready project-scoped work packet for an Auditor agent. Retrieval-only: does not generate audit judgments, apply audit decisions, create runs, mutate git, run shell commands, or expose arbitrary filesystem access.",
	InputSchema: getNextAuditWorkSchema,
}

// ----------------------------------------------------------------------------
// Argument structs.
// ----------------------------------------------------------------------------

type getNextPassWorkArgs struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	PassID    string `json:"pass_id,omitempty"`
}

type prepareHandoffContextArgs struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	PassID    string `json:"pass_id"`
}

type getNextAuditWorkArgs struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	PassID    string `json:"pass_id"`
	RunID     string `json:"run_id"`
}

// ----------------------------------------------------------------------------
// Helpers -- top-level orchestrator work tool payload marshaling.
// ----------------------------------------------------------------------------

// orchestratorWorkToolPayload marshals the service response as top-level JSON
// text content without a broker-style wrapper.
func orchestratorWorkToolPayload(payload interface{}, isError bool) ToolCallResult {
	data, err := json.Marshal(payload)
	if err != nil {
		return ToolCallResult{
			IsError: true,
			Content: []ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf(`{"ok":false,"error":{"code":"INTERNAL_ERROR","message":"failed to marshal response: %v"}}`, err),
			}},
		}
	}
	return ToolCallResult{
		IsError: isError,
		Content: []ContentBlock{{
			Type: "text",
			Text: string(data),
		}},
	}
}

func orchestratorWorkNextPassPayload(resp appplans.NextPassWorkResponse) ToolCallResult {
	summary := appplans.CompactNextPassWorkSummary(resp)
	return ToolCallResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: nextPassWorkSummaryText(summary),
		}},
		StructuredContent: summary,
	}
}

func orchestratorWorkPrepareHandoffContextPayload(resp appplans.PrepareHandoffContextResponse) ToolCallResult {
	return ToolCallResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: prepareHandoffContextSummaryText(resp),
		}},
		StructuredContent: resp,
	}
}

func prepareHandoffContextSummaryText(resp appplans.PrepareHandoffContextResponse) string {
	blockers := "none"
	if len(resp.Blockers) > 0 {
		parts := make([]string, 0, len(resp.Blockers))
		for _, blocker := range resp.Blockers {
			parts = append(parts, fmt.Sprintf("%s recoverable=%t", blocker.Code, blocker.Recoverable))
		}
		blockers = fmt.Sprintf("%s", parts)
	}
	return fmt.Sprintf(
		"prepare_handoff_context: pass=%s readiness=%s ok=%t source_snapshot_id=%q context_packet_id=%q blockers=%s. %s",
		resp.PassID,
		resp.ReadinessState,
		resp.OK,
		resp.SourceSnapshotID,
		resp.ContextPacketID,
		blockers,
		resp.RecommendedNextAction,
	)
}

func nextPassWorkSummaryText(summary appplans.NextPassWorkMCPSummary) string {
	selected := "none"
	if summary.SelectedPass != nil {
		selected = fmt.Sprintf("%s seq=%d name=%q status=%s", summary.SelectedPass.PassID, summary.SelectedPass.Sequence, summary.SelectedPass.Name, summary.SelectedPass.Status)
	}
	blockers := "none"
	if len(summary.Blockers) > 0 {
		blockers = ""
		for i, blocker := range summary.Blockers {
			if i > 0 {
				blockers += "; "
			}
			blockers += fmt.Sprintf("%s recoverable=%t", blocker.Code, blocker.Recoverable)
		}
	}
	bundle := "absent"
	if summary.RequiredContextBundle != nil {
		bundle = "present"
		if len(summary.RequiredContextBundle.Blockers) > 0 {
			bundle = fmt.Sprintf("present blockers=%d", len(summary.RequiredContextBundle.Blockers))
		}
	}
	next := "Use structuredContent.next_actions for follow-up references."
	if len(summary.NextActions) > 0 {
		next = summary.NextActions[0].Description
		if summary.NextActions[0].Tool != "" {
			next = summary.NextActions[0].Tool + ": " + next
		}
	}
	if summary.HandoffWork != nil && summary.ReadinessState == "ready_for_handoff_authoring" {
		next = "draft_planner_handoff: Use structuredContent.handoff_work to draft the Planner handoff; do not submit a run until the handoff is reviewed."
	}
	if summary.AcquisitionFailureReport != nil {
		code := summary.AcquisitionFailureReport.FailureCode
		limitHit := ""
		if summary.AcquisitionFailureReport.PacketSummary != nil && summary.AcquisitionFailureReport.PacketSummary.LimitHit != "" {
			limitHit = fmt.Sprintf(" limit_hit=%s", summary.AcquisitionFailureReport.PacketSummary.LimitHit)
		}
		return fmt.Sprintf(
			"get_next_pass_work: selected_pass=%s readiness=%s terminal_failure_code=%s context_packet_id=%q%s. Use structuredContent.acquisition_failure_report for bounded diagnostics. %s",
			selected,
			summary.ReadinessState,
			code,
			summary.ContextPacketID,
			limitHit,
			summary.LocalPreviewHint,
		)
	}
	return fmt.Sprintf(
		"get_next_pass_work: selected_pass=%s readiness=%s context_ready=%t source_snapshot_id=%q context_packet_id=%q required_context_bundle=%s blockers=%s. %s. %s",
		selected,
		summary.ReadinessState,
		summary.ContextReady,
		summary.SourceSnapshotID,
		summary.ContextPacketID,
		bundle,
		blockers,
		next,
		summary.LocalPreviewHint,
	)
}

// orchestratorWorkToolErr builds a top-level error payload shaped as a work packet blocker response.
func orchestratorWorkToolErr(toolName string, code string, message string) ToolCallResult {
	taxonomy := MCPBlockerUnsafeRequest
	if code == "tool_unavailable" {
		taxonomy = MCPBlockerToolUnavailable
	}
	result := toolBlockedResult(toolName, []MCPBlocker{
		newMCPBlocker(taxonomy, message, false, []MCPBlockerEvidence{{Kind: "tool", Ref: toolName}}, []string{"Correct the request or tool dependency, then retry."}),
	}, nil)
	legacyCode := code
	if code == "tool_unavailable" {
		legacyCode = appplans.BlockerUnsafeRequest
	}
	legacy := map[string]any{
		"ok":   false,
		"tool": toolName,
		"blockers": []map[string]any{{
			"code":         legacyCode,
			"message":      message,
			"recoverable":  false,
			"evidence":     []any{},
			"next_actions": []string{"Correct the request or tool dependency, then retry."},
		}},
	}
	if text, err := marshalTool(legacy); err == nil {
		result.Content = []ContentBlock{{Type: "text", Text: text}}
	}
	return result
}

// ----------------------------------------------------------------------------
// Handlers.
// ----------------------------------------------------------------------------

// HandleGetNextPassWork retrieves the next eligible Planner work packet
// for a project-scoped managed plan. When source and context packet services
// are available, the tool performs bounded backend acquisition (creating
// source snapshots and context packets as needed) before returning handoff
// readiness.
func (s *Server) HandleGetNextPassWork(rawArgs json.RawMessage) ToolCallResult {
	var args getNextPassWorkArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return orchestratorWorkToolErr(appplans.NextPassWorkTool, appplans.BlockerUnsafeRequest, "invalid params: "+err.Error())
	}

	if s.deps == nil || s.deps.Store == nil {
		return orchestratorWorkToolErr(appplans.NextPassWorkTool, "tool_unavailable", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	svc := appplans.NewOrchestratorWorkService(s.deps.Store)
	svc.SetSourceService(&sourceSnapshotAdapter{svc: sources.NewService(s.deps.Store)})
	svc.SetContextPacketService(&contextPacketAdapter{svc: contextpackets.NewService(s.deps.Store)})
	req := appplans.NextPassWorkRequest{
		ProjectID: args.ProjectID,
		PlanID:    args.PlanID,
		PassID:    args.PassID,
	}

	resp, err := svc.GetNextPassWork(context.Background(), req)
	if err != nil {
		return orchestratorWorkToolErr(appplans.NextPassWorkTool, appplans.BlockerUnsafeRequest, fmt.Sprintf("service error: %v", err))
	}

	// Ensure that when the service response has no handoff work, MCP structured content does not include it.
	if resp.HandoffWork == nil {
		resp.HandoffAuthoringPacket = nil
	}

	return orchestratorWorkNextPassPayload(resp)
}

// HandlePrepareHandoffContext prepares bounded source/context evidence and
// readiness diagnostics for one explicit managed pass without generating a
// Planner handoff or creating a run.
func (s *Server) HandlePrepareHandoffContext(rawArgs json.RawMessage) ToolCallResult {
	var args prepareHandoffContextArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return orchestratorWorkToolErr(appplans.PrepareHandoffContextTool, appplans.BlockerUnsafeRequest, "invalid params: "+err.Error())
	}

	if s.deps == nil || s.deps.Store == nil {
		return orchestratorWorkToolErr(appplans.PrepareHandoffContextTool, "tool_unavailable", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	svc := appplans.NewOrchestratorWorkService(s.deps.Store)
	svc.SetSourceService(&sourceSnapshotAdapter{svc: sources.NewService(s.deps.Store)})
	svc.SetContextPacketService(&contextPacketAdapter{svc: contextpackets.NewService(s.deps.Store)})
	resp, err := svc.PrepareHandoffContext(context.Background(), appplans.PrepareHandoffContextRequest{
		ProjectID: args.ProjectID,
		PlanID:    args.PlanID,
		PassID:    args.PassID,
	})
	if err != nil {
		return orchestratorWorkToolErr(appplans.PrepareHandoffContextTool, appplans.BlockerUnsafeRequest, fmt.Sprintf("service error: %v", err))
	}
	return orchestratorWorkPrepareHandoffContextPayload(resp)
}

// HandleGetNextAuditWork retrieves the next eligible audit work packet
// for a project-scoped managed plan.
func (s *Server) HandleGetNextAuditWork(rawArgs json.RawMessage) ToolCallResult {
	var args getNextAuditWorkArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return orchestratorWorkToolErr(appplans.NextAuditWorkTool, appplans.BlockerUnsafeRequest, "invalid params: "+err.Error())
	}

	if s.deps == nil || s.deps.Store == nil {
		return orchestratorWorkToolErr(appplans.NextAuditWorkTool, appplans.BlockerUnsafeRequest, "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	svc := appplans.NewOrchestratorWorkService(s.deps.Store)
	req := appplans.NextAuditWorkRequest{
		ProjectID: args.ProjectID,
		PlanID:    args.PlanID,
		PassID:    args.PassID,
		RunID:     args.RunID,
	}

	resp, err := svc.GetNextAuditWork(context.Background(), req)
	if err != nil {
		return orchestratorWorkToolErr(appplans.NextAuditWorkTool, appplans.BlockerUnsafeRequest, fmt.Sprintf("service error: %v", err))
	}

	return orchestratorWorkToolPayload(resp, false)
}
