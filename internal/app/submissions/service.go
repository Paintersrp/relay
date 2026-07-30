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

	appcutover "relay/internal/app/cutover"
	workflowplans "relay/internal/app/plans/workflow"
	"relay/internal/planningartifacts"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const MaxDiagnostics = 50

var lowercaseSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

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

// PlanSubmissionGate keeps canonical Plan submission and the underlying Plan
// mutation on the same cutover boundary.
type PlanSubmissionGate interface {
	workflowplans.PlanMutationGate
	AllowNewPlan(context.Context) (appcutover.LegacyGateDecision, error)
}

type Service struct {
	store       *workflowstore.Store
	cutoverGate PlanSubmissionGate
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

func NewService(store *workflowstore.Store) (*Service, error) {
	cutoverService, err := appcutover.NewService(store)
	if err != nil {
		return nil, err
	}
	return NewServiceWithGate(store, appcutover.NewLegacyGate(cutoverService))
}

func NewServiceWithGate(store *workflowstore.Store, gate PlanSubmissionGate) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if gate == nil {
		return nil, fmt.Errorf("workflow cutover gate is required")
	}
	return &Service{store: store, cutoverGate: gate}, nil
}

func (s *Service) admitNewPlan(ctx context.Context) error {
	if s == nil || s.cutoverGate == nil {
		return applicationError(ErrorCutoverStateUnavailable, "Cutover admission state is unavailable", "cutover", false, nil)
	}
	decision, err := s.cutoverGate.AllowNewPlan(ctx)
	if err != nil {
		return applicationError(ErrorCutoverStateUnavailable, "Cutover admission state is unavailable", "cutover", false, err)
	}
	if !decision.Allowed {
		return applicationError(ErrorLegacyAdmissionClosed, "Legacy Plan submission is closed; use ticket-oriented admission", "cutover", true, appcutover.ErrLegacyAdmissionClosed)
	}
	return nil
}

func (s *Service) ValidateArtifact(_ context.Context, input ValidationInput) (ValidationResult, error) {
	return validateArtifact(input), nil
}

func validateArtifact(input ValidationInput) ValidationResult {
	kind := "unknown"
	diagnostics := []speccompiler.Diagnostic{}
	notices := []speccompiler.Diagnostic{}
	ok := false
	if identity, filenameDiagnostics := speccompiler.ParseFilename(input.DisplayName); len(filenameDiagnostics) == 0 {
		kind = string(identity.Kind)
		switch identity.Kind {
		case speccompiler.ArtifactRequirements, speccompiler.ArtifactSharedDesign, speccompiler.ArtifactTicketDesignBrief:
			diagnostics = planningartifacts.Validate(identity.Kind, input.CanonicalBytes)
			ok = len(diagnostics) == 0
		default:
			compiled := speccompiler.Compile(input.DisplayName, input.CanonicalBytes)
			diagnostics = compiled.Errors
			notices = compiled.Notices
			ok = len(diagnostics) == 0
		}
	} else {
		diagnostics = filenameDiagnostics
	}
	result := ValidationResult{
		OK:          ok,
		Status:      "valid",
		Kind:        kind,
		SHA256:      SHA256(input.CanonicalBytes),
		Diagnostics: boundedDiagnostics(diagnostics),
		Notices:     boundedDiagnostics(notices),
	}
	if !result.OK {
		result.Status = "blocked"
	}
	return result
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
	if err := s.admitNewPlan(ctx); err != nil {
		return SubmitPlanResult{}, err
	}
	plans, err := workflowplans.NewServiceWithGate(s.store, s.cutoverGate)
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
	if len(compiled.Errors) != 0 || (compiled.Markdown == nil && identity.Kind != speccompiler.ArtifactDeterministicOperations) {
		return speccompiler.FilenameInfo{}, "", compilerError(
			"canonical artifact failed deterministic compiler validation",
			"artifact_file",
			compiled.Errors,
			compiled.Notices,
		)
	}
	if identity.Kind == speccompiler.ArtifactDeterministicOperations {
		return identity, "", nil
	}
	return identity, *compiled.Markdown, nil
}

func classifyPlanError(err error) error {
	switch {
	case errors.Is(err, appcutover.ErrLegacyAdmissionClosed):
		return applicationError(ErrorLegacyAdmissionClosed, "Legacy Plan submission is closed; use ticket-oriented admission", "cutover", true, err)
	case errors.Is(err, workflowplans.ErrCutoverStateUnavailable):
		return applicationError(ErrorCutoverStateUnavailable, "Cutover admission state is unavailable", "cutover", false, err)
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
	result := make([]speccompiler.Diagnostic, len(values))
	copy(result, values)
	return result
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
