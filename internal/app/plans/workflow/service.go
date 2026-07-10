package workflowplans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrProjectNotFound          = errors.New("Project not found")
	ErrProjectArchived          = errors.New("Project is archived")
	ErrRepositoryTargetNotFound = errors.New("repository target not found")
	ErrPlanNotFound             = errors.New("Plan not found")
)

type IDGenerator interface {
	PlanID() string
	PassID() string
	ArtifactID() string
}

type defaultIDGenerator struct{}

func (defaultIDGenerator) PlanID() string     { return workflowstore.NewPlanID() }
func (defaultIDGenerator) PassID() string     { return workflowstore.NewPassID() }
func (defaultIDGenerator) ArtifactID() string { return workflowstore.NewArtifactID() }

type Service struct {
	store *workflowstore.Store
	ids   IDGenerator
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{})
}

func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if ids == nil {
		return nil, fmt.Errorf("workflow ID generator is required")
	}
	return &Service{store: store, ids: ids}, nil
}

func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (CreatePlanResult, error) {
	if err := validateCreatePlanInput(input); err != nil {
		return CreatePlanResult{}, err
	}

	planID := s.ids.PlanID()
	passIDs := make([]string, len(input.Passes))
	for index := range passIDs {
		passIDs[index] = s.ids.PassID()
	}

	batch, err := s.store.ArtifactStore().Begin("plans/" + planID)
	if err != nil {
		return CreatePlanResult{}, err
	}
	canonical, err := batch.Stage(
		"canonical_plan",
		input.FeatureSlug+".plan.json",
		"application/json",
		input.CanonicalJSON,
	)
	if err != nil {
		_ = batch.Rollback()
		return CreatePlanResult{}, err
	}
	rendered, err := batch.Stage(
		"rendered_plan",
		input.FeatureSlug+".plan.md",
		"text/markdown",
		input.RenderedMarkdown,
	)
	if err != nil {
		_ = batch.Rollback()
		return CreatePlanResult{}, err
	}

	result := CreatePlanResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, input.ProjectID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrProjectNotFound, input.ProjectID)
		}
		if err != nil {
			return err
		}
		if project.Status != workflowstore.ProjectStatusActive {
			return fmt.Errorf("%w: %s", ErrProjectArchived, project.ProjectID)
		}
		result.Project = project

		for _, target := range input.Repositories {
			registered, err := tx.GetRepositoryTarget(ctx, target.RepoTarget)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrRepositoryTargetNotFound, target.RepoTarget)
			}
			if err != nil {
				return err
			}
			if registered.RepoTarget != target.RepoTarget {
				return fmt.Errorf("%w: repository target %q must use registered key casing %q", ErrRepositoryTargetNotFound, target.RepoTarget, registered.RepoTarget)
			}
		}

		plan, err := tx.CreatePlan(ctx, workflowstore.CreatePlanParams{
			ProjectRowID:    project.ID,
			PlanID:          planID,
			FeatureSlug:     input.FeatureSlug,
			CanonicalSHA256: canonical.SHA256,
		})
		if err != nil {
			return fmt.Errorf("create plan: %w", err)
		}
		result.Plan = plan

		for index, target := range input.Repositories {
			if _, err := tx.CreatePlanRepositoryTarget(ctx, workflowstore.CreatePlanRepositoryTargetParams{
				PlanRowID:          plan.ID,
				Sequence:           int64(index + 1),
				RepoTarget:         target.RepoTarget,
				Branch:             target.Branch,
				PlanningBaseCommit: target.PlanningBaseCommit,
			}); err != nil {
				return fmt.Errorf("create plan repository target %q: %w", target.RepoTarget, err)
			}
		}

		passesByNumber := make(map[int64]workflowstore.PlanPass, len(input.Passes))
		for index, passInput := range input.Passes {
			pass, err := tx.CreatePlanPass(ctx, workflowstore.CreatePlanPassParams{
				PassID:     passIDs[index],
				PlanRowID:  plan.ID,
				PassNumber: passInput.Number,
				Name:       passInput.Name,
				RepoTarget: passInput.RepoTarget,
			})
			if err != nil {
				return fmt.Errorf("create pass %d: %w", passInput.Number, err)
			}
			result.Passes = append(result.Passes, pass)
			passesByNumber[passInput.Number] = pass
		}

		for _, passInput := range input.Passes {
			pass := passesByNumber[passInput.Number]
			for _, dependencyNumber := range passInput.DependsOn {
				dependency := passesByNumber[dependencyNumber]
				if err := tx.CreatePlanPassDependency(ctx, pass.ID, dependency.ID); err != nil {
					return fmt.Errorf("create pass %d dependency %d: %w", passInput.Number, dependencyNumber, err)
				}
			}
		}

		for _, staged := range []workflowartifacts.File{canonical, rendered} {
			artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
				ArtifactID:   s.ids.ArtifactID(),
				OwnerType:    workflowstore.ArtifactOwnerPlan,
				PlanRowID:    sql.NullInt64{Int64: plan.ID, Valid: true},
				Kind:         staged.Kind,
				RelativePath: staged.RelativePath,
				MediaType:    staged.MediaType,
				SHA256:       staged.SHA256,
				SizeBytes:    staged.SizeBytes,
			})
			if err != nil {
				return fmt.Errorf("create plan artifact metadata: %w", err)
			}
			result.Artifacts = append(result.Artifacts, artifact)
		}
		return nil
	})
	if err != nil {
		return CreatePlanResult{}, err
	}
	return result, nil
}

func (s *Service) GetPlan(ctx context.Context, planID string) (GetPlanResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return GetPlanResult{}, fmt.Errorf("%w: Plan ID is required", ErrPlanNotFound)
	}
	plan, err := s.store.GetPlanByPlanID(ctx, planID)
	if errors.Is(err, sql.ErrNoRows) {
		return GetPlanResult{}, fmt.Errorf("%w: %s", ErrPlanNotFound, planID)
	}
	if err != nil {
		return GetPlanResult{}, err
	}
	project, err := s.store.GetProjectByRowID(ctx, plan.ProjectRowID)
	if err != nil {
		return GetPlanResult{}, err
	}
	passes, err := s.store.ListPlanPasses(ctx, plan.ID)
	if err != nil {
		return GetPlanResult{}, fmt.Errorf("list Plan passes: %w", err)
	}
	artifacts, err := s.store.ListArtifactsByPlan(ctx, plan.ID)
	if err != nil {
		return GetPlanResult{}, fmt.Errorf("list Plan artifacts: %w", err)
	}
	return GetPlanResult{Project: project, Plan: plan, Passes: passes, Artifacts: artifacts}, nil
}

func (s *Service) MovePlan(ctx context.Context, input MovePlanInput) (MovePlanResult, error) {
	planID := strings.TrimSpace(input.PlanID)
	projectID := strings.TrimSpace(input.ProjectID)
	if planID == "" {
		return MovePlanResult{}, fmt.Errorf("%w: Plan ID is required", ErrPlanNotFound)
	}
	if projectID == "" {
		return MovePlanResult{}, fmt.Errorf("%w: Project ID is required", ErrProjectNotFound)
	}
	result := MovePlanResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		plan, err := tx.GetPlanByPlanID(ctx, planID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrPlanNotFound, planID)
		}
		if err != nil {
			return err
		}
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrProjectNotFound, projectID)
		}
		if err != nil {
			return err
		}
		if project.Status != workflowstore.ProjectStatusActive {
			return fmt.Errorf("%w: %s", ErrProjectArchived, project.ProjectID)
		}
		result.Project = project
		if plan.ProjectRowID == project.ID {
			result.Plan = plan
			return nil
		}
		result.Plan, err = tx.MovePlanToProject(ctx, planID, project.ID)
		return err
	})
	if err != nil {
		return MovePlanResult{}, err
	}
	return result, nil
}

func validateCreatePlanInput(input CreatePlanInput) error {
	if input.ProjectID == "" || strings.TrimSpace(input.ProjectID) != input.ProjectID {
		return fmt.Errorf("%w: Project ID is required without outer whitespace", ErrProjectNotFound)
	}
	if !validFeatureSlug(input.FeatureSlug) {
		return fmt.Errorf("feature slug must be lowercase kebab-case")
	}
	if len(input.CanonicalJSON) == 0 || len(input.RenderedMarkdown) == 0 {
		return fmt.Errorf("canonical Plan JSON and rendered Plan Markdown are required")
	}
	if len(input.Repositories) == 0 {
		return fmt.Errorf("at least one repository target is required")
	}
	if len(input.Passes) == 0 {
		return fmt.Errorf("at least one pass is required")
	}

	repositories := make(map[string]struct{}, len(input.Repositories))
	for index, target := range input.Repositories {
		key := strings.ToLower(target.RepoTarget)
		if strings.TrimSpace(target.RepoTarget) == "" || strings.TrimSpace(target.RepoTarget) != target.RepoTarget {
			return fmt.Errorf("repository target %d is invalid", index+1)
		}
		if _, duplicate := repositories[key]; duplicate {
			return fmt.Errorf("repository target %q is duplicated", target.RepoTarget)
		}
		repositories[key] = struct{}{}
		if strings.TrimSpace(target.Branch) == "" || strings.TrimSpace(target.Branch) != target.Branch {
			return fmt.Errorf("repository target %q has an invalid branch", target.RepoTarget)
		}
		if !validCommit(target.PlanningBaseCommit) {
			return fmt.Errorf("repository target %q has an invalid planning base commit", target.RepoTarget)
		}
	}

	for index, pass := range input.Passes {
		expected := int64(index + 1)
		if pass.Number != expected {
			return fmt.Errorf("pass number must be %d", expected)
		}
		if strings.TrimSpace(pass.Name) == "" {
			return fmt.Errorf("pass %d name is required", pass.Number)
		}
		if _, ok := repositories[strings.ToLower(pass.RepoTarget)]; !ok {
			return fmt.Errorf("pass %d references undeclared repository target %q", pass.Number, pass.RepoTarget)
		}
		seenDependencies := map[int64]struct{}{}
		for _, dependency := range pass.DependsOn {
			if dependency < 1 || dependency >= pass.Number {
				return fmt.Errorf("pass %d dependency %d must reference an earlier pass", pass.Number, dependency)
			}
			if _, duplicate := seenDependencies[dependency]; duplicate {
				return fmt.Errorf("pass %d dependency %d is duplicated", pass.Number, dependency)
			}
			seenDependencies[dependency] = struct{}{}
		}
	}
	return nil
}

func validFeatureSlug(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		if r != '-' || index == 0 || index == len(value)-1 {
			return false
		}
	}
	return !strings.Contains(value, "--")
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
