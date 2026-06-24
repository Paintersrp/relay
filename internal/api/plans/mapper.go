package plans

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	appplans "relay/internal/app/plans"
	"relay/internal/store"
)

func mapPlanToAPI(plan store.Plan) PlanAPIPlan {
	return PlanAPIPlan{
		ID:                  strconv.FormatInt(plan.ID, 10),
		PlanID:              plan.PlanID,
		SchemaVersion:       plan.SchemaVersion,
		Title:               plan.Title,
		Goal:                plan.Goal,
		RepoTarget:          plan.RepoTarget,
		BranchContext:       plan.BranchContext,
		Status:              plan.Status,
		SourceIntentSummary: plan.SourceIntentSummary,
		SourceArtifactPath:  plan.SourceArtifactPath,
		CreatedAt:           shared.ParseAndFormatTime(plan.CreatedAt),
		UpdatedAt:           shared.ParseAndFormatTime(plan.UpdatedAt),
		ProjectRowID:        strconv.FormatInt(plan.ProjectRowID, 10),
		ProjectID:           plan.ProjectID,
	}
}

func mapRunToPlanAPIRunSummary(run store.Run) PlanAPIRunSummary {
	idStr := strconv.FormatInt(run.ID, 10)
	activeStep := resolveRunStep(run.Status)
	return PlanAPIRunSummary{
		ID:             idStr,
		Title:          run.Title,
		Status:         run.Status,
		LifecycleState: resolveRunLifecycleState(run.Status),
		ActiveStep:     activeStep,
		WorkbenchPath:  fmt.Sprintf("/runs/%s/%s", idStr, activeStep),
		CreatedAt:      shared.ParseAndFormatTime(run.CreatedAt),
		UpdatedAt:      shared.ParseAndFormatTime(run.UpdatedAt),
	}
}

func mapPlanPassToAPI(pass store.PlanPass, associatedRuns []store.Run) PlanAPIPass {
	contextPlan, sourceRequirements, readinessCriteria, contextBudget, warnings := decodePlanPassContext(pass)
	runSummaries := make([]PlanAPIRunSummary, 0, len(associatedRuns))
	runIDs := make([]string, 0, len(associatedRuns))
	for _, run := range associatedRuns {
		summary := mapRunToPlanAPIRunSummary(run)
		runSummaries = append(runSummaries, summary)
		runIDs = append(runIDs, summary.ID)
	}

	return PlanAPIPass{
		ID:                         strconv.FormatInt(pass.ID, 10),
		PlanRowID:                  strconv.FormatInt(pass.PlanRowID, 10),
		PassID:                     pass.PassID,
		Sequence:                   pass.Sequence,
		Name:                       pass.Name,
		Goal:                       pass.Goal,
		IntendedExecutionScope:     decodeStoredStringSlice(pass.IntendedExecutionScopeJson),
		NonGoals:                   decodeStoredStringSlice(pass.NonGoalsJson),
		Dependencies:               decodeStoredStringSlice(pass.DependenciesJson),
		Status:                     pass.Status,
		AssociatedRunIDs:           runIDs,
		AssociatedRuns:             runSummaries,
		CreatedAt:                  shared.ParseAndFormatTime(pass.CreatedAt),
		UpdatedAt:                  shared.ParseAndFormatTime(pass.UpdatedAt),
		PassType:                   strings.TrimSpace(pass.PassType),
		ContextPlan:                contextPlan,
		SourceSnapshotRequirements: sourceRequirements,
		HandoffReadinessCriteria:   readinessCriteria,
		RiskLevel:                  strings.TrimSpace(pass.RiskLevel),
		ContextBudget:              contextBudget,
		ContextParseWarnings:       warnings,
	}
}

func buildPlanAPIReadPlan(plan store.Plan, passes []store.PlanPass, completionReady bool) PlanAPIReadPlan {
	apiPlan := mapPlanToAPI(plan)
	var completedPassCount int
	var inProgressPassCount int
	var plannedPassCount int
	var skippedPassCount int
	var currentPass *store.PlanPass
	var nextPass *store.PlanPass

	for i := range passes {
		pass := &passes[i]
		switch pass.Status {
		case "completed":
			completedPassCount++
		case "in_progress":
			inProgressPassCount++
			if currentPass == nil || pass.Sequence < currentPass.Sequence {
				currentPass = pass
			}
		case "planned":
			plannedPassCount++
			if nextPass == nil || pass.Sequence < nextPass.Sequence {
				nextPass = pass
			}
		case "skipped":
			skippedPassCount++
		}
	}

	return PlanAPIReadPlan{
		PlanAPIPlan:         apiPlan,
		PassCount:           len(passes),
		CompletionReady:     completionReady,
		CompletedPassCount:  completedPassCount,
		InProgressPassCount: inProgressPassCount,
		PlannedPassCount:    plannedPassCount,
		SkippedPassCount:    skippedPassCount,
		CurrentPassID:       planPassField(currentPass, func(p *store.PlanPass) string { return p.PassID }),
		CurrentPassName:     planPassField(currentPass, func(p *store.PlanPass) string { return p.Name }),
		CurrentPassGoal:     planPassField(currentPass, func(p *store.PlanPass) string { return p.Goal }),
		NextPassID:          planPassField(nextPass, func(p *store.PlanPass) string { return p.PassID }),
		NextPassName:        planPassField(nextPass, func(p *store.PlanPass) string { return p.Name }),
		NextPassGoal:        planPassField(nextPass, func(p *store.PlanPass) string { return p.Goal }),
	}
}

// decodePlanPassContext decodes stored JSON fields from a plan pass row into
// API presentation DTOs.
func decodePlanPassContext(pass store.PlanPass) (
	PlanAPIContextPlan,
	PlanAPISourceSnapshotRequirements,
	[]string,
	PlanAPIContextBudget,
	[]string,
) {
	contextPlan := PlanAPIContextPlan{
		RequiredRepositories:        []string{},
		SeedSearchTerms:             []PlanAPIContextSearchTerm{},
		SeedFilesToRead:             []PlanAPIContextFileRead{},
		ContextCoverageExpectations: []string{},
		BlockedIfMissing:            []string{},
	}
	sourceRequirements := PlanAPISourceSnapshotRequirements{}
	readinessCriteria := []string{}
	contextBudget := PlanAPIContextBudget{}
	warnings := []string{}

	var storedContextPlan appplans.ContextPlan
	if err := decodeJSONValue(pass.ContextPlanJson, &storedContextPlan); err != nil {
		appendParseWarning(&warnings, "contextPlan")
	} else {
		contextPlan = mapContextPlanToAPI(storedContextPlan)
	}

	var storedSourceRequirements appplans.SourceSnapshotRequirements
	if err := decodeJSONValue(pass.SourceSnapshotRequirementsJson, &storedSourceRequirements); err != nil {
		appendParseWarning(&warnings, "sourceSnapshotRequirements")
	} else {
		sourceRequirements = mapSourceSnapshotRequirementsToAPI(storedSourceRequirements)
	}

	if trimmed := strings.TrimSpace(pass.HandoffReadinessCriteriaJson); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &readinessCriteria); err != nil {
			appendParseWarning(&warnings, "handoffReadinessCriteria")
			readinessCriteria = []string{}
		}
	}

	var storedContextBudget appplans.ContextBudget
	if err := decodeJSONValue(pass.ContextBudgetJson, &storedContextBudget); err != nil {
		appendParseWarning(&warnings, "contextBudget")
	} else {
		contextBudget = mapContextBudgetToAPI(storedContextBudget)
	}

	return contextPlan, sourceRequirements, readinessCriteria, contextBudget, warnings
}

func mapContextSearchTermToAPI(term appplans.ContextSearchTerm) PlanAPIContextSearchTerm {
	return PlanAPIContextSearchTerm{
		RepoID:   term.RepoID,
		Query:    term.Query,
		Purpose:  term.Purpose,
		Required: term.Required,
	}
}

func mapContextFileReadToAPI(file appplans.ContextFileRead) PlanAPIContextFileRead {
	return PlanAPIContextFileRead{
		RepoID:   file.RepoID,
		Path:     file.Path,
		Purpose:  file.Purpose,
		Required: file.Required,
	}
}

func mapContextPlanToAPI(cp appplans.ContextPlan) PlanAPIContextPlan {
	searchTerms := make([]PlanAPIContextSearchTerm, 0, len(cp.SeedSearchTerms))
	for _, term := range cp.SeedSearchTerms {
		searchTerms = append(searchTerms, mapContextSearchTermToAPI(term))
	}

	filesToRead := make([]PlanAPIContextFileRead, 0, len(cp.SeedFilesToRead))
	for _, file := range cp.SeedFilesToRead {
		filesToRead = append(filesToRead, mapContextFileReadToAPI(file))
	}

	return PlanAPIContextPlan{
		RequiredRepositories:        cloneStringSlice(cp.RequiredRepositories),
		SeedSearchTerms:             searchTerms,
		SeedFilesToRead:             filesToRead,
		ContextCoverageExpectations: cloneStringSlice(cp.ContextCoverageExpectations),
		BlockedIfMissing:            cloneStringSlice(cp.BlockedIfMissing),
	}
}

func mapSourceSnapshotRequirementsToAPI(r appplans.SourceSnapshotRequirements) PlanAPISourceSnapshotRequirements {
	return PlanAPISourceSnapshotRequirements{
		RequireGitStatus:   r.RequireGitStatus,
		RequireCommitSHA:   r.RequireCommitSHA,
		AllowDirtyWorktree: r.AllowDirtyWorktree,
	}
}

func mapContextBudgetToAPI(b appplans.ContextBudget) PlanAPIContextBudget {
	return PlanAPIContextBudget{
		MaxFiles:         b.MaxFiles,
		MaxBytes:         b.MaxBytes,
		MaxSearchResults: b.MaxSearchResults,
		MaxContextLines:  b.MaxContextLines,
	}
}

// ---- helpers ----

func rawPlanFromRequest(req PlanAPIRequest) ([]byte, bool, error) {
	raw := []byte(req.Plan)
	if len(raw) == 0 {
		return nil, false, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, false, nil
	}
	return []byte(trimmed), true, nil
}

func hasPlanIssue(report appplans.PlanValidationReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func decodeStoredStringSlice(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return []string{}
	}
	if items == nil {
		return []string{}
	}
	return items
}

func cloneStringSlice(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	return append([]string{}, items...)
}

func appendParseWarning(warnings *[]string, field string) {
	if len(*warnings) >= 4 {
		return
	}
	*warnings = append(*warnings, fmt.Sprintf("%s could not be decoded from persisted JSON; using an empty value.", field))
}

func decodeJSONValue[T any](value string, target *T) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return json.Unmarshal([]byte(trimmed), target)
}

func planPassField(pass *store.PlanPass, selector func(*store.PlanPass) string) string {
	if pass == nil {
		return ""
	}
	return selector(pass)
}

func resolveRunStep(status string) string {
	switch status {
	case "draft", "needs_cleanup",
		"intake_received", "intake_needs_review",
		"validated", "needs_review",
		"intake_approved", "intake_rejected", "intake_blocked":
		return "intake"
	case "approved_for_prepare",
		"packet_ready", "packet_validated", "packet_validation_failed",
		"repair_validated",
		"brief_ready_for_review", "brief_validation_failed":
		return "prepare"
	case "approved_for_executor",
		"executor_dispatched",
		"executor_running", "executor_done", "executor_blocked",
		"executor_error", "executor_cancelled",
		"agent_done", "agent_blocked", "agent_result_needs_review",
		"local_validation_running":
		return "execute"
	case "validation_passed", "validation_failed_accepted", "validation_failed",
		"audit_ready", "audit_ready_for_review",
		"revision_required",
		"accepted", "accepted_with_warnings",
		"completed",
		"audit_pending", "audit_generated", "audit_submitted",
		"audit_approved", "audit_approved_with_warnings",
		"audit_revision_requested", "audit_closed", "closed":
		return "audit"
	case "blocked":
		return "intake"
	default:
		return "intake"
	}
}

func resolveRunLifecycleState(status string) string {
	switch status {
	case "executor_blocked", "agent_blocked", "validation_failed", "blocked":
		return "failed"
	case "completed":
		return "completed"
	default:
		return resolveRunStep(status)
	}
}
