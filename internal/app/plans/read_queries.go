package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"relay/internal/store"
)

// Sentinel errors for plan read/query composition. The API transport adapter
// maps these to the existing HTTP responses without owning store queries.
var (
	// ErrPlanNotFound indicates the requested plan ID does not exist.
	ErrPlanNotFound = errors.New("plan not found")
	// ErrPlanProjectMismatch indicates the plan exists but does not belong to
	// the project filter supplied with the request.
	ErrPlanProjectMismatch = errors.New("plan not found in project")
	// ErrPlanPassNotFound indicates the requested pass ID does not exist on the
	// resolved plan.
	ErrPlanPassNotFound = errors.New("plan pass not found")
)

// PlanListQuery describes a plan list/query request resolved from HTTP query
// parameters. ProjectID is optional; an empty value lists plans globally.
type PlanListQuery struct {
	Status    string
	Limit     int64
	ProjectID string
}

// PlanReadSummary is a DTO-neutral list entry combining a plan row, its pass
// rows, and computed completion readiness.
type PlanReadSummary struct {
	Plan            store.Plan
	Passes          []store.PlanPass
	CompletionReady bool
}

// PlanDetailQuery describes a single plan detail request.
type PlanDetailQuery struct {
	PlanID    string
	ProjectID string
}

// PlanDetailResult carries the composed read model for a single plan, including
// associated runs keyed by plan-pass row ID.
type PlanDetailResult struct {
	Plan            store.Plan
	Passes          []store.PlanPass
	RunsByPass      map[int64][]store.Run
	CompletionReady bool
}

// PlanPassDetailQuery describes a single plan-pass detail request.
type PlanPassDetailQuery struct {
	PlanID    string
	PassID    string
	ProjectID string
}

// PlanPassDetailResult carries the composed read model for a single plan pass.
type PlanPassDetailResult struct {
	Plan            store.Plan
	Passes          []store.PlanPass
	Pass            store.PlanPass
	AssociatedRuns  []store.Run
	CompletionReady bool
}

// completionReadyFromPasses reports whether every pass is terminal. An empty
// pass set is never completion ready. This mirrors RunLifecycleService.CompletionReady
// while reusing already-loaded pass rows.
func completionReadyFromPasses(passes []store.PlanPass) bool {
	if len(passes) == 0 {
		return false
	}
	for _, pass := range passes {
		if pass.Status != StatusPassCompleted && pass.Status != StatusPassSkipped {
			return false
		}
	}
	return true
}

// ValidatePlanForSubmission validates a raw plan payload and resolves project
// existence for the validate endpoint. It preserves the prior HTTP-handler
// behavior: invalid reports are returned unchanged, project-required and
// project-unknown issues are appended for valid plans, and the report is
// finalized after project checks.
func (svc *Service) ValidatePlanForSubmission(ctx context.Context, rawPlan []byte, requestProjectID string) (PlanValidationReport, error) {
	plan, report, err := svc.ValidatePlanJSON(ctx, rawPlan)
	if err != nil {
		return PlanValidationReport{}, err
	}
	if !report.Valid {
		return report, nil
	}

	projectID := ResolvePlanProjectID(requestProjectID, plan)
	if projectID == "" {
		report.addIssue(
			IssuePlanProjectRequired,
			"$.plan_meta.project_id",
			"project_id is required",
		)
	} else if _, err := svc.store.GetProjectByProjectID(projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			report.addIssue(
				IssuePlanProjectUnknown,
				"$.plan_meta.project_id",
				fmt.Sprintf("project_id %q is unknown", projectID),
			)
		} else {
			return report, fmt.Errorf("lookup project: %w", err)
		}
	}
	report.finalize()
	return report, nil
}

// ListPlanReadSummaries composes the plan list read model. An unknown project
// filter returns an empty list without error. Per-plan pass and completion
// failures are best-effort, matching prior list semantics.
func (svc *Service) ListPlanReadSummaries(ctx context.Context, query PlanListQuery) ([]PlanReadSummary, error) {
	_ = ctx

	var projectRowID int64
	if query.ProjectID != "" {
		project, err := svc.store.GetProjectByProjectID(query.ProjectID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return []PlanReadSummary{}, nil
			}
			return nil, fmt.Errorf("lookup project: %w", err)
		}
		projectRowID = project.ID
	}

	var planRows []store.Plan
	var listErr error
	if projectRowID > 0 {
		if query.Status == "" {
			planRows, listErr = svc.store.ListPlansByProject(projectRowID, query.Limit)
		} else {
			planRows, listErr = svc.store.ListPlansByProjectAndStatus(projectRowID, query.Status, query.Limit)
		}
	} else {
		if query.Status == "" {
			planRows, listErr = svc.store.ListPlans(query.Limit)
		} else {
			planRows, listErr = svc.store.ListPlansByStatus(query.Status, query.Limit)
		}
	}
	if listErr != nil {
		return nil, fmt.Errorf("list plans: %w", listErr)
	}

	summaries := make([]PlanReadSummary, 0, len(planRows))
	for _, plan := range planRows {
		passes, _ := svc.store.ListPlanPassesByPlan(plan.ID)
		summaries = append(summaries, PlanReadSummary{
			Plan:            plan,
			Passes:          passes,
			CompletionReady: completionReadyFromPasses(passes),
		})
	}
	return summaries, nil
}

// GetPlanDetail composes the single-plan read model. It returns ErrPlanNotFound
// for a missing plan and ErrPlanProjectMismatch when the project filter does not
// match the plan's project.
func (svc *Service) GetPlanDetail(ctx context.Context, query PlanDetailQuery) (*PlanDetailResult, error) {
	_ = ctx

	plan, err := svc.store.GetPlanByPlanID(query.PlanID)
	if err != nil {
		return nil, ErrPlanNotFound
	}

	if query.ProjectID != "" && plan.ProjectID != query.ProjectID {
		return nil, ErrPlanProjectMismatch
	}

	passes, err := svc.store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return nil, fmt.Errorf("list plan passes: %w", err)
	}

	associatedRuns, err := svc.store.ListRunsByPlan(plan.ID)
	if err != nil {
		return nil, fmt.Errorf("list associated runs: %w", err)
	}
	runsByPass := make(map[int64][]store.Run)
	for _, run := range associatedRuns {
		if run.PlanPassRowID.Valid {
			runsByPass[run.PlanPassRowID.Int64] = append(runsByPass[run.PlanPassRowID.Int64], run)
		}
	}

	return &PlanDetailResult{
		Plan:            *plan,
		Passes:          passes,
		RunsByPass:      runsByPass,
		CompletionReady: completionReadyFromPasses(passes),
	}, nil
}

// GetPlanPassDetail composes the single plan-pass read model. It returns
// ErrPlanNotFound, ErrPlanProjectMismatch, or ErrPlanPassNotFound to preserve
// the prior 404 responses.
func (svc *Service) GetPlanPassDetail(ctx context.Context, query PlanPassDetailQuery) (*PlanPassDetailResult, error) {
	_ = ctx

	plan, err := svc.store.GetPlanByPlanID(query.PlanID)
	if err != nil {
		return nil, ErrPlanNotFound
	}

	if query.ProjectID != "" && plan.ProjectID != query.ProjectID {
		return nil, ErrPlanProjectMismatch
	}

	pass, err := svc.store.GetPlanPassByPassID(plan.ID, query.PassID)
	if err != nil {
		return nil, ErrPlanPassNotFound
	}

	passes, _ := svc.store.ListPlanPassesByPlan(plan.ID)

	associatedRuns, err := svc.store.ListRunsByPlanPass(pass.ID)
	if err != nil {
		return nil, fmt.Errorf("list associated runs: %w", err)
	}

	return &PlanPassDetailResult{
		Plan:            *plan,
		Passes:          passes,
		Pass:            *pass,
		AssociatedRuns:  associatedRuns,
		CompletionReady: completionReadyFromPasses(passes),
	}, nil
}
