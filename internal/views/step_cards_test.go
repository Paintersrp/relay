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
}
