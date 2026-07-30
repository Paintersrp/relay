package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	workflowplans "relay/internal/app/plans/workflow"
	workflowsubmissions "relay/internal/app/submissions"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const maxSubmissionDiagnostics = 50

const (
	submissionBlockerCompilerRejected   = "compiler_rejected"
	submissionBlockerPersistenceFailed  = "persistence_failed"
	submissionBlockerAssociationInvalid = "association_invalid"
	submissionBlockerArtifactKind       = "artifact_kind_mismatch"
)

var artifactFileSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["download_url", "file_id", "file_name"],
  "properties": {
    "download_url": {"type": "string", "format": "uri"},
    "file_id": {"type": "string", "minLength": 1},
    "mime_type": {"type": "string"},
    "file_name": {"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]*\\.plan\\.json$"}
  }
}`)

var validationArtifactFileSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["download_url", "file_id", "file_name"],
  "properties": {
    "download_url": {"type": "string", "format": "uri"},
    "file_id": {"type": "string", "minLength": 1},
    "mime_type": {"type": "string"},
    "file_name": {"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]*(?:\\.plan\\.json|\\.deterministic-operations\\.json|\\.requirements\\.md|\\.design\\.md|\\.ticket-[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*\\.r[1-9][0-9]*\\.design-brief\\.md)$"}
  }
}`)

var validateArtifactSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["artifact_file"],
  "properties": {"artifact_file": ` + string(validationArtifactFileSchema) + `}
}`)

var submitPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "artifact_file", "expected_sha256"],
  "properties": {
    "project_id": {"type": "string", "minLength": 1},
    "artifact_file": ` + string(artifactFileSchema) + `,
    "expected_sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"}
  }
}`)

var getPlanSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["plan_id"],
  "properties": {"plan_id": {"type": "string", "minLength": 1}}
}`)

var (
	ToolValidateArtifact = ToolDefinition{
		Name:        "validate_artifact",
		Description: "Validate one canonical Plan or Deterministic Operations JSON file, or authored Requirements, Shared Design, or Ticket Design Brief Markdown file, by exact downloaded bytes. Returns bounded diagnostics and SHA-256 only; never returns artifact bodies.",
		InputSchema: validateArtifactSchema,
		Meta:        map[string]any{"openai/fileParams": []string{"artifact_file"}},
	}
	ToolSubmitPlan = ToolDefinition{
		Name:        "submit_plan",
		Description: "Submit one canonical Plan JSON file to an active Relay Project after exact SHA-256 verification and deterministic recompilation. Creates Plan, Pass, and artifact metadata atomically.",
		InputSchema: submitPlanSchema,
		Meta:        map[string]any{"openai/fileParams": []string{"artifact_file"}},
	}
	ToolGetPlan = ToolDefinition{
		Name:        "get_plan",
		Description: "Read bounded Project, Plan, Pass, and artifact metadata without returning canonical JSON or rendered Markdown bodies.",
		InputSchema: getPlanSchema,
	}
)

type artifactArgs struct {
	ArtifactFile ChatGPTFileReference `json:"artifact_file"`
}

type artifactSubmissionArgs struct {
	ProjectID      string               `json:"project_id,omitempty"`
	ArtifactFile   ChatGPTFileReference `json:"artifact_file"`
	ExpectedSHA256 string               `json:"expected_sha256"`
}

type getPlanArgs struct {
	PlanID string `json:"plan_id"`
}

type artifactValidationOutput struct {
	OK          bool                      `json:"ok"`
	Tool        string                    `json:"tool"`
	Status      string                    `json:"status"`
	Artifact    SubmittedArtifactIdentity `json:"artifact"`
	SHA256      string                    `json:"sha256"`
	Kind        string                    `json:"kind"`
	Diagnostics []speccompiler.Diagnostic `json:"diagnostics"`
	Notices     []speccompiler.Diagnostic `json:"notices"`
}

type planOutput struct {
	OK        bool                     `json:"ok"`
	Tool      string                   `json:"tool"`
	Project   projectMetadata          `json:"project"`
	Plan      planMetadata             `json:"plan"`
	Passes    []passMetadata           `json:"passes"`
	Artifacts []workflowArtifactOutput `json:"artifacts"`
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

func workflowToolDefinitions(profile ToolProfile) []ToolDefinition {
	switch profile {
	case ToolProfileAuditor:
		return []ToolDefinition{ToolValidateArtifact, ToolGetAuditPacket, ToolGetRunArtifact, ToolRecordAuditDecision}
	case ToolProfilePlanner:
		return []ToolDefinition{ToolValidateArtifact, ToolListProjects, ToolSubmitPlan, ToolGetPlan}
	default:
		return []ToolDefinition{ToolValidateArtifact, ToolListProjects, ToolSubmitPlan, ToolGetPlan}
	}
}

func (s *Server) artifactFetcher() ArtifactFileParameterFetcher {
	if s != nil && s.deps != nil && s.deps.ArtifactFileFetcher != nil {
		return s.deps.ArtifactFileFetcher
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
	var input artifactArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("validate_artifact", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "validate_artifact", nil)
	}
	content, fetchErr := s.artifactFetcher().FetchArtifact(context.Background(), input.ArtifactFile)
	if fetchErr != nil {
		return toolBlockedResult("validate_artifact", []MCPBlocker{artifactFileParameterBlocker(fetchErr)}, nil)
	}
	service, err := s.submissionService()
	if err != nil {
		return workflowBlocked("validate_artifact", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", nil)
	}
	result, err := service.ValidateArtifact(context.Background(), workflowsubmissions.ValidationInput{
		DisplayName:    content.DisplayName,
		CanonicalBytes: content.Bytes,
	})
	if err != nil {
		return submissionApplicationBlocked("validate_artifact", err, nil)
	}
	return workflowOK(artifactValidationOutput{
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
		return workflowBlocked("submit_plan", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input artifactSubmissionArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("submit_plan", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "submit_plan", nil)
	}
	content, fetchErr := s.artifactFetcher().FetchArtifact(context.Background(), input.ArtifactFile)
	if fetchErr != nil {
		return toolBlockedResult("submit_plan", []MCPBlocker{artifactFileParameterBlocker(fetchErr)}, nil)
	}
	provenance := exactArtifactProvenance(content, input.ExpectedSHA256)
	service, err := s.submissionService()
	if err != nil {
		return workflowBlocked("submit_plan", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", map[string]any{"provenance": provenance})
	}
	result, err := service.SubmitPlan(context.Background(), workflowsubmissions.SubmitPlanInput{
		ProjectID:      input.ProjectID,
		DisplayName:    content.DisplayName,
		ExpectedSHA256: input.ExpectedSHA256,
		CanonicalBytes: content.Bytes,
	})
	if err != nil {
		return submissionApplicationBlocked("submit_plan", err, provenance)
	}
	return workflowOK(planOutput{
		OK:        true,
		Tool:      "submit_plan",
		Project:   projectOut(result.Project),
		Plan:      planOut(result.Plan),
		Passes:    passOut(result.Passes),
		Artifacts: artifactOut(result.Artifacts),
	})
}

func (s *Server) HandleGetPlan(rawArgs json.RawMessage) ToolCallResult {
	if s.workflowStore() == nil {
		return workflowBlocked("get_plan", MCPBlockerToolUnavailable, "MCP server is not connected to a workflow store.", false, "workflow_store", nil)
	}
	var input getPlanArgs
	if err := brokerDecodeStrict(rawArgs, &input); err != nil {
		return workflowBlocked("get_plan", MCPBlockerSchemaMismatch, "invalid arguments: "+err.Error(), false, "get_plan", nil)
	}
	svc, err := workflowplans.NewService(s.workflowStore())
	if err != nil {
		return workflowBlocked("get_plan", MCPBlockerToolUnavailable, err.Error(), false, "workflow_store", nil)
	}
	result, err := svc.GetPlan(context.Background(), input.PlanID)
	if err != nil {
		return submissionApplicationBlocked("get_plan", err, nil)
	}
	return workflowOK(planOutput{
		OK:        true,
		Tool:      "get_plan",
		Project:   projectOut(result.Project),
		Plan:      planOut(result.Plan),
		Passes:    passOut(result.Passes),
		Artifacts: artifactOut(result.Artifacts),
	})
}

func workflowBlocked(tool, code, message string, recoverable bool, ref string, metadata any) ToolCallResult {
	return toolBlockedResult(tool, []MCPBlocker{newMCPBlocker(code, message, recoverable, []MCPBlockerEvidence{{Kind: "field", Ref: ref}}, []string{"Correct the blocker and retry the tool."})}, metadata)
}

func workflowOK(out any) ToolCallResult {
	text, err := marshalTool(out)
	if err != nil {
		return toolErr(fmt.Sprintf("INTERNAL_ERROR: %s", err))
	}
	return ToolCallResult{
		Content:           []ContentBlock{{Type: "text", Text: text}},
		StructuredContent: out,
	}
}

func artifactKind(displayName string) string {
	return workflowsubmissions.ArtifactKind(displayName)
}

func exactArtifactProvenance(content FileParameterContent, expectedSHA string) ExactSubmissionProvenance {
	out := exactSubmissionProvenance(content.Bytes, expectedSHA, "file_parameter", content.DisplayName)
	out.ArtifactIdentity.ArtifactKind = artifactKind(content.DisplayName)
	out.ArtifactIdentity.DisplayName = safeArtifactDisplayName(content.DisplayName, "artifact.json")
	return out
}

func exactSubmissionProvenance(data []byte, expectedSHA, sourceMode, displayName string) ExactSubmissionProvenance {
	submittedSHA := sha256Hex(data)
	status := "not_supplied"
	if strings.TrimSpace(expectedSHA) != "" {
		status = "mismatched"
		if expectedSHA == submittedSHA {
			status = "matched"
		}
	}
	return ExactSubmissionProvenance{
		SubmittedSHA256: submittedSHA,
		ExpectedSHA256:  strings.TrimSpace(expectedSHA),
		SHAMatchStatus:  status,
		SourceMode:      sourceMode,
		ArtifactIdentity: SubmittedArtifactIdentity{
			DisplayName: safeArtifactDisplayName(displayName, "artifact.json"),
			ByteCount:   int64(len(data)),
		},
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func artifactFileParameterBlocker(err *FileParameterError) MCPBlocker {
	if err == nil {
		err = fileParamErr(MCPBlockerFileDownloadFailed, "artifact_file could not be downloaded")
	}
	recoverable := err.Code != MCPBlockerUnsafeDownloadTarget
	return newMCPBlocker(err.Code, err.Message, recoverable, []MCPBlockerEvidence{{Kind: "field", Ref: "artifact_file"}}, []string{"Attach one reviewed canonical JSON artifact file and retry."})
}

func boundedDiagnostics(in []speccompiler.Diagnostic) []speccompiler.Diagnostic {
	if len(in) > maxSubmissionDiagnostics {
		in = in[:maxSubmissionDiagnostics]
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
