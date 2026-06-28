package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/contextpackets"
	"relay/internal/sources"
	"relay/internal/store"
)

const brokerRawJSONByteLimit = 128 * 1024

var getProjectSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    }
  }
}`)

var getPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["plan_id"],
  "properties": {
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "include_raw": {
      "type": "boolean",
      "description": "Include bounded raw_plan_json when it fits within the safety cap."
    }
  }
}`)

var getPassSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["plan_id", "pass_id"],
  "properties": {
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay pass identifier within the plan."
    }
  }
}`)

var getPassContextSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["plan_id", "pass_id"],
  "properties": {
    "plan_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay pass identifier within the plan."
    },
    "include_latest_source_snapshot": {
      "type": "boolean",
      "description": "Include latest source snapshot metadata when available."
    },
    "include_latest_context_packet": {
      "type": "boolean",
      "description": "Include latest matching context packet metadata when available."
    }
  }
}`)

var createSourceSnapshotSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "repo_ids": {
      "type": "array",
      "description": "Optional registered repository IDs to snapshot.",
      "items": {
        "type": "string",
        "minLength": 1
      },
      "maxItems": 20
    },
    "include_file_metadata": {
      "type": "boolean",
      "description": "Capture bounded file metadata rows for the snapshot."
    },
    "max_files_per_repo": {
      "type": "integer",
      "description": "Maximum file metadata rows to capture per repository.",
      "minimum": 1,
      "maximum": 10000
    }
  }
}`)

var listProjectFilesSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "source_snapshot_id": {
      "type": "string",
      "description": "Optional source snapshot identifier; defaults to the latest project snapshot."
    },
    "repo_ids": {
      "type": "array",
      "description": "Optional repository IDs to limit the inventory.",
      "items": {
        "type": "string",
        "minLength": 1
      },
      "maxItems": 20
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of file rows to return.",
      "minimum": 1,
      "maximum": 10000
    }
  }
}`)

var searchProjectFilesSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "pattern"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "source_snapshot_id": {
      "type": "string",
      "description": "Optional source snapshot identifier; defaults to the latest project snapshot."
    },
    "repo_ids": {
      "type": "array",
      "description": "Optional repository IDs to search.",
      "items": {
        "type": "string",
        "minLength": 1
      },
      "maxItems": 20
    },
    "pattern": {
      "type": "string",
      "minLength": 1,
      "description": "Literal fixed-string pattern to search for."
    },
    "case_sensitive": {
      "type": "boolean",
      "description": "Match case exactly when true."
    },
    "context_lines": {
      "type": "integer",
      "description": "Context lines around each literal match.",
      "minimum": 0,
      "maximum": 3
    },
    "max_results": {
      "type": "integer",
      "description": "Maximum number of matches to return.",
      "minimum": 1,
      "maximum": 200
    },
    "max_bytes": {
      "type": "integer",
      "description": "Maximum rg stdout bytes to process.",
      "minimum": 1,
      "maximum": 1048576
    }
  }
}`)

var readProjectFileSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "repo_id", "path"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "source_snapshot_id": {
      "type": "string",
      "description": "Optional source snapshot identifier; defaults to the latest project snapshot."
    },
    "repo_id": {
      "type": "string",
      "minLength": 1,
      "description": "Registered repository identifier."
    },
    "path": {
      "type": "string",
      "minLength": 1,
      "description": "Repository-relative slash-delimited path."
    },
    "line_start": {
      "type": "integer",
      "description": "1-based starting line number.",
      "minimum": 1
    },
    "line_end": {
      "type": "integer",
      "description": "1-based inclusive ending line number.",
      "minimum": 1
    },
    "max_bytes": {
      "type": "integer",
      "description": "Maximum bytes to return.",
      "minimum": 1,
      "maximum": 262144
    }
  }
}`)

var createContextPacketSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "task_slug", "source_snapshot_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "plan_id": {
      "type": "string",
      "description": "Optional Relay plan identifier."
    },
    "pass_id": {
      "type": "string",
      "description": "Optional Relay pass identifier."
    },
    "task_slug": {
      "type": "string",
      "minLength": 1,
      "description": "Human-readable slug for the packet artifacts."
    },
    "source_snapshot_id": {
      "type": "string",
      "minLength": 1,
      "description": "Source snapshot identifier to ground the packet."
    },
    "seed_files": {
      "type": "array",
      "description": "Optional bounded file reads to include.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["repo_id", "path", "reason"],
        "properties": {
          "repo_id": { "type": "string", "minLength": 1 },
          "path": { "type": "string", "minLength": 1 },
          "line_start": { "type": "integer", "minimum": 1 },
          "line_end": { "type": "integer", "minimum": 1 },
          "reason": { "type": "string", "minLength": 1 },
          "required": { "type": "boolean" },
          "max_bytes": { "type": "integer", "minimum": 1, "maximum": 262144 }
        }
      },
      "maxItems": 100
    },
    "seed_searches": {
      "type": "array",
      "description": "Optional bounded literal searches to include.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["pattern", "reason"],
        "properties": {
          "repo_ids": {
            "type": "array",
            "items": { "type": "string", "minLength": 1 },
            "maxItems": 20
          },
          "pattern": { "type": "string", "minLength": 1 },
          "case_sensitive": { "type": "boolean" },
          "context_lines": { "type": "integer", "minimum": 0, "maximum": 3 },
          "max_results": { "type": "integer", "minimum": 1, "maximum": 200 },
          "reason": { "type": "string", "minLength": 1 },
          "required": { "type": "boolean" }
        }
      },
      "maxItems": 100
    },
    "include_inventory": {
      "type": "boolean",
      "description": "Include snapshot-backed file inventory metadata."
    },
    "max_sources": {
      "type": "integer",
      "description": "Maximum sources to retain in the packet.",
      "minimum": 1,
      "maximum": 200
    },
    "max_total_bytes": {
      "type": "integer",
      "description": "Maximum total source bytes to retain in the packet.",
      "minimum": 1,
      "maximum": 1048576
    }
  }
}`)

var getContextPacketSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["context_packet_id"],
  "properties": {
    "context_packet_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay context packet identifier."
    },
    "include_sources": {
      "type": "boolean",
      "description": "Include stored source metadata rows."
    }
  }
}`)

var (
	ToolGetProject = ToolDefinition{
		Name:        "get_project",
		Description: "Return bounded Relay project and registered repository metadata without local absolute paths.",
		InputSchema: getProjectSchema,
	}
	ToolGetPlan = ToolDefinition{
		Name:        "get_plan",
		Description: "Return bounded Relay plan metadata, persisted Plan v2 context JSON fields, and ordered pass summaries.",
		InputSchema: getPlanSchema,
	}
	ToolGetPass = ToolDefinition{
		Name:        "get_pass",
		Description: "Return bounded Relay pass metadata together with persisted context plan, snapshot requirements, and readiness criteria.",
		InputSchema: getPassSchema,
	}
	ToolGetPassContext = ToolDefinition{
		Name:        "get_pass_context",
		Description: "Return retrieval-only pass context plus optional latest source snapshot and context packet metadata.",
		InputSchema: getPassContextSchema,
	}
	ToolCreateSourceSnapshot = ToolDefinition{
		Name:        "create_source_snapshot",
		Description: "Create a bounded source snapshot for registered project repositories without exposing arbitrary filesystem paths or git mutation.",
		InputSchema: createSourceSnapshotSchema,
	}
	ToolListProjectFiles = ToolDefinition{
		Name:        "list_project_files",
		Description: "List bounded snapshot-backed project file metadata with provenance fields.",
		InputSchema: listProjectFilesSchema,
	}
	ToolSearchProjectFiles = ToolDefinition{
		Name:        "search_project_files",
		Description: "Search project files using bounded fixed-string matching only and return provenance-rich results.",
		InputSchema: searchProjectFilesSchema,
	}
	ToolReadProjectFile = ToolDefinition{
		Name:        "read_project_file",
		Description: "Read a bounded repository-relative file range from a source snapshot with stale-hash and redaction blockers.",
		InputSchema: readProjectFileSchema,
	}
	ToolCreateContextPacket = ToolDefinition{
		Name:        "create_context_packet",
		Description: "Create a bounded context packet from snapshots, file reads, searches, and optional inventory; returns metadata and artifact paths only.",
		InputSchema: createContextPacketSchema,
	}
	ToolGetContextPacket = ToolDefinition{
		Name:        "get_context_packet",
		Description: "Return stored context packet metadata and optional source metadata rows without returning full packet contents.",
		InputSchema: getContextPacketSchema,
	}
)

type brokerErrorPayload struct {
	OK    bool `json:"ok"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type brokerSuccessPayload struct {
	OK     bool        `json:"ok"`
	Tool   string      `json:"tool"`
	Result interface{} `json:"result"`
}

type getProjectArgs struct {
	ProjectID string `json:"project_id"`
}

type getPlanArgs struct {
	PlanID     string `json:"plan_id"`
	IncludeRaw bool   `json:"include_raw"`
}

type getPassArgs struct {
	PlanID string `json:"plan_id"`
	PassID string `json:"pass_id"`
}

type getPassContextArgs struct {
	PlanID                      string `json:"plan_id"`
	PassID                      string `json:"pass_id"`
	IncludeLatestSourceSnapshot bool   `json:"include_latest_source_snapshot"`
	IncludeLatestContextPacket  bool   `json:"include_latest_context_packet"`
}

type createSourceSnapshotArgs struct {
	ProjectID           string   `json:"project_id"`
	RepoIDs             []string `json:"repo_ids"`
	IncludeFileMetadata bool     `json:"include_file_metadata"`
	MaxFilesPerRepo     int      `json:"max_files_per_repo"`
}

type listProjectFilesArgs struct {
	ProjectID        string   `json:"project_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	RepoIDs          []string `json:"repo_ids"`
	MaxResults       int      `json:"max_results"`
}

type searchProjectFilesArgs struct {
	ProjectID        string   `json:"project_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	RepoIDs          []string `json:"repo_ids"`
	Pattern          string   `json:"pattern"`
	CaseSensitive    bool     `json:"case_sensitive"`
	ContextLines     int      `json:"context_lines"`
	MaxResults       int      `json:"max_results"`
	MaxBytes         int      `json:"max_bytes"`
}

type readProjectFileArgs struct {
	ProjectID        string `json:"project_id"`
	SourceSnapshotID string `json:"source_snapshot_id"`
	RepoID           string `json:"repo_id"`
	Path             string `json:"path"`
	LineStart        int    `json:"line_start"`
	LineEnd          int    `json:"line_end"`
	MaxBytes         int    `json:"max_bytes"`
}

type createContextPacketSeedFileArgs struct {
	RepoID    string `json:"repo_id"`
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Reason    string `json:"reason"`
	Required  bool   `json:"required"`
	MaxBytes  int    `json:"max_bytes"`
}

type createContextPacketSeedSearchArgs struct {
	RepoIDs       []string `json:"repo_ids"`
	Pattern       string   `json:"pattern"`
	CaseSensitive bool     `json:"case_sensitive"`
	ContextLines  int      `json:"context_lines"`
	MaxResults    int      `json:"max_results"`
	Reason        string   `json:"reason"`
	Required      bool     `json:"required"`
}

type createContextPacketArgs struct {
	ProjectID        string                              `json:"project_id"`
	PlanID           string                              `json:"plan_id"`
	PassID           string                              `json:"pass_id"`
	TaskSlug         string                              `json:"task_slug"`
	SourceSnapshotID string                              `json:"source_snapshot_id"`
	SeedFiles        []createContextPacketSeedFileArgs   `json:"seed_files"`
	SeedSearches     []createContextPacketSeedSearchArgs `json:"seed_searches"`
	IncludeInventory bool                                `json:"include_inventory"`
	MaxSources       int                                 `json:"max_sources"`
	MaxTotalBytes    int                                 `json:"max_total_bytes"`
}

type getContextPacketArgs struct {
	ContextPacketID string `json:"context_packet_id"`
	IncludeSources  bool   `json:"include_sources"`
}

type brokerProjectRepositoryResult struct {
	RepoID           string   `json:"repo_id"`
	Role             string   `json:"role"`
	DefaultBranch    string   `json:"default_branch"`
	Enabled          bool     `json:"enabled"`
	AllowedRoots     []string `json:"allowed_roots"`
	IgnoredGlobs     []string `json:"ignored_globs"`
	MaxFileSizeBytes int64    `json:"max_file_size_bytes"`
	IncludeUntracked bool     `json:"include_untracked"`
}

type brokerProjectResult struct {
	ProjectID           string                          `json:"project_id"`
	Name                string                          `json:"name"`
	Description         string                          `json:"description,omitempty"`
	Status              string                          `json:"status"`
	DefaultRepositoryID string                          `json:"default_repository_id,omitempty"`
	Repositories        []brokerProjectRepositoryResult `json:"repositories"`
}

type brokerPlanPassSummary struct {
	PassID       string          `json:"pass_id"`
	Sequence     int64           `json:"sequence"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	PassType     string          `json:"pass_type"`
	Dependencies json.RawMessage `json:"dependencies"`
}

type brokerPlanResult struct {
	PlanRowID            int64                   `json:"plan_row_id"`
	PlanID               string                  `json:"plan_id"`
	SchemaVersion        string                  `json:"schema_version"`
	Title                string                  `json:"title"`
	Goal                 string                  `json:"goal"`
	RepoTarget           string                  `json:"repo_target"`
	BranchContext        string                  `json:"branch_context"`
	Status               string                  `json:"status"`
	SourceIntentSummary  string                  `json:"source_intent_summary"`
	SourceArtifactPath   string                  `json:"source_artifact_path,omitempty"`
	CreatedAt            string                  `json:"created_at"`
	UpdatedAt            string                  `json:"updated_at"`
	PlanMeta             json.RawMessage         `json:"plan_meta"`
	ProjectContext       json.RawMessage         `json:"project_context"`
	MCPCapabilityProfile json.RawMessage         `json:"mcp_capability_profile"`
	GlobalContextRules   json.RawMessage         `json:"global_context_rules"`
	RawPlanJSON          json.RawMessage         `json:"raw_plan_json,omitempty"`
	RawPlanJSONTruncated bool                    `json:"raw_plan_json_truncated,omitempty"`
	Passes               []brokerPlanPassSummary `json:"passes"`
}

type brokerPassResult struct {
	PlanRowID                  int64           `json:"plan_row_id"`
	PassRowID                  int64           `json:"pass_row_id"`
	PlanID                     string          `json:"plan_id"`
	PassID                     string          `json:"pass_id"`
	Sequence                   int64           `json:"sequence"`
	Name                       string          `json:"name"`
	Goal                       string          `json:"goal"`
	Status                     string          `json:"status"`
	PassType                   string          `json:"pass_type"`
	IntendedExecutionScope     json.RawMessage `json:"intended_execution_scope"`
	NonGoals                   json.RawMessage `json:"non_goals"`
	Dependencies               json.RawMessage `json:"dependencies"`
	ContextPlan                json.RawMessage `json:"context_plan"`
	SourceSnapshotRequirements json.RawMessage `json:"source_snapshot_requirements"`
	HandoffReadinessCriteria   json.RawMessage `json:"handoff_readiness_criteria"`
	RiskLevel                  string          `json:"risk_level,omitempty"`
	ContextBudget              json.RawMessage `json:"context_budget"`
	CreatedAt                  string          `json:"created_at"`
	UpdatedAt                  string          `json:"updated_at"`
}

type brokerSourceSnapshotMetadata struct {
	SourceSnapshotID string          `json:"source_snapshot_id"`
	ProjectID        string          `json:"project_id"`
	SnapshotKind     string          `json:"snapshot_kind"`
	Status           string          `json:"status"`
	CreatedAt        string          `json:"created_at"`
	CompletedAt      string          `json:"completed_at,omitempty"`
	Summary          json.RawMessage `json:"summary"`
}

type brokerContextPacketMetadata struct {
	ContextPacketID    string          `json:"context_packet_id"`
	ProjectID          string          `json:"project_id"`
	PlanID             string          `json:"plan_id,omitempty"`
	PassID             string          `json:"pass_id,omitempty"`
	TaskSlug           string          `json:"task_slug"`
	SourceSnapshotID   string          `json:"source_snapshot_id"`
	Status             string          `json:"status"`
	PacketJSONPath     string          `json:"packet_json_path,omitempty"`
	PacketMarkdownPath string          `json:"packet_markdown_path,omitempty"`
	CoverageReportPath string          `json:"coverage_report_path,omitempty"`
	SourceCount        int64           `json:"source_count"`
	CoveredSeedCount   int64           `json:"covered_seed_count"`
	BlockedSeedCount   int64           `json:"blocked_seed_count"`
	MissingSeedCount   int64           `json:"missing_seed_count"`
	Truncated          bool            `json:"truncated"`
	Blockers           json.RawMessage `json:"blockers"`
	Summary            json.RawMessage `json:"summary"`
	CreatedAt          string          `json:"created_at"`
	CompletedAt        string          `json:"completed_at,omitempty"`
}

type brokerPassContextResult struct {
	ProjectID                  string                        `json:"project_id"`
	PlanID                     string                        `json:"plan_id"`
	PassID                     string                        `json:"pass_id"`
	ContextPlan                json.RawMessage               `json:"context_plan"`
	SourceSnapshotRequirements json.RawMessage               `json:"source_snapshot_requirements"`
	HandoffReadinessCriteria   json.RawMessage               `json:"handoff_readiness_criteria"`
	RiskLevel                  string                        `json:"risk_level,omitempty"`
	ContextBudget              json.RawMessage               `json:"context_budget"`
	LatestSourceSnapshot       *brokerSourceSnapshotMetadata `json:"latest_source_snapshot,omitempty"`
	LatestContextPacket        *brokerContextPacketMetadata  `json:"latest_context_packet,omitempty"`
	CoverageReadiness          map[string]bool               `json:"coverage_readiness"`
	HandoffReadiness           brokerHandoffReadinessResult  `json:"handoff_readiness"`
}

type brokerMissingEvidenceResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type brokerNextContextActionResult struct {
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

type brokerHandoffReadinessResult struct {
	Status                 string                          `json:"status"`
	ReadyForHandoff        bool                            `json:"ready_for_handoff"`
	RequiresSourceSnapshot bool                            `json:"requires_source_snapshot"`
	RequiresContextPacket  bool                            `json:"requires_context_packet"`
	SourceSnapshotID       string                          `json:"source_snapshot_id,omitempty"`
	ContextPacketID        string                          `json:"context_packet_id,omitempty"`
	CoverageReportPath     string                          `json:"coverage_report_path,omitempty"`
	MissingEvidence        []brokerMissingEvidenceResult   `json:"missing_evidence,omitempty"`
	NextActions            []brokerNextContextActionResult `json:"next_actions,omitempty"`
}

type brokerRepositorySnapshotResult struct {
	RepoID            string                      `json:"repo_id"`
	Role              string                      `json:"role"`
	DefaultBranch     string                      `json:"default_branch"`
	GitStatus         sources.RepositoryGitStatus `json:"git_status"`
	RecentCommit      *brokerRecentCommit         `json:"recent_commit,omitempty"`
	FileCount         int                         `json:"file_count"`
	IncludedFileCount int                         `json:"included_file_count"`
}

type brokerRecentCommit struct {
	RepoID     string `json:"repo_id"`
	CommitSHA  string `json:"commit_sha"`
	AuthorName string `json:"author_name"`
	AuthorDate string `json:"author_date"`
	Subject    string `json:"subject"`
}

type brokerSourceSnapshotResult struct {
	SourceSnapshotID string                           `json:"source_snapshot_id"`
	ProjectID        string                           `json:"project_id"`
	SnapshotKind     string                           `json:"snapshot_kind"`
	Status           string                           `json:"status"`
	Repositories     []brokerRepositorySnapshotResult `json:"repositories"`
	Blockers         []sources.SourceBlocker          `json:"blockers"`
}

type brokerContextPacketSourceResult struct {
	SourceID         string `json:"source_id"`
	SourceType       string `json:"source_type"`
	ProjectID        string `json:"project_id"`
	RepoID           string `json:"repo_id"`
	SourceSnapshotID string `json:"source_snapshot_id"`
	Path             string `json:"path"`
	LineStart        int64  `json:"line_start,omitempty"`
	LineEnd          int64  `json:"line_end,omitempty"`
	ContentHash      string `json:"content_hash,omitempty"`
	SnippetHash      string `json:"snippet_hash,omitempty"`
	RedactionStatus  string `json:"redaction_status"`
	Truncated        bool   `json:"truncated"`
	GeneratedAt      string `json:"generated_at"`
	Reason           string `json:"reason,omitempty"`
	CreatedAt        string `json:"created_at"`
}

type brokerContextPacketResult struct {
	ContextPacketID    string                            `json:"context_packet_id"`
	ProjectID          string                            `json:"project_id"`
	PlanID             string                            `json:"plan_id,omitempty"`
	PassID             string                            `json:"pass_id,omitempty"`
	TaskSlug           string                            `json:"task_slug"`
	SourceSnapshotID   string                            `json:"source_snapshot_id"`
	Status             string                            `json:"status"`
	PacketJSONPath     string                            `json:"packet_json_path,omitempty"`
	PacketMarkdownPath string                            `json:"packet_markdown_path,omitempty"`
	CoverageReportPath string                            `json:"coverage_report_path,omitempty"`
	SourceCount        int64                             `json:"source_count"`
	CoveredSeedCount   int64                             `json:"covered_seed_count"`
	BlockedSeedCount   int64                             `json:"blocked_seed_count"`
	MissingSeedCount   int64                             `json:"missing_seed_count"`
	Truncated          bool                              `json:"truncated"`
	Blockers           json.RawMessage                   `json:"blockers"`
	Summary            json.RawMessage                   `json:"summary"`
	CreatedAt          string                            `json:"created_at"`
	CompletedAt        string                            `json:"completed_at,omitempty"`
	Sources            []brokerContextPacketSourceResult `json:"sources,omitempty"`
}

type brokerSourceBlockerResult struct {
	RepoID  string `json:"repo_id,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type brokerGitStatusResult struct {
	RepoID             string `json:"repo_id"`
	CurrentBranch      string `json:"current_branch"`
	HeadSHA            string `json:"head_sha"`
	Dirty              bool   `json:"dirty"`
	StagedCount        int    `json:"staged_count"`
	UnstagedCount      int    `json:"unstaged_count"`
	UntrackedCount     int    `json:"untracked_count"`
	ChangedFileCount   int    `json:"changed_file_count"`
	PorcelainHash      string `json:"porcelain_hash"`
	GitStatusAvailable bool   `json:"git_status_available"`
	GitError           string `json:"git_error,omitempty"`
}

type brokerSourceFileRecord struct {
	ProjectID        string `json:"project_id"`
	RepoID           string `json:"repo_id"`
	SourceSnapshotID string `json:"source_snapshot_id"`
	Path             string `json:"path"`
	SizeBytes        int64  `json:"size_bytes"`
	ContentHash      string `json:"content_hash"`
	HashAlgorithm    string `json:"hash_algorithm"`
	Tracked          bool   `json:"tracked"`
	Included         bool   `json:"included"`
	ExclusionReason  string `json:"exclusion_reason,omitempty"`
	RedactionStatus  string `json:"redaction_status"`
	IndexedAt        string `json:"indexed_at"`
}

type brokerFileInventoryResult struct {
	ProjectID        string                      `json:"project_id"`
	SourceSnapshotID string                      `json:"source_snapshot_id"`
	Files            []brokerSourceFileRecord    `json:"files"`
	Truncated        bool                        `json:"truncated"`
	Blockers         []brokerSourceBlockerResult `json:"blockers,omitempty"`
	GeneratedAt      string                      `json:"generated_at"`
}

type brokerSourceSearchMatchResult struct {
	ProjectID        string `json:"project_id"`
	RepoID           string `json:"repo_id"`
	SourceSnapshotID string `json:"source_snapshot_id"`
	Path             string `json:"path"`
	LineStart        int    `json:"line_start"`
	LineEnd          int    `json:"line_end"`
	Snippet          string `json:"snippet"`
	SnippetHash      string `json:"snippet_hash"`
	ContentHash      string `json:"content_hash"`
	RedactionStatus  string `json:"redaction_status"`
	Truncated        bool   `json:"truncated"`
	GeneratedAt      string `json:"generated_at"`
}

type brokerSourceSearchResult struct {
	ProjectID        string                          `json:"project_id"`
	SourceSnapshotID string                          `json:"source_snapshot_id"`
	Matches          []brokerSourceSearchMatchResult `json:"matches"`
	Truncated        bool                            `json:"truncated"`
	Blockers         []brokerSourceBlockerResult     `json:"blockers,omitempty"`
	GeneratedAt      string                          `json:"generated_at"`
}

type brokerBoundedFileReadResult struct {
	ProjectID        string                      `json:"project_id"`
	RepoID           string                      `json:"repo_id"`
	SourceSnapshotID string                      `json:"source_snapshot_id"`
	Path             string                      `json:"path"`
	LineStart        int                         `json:"line_start"`
	LineEnd          int                         `json:"line_end"`
	Content          string                      `json:"content,omitempty"`
	ContentHash      string                      `json:"content_hash,omitempty"`
	CurrentHash      string                      `json:"current_hash,omitempty"`
	SnippetHash      string                      `json:"snippet_hash,omitempty"`
	RedactionStatus  string                      `json:"redaction_status,omitempty"`
	Truncated        bool                        `json:"truncated"`
	GeneratedAt      string                      `json:"generated_at"`
	Blockers         []brokerSourceBlockerResult `json:"blockers,omitempty"`
}

func contextBrokerToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		ToolGetProject,
		ToolGetPlan,
		ToolGetPass,
		ToolGetPassContext,
		ToolGetNextPassWork,
		ToolGetNextAuditWork,
		ToolCreateSourceSnapshot,
		ToolListProjectFiles,
		ToolSearchProjectFiles,
		ToolReadProjectFile,
		ToolGetRepositoryGitStatus,
		ToolGetRepositoryRecentCommit,
		ToolListRepositoryChangedFiles,
		ToolGetRepositoryDiff,
		ToolCreateContextPacket,
		ToolGetContextPacket,
		ToolCreateLocalAudit,
		ToolGetLocalAudit,
		ToolListProjectLocalAudits,
		ToolSearchProjectContextMemory,
		ToolListProjectContextRecords,
		ToolGetProjectContextRecord,
		ToolCreateProjectContextRecord,
		ToolSupersedeProjectContextRecord,
	}
}

func (s *Server) HandleGetProject(rawArgs json.RawMessage) ToolCallResult {
	var args getProjectArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	projectID := strings.TrimSpace(args.ProjectID)
	if projectID == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id is required")
	}

	project, repos, err := s.loadProjectWithRepos(projectID)
	if err != nil {
		return brokerWrappedErr(err)
	}

	result := brokerProjectResult{
		ProjectID:           project.ProjectID,
		Name:                project.Name,
		Description:         project.Description,
		Status:              project.Status,
		DefaultRepositoryID: project.DefaultRepositoryID,
		Repositories:        make([]brokerProjectRepositoryResult, 0, len(repos)),
	}
	for _, repo := range repos {
		result.Repositories = append(result.Repositories, brokerProjectRepositoryResult{
			RepoID:           repo.RepoID,
			Role:             repo.Role,
			DefaultBranch:    repo.DefaultBranch,
			Enabled:          repo.Enabled == 1,
			AllowedRoots:     brokerDecodeStringArray(repo.AllowedRootsJson),
			IgnoredGlobs:     brokerDecodeStringArray(repo.IgnoredGlobsJson),
			MaxFileSizeBytes: repo.MaxFileSizeBytes,
			IncludeUntracked: repo.IncludeUntracked == 1,
		})
	}
	return brokerToolOK(ToolGetProject.Name, result)
}

func (s *Server) HandleGetPlan(rawArgs json.RawMessage) ToolCallResult {
	var args getPlanArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	planID := strings.TrimSpace(args.PlanID)
	if planID == "" {
		return brokerToolErr("VALIDATION_ERROR", "plan_id is required")
	}

	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	plan, err := s.deps.Store.GetPlanByPlanID(planID)
	if errors.Is(err, sql.ErrNoRows) {
		return brokerToolErr("NOT_FOUND", fmt.Sprintf("plan %q not found", planID))
	}
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to load plan")
	}
	passes, err := s.deps.Store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to list plan passes")
	}

	planMeta, err := brokerJSONField(plan.PlanMetaJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode plan_meta_json")
	}
	projectContext, err := brokerJSONField(plan.ProjectContextJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode project_context_json")
	}
	mcpCapabilityProfile, err := brokerJSONField(plan.McpCapabilityProfileJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode mcp_capability_profile_json")
	}
	globalContextRules, err := brokerJSONField(plan.GlobalContextRulesJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode global_context_rules_json")
	}

	result := brokerPlanResult{
		PlanRowID:            plan.ID,
		PlanID:               plan.PlanID,
		SchemaVersion:        plan.SchemaVersion,
		Title:                plan.Title,
		Goal:                 plan.Goal,
		RepoTarget:           plan.RepoTarget,
		BranchContext:        plan.BranchContext,
		Status:               plan.Status,
		SourceIntentSummary:  plan.SourceIntentSummary,
		SourceArtifactPath:   brokerSafeArtifactPath(plan.SourceArtifactPath),
		CreatedAt:            plan.CreatedAt,
		UpdatedAt:            plan.UpdatedAt,
		PlanMeta:             planMeta,
		ProjectContext:       projectContext,
		MCPCapabilityProfile: mcpCapabilityProfile,
		GlobalContextRules:   globalContextRules,
		Passes:               make([]brokerPlanPassSummary, 0, len(passes)),
	}
	for _, pass := range passes {
		dependencies, err := brokerJSONField(pass.DependenciesJson, "[]")
		if err != nil {
			return brokerToolErr("INTERNAL_ERROR", "failed to decode pass dependencies")
		}
		result.Passes = append(result.Passes, brokerPlanPassSummary{
			PassID:       pass.PassID,
			Sequence:     pass.Sequence,
			Name:         pass.Name,
			Status:       pass.Status,
			PassType:     pass.PassType,
			Dependencies: dependencies,
		})
	}
	if args.IncludeRaw {
		if raw, truncated, ok := brokerBoundedRawJSON(plan.RawPlanJson); ok {
			result.RawPlanJSON = raw
			result.RawPlanJSONTruncated = truncated
		} else {
			result.RawPlanJSONTruncated = true
		}
	}
	return brokerToolOK(ToolGetPlan.Name, result)
}

func (s *Server) HandleGetPass(rawArgs json.RawMessage) ToolCallResult {
	var args getPassArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	pass, plan, err := s.loadPlanPass(strings.TrimSpace(args.PlanID), strings.TrimSpace(args.PassID))
	if err != nil {
		return brokerWrappedErr(err)
	}
	result, err := brokerBuildPassResult(plan, pass)
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", err.Error())
	}
	return brokerToolOK(ToolGetPass.Name, result)
}

func (s *Server) HandleGetPassContext(rawArgs json.RawMessage) ToolCallResult {
	var args getPassContextArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	pass, plan, err := s.loadPlanPass(strings.TrimSpace(args.PlanID), strings.TrimSpace(args.PassID))
	if err != nil {
		return brokerWrappedErr(err)
	}

	contextPlan, err := brokerJSONField(pass.ContextPlanJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode context_plan_json")
	}
	sourceRequirements, err := brokerJSONField(pass.SourceSnapshotRequirementsJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode source_snapshot_requirements_json")
	}
	readiness, err := brokerJSONField(pass.HandoffReadinessCriteriaJson, "[]")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode handoff_readiness_criteria_json")
	}
	contextBudget, err := brokerJSONField(pass.ContextBudgetJson, "{}")
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to decode context_budget_json")
	}

	result := brokerPassContextResult{
		ProjectID:                  "",
		PlanID:                     plan.PlanID,
		PassID:                     pass.PassID,
		ContextPlan:                contextPlan,
		SourceSnapshotRequirements: sourceRequirements,
		HandoffReadinessCriteria:   readiness,
		RiskLevel:                  pass.RiskLevel,
		ContextBudget:              contextBudget,
		CoverageReadiness: map[string]bool{
			"source_snapshot_available": false,
			"context_packet_available":  false,
			"ready_for_handoff":         false,
		},
	}

	projectID := strings.TrimSpace(brokerProjectIDFromPlan(plan))
	if projectID == "" {
		projectID = brokerProjectIDFromPass(pass)
	}
	if projectID == "" {
		projectID = plan.RepoTarget
	}
	result.ProjectID = projectID
	latestSnapshot, snapshotOK, err := s.loadLatestSourceSnapshotMetadata(projectID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	if snapshotOK {
		if args.IncludeLatestSourceSnapshot {
			result.LatestSourceSnapshot = latestSnapshot
		}
		result.CoverageReadiness["source_snapshot_available"] = true
	}

	latestPacket, packetOK, err := s.loadLatestContextPacketMetadata(projectID, plan.PlanID, pass.PassID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	if packetOK {
		if args.IncludeLatestContextPacket {
			result.LatestContextPacket = latestPacket
		}
		result.CoverageReadiness["context_packet_available"] = true
	}
	result.HandoffReadiness = brokerBuildHandoffReadiness(pass, plan, latestSnapshot, latestPacket, contextPlan, sourceRequirements)
	result.CoverageReadiness["ready_for_handoff"] = result.HandoffReadiness.ReadyForHandoff
	return brokerToolOK(ToolGetPassContext.Name, result)
}

func (s *Server) HandleCreateSourceSnapshot(rawArgs json.RawMessage) ToolCallResult {
	var args createSourceSnapshotArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if strings.TrimSpace(args.ProjectID) == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id is required")
	}
	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	result, err := sources.NewService(s.deps.Store).CreateSourceSnapshot(context.Background(), sources.SourceSnapshotInput{
		ProjectID:           strings.TrimSpace(args.ProjectID),
		RepoIDs:             args.RepoIDs,
		IncludeFileMetadata: args.IncludeFileMetadata,
		MaxFilesPerRepo:     args.MaxFilesPerRepo,
	})
	if err != nil {
		return brokerToolErr("VALIDATION_ERROR", err.Error())
	}
	sanitized := brokerSourceSnapshotResult{
		SourceSnapshotID: result.SourceSnapshotID,
		ProjectID:        result.ProjectID,
		SnapshotKind:     result.SnapshotKind,
		Status:           result.Status,
		Repositories:     make([]brokerRepositorySnapshotResult, 0, len(result.Repositories)),
		Blockers:         result.Blockers,
	}
	for _, repo := range result.Repositories {
		snapshotRepo := brokerRepositorySnapshotResult{
			RepoID:        repo.RepoID,
			Role:          repo.Role,
			DefaultBranch: repo.DefaultBranch,
			GitStatus: sources.RepositoryGitStatus{
				RepoID:             repo.GitStatus.RepoID,
				CurrentBranch:      repo.GitStatus.CurrentBranch,
				HeadSHA:            repo.GitStatus.HeadSHA,
				Dirty:              repo.GitStatus.Dirty,
				StagedCount:        repo.GitStatus.StagedCount,
				UnstagedCount:      repo.GitStatus.UnstagedCount,
				UntrackedCount:     repo.GitStatus.UntrackedCount,
				ChangedFileCount:   repo.GitStatus.ChangedFileCount,
				PorcelainHash:      repo.GitStatus.PorcelainHash,
				GitStatusAvailable: repo.GitStatus.GitStatusAvailable,
				GitError:           repo.GitStatus.GitError,
			},
			FileCount:         repo.FileCount,
			IncludedFileCount: repo.IncludedFileCount,
		}
		if repo.RecentCommit != nil {
			snapshotRepo.RecentCommit = &brokerRecentCommit{
				RepoID:     repo.RecentCommit.RepoID,
				CommitSHA:  repo.RecentCommit.CommitSHA,
				AuthorName: repo.RecentCommit.AuthorName,
				AuthorDate: repo.RecentCommit.AuthorDate,
				Subject:    repo.RecentCommit.Subject,
			}
		}
		sanitized.Repositories = append(sanitized.Repositories, snapshotRepo)
	}
	return brokerToolOK(ToolCreateSourceSnapshot.Name, sanitized)
}

func (s *Server) HandleListProjectFiles(rawArgs json.RawMessage) ToolCallResult {
	var args listProjectFilesArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if strings.TrimSpace(args.ProjectID) == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id is required")
	}
	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := sources.NewService(s.deps.Store).ListProjectFiles(context.Background(), sources.FileInventoryInput{
		ProjectID:        strings.TrimSpace(args.ProjectID),
		SourceSnapshotID: strings.TrimSpace(args.SourceSnapshotID),
		RepoIDs:          args.RepoIDs,
		MaxResults:       args.MaxResults,
	})
	if err != nil {
		return brokerWrappedErr(err)
	}
	return brokerToolOK(ToolListProjectFiles.Name, brokerFileInventoryFromResult(result))
}

func (s *Server) HandleSearchProjectFiles(rawArgs json.RawMessage) ToolCallResult {
	var args searchProjectFilesArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if strings.TrimSpace(args.ProjectID) == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id is required")
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return brokerToolErr("VALIDATION_ERROR", "pattern is required")
	}
	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := sources.NewService(s.deps.Store).SearchProjectFiles(context.Background(), sources.SourceSearchInput{
		ProjectID:        strings.TrimSpace(args.ProjectID),
		SourceSnapshotID: strings.TrimSpace(args.SourceSnapshotID),
		RepoIDs:          args.RepoIDs,
		Pattern:          strings.TrimSpace(args.Pattern),
		Literal:          true,
		CaseSensitive:    args.CaseSensitive,
		ContextLines:     args.ContextLines,
		MaxResults:       args.MaxResults,
		MaxBytes:         args.MaxBytes,
	})
	if err != nil {
		return brokerToolErr("VALIDATION_ERROR", err.Error())
	}
	return brokerToolOK(ToolSearchProjectFiles.Name, brokerSourceSearchFromResult(result))
}

func (s *Server) HandleReadProjectFile(rawArgs json.RawMessage) ToolCallResult {
	var args readProjectFileArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if strings.TrimSpace(args.ProjectID) == "" || strings.TrimSpace(args.RepoID) == "" || strings.TrimSpace(args.Path) == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id, repo_id, and path are required")
	}
	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}
	result, err := sources.NewService(s.deps.Store).ReadProjectFile(context.Background(), sources.BoundedFileReadInput{
		ProjectID:        strings.TrimSpace(args.ProjectID),
		SourceSnapshotID: strings.TrimSpace(args.SourceSnapshotID),
		RepoID:           strings.TrimSpace(args.RepoID),
		Path:             strings.TrimSpace(args.Path),
		LineStart:        args.LineStart,
		LineEnd:          args.LineEnd,
		MaxBytes:         args.MaxBytes,
	})
	if err != nil {
		return brokerWrappedErr(err)
	}
	return brokerToolOK(ToolReadProjectFile.Name, brokerBoundedFileReadFromResult(result))
}

func (s *Server) HandleCreateContextPacket(rawArgs json.RawMessage) ToolCallResult {
	var args createContextPacketArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	if strings.TrimSpace(args.ProjectID) == "" || strings.TrimSpace(args.TaskSlug) == "" || strings.TrimSpace(args.SourceSnapshotID) == "" {
		return brokerToolErr("VALIDATION_ERROR", "project_id, task_slug, and source_snapshot_id are required")
	}
	if !args.IncludeInventory && len(args.SeedFiles) == 0 && len(args.SeedSearches) == 0 {
		return brokerToolErr("VALIDATION_ERROR", "at least one seed file, seed search, or inventory request is required")
	}
	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	input := contextpackets.ContextPacketInput{
		ProjectID:        strings.TrimSpace(args.ProjectID),
		PlanID:           strings.TrimSpace(args.PlanID),
		PassID:           strings.TrimSpace(args.PassID),
		TaskSlug:         strings.TrimSpace(args.TaskSlug),
		SourceSnapshotID: strings.TrimSpace(args.SourceSnapshotID),
		IncludeInventory: args.IncludeInventory,
		MaxSources:       args.MaxSources,
		MaxTotalBytes:    args.MaxTotalBytes,
		SeedFiles:        make([]contextpackets.ContextSeedFile, 0, len(args.SeedFiles)),
		SeedSearches:     make([]contextpackets.ContextSeedSearch, 0, len(args.SeedSearches)),
	}
	for _, seed := range args.SeedFiles {
		input.SeedFiles = append(input.SeedFiles, contextpackets.ContextSeedFile{
			RepoID:    strings.TrimSpace(seed.RepoID),
			Path:      strings.TrimSpace(seed.Path),
			LineStart: seed.LineStart,
			LineEnd:   seed.LineEnd,
			Reason:    strings.TrimSpace(seed.Reason),
			Required:  seed.Required,
			MaxBytes:  seed.MaxBytes,
		})
	}
	for _, seed := range args.SeedSearches {
		input.SeedSearches = append(input.SeedSearches, contextpackets.ContextSeedSearch{
			RepoIDs:       nonEmptyToolStrings(seed.RepoIDs),
			Pattern:       strings.TrimSpace(seed.Pattern),
			Literal:       true,
			CaseSensitive: seed.CaseSensitive,
			ContextLines:  seed.ContextLines,
			MaxResults:    seed.MaxResults,
			Reason:        strings.TrimSpace(seed.Reason),
			Required:      seed.Required,
		})
	}

	result, err := contextpackets.NewService(s.deps.Store).CreateContextPacket(context.Background(), input)
	if err != nil {
		return brokerToolErr("VALIDATION_ERROR", err.Error())
	}
	return brokerToolOK(ToolCreateContextPacket.Name, map[string]interface{}{
		"context_packet_id":    result.ContextPacketID,
		"project_id":           result.ProjectID,
		"plan_id":              result.PlanID,
		"pass_id":              result.PassID,
		"source_snapshot_id":   result.SourceSnapshotID,
		"status":               result.Status,
		"packet_json_path":     brokerSafeArtifactPath(result.PacketJSONPath),
		"packet_markdown_path": brokerSafeArtifactPath(result.PacketMarkdownPath),
		"coverage_report_path": brokerSafeArtifactPath(result.CoverageReportPath),
		"source_count":         result.SourceCount,
		"covered_seed_count":   result.CoveredSeedCount,
		"blocked_seed_count":   result.BlockedSeedCount,
		"missing_seed_count":   result.MissingSeedCount,
		"truncated":            result.Truncated,
		"blockers":             brokerBlockers(result.Blockers),
	})
}

func (s *Server) HandleGetContextPacket(rawArgs json.RawMessage) ToolCallResult {
	var args getContextPacketArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	contextPacketID := strings.TrimSpace(args.ContextPacketID)
	if contextPacketID == "" {
		return brokerToolErr("VALIDATION_ERROR", "context_packet_id is required")
	}
	if s.deps == nil || s.deps.Store == nil {
		return brokerToolErr("DEPENDENCY_ERROR", "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set")
	}

	row, err := s.deps.Store.GetContextPacketByID(contextPacketID)
	if errors.Is(err, sql.ErrNoRows) {
		return brokerToolErr("NOT_FOUND", fmt.Sprintf("context packet %q not found", contextPacketID))
	}
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to load context packet")
	}
	result, err := brokerContextPacketRowResult(row)
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", err.Error())
	}
	if args.IncludeSources {
		rows, err := s.deps.Store.ListContextPacketSources(row.ID)
		if err != nil {
			return brokerToolErr("INTERNAL_ERROR", "failed to list context packet sources")
		}
		result.Sources = make([]brokerContextPacketSourceResult, 0, len(rows))
		for _, source := range rows {
			result.Sources = append(result.Sources, brokerContextPacketSourceResult{
				SourceID:         source.SourceID,
				SourceType:       source.SourceType,
				ProjectID:        source.ProjectID,
				RepoID:           source.RepoID,
				SourceSnapshotID: source.SourceSnapshotID,
				Path:             source.Path,
				LineStart:        source.LineStart,
				LineEnd:          source.LineEnd,
				ContentHash:      source.ContentHash,
				SnippetHash:      source.SnippetHash,
				RedactionStatus:  source.RedactionStatus,
				Truncated:        source.Truncated == 1,
				GeneratedAt:      source.GeneratedAt,
				Reason:           source.Reason,
				CreatedAt:        source.CreatedAt,
			})
		}
	}
	return brokerToolOK(ToolGetContextPacket.Name, result)
}

func (s *Server) loadProjectWithRepos(projectID string) (*store.Project, []store.ProjectRepository, error) {
	if s.deps == nil || s.deps.Store == nil {
		return nil, nil, brokerOpError{Code: "DEPENDENCY_ERROR", Message: "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set"}
	}
	project, err := s.deps.Store.GetProjectByProjectID(projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, brokerOpError{Code: "NOT_FOUND", Message: fmt.Sprintf("project %q not found", projectID)}
	}
	if err != nil {
		return nil, nil, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to load project"}
	}
	repos, err := s.deps.Store.ListProjectRepositories(project.ID)
	if err != nil {
		return nil, nil, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to list project repositories"}
	}
	return project, repos, nil
}

func (s *Server) loadPlanPass(planID string, passID string) (*store.PlanPass, *store.Plan, error) {
	if planID == "" || passID == "" {
		return nil, nil, brokerOpError{Code: "VALIDATION_ERROR", Message: "plan_id and pass_id are required"}
	}
	if s.deps == nil || s.deps.Store == nil {
		return nil, nil, brokerOpError{Code: "DEPENDENCY_ERROR", Message: "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set"}
	}
	plan, err := s.deps.Store.GetPlanByPlanID(planID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, brokerOpError{Code: "NOT_FOUND", Message: fmt.Sprintf("plan %q not found", planID)}
	}
	if err != nil {
		return nil, nil, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to load plan"}
	}
	pass, err := s.deps.Store.GetPlanPassByPassID(plan.ID, passID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, brokerOpError{Code: "NOT_FOUND", Message: fmt.Sprintf("pass %q not found under plan %q", passID, planID)}
	}
	if err != nil {
		return nil, nil, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to load pass"}
	}
	return pass, plan, nil
}

func (s *Server) loadLatestSourceSnapshotMetadata(projectID string) (*brokerSourceSnapshotMetadata, bool, error) {
	if projectID == "" {
		return nil, false, nil
	}
	project, _, err := s.loadProjectWithRepos(projectID)
	if err != nil {
		var opErr brokerOpError
		if errors.As(err, &opErr) && opErr.Code == "NOT_FOUND" {
			return nil, false, nil
		}
		return nil, false, err
	}
	row, err := s.deps.Store.GetLatestSourceSnapshotForProject(project.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to load latest source snapshot"}
	}
	summary, err := brokerJSONField(row.SummaryJson, "{}")
	if err != nil {
		return nil, false, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to decode source snapshot summary"}
	}
	return &brokerSourceSnapshotMetadata{
		SourceSnapshotID: row.SourceSnapshotID,
		ProjectID:        row.ProjectID,
		SnapshotKind:     row.SnapshotKind,
		Status:           row.Status,
		CreatedAt:        row.CreatedAt,
		CompletedAt:      row.CompletedAt,
		Summary:          summary,
	}, true, nil
}

func (s *Server) loadLatestContextPacketMetadata(projectID string, planID string, passID string) (*brokerContextPacketMetadata, bool, error) {
	if projectID == "" {
		return nil, false, nil
	}
	if s.deps == nil || s.deps.Store == nil {
		return nil, false, brokerOpError{Code: "DEPENDENCY_ERROR", Message: "MCP server is not connected to a Relay store; start with RELAY_DB_PATH set"}
	}
	rows, err := s.deps.Store.ListContextPacketsByProject(projectID)
	if err != nil {
		return nil, false, brokerOpError{Code: "INTERNAL_ERROR", Message: "failed to list context packets"}
	}
	for _, row := range rows {
		if row.PlanID == planID && row.PassID == passID {
			result, err := brokerContextPacketMetadataFromRow(&row)
			if err != nil {
				return nil, false, brokerOpError{Code: "INTERNAL_ERROR", Message: err.Error()}
			}
			return result, true, nil
		}
	}
	return nil, false, nil
}

func brokerBuildHandoffReadiness(pass *store.PlanPass, plan *store.Plan, latestSnapshot *brokerSourceSnapshotMetadata, latestPacket *brokerContextPacketMetadata, contextPlan json.RawMessage, sourceRequirements json.RawMessage) brokerHandoffReadinessResult {
	requiresContextPacket := brokerContextPlanRequiresPacket(contextPlan)
	requiresSourceSnapshot := requiresContextPacket || brokerSourceRequirementsRequireSnapshot(sourceRequirements)
	result := brokerHandoffReadinessResult{
		Status:                 "ready",
		ReadyForHandoff:        true,
		RequiresSourceSnapshot: requiresSourceSnapshot,
		RequiresContextPacket:  requiresContextPacket,
	}
	if latestSnapshot != nil {
		result.SourceSnapshotID = latestSnapshot.SourceSnapshotID
	}
	if latestPacket != nil {
		result.ContextPacketID = latestPacket.ContextPacketID
		result.CoverageReportPath = brokerSafeArtifactPath(latestPacket.CoverageReportPath)
		if result.SourceSnapshotID == "" {
			result.SourceSnapshotID = latestPacket.SourceSnapshotID
		}
	}

	if requiresSourceSnapshot && latestSnapshot == nil {
		result.MissingEvidence = append(result.MissingEvidence, brokerMissingEvidenceResult{
			Code:    "source_snapshot_missing",
			Message: "A source snapshot is required before context packet evidence can ground this pass handoff.",
		})
		result.NextActions = append(result.NextActions, brokerNextContextActionResult{
			Tool:   "create_source_snapshot",
			Reason: "Capture the selected pass repository source state before gathering context evidence.",
		})
	}
	if requiresContextPacket && latestPacket == nil {
		result.MissingEvidence = append(result.MissingEvidence, brokerMissingEvidenceResult{
			Code:    "context_packet_missing",
			Message: "A context packet is required because the selected pass defines context evidence expectations.",
		})
		tool := "create_context_packet"
		reason := "Gather bounded source evidence from the selected pass context plan."
		if requiresSourceSnapshot && latestSnapshot == nil {
			tool = "create_source_snapshot"
			reason = "Create a source snapshot first; context packet creation requires a snapshot ID."
		}
		result.NextActions = append(result.NextActions, brokerNextContextActionResult{Tool: tool, Reason: reason})
	}
	if latestPacket != nil {
		switch {
		case latestPacket.Status == contextpackets.ContextPacketStatusBlocked || latestPacket.BlockedSeedCount > 0:
			result.MissingEvidence = append(result.MissingEvidence, brokerMissingEvidenceResult{
				Code:    "context_packet_blocked",
				Message: "The latest context packet has blocked required evidence; review its coverage report before creating the handoff.",
			})
			result.NextActions = append(result.NextActions, brokerNextContextActionResult{
				Tool:   "get_context_packet",
				Reason: "Inspect context packet coverage and blockers.",
			})
		case latestPacket.Status == contextpackets.ContextPacketStatusPartial:
			result.MissingEvidence = append(result.MissingEvidence, brokerMissingEvidenceResult{
				Code:    "context_packet_incomplete",
				Message: "The latest context packet is partial; optional or truncated evidence should be reviewed before handoff creation.",
			})
		}
	}

	if len(result.MissingEvidence) > 0 {
		result.Status = "blocked"
		result.ReadyForHandoff = false
		if latestPacket != nil && latestPacket.Status == contextpackets.ContextPacketStatusPartial && latestPacket.BlockedSeedCount == 0 {
			onlyPartial := len(result.MissingEvidence) == 1 && result.MissingEvidence[0].Code == "context_packet_incomplete"
			if onlyPartial {
				result.Status = "ready"
				result.ReadyForHandoff = true
			}
		}
	}
	_ = pass
	_ = plan
	return result
}

func brokerContextPlanRequiresPacket(contextPlan json.RawMessage) bool {
	if len(bytes.TrimSpace(contextPlan)) == 0 || bytes.Equal(bytes.TrimSpace(contextPlan), []byte("{}")) {
		return false
	}
	var value interface{}
	if err := json.Unmarshal(contextPlan, &value); err != nil {
		return false
	}
	return brokerJSONHasNonEmptyArray(value,
		"seed_files", "seedFiles", "seed_files_to_read", "seedFilesToRead",
		"seed_searches", "seedSearches", "seed_search_terms", "seedSearchTerms",
		"context_coverage_expectations", "contextCoverageExpectations",
		"blocked_if_missing", "blockedIfMissing",
	)
}

func brokerSourceRequirementsRequireSnapshot(sourceRequirements json.RawMessage) bool {
	var value map[string]interface{}
	if err := json.Unmarshal(sourceRequirements, &value); err != nil {
		return false
	}
	return brokerJSONBool(value, "require_git_status", "requireGitStatus") ||
		brokerJSONBool(value, "require_commit_sha", "requireCommitSha", "require_commit_SHA")
}

func brokerJSONHasNonEmptyArray(value interface{}, keys ...string) bool {
	switch typed := value.(type) {
	case map[string]interface{}:
		for _, key := range keys {
			if arr, ok := typed[key].([]interface{}); ok && len(arr) > 0 {
				return true
			}
		}
		for _, nested := range typed {
			if brokerJSONHasNonEmptyArray(nested, keys...) {
				return true
			}
		}
	case []interface{}:
		return len(typed) > 0
	}
	return false
}

func brokerJSONBool(value map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if b, ok := value[key].(bool); ok && b {
			return true
		}
	}
	return false
}

func brokerBuildPassResult(plan *store.Plan, pass *store.PlanPass) (brokerPassResult, error) {
	intendedScope, err := brokerJSONField(pass.IntendedExecutionScopeJson, "[]")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode intended_execution_scope_json")
	}
	nonGoals, err := brokerJSONField(pass.NonGoalsJson, "[]")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode non_goals_json")
	}
	dependencies, err := brokerJSONField(pass.DependenciesJson, "[]")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode dependencies_json")
	}
	contextPlan, err := brokerJSONField(pass.ContextPlanJson, "{}")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode context_plan_json")
	}
	sourceRequirements, err := brokerJSONField(pass.SourceSnapshotRequirementsJson, "{}")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode source_snapshot_requirements_json")
	}
	readiness, err := brokerJSONField(pass.HandoffReadinessCriteriaJson, "[]")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode handoff_readiness_criteria_json")
	}
	contextBudget, err := brokerJSONField(pass.ContextBudgetJson, "{}")
	if err != nil {
		return brokerPassResult{}, fmt.Errorf("failed to decode context_budget_json")
	}
	return brokerPassResult{
		PlanRowID:                  plan.ID,
		PassRowID:                  pass.ID,
		PlanID:                     plan.PlanID,
		PassID:                     pass.PassID,
		Sequence:                   pass.Sequence,
		Name:                       pass.Name,
		Goal:                       pass.Goal,
		Status:                     pass.Status,
		PassType:                   pass.PassType,
		IntendedExecutionScope:     intendedScope,
		NonGoals:                   nonGoals,
		Dependencies:               dependencies,
		ContextPlan:                contextPlan,
		SourceSnapshotRequirements: sourceRequirements,
		HandoffReadinessCriteria:   readiness,
		RiskLevel:                  pass.RiskLevel,
		ContextBudget:              contextBudget,
		CreatedAt:                  pass.CreatedAt,
		UpdatedAt:                  pass.UpdatedAt,
	}, nil
}

func brokerContextPacketRowResult(row *store.ContextPacket) (brokerContextPacketResult, error) {
	blockers, err := brokerJSONField(row.BlockersJson, "[]")
	if err != nil {
		return brokerContextPacketResult{}, fmt.Errorf("failed to decode context packet blockers")
	}
	summary, err := brokerJSONField(row.SummaryJson, "{}")
	if err != nil {
		return brokerContextPacketResult{}, fmt.Errorf("failed to decode context packet summary")
	}
	return brokerContextPacketResult{
		ContextPacketID:    row.ContextPacketID,
		ProjectID:          row.ProjectID,
		PlanID:             row.PlanID,
		PassID:             row.PassID,
		TaskSlug:           row.TaskSlug,
		SourceSnapshotID:   row.SourceSnapshotID,
		Status:             row.Status,
		PacketJSONPath:     brokerSafeArtifactPath(row.PacketJsonPath),
		PacketMarkdownPath: brokerSafeArtifactPath(row.PacketMarkdownPath),
		CoverageReportPath: brokerSafeArtifactPath(row.CoverageReportPath),
		SourceCount:        row.SourceCount,
		CoveredSeedCount:   row.CoveredSeedCount,
		BlockedSeedCount:   row.BlockedSeedCount,
		MissingSeedCount:   row.MissingSeedCount,
		Truncated:          row.Truncated == 1,
		Blockers:           blockers,
		Summary:            summary,
		CreatedAt:          row.CreatedAt,
		CompletedAt:        row.CompletedAt,
	}, nil
}

func brokerContextPacketMetadataFromRow(row *store.ContextPacket) (*brokerContextPacketMetadata, error) {
	blockers, err := brokerJSONField(row.BlockersJson, "[]")
	if err != nil {
		return nil, fmt.Errorf("failed to decode context packet blockers")
	}
	summary, err := brokerJSONField(row.SummaryJson, "{}")
	if err != nil {
		return nil, fmt.Errorf("failed to decode context packet summary")
	}
	return &brokerContextPacketMetadata{
		ContextPacketID:    row.ContextPacketID,
		ProjectID:          row.ProjectID,
		PlanID:             row.PlanID,
		PassID:             row.PassID,
		TaskSlug:           row.TaskSlug,
		SourceSnapshotID:   row.SourceSnapshotID,
		Status:             row.Status,
		PacketJSONPath:     brokerSafeArtifactPath(row.PacketJsonPath),
		PacketMarkdownPath: brokerSafeArtifactPath(row.PacketMarkdownPath),
		CoverageReportPath: brokerSafeArtifactPath(row.CoverageReportPath),
		SourceCount:        row.SourceCount,
		CoveredSeedCount:   row.CoveredSeedCount,
		BlockedSeedCount:   row.BlockedSeedCount,
		MissingSeedCount:   row.MissingSeedCount,
		Truncated:          row.Truncated == 1,
		Blockers:           blockers,
		Summary:            summary,
		CreatedAt:          row.CreatedAt,
		CompletedAt:        row.CompletedAt,
	}, nil
}

func brokerFileInventoryFromResult(result *sources.FileInventoryResult) brokerFileInventoryResult {
	out := brokerFileInventoryResult{
		ProjectID:        result.ProjectID,
		SourceSnapshotID: result.SourceSnapshotID,
		Files:            make([]brokerSourceFileRecord, 0, len(result.Files)),
		Truncated:        result.Truncated,
		Blockers:         brokerBlockers(result.Blockers),
		GeneratedAt:      result.GeneratedAt,
	}
	for _, file := range result.Files {
		out.Files = append(out.Files, brokerSourceFileRecord{
			ProjectID:        file.ProjectID,
			RepoID:           file.RepoID,
			SourceSnapshotID: file.SourceSnapshotID,
			Path:             file.Path,
			SizeBytes:        file.SizeBytes,
			ContentHash:      file.ContentHash,
			HashAlgorithm:    file.HashAlgorithm,
			Tracked:          file.Tracked,
			Included:         file.Included,
			ExclusionReason:  file.ExclusionReason,
			RedactionStatus:  file.RedactionStatus,
			IndexedAt:        file.IndexedAt,
		})
	}
	return out
}

func brokerSourceSearchFromResult(result *sources.SourceSearchResult) brokerSourceSearchResult {
	out := brokerSourceSearchResult{
		ProjectID:        result.ProjectID,
		SourceSnapshotID: result.SourceSnapshotID,
		Matches:          make([]brokerSourceSearchMatchResult, 0, len(result.Matches)),
		Truncated:        result.Truncated,
		Blockers:         brokerBlockers(result.Blockers),
		GeneratedAt:      result.GeneratedAt,
	}
	for _, match := range result.Matches {
		out.Matches = append(out.Matches, brokerSourceSearchMatchResult{
			ProjectID:        match.ProjectID,
			RepoID:           match.RepoID,
			SourceSnapshotID: match.SourceSnapshotID,
			Path:             match.Path,
			LineStart:        match.LineStart,
			LineEnd:          match.LineEnd,
			Snippet:          match.Snippet,
			SnippetHash:      match.SnippetHash,
			ContentHash:      match.ContentHash,
			RedactionStatus:  match.RedactionStatus,
			Truncated:        match.Truncated,
			GeneratedAt:      match.GeneratedAt,
		})
	}
	return out
}

func brokerBoundedFileReadFromResult(result *sources.BoundedFileReadResult) brokerBoundedFileReadResult {
	return brokerBoundedFileReadResult{
		ProjectID:        result.ProjectID,
		RepoID:           result.RepoID,
		SourceSnapshotID: result.SourceSnapshotID,
		Path:             result.Path,
		LineStart:        result.LineStart,
		LineEnd:          result.LineEnd,
		Content:          result.Content,
		ContentHash:      result.ContentHash,
		CurrentHash:      result.CurrentHash,
		SnippetHash:      result.SnippetHash,
		RedactionStatus:  result.RedactionStatus,
		Truncated:        result.Truncated,
		GeneratedAt:      result.GeneratedAt,
		Blockers:         brokerBlockers(result.Blockers),
	}
}

func brokerBlockers(blockers []sources.SourceBlocker) []brokerSourceBlockerResult {
	if len(blockers) == 0 {
		return nil
	}
	out := make([]brokerSourceBlockerResult, 0, len(blockers))
	for _, blocker := range blockers {
		out = append(out, brokerSourceBlockerResult{
			RepoID:  blocker.RepoID,
			Code:    blocker.Code,
			Message: blocker.Message,
		})
	}
	return out
}

type brokerOpError struct {
	Code    string
	Message string
}

func (e brokerOpError) Error() string {
	return e.Code + ": " + e.Message
}

func brokerWrappedErr(err error) ToolCallResult {
	var opErr brokerOpError
	if errors.As(err, &opErr) {
		return brokerToolErr(opErr.Code, opErr.Message)
	}
	return brokerToolErr("INTERNAL_ERROR", "unexpected broker error")
}

func brokerToolOK(tool string, result interface{}) ToolCallResult {
	text, err := marshalTool(brokerSuccessPayload{
		OK:     true,
		Tool:   tool,
		Result: result,
	})
	if err != nil {
		return brokerToolErr("INTERNAL_ERROR", "failed to marshal broker result")
	}
	return toolOK(text)
}

func brokerToolErr(code string, message string) ToolCallResult {
	payload := brokerErrorPayload{OK: false}
	payload.Error.Code = code
	payload.Error.Message = message
	text, err := marshalTool(payload)
	if err != nil {
		return toolErr(`{"ok":false,"error":{"code":"INTERNAL_ERROR","message":"failed to marshal broker error"}}`)
	}
	return toolErr(text)
}

func brokerDecodeStrict[T any](raw json.RawMessage, dest *T) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

func brokerDecodeStringArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

func brokerJSONField(raw string, fallback string) (json.RawMessage, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = fallback
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	return json.RawMessage(value), nil
}

func brokerBoundedRawJSON(raw string) (json.RawMessage, bool, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, false, true
	}
	if len([]byte(value)) > brokerRawJSONByteLimit {
		return nil, true, false
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, false, false
	}
	return json.RawMessage(value), false, true
}

func brokerSafeArtifactPath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." || cleaned == ".." {
		return ""
	}
	if filepath.IsAbs(cleaned) {
		rel, err := filepath.Rel(artifacts.BaseDir, cleaned)
		if err != nil {
			return ""
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			return ""
		}
		return rel
	}
	normalized := filepath.ToSlash(cleaned)
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return ""
	}
	return normalized
}

func brokerProjectIDFromPlan(plan *store.Plan) string {
	if plan == nil {
		return ""
	}
	if planMeta, err := brokerJSONField(plan.ProjectContextJson, "{}"); err == nil {
		var payload struct {
			ProjectID string `json:"project_id"`
		}
		if json.Unmarshal(planMeta, &payload) == nil && strings.TrimSpace(payload.ProjectID) != "" {
			return strings.TrimSpace(payload.ProjectID)
		}
	}
	return ""
}

func brokerProjectIDFromPass(pass *store.PlanPass) string {
	if pass == nil {
		return ""
	}
	var payload struct {
		RequiredRepositories []string `json:"required_repositories"`
	}
	if json.Unmarshal([]byte(pass.ContextPlanJson), &payload) != nil {
		return ""
	}
	if len(payload.RequiredRepositories) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.RequiredRepositories[0])
}
