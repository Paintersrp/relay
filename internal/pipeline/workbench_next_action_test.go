package pipeline

import (
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
	if action.Kind != WorkbenchNextActionReadyForAudit {
		t.Errorf("expected ready_for_audit, got %s (should not fall back to CLI check)", action.Kind)
	}
	if action.Severity != "done" {
		t.Errorf("expected done severity, got %s", action.Severity)
	}
	if action.PrimaryFormAction != "generate-audit-handoff" {
		t.Errorf("expected generate-audit-handoff form action, got %s", action.PrimaryFormAction)
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
		HasAuditHandoff:       true,
	}
	action := BuildWorkbenchNextAction(input)
	if action.Kind != WorkbenchNextActionReadyForAudit {
		t.Errorf("expected ready_for_audit, got %s", action.Kind)
	}
	if action.PrimaryFormAction != "" {
		t.Errorf("expected no primary form action when audit handoff exists, got %s", action.PrimaryFormAction)
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
	if action.Kind != WorkbenchNextActionReadyForAudit {
		t.Errorf("expected ready_for_audit, got %s", action.Kind)
	}
	if action.Severity != "done" {
		t.Errorf("expected done severity, got %s", action.Severity)
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
