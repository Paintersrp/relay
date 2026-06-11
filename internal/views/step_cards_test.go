package views

import (
	"context"
	"strings"
	"testing"

	"relay/internal/pipeline"
	"relay/internal/store"
)

func TestRelayValidationUIStateNoResult(t *testing.T) {
	got := relayValidationUIStateFor(false, false, false)
	if got != relayValidationNoResult {
		t.Fatalf("expected no_result, got %q", got)
	}
}

func TestRelayValidationUIStateNoCommands(t *testing.T) {
	got := relayValidationUIStateFor(true, false, false)
	if got != relayValidationNoCommands {
		t.Fatalf("expected no_commands, got %q", got)
	}
}

func TestRelayValidationUIStateReady(t *testing.T) {
	got := relayValidationUIStateFor(true, true, false)
	if got != relayValidationReady {
		t.Fatalf("expected ready, got %q", got)
	}
}

func TestRelayValidationUIStateCompleted(t *testing.T) {
	got := relayValidationUIStateFor(true, true, true)
	if got != relayValidationCompleted {
		t.Fatalf("expected completed, got %q", got)
	}
}

func TestRelayValidationUIStateCompletedRegardlessOfCommands(t *testing.T) {
	got := relayValidationUIStateFor(true, false, true)
	if got != relayValidationCompleted {
		t.Fatalf("expected completed regardless of commands, got %q", got)
	}
}

func TestRelayValidationUIStateNoResultPrioritizedOverCommands(t *testing.T) {
	got := relayValidationUIStateFor(false, false, false)
	if got != relayValidationNoResult {
		t.Fatalf("expected no_result when agent result missing, got %q", got)
	}
}

func TestRelayValidationUIStateCompletedRegardlessOfAgentResult(t *testing.T) {
	got := relayValidationUIStateFor(false, false, true)
	if got != relayValidationCompleted {
		t.Fatalf("expected completed regardless of agent result, got %q", got)
	}
}

func TestRunDetailRendersWorkbenchShell(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="run-workbench-shell"`) {
		t.Errorf("expected run-workbench-shell id")
	}
	if !strings.Contains(html, `data-relay-workbench`) {
		t.Errorf("expected data-relay-workbench attribute")
	}
}

func TestRunDetailStepLinksUseShellHTMXSwap(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on step links")
	}
	if !strings.Contains(html, `hx-select="#run-workbench-shell"`) {
		t.Errorf("expected hx-select on step links")
	}
	if !strings.Contains(html, `hx-push-url="true"`) {
		t.Errorf("expected hx-push-url on step links")
	}
}

func TestNextActionFormTargetsWorkbenchShell(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		NextAction: WorkbenchNextActionView{
			Kind:              "generate_agent_prompt",
			Title:             "Generate the Agent Prompt",
			Summary:           "Ready",
			Step:              "prompt",
			PrimaryFormAction: "prepare-prompt",
			Severity:          "ready",
		},
	}
	err := RunInspectorSummary(run, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render NextActionCard: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on next action form")
	}
	if !strings.Contains(html, `hx-select="#run-workbench-shell"`) {
		t.Errorf("expected hx-select on next action form")
	}
	if !strings.Contains(html, `hx-indicator="#run-workbench-loading"`) {
		t.Errorf("expected hx-indicator on next action form")
	}
}

func TestStepFlowFooterRendersPreviousAndNextLinks(t *testing.T) {
	var buf strings.Builder
	err := StepFlowFooter(1, "validation").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render StepFlowFooter: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `?step=run`) {
		t.Errorf("expected Back link to run step")
	}
	if !strings.Contains(html, `?step=audit`) {
		t.Errorf("expected Next link to audit step")
	}
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected footer links to target workbench shell")
	}
}

func TestArtifactDownloadsRemainNormalLinks(t *testing.T) {
	var buf strings.Builder
	artifacts := []store.Artifact{
		{Kind: "agent_prompt", CreatedAt: "2024-01-01"},
	}
	err := ArtifactList(artifacts, 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactList: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `/artifacts/agent_prompt/download`) {
		t.Errorf("expected artifact download href")
	}
	if strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("artifact download links should not have shell hx-target")
	}
}

func TestWorkflowStepStateIntake(t *testing.T) {
	previews := RunPreviews{}
	artifacts := []store.Artifact{}
	checks := []store.Check{}
	if pipelineStageState("intake", "prompt", previews, artifacts, checks, nil) != "pending" {
		t.Errorf("expected pending for fresh intake")
	}
	artifacts = []store.Artifact{{Kind: "original_handoff"}}
	if pipelineStageState("intake", "intake", previews, artifacts, checks, nil) != "active" {
		t.Errorf("expected active for current intake")
	}
	checks = []store.Check{{Kind: "validation", Status: "pass"}}
	if pipelineStageState("intake", "prompt", previews, artifacts, checks, nil) != "done" {
		t.Errorf("expected done for validated intake")
	}
}

func TestWorkflowStepStatePromptBlocked(t *testing.T) {
	previews := RunPreviews{}
	artifacts := []store.Artifact{}
	checks := []store.Check{}
	review := &pipeline.IntakeReview{Blockers: []string{"missing goal"}}
	if pipelineStageState("prompt", "prompt", previews, artifacts, checks, review) != "blocked" {
		t.Errorf("expected blocked when intake has blockers")
	}
}

func TestRunInspectorSummaryTargetsWorkbenchShell(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		NextAction: WorkbenchNextActionView{
			Kind:              "generate_agent_prompt",
			Title:             "Generate the Agent Prompt",
			Summary:           "Ready",
			Step:              "prompt",
			PrimaryFormAction: "prepare-prompt",
			Severity:          "ready",
		},
	}
	err := RunInspectorSummary(run, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunInspectorSummary: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on run inspector summary action form")
	}
	if !strings.Contains(html, `hx-select="#run-workbench-shell"`) {
		t.Errorf("expected hx-select on run inspector summary action form")
	}
	if !strings.Contains(html, `hx-indicator="#run-workbench-loading"`) {
		t.Errorf("expected hx-indicator on run inspector summary action form")
	}
}

func TestRunDetailRendersInspectorShell(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="run-workbench-shell"`) {
		t.Errorf("expected run-workbench-shell id")
	}
	if !strings.Contains(html, `data-relay-workbench`) {
		t.Errorf("expected data-relay-workbench attribute")
	}
	if !strings.Contains(html, `relay-inspector-grid`) {
		t.Errorf("expected relay-inspector-grid class")
	}
	if !strings.Contains(html, `relay-stage-panel-card`) {
		t.Errorf("expected relay-stage-panel-card class")
	}
}

func TestRunDetailContainsPipelineNavigation(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `aria-label="Pipeline stages"`) {
		t.Errorf("expected pipeline stages aria-label")
	}
	if !strings.Contains(html, `relay-stage-rail`) {
		t.Errorf("expected relay-stage-rail class")
	}
	if !strings.Contains(html, `relay-stage-strip`) {
		t.Errorf("expected relay-stage-strip class")
	}
}

func TestSelectedStagePanelRendersExactlyOneStage(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := SelectedStagePanel(run, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render SelectedStagePanel: %v", err)
	}
	html := buf.String()
	count := strings.Count(html, `relay-card-header border-b`)
	if count != 1 {
		t.Errorf("expected exactly one selected stage header, got %d", count)
	}
	if !strings.Contains(html, `data-run-stage-heading`) {
		t.Errorf("expected data-run-stage-heading attribute")
	}
}

func TestStageNavigationLinksKeepHTMXAttributes(t *testing.T) {
	var buf strings.Builder
	err := PipelineStageRail(1, RunPreviews{}, nil, nil, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PipelineStageRail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-get="/runs/1?step=`) {
		t.Errorf("expected hx-get on stage links")
	}
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on stage links")
	}
	if !strings.Contains(html, `hx-select="#run-workbench-shell"`) {
		t.Errorf("expected hx-select on stage links")
	}
	if !strings.Contains(html, `hx-push-url="true"`) {
		t.Errorf("expected hx-push-url on stage links")
	}
	if !strings.Contains(html, `hx-indicator="#run-workbench-loading"`) {
		t.Errorf("expected hx-indicator on stage links")
	}
}

func TestPipelineStageStripKeepsHTMXAttributes(t *testing.T) {
	var buf strings.Builder
	err := PipelineStageStrip(1, RunPreviews{}, nil, nil, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PipelineStageStrip: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on strip links")
	}
}

func TestValidationStageRendersValidationControls(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{{Kind: "agent_result_raw"}}
	previews := RunPreviews{HasValidationCommands: true}
	err := RelayValidationStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Run Validation Commands") {
		t.Errorf("expected validation button")
	}
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on validation form")
	}
	if !strings.Contains(html, `hx-select="#run-workbench-shell"`) {
		t.Errorf("expected hx-select on validation form")
	}
}

func TestPipelineStageSymbol(t *testing.T) {
	if pipelineStageSymbol("done") != "✓" {
		t.Errorf("expected ✓ for done")
	}
	if pipelineStageSymbol("active") != "▶" {
		t.Errorf("expected ▶ for active")
	}
	if pipelineStageSymbol("running") != "▶" {
		t.Errorf("expected ▶ for running")
	}
	if pipelineStageSymbol("ready") != "●" {
		t.Errorf("expected ● for ready")
	}
	if pipelineStageSymbol("blocked") != "✗" {
		t.Errorf("expected ✗ for blocked")
	}
	if pipelineStageSymbol("pending") != "○" {
		t.Errorf("expected ○ for pending")
	}
}

func TestPipelineStageLinkClassActive(t *testing.T) {
	cls := pipelineStageLinkClass("intake", "intake", "active")
	if !strings.Contains(cls, "relay-stage-link-active") {
		t.Errorf("expected active class")
	}
}

func TestPipelineStageLinkClassDone(t *testing.T) {
	cls := pipelineStageLinkClass("intake", "prompt", "done")
	if !strings.Contains(cls, "relay-stage-link-done") {
		t.Errorf("expected done class")
	}
}

func TestPipelineStageLinkClassBlocked(t *testing.T) {
	cls := pipelineStageLinkClass("intake", "prompt", "blocked")
	if !strings.Contains(cls, "relay-stage-link-blocked") {
		t.Errorf("expected blocked class")
	}
}

func TestSelectedStageHeaderUsesStageHeadingAttribute(t *testing.T) {
	var buf strings.Builder
	err := SelectedStageHeader("intake", RunPreviews{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render SelectedStageHeader: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-run-stage-heading`) {
		t.Errorf("expected data-run-stage-heading attribute")
	}
	if strings.Contains(html, `data-run-step-heading`) {
		t.Errorf("should not contain old data-run-step-heading attribute")
	}
}

func TestStepDisplayLabelReturnsStageNames(t *testing.T) {
	if stepDisplayLabel("intake") != "Intake" {
		t.Errorf("expected Intake")
	}
	if stepDisplayLabel("prompt") != "Prompt" {
		t.Errorf("expected Prompt")
	}
	if stepDisplayLabel("handoff") != "OpenCode" {
		t.Errorf("expected OpenCode")
	}
	if stepDisplayLabel("validation") != "Validation" {
		t.Errorf("expected Validation")
	}
	if stepDisplayLabel("audit") != "Diff / Audit" {
		t.Errorf("expected Diff / Audit")
	}
	if stepDisplayLabel("commit") != "Commit" {
		t.Errorf("expected Commit for commit step")
	}
}

func TestStageEvidenceRowRendersStatusLabelSummaryAndActions(t *testing.T) {
	var buf strings.Builder
	err := StageEvidenceRow("passed", "Intake Review", "Review passed", "2 blockers").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render StageEvidenceRow: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-evidence-row`) {
		t.Errorf("expected data-stage-evidence-row attribute")
	}
	if !strings.Contains(html, "relay-stage-evidence-row-passed") {
		t.Errorf("expected passed variant class")
	}
	if !strings.Contains(html, "Intake Review") {
		t.Errorf("expected title text")
	}
	if !strings.Contains(html, "Review passed") {
		t.Errorf("expected summary text")
	}
	if !strings.Contains(html, "2 blockers") {
		t.Errorf("expected meta text")
	}
}

func TestStageFailurePanelRendersPrimaryFailureAndActions(t *testing.T) {
	var buf strings.Builder
	err := StageFailurePanel("OpenCode adapter blocked", "Adapter error text").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render StageFailurePanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-failure-panel`) {
		t.Errorf("expected data-stage-failure-panel attribute")
	}
	if !strings.Contains(html, "OpenCode adapter blocked") {
		t.Errorf("expected title text")
	}
	if !strings.Contains(html, "Adapter error text") {
		t.Errorf("expected summary text")
	}
}

func TestValidationStagePrioritizesFailedCommandEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{{Kind: "validation_run_json"}}
	previews := RunPreviews{
		HasValidationCommands: true,
		HasGitStatus:          true,
		HasAuditHandoff:       true,
		ValidationRun: ValidationRunPreview{
			Status:       "fail",
			CommandCount: 1,
			PassedCount:  0,
			FailedCount:  1,
			Commands: []ValidationCommandPreview{
				{Command: "go test", Status: "fail", ExitCode: 1, DurationMs: 500, HasStdout: true},
			},
		},
	}
	checks := []store.Check{{Kind: "validation_run", Status: "fail"}}
	err := RelayValidationStepPanel(run, artifacts, checks, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-failure-panel`) {
		t.Errorf("expected failure panel for failed command")
	}
	if !strings.Contains(html, "go test") {
		t.Errorf("expected failed command to be visible")
	}
}

func TestValidationStageShowsMissingCommandsEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{{Kind: "agent_result_raw"}}
	previews := RunPreviews{
		HasValidationCommands: false,
	}
	err := RelayValidationStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-failure-panel`) {
		t.Errorf("expected failure panel for missing commands")
	}
	if !strings.Contains(html, "No validation commands") {
		t.Errorf("expected no commands message")
	}
}

func TestValidationStageShowsRunningProgressEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		ValidationProgressRunning: true,
		HasValidationCommands:     true,
		ValidationProgressPreview: ValidationProgressPreview{
			Status:        "running",
			StartedAt:     "2024-01-01T00:00:00Z",
			UpdatedAt:     "2024-01-01T00:00:10Z",
			TotalCommands: 3,
			CurrentIndex:  1,
		},
	}
	err := RelayValidationStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Validation is running") {
		t.Errorf("expected running message")
	}
	if !strings.Contains(html, "1 / 3") {
		t.Errorf("expected progress indicator")
	}
}

func TestAgentRunStageShowsExecutionFailureEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "failed",
		OpenCodeFailureHint:     "OpenCode timed out",
		HasOpenCodeStdout:       true,
		HasOpenCodeStderr:       true,
	}
	err := AgentRunMonitorStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-failure-panel`) {
		t.Errorf("expected failure panel for execution failure")
	}
	if !strings.Contains(html, "Execution failed") {
		t.Errorf("expected Execution failed title")
	}
	if !strings.Contains(html, "OpenCode timed out") {
		t.Errorf("expected failure hint")
	}
}

func TestOpenCodeHandoffStageShowsBlockedAdapterEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", SelectedModel: "gpt-4"}
	artifacts := []store.Artifact{{Kind: "opencode_handoff_packet"}}
	previews := RunPreviews{
		OpenCodeAdapterError: "Binary not found in PATH",
	}
	err := OpenCodeGoHandoffStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render OpenCodeGoHandoffStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-failure-panel`) {
		t.Errorf("expected failure panel for blocked adapter")
	}
	if !strings.Contains(html, "OpenCode adapter blocked") {
		t.Errorf("expected adapter blocked title")
	}
	if !strings.Contains(html, "Binary not found in PATH") {
		t.Errorf("expected adapter error text")
	}
}

func TestDiffAuditStageShowsGitEvidenceRows(t *testing.T) {
	var buf strings.Builder
	previews := RunPreviews{
		HasGitStatus:        true,
		HasGitDiffStat:      true,
		HasGitDiffPatch:     true,
		GitChangedFileCount: 3,
		GitDiffSummary:      "Modified 3 files",
	}
	err := DiffAuditStepPanel(previews, 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render DiffAuditStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-evidence-row`) {
		t.Errorf("expected evidence rows")
	}
	if !strings.Contains(html, "Git diff evidence ready") {
		t.Errorf("expected git diff ready title")
	}
	if !strings.Contains(html, "Changed files: 3") {
		t.Errorf("expected changed file count")
	}
	if !strings.Contains(html, "View status") {
		t.Errorf("expected git status artifact link")
	}
	if !strings.Contains(html, "View diff stat") {
		t.Errorf("expected diff stat artifact link")
	}
}

func TestCommitStageShowsCommitSuggestionEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		HasCommitSuggestion: true,
		CommitMessage:       "feat: add new feature",
	}
	err := GitCommitStepPanel(run, artifacts, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render GitCommitStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-evidence-row`) {
		t.Errorf("expected evidence rows")
	}
	if !strings.Contains(html, "Commit suggestion ready") {
		t.Errorf("expected commit suggestion title")
	}
	if !strings.Contains(html, "Suggested commit message") {
		t.Errorf("expected commit message section")
	}
	if !strings.Contains(html, "feat: add new feature") {
		t.Errorf("expected commit message text")
	}
	if !strings.Contains(html, "Manual commit reminder") {
		t.Errorf("expected manual commit reminder")
	}
}

func TestRunDetailsRailRendersOnDesktop(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", BranchName: "main", SelectedModel: "gpt-4"}
	repo := &store.Repo{Name: "test-repo"}
	artifacts := []store.Artifact{
		{Kind: "original_handoff", CreatedAt: "2024-01-01"},
		{Kind: "agent_prompt", CreatedAt: "2024-01-01"},
	}
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}
	previews := RunPreviews{
		NextAction: WorkbenchNextActionView{
			Kind:     "generate_agent_prompt",
			Title:    "Generate Agent Prompt",
			Severity: "ready",
		},
		HasAuditHandoff:     true,
		HasCommitSuggestion: true,
	}
	err := RunDetailsRail(run, repo, artifacts, checks, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetailsRail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `aria-label="Run details"`) {
		t.Errorf("expected Run details aria-label")
	}
	if !strings.Contains(html, "relay-details-rail") {
		t.Errorf("expected relay-details-rail class")
	}
	if !strings.Contains(html, "test-repo") {
		t.Errorf("expected repo name")
	}
	if !strings.Contains(html, "main") {
		t.Errorf("expected branch name")
	}
	if !strings.Contains(html, "gpt-4") {
		t.Errorf("expected model name")
	}
	if !strings.Contains(html, "draft") {
		t.Errorf("expected run status")
	}
	if !strings.Contains(html, "Generate Agent Prompt") {
		t.Errorf("expected current gate title")
	}
	// artifact count
	if !strings.Contains(html, "2") {
		t.Errorf("expected artifact count 2")
	}
	if !strings.Contains(html, "passed") {
		t.Errorf("expected validation status passed")
	}
	if !strings.Contains(html, "ready") {
		t.Errorf("expected ready status for audit")
	}
	if !strings.Contains(html, "Manual Status Controls") {
		t.Errorf("expected legacy actions section")
	}
}

func TestRunDetailsSummaryRendersLabels(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetailsSummary(run, nil, RunPreviews{}, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetailsSummary: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Repo") {
		t.Errorf("expected Repo label")
	}
	if !strings.Contains(html, "Branch") {
		t.Errorf("expected Branch label")
	}
	if !strings.Contains(html, "Model") {
		t.Errorf("expected Model label")
	}
	if !strings.Contains(html, "Status") {
		t.Errorf("expected Status label")
	}
	if !strings.Contains(html, "not set") {
		t.Errorf("expected not set for missing values")
	}
}

func TestArtifactShortcutGroupsRenderNormalLinks(t *testing.T) {
	var buf strings.Builder
	artifacts := []store.Artifact{
		{Kind: "original_handoff", CreatedAt: "2024-01-01"},
		{Kind: "agent_prompt", CreatedAt: "2024-01-01"},
		{Kind: "audit_handoff", CreatedAt: "2024-01-01"},
	}
	err := ArtifactShortcutGroups(1, artifacts).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactShortcutGroups: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Intake / Prompt") {
		t.Errorf("expected Intake / Prompt group label")
	}
	if !strings.Contains(html, "Diff / Audit") {
		t.Errorf("expected Diff / Audit group label")
	}
	if !strings.Contains(html, `/artifacts/original_handoff`) {
		t.Errorf("expected artifact href")
	}
	if !strings.Contains(html, `/artifacts/agent_prompt`) {
		t.Errorf("expected artifact href for prompt")
	}
	if !strings.Contains(html, `/artifacts/audit_handoff`) {
		t.Errorf("expected artifact href for audit")
	}
	if strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("artifact shortcut links should not have shell hx-target")
	}
	if strings.Contains(html, `hx-get`) {
		t.Errorf("artifact shortcut links should not have hx-get")
	}
}

func TestLatestEventsSummaryRendersCompact(t *testing.T) {
	var buf strings.Builder
	events := []store.Event{
		{Level: "info", Message: "Event 1", CreatedAt: "2024-01-01"},
		{Level: "warn", Message: "Event 2", CreatedAt: "2024-01-01"},
		{Level: "error", Message: "Event 3", CreatedAt: "2024-01-01"},
	}
	err := LatestEventsSummary(events, 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render LatestEventsSummary: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Latest Events") {
		t.Errorf("expected Latest Events header")
	}
	if !strings.Contains(html, "Event 1") {
		t.Errorf("expected event 1")
	}
	if !strings.Contains(html, "Event 2") {
		t.Errorf("expected event 2")
	}
	if !strings.Contains(html, "Event 3") {
		t.Errorf("expected event 3")
	}
	if strings.Contains(html, "View all") {
		t.Errorf("should not show view all when exactly 3 events")
	}
	// Add a 4th event to trigger the disclosure
	var buf2 strings.Builder
	events4 := append(events, store.Event{Level: "info", Message: "Event 4", CreatedAt: "2024-01-01"})
	err = LatestEventsSummary(events4, 1).Render(context.Background(), &buf2)
	if err != nil {
		t.Fatalf("render LatestEventsSummary with 4 events: %v", err)
	}
	html2 := buf2.String()
	if !strings.Contains(html2, "View all 4 events") {
		t.Errorf("expected View all link with count")
	}
}

func TestLatestEventsSummaryShowsEmptyState(t *testing.T) {
	var buf strings.Builder
	err := LatestEventsSummary(nil, 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render LatestEventsSummary empty: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "No events yet.") {
		t.Errorf("expected empty state")
	}
}

func TestRunMaterialsDisclosureRemainsAvailable(t *testing.T) {
	var buf strings.Builder
	err := RunMaterialsDisclosure(RunPreviews{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunMaterialsDisclosure: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Raw Run Materials") {
		t.Errorf("expected Raw Run Materials label")
	}
	if !strings.Contains(html, "Original Handoff") {
		t.Errorf("expected Original Handoff preview reference")
	}
}

func TestLegacyActionsRemainAvailableInRail(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetailsRail(run, nil, nil, nil, nil, RunPreviews{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetailsRail for legacy: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Manual Status Controls") {
		t.Errorf("expected Manual Status Controls label")
	}
	if !strings.Contains(html, "Mark Accepted") {
		t.Errorf("expected Mark Accepted button")
	}
	if !strings.Contains(html, "Mark Needs Cleanup") {
		t.Errorf("expected Mark Needs Cleanup button")
	}
}

func TestRunDetailsRailUsesMobileSafeClasses(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", BranchName: "main", SelectedModel: "gpt-4"}
	repo := &store.Repo{Name: "test-repo"}
	artifacts := []store.Artifact{
		{Kind: "original_handoff", CreatedAt: "2024-01-01"},
	}
	err := RunDetailsRail(run, repo, artifacts, nil, nil, RunPreviews{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetailsRail mobile: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "relay-details-rail") {
		t.Errorf("expected relay-details-rail container")
	}
	if !strings.Contains(html, "relay-detail-value") {
		t.Errorf("expected relay-detail-value class for break-all/min-w-0")
	}
	if !strings.Contains(html, "relay-detail-row") {
		t.Errorf("expected relay-detail-row class for wrapping details")
	}
}

func TestRunDetailsRailFullArtifactListAvailable(t *testing.T) {
	var buf strings.Builder
	artifacts := []store.Artifact{
		{Kind: "original_handoff", CreatedAt: "2024-01-01"},
	}
	err := RunDetailsRail(&store.Run{ID: 1}, nil, artifacts, nil, nil, RunPreviews{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetailsRail full artifacts: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "All Artifacts") {
		t.Errorf("expected All Artifacts disclosure")
	}
	if !strings.Contains(html, "1 files") {
		t.Errorf("expected file count in disclosure header")
	}
	if !strings.Contains(html, `/artifacts/original_handoff`) {
		t.Errorf("expected full artifact list to include artifact links")
	}
}

func TestRunDetailRendersDetailsRailInInspectorShell(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail for rail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "relay-details-rail") {
		t.Errorf("expected relay-details-rail class in RunDetail output")
	}
	if !strings.Contains(html, "aria-label=\"Run details\"") {
		t.Errorf("expected Run details aria-label in RunDetail output")
	}
	if !strings.Contains(html, "Run Details") {
		t.Errorf("expected Run Details card header")
	}
}

func TestRunDetailGridIncludesRightRail(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail for grid: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "relay-inspector-grid") {
		t.Errorf("expected relay-inspector-grid class")
	}
	if !strings.Contains(html, "relay-details-rail") {
		t.Errorf("expected details rail in run detail output")
	}
	if !strings.Contains(html, "run-workbench-shell") {
		t.Errorf("expected workbench shell")
	}
}

func TestFilterArtifactKinds(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "original_handoff"},
		{Kind: "agent_prompt"},
		{Kind: "audit_handoff"},
	}
	kinds := []string{"original_handoff", "agent_prompt", "nonexistent"}
	result := filterArtifactKinds(artifacts, kinds)
	if len(result) != 2 {
		t.Errorf("expected 2 matching kinds, got %d", len(result))
	}
	if result[0] != "original_handoff" {
		t.Errorf("expected original_handoff first")
	}
	if result[1] != "agent_prompt" {
		t.Errorf("expected agent_prompt second")
	}
}

func TestShortArtifactLabel(t *testing.T) {
	if shortArtifactLabel("original_handoff") != "handoff" {
		t.Errorf("expected handoff")
	}
	if shortArtifactLabel("agent_prompt") != "prompt" {
		t.Errorf("expected prompt")
	}
	if shortArtifactLabel("unknown_kind") != "unknown_kind" {
		t.Errorf("expected passthrough for unknown kinds")
	}
}

func TestLatestEvents(t *testing.T) {
	events := make([]store.Event, 5)
	for i := 0; i < 5; i++ {
		events[i] = store.Event{Message: Itoa(int64(i))}
	}
	result := latestEvents(events, 3)
	if len(result) != 3 {
		t.Errorf("expected 3 events, got %d", len(result))
	}
	if result[0].Message != "2" {
		t.Errorf("expected first to be index 2, got %s", result[0].Message)
	}
	if result[2].Message != "4" {
		t.Errorf("expected last to be index 4, got %s", result[2].Message)
	}
	// Test when fewer events than n
	short := make([]store.Event, 2)
	for i := 0; i < 2; i++ {
		short[i] = store.Event{Message: Itoa(int64(i))}
	}
	result2 := latestEvents(short, 3)
	if len(result2) != 2 {
		t.Errorf("expected 2 events when only 2 available, got %d", len(result2))
	}
}

func TestEvidenceRowsUseMobileSafeClasses(t *testing.T) {
	var buf strings.Builder
	err := StageEvidenceRow("passed", "Test", "Summary", "meta").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render StageEvidenceRow: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "min-w-0") {
		t.Errorf("expected min-w-0 for mobile safety")
	}
	if !strings.Contains(html, "break-words") {
		t.Errorf("expected break-words for mobile safety")
	}
	if !strings.Contains(html, "relay-action-row") {
		t.Errorf("expected relay-action-row for wrapping actions")
	}
}
