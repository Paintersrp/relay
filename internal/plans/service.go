package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"relay/internal/store"
	"relay/internal/store/generated"
)

type Service struct {
	store      *store.Store
	schemaPath string
}

func NewService(s *store.Store) *Service {
	return NewServiceWithSchemaPath(s, defaultSchemaPath)
}

func NewServiceWithSchemaPath(s *store.Store, schemaPath string) *Service {
	return &Service{
		store:      s,
		schemaPath: schemaPath,
	}
}

func (svc *Service) SubmitPlan(ctx context.Context, req SubmitPlanRequest) (*SubmitPlanResult, error) {
	plan, report, err := svc.ValidatePlanJSON(ctx, req.RawJSON)
	result := &SubmitPlanResult{Report: report}
	if err != nil {
		return result, err
	}
	if !report.Valid {
		return result, nil
	}

	projectID := ResolvePlanProjectID(req.ProjectID, plan)
	if projectID == "" {
		result.Report.addIssue(
			IssuePlanProjectRequired,
			"$.plan_meta.project_id",
			"project_id is required",
		)
		result.Report.finalize()
		return result, nil
	}

	project, err := svc.store.GetProjectByProjectID(projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			result.Report.addIssue(
				IssuePlanProjectUnknown,
				"$.plan_meta.project_id",
				fmt.Sprintf("project_id %q is unknown", projectID),
			)
			result.Report.finalize()
			return result, nil
		}
		return result, fmt.Errorf("lookup project %q: %w", projectID, err)
	}

	queries := generated.New(svc.store.DB())
	if _, err := queries.GetPlanByPlanID(ctx, plan.PlanMeta.PlanID); err == nil {
		result.Report.addIssue(
			IssuePlanDuplicatePlanID,
			"$.plan_meta.plan_id",
			fmt.Sprintf("plan_id %q already exists", plan.PlanMeta.PlanID),
		)
		return result, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("lookup existing plan by plan_id: %w", err)
	}

	tx, err := svc.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin plan submission transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txQueries := generated.New(tx)
	planMetaJSON, err := marshalJSONValue(plan.PlanMeta)
	if err != nil {
		result.Report.addIssue(IssuePlanStorageFailed, "$.plan_meta", "failed to encode plan_meta")
		return result, fmt.Errorf("marshal plan_meta: %w", err)
	}
	projectContextJSON, err := marshalOptionalJSONObject(plan.PlanMeta.ProjectContext)
	if err != nil {
		result.Report.addIssue(IssuePlanStorageFailed, "$.plan_meta.project_context", "failed to encode project_context")
		return result, fmt.Errorf("marshal project_context: %w", err)
	}
	mcpCapabilityProfileJSON, err := marshalOptionalJSONObject(plan.PlanMeta.MCPCapabilityProfile)
	if err != nil {
		result.Report.addIssue(IssuePlanStorageFailed, "$.plan_meta.mcp_capability_profile", "failed to encode mcp_capability_profile")
		return result, fmt.Errorf("marshal mcp_capability_profile: %w", err)
	}
	globalContextRulesJSON, err := marshalOptionalJSONObject(plan.GlobalContextRules)
	if err != nil {
		result.Report.addIssue(IssuePlanStorageFailed, "$.global_context_rules", "failed to encode global_context_rules")
		return result, fmt.Errorf("marshal global_context_rules: %w", err)
	}

	createdPlan, err := txQueries.CreatePlan(ctx, generated.CreatePlanParams{
		PlanID:                   plan.PlanMeta.PlanID,
		SchemaVersion:            plan.PlanMeta.SchemaVersion,
		Title:                    plan.PlanMeta.Title,
		Goal:                     plan.PlanMeta.Goal,
		RepoTarget:               plan.PlanMeta.RepoTarget,
		BranchContext:            plan.PlanMeta.BranchContext,
		Status:                   plan.PlanMeta.Status,
		SourceIntentSummary:      plan.SourceIntent.Summary,
		SourceArtifactPath:       req.SourceArtifactPath,
		PlanMetaJson:             planMetaJSON,
		ProjectContextJson:       projectContextJSON,
		McpCapabilityProfileJson: mcpCapabilityProfileJSON,
		GlobalContextRulesJson:   globalContextRulesJSON,
		SubmissionNote:           plan.PlanMeta.SubmissionNote,
		RawPlanJson:              string(req.RawJSON),
		ProjectRowID:             project.ID,
		ProjectID:                project.ProjectID,
	})
	if err != nil {
		result.Report.addIssue(IssuePlanStorageFailed, "$.plan_meta.plan_id", "failed to store plan")
		return result, fmt.Errorf("create plan: %w", err)
	}

	orderedPasses := append([]PlanPassInput(nil), plan.Passes...)
	sort.Slice(orderedPasses, func(i, j int) bool {
		return orderedPasses[i].Sequence < orderedPasses[j].Sequence
	})

	createdPasses := make([]store.PlanPass, 0, len(orderedPasses))
	for _, pass := range orderedPasses {
		intendedJSON, err := marshalStringSlice(pass.IntendedExecutionScope)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode intended_execution_scope")
			return result, fmt.Errorf("marshal intended_execution_scope for %q: %w", pass.PassID, err)
		}
		nonGoalsJSON, err := marshalStringSlice(pass.NonGoals)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode non_goals")
			return result, fmt.Errorf("marshal non_goals for %q: %w", pass.PassID, err)
		}
		dependenciesJSON, err := marshalStringSlice(pass.Dependencies)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode dependencies")
			return result, fmt.Errorf("marshal dependencies for %q: %w", pass.PassID, err)
		}
		contextPlanJSON, err := marshalJSONValue(pass.ContextPlan)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode context_plan")
			return result, fmt.Errorf("marshal context_plan for %q: %w", pass.PassID, err)
		}
		sourceSnapshotRequirementsJSON, err := marshalJSONValue(pass.SourceSnapshotRequirements)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode source_snapshot_requirements")
			return result, fmt.Errorf("marshal source_snapshot_requirements for %q: %w", pass.PassID, err)
		}
		handoffReadinessCriteriaJSON, err := marshalJSONValue(pass.HandoffReadinessCriteria)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode handoff_readiness_criteria")
			return result, fmt.Errorf("marshal handoff_readiness_criteria for %q: %w", pass.PassID, err)
		}
		contextBudgetJSON, err := marshalOptionalJSONObject(pass.ContextBudget)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode context_budget")
			return result, fmt.Errorf("marshal context_budget for %q: %w", pass.PassID, err)
		}
		rawPassJSON, err := marshalJSONValue(pass)
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to encode raw pass JSON")
			return result, fmt.Errorf("marshal raw pass for %q: %w", pass.PassID, err)
		}

		createdPass, err := txQueries.CreatePlanPass(ctx, generated.CreatePlanPassParams{
			PlanRowID:                      createdPlan.ID,
			PassID:                         pass.PassID,
			Sequence:                       pass.Sequence,
			Name:                           pass.Name,
			Goal:                           pass.Goal,
			IntendedExecutionScopeJson:     intendedJSON,
			NonGoalsJson:                   nonGoalsJSON,
			DependenciesJson:               dependenciesJSON,
			Status:                         pass.Status,
			PassType:                       pass.PassType,
			ContextPlanJson:                contextPlanJSON,
			SourceSnapshotRequirementsJson: sourceSnapshotRequirementsJSON,
			HandoffReadinessCriteriaJson:   handoffReadinessCriteriaJSON,
			RiskLevel:                      pass.RiskLevel,
			ContextBudgetJson:              contextBudgetJSON,
			RawPassJson:                    rawPassJSON,
		})
		if err != nil {
			result.Report.addIssue(IssuePlanStorageFailed, "$.passes", "failed to store plan passes")
			return result, fmt.Errorf("create plan pass %q: %w", pass.PassID, err)
		}
		createdPasses = append(createdPasses, createdPass)
	}

	if err := tx.Commit(); err != nil {
		result.Report.addIssue(IssuePlanStorageFailed, "$", "failed to commit plan submission")
		return result, fmt.Errorf("commit plan submission: %w", err)
	}
	committed = true

	result.Plan = createdPlan
	result.Passes = createdPasses
	result.Report = report
	return result, nil
}

func marshalStringSlice(values []string) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func marshalJSONValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func marshalOptionalJSONObject(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	return marshalJSONValue(value)
}
