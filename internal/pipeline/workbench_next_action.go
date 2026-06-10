package pipeline

type WorkbenchNextActionKind string

const (
	WorkbenchNextActionNone                   WorkbenchNextActionKind = "none"
	WorkbenchNextActionReviewIntake           WorkbenchNextActionKind = "review_intake"
	WorkbenchNextActionGenerateFixHandoff     WorkbenchNextActionKind = "generate_fix_handoff"
	WorkbenchNextActionGenerateAgentPrompt    WorkbenchNextActionKind = "generate_agent_prompt"
	WorkbenchNextActionGenerateAgentPacket    WorkbenchNextActionKind = "generate_agent_packet"
	WorkbenchNextActionCheckOpenCodeCLI       WorkbenchNextActionKind = "check_opencode_cli"
	WorkbenchNextActionPreviewOpenCodeCommand WorkbenchNextActionKind = "preview_opencode_command"
	WorkbenchNextActionStartOpenCode          WorkbenchNextActionKind = "start_opencode"
	WorkbenchNextActionMonitorAgentRun        WorkbenchNextActionKind = "monitor_agent_run"
	WorkbenchNextActionReviewAgentResult      WorkbenchNextActionKind = "review_agent_result"
	WorkbenchNextActionRunValidation          WorkbenchNextActionKind = "run_validation"
	WorkbenchNextActionMonitorValidation      WorkbenchNextActionKind = "monitor_validation"
	WorkbenchNextActionReviewValidationOutput WorkbenchNextActionKind = "review_validation_output"
	WorkbenchNextActionReadyForAudit          WorkbenchNextActionKind = "ready_for_audit"
	WorkbenchNextActionInspectDiff            WorkbenchNextActionKind = "inspect_diff"
	WorkbenchNextActionGenerateAuditHandoff   WorkbenchNextActionKind = "generate_audit_handoff"
	WorkbenchNextActionPrepareGitCommit       WorkbenchNextActionKind = "prepare_git_commit"
	WorkbenchNextActionReadyToCommit          WorkbenchNextActionKind = "ready_to_commit"
)

type WorkbenchNextAction struct {
	Kind              WorkbenchNextActionKind
	Title             string
	Summary           string
	Step              string
	PrimaryAction     string
	PrimaryFormAction string
	PrimaryHref       string
	Disabled          bool
	DisabledReason    string
	Severity          string
}

type WorkbenchNextActionInput struct {
	HasOriginalHandoff          bool
	HasIntakeReview             bool
	IntakeHasBlockers           bool
	IntakeHasWarnings           bool
	HasIntakeRemediationHandoff bool

	HasAgentPrompt bool
	HasAgentPacket bool

	HandoffPreflightStatus string
	OpenCodeAdapterError   string
	HasOpenCodeCLICheck    bool
	OpenCodeCLICheckStatus string
	HasOpenCodeDryRun      bool

	HasOpenCodeExecution    bool
	OpenCodeExecutionStatus string
	HasAgentResult          bool
	AgentResultStatus       string

	HasValidationCommands bool
	HasValidationRun      bool
	ValidationPassed      bool
	ValidationFailed      bool

	HasValidationProgress     bool
	ValidationProgressRunning bool
	ValidationProgressStatus  string

	HasAuditHandoff bool

	HasGitDiffEvidence            bool
	HasGitStatus                  bool
	HasGitDiffStat                bool
	HasGitDiffPatch               bool
	HasGitDiffNameStatus          bool
	HasCommitSuggestion           bool
	ValidationAcceptedWithFailure bool
}

func BuildWorkbenchNextAction(input WorkbenchNextActionInput) WorkbenchNextAction {
	if !input.HasOriginalHandoff {
		return WorkbenchNextAction{
			Kind:     WorkbenchNextActionReviewIntake,
			Title:    "Handoff intake needed",
			Summary:  "Relay does not have an original handoff artifact for this run.",
			Step:     "intake",
			Severity: "blocked",
		}
	}

	if input.IntakeHasBlockers {
		if !input.HasIntakeRemediationHandoff {
			return WorkbenchNextAction{
				Kind:              WorkbenchNextActionGenerateFixHandoff,
				Title:             "Intake has blockers",
				Summary:           "Generate a focused fix handoff before continuing.",
				Step:              "intake",
				PrimaryAction:     "generate-intake-remediation-handoff",
				PrimaryFormAction: "generate-intake-remediation-handoff",
				Severity:          "blocked",
			}
		}
		return WorkbenchNextAction{
			Kind:     WorkbenchNextActionReviewIntake,
			Title:    "Review intake blockers",
			Summary:  "A fix handoff exists. Review or download it, then update the original handoff.",
			Step:     "intake",
			Severity: "blocked",
		}
	}

	if input.IntakeHasWarnings && !input.HasIntakeRemediationHandoff {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionGenerateFixHandoff,
			Title:             "Intake has warnings",
			Summary:           "You can continue, but a fix handoff can clean up the warning before execution.",
			Step:              "intake",
			PrimaryFormAction: "generate-intake-remediation-handoff",
			Severity:          "warn",
		}
	}

	if !input.HasAgentPrompt {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionGenerateAgentPrompt,
			Title:             "Generate the Agent Prompt",
			Summary:           "Relay is ready to build the compact prompt for the repo agent.",
			Step:              "prompt",
			PrimaryFormAction: "prepare-prompt",
			Severity:          "ready",
		}
	}

	if !input.HasAgentPacket {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionGenerateAgentPacket,
			Title:             "Generate the Agent Packet",
			Summary:           "Create the OpenCode handoff packet from the prepared Agent Prompt.",
			Step:              "packet",
			PrimaryFormAction: "generate-opencode-packet",
			Severity:          "ready",
		}
	}

	if input.HasOpenCodeExecution && (input.OpenCodeExecutionStatus == "starting" || input.OpenCodeExecutionStatus == "running") {
		return WorkbenchNextAction{
			Kind:     WorkbenchNextActionMonitorAgentRun,
			Title:    "OpenCode is running",
			Summary:  "Monitor the current agent execution.",
			Step:     "run",
			Severity: "running",
		}
	}

	// If validation is running, show monitor action
	if input.HasValidationProgress && input.ValidationProgressRunning {
		return WorkbenchNextAction{
			Kind:     WorkbenchNextActionMonitorValidation,
			Title:    "Validation is running",
			Summary:  "Relay is running validation commands. Watch Step 6 for progress.",
			Step:     "validation",
			Severity: "running",
		}
	}

	// If validation has been run, skip CLI/preflight/start checks.
	if input.HasValidationRun {
		if input.ValidationFailed && !input.ValidationAcceptedWithFailure {
			return WorkbenchNextAction{
				Kind:     WorkbenchNextActionReviewValidationOutput,
				Title:    "Review validation failure",
				Summary:  "Validation failed. Review stdout/stderr before marking cleanup or creating a follow-up handoff.",
				Step:     "validation",
				Severity: "blocked",
			}
		}
		if input.ValidationPassed || input.ValidationAcceptedWithFailure {
			// Gate: require diff inspection before audit
			if !input.HasGitDiffEvidence {
				action := WorkbenchNextAction{
					Kind:              WorkbenchNextActionInspectDiff,
					Title:             "Inspect git diff",
					Summary:           "Validation passed. Collect git diff evidence before audit.",
					Step:              "audit",
					PrimaryFormAction: "inspect-diff",
					PrimaryAction:     "inspect-diff",
					Severity:          "ready",
				}
				if input.ValidationFailed && input.ValidationAcceptedWithFailure {
					action.Summary = "Validation failed but accepted. Collect git diff evidence before audit."
				}
				return action
			}
			// Gate: require audit handoff before commit prep
			if !input.HasAuditHandoff {
				action := WorkbenchNextAction{
					Kind:              WorkbenchNextActionGenerateAuditHandoff,
					Title:             "Generate audit handoff",
					Summary:           "Diff evidence collected. Generate the audit handoff for review.",
					Step:              "audit",
					PrimaryFormAction: "generate-audit-handoff",
					PrimaryAction:     "generate-audit-handoff",
					Severity:          "ready",
				}
				if input.ValidationFailed && input.ValidationAcceptedWithFailure {
					action.Summary = "Diff evidence collected. Generate audit handoff (validation failed but accepted)."
				}
				return action
			}
			// Audit handoff exists: gate commit prep
			if !input.HasCommitSuggestion {
				action := WorkbenchNextAction{
					Kind:              WorkbenchNextActionPrepareGitCommit,
					Title:             "Prepare Git Commit",
					Summary:           "Audit handoff ready. Prepare a suggested commit message.",
					Step:              "commit",
					PrimaryFormAction: "prepare-git-commit",
					PrimaryAction:     "prepare-git-commit",
					Severity:          "ready",
				}
				if input.ValidationFailed && input.ValidationAcceptedWithFailure {
					action.Summary = "Audit handoff ready. Prepare commit suggestion (validation failed but accepted)."
				}
				return action
			}
			// Commit suggestion exists: ready to commit
			summary := "Review the suggested commit message, then run git add and git commit manually."
			if input.ValidationFailed && input.ValidationAcceptedWithFailure {
				summary = "Validation failed but accepted. Review the suggested commit message, then commit manually."
			}
			return WorkbenchNextAction{
				Kind:     WorkbenchNextActionReadyToCommit,
				Title:    "Ready to commit",
				Summary:  summary,
				Step:     "commit",
				Severity: "done",
			}
		}
	}

	if !input.HasOpenCodeCLICheck {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionCheckOpenCodeCLI,
			Title:             "Check OpenCode CLI",
			Summary:           "Verify the local OpenCode binary and resolved model before starting execution.",
			Step:              "handoff",
			PrimaryFormAction: "check-opencode-cli",
			Severity:          "ready",
		}
	}

	if input.OpenCodeCLICheckStatus == "fail" || input.OpenCodeCLICheckStatus == "warn" {
		severity := "warn"
		if input.OpenCodeCLICheckStatus == "fail" {
			severity = "blocked"
		}
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionCheckOpenCodeCLI,
			Title:             "OpenCode CLI needs attention",
			Summary:           "Review the CLI check result before starting OpenCode.",
			Step:              "handoff",
			PrimaryFormAction: "check-opencode-cli",
			Severity:          severity,
		}
	}

	if !input.HasOpenCodeDryRun {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionPreviewOpenCodeCommand,
			Title:             "Preview the OpenCode command",
			Summary:           "Generate a dry-run command preview before starting the local agent.",
			Step:              "handoff",
			PrimaryFormAction: "dry-run-opencode-go",
			Severity:          "ready",
		}
	}

	if input.OpenCodeAdapterError != "" || input.HandoffPreflightStatus == "blocked" {
		reason := input.OpenCodeAdapterError
		if reason == "" {
			reason = "Handoff preflight checks are blocked."
		}
		return WorkbenchNextAction{
			Kind:           WorkbenchNextActionStartOpenCode,
			Title:          "OpenCode start is blocked",
			Summary:        "Resolve the blocked handoff or adapter checks before starting OpenCode.",
			Step:           "handoff",
			Disabled:       true,
			DisabledReason: reason,
			Severity:       "blocked",
		}
	}

	if !input.HasOpenCodeExecution && !input.HasAgentResult {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionStartOpenCode,
			Title:             "Ready to start OpenCode",
			Summary:           "The packet, CLI check, and command preview are ready. Start the local agent run.",
			Step:              "handoff",
			PrimaryFormAction: "start-opencode-go",
			Severity:          "ready",
		}
	}

	if input.HasOpenCodeExecution && !input.HasAgentResult {
		return WorkbenchNextAction{
			Kind:     WorkbenchNextActionReviewAgentResult,
			Title:    "Review agent output",
			Summary:  "OpenCode finished, but Relay did not parse a DONE/BLOCKED result. Review the logs or paste a manual result.",
			Step:     "run",
			Severity: "warn",
		}
	}

	if input.HasAgentResult && !input.HasValidationCommands {
		return WorkbenchNextAction{
			Kind:           WorkbenchNextActionRunValidation,
			Title:          "Validation commands needed",
			Summary:        "Relay has an agent result, but no validation commands are available.",
			Step:           "validation",
			Disabled:       true,
			DisabledReason: "No validation commands available.",
			Severity:       "warn",
		}
	}

	if input.HasAgentResult && input.HasValidationCommands && !input.HasValidationRun {
		return WorkbenchNextAction{
			Kind:              WorkbenchNextActionRunValidation,
			Title:             "Run Relay Validation",
			Summary:           "Run the extracted validation commands against the repo.",
			Step:              "validation",
			PrimaryFormAction: "run-validation",
			Severity:          "ready",
		}
	}

	return WorkbenchNextAction{
		Kind:     WorkbenchNextActionNone,
		Title:    "Review run state",
		Summary:  "Relay has no recommended action for the current state.",
		Step:     "intake",
		Severity: "neutral",
	}
}
