package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		report.addIssue(IssuePlanJSONSyntax, "$", fmt.Sprintf("invalid JSON syntax: %v", err))
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

func validatePlanSemantics(plan *PlannerPassPlan, report *PlanValidationReport) {
	validateNonEmptyString(report, "$.plan_meta.plan_id", plan.PlanMeta.PlanID, "plan_meta.plan_id")
	validateNonEmptyString(report, "$.plan_meta.title", plan.PlanMeta.Title, "plan_meta.title")
	validateNonEmptyString(report, "$.plan_meta.goal", plan.PlanMeta.Goal, "plan_meta.goal")
	validateNonEmptyString(report, "$.plan_meta.repo_target", plan.PlanMeta.RepoTarget, "plan_meta.repo_target")
	validateNonEmptyString(report, "$.plan_meta.branch_context", plan.PlanMeta.BranchContext, "plan_meta.branch_context")
	validateNonEmptyString(report, "$.source_intent.summary", plan.SourceIntent.Summary, "source_intent.summary")

	if strings.TrimSpace(plan.PlanMeta.Status) != "active" {
		report.addIssue(
			IssuePlanStatusInvalidForSubmission,
			"$.plan_meta.status",
			"plan_meta.status must be \"active\" for initial submission",
		)
	}

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

		if pass.Sequence < 1 {
			report.addIssue(IssuePlanEmptyRequiredValue, passPath+".sequence", "passes.sequence must be greater than or equal to 1")
		}

		if strings.TrimSpace(pass.Status) != "planned" {
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

func validateNonEmptyString(report *PlanValidationReport, path string, value string, fieldName string) {
	if strings.TrimSpace(value) == "" {
		report.addIssue(IssuePlanEmptyRequiredValue, path, fmt.Sprintf("%s must be non-empty", fieldName))
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
