package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	workflowplans "relay/internal/app/plans/workflow"
	workflowruns "relay/internal/app/runs/workflow"
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
  "required": ["artifact_file", "expected_sha256"],
  "properties": {
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
		Description: "Submit one canonical Plan JSON file after exact SHA-256 verification and deterministic recompilation. Creates Plan, Pass, and artifact metadata atomically.",
		InputSchema: submitPlanSchema,
		Meta:        map[string]any{"openai/fileParams": []string{"artifact_file"}},
	}
	ToolGetCanonicalPlan = ToolDefinition{
		Name:        "get_plan",
		Description: "Read bounded Plan, Pass, and artifact metadata without returning canonical JSON or rendered Markdown bodies.",
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

type planArtifactModel struct {
	FeatureSlug string `json:"feature_slug"`
	RepoTargets []struct {
		RepoTarget         string `json:"repo_target"`
		Branch             string `json:"branch"`
		PlanningBaseCommit string `json:"planning_base_commit"`
	} `json:"repo_targets"`
	Passes []struct {
		Number     int64   `json:"number"`
		Name       string  `json:"name"`
		RepoTarget string  `json:"repo_target"`
		DependsOn  []int64 `json:"depends_on"`
	} `json:"passes"`
}

type executionSpecModel struct {
	FeatureSlug string `json:"feature_slug"`
	RepoTarget  string `json:"repo_target"`
	Branch      string `json:"branch"`
	BaseCommit  string `json:"base_commit"`
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
			ToolSubmitPlan,
			ToolGetCanonicalPlan,
			ToolCreateCanonicalRun,
			ToolGetAuditPacket,
			ToolRecordAuditDecision,
		}
	case ToolProfilePlanner:
		return []ToolDefinition{ToolValidateArtifact, ToolSubmitPlan, ToolGetCanonicalPlan, ToolCreateCanonicalRun}
	default:
		return []ToolDefinition{ToolValidateArtifact, ToolSubmitPlan, ToolGetCanonicalPlan, ToolCreateCanonicalRun}
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
	return canonicalValidationResult("validate_artifact", content)
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
	if blocked := verifyCanonicalSubmission("submit_plan", content.DisplayName, input.ExpectedSHA256, "plan", provenance); blocked.IsError {
		return blocked
	}
	compile := speccompiler.Compile(content.DisplayName, content.Bytes)
	if len(compile.Errors) > 0 || compile.Markdown == nil {
		return canonicalCompilerBlocked("submit_plan", content, compile, provenance)
	}
	var model planArtifactModel
	if err := json.Unmarshal(content.Bytes, &model); err != nil {
		return canonicalBlocked("submit_plan", canonicalBlockerCompilerRejected, "compiled Plan could not be decoded for persistence metadata", false, "artifact_file", map[string]any{"provenance": provenance})
	}
	svc, err := workflowplans.NewService(s.workflowStore())
	if err != nil {
		return canonicalBlocked("submit_plan", MCPBlockerToolUnavailable, "workflow Plan service is unavailable", false, "workflow_store", map[string]any{"provenance": provenance})
	}
	result, err := svc.CreatePlan(context.Background(), workflowplans.CreatePlanInput{
		FeatureSlug:      model.FeatureSlug,
		CanonicalJSON:    content.Bytes,
		RenderedMarkdown: []byte(*compile.Markdown),
		Repositories:     canonicalPlanRepos(model),
		Passes:           canonicalPlanPasses(model),
	})
	if err != nil {
		return canonicalServiceBlocked("submit_plan", err, provenance)
	}
	out := canonicalPlanOutput{OK: true, Tool: "submit_plan", Plan: planOut(result.Plan), Passes: passOut(result.Passes), Artifacts: artifactOut(result.Artifacts)}
	return canonicalOK(out)
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
		return canonicalServiceBlocked("get_plan", err, nil)
	}
	out := canonicalPlanOutput{OK: true, Tool: "get_plan", Plan: planOut(result.Plan), Passes: passOut(result.Passes), Artifacts: artifactOut(result.Artifacts)}
	return canonicalOK(out)
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
	if blocked := verifyCanonicalSubmission("create_run", content.DisplayName, input.ExpectedSHA256, "execution_spec", provenance); blocked.IsError {
		return blocked
	}
	compile := speccompiler.Compile(content.DisplayName, content.Bytes)
	if len(compile.Errors) > 0 || compile.Markdown == nil {
		return canonicalCompilerBlocked("create_run", content, compile, provenance)
	}
	if blocked := verifyCanonicalRunFilenameAssociation(content.DisplayName, input, provenance); blocked.IsError {
		return blocked
	}
	var model executionSpecModel
	if err := json.Unmarshal(content.Bytes, &model); err != nil {
		return canonicalBlocked("create_run", canonicalBlockerCompilerRejected, "compiled Execution Spec could not be decoded for persistence metadata", false, "artifact_file", map[string]any{"provenance": provenance})
	}
	svc, err := workflowruns.NewService(s.workflowStore())
	if err != nil {
		return canonicalBlocked("create_run", MCPBlockerToolUnavailable, "workflow Run service is unavailable", false, "workflow_store", map[string]any{"provenance": provenance})
	}
	result, err := svc.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      model.FeatureSlug,
		RepoTarget:       model.RepoTarget,
		Branch:           model.Branch,
		BaseCommit:       model.BaseCommit,
		CanonicalJSON:    content.Bytes,
		RenderedMarkdown: []byte(*compile.Markdown),
		PlanID:           strings.TrimSpace(input.PlanID),
		PassNumber:       input.PassNumber,
		RemediatesRunID:  strings.TrimSpace(input.RemediatesRunID),
	})
	if err != nil {
		return canonicalServiceBlocked("create_run", err, provenance)
	}
	out := canonicalRunOutput{
		OK:         true,
		Tool:       "create_run",
		Run:        runOut(result.Run, s.workflowStore()),
		Artifacts:  artifactOut(result.Artifacts),
		Provenance: provenance,
		ReviewURL:  canonicalRunReviewURL(result.Run.RunID),
	}
	return canonicalOK(out)
}

func verifyCanonicalRunFilenameAssociation(filename string, input canonicalSubmissionArgs, provenance ExactSubmissionProvenance) ToolCallResult {
	identity, diagnostics := speccompiler.ParseFilename(filename)
	if len(diagnostics) != 0 {
		return canonicalBlocked("create_run", canonicalBlockerCompilerRejected, "canonical Execution Spec filename is invalid", true, "artifact_file", map[string]any{
			"provenance":  provenance,
			"diagnostics": boundedDiagnostics(diagnostics),
		})
	}

	if strings.TrimSpace(input.PlanID) != "" {
		if !identity.HasPassQualifier {
			return canonicalBlocked("create_run", canonicalBlockerAssociationInvalid, "managed Run Execution Spec filename must include a .pass-<number> qualifier", true, "artifact_file.file_name", map[string]any{"provenance": provenance})
		}
		if identity.PassNumber != input.PassNumber {
			return canonicalBlocked("create_run", canonicalBlockerAssociationInvalid, "Execution Spec filename pass qualifier does not match pass_number", true, "artifact_file.file_name", map[string]any{"provenance": provenance})
		}
		return ToolCallResult{}
	}

	if identity.HasPassQualifier {
		return canonicalBlocked("create_run", canonicalBlockerAssociationInvalid, "standalone Run Execution Spec filename must not include a pass qualifier", true, "artifact_file.file_name", map[string]any{"provenance": provenance})
	}
	return ToolCallResult{}
}

func canonicalRunReviewURL(runID string) string {
	base := strings.TrimSpace(os.Getenv("RELAY_WEB_BASE_URL"))
	if base == "" {
		base = "http://localhost:3000"
	}
	return strings.TrimRight(base, "/") + "/runs/" + url.PathEscape(runID) + "/specification"
}

func canonicalValidationResult(tool string, content FileParameterContent) ToolCallResult {
	result := speccompiler.Compile(content.DisplayName, content.Bytes)
	kind := canonicalKind(content.DisplayName)
	out := canonicalValidationOutput{
		OK:       len(result.Errors) == 0,
		Tool:     tool,
		Status:   "valid",
		Kind:     kind,
		SHA256:   sha256Hex(content.Bytes),
		Artifact: SubmittedArtifactIdentity{ArtifactKind: kind, DisplayName: content.DisplayName, ByteCount: int64(len(content.Bytes))},
		Notices:  boundedDiagnostics(result.Notices),
	}
	if len(result.Errors) > 0 {
		out.Status = "blocked"
		out.Diagnostics = boundedDiagnostics(result.Errors)
	}
	return canonicalOK(out)
}

func verifyCanonicalSubmission(tool, displayName, expectedSHA, expectedKind string, provenance ExactSubmissionProvenance) ToolCallResult {
	if err := validateExpectedSHA256(strings.TrimSpace(expectedSHA)); err != nil {
		return canonicalBlocked(tool, MCPBlockerSchemaMismatch, err.Error(), false, "expected_sha256", map[string]any{"provenance": provenance})
	}
	if provenance.SubmittedSHA256 != strings.TrimSpace(expectedSHA) {
		return canonicalBlocked(tool, MCPBlockerExpectedHashMismatch, "expected_sha256 does not match submitted artifact sha256", true, "expected_sha256", map[string]any{"provenance": provenance})
	}
	if canonicalKind(displayName) != expectedKind {
		return canonicalBlocked(tool, canonicalBlockerArtifactKind, "artifact_file.file_name has the wrong canonical artifact kind for this tool", true, "artifact_file", map[string]any{"provenance": provenance})
	}
	return ToolCallResult{}
}

func canonicalServiceBlocked(tool string, err error, provenance any) ToolCallResult {
	lower := strings.ToLower(err.Error())
	metadata := any(nil)
	if provenance != nil {
		metadata = map[string]any{"provenance": provenance}
	}

	switch {
	case errors.Is(err, sql.ErrNoRows) && strings.Contains(lower, "repository target"):
		return canonicalBlocked(tool, MCPBlockerUnknownRepository, "repository target is not registered", true, "repo_target", metadata)
	case errors.Is(err, sql.ErrNoRows):
		return canonicalBlocked(tool, MCPBlockerUnknownResource, "referenced Plan, pass, or remediation Run was not found", true, "association", metadata)
	case strings.Contains(lower, "plan id and pass number"),
		strings.Contains(lower, "managed plan"),
		strings.Contains(lower, "managed pass"),
		strings.Contains(lower, "remediation source run"),
		strings.Contains(lower, "does not match run repository"):
		return canonicalBlocked(tool, canonicalBlockerAssociationInvalid, "Plan, pass, repository, or remediation association is invalid", true, "association", metadata)
	case strings.Contains(lower, "repository target") &&
		(strings.Contains(lower, "not registered") || strings.Contains(lower, "registered key casing")):
		return canonicalBlocked(tool, MCPBlockerUnknownRepository, "repository target is not registered", true, "repo_target", metadata)
	default:
		return canonicalBlocked(tool, canonicalBlockerPersistenceFailed, "workflow persistence failed", false, "workflow_store", metadata)
	}
}

func canonicalCompilerBlocked(tool string, content FileParameterContent, result speccompiler.Result, provenance ExactSubmissionProvenance) ToolCallResult {
	return toolBlockedResult(tool, []MCPBlocker{newMCPBlocker(canonicalBlockerCompilerRejected, "canonical artifact failed deterministic compiler validation", true, []MCPBlockerEvidence{{Kind: "artifact_name", Ref: content.DisplayName}}, []string{"Correct the canonical JSON and retry with a fresh validation SHA-256."})}, map[string]any{
		"provenance":  provenance,
		"diagnostics": boundedDiagnostics(result.Errors),
		"notices":     boundedDiagnostics(result.Notices),
	})
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
	if strings.HasSuffix(displayName, ".plan.json") {
		return "plan"
	}
	if strings.HasSuffix(displayName, ".execution-spec.json") {
		return "execution_spec"
	}
	return "unknown"
}

func exactCanonicalProvenance(content FileParameterContent, expectedSHA string) ExactSubmissionProvenance {
	out := exactSubmissionProvenance(content.Bytes, strings.TrimSpace(expectedSHA), "file_parameter", content.DisplayName)
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

func canonicalPlanRepos(model planArtifactModel) []workflowplans.RepositoryTargetInput {
	out := make([]workflowplans.RepositoryTargetInput, 0, len(model.RepoTargets))
	for _, repo := range model.RepoTargets {
		out = append(out, workflowplans.RepositoryTargetInput{
			RepoTarget:         repo.RepoTarget,
			Branch:             repo.Branch,
			PlanningBaseCommit: repo.PlanningBaseCommit,
		})
	}
	return out
}

func canonicalPlanPasses(model planArtifactModel) []workflowplans.PassInput {
	out := make([]workflowplans.PassInput, 0, len(model.Passes))
	for _, pass := range model.Passes {
		out = append(out, workflowplans.PassInput{
			Number:     pass.Number,
			Name:       pass.Name,
			RepoTarget: pass.RepoTarget,
			DependsOn:  append([]int64(nil), pass.DependsOn...),
		})
	}
	return out
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
		if plan, err := getPlanByRowID(context.Background(), store, run.PlanRowID.Int64); err == nil {
			out.PlanID = plan.PlanID
		}
	}
	if store != nil && run.PlanPassRowID.Valid {
		if pass, err := store.GetPlanPassByRowID(context.Background(), run.PlanPassRowID.Int64); err == nil {
			out.PassNumber = pass.PassNumber
			if out.PlanID == "" {
				if plan, err := getPlanByRowID(context.Background(), store, pass.PlanRowID); err == nil {
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

func getPlanByRowID(ctx context.Context, store *workflowstore.Store, rowID int64) (workflowstore.Plan, error) {
	var value workflowstore.Plan
	err := store.DB().QueryRowContext(ctx, `
SELECT id, plan_id, feature_slug, status, canonical_sha256, created_at, updated_at, completed_at
FROM plans
WHERE id = ?`, rowID).Scan(
		&value.ID,
		&value.PlanID,
		&value.FeatureSlug,
		&value.Status,
		&value.CanonicalSHA256,
		&value.CreatedAt,
		&value.UpdatedAt,
		&value.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return workflowstore.Plan{}, err
	}
	return value, err
}
