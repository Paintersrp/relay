package pipeline

import (
	"strings"
	"testing"
)

func TestNextAction_NoOriginalHandoff(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff: false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReviewIntake {
		t.Errorf("expected review_intake, got %s", action.Kind)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
	if action.Step != "intake" {
		t.Errorf("expected step intake, got %s", action.Step)
	}
}

func TestNextAction_IntakeBlockersNoRemediation(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:          true,
		IntakeHasBlockers:           true,
		IntakeHasWarnings:           false,
		HasIntakeRemediationHandoff: false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionGenerateFixHandoff {
		t.Errorf("expected generate_fix_handoff, got %s", action.Kind)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
	if action.PrimaryFormAction != "generate-intake-remediation-handoff" {
		t.Errorf("expected generate-intake-remediation-handoff form action, got %s", action.PrimaryFormAction)
	}
}

func TestNextAction_IntakeBlockersWithRemediation(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:          true,
		IntakeHasBlockers:           true,
		HasIntakeRemediationHandoff: true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReviewIntake {
		t.Errorf("expected review_intake, got %s", action.Kind)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
}

func TestNextAction_IntakeWarningsNoRemediation(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:          true,
		IntakeHasBlockers:           false,
		IntakeHasWarnings:           true,
		HasIntakeRemediationHandoff: false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionGenerateFixHandoff {
		t.Errorf("expected generate_fix_handoff, got %s", action.Kind)
	}
	if action.Severity != "warn" {
		t.Errorf("expected warn severity, got %s", action.Severity)
	}
}

func TestNextAction_IntakeWarningsWithRemediationNoPrompt(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:          true,
		IntakeHasBlockers:           false,
		IntakeHasWarnings:           true,
		HasIntakeRemediationHandoff: true,
		HasAgentPrompt:              false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionGenerateAgentPrompt {
		t.Errorf("expected generate_agent_prompt, got %s", action.Kind)
	}
}

func TestNextAction_NoAgentPrompt(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:          true,
		HasAgentPrompt:              false,
		HasIntakeRemediationHandoff: true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionGenerateAgentPrompt {
		t.Errorf("expected generate_agent_prompt, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "prepare-prompt" {
		t.Errorf("expected prepare-prompt form action, got %s", action.PrimaryFormAction)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_PromptButNoPacket(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff: true,
		HasAgentPrompt:     true,
		HasAgentPacket:     false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionGenerateAgentPacket {
		t.Errorf("expected generate_agent_packet, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "generate-opencode-packet" {
		t.Errorf("expected generate-opencode-packet form action, got %s", action.PrimaryFormAction)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_ExecutionRunning(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:      true,
		HasAgentPrompt:          true,
		HasAgentPacket:          true,
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "running",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionMonitorAgentRun {
		t.Errorf("expected monitor_agent_run, got %s", action.Kind)
	}
	if action.Severity != "running" {
		t.Errorf("expected running severity, got %s", action.Severity)
	}
}

func TestNextAction_ExecutionStarting(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:      true,
		HasAgentPrompt:          true,
		HasAgentPacket:          true,
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "starting",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionMonitorAgentRun {
		t.Errorf("expected monitor_agent_run, got %s", action.Kind)
	}
}

func TestNextAction_StaleExecutionNeedsRecovery(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:      true,
		HasAgentPrompt:          true,
		HasAgentPacket:          true,
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "running",
		HasOpenCodeStaleRunning: true,
		OpenCodeLifecycleState:  "stale_output",
		OpenCodeCanRecover:      true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionMonitorAgentRun {
		t.Errorf("expected monitor_agent_run, got %s", action.Kind)
	}
	if action.Severity != "warn" {
		t.Errorf("expected warn severity, got %s", action.Severity)
	}
	if action.Step != "run" {
		t.Errorf("expected run step, got %s", action.Step)
	}
	if !strings.Contains(action.Title, "stalled") {
		t.Errorf("expected stalled title, got %q", action.Title)
	}
	if !strings.Contains(action.Summary, "recover") {
		t.Errorf("expected recovery summary, got %q", action.Summary)
	}
}

func TestNextAction_NoCLICheck(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:  true,
		HasAgentPrompt:      true,
		HasAgentPacket:      true,
		HasOpenCodeCLICheck: false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionCheckOpenCodeCLI {
		t.Errorf("expected check_opencode_cli, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "check-opencode-cli" {
		t.Errorf("expected check-opencode-cli form action, got %s", action.PrimaryFormAction)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_CLICheckFail(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "fail",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionCheckOpenCodeCLI {
		t.Errorf("expected check_opencode_cli, got %s", action.Kind)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
}

func TestNextAction_CLICheckWarn(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "warn",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionCheckOpenCodeCLI {
		t.Errorf("expected check_opencode_cli, got %s", action.Kind)
	}
	if action.Severity != "warn" {
		t.Errorf("expected warn severity, got %s", action.Severity)
	}
}

func TestNextAction_CLICheckPassNoDryRun(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionPreviewOpenCodeCommand {
		t.Errorf("expected preview_opencode_command, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "dry-run-opencode-go" {
		t.Errorf("expected dry-run-opencode-go form action, got %s", action.PrimaryFormAction)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_AdapterErrorBlocked(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		OpenCodeAdapterError:   "model resolution failed",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionStartOpenCode {
		t.Errorf("expected start_opencode, got %s", action.Kind)
	}
	if !action.Disabled {
		t.Error("expected action to be disabled")
	}
	if action.DisabledReason != "model resolution failed" {
		t.Errorf("expected disabled reason 'model resolution failed', got %s", action.DisabledReason)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
}

func TestNextAction_PreflightBlocked(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HandoffPreflightStatus: "blocked",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionStartOpenCode {
		t.Errorf("expected start_opencode, got %s", action.Kind)
	}
	if !action.Disabled {
		t.Error("expected action to be disabled")
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
}

func TestNextAction_ReadyToStartOpenCode(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   false,
		HasAgentResult:         false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionStartOpenCode {
		t.Errorf("expected start_opencode, got %s", action.Kind)
	}
	if action.Disabled {
		t.Error("expected action to be enabled")
	}
	if action.PrimaryFormAction != "start-opencode-go" {
		t.Errorf("expected start-opencode-go form action, got %s", action.PrimaryFormAction)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_ExecutionCompletedNoResult(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:      true,
		HasAgentPrompt:          true,
		HasAgentPacket:          true,
		HasOpenCodeCLICheck:     true,
		OpenCodeCLICheckStatus:  "pass",
		HasOpenCodeDryRun:       true,
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "completed",
		HasAgentResult:          false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReviewAgentResult {
		t.Errorf("expected review_agent_result, got %s", action.Kind)
	}
	if action.Severity != "warn" {
		t.Errorf("expected warn severity, got %s", action.Severity)
	}
}

func TestNextAction_AgentResultNoValidationCommands(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionRunValidation {
		t.Errorf("expected run_validation, got %s", action.Kind)
	}
	if !action.Disabled {
		t.Error("expected action to be disabled")
	}
	if action.DisabledReason != "No validation commands available." {
		t.Errorf("expected 'No validation commands available.', got %s", action.DisabledReason)
	}
	if action.Severity != "warn" {
		t.Errorf("expected warn severity, got %s", action.Severity)
	}
}

func TestNextAction_AgentResultAndValidationCommandsNoRun(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionRunValidation {
		t.Errorf("expected run_validation, got %s", action.Kind)
	}
	if action.Disabled {
		t.Error("expected action to be enabled")
	}
	if action.PrimaryFormAction != "run-validation" {
		t.Errorf("expected run-validation form action, got %s", action.PrimaryFormAction)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_ValidationPassedNoCLICheck(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:    true,
		HasAgentPrompt:        true,
		HasAgentPacket:        true,
		HasOpenCodeCLICheck:   false,
		HasOpenCodeDryRun:     false,
		HasOpenCodeExecution:  true,
		HasAgentResult:        true,
		HasValidationCommands: true,
		HasValidationRun:      true,
		ValidationPassed:      true,
		ValidationFailed:      false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionInspectDiff {
		t.Errorf("expected inspect_diff, got %s (should not skip to ready_for_audit)", action.Kind)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
	if action.PrimaryFormAction != "inspect-diff" {
		t.Errorf("expected inspect-diff form action, got %s", action.PrimaryFormAction)
	}
}

func TestNextAction_ValidationFailedNoCLICheck(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:    true,
		HasAgentPrompt:        true,
		HasAgentPacket:        true,
		HasOpenCodeCLICheck:   false,
		HasOpenCodeDryRun:     false,
		HasOpenCodeExecution:  true,
		HasAgentResult:        true,
		HasValidationCommands: true,
		HasValidationRun:      true,
		ValidationPassed:      false,
		ValidationFailed:      true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReviewValidationOutput {
		t.Errorf("expected review_validation_output, got %s (should not fall back to CLI check)", action.Kind)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
}

func TestNextAction_ValidationPassedWithAuditHandoff(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:    true,
		HasAgentPrompt:        true,
		HasAgentPacket:        true,
		HasOpenCodeCLICheck:   false,
		HasOpenCodeDryRun:     false,
		HasOpenCodeExecution:  true,
		HasAgentResult:        true,
		HasValidationCommands: true,
		HasValidationRun:      true,
		ValidationPassed:      true,
		ValidationFailed:      false,
		HasGitDiffEvidence:    true,
		HasAuditHandoff:       true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionPrepareGitCommit {
		t.Errorf("expected prepare_git_commit, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "prepare-git-commit" {
		t.Errorf("expected prepare-git-commit form action, got %s", action.PrimaryFormAction)
	}
}

func TestNextAction_ValidationFailed(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
		ValidationPassed:       false,
		ValidationFailed:       true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReviewValidationOutput {
		t.Errorf("expected review_validation_output, got %s", action.Kind)
	}
	if action.Severity != "blocked" {
		t.Errorf("expected blocked severity, got %s", action.Severity)
	}
}

func TestNextAction_ValidationPassed(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
		ValidationPassed:       true,
		ValidationFailed:       false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionInspectDiff {
		t.Errorf("expected inspect_diff, got %s", action.Kind)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_ValidationRunningWinsOverCLICheck(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:        true,
		HasAgentPrompt:            true,
		HasAgentPacket:            true,
		HasOpenCodeCLICheck:       false,
		HasOpenCodeDryRun:         false,
		HasAgentResult:            true,
		HasValidationCommands:     true,
		HasValidationRun:          false,
		HasValidationProgress:     true,
		ValidationProgressRunning: true,
		ValidationProgressStatus:  "running",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionMonitorValidation {
		t.Errorf("expected monitor_validation, got %s", action.Kind)
	}
	if action.Severity != "running" {
		t.Errorf("expected running severity, got %s", action.Severity)
	}
	if action.Step != "validation" {
		t.Errorf("expected step validation, got %s", action.Step)
	}
}

func TestNextAction_ValidationRunningWinsOverReadyForAudit(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:        true,
		HasAgentPrompt:            true,
		HasAgentPacket:            true,
		HasOpenCodeCLICheck:       false,
		HasOpenCodeDryRun:         false,
		HasAgentResult:            true,
		HasValidationCommands:     true,
		HasValidationRun:          true,
		ValidationPassed:          false,
		ValidationFailed:          false,
		HasValidationProgress:     true,
		ValidationProgressRunning: true,
		ValidationProgressStatus:  "running",
		HasAuditHandoff:           false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionMonitorValidation {
		t.Errorf("expected monitor_validation, got %s (should not be ready_for_audit)", action.Kind)
	}
}

func TestNextAction_ValidationRunningStopsAtMonitor(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:        true,
		HasAgentPrompt:            true,
		HasAgentPacket:            true,
		HasOpenCodeCLICheck:       false,
		HasOpenCodeDryRun:         false,
		HasAgentResult:            true,
		HasValidationCommands:     true,
		HasValidationProgress:     true,
		ValidationProgressRunning: true,
		ValidationProgressStatus:  "running",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionMonitorValidation {
		t.Errorf("expected monitor_validation, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "" {
		t.Errorf("expected no primary form action for running validation, got %s", action.PrimaryFormAction)
	}
}

func TestNextAction_ValidationPassedStillReadyForAudit(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:        true,
		HasAgentPrompt:            true,
		HasAgentPacket:            true,
		HasOpenCodeCLICheck:       false,
		HasOpenCodeDryRun:         false,
		HasAgentResult:            true,
		HasValidationCommands:     true,
		HasValidationRun:          true,
		ValidationPassed:          true,
		ValidationFailed:          false,
		HasValidationProgress:     true,
		ValidationProgressRunning: false,
		ValidationProgressStatus:  "pass",
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionInspectDiff {
		t.Errorf("expected inspect_diff after validation passed, got %s", action.Kind)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}

func TestNextAction_Fallback(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionNone {
		t.Errorf("expected fallback none, got %s", action.Kind)
	}
	if action.Severity != "neutral" {
		t.Errorf("expected neutral severity, got %s", action.Severity)
	}
}

func TestNextAction_ValidationPassedNeedsDiffInspection(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
		ValidationPassed:       true,
		ValidationFailed:       false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionInspectDiff {
		t.Errorf("expected inspect_diff, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "inspect-diff" {
		t.Errorf("expected inspect-diff form action, got %s", action.PrimaryFormAction)
	}
	if action.Step != "audit" {
		t.Errorf("expected step audit, got %s", action.Step)
	}
}

func TestNextAction_DiffInspectedNeedsAuditHandoff(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
		ValidationPassed:       true,
		ValidationFailed:       false,
		HasGitDiffEvidence:     true,
		HasAuditHandoff:        false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionGenerateAuditHandoff {
		t.Errorf("expected generate_audit_handoff, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "generate-audit-handoff" {
		t.Errorf("expected generate-audit-handoff form action, got %s", action.PrimaryFormAction)
	}
	if action.Step != "audit" {
		t.Errorf("expected step audit, got %s", action.Step)
	}
}

func TestNextAction_AuditHandoffNeedsCommitPreparation(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
		ValidationPassed:       true,
		ValidationFailed:       false,
		HasGitDiffEvidence:     true,
		HasAuditHandoff:        true,
		HasCommitSuggestion:    false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionPrepareGitCommit {
		t.Errorf("expected prepare_git_commit, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "prepare-git-commit" {
		t.Errorf("expected prepare-git-commit form action, got %s", action.PrimaryFormAction)
	}
	if action.Step != "commit" {
		t.Errorf("expected step commit, got %s", action.Step)
	}
}

func TestNextAction_CommitSuggestionReady(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:     true,
		HasAgentPrompt:         true,
		HasAgentPacket:         true,
		HasOpenCodeCLICheck:    true,
		OpenCodeCLICheckStatus: "pass",
		HasOpenCodeDryRun:      true,
		HasOpenCodeExecution:   true,
		HasAgentResult:         true,
		HasValidationCommands:  true,
		HasValidationRun:       true,
		ValidationPassed:       true,
		ValidationFailed:       false,
		HasGitDiffEvidence:     true,
		HasAuditHandoff:        true,
		HasCommitSuggestion:    true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReadyToCommit {
		t.Errorf("expected ready_to_commit, got %s", action.Kind)
	}
	if action.Severity != "done" {
		t.Errorf("expected done severity, got %s", action.Severity)
	}
	if action.Step != "commit" {
		t.Errorf("expected step commit, got %s", action.Step)
	}
}

func TestNextAction_ValidationAcceptedFailureBlockedByDiff(t *testing.T) {
	input := WorkbenchNextActionInput{
		HasOriginalHandoff:            true,
		HasAgentPrompt:                true,
		HasAgentPacket:                true,
		HasOpenCodeCLICheck:           true,
		OpenCodeCLICheckStatus:        "pass",
		HasOpenCodeDryRun:             true,
		HasOpenCodeExecution:          true,
		HasAgentResult:                true,
		HasValidationCommands:         true,
		HasValidationRun:              true,
		ValidationPassed:              false,
		ValidationFailed:              true,
		ValidationAcceptedWithFailure: true,
		HasGitDiffEvidence:            false,
		HasAuditHandoff:               false,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionInspectDiff {
		t.Errorf("expected inspect_diff, got %s", action.Kind)
	}
	if action.Severity != "ready" {
		t.Errorf("expected ready severity, got %s", action.Severity)
	}
}
