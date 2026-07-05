package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/xeipuuv/gojsonschema"

	"relay/internal/validation"
)

func (svc *Service) ValidatePlanJSON(ctx context.Context, raw []byte) (*PlannerPassPlan, PlanValidationReport, error) {
	_ = ctx

	report := newValidationReport()
	if len(strings.TrimSpace(string(raw))) == 0 {
		report.addIssue(IssuePlanJSONSyntax, "$", "plan JSON input is empty")
		return nil, report, nil
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		report.addIssue(IssuePlanJSONSyntax, "$", fmt.Sprintf("invalid JSON syntax: %v", err))
		return nil, report, nil
	}

	if err := svc.validateAgainstSchema(doc, &report); err != nil {
		return nil, report, err
	}

	var plan PlannerPassPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		report.addIssue(IssuePlanSchemaInvalid, "$", fmt.Sprintf("plan JSON does not match the Plan v2 structure: %v", err))
		return nil, report, nil
	}

	scanForSecrets("$", doc, &report)
	validatePlanSemantics(&plan, &report)
	report.finalize()

	if !report.Valid {
		return nil, report, nil
	}

	return &plan, report, nil
}

func (svc *Service) validateAgainstSchema(doc any, report *PlanValidationReport) error {
	schemaPath, found := locateSchemaFile(svc.schemaPath)
	if !found {
		return nil
	}

	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read plan schema %q: %w", schemaPath, err)
	}

	schemaBytes, err = sanitizePlanSchemaForRuntime(schemaBytes)
	if err != nil {
		return fmt.Errorf("prepare plan schema %q: %w", schemaPath, err)
	}

	result, err := gojsonschema.Validate(
		gojsonschema.NewBytesLoader(schemaBytes),
		gojsonschema.NewGoLoader(doc),
	)
	if err != nil {
		return fmt.Errorf("validate plan schema: %w", err)
	}

	if !result.Valid() {
		for _, schemaErr := range result.Errors() {
			report.addIssue(
				IssuePlanSchemaInvalid,
				schemaErrorPath(schemaErr),
				schemaErr.String(),
			)
		}
	}

	return nil
}

func sanitizePlanSchemaForRuntime(schemaBytes []byte) ([]byte, error) {
	var schemaDoc map[string]any
	if err := json.Unmarshal([]byte(sanitizePlanSchemaRegexes(string(schemaBytes))), &schemaDoc); err != nil {
		return nil, err
	}
	allowRuntimeRefactorMetadata(schemaDoc)

	return json.Marshal(schemaDoc)
}

func allowRuntimeRefactorMetadata(schemaDoc map[string]any) {
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			props, _ := typed["properties"].(map[string]any)
			if passType, ok := props["pass_type"].(map[string]any); ok {
				props["refactor_candidate"] = map[string]any{"type": "object"}
				values, _ := passType["enum"].([]any)
				for _, value := range values {
					if value == "refactor" {
						return
					}
				}
				passType["enum"] = append(values, "refactor")
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(schemaDoc)
}

func sanitizePlanSchemaRegexes(schemaContent string) string {
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*[\\r\\n])`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!\\s*$)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!\\s)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*\\s$)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*[\\u0000-\\u001F\\u007F])`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!handoffs/(?:requirements|design)/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?![A-Za-z]:)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!//)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(^|/)\\.\\.($|/))`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(^|/)\\.($|/))`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(?:^|/)\\.\\.?$)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(?:^|/)\\.\\.?/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*\\\\)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?:`, `(`)
	return schemaContent
}

var contextFileReadSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._@+=:-]+$`)

func isSafeRepoRelativePath(path string) bool {
	if path == "" || len(path) > 260 {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, `\`) {
		return false
	}

	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || !contextFileReadSegmentPattern.MatchString(part) {
			return false
		}
	}

	return true
}

func validatePlanSemantics(plan *PlannerPassPlan, report *PlanValidationReport) {
	validateNonEmptyString(report, "$.plan_meta.plan_id", plan.PlanMeta.PlanID, "plan_meta.plan_id")
	validateNonEmptyString(report, "$.plan_meta.schema_version", plan.PlanMeta.SchemaVersion, "plan_meta.schema_version")
	validateNonEmptyString(report, "$.plan_meta.title", plan.PlanMeta.Title, "plan_meta.title")
	validateNonEmptyString(report, "$.plan_meta.goal", plan.PlanMeta.Goal, "plan_meta.goal")
	validateNonEmptyString(report, "$.plan_meta.repo_target", plan.PlanMeta.RepoTarget, "plan_meta.repo_target")
	validateNonEmptyString(report, "$.plan_meta.branch_context", plan.PlanMeta.BranchContext, "plan_meta.branch_context")
	validateNonEmptyString(report, "$.source_intent.summary", plan.SourceIntent.Summary, "source_intent.summary")
	validatePlanV2SchemaVersion(report, plan.PlanMeta.SchemaVersion)

	if strings.TrimSpace(plan.PlanMeta.Status) != "active" {
		report.addIssue(
			IssuePlanStatusInvalidForSubmission,
			"$.plan_meta.status",
			"plan_meta.status must be \"active\" for initial submission",
		)
	}
	validateProjectContext(report, "$.plan_meta.project_context", plan.PlanMeta.ProjectContext)
	validateMCPCapabilityProfile(report, "$.plan_meta.mcp_capability_profile", plan.PlanMeta.MCPCapabilityProfile)
	validateGlobalContextRules(report, "$.global_context_rules", plan.GlobalContextRules)
	validateRefactorPlanMetadata(report, "$.plan_meta.refactor_plan_metadata", plan.PlanMeta.RefactorPlanMetadata)

	if len(plan.Passes) == 0 {
		report.addIssue(IssuePlanEmptyRequiredArray, "$.passes", "passes must contain at least one pass")
		return
	}

	passIDs := make(map[string]int)
	sequences := make(map[int64]int)
	for idx, pass := range plan.Passes {
		passPath := fmt.Sprintf("$.passes[%d]", idx)

		validateNonEmptyString(report, passPath+".pass_id", pass.PassID, "passes.pass_id")
		validateNonEmptyString(report, passPath+".name", pass.Name, "passes.name")
		validateNonEmptyString(report, passPath+".goal", pass.Goal, "passes.goal")
		validateRequiredStringSlice(report, passPath+".intended_execution_scope", pass.IntendedExecutionScope, "passes.intended_execution_scope")
		validateRequiredStringSlice(report, passPath+".non_goals", pass.NonGoals, "passes.non_goals")
		validateNonEmptyString(report, passPath+".pass_type", pass.PassType, "passes.pass_type")
		validateContextPlan(report, passPath+".context_plan", pass.ContextPlan)
		validateSourceSnapshotRequirements(report, passPath+".source_snapshot_requirements", pass.SourceSnapshotRequirements)
		validateRequiredStringSlice(report, passPath+".handoff_readiness_criteria", pass.HandoffReadinessCriteria, "passes.handoff_readiness_criteria")
		validateRefactorCandidate(report, passPath, pass.PassType, pass.RefactorCandidate)

		if pass.Sequence < 1 {
			report.addIssue(IssuePlanEmptyRequiredValue, passPath+".sequence", "passes.sequence must be greater than or equal to 1")
		}

		if !IsInitialPlanPassStatus(strings.TrimSpace(pass.Status)) {
			report.addIssue(
				IssuePlanPassStatusInvalid,
				passPath+".status",
				"pass status must be \"planned\" for initial submission",
			)
		}

		trimmedPassID := strings.TrimSpace(pass.PassID)
		if trimmedPassID != "" {
			passIDs[trimmedPassID]++
			if passIDs[trimmedPassID] > 1 {
				report.addIssue(
					IssuePlanDuplicatePassID,
					passPath+".pass_id",
					fmt.Sprintf("duplicate pass_id %q", trimmedPassID),
				)
			}
		}

		sequences[pass.Sequence]++
		if sequences[pass.Sequence] > 1 {
			report.addIssue(
				IssuePlanDuplicateSequence,
				passPath+".sequence",
				fmt.Sprintf("duplicate sequence %d", pass.Sequence),
			)
		}
	}

	validPassIDs := make(map[string]struct{}, len(passIDs))
	for passID := range passIDs {
		validPassIDs[passID] = struct{}{}
	}

	for idx, pass := range plan.Passes {
		passPath := fmt.Sprintf("$.passes[%d]", idx)
		seenDeps := make(map[string]struct{}, len(pass.Dependencies))
		for depIdx, dep := range pass.Dependencies {
			depPath := fmt.Sprintf("%s.dependencies[%d]", passPath, depIdx)
			trimmedDep := strings.TrimSpace(dep)
			if trimmedDep == "" {
				report.addIssue(IssuePlanEmptyRequiredValue, depPath, "dependency values must be non-empty")
				continue
			}
			if _, exists := seenDeps[trimmedDep]; exists {
				report.addIssue(
					IssuePlanDependencyDuplicate,
					depPath,
					fmt.Sprintf("duplicate dependency %q", trimmedDep),
				)
			}
			seenDeps[trimmedDep] = struct{}{}

			if trimmedDep == strings.TrimSpace(pass.PassID) {
				report.addIssue(
					IssuePlanDependencySelf,
					depPath,
					fmt.Sprintf("pass %q cannot depend on itself", trimmedDep),
				)
			}
			if _, exists := validPassIDs[trimmedDep]; !exists {
				report.addIssue(
					IssuePlanDependencyUnknown,
					depPath,
					fmt.Sprintf("dependency %q does not match any pass_id", trimmedDep),
				)
			}
		}
	}
}

func validatePlanV2SchemaVersion(report *PlanValidationReport, schemaVersion string) {
	parts := strings.Split(strings.TrimSpace(schemaVersion), ".")
	if len(parts) == 0 || parts[0] != "2" {
		report.addIssue(
			IssuePlanSchemaInvalid,
			"$.plan_meta.schema_version",
			"plan_meta.schema_version must be Plan v2-compatible (major version 2)",
		)
	}
}

func validateProjectContext(report *PlanValidationReport, path string, projectContext *ProjectContext) {
	if projectContext == nil {
		return
	}

	validateNonEmptyString(report, path+".primary_project", projectContext.PrimaryProject, "plan_meta.project_context.primary_project")
	validateNonEmptyString(report, path+".primary_repository", projectContext.PrimaryRepository, "plan_meta.project_context.primary_repository")
	validateNonEmptyString(report, path+".github_role", projectContext.GitHubRole, "plan_meta.project_context.github_role")
}

func validateMCPCapabilityProfile(report *PlanValidationReport, path string, profile *MCPCapabilityProfile) {
	if profile == nil {
		return
	}

	validateNonEmptyString(report, path+".profile_id", profile.ProfileID, "plan_meta.mcp_capability_profile.profile_id")
	validateNonEmptyString(report, path+".mode", profile.Mode, "plan_meta.mcp_capability_profile.mode")
	validateRequiredBool(report, path+".context_broker_enabled", profile.ContextBrokerEnabled, "plan_meta.mcp_capability_profile.context_broker_enabled")
}

func validateGlobalContextRules(report *PlanValidationReport, path string, rules *GlobalContextRules) {
	if rules == nil {
		return
	}

	validateNonEmptyString(report, path+".default_source_of_truth", rules.DefaultSourceOfTruth, "global_context_rules.default_source_of_truth")
	validateNonEmptyString(report, path+".planner_context_boundary", rules.PlannerContextBoundary, "global_context_rules.planner_context_boundary")
	validateRequiredStringSlice(report, path+".forbidden_context_domains", rules.ForbiddenContextDomains, "global_context_rules.forbidden_context_domains")
}

func validateRefactorPlanMetadata(report *PlanValidationReport, path string, metadata *RefactorPlanMetadata) {
	if metadata == nil {
		return
	}

	if strings.TrimSpace(metadata.Source) != "selected_refactor_candidates" {
		report.addIssue(
			IssuePlanRefactorMetadataInvalid,
			path+".source",
			"plan_meta.refactor_plan_metadata.source must be \"selected_refactor_candidates\"",
		)
	}

	if strings.TrimSpace(metadata.SubmissionPolicy) != "review_required_no_auto_submit" {
		report.addIssue(
			IssuePlanRefactorMetadataInvalid,
			path+".submission_policy",
			"plan_meta.refactor_plan_metadata.submission_policy must be \"review_required_no_auto_submit\"",
		)
	}

	validateNonEmptyUniqueStrings(
		report,
		path+".source_candidate_ids",
		metadata.SourceCandidateIDs,
		"plan_meta.refactor_plan_metadata.source_candidate_ids",
		true,
	)
}

func validateRefactorCandidate(report *PlanValidationReport, passPath string, passType string, candidate *RefactorCandidateMetadata) {
	path := passPath + ".refactor_candidate"
	isRefactorPass := strings.TrimSpace(passType) == "refactor"

	if !isRefactorPass {
		if candidate != nil {
			report.addIssue(
				IssuePlanRefactorMetadataInvalid,
				path,
				"refactor_candidate is only allowed when pass_type is \"refactor\"",
			)
		}
		return
	}

	if candidate == nil {
		report.addIssue(
			IssuePlanRefactorMetadataInvalid,
			path,
			"refactor_candidate is required when pass_type is \"refactor\"",
		)
		return
	}

	validateNonEmptyString(report, path+".candidate_id", candidate.CandidateID, "passes.refactor_candidate.candidate_id")

	if strings.TrimSpace(candidate.Source) != "refactor_backlog_candidate" {
		report.addIssue(
			IssuePlanRefactorMetadataInvalid,
			path+".source",
			"passes.refactor_candidate.source must be \"refactor_backlog_candidate\"",
		)
	}

	switch strings.TrimSpace(candidate.SchedulingMode) {
	case "existing_plan_bonus_pass", "generated_refactor_only_plan":
	default:
		report.addIssue(
			IssuePlanRefactorMetadataInvalid,
			path+".scheduling_mode",
			"passes.refactor_candidate.scheduling_mode must be \"existing_plan_bonus_pass\" or \"generated_refactor_only_plan\"",
		)
	}

	if candidate.SourceDiscoveryTaskIDs != nil {
		validateNonEmptyUniqueStrings(
			report,
			path+".source_discovery_task_ids",
			candidate.SourceDiscoveryTaskIDs,
			"passes.refactor_candidate.source_discovery_task_ids",
			false,
		)
	}
}

// validateNonEmptyUniqueStrings reports empty or duplicate entries in a string
// slice using the refactor metadata issue code. When requireNonEmptySlice is
// true, an absent or empty slice is also reported.
func validateNonEmptyUniqueStrings(report *PlanValidationReport, path string, values []string, fieldName string, requireNonEmptySlice bool) {
	if len(values) == 0 {
		if requireNonEmptySlice {
			report.addIssue(IssuePlanRefactorMetadataInvalid, path, fmt.Sprintf("%s must contain at least one item", fieldName))
		}
		return
	}

	seen := make(map[string]struct{}, len(values))
	for idx, value := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, idx)
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			report.addIssue(IssuePlanRefactorMetadataInvalid, itemPath, fmt.Sprintf("%s items must be non-empty", fieldName))
			continue
		}
		if _, exists := seen[trimmed]; exists {
			report.addIssue(IssuePlanRefactorMetadataInvalid, itemPath, fmt.Sprintf("%s items must be unique", fieldName))
			continue
		}
		seen[trimmed] = struct{}{}
	}
}

func validateContextPlan(report *PlanValidationReport, path string, contextPlan ContextPlan) {
	validateRequiredStringSlice(report, path+".required_repositories", contextPlan.RequiredRepositories, "passes.context_plan.required_repositories")
	validateRequiredStringSlice(report, path+".context_coverage_expectations", contextPlan.ContextCoverageExpectations, "passes.context_plan.context_coverage_expectations")
	validateRequiredStringSlice(report, path+".blocked_if_missing", contextPlan.BlockedIfMissing, "passes.context_plan.blocked_if_missing")

	if len(contextPlan.SeedSearchTerms) == 0 {
		report.addIssue(IssuePlanEmptyRequiredArray, path+".seed_search_terms", "passes.context_plan.seed_search_terms must contain at least one item")
	} else {
		for idx, term := range contextPlan.SeedSearchTerms {
			termPath := fmt.Sprintf("%s.seed_search_terms[%d]", path, idx)
			validateNonEmptyString(report, termPath+".repo_id", term.RepoID, "passes.context_plan.seed_search_terms.repo_id")
			validateNonEmptyString(report, termPath+".query", term.Query, "passes.context_plan.seed_search_terms.query")
			validateNonEmptyString(report, termPath+".purpose", term.Purpose, "passes.context_plan.seed_search_terms.purpose")
			validateRequiredBool(report, termPath+".required", term.Required, "passes.context_plan.seed_search_terms.required")
		}
	}

	if len(contextPlan.SeedFilesToRead) == 0 {
		report.addIssue(IssuePlanEmptyRequiredArray, path+".seed_files_to_read", "passes.context_plan.seed_files_to_read must contain at least one item")
		return
	}

	for idx, fileRead := range contextPlan.SeedFilesToRead {
		filePath := fmt.Sprintf("%s.seed_files_to_read[%d]", path, idx)
		validateNonEmptyString(report, filePath+".repo_id", fileRead.RepoID, "passes.context_plan.seed_files_to_read.repo_id")
		validateNonEmptyString(report, filePath+".path", fileRead.Path, "passes.context_plan.seed_files_to_read.path")
		validateNonEmptyString(report, filePath+".purpose", fileRead.Purpose, "passes.context_plan.seed_files_to_read.purpose")
		validateRequiredBool(report, filePath+".required", fileRead.Required, "passes.context_plan.seed_files_to_read.required")
		if trimmedPath := strings.TrimSpace(fileRead.Path); trimmedPath != "" && !isSafeRepoRelativePath(trimmedPath) {
			report.addIssue(
				IssuePlanSchemaInvalid,
				filePath+".path",
				"context file read paths must be safe repo-relative paths",
			)
		}
	}
}

func validateSourceSnapshotRequirements(report *PlanValidationReport, path string, requirements SourceSnapshotRequirements) {
	validateRequiredBool(report, path+".require_git_status", requirements.RequireGitStatus, "passes.source_snapshot_requirements.require_git_status")
	validateRequiredBool(report, path+".require_commit_sha", requirements.RequireCommitSHA, "passes.source_snapshot_requirements.require_commit_sha")
	validateRequiredBool(report, path+".allow_dirty_worktree", requirements.AllowDirtyWorktree, "passes.source_snapshot_requirements.allow_dirty_worktree")
}

func validateNonEmptyString(report *PlanValidationReport, path string, value string, fieldName string) {
	if strings.TrimSpace(value) == "" {
		report.addIssue(IssuePlanEmptyRequiredValue, path, fmt.Sprintf("%s must be non-empty", fieldName))
	}
}

func validateRequiredBool(report *PlanValidationReport, path string, value *bool, fieldName string) {
	if value == nil {
		report.addIssue(IssuePlanEmptyRequiredValue, path, fmt.Sprintf("%s must be provided", fieldName))
	}
}

func validateRequiredStringSlice(report *PlanValidationReport, path string, values []string, fieldName string) {
	if len(values) == 0 {
		report.addIssue(IssuePlanEmptyRequiredArray, path, fmt.Sprintf("%s must contain at least one item", fieldName))
		return
	}

	for idx, value := range values {
		if strings.TrimSpace(value) == "" {
			report.addIssue(
				IssuePlanEmptyRequiredValue,
				fmt.Sprintf("%s[%d]", path, idx),
				fmt.Sprintf("%s items must be non-empty", fieldName),
			)
		}
	}
}

func scanForSecrets(path string, value any, report *PlanValidationReport) {
	switch typed := value.(type) {
	case string:
		if validation.HasSecret(typed) {
			report.addIssue(IssuePlanSecretDetected, path, "secret-like content detected")
		}
	case []any:
		for idx, item := range typed {
			scanForSecrets(fmt.Sprintf("%s[%d]", path, idx), item, report)
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			scanForSecrets(path+"."+key, typed[key], report)
		}
	}
}

func schemaErrorPath(schemaErr gojsonschema.ResultError) string {
	field := strings.TrimSpace(schemaErr.Field())
	if field == "" || field == "(root)" {
		return "$"
	}
	if strings.HasPrefix(field, "(") {
		return "$"
	}
	return "$." + field
}

func locateSchemaFile(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	if _, err := os.Stat(path); err == nil {
		return path, true
	}

	searchDir := "."
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(searchDir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		searchDir = filepath.Join(searchDir, "..")
	}

	return "", false
}

func newValidationReport() PlanValidationReport {
	return PlanValidationReport{
		Valid:  true,
		Issues: make([]PlanValidationIssue, 0),
	}
}

func (r *PlanValidationReport) addIssue(code string, path string, message string) {
	r.Valid = false
	r.Issues = append(r.Issues, PlanValidationIssue{
		Code:    code,
		Path:    path,
		Message: message,
	})
}

func (r *PlanValidationReport) finalize() {
	if len(r.Issues) == 0 {
		r.Valid = true
		return
	}
	r.Valid = false
}

func (r *PlanValidationReport) AddIssue(code string, path string, message string) {
	r.addIssue(code, path, message)
}

func (r *PlanValidationReport) Finalize() {
	r.finalize()
}

func IsInitialPlanPassStatus(status string) bool {
	return status == "planned"
}

func ResolvePlanProjectID(explicitProjectID string, plan *PlannerPassPlan) string {
	if explicitProjectID != "" {
		return explicitProjectID
	}
	if plan == nil {
		return ""
	}
	if plan.PlanMeta.ProjectID != "" {
		return plan.PlanMeta.ProjectID
	}
	if plan.PlanMeta.ProjectContext != nil && plan.PlanMeta.ProjectContext.PrimaryProject != "" {
		return plan.PlanMeta.ProjectContext.PrimaryProject
	}
	return ""
}
