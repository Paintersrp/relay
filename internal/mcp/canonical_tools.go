package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	workflowcanonical "relay/internal/app/canonical"
	workflowplans "relay/internal/app/plans/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const maxCanonicalDiagnostics = 50

const (
	canonicalBlockerCompilerRejected   = "compiler_rejected"
	canonicalBlockerPersistenceFailed  = "persistence_failed"
	canonicalBlockerAssociationInvalid = "association_invalid"
	canonicalBlockerArtifactKind       = "artifact_kind_mismatch"
)

var canonicalArtifactFileSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["download_url", "file_id", "file_name"],
  "properties": {
    "download_url": {"type": "string", "format": "uri"},
    "file_id": {"type": "string", "minLength": 1},
    "mime_type": {"type": "string"},
    "file_name": {"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]*(?:\\.plan\\.json|\\.execution-spec\\.json)$"}
  }
}`)

var validateArtifactSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["artifact_file"],
  "properties": {"artifact_file": ` + string(canonicalArtifactFileSchema) + `}
}`)

var submitPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "artifact_file", "expected_sha256"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "artifact_file": ` + string(canonicalArtifactFileSchema) + `,
    "expected_sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"}
  }
}`)

var getCanonicalPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["plan_id"],
  "properties": {"plan_id": {"type": "string", "minLength": 1}}
}`)

var createCanonicalRunSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["artifact_file", "expected_sha256"],
  "properties": {
    "artifact_file": ` + string(canonicalArtifactFileSchema) + `,
    "expected_sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
    "plan_id": {"type": "string", "minLength": 1},
    "pass_number": {"type": "integer", "minimum": 1},
    "remediates_run_id": {"type": "string", "minLength": 1}
  },
  "dependentRequired": {"plan_id": ["pass_number"], "pass_number": ["plan_id"]}
}`)

var (
	ToolValidateArtifact = ToolDefinition{
		Name:        "validate_artifact",
		Description: "Validate one canonical Plan or Execution Spec JSON file by exact downloaded bytes. Returns bounded diagnostics and SHA-256 only; never returns artifact bodies.",
		InputSchema: validateArtifactSchema,
		Meta:        map[string]any{"openai/fileParams": []string{"artifact_file"}},
	}
	ToolSubmitPlan = ToolDefinition{
		Name:        "submit_plan",
		Description: "Submit one canonical Plan JSON file to an active Relay Project after exact SHA-256 verification and deterministic recompilation. Creates Plan, Pass, and artifact metadata atomically.",
		InputSchema: submitPlanSchema,
		Meta:        map[string]any{"openai/fileParams": []string{"artifact_file"}},
	}
	ToolGetCanonicalPlan = ToolDefinition{
		Name:        "get_plan",
		Description: "Read bounded Project, Plan, Pass, and artifact metadata without returning canonical JSON or rendered Markdown bodies.",
		InputSchema: getCanonicalPlanSchema,
	}
	ToolCreateCanonicalRun = ToolDefinition{
		Name:        "create_run",
		Description: "Submit one canonical Execution Spec JSON file after exact SHA-256 verification and deterministic recompilation. Creates a setup-ready Run atomically.",
		InputSchema: createCanonicalRunSchema,
		Meta:        map[string]any{"openai/fileParams": []string{"artifact_file"}},
	}
)

type canonicalArtifactArgs struct {
	ArtifactFile ChatGPTFileReference `json:"artifact_file"`
}

type canonicalSubmissionArgs struct {
	ProjectID       string               `json:"project_id,omitempty"`
	ArtifactFile    ChatGPTFileReference `json:"artifact_file"`
	ExpectedSHA256  string               `json:"expected_sha256"`
	PlanID          string               `json:"plan_id,omitempty"`
	PassNumber      int64                `json:"pass_number,omitempty"`
	RemediatesRunID string               `json:"remediates_run_id,omitempty"`
}

type getCanonicalPlanArgs struct {
	PlanID string `json:"plan_id"`
}

type canonicalValidationOutput struct {
	OK          bool                      `json:"ok"`
	Tool        string                    `json:"tool"`
	Status      string                    `json:"status"`
	Artifact    SubmittedArtifactIdentity `json:"artifact"`
	SHA256      string                    `json:"sha256"`
	Kind        string                    `json:"kind"`
	Diagnostics []speccompiler.Diagnostic `json:"diagnostics"`
	Notices     []speccompiler.Diagnostic `json:"notices"`
}

type canonicalPlanOutput struct {
	OK        bool                     `json:"ok"`
	Tool      string                   `json:"tool"`
	Project   projectMetadata          `json:"project"`
	Plan      planMetadata             `json:"plan"`
	Passes    []passMetadata           `json:"passes"`
	Artifacts []workflowArtifactOutput `json:"artifacts"`
}

type canonicalRunOutput struct {
	OK         bool                      `json:"ok"`
	Tool       string                    `json:"tool"`
	Run        runMetadata               `json:"run"`
	Artifacts  []workflowArtifactOutput  `json:"artifacts"`
	Provenance ExactSubmissionProvenance `json:"provenance"`
	ReviewURL  string                    `json:"review_url"`
}

type planMetadata struct {
	PlanID          string `json:"plan_id"`
	FeatureSlug     string `json:"feature_slug"`
	Status          string `json:"status"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type passMetadata struct {
	PassID     string `json:"pass_id"`
	Number     int64  `json:"number"`
	Name       string `json:"name"`
	RepoTarget string `json:"repo_target"`
	Status     string `json:"status"`
}

type runMetadata struct {
	RunID           string `json:"run_id"`
	FeatureSlug     string `json:"feature_slug"`
	RepoTarget      string `json:"repo_target"`
	Status          string `json:"status"`
	Branch          string `json:"branch"`
	BaseCommit      string `json:"base_commit"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	PlanID          string `json:"plan_id,omitempty"`
	PassNumber      int64  `json:"pass_number,omitempty"`
	RemediatesRunID string `json:"remediates_run_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type workflowArtifactOutput struct {
	ArtifactID   string `json:"artifact_id"`
	OwnerType    string `json:"owner_type"`
	Kind         string `json:"kind"`
	RelativePath string `json:"relative_path"`
	MediaType    string `json:"media_type"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	CreatedAt    string `json:"created_at"`
}

func canonicalToolDefinitions(profile ToolProfile) []ToolDefinition {
	switch profile {
	case ToolProfileAuditor:
		return []ToolDefinition{
			ToolValidateArtifact,
			ToolCreateCanonicalRun,
			ToolGetAuditPacket,
			ToolRecordAuditDecision,
		}
	case ToolProfileLocalOperator:
		return []ToolDefinition{
			ToolValidateArtifact,
			ToolListProjects,
			ToolSubmitPlan,
			ToolGetCanonicalPlan,
			ToolCreateCanonicalRun,
			ToolGetAuditPacket,
			ToolRecordAuditDecision,
		}
	case ToolProfilePlanner:
		return []ToolDefinition{ToolValidateArtifact, ToolListProjects, ToolSubmitPlan, ToolGetCanonicalPlan, ToolCreateCanonicalRun}
	default:
		return []ToolDefinition{ToolValidateArtifact, ToolListProjects, ToolSubmitPlan, ToolGetCanonicalPlan, ToolCreateCanonicalRun}
	}
}

func legacyBaseToolDefinitions() []ToolDefinition {
	out := []ToolDefinition{
		ToolSubmitTestAuditPacket,
		ToolCreateRunFromPlannerHandoff,
		ToolCreateRunFromPlannerHandoffFile,
		ToolValidatePlannerHandoffForCompile,
		ToolSubmitPlannerPassPlan,
		ToolListOpenRuns,
		ToolGetRunStatus,
		ToolSubmitAuditPacket,
	}
	out = append(out, planAttemptToolDefinitions()...)
	out = append(out, planSeedToolDefinitions()...)
	return out
}

func legacyLocalOperatorToolDefinitions() []ToolDefinition {
	out := legacyBaseToolDefinitions()
	out = append(out, contextBrokerToolDefinitions()...)
	out = append(out, refactorBacklogToolDefinitions()...)
	return out
}

func (s *Server) canonicalFetcher() CanonicalFileParameterFetcher {
	if s != nil && s.deps != nil && s.deps.CanonicalFileFetcher != nil {
		return s.deps.CanonicalFileFetcher
	}
	if s != nil && s.deps != nil && s.deps.FileFetcher != nil {
		if fetcher, ok := s.deps.FileFetcher.(CanonicalFileParameterFetcher); ok {
			return fetcher
		}
	}
	return NewHTTPSFileParameterFetcher()
}

func (s *Server) workflowStore() *workflowstore.Store {
	if s == nil || s.deps == nil {
		return nil
	}
	return s.deps.WorkflowStore
}

func (s *Server) HandleValidateArtifact(rawArgs json.RawMessage) ToolCallResult {
	var input canonicalArtifactArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return canonicalBlocked("validate_artifact", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "validate_artifact", nil)
	}
	content, fetchErr := s.canonicalFetcher().FetchCanonicalArtifact(context.Background(), input.ArtifactFile)
	if fetchErr != nil {
		return toolBlockedResult("validate_artifact", []MCPBlocker{canonicalFileParameterBlocker(fetchErr)}, nil)
	}
	service, err := s.canonicalWorkflowService()
	if err != nil {
		return canonicalBlocked("validate_artifact", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", nil)
	}
	result, err := service.ValidateArtifact(context.Background(), workflowcanonical.ValidationInput{
		DisplayName:    content.DisplayName,
		CanonicalBytes: content.Bytes,
	})
	if err != nil {
		return canonicalApplicationBlocked("validate_artifact", err, nil)
	}
	return canonicalOK(canonicalValidationOutput{
		OK:          result.OK,
		Tool:        "validate_artifact",
		Status:      result.Status,
		Artifact:    SubmittedArtifactIdentity{ArtifactKind: result.Kind, DisplayName: content.DisplayName, ByteCount: int64(len(content.Bytes))},
		SHA256:      result.SHA256,
		Kind:        result.Kind,
		Diagnostics: result.Diagnostics,
		Notices:     result.Notices,
	})
}

func (s *Server) HandleSubmitPlan(rawArgs json.RawMessage) ToolCallResult {
	if s.workflowStore() == nil {
		return canonicalBlocked("submit_plan", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input canonicalSubmissionArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return canonicalBlocked("submit_plan", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "submit_plan", nil)
	}
	content, fetchErr := s.canonicalFetcher().FetchCanonicalArtifact(context.Background(), input.ArtifactFile)
	if fetchErr != nil {
		return toolBlockedResult("submit_plan", []MCPBlocker{canonicalFileParameterBlocker(fetchErr)}, nil)
	}
	provenance := exactCanonicalProvenance(content, input.ExpectedSHA256)
	service, err := s.canonicalWorkflowService()
	if err != nil {
		return canonicalBlocked("submit_plan", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", map[string]any{"provenance": provenance})
	}
	result, err := service.SubmitPlan(context.Background(), workflowcanonical.SubmitPlanInput{
		ProjectID:      input.ProjectID,
		DisplayName:    content.DisplayName,
		ExpectedSHA256: input.ExpectedSHA256,
		CanonicalBytes: content.Bytes,
	})
	if err != nil {
		return canonicalApplicationBlocked("submit_plan", err, provenance)
	}
	return canonicalOK(canonicalPlanOutput{
		OK:        true,
		Tool:      "submit_plan",
		Project:   projectOut(result.Project),
		Plan:      planOut(result.Plan),
		Passes:    passOut(result.Passes),
		Artifacts: artifactOut(result.Artifacts),
	})
}

func (s *Server) HandleGetCanonicalPlan(rawArgs json.RawMessage) ToolCallResult {
	if s.workflowStore() == nil {
		return canonicalBlocked("get_plan", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input getCanonicalPlanArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return canonicalBlocked("get_plan", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "get_plan", nil)
	}
	svc, err := workflowplans.NewService(s.workflowStore())
	if err != nil {
		return canonicalBlocked("get_plan", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", nil)
	}
	result, err := svc.GetPlan(context.Background(), input.PlanID)
	if err != nil {
		return canonicalApplicationBlocked("get_plan", err, nil)
	}
	return canonicalOK(canonicalPlanOutput{
		OK:        true,
		Tool:      "get_plan",
		Project:   projectOut(result.Project),
		Plan:      planOut(result.Plan),
		Passes:    passOut(result.Passes),
		Artifacts: artifactOut(result.Artifacts),
	})
}

func (s *Server) HandleCreateCanonicalRun(rawArgs json.RawMessage) ToolCallResult {
	if s.workflowStore() == nil {
		return canonicalBlocked("create_run", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input canonicalSubmissionArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return canonicalBlocked("create_run", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "create_run", nil)
	}
	content, fetchErr := s.canonicalFetcher().FetchCanonicalArtifact(context.Background(), input.ArtifactFile)
	if fetchErr != nil {
		return toolBlockedResult("create_run", []MCPBlocker{canonicalFileParameterBlocker(fetchErr)}, nil)
	}
	provenance := exactCanonicalProvenance(content, input.ExpectedSHA256)
	service, err := s.canonicalWorkflowService()
	if err != nil {
		return canonicalBlocked("create_run", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", map[string]any{"provenance": provenance})
	}
	result, err := service.CreateRun(context.Background(), workflowcanonical.CreateRunInput{
		DisplayName:     content.DisplayName,
		ExpectedSHA256:  input.ExpectedSHA256,
		CanonicalBytes:  content.Bytes,
		PlanID:          input.PlanID,
		PassNumber:      input.PassNumber,
		RemediatesRunID: input.RemediatesRunID,
	})
	if err != nil {
		return canonicalApplicationBlocked("create_run", err, provenance)
	}
	return canonicalOK(canonicalRunOutput{
		OK:         true,
		Tool:       "create_run",
		Run:        runOut(result.Run, s.workflowStore()),
		Artifacts:  artifactOut(result.Artifacts),
		Provenance: provenance,
		ReviewURL:  canonicalRunReviewURL(result.Run.RunID),
	})
}

func canonicalRunReviewURL(runID string) string {
	base := strings.TrimSpace(os.Getenv("RELAY_WEB_BASE_URL"))
	if base == "" {
		base = "http://localhost:3000"
	}
	return strings.TrimRight(base, "/") + "/runs/" + url.PathEscape(runID) + "/specification"
}

func canonicalBlocked(tool, code, message string, recoverable bool, ref string, metadata any) ToolCallResult {
	return toolBlockedResult(tool, []MCPBlocker{newMCPBlocker(code, message, recoverable, []MCPBlockerEvidence{{Kind: "field", Ref: ref}}, []string{"Correct the blocker and retry the tool."})}, metadata)
}

func canonicalOK(out any) ToolCallResult {
	text, err := marshalTool(out)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return ToolCallResult{
		Content:           []ContentBlock{{Type: "text", Text: text}},
		StructuredContent: out,
	}
}

func canonicalKind(displayName string) string {
	return workflowcanonical.ArtifactKind(displayName)
}

func exactCanonicalProvenance(content FileParameterContent, expectedSHA string) ExactSubmissionProvenance {
	out := exactSubmissionProvenance(content.Bytes, expectedSHA, "file_parameter", content.DisplayName)
	out.ArtifactIdentity.ArtifactKind = canonicalKind(content.DisplayName)
	out.ArtifactIdentity.DisplayName = safeArtifactDisplayName(content.DisplayName, "artifact.json")
	return out
}

func canonicalFileParameterBlocker(err *FileParameterError) MCPBlocker {
	if err == nil {
		err = fileParamErr(MCPBlockerFileDownloadFailed, "artifact_file could not be downloaded")
	}
	recoverable := err.Code != MCPBlockerUnsafeDownloadTarget
	return newMCPBlocker(err.Code, err.Message, recoverable, []MCPBlockerEvidence{{Kind: "field", Ref: "artifact_file"}}, []string{"Attach one reviewed canonical JSON artifact file and retry."})
}

func boundedDiagnostics(in []speccompiler.Diagnostic) []speccompiler.Diagnostic {
	if len(in) > maxCanonicalDiagnostics {
		in = in[:maxCanonicalDiagnostics]
	}
	if in == nil {
		return []speccompiler.Diagnostic{}
	}
	return append([]speccompiler.Diagnostic(nil), in...)
}

func planOut(plan workflowstore.Plan) planMetadata {
	return planMetadata{PlanID: plan.PlanID, FeatureSlug: plan.FeatureSlug, Status: plan.Status, CanonicalSHA256: plan.CanonicalSHA256, CreatedAt: plan.CreatedAt, UpdatedAt: plan.UpdatedAt}
}

func passOut(passes []workflowstore.PlanPass) []passMetadata {
	out := make([]passMetadata, 0, len(passes))
	for _, pass := range passes {
		out = append(out, passMetadata{PassID: pass.PassID, Number: pass.PassNumber, Name: pass.Name, RepoTarget: pass.RepoTarget, Status: pass.Status})
	}
	return out
}

func artifactOut(artifacts []workflowstore.Artifact) []workflowArtifactOutput {
	out := make([]workflowArtifactOutput, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, workflowArtifactOutput{
			ArtifactID:   artifact.ArtifactID,
			OwnerType:    artifact.OwnerType,
			Kind:         artifact.Kind,
			RelativePath: artifact.RelativePath,
			MediaType:    artifact.MediaType,
			SHA256:       artifact.SHA256,
			SizeBytes:    artifact.SizeBytes,
			CreatedAt:    artifact.CreatedAt,
		})
	}
	return out
}

func runOut(run workflowstore.Run, store *workflowstore.Store) runMetadata {
	out := runMetadata{
		RunID:           run.RunID,
		FeatureSlug:     run.FeatureSlug,
		RepoTarget:      run.RepoTarget,
		Status:          run.Status,
		Branch:          run.Branch,
		BaseCommit:      run.BaseCommit,
		CanonicalSHA256: run.CanonicalSHA256,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
	if store != nil && run.PlanRowID.Valid {
		if plan, err := store.GetPlanByRowID(context.Background(), run.PlanRowID.Int64); err == nil {
			out.PlanID = plan.PlanID
		}
	}
	if store != nil && run.PlanPassRowID.Valid {
		if pass, err := store.GetPlanPassByRowID(context.Background(), run.PlanPassRowID.Int64); err == nil {
			out.PassNumber = pass.PassNumber
			if out.PlanID == "" {
				if plan, err := store.GetPlanByRowID(context.Background(), pass.PlanRowID); err == nil {
					out.PlanID = plan.PlanID
				}
			}
		}
	}
	if store != nil && run.RemediatesRunRowID.Valid {
		if remediates, err := store.GetRunByRowID(context.Background(), run.RemediatesRunRowID.Int64); err == nil {
			out.RemediatesRunID = remediates.RunID
		}
	}
	return out
}
