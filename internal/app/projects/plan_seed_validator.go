package projects

import (
	"encoding/json"
	"fmt"
	"strings"
)

var planSeedSecretLikeMarkers = []string{
	"BEGIN PRIVATE KEY",
	"Authorization:",
	"Bearer ",
	"xoxb-",
	"ghp_",
	"sk-",
}

func containsPlanSeedSecretLikeValue(value string) bool {
	for _, marker := range planSeedSecretLikeMarkers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func planSeedValidationIssue(field, code, message string) PlanSeedValidationIssue {
	return PlanSeedValidationIssue{
		Field:   field,
		Code:    code,
		Message: message,
	}
}

func scanPlanSeedSecretLikeStrings(field string, values ...string) []PlanSeedValidationIssue {
	for _, v := range values {
		if containsPlanSeedSecretLikeValue(v) {
			return []PlanSeedValidationIssue{{
				Field:   field,
				Code:    PlanSeedIssueSecretLikeValue,
				Message: fmt.Sprintf("%s contains a value that looks like a secret/credential and was rejected", field),
			}}
		}
	}
	return nil
}

func normalizePlanSeedStringSlice(values []string) []string {
	var normalized []string
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func marshalPlanSeedStringSlice(field string, values []string) (string, []PlanSeedValidationIssue) {
	marshalled, err := json.Marshal(values)
	if err != nil {
		return "[]", []PlanSeedValidationIssue{
			planSeedValidationIssue(field, "invalid_json", fmt.Sprintf("%s must marshal to a JSON string array", field)),
		}
	}
	return string(marshalled), nil
}

func normalizePlanSeedListLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultListPlanSeedsLimit
	}
	if limit > MaxListPlanSeedsLimit {
		return MaxListPlanSeedsLimit
	}
	return limit
}

func isValidPlanSeedStatus(status string) bool {
	switch status {
	case PlanSeedStatusCaptured, PlanSeedStatusPlanned, PlanSeedStatusDeferred, PlanSeedStatusRejected:
		return true
	default:
		return false
	}
}

func isValidPlanSeedSourceType(sourceType string) bool {
	switch sourceType {
	case PlanSeedSourceManual, PlanSeedSourceChat, PlanSeedSourceMCP:
		return true
	default:
		return false
	}
}

func NormalizePlanSeedInput(input PlanSeedInput, requireSeedID bool) (NormalizedPlanSeedInput, []PlanSeedValidationIssue) {
	normalized := NormalizedPlanSeedInput{
		SeedID:       strings.TrimSpace(input.SeedID),
		Title:        strings.TrimSpace(input.Title),
		QuickContext: strings.TrimSpace(input.QuickContext),
		Constraints:  normalizePlanSeedStringSlice(input.Constraints),
		NonGoals:     normalizePlanSeedStringSlice(input.NonGoals),
		Tags:         normalizePlanSeedStringSlice(input.Tags),
		Priority:     strings.TrimSpace(input.Priority),
		SourceType:   strings.TrimSpace(input.SourceType),
		SourceLabel:  strings.TrimSpace(input.SourceLabel),
		SourceRefID:  strings.TrimSpace(input.SourceRefID),
	}

	if normalized.Priority == "" {
		normalized.Priority = DefaultPlanSeedPriority
	}
	if normalized.SourceType == "" {
		normalized.SourceType = PlanSeedSourceManual
	}

	var issues []PlanSeedValidationIssue

	if requireSeedID && normalized.SeedID == "" {
		issues = append(issues, planSeedValidationIssue("seed_id", PlanSeedIssueRequired, "seed_id is required"))
	}
	if normalized.Title == "" {
		issues = append(issues, planSeedValidationIssue("title", PlanSeedIssueRequired, "title is required"))
	}
	if normalized.QuickContext == "" {
		issues = append(issues, planSeedValidationIssue("quick_context", PlanSeedIssueRequired, "quick_context is required"))
	}
	if !isValidPlanSeedSourceType(normalized.SourceType) {
		issues = append(issues, planSeedValidationIssue("source_type", PlanSeedIssueInvalidSourceType, "source_type is invalid"))
	}

	// Secret validation
	issues = append(issues, scanPlanSeedSecretLikeStrings("seed_id", normalized.SeedID)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("title", normalized.Title)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("quick_context", normalized.QuickContext)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("priority", normalized.Priority)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("source_type", normalized.SourceType)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("source_label", normalized.SourceLabel)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("source_ref_id", normalized.SourceRefID)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("constraints", normalized.Constraints...)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("non_goals", normalized.NonGoals...)...)
	issues = append(issues, scanPlanSeedSecretLikeStrings("tags", normalized.Tags...)...)

	if len(issues) > 0 {
		return normalized, issues
	}

	var jsonIssues []PlanSeedValidationIssue
	var cJSON, ngJSON, tJSON string

	cJSON, jsonIssues = marshalPlanSeedStringSlice("constraints", normalized.Constraints)
	if len(jsonIssues) > 0 {
		issues = append(issues, jsonIssues...)
	}
	normalized.ConstraintsJSON = cJSON

	ngJSON, jsonIssues = marshalPlanSeedStringSlice("non_goals", normalized.NonGoals)
	if len(jsonIssues) > 0 {
		issues = append(issues, jsonIssues...)
	}
	normalized.NonGoalsJSON = ngJSON

	tJSON, jsonIssues = marshalPlanSeedStringSlice("tags", normalized.Tags)
	if len(jsonIssues) > 0 {
		issues = append(issues, jsonIssues...)
	}
	normalized.TagsJSON = tJSON

	return normalized, issues
}
