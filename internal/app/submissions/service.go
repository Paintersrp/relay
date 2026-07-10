package submissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	workflowplans "relay/internal/app/plans/workflow"
	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const MaxDiagnostics = 50

var lowercaseSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Type aliases for API packages to use app-layer names instead of importing internal/store/workflow
type (
	Project  = workflowstore.Project
	Plan     = workflowstore.Plan
	PlanPass = workflowstore.PlanPass
	Artifact = workflowstore.Artifact
	Run      = workflowstore.Run
)

type ValidationInput struct {
	DisplayName    string
	CanonicalBytes []byte
}

type ValidationResult struct {
	OK          bool
	Status      string
	Kind        string
	SHA256      string
	Diagnostics []speccompiler.Diagnostic
	Notices     []speccompiler.Diagnostic
}

type SubmitPlanInput struct {
	ProjectID      string
	DisplayName    string
	ExpectedSHA256 string
	CanonicalBytes []byte
}

type SubmitPlanResult struct {
	Project   workflowstore.Project
	Plan      workflowstore.Plan
	Passes    []workflowstore.PlanPass
	Artifacts []workflowstore.Artifact
}

type CreateRunInput struct {
	DisplayName     string
	ExpectedSHA256  string
	CanonicalBytes  []byte
	PlanID          string
	PassNumber      int64
	RemediatesRunID string
}

type CreateRunResult struct {
	Run       workflowstore.Run
	Artifacts []workflowstore.Artifact
}

type Service struct {
	store *workflowstore.Store
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

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) ValidateArtifact(_ context.Context, input ValidationInput) (ValidationResult, error) {
	compiled := speccompiler.Compile(input.DisplayName, input.CanonicalBytes)
	kind := "unknown"
	if identity, diagnostics := speccompiler.ParseFilename(input.DisplayName); len(diagnostics) == 0 {
		kind = string(identity.Kind)
	}
	result := ValidationResult{
		OK:          len(compiled.Errors) == 0,
		Status:      "valid",
		Kind:        kind,
		SHA256:      SHA256(input.CanonicalBytes),
		Diagnostics: boundedDiagnostics(compiled.Errors),
		Notices:     boundedDiagnostics(compiled.Notices),
	}
	if !result.OK {
		result.Status = "blocked"
	}
	return result, nil
}

func (s *Service) SubmitPlan(ctx context.Context, input SubmitPlanInput) (SubmitPlanResult, error) {
	if input.ProjectID == "" || strings.TrimSpace(input.ProjectID) != input.ProjectID {
		return SubmitPlanResult{}, applicationError(
			ErrorProjectNotFound,
			"Project ID is required without outer whitespace",
			"project_id",
			true,
			nil,
		)
	}
	_, markdown, err := compileMutation(input.DisplayName, input.ExpectedSHA256, input.CanonicalBytes, speccompiler.ArtifactPlan)
	if err != nil {
		return SubmitPlanResult{}, err
	}
	var model planArtifactModel
	if err := json.Unmarshal(input.CanonicalBytes, &model); err != nil {
		return SubmitPlanResult{}, compilerError(
			"compiled Plan could not be decoded for persistence metadata",
			"artifact_file",
			nil,
			nil,
		)
	}
	plans, err := workflowplans.NewService(s.store)
	if err != nil {
		return SubmitPlanResult{}, applicationError(ErrorPersistence, "workflow Plan service is unavailable", "workflow_store", false, err)
	}
	created, err := plans.CreatePlan(ctx, workflowplans.CreatePlanInput{
		ProjectID:        input.ProjectID,
		FeatureSlug:      model.FeatureSlug,
		CanonicalJSON:    input.CanonicalBytes,
		RenderedMarkdown: []byte(markdown),
		Repositories:     planRepositories(model),
		Passes:           planPasses(model),
	})
	if err != nil {
		return SubmitPlanResult{}, classifyPlanError(err)
	}
	return SubmitPlanResult{
		Project:   created.Project,
		Plan:      created.Plan,
		Passes:    created.Passes,
		Artifacts: created.Artifacts,
	}, nil
}

func (s *Service) CreateRun(ctx context.Context, input CreateRunInput) (CreateRunResult, error) {
	if input.PlanID != strings.TrimSpace(input.PlanID) {
		return CreateRunResult{}, applicationError(ErrorPlanPassAssociation, "Plan ID must not contain outer whitespace", "plan_id", true, nil)
	}
	if input.RemediatesRunID != strings.TrimSpace(input.RemediatesRunID) {
		return CreateRunResult{}, applicationError(ErrorRemediationAssociation, "remediates Run ID must not contain outer whitespace", "remediates_run_id", true, nil)
	}
	managed := input.PlanID != "" || input.PassNumber != 0
	if managed && (input.PlanID == "" || input.PassNumber < 1) {
		return CreateRunResult{}, applicationError(
			ErrorPlanPassAssociation,
			"Plan ID and positive pass number must be supplied together",
			"association",
			true,
			nil,
		)
	}

	identity, markdown, err := compileMutation(input.DisplayName, input.ExpectedSHA256, input.CanonicalBytes, speccompiler.ArtifactExecutionSpec)
	if err != nil {
		return CreateRunResult{}, err
	}
	if managed {
		if !identity.HasPassQualifier {
			return CreateRunResult{}, applicationError(
				ErrorSelectedPassFilename,
				"managed Run Execution Spec filename must include a terminal .pass-<number> qualifier",
				"file_name",
				true,
				nil,
			)
		}
		if identity.PassNumber != input.PassNumber {
			return CreateRunResult{}, applicationError(
				ErrorSelectedPassFilename,
				"Execution Spec filename pass qualifier does not match pass_number",
				"file_name",
				true,
				nil,
			)
		}
	} else if identity.HasPassQualifier {
		return CreateRunResult{}, applicationError(
			ErrorSelectedPassFilename,
			"Standalone Run Execution Spec filename must not include a pass qualifier",
			"file_name",
			true,
			nil,
		)
	}

	var model executionSpecModel
	if err := json.Unmarshal(input.CanonicalBytes, &model); err != nil {
		return CreateRunResult{}, compilerError(
			"compiled Execution Spec could not be decoded for persistence metadata",
			"artifact_file",
			nil,
			nil,
		)
	}
	runs, err := workflowruns.NewService(s.store)
	if err != nil {
		return CreateRunResult{}, applicationError(ErrorPersistence, "workflow Run service is unavailable", "workflow_store", false, err)
	}
	created, err := runs.CreateRun(ctx, workflowruns.CreateRunInput{
		FeatureSlug:      model.FeatureSlug,
		RepoTarget:       model.RepoTarget,
		Branch:           model.Branch,
		BaseCommit:       model.BaseCommit,
		CanonicalJSON:    input.CanonicalBytes,
		RenderedMarkdown: []byte(markdown),
		PlanID:           input.PlanID,
		PassNumber:       input.PassNumber,
		RemediatesRunID:  input.RemediatesRunID,
	})
	if err != nil {
		return CreateRunResult{}, classifyRunError(err)
	}
	return CreateRunResult{Run: created.Run, Artifacts: created.Artifacts}, nil
}

func compileMutation(displayName, expectedSHA string, data []byte, expectedKind speccompiler.ArtifactKind) (speccompiler.FilenameInfo, string, error) {
	if !lowercaseSHA256.MatchString(expectedSHA) {
		return speccompiler.FilenameInfo{}, "", applicationError(
			ErrorInvalidExpectedHash,
			"expected SHA-256 must be exactly 64 lowercase hexadecimal characters",
			"expected_sha256",
			true,
			nil,
		)
	}
	if SHA256(data) != expectedSHA {
		return speccompiler.FilenameInfo{}, "", applicationError(
			ErrorExpectedHashMismatch,
			"expected SHA-256 does not match canonical content",
			"expected_sha256",
			true,
			nil,
		)
	}
	identity, filenameDiagnostics := speccompiler.ParseFilename(displayName)
	if len(filenameDiagnostics) != 0 {
		for _, diagnostic := range filenameDiagnostics {
			if diagnostic.Code == "invalid_pass_qualifier" {
				failure := applicationError(
					ErrorSelectedPassFilename,
					"Execution Spec pass qualifier is malformed",
					"file_name",
					true,
					nil,
				)
				failure.Diagnostics = boundedDiagnostics(filenameDiagnostics)
				return speccompiler.FilenameInfo{}, "", failure
			}
		}
		return speccompiler.FilenameInfo{}, "", compilerError(
			"canonical artifact filename is invalid",
			"file_name",
			filenameDiagnostics,
			nil,
		)
	}
	if identity.Kind != expectedKind {
		return speccompiler.FilenameInfo{}, "", applicationError(
			ErrorInvalidArtifactKind,
			"canonical artifact kind does not match the requested operation",
			"file_name",
			true,
			nil,
		)
	}
	compiled := speccompiler.Compile(displayName, data)
	if len(compiled.Errors) != 0 || compiled.Markdown == nil {
		return speccompiler.FilenameInfo{}, "", compilerError(
			"canonical artifact failed deterministic compiler validation",
			"artifact_file",
			compiled.Errors,
			compiled.Notices,
		)
	}
	return identity, *compiled.Markdown, nil
}

func classifyPlanError(err error) error {
	switch {
	case errors.Is(err, workflowplans.ErrProjectNotFound):
		return applicationError(ErrorProjectNotFound, "referenced Project was not found", "project_id", true, err)
	case errors.Is(err, workflowplans.ErrProjectArchived):
		return applicationError(ErrorProjectArchived, "only active Projects may receive Plans", "project_id", true, err)
	case errors.Is(err, workflowplans.ErrRepositoryTargetNotFound):
		return applicationError(ErrorRepositoryNotFound, "repository target is not registered with exact key casing", "repo_target", true, err)
	case errors.Is(err, workflowplans.ErrPlanNotFound):
		return applicationError(ErrorPlanPassAssociation, "referenced Plan was not found", "plan_id", true, err)
	default:
		return applicationError(ErrorPersistence, "workflow persistence failed", "workflow_store", false, err)
	}
}

func classifyRunError(err error) error {
	switch {
	case errors.Is(err, workflowruns.ErrRepositoryTargetNotFound):
		return applicationError(ErrorRepositoryNotFound, "repository target is not registered with exact key casing", "repo_target", true, err)
	case errors.Is(err, workflowruns.ErrPlanPassAssociation):
		if strings.Contains(err.Error(), "managed Plan") && strings.Contains(err.Error(), "was not found") {
			return applicationError(ErrorUnknownResource, "referenced Plan was not found", "plan_id", true, err)
		}
		return applicationError(ErrorPlanPassAssociation, "Plan, pass, or repository association is invalid", "association", true, err)
	case errors.Is(err, workflowruns.ErrRemediationAssociation):
		return applicationError(ErrorRemediationAssociation, "remediation Run association is invalid", "remediates_run_id", true, err)
	case errors.Is(err, workflowruns.ErrInvalidRunInput):
		return applicationError(ErrorPlanPassAssociation, err.Error(), "association", true, err)
	default:
		return applicationError(ErrorPersistence, "workflow persistence failed", "workflow_store", false, err)
	}
}

func ArtifactKind(displayName string) string {
	identity, diagnostics := speccompiler.ParseFilename(displayName)
	if len(diagnostics) != 0 {
		return "unknown"
	}
	return string(identity.Kind)
}

func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func boundedDiagnostics(values []speccompiler.Diagnostic) []speccompiler.Diagnostic {
	if len(values) > MaxDiagnostics {
		values = values[:MaxDiagnostics]
	}
	if values == nil {
		return []speccompiler.Diagnostic{}
	}
	return append([]speccompiler.Diagnostic(nil), values...)
}

func planRepositories(model planArtifactModel) []workflowplans.RepositoryTargetInput {
	out := make([]workflowplans.RepositoryTargetInput, 0, len(model.RepoTargets))
	for _, repository := range model.RepoTargets {
		out = append(out, workflowplans.RepositoryTargetInput{
			RepoTarget:         repository.RepoTarget,
			Branch:             repository.Branch,
			PlanningBaseCommit: repository.PlanningBaseCommit,
		})
	}
	return out
}

func planPasses(model planArtifactModel) []workflowplans.PassInput {
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
