package refactors

import (
	"fmt"
	"strings"
)

// secretLikeMarkers is a minimal guard for obviously secret-bearing text. It is
// intentionally small and does not replace the broader redaction policy; it only
// blocks the most obvious credential material from being persisted into prompts,
// titles, descriptions, reasons, metadata, or arrays.
var secretLikeMarkers = []string{
	"BEGIN PRIVATE KEY",
	"Authorization:",
	"Bearer ",
	"xoxb-",
	"ghp_",
	"sk-",
}

// containsSecretLikeValue reports whether the value contains an obvious
// secret-bearing marker.
func containsSecretLikeValue(value string) bool {
	for _, marker := range secretLikeMarkers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

// normalizeLimit clamps a requested limit into [1, MaxListLimit], defaulting to
// DefaultListLimit when non-positive.
func normalizeLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultListLimit
	}
	if limit > MaxListLimit {
		return MaxListLimit
	}
	return limit
}

// validateNonEmptyString returns a single issue (with the given code) when the
// trimmed value is empty.
func validateNonEmptyString(field, value, code string) []ValidationIssue {
	if strings.TrimSpace(value) == "" {
		return []ValidationIssue{{
			Field:   field,
			Code:    code,
			Message: fmt.Sprintf("%s is required and must not be blank", field),
		}}
	}
	return nil
}

// validateNonEmptyStringSlice returns a single issue (with the given code) when
// the slice is empty or contains only blank entries.
func validateNonEmptyStringSlice(field string, values []string, code string) []ValidationIssue {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return nil
		}
	}
	return []ValidationIssue{{
		Field:   field,
		Code:    code,
		Message: fmt.Sprintf("%s must contain at least one non-empty value", field),
	}}
}

// validateRiskLevel validates the candidate risk level.
func validateRiskLevel(risk string) []ValidationIssue {
	switch strings.TrimSpace(risk) {
	case RiskLow, RiskMedium, RiskHigh:
		return nil
	default:
		return []ValidationIssue{{
			Field:   "risk_level",
			Code:    CodeInvalidRiskLevel,
			Message: "risk_level must be one of: low, medium, high",
		}}
	}
}

// validateTargetScope validates the discovery target scope kind and values.
func validateTargetScope(scope TargetScope) []ValidationIssue {
	var issues []ValidationIssue
	kind := strings.TrimSpace(scope.Kind)
	if kind == "" || !allowedTargetScopeKinds[kind] {
		issues = append(issues, ValidationIssue{
			Field:   "target_scope.kind",
			Code:    CodeInvalidTargetScope,
			Message: "target_scope.kind must be one of: repository, subsystem, directory, file_set, plan, pass",
		})
	}
	if v := validateNonEmptyStringSlice("target_scope.values", scope.Values, CodeInvalidTargetScope); len(v) > 0 {
		issues = append(issues, v...)
	}
	return issues
}

// scanSecretLikeStrings appends a secret_like_value issue for the given field if
// any of the provided values contains a secret marker.
func scanSecretLikeStrings(field string, values ...string) []ValidationIssue {
	for _, v := range values {
		if containsSecretLikeValue(v) {
			return []ValidationIssue{{
				Field:   field,
				Code:    CodeSecretLikeValue,
				Message: fmt.Sprintf("%s contains a value that looks like a secret/credential and was rejected", field),
			}}
		}
	}
	return nil
}

// validateDiscoveryTaskInput validates a discovery task create/update payload.
// requireID controls whether discovery_task_id must be present (create path).
func validateDiscoveryTaskInput(input DiscoveryTaskInput, requireID bool) []ValidationIssue {
	var issues []ValidationIssue

	if requireID {
		issues = append(issues, validateNonEmptyString("discovery_task_id", input.DiscoveryTaskID, CodeRequired)...)
	}
	issues = append(issues, validateNonEmptyString("title", input.Title, CodeRequired)...)
	issues = append(issues, validateNonEmptyString("analysis_prompt", input.AnalysisPrompt, CodeRequired)...)
	issues = append(issues, validateTargetScope(input.TargetScope)...)

	// Secret-like guard across user-provided text.
	issues = append(issues, scanSecretLikeStrings("title", input.Title)...)
	issues = append(issues, scanSecretLikeStrings("analysis_prompt", input.AnalysisPrompt)...)
	issues = append(issues, scanSecretLikeStrings("target_scope.values", input.TargetScope.Values...)...)
	issues = append(issues, scanSecretLikeStrings("tags", input.Tags...)...)
	issues = append(issues, scanSecretLikeStrings("metadata", metadataValues(input.Metadata)...)...)

	return issues
}

// validateCandidateInput validates a candidate create/update payload, returning
// all pass-ready problems at once. requireID controls whether candidate_id must
// be present (create path).
func validateCandidateInput(input CandidateInput, requireID bool) []ValidationIssue {
	var issues []ValidationIssue

	if requireID {
		issues = append(issues, validateNonEmptyString("candidate_id", input.CandidateID, CodeRequired)...)
	}

	// Required pass-ready string fields.
	issues = append(issues, validateNonEmptyString("title", input.Title, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyString("problem_summary", input.ProblemSummary, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyString("desired_behavior", input.DesiredBehavior, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyString("rationale", input.Rationale, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyString("proposed_pass_name", input.ProposedPassName, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyString("proposed_pass_goal", input.ProposedPassGoal, CodeNotPassReady)...)

	// Required pass-ready array fields (at least one non-empty value).
	issues = append(issues, validateNonEmptyStringSlice("proposed_pass_scope", input.ProposedPassScope, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyStringSlice("non_goals", input.NonGoals, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyStringSlice("target_files", input.TargetFiles, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyStringSlice("validation_commands", input.ValidationCommands, CodeNotPassReady)...)
	issues = append(issues, validateNonEmptyStringSlice("audit_focus", input.AuditFocus, CodeNotPassReady)...)

	// Risk level.
	issues = append(issues, validateRiskLevel(input.RiskLevel)...)

	// Secret-like guard across user-provided text/arrays/metadata.
	issues = append(issues, scanSecretLikeStrings("title", input.Title)...)
	issues = append(issues, scanSecretLikeStrings("problem_summary", input.ProblemSummary)...)
	issues = append(issues, scanSecretLikeStrings("current_behavior", input.CurrentBehavior)...)
	issues = append(issues, scanSecretLikeStrings("desired_behavior", input.DesiredBehavior)...)
	issues = append(issues, scanSecretLikeStrings("rationale", input.Rationale)...)
	issues = append(issues, scanSecretLikeStrings("proposed_pass_name", input.ProposedPassName)...)
	issues = append(issues, scanSecretLikeStrings("proposed_pass_goal", input.ProposedPassGoal)...)
	issues = append(issues, scanSecretLikeStrings("dependency_notes", input.DependencyNotes)...)
	issues = append(issues, scanSecretLikeStrings("proposed_pass_scope", input.ProposedPassScope...)...)
	issues = append(issues, scanSecretLikeStrings("non_goals", input.NonGoals...)...)
	issues = append(issues, scanSecretLikeStrings("target_files", input.TargetFiles...)...)
	issues = append(issues, scanSecretLikeStrings("validation_commands", input.ValidationCommands...)...)
	issues = append(issues, scanSecretLikeStrings("audit_focus", input.AuditFocus...)...)
	issues = append(issues, scanSecretLikeStrings("constraints", input.Constraints...)...)
	issues = append(issues, scanSecretLikeStrings("metadata", metadataValues(input.Metadata)...)...)

	return issues
}

// validateCandidateLifecycleInput validates a candidate lifecycle request's
// required reason/target fields for the given action (defer/reject/supersede).
func validateCandidateLifecycleInput(action string, input CandidateLifecycleInput) []ValidationIssue {
	var issues []ValidationIssue
	switch action {
	case "defer":
		issues = append(issues, validateNonEmptyString("defer_reason", input.DeferReason, CodeRequired)...)
		issues = append(issues, scanSecretLikeStrings("defer_reason", input.DeferReason)...)
	case "reject":
		issues = append(issues, validateNonEmptyString("reject_reason", input.RejectReason, CodeRequired)...)
		issues = append(issues, scanSecretLikeStrings("reject_reason", input.RejectReason)...)
	case "supersede":
		issues = append(issues, validateNonEmptyString("superseded_by_candidate_id", input.SupersededByCandidateID, CodeRequired)...)
		issues = append(issues, scanSecretLikeStrings("supersede_reason", input.SupersedeReason)...)
	}
	return issues
}

// allowedScheduleKinds is the set of schedule kinds the mark-scheduled boundary
// accepts. It mirrors the store-layer schedule_kind contract.
var allowedScheduleKinds = map[string]bool{
	"existing_plan_bonus_pass":     true,
	"generated_refactor_only_plan": true,
}

// validateCandidateScheduleInput validates a mark-scheduled request. schedule_kind,
// plan_id, and pass_id are required; run_id, note, and schedule_ref_id are
// optional. Secret-like values are rejected where practical.
func validateCandidateScheduleInput(input CandidateScheduleInput) []ValidationIssue {
	var issues []ValidationIssue

	if kind := strings.TrimSpace(input.ScheduleKind); kind == "" {
		issues = append(issues, ValidationIssue{
			Field:   "schedule_kind",
			Code:    CodeRequired,
			Message: "schedule_kind is required and must not be blank",
		})
	} else if !allowedScheduleKinds[kind] {
		issues = append(issues, ValidationIssue{
			Field:   "schedule_kind",
			Code:    CodeInvalidScheduleKind,
			Message: "schedule_kind must be one of: existing_plan_bonus_pass, generated_refactor_only_plan",
		})
	}

	issues = append(issues, validateNonEmptyString("plan_id", input.PlanID, CodeRequired)...)
	issues = append(issues, validateNonEmptyString("pass_id", input.PassID, CodeRequired)...)

	issues = append(issues, scanSecretLikeStrings("plan_id", input.PlanID)...)
	issues = append(issues, scanSecretLikeStrings("pass_id", input.PassID)...)
	issues = append(issues, scanSecretLikeStrings("run_id", input.RunID)...)
	issues = append(issues, scanSecretLikeStrings("schedule_ref_id", input.ScheduleRefID)...)
	issues = append(issues, scanSecretLikeStrings("note", input.Note)...)

	return issues
}

// allowedCompletionHookStatuses is the set of target statuses the service-only
// completion hook accepts. rejected is intentionally excluded: rejection requires
// an explicit user decision in a later pass.
var allowedCompletionHookStatuses = map[string]bool{
	CandidateStatusCompleted:                 true,
	CandidateStatusCompletedWithWarnings:     true,
	CandidateStatusScheduledRevisionRequired: true,
	CandidateStatusDeferred:                  true,
}

// validateCandidateCompletionHookInput validates a completion hook request. The
// target status is required and must be one of the allowed completion outcomes.
func validateCandidateCompletionHookInput(input CandidateCompletionHookInput) []ValidationIssue {
	var issues []ValidationIssue

	if status := strings.TrimSpace(input.Status); status == "" {
		issues = append(issues, ValidationIssue{
			Field:   "status",
			Code:    CodeRequired,
			Message: "status is required and must not be blank",
		})
	} else if !allowedCompletionHookStatuses[status] {
		issues = append(issues, ValidationIssue{
			Field:   "status",
			Code:    CodeInvalidStatus,
			Message: "status must be one of: completed, completed_with_warnings, scheduled_revision_required, deferred",
		})
	}

	issues = append(issues, scanSecretLikeStrings("reason", input.Reason)...)

	return issues
}

// metadataValues flattens metadata keys and values for secret scanning.
func metadataValues(metadata map[string]string) []string {
	if len(metadata) == 0 {
		return nil
	}
	values := make([]string, 0, len(metadata)*2)
	for k, v := range metadata {
		values = append(values, k, v)
	}
	return values
}
