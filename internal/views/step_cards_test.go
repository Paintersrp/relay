package views

import (
	"context"
	"os"
	"path/filepath"
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

func TestStageNavigationLinksIncludeSettleTiming(t *testing.T) {
	var buf strings.Builder
	err := PipelineStageRail(1, RunPreviews{}, nil, nil, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PipelineStageRail: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `settle:120ms`) {
		t.Errorf("expected settle:120ms in hx-swap on stage rail links, got: %s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected stage rail icons to be decorative, got: %s", html)
	}
	if strings.Contains(html, `relay-stage-symbol`) {
		t.Errorf("did not expect old stage symbol markup, got: %s", html)
	}
}

func TestStageStripLinksIncludeSettleTiming(t *testing.T) {
	var buf strings.Builder
	err := PipelineStageStrip(1, RunPreviews{}, nil, nil, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PipelineStageStrip: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `settle:120ms`) {
		t.Errorf("expected settle:120ms in hx-swap on stage strip links, got: %s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected stage strip icons to be decorative, got: %s", html)
	}
}

func TestStepFlowFooterLinksIncludeSettleTiming(t *testing.T) {
	var buf strings.Builder
	err := StepFlowFooter(1, "validation").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render StepFlowFooter: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `settle:120ms`) {
		t.Errorf("expected settle:120ms in hx-swap on footer links, got: %s", html)
	}
}

func TestWorkbenchActionFormIncludesSettleTiming(t *testing.T) {
	var buf strings.Builder
	err := WorkbenchActionForm(1, "test-action", "w-full").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render WorkbenchActionForm: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `settle:120ms`) {
		t.Errorf("expected settle:120ms in hx-swap on action form, got: %s", html)
	}
}

func TestRunInspectorActionFormIncludesSettleTiming(t *testing.T) {
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
	if !strings.Contains(html, `settle:120ms`) {
		t.Errorf("expected settle:120ms in hx-swap on inspector summary, got: %s", html)
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
	if !strings.Contains(html, `id="run-workbench"`) {
		t.Errorf("expected run-workbench id")
	}
	if !strings.Contains(html, `data-relay-workbench`) {
		t.Errorf("expected data-relay-workbench attribute")
	}
	if !strings.Contains(html, `data-relay-run-events="/runs/1/events"`) {
		t.Errorf("expected stable run event stream attribute")
	}
	if !strings.Contains(html, `data-relay-run-url="/runs/1?step=intake"`) {
		t.Errorf("expected stable run URL attribute")
	}
	if strings.Count(html, `data-relay-live-updates-indicator`) != 1 {
		t.Errorf("expected exactly one live updates indicator")
	}
	if strings.Index(html, `data-relay-live-updates-indicator`) > strings.Index(html, `id="run-workbench-shell"`) {
		t.Errorf("expected live indicator outside the swapped shell")
	}
	if !strings.Contains(html, `id="run-workbench-shell"`) {
		t.Errorf("expected run-workbench-shell id")
	}
	if !strings.Contains(html, `data-relay-run-url="/runs/1?step=intake"`) {
		t.Errorf("expected shell refresh URL to reflect the active step")
	}
}

func TestRunDetailRendersSetupReviewBannerWhenEnabled(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", SelectedModel: "DeepSeek V4 Pro", RecommendedModel: "DeepSeek V4 Flash"}
	artifacts := []store.Artifact{{Kind: "agent_prompt"}, {Kind: "opencode_handoff_packet"}}
	previews := RunPreviews{
		NextAction: WorkbenchNextActionView{
			Kind:              "check_opencode_cli",
			Title:             "Check OpenCode CLI",
			Summary:           "Verify the local OpenCode binary and resolved model before starting execution.",
			Step:              "handoff",
			PrimaryFormAction: "check-opencode-cli",
			Severity:          "ready",
		},
		AgentPrompt:            "# prompt",
		AgentPromptEstimate:    "1.2 KB (~300 tokens, approximate)",
		OpenCodePacket:         "{\"selected_model\":\"DeepSeek V4 Pro\"}",
		GitBaselineAvailable:   true,
		GitBaselineBaselineSHA: "abcdef1234567890",
	}
	review := &pipeline.IntakeReview{
		Metadata: pipeline.HandoffMetadata{FinalOutputContract: "Return DONE or BLOCKED."},
	}
	err := RunDetail(run, nil, artifacts, nil, nil, previews, review, "handoff", true).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail with setup review: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Run setup") {
		t.Fatalf("expected run setup banner, got:\n%s", html)
	}
	if !strings.Contains(html, "Relay already completed Intake, Prompt, and Packet") {
		t.Fatalf("expected setup completion explanation, got:\n%s", html)
	}
	if !strings.Contains(html, "Check OpenCode CLI") {
		t.Fatalf("expected current gate action in setup review, got:\n%s", html)
	}
	if !strings.Contains(html, "View Intake") || !strings.Contains(html, "View Prompt") || !strings.Contains(html, "View Packet") {
		t.Fatalf("expected setup review stage links, got:\n%s", html)
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

func TestRunInspectorSummaryShowsPushToUpstreamForCommittedLocal(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		NextAction: WorkbenchNextActionView{
			Kind:              "committed_local",
			Title:             "Committed locally",
			Summary:           "Commit created and ready to push upstream.",
			Step:              "commit",
			PrimaryFormAction: "push-git-commit",
			Severity:          "ready",
		},
	}
	err := RunInspectorSummary(run, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunInspectorSummary: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Committed locally") {
		t.Fatalf("expected committed locally title, got:\n%s", html)
	}
	if !strings.Contains(html, "Push to Upstream") {
		t.Fatalf("expected push button label, got:\n%s", html)
	}
	if strings.Contains(html, "Ready to commit") {
		t.Fatalf("expected not to show stale ready-to-commit text, got:\n%s", html)
	}
}

func TestRunInspectorSummaryShowsPushedForPushedState(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		NextAction: WorkbenchNextActionView{
			Kind:     "pushed",
			Title:    "Pushed",
			Summary:  "Commit has been pushed to the upstream branch.",
			Severity: "done",
		},
	}
	err := RunInspectorSummary(run, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunInspectorSummary: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Pushed") {
		t.Fatalf("expected pushed title, got:\n%s", html)
	}
	if strings.Contains(html, "Ready to commit") {
		t.Fatalf("expected not to show ready-to-commit text, got:\n%s", html)
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

func TestAgentPromptStepPanelGeneratedPromptShowsTransformationReview(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", SelectedModel: "DeepSeek V4 Pro", RecommendedModel: "DeepSeek V4 Flash"}
	artifacts := []store.Artifact{
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
	}
	review := &pipeline.IntakeReview{
		Metadata: pipeline.HandoffMetadata{
			ScopedFiles: []pipeline.ScopedFile{
				{Path: "internal/handlers/runs.go"},
				{Path: "internal/views/step_cards.templ"},
			},
			FinalOutputContract: "Return DONE or BLOCKED.",
		},
	}
	previews := RunPreviews{
		AgentPrompt:         "# Prompt\n\nDo the thing.",
		AgentPromptEstimate: "1.2 KB (~300 tokens, approximate)",
		AgentPromptDiff: PreviewDiff{
			Lines: []PreviewDiffLine{
				{Kind: "remove", Text: "Orchestration wrapper"},
				{Kind: "add", Text: "Compact repo-agent prompt"},
			},
		},
	}
	err := AgentPromptStepPanel(run, artifacts, nil, previews, review).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentPromptStepPanel generated state: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Transformation summary") {
		t.Fatalf("expected transformation summary, got:\n%s", html)
	}
	if !strings.Contains(html, "Continue to Packet") {
		t.Fatalf("expected continue to packet primary action, got:\n%s", html)
	}
	if !strings.Contains(html, "Regenerate Agent Prompt") {
		t.Fatalf("expected regenerate prompt secondary action, got:\n%s", html)
	}
	continueIdx := strings.Index(html, "Continue to Packet")
	regenIdx := strings.Index(html, "Regenerate Agent Prompt")
	if continueIdx == -1 || regenIdx == -1 || continueIdx > regenIdx {
		t.Fatalf("expected Continue to Packet before Regenerate Agent Prompt, got:\n%s", html)
	}
	if strings.Count(html, "btn-primary") != 1 {
		t.Fatalf("expected only Continue to Packet to use btn-primary in the generated prompt state, got:\n%s", html)
	}
	if !strings.Contains(html, "Model and execution") {
		t.Fatalf("expected model and execution section, got:\n%s", html)
	}
	if !strings.Contains(html, "Scope preserved") {
		t.Fatalf("expected scope preserved section, got:\n%s", html)
	}
	if !strings.Contains(html, "Prompt preview") {
		t.Fatalf("expected prompt preview section, got:\n%s", html)
	}
	summaryIdx := strings.Index(html, "Transformation summary")
	diffIdx := strings.Index(html, "Technical prompt diff")
	if summaryIdx == -1 || diffIdx == -1 || summaryIdx > diffIdx {
		t.Fatalf("expected transformation summary before technical prompt diff, got:\n%s", html)
	}
	if !strings.Contains(html, "Technical prompt diff") {
		t.Fatalf("expected collapsed technical diff, got:\n%s", html)
	}
	if !strings.Contains(html, "<details") || strings.Contains(html, "<details open") {
		t.Fatalf("expected technical diff to be collapsed by default, got:\n%s", html)
	}
	if !strings.Contains(html, "View Full Prompt") || !strings.Contains(html, "Download Prompt") {
		t.Fatalf("expected prompt artifact links, got:\n%s", html)
	}
}

func TestAgentPromptStepPanelNoPromptShowsGenerateAction(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := AgentPromptStepPanel(run, nil, nil, RunPreviews{}, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentPromptStepPanel empty state: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Generate Agent Prompt") {
		t.Fatalf("expected generate prompt primary action, got:\n%s", html)
	}
	if strings.Contains(html, "Technical prompt diff") || strings.Contains(html, "<details") {
		t.Fatalf("did not expect technical diff before prompt generation, got:\n%s", html)
	}
	if !strings.Contains(html, "What Relay will generate") {
		t.Fatalf("expected generation explanation, got:\n%s", html)
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

func TestIconHelperRendersKnownIcon(t *testing.T) {
	var buf strings.Builder
	err := Icon("check-circle", "relay-icon-sm").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Icon: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `<svg`) {
		t.Fatalf("expected svg output, got:\n%s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected decorative icon to be hidden from assistive tech")
	}
	if !strings.Contains(html, `focusable="false"`) {
		t.Errorf("expected decorative icon to be unfocusable")
	}
	if !strings.Contains(html, `relay-icon-sm`) {
		t.Errorf("expected icon size class")
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
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected status icon to be decorative")
	}
}

func TestIconHelperFallsBackForUnknownIcon(t *testing.T) {
	var buf strings.Builder
	err := Icon("definitely-not-real", "relay-icon-sm").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render fallback icon: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `<svg`) {
		t.Fatalf("expected svg output for fallback icon, got:\n%s", html)
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected fallback icon to be decorative")
	}
}

func TestStageEvidenceRowNotAvailableRendersValidCSS(t *testing.T) {
	var buf strings.Builder
	err := StageEvidenceRow("not-available", "Test", "Summary", "").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render StageEvidenceRow: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "relay-stage-evidence-row-not-available") {
		t.Errorf("expected relay-stage-evidence-row-not-available class")
	}
	if !strings.Contains(html, "relay-stage-evidence-symbol-not-available") {
		t.Errorf("expected relay-stage-evidence-symbol-not-available class")
	}
	if strings.Contains(html, `relay-stage-evidence-row-not available`) {
		t.Errorf("should not contain space-separated not available class")
	}
	if strings.Contains(html, `relay-stage-evidence-symbol-not available`) {
		t.Errorf("should not contain space-separated not available symbol class")
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected stage evidence icon to be decorative")
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
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected failure icon to be decorative")
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
	artifacts := []store.Artifact{
		{Kind: "validation_stdout"},
		{Kind: "validation_stderr"},
	}
	previews := RunPreviews{
		ValidationProgressRunning: true,
		HasValidationCommands:     true,
		ValidationProgressPreview: ValidationProgressPreview{
			Status:         "running",
			StartedAt:      "2024-01-01T00:00:00Z",
			UpdatedAt:      "2024-01-01T00:00:10Z",
			TotalCommands:  3,
			CurrentIndex:   2,
			CurrentCommand: "go test ./...",
			PendingCount:   1,
			RunningCount:   1,
			PassedCount:    1,
			CompletedCount: 1,
			Commands: []ValidationProgressCommandView{
				{Index: 1, Command: "templ generate", Status: "pass", DurationMs: 1500, HasStdout: true},
				{Index: 2, Command: "go test ./...", Status: "running", StartedAt: "2024-01-01T00:00:10Z", HasStdout: true, HasStderr: true},
				{Index: 3, Command: "go vet ./...", Status: "pending"},
			},
		},
	}
	err := RelayValidationStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("did not expect polling wrapper")
	}
	if !strings.Contains(html, "Validation live progress") {
		t.Errorf("expected live progress title")
	}
	if !strings.Contains(html, "go test ./...") {
		t.Errorf("expected current command to render")
	}
	if !strings.Contains(html, "templ generate") || !strings.Contains(html, "go vet ./...") {
		t.Errorf("expected planned commands to render")
	}
	if !strings.Contains(html, "pending") || !strings.Contains(html, "running") || !strings.Contains(html, "passed") {
		t.Errorf("expected command status chips to render")
	}
	if !strings.Contains(html, "stdout") || !strings.Contains(html, "stderr") {
		t.Errorf("expected artifact links to render")
	}
}

func TestValidationStageDoesNotPollAfterTerminalProgress(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1}
	artifacts := []store.Artifact{
		{Kind: "validation_run_json"},
		{Kind: "validation_stdout"},
		{Kind: "validation_stderr"},
	}
	previews := RunPreviews{
		ValidationProgressRunning: false,
		HasValidationCommands:     true,
		ValidationProgressPreview: ValidationProgressPreview{
			Status:         "pass",
			StartedAt:      "2024-01-01T00:00:00Z",
			UpdatedAt:      "2024-01-01T00:01:00Z",
			FinishedAt:     "2024-01-01T00:01:00Z",
			TotalCommands:  2,
			PassedCount:    2,
			CompletedCount: 2,
		},
		ValidationRun: ValidationRunPreview{
			Status:        "pass",
			CommandCount:  2,
			PassedCount:   2,
			TimedOutCount: 0,
			Commands: []ValidationCommandPreview{
				{Command: "templ generate", Status: "pass", DurationMs: 1500, HasStdout: true},
				{Command: "go test ./...", Status: "pass", DurationMs: 5000, HasStderr: true},
			},
		},
	}
	err := RelayValidationStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("did not expect polling wrapper after terminal progress")
	}
	if !strings.Contains(html, "Validation results") {
		t.Errorf("expected validation results card")
	}
	if !strings.Contains(html, "View validation JSON") || !strings.Contains(html, "View stdout") || !strings.Contains(html, "View stderr") {
		t.Errorf("expected validation artifact links")
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

func TestAgentRunStageShowsExecutionErrorFallback(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "failed",
		OpenCodeExecutionError:  "OpenCode execution recovered as failed: runtime exceeded the timeout window and no stdout/stderr artifacts were captured. Relay may have restarted, lost the worker, or OpenCode exited before producing output.",
	}
	err := AgentRunMonitorStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-failure-panel`) {
		t.Errorf("expected failure panel for execution failure")
	}
	if !strings.Contains(html, "runtime exceeded the timeout window") {
		t.Errorf("expected execution error fallback")
	}
	if strings.Contains(html, "OpenCode exited with code") {
		t.Errorf("did not expect exit code fallback when execution error is present")
	}
}

func TestAgentRunStageShowsCompletedWithoutResultFallbackWithoutGitEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "completed",
		OpenCodeLifecycleState:  "completed_without_result",
	}
	err := AgentRunMonitorStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "OpenCode completed without a final DONE/BLOCKED result") {
		t.Errorf("expected completed-without-result warning")
	}
	if !strings.Contains(html, "Inspect Git Diff (Step 7)") {
		t.Errorf("expected Step 7 diff inspection link")
	}
	if !strings.Contains(html, "Manual result intake fallback") {
		t.Errorf("expected manual result fallback")
	}
	if strings.Contains(html, "No repo changes detected") {
		t.Errorf("did not expect no-changes warning without git evidence")
	}
}

func TestAgentRunStageShowsNoChangesWarningWhenGitEvidenceIsClean(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "completed",
		OpenCodeLifecycleState:  "completed_without_result",
		HasGitChangeEvidence:    true,
		GitChangeEvidenceMode:   "no_changes",
	}
	err := AgentRunMonitorStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "No repo changes detected") {
		t.Errorf("expected no-changes warning when git evidence is clean")
	}
	if !strings.Contains(html, "Inspect Git Diff (Step 7)") {
		t.Errorf("expected Step 7 diff inspection link")
	}
	if strings.Contains(html, "Manual result intake fallback") {
		t.Errorf("did not expect manual fallback when git evidence is already clean")
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

func TestOpenCodeHandoffStageShowsStaleRecoveryEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", SelectedModel: "gpt-4"}
	artifacts := []store.Artifact{{Kind: "opencode_handoff_packet"}}
	previews := RunPreviews{
		HasOpenCodeExecution:        true,
		OpenCodeExecutionStatus:     "running",
		HasOpenCodeRunning:          false,
		HasOpenCodeStaleRunning:     true,
		OpenCodeLifecycleState:      "stale_timeout",
		OpenCodeStaleReason:         "OpenCode runtime 4h 21m exceeded the timeout window.",
		OpenCodeCanRecover:          true,
		OpenCodeRecoveryActionLabel: "Recover Stale OpenCode Run",
	}
	err := OpenCodeGoHandoffStepPanel(run, artifacts, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render OpenCodeGoHandoffStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "OpenCode execution exceeded timeout or Relay lost the worker") {
		t.Errorf("expected stale timeout warning, got: %s", html)
	}
	if !strings.Contains(html, "Review Agent Run") {
		t.Errorf("expected link back to the run monitor")
	}
	if !strings.Contains(html, "Recover the stale run in Step 5 before starting a new one.") {
		t.Errorf("expected blocked start copy for stale recovery")
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

func TestAuditDecisionStageRendersSingleAcceptButtonInStep7Only(t *testing.T) {
	var step7 strings.Builder
	var step8 strings.Builder

	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	pending := RunPreviews{HasAuditHandoff: true}
	blocked := RunPreviews{CommitState: "blocked_audit_not_accepted", HasAuditHandoff: true}

	if err := DiffAuditStepPanel(pending, 1).Render(context.Background(), &step7); err != nil {
		t.Fatalf("render DiffAuditStepPanel: %v", err)
	}
	if err := GitCommitStepPanel(run, nil, blocked).Render(context.Background(), &step8); err != nil {
		t.Fatalf("render GitCommitStepPanel: %v", err)
	}

	step7HTML := step7.String()
	step8HTML := step8.String()
	combined := step7HTML + step8HTML

	if count := strings.Count(combined, "Mark Audit Accepted"); count != 1 {
		t.Fatalf("expected one Mark Audit Accepted button across Step 7 and Step 8, got %d\nStep 7:\n%s\nStep 8:\n%s", count, step7HTML, step8HTML)
	}
	if !strings.Contains(step7HTML, "Mark Audit Accepted") {
		t.Fatalf("expected Step 7 to render Mark Audit Accepted, got:\n%s", step7HTML)
	}
	if strings.Contains(step8HTML, "Mark Audit Accepted") {
		t.Fatalf("expected Step 8 not to render Mark Audit Accepted, got:\n%s", step8HTML)
	}
	if !strings.Contains(step8HTML, "Go to Step 7: Diff / Audit") {
		t.Fatalf("expected Step 8 to link back to Step 7, got:\n%s", step8HTML)
	}
}

func TestAuditDecisionStageRendersAcceptedReadOnlyState(t *testing.T) {
	var step7 strings.Builder
	var step8 strings.Builder

	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	accepted := RunPreviews{
		HasAuditHandoff:          true,
		AuditClearanceStatus:     "accepted",
		AuditClearanceAcceptedAt: "2026-06-13T10:00:00Z",
		AuditClearanceSource:     "manual_ui",
		CommitState:              "ready_to_commit",
	}

	if err := DiffAuditStepPanel(accepted, 1).Render(context.Background(), &step7); err != nil {
		t.Fatalf("render DiffAuditStepPanel: %v", err)
	}
	if err := GitCommitStepPanel(run, nil, accepted).Render(context.Background(), &step8); err != nil {
		t.Fatalf("render GitCommitStepPanel: %v", err)
	}

	step7HTML := step7.String()
	step8HTML := step8.String()

	if !strings.Contains(step7HTML, "Audit accepted") {
		t.Fatalf("expected Step 7 accepted state, got:\n%s", step7HTML)
	}
	if !strings.Contains(step7HTML, "Revoke Audit Acceptance") {
		t.Fatalf("expected Step 7 revoke action, got:\n%s", step7HTML)
	}
	if !strings.Contains(step7HTML, "manual_ui") || !strings.Contains(step7HTML, "2026-06-13T10:00:00Z") {
		t.Fatalf("expected Step 7 to show accepted metadata, got:\n%s", step7HTML)
	}

	if !strings.Contains(step8HTML, "Audit accepted") {
		t.Fatalf("expected Step 8 read-only accepted state, got:\n%s", step8HTML)
	}
	if strings.Contains(step8HTML, "Mark Audit Accepted") {
		t.Fatalf("expected Step 8 not to render accept action, got:\n%s", step8HTML)
	}
	if strings.Contains(step8HTML, "Revoke Audit Acceptance") {
		t.Fatalf("expected Step 8 not to render revoke action, got:\n%s", step8HTML)
	}
	if !strings.Contains(step8HTML, "manual_ui") || !strings.Contains(step8HTML, "2026-06-13T10:00:00Z") {
		t.Fatalf("expected Step 8 to show accepted metadata, got:\n%s", step8HTML)
	}
}

func TestCommitStageShowsCommitSuggestionEvidence(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	artifacts := []store.Artifact{}
	previews := RunPreviews{
		HasCommitSuggestion:        true,
		CommitSuggestionSelected:   "feat: add new feature",
		CommitSuggestionSource:     "diff_summary",
		CommitSuggestionConfidence: "medium",
	}
	err := GitCommitStepPanel(run, artifacts, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render GitCommitStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-stage-evidence-row`) {
		t.Errorf("expected evidence rows")
	}
	if !strings.Contains(html, "Selected commit message") {
		t.Errorf("expected commit message section")
	}
	if !strings.Contains(html, "feat: add new feature") {
		t.Errorf("expected commit message text")
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
	if !strings.Contains(html, `hx-target="#run-artifact-preview"`) {
		t.Errorf("artifact shortcut links should target #run-artifact-preview")
	}
	if !strings.Contains(html, `hx-get="/runs/1/artifacts/original_handoff/preview"`) {
		t.Errorf("artifact shortcut links should have hx-get with preview URL")
	}
	if strings.Contains(html, `hx-push-url`) {
		t.Errorf("artifact shortcut links should not have hx-push-url")
	}
	if !strings.Contains(html, `data-relay-artifact-preview-link="true"`) {
		t.Errorf("artifact shortcut links should have data-relay-artifact-preview-link")
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
	// The nested disclosure was removed; only the compact preview remains
	if strings.Contains(html, "View all") {
		t.Errorf("LatestEventsSummary no longer has a nested View all disclosure")
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

func TestLatestEventsSummaryNoLongerHasNestedDisclosure(t *testing.T) {
	var buf strings.Builder
	events := []store.Event{
		{Level: "info", Message: "E1", CreatedAt: "2024-01-01"},
		{Level: "info", Message: "E2", CreatedAt: "2024-01-01"},
		{Level: "info", Message: "E3", CreatedAt: "2024-01-01"},
		{Level: "info", Message: "E4", CreatedAt: "2024-01-01"},
	}
	err := LatestEventsSummary(events, 1).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render LatestEventsSummary: %v", err)
	}
	html := buf.String()
	// Should show the compact preview (5 checkpoints)
	if !strings.Contains(html, "Latest Events") {
		t.Errorf("expected Latest Events header")
	}
	// Must NOT contain a nested disclosure for full event log
	if strings.Contains(html, "View all") {
		t.Errorf("LatestEventsSummary should not contain a nested full event log disclosure")
	}
}

func TestRunDetailsRailHasExactlyOneFullEventLogDisclosure(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	events := []store.Event{
		{Level: "info", Message: "E1", CreatedAt: "2024-01-01"},
		{Level: "info", Message: "E2", CreatedAt: "2024-01-01"},
	}
	err := RunDetailsRail(run, nil, nil, nil, events, RunPreviews{}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetailsRail: %v", err)
	}
	html := buf.String()
	count := strings.Count(html, "Full Event Log")
	if count != 1 {
		t.Errorf("expected exactly 1 Full Event Log disclosure, got %d", count)
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

func TestLatestEventsDeterministicWithTimestampsNewestFirst(t *testing.T) {
	events := []store.Event{
		{CreatedAt: "2024-01-03T00:00:00Z", Message: "oldest"},
		{CreatedAt: "2024-01-05T00:00:00Z", Message: "newest"},
		{CreatedAt: "2024-01-04T00:00:00Z", Message: "middle"},
	}
	result := latestEvents(events, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 events, got %d", len(result))
	}
	if result[0].Message != "newest" {
		t.Errorf("expected newest first, got %s", result[0].Message)
	}
	if result[1].Message != "middle" {
		t.Errorf("expected middle second, got %s", result[1].Message)
	}
	if result[2].Message != "oldest" {
		t.Errorf("expected oldest last, got %s", result[2].Message)
	}
}

func TestLatestEventsDeterministicWithTimestampsOldestFirst(t *testing.T) {
	events := []store.Event{
		{CreatedAt: "2024-01-01T00:00:00Z", Message: "first"},
		{CreatedAt: "2024-01-02T00:00:00Z", Message: "second"},
		{CreatedAt: "2024-01-03T00:00:00Z", Message: "third"},
	}
	result := latestEvents(events, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result))
	}
	if result[0].Message != "third" {
		t.Errorf("expected third (newest) first, got %s", result[0].Message)
	}
	if result[1].Message != "second" {
		t.Errorf("expected second second, got %s", result[1].Message)
	}
}

func TestLatestEventsNoTimestampsTakesLastN(t *testing.T) {
	events := []store.Event{
		{Message: "first"},
		{Message: "second"},
		{Message: "third"},
		{Message: "fourth"},
		{Message: "fifth"},
	}
	result := latestEvents(events, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3 events, got %d", len(result))
	}
	if result[0].Message != "third" {
		t.Errorf("expected third (last-2) first, got %s", result[0].Message)
	}
	if result[1].Message != "fourth" {
		t.Errorf("expected fourth (last-1) second, got %s", result[1].Message)
	}
	if result[2].Message != "fifth" {
		t.Errorf("expected fifth (last) third, got %s", result[2].Message)
	}
}

func TestLatestEventsEmptyReturnsNil(t *testing.T) {
	result := latestEvents(nil, 3)
	if result != nil {
		t.Errorf("expected nil for empty input")
	}
}

func TestLatestEventsFewerThanN(t *testing.T) {
	events := []store.Event{
		{CreatedAt: "2024-01-02T00:00:00Z", Message: "b"},
		{CreatedAt: "2024-01-01T00:00:00Z", Message: "a"},
	}
	result := latestEvents(events, 5)
	if len(result) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result))
	}
	if result[0].Message != "b" {
		t.Errorf("expected b (newest) first, got %s", result[0].Message)
	}
}

func TestLatestEventsDoesNotMutateOriginal(t *testing.T) {
	original := []store.Event{
		{CreatedAt: "2024-01-01T00:00:00Z", Message: "first"},
		{CreatedAt: "2024-01-02T00:00:00Z", Message: "second"},
	}
	_ = latestEvents(original, 1)
	if original[0].Message != "first" {
		t.Errorf("original should not be mutated, got %s", original[0].Message)
	}
	if original[1].Message != "second" {
		t.Errorf("original should not be mutated, got %s", original[1].Message)
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

func TestPipelineStageStripHasDataMarker(t *testing.T) {
	var buf strings.Builder
	err := PipelineStageStrip(1, RunPreviews{}, nil, nil, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PipelineStageStrip: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-pipeline-stage-strip`) {
		t.Errorf("expected data-pipeline-stage-strip marker on stage strip")
	}
	if !strings.Contains(html, `relay-stage-strip-row`) {
		t.Errorf("expected relay-stage-strip-row class on nav")
	}
}

func TestPipelineStageStripHasMobileWidthBounds(t *testing.T) {
	var buf strings.Builder
	err := PipelineStageStrip(1, RunPreviews{}, nil, nil, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render PipelineStageStrip: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `min-w-[8.5rem]`) {
		t.Errorf("expected min-w-[8.5rem] on stage strip items for mobile width")
	}
	if !strings.Contains(html, `max-w-[12rem]`) {
		t.Errorf("expected max-w-[12rem] on stage strip items for mobile width")
	}
	if !strings.Contains(html, `snap-start`) {
		t.Errorf("expected snap-start on stage strip items for scroll snapping")
	}
}

func TestRunInspectorSummaryHasMobileFlexLayout(t *testing.T) {
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
	if !strings.Contains(html, `flex-row flex-wrap`) {
		t.Errorf("expected flex-row flex-wrap for mobile-safe action layout")
	}
	if !strings.Contains(html, `sm:flex-col sm:items-end`) {
		t.Errorf("expected sm:flex-col sm:items-end for desktop action layout")
	}
}

func TestRunDetailsRailCSSHasMobileSpacingRule(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod from %s", dir)
		}
		dir = parent
	}
	data, err := os.ReadFile(filepath.Join(dir, "web", "src", "styles.css"))
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	content := string(data)
	idx := strings.LastIndex(content, ".relay-details-rail")
	if idx < 0 {
		t.Fatalf("styles.css missing .relay-details-rail rule")
	}
	ruleSection := content[idx:]
	if !strings.Contains(ruleSection, "mt-4") {
		t.Errorf(".relay-details-rail rule should contain mt-4 for mobile spacing")
	}
	if !strings.Contains(ruleSection, "lg:mt-0") {
		t.Errorf(".relay-details-rail rule should contain lg:mt-0 for desktop reset")
	}
	if !strings.Contains(ruleSection, "lg:sticky") {
		t.Errorf(".relay-details-rail rule should contain lg:sticky")
	}
	if !strings.Contains(ruleSection, "lg:top-4") {
		t.Errorf(".relay-details-rail rule should contain lg:top-4")
	}
	if !strings.Contains(ruleSection, "lg:self-start") {
		t.Errorf(".relay-details-rail rule should contain lg:self-start")
	}
}

func TestStep5RunningMonitorHasCorrectPollingAttributes(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "running",
		HasOpenCodeRunning:      true,
		OpenCodeLifecycleState:  "running_no_output",
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-get="/runs/1?step=run"`) {
		t.Errorf("did not expect Step 5 self-refresh hx-get, got: %s", html)
	}
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("did not expect Step 5 polling wrapper")
	}
	if !strings.Contains(html, "OpenCode activity") {
		t.Errorf("expected OpenCode activity block")
	}
}

func TestStep5RunningMonitorPollWrapperOmitsShowTop(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "running",
		HasOpenCodeRunning:      true,
		OpenCodeLifecycleState:  "running_no_output",
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-get="/runs/1?step=run"`) {
		t.Fatal("did not expect Step 5 polling wrapper")
	}
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Fatal("did not expect Step 5 polling trigger")
	}
}

func TestStep6ValidationRunningPollHasCorrectAttributes(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
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
	err := RelayValidationStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-get="/runs/1?step=validation"`) {
		t.Errorf("did not expect Step 6 self-refresh hx-get, got: %s", html)
	}
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("did not expect Step 6 polling wrapper")
	}
	if !strings.Contains(html, "Validation live progress") {
		t.Errorf("expected validation live progress block")
	}
}

func TestStep6ValidationRunningPollWrapperOmitsShowTop(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
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
	err := RelayValidationStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RelayValidationStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-get="/runs/1?step=validation"`) {
		t.Fatal("did not expect Step 6 polling wrapper")
	}
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Fatal("did not expect Step 6 polling trigger")
	}
}

func TestArtifactPreviewSlotHasCorrectID(t *testing.T) {
	var buf strings.Builder
	err := ArtifactPreviewSlot().Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactPreviewSlot: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="run-artifact-preview"`) {
		t.Errorf("expected id=run-artifact-preview on preview slot")
	}
	if !strings.Contains(html, `relay-artifact-preview-slot`) {
		t.Errorf("expected relay-artifact-preview-slot class on preview slot")
	}
}

func TestArtifactInlinePreviewContainsExpectedElements(t *testing.T) {
	var buf strings.Builder
	err := ArtifactInlinePreview(1, "agent_prompt", "test content", false).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactInlinePreview: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="run-artifact-preview"`) {
		t.Errorf("expected id=run-artifact-preview on preview card")
	}
	if !strings.Contains(html, `relay-artifact-preview-card`) {
		t.Errorf("expected relay-artifact-preview-card class")
	}
	if !strings.Contains(html, `relay-artifact-preview-pre`) {
		t.Errorf("expected relay-artifact-preview-pre class on pre element")
	}
	if !strings.Contains(html, `data-relay-clear-artifact-preview="true"`) {
		t.Errorf("expected close button with data-relay-clear-artifact-preview")
	}
	if !strings.Contains(html, "Open full") {
		t.Errorf("expected Open full link")
	}
	if !strings.Contains(html, "Download") {
		t.Errorf("expected Download link")
	}
	if !strings.Contains(html, `href="/runs/1/artifacts/agent_prompt"`) {
		t.Errorf("expected Open full href to artifact")
	}
	if !strings.Contains(html, `href="/runs/1/artifacts/agent_prompt/download"`) {
		t.Errorf("expected Download href to artifact download")
	}
}

func TestArtifactInlinePreviewTruncatedMessage(t *testing.T) {
	var buf strings.Builder
	err := ArtifactInlinePreview(1, "agent_prompt", "content", true).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactInlinePreview truncated: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Preview truncated") {
		t.Errorf("expected truncation notice when truncated=true")
	}
}

func TestArtifactInlinePreviewOmitsTruncatedMessageWhenNotTruncated(t *testing.T) {
	var buf strings.Builder
	err := ArtifactInlinePreview(1, "agent_prompt", "content", false).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactInlinePreview not truncated: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, "Preview truncated") {
		t.Errorf("should not contain truncation notice when truncated=false")
	}
}

func TestArtifactPreviewLinkRetainsHrefAndHXGet(t *testing.T) {
	var buf strings.Builder
	err := ArtifactPreviewLink(1, "agent_prompt", "text-xs").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render ArtifactPreviewLink: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `href="/runs/1/artifacts/agent_prompt"`) {
		t.Errorf("expected full href fallback")
	}
	if !strings.Contains(html, `hx-get="/runs/1/artifacts/agent_prompt/preview"`) {
		t.Errorf("expected hx-get for preview")
	}
	if !strings.Contains(html, `hx-target="#run-artifact-preview"`) {
		t.Errorf("expected hx-target pointing to run-artifact-preview")
	}
	if !strings.Contains(html, `data-relay-artifact-preview-link="true"`) {
		t.Errorf("expected data-relay-artifact-preview-link attribute")
	}
	if !strings.Contains(html, `aria-hidden="true"`) {
		t.Errorf("expected artifact preview icon to be decorative")
	}
}

func TestRunInspectorShellRendersDetailsRail(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunInspectorShell(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunInspectorShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `relay-details-rail`) {
		t.Errorf("expected relay-details-rail in inspector shell")
	}
	if !strings.Contains(html, `relay-stage-strip`) {
		t.Errorf("expected relay-stage-strip in inspector shell")
	}
	if !strings.Contains(html, `data-pipeline-stage-strip`) {
		t.Errorf("expected data-pipeline-stage-strip in inspector shell")
	}
}

func TestOpenCodeHandoffStageShowsSelectedModel(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", SelectedModel: "DeepSeek V4 Pro"}
	previews := RunPreviews{}
	err := OpenCodeGoHandoffStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render OpenCodeGoHandoffStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "DeepSeek V4 Pro") {
		t.Errorf("expected selected model text in output")
	}
}

func TestOpenCodeHandoffStageShowsModelOverrideForm(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft", SelectedModel: "gpt-4"}
	previews := RunPreviews{}
	err := OpenCodeGoHandoffStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render OpenCodeGoHandoffStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Change selected model") {
		t.Errorf("expected 'Change selected model' summary text")
	}
	if !strings.Contains(html, "update-selected-model") {
		t.Errorf("expected hidden action input with update-selected-model")
	}
	if !strings.Contains(html, `name="selected_model_option"`) {
		t.Errorf("expected select with name selected_model_option")
	}
	if !strings.Contains(html, `name="selected_model_custom"`) {
		t.Errorf("expected input with name selected_model_custom")
	}
	if !strings.Contains(html, `data-relay-workbench-action`) {
		t.Errorf("expected data-relay-workbench-action attribute on form")
	}
	if !strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("expected hx-target on form")
	}
	if !strings.Contains(html, `hx-select="#run-workbench-shell"`) {
		t.Errorf("expected hx-select on form")
	}
	if !strings.Contains(html, `settle:120ms`) {
		t.Errorf("expected settle:120ms in hx-swap on form")
	}
}

func TestAgentRunTerminalStateDoesNotPoll(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "completed",
		HasOpenCodeStdout:       true,
		HasOpenCodeStderr:       true,
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("terminal execution should not have hx-trigger polling")
	}
	// stdout and combined log links should be present
	if !strings.Contains(html, `download stdout`) {
		t.Errorf("expected stdout download link for terminal execution")
	}
	if !strings.Contains(html, `download stderr`) {
		t.Errorf("expected stderr download link for terminal execution")
	}
}

func TestAgentRunWaitingResponseShowsStopWaitingAction(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "running",
		HasOpenCodeRunning:      true,
		OpenCodeLifecycleState:  "waiting_response",
		OpenCodeLastOutputAge:   "3m ago",
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Stop Waiting and Inspect Git Diff") {
		t.Fatalf("expected stop-waiting CTA for waiting_response, got: %s", html)
	}
	if !strings.Contains(html, "stop-opencode-and-inspect-diff") {
		t.Fatal("expected stop-opencode-and-inspect-diff action for waiting_response")
	}
	if strings.Contains(html, "Recover Stale OpenCode Run") {
		t.Fatal("did not expect stale recovery CTA for waiting_response")
	}
}

func TestAgentRunStaleRunningWithCapturedOutputShowsRecoveryAction(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:        true,
		OpenCodeExecutionStatus:     "running",
		HasOpenCodeRunning:          false,
		HasOpenCodeStaleRunning:     true,
		OpenCodeLifecycleState:      "stale_output",
		OpenCodeStaleReason:         "OpenCode output stopped 20m ago.",
		OpenCodeRecoveryActionLabel: "Recover Stale OpenCode Run",
		HasOpenCodeStdout:           true,
		HasOpenCodeCombinedLog:      true,
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	// Should show recovery action
	if !strings.Contains(html, "Recover Stale OpenCode Run") {
		t.Errorf("expected recovery action for stale running state, got: %s", html)
	}
	if !strings.Contains(html, "recover-stale-opencode-execution") {
		t.Errorf("expected recover-stale-opencode-execution action input")
	}
	if !strings.Contains(html, "Stop Waiting and Inspect Git Diff") {
		t.Errorf("expected stop-waiting CTA for stale running state, got: %s", html)
	}
	if !strings.Contains(html, "stop-opencode-and-inspect-diff") {
		t.Errorf("expected stop-opencode-and-inspect-diff action input for stale running")
	}
	// Stale running executions should stop polling; the operator must click recovery.
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("stale running execution should stop hx-trigger polling, got: %s", html)
	}
	// Should show warning about captured output
	if !strings.Contains(html, "OpenCode output stopped before a final result") {
		t.Errorf("expected warning about captured output, got: %s", html)
	}
	// Log artifact links should be present
	if !strings.Contains(html, "download stdout") {
		t.Errorf("expected stdout download link for stale running")
	}
	if !strings.Contains(html, "download combined log") {
		t.Errorf("expected combined log download link for stale running")
	}
}

func TestAgentRunActiveStreamingAndOutputHideStopWaitingAction(t *testing.T) {
	testCases := []RunPreviews{
		{
			HasOpenCodeExecution:    true,
			OpenCodeExecutionStatus: "running",
			HasOpenCodeRunning:      true,
			OpenCodeLifecycleState:  "active_streaming",
		},
		{
			HasOpenCodeExecution:    true,
			OpenCodeExecutionStatus: "running",
			HasOpenCodeRunning:      true,
			HasOpenCodeOutput:       true,
			OpenCodeLifecycleState:  "active_output",
		},
	}

	for _, previews := range testCases {
		var buf strings.Builder
		run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
		err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
		if err != nil {
			t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
		}
		html := buf.String()
		if strings.Contains(html, "Stop Waiting and Inspect Git Diff") {
			t.Fatalf("did not expect stop-waiting CTA for %s", previews.OpenCodeLifecycleState)
		}
		if strings.Contains(html, "stop-opencode-and-inspect-diff") {
			t.Fatalf("did not expect stop-waiting action for %s", previews.OpenCodeLifecycleState)
		}
	}
}

func TestRunDetailRendersLiveUpdateIconSet(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail: %v", err)
	}
	html := buf.String()

	states := []string{"connecting", "connected", "reconnecting", "disconnected"}
	for _, state := range states {
		attr := `data-relay-live-updates-state-icon="` + state + `"`
		if strings.Count(html, attr) != 1 {
			t.Errorf("expected exactly one %s icon wrapper, got %d", attr, strings.Count(html, attr))
		}
	}

	if !strings.Contains(html, `data-relay-live-updates-icon`) {
		t.Errorf("expected outer icon wrapper with data-relay-live-updates-icon")
	}
}

func TestLiveUpdateIconSetConnectingVisibleByDefault(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	err := RunDetail(run, nil, nil, nil, nil, RunPreviews{}, nil, "intake").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RunDetail: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `data-relay-live-updates-state-icon="connecting"`) {
		t.Errorf("expected connecting icon wrapper")
	}

	if strings.Contains(html, `data-relay-live-updates-state-icon="connecting" hidden`) {
		t.Errorf("connecting icon should not have hidden attribute")
	}

	if !strings.Contains(html, `data-relay-live-updates-state-icon="connected" hidden`) {
		t.Errorf("connected icon should have hidden attribute in initial state")
	}
	if !strings.Contains(html, `data-relay-live-updates-state-icon="reconnecting" hidden`) {
		t.Errorf("reconnecting icon should have hidden attribute in initial state")
	}
	if !strings.Contains(html, `data-relay-live-updates-state-icon="disconnected" hidden`) {
		t.Errorf("disconnected icon should have hidden attribute in initial state")
	}
}

func TestLiveUpdateIconSetSVGsAreDecorative(t *testing.T) {
	var buf strings.Builder
	err := LiveUpdateIconSet("relay-icon-sm").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render LiveUpdateIconSet: %v", err)
	}
	html := buf.String()

	svgCount := strings.Count(html, `<svg`)
	if svgCount != 4 {
		t.Errorf("expected 4 SVGs, got %d", svgCount)
	}

	ariaHiddenCount := strings.Count(html, `aria-hidden="true"`)
	if ariaHiddenCount != 4 {
		t.Errorf("expected 4 aria-hidden attributes, got %d", ariaHiddenCount)
	}

	focusableCount := strings.Count(html, `focusable="false"`)
	if focusableCount != 4 {
		t.Errorf("expected 4 focusable attributes, got %d", focusableCount)
	}
}

func TestMainTSDoesNotOwnLiveUpdateIconPaths(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod from %s", dir)
		}
		dir = parent
	}
	data, err := os.ReadFile(filepath.Join(dir, "web", "src", "main.ts"))
	if err != nil {
		t.Fatalf("read main.ts: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "liveUpdatesIndicatorSvg") {
		t.Fatal("main.ts must not contain liveUpdatesIndicatorSvg")
	}
	if strings.Contains(content, `"<svg`) || strings.Contains(content, "`<svg") {
		t.Fatal("main.ts must not contain inline SVG string construction for live-update icons")
	}
	// Verify no Lucide live-update path data leaked into TypeScript
	if strings.Contains(content, `"M5 12.55a11`) {
		t.Fatal("main.ts must not contain wifi icon path data")
	}
	if strings.Contains(content, `"M3 2v6h6"`) {
		t.Fatal("main.ts must not contain rotate-ccw icon path data")
	}
	if strings.Contains(content, `"M2 2l20 20"`) {
		t.Fatal("main.ts must not contain wifi-off icon path data")
	}
	if strings.Contains(content, `"M21 12a9`) {
		t.Fatal("main.ts must not contain loader-circle icon path data")
	}
}

func TestAgentRunRunningWithoutOutputShowsNoOutputYet(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:    true,
		OpenCodeExecutionStatus: "running",
		HasOpenCodeRunning:      true,
		OpenCodeLifecycleState:  "running_no_output",
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, `hx-trigger="every 2s"`) {
		t.Errorf("did not expect polling while execution is running without output")
	}
	if !strings.Contains(html, "OpenCode is still inside the startup grace period and has not emitted output yet.") {
		t.Errorf("expected no-output running message, got: %s", html)
	}
	if strings.Contains(html, "Log artifacts") {
		t.Errorf("did not expect log artifacts row before any output exists")
	}
}

func TestAgentRunRunningWithPermissionWarningShowsWarningAndTiming(t *testing.T) {
	var buf strings.Builder
	run := &store.Run{ID: 1, Title: "Test Run", Status: "draft"}
	previews := RunPreviews{
		HasOpenCodeExecution:      true,
		OpenCodeExecutionStatus:   "running",
		HasOpenCodeRunning:        true,
		HasOpenCodeOutput:         true,
		OpenCodeLifecycleState:    "active_output",
		HasOpenCodeStderr:         true,
		HasOpenCodeCombinedLog:    true,
		OpenCodeCommandPreview:    "opencode run",
		OpenCodeExecutionStarted:  "2026-06-11 21:00:00",
		OpenCodeRuntime:           "14s",
		OpenCodeLastOutputAge:     "2s ago",
		OpenCodePermissionWarning: "OpenCode requested a permission that was denied. Review stderr or the combined log.",
	}
	err := AgentRunMonitorStepPanel(run, nil, nil, previews).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render AgentRunMonitorStepPanel: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "OpenCode requested a permission that was denied") {
		t.Errorf("expected permission warning in running monitor, got: %s", html)
	}
	if !strings.Contains(html, "Runtime") || !strings.Contains(html, "14s") {
		t.Errorf("expected runtime metadata, got: %s", html)
	}
	if !strings.Contains(html, "Last output") || !strings.Contains(html, "2s ago") {
		t.Errorf("expected last output timing metadata, got: %s", html)
	}
	if !strings.Contains(html, "download combined log") {
		t.Errorf("expected combined log link when output exists")
	}
}
