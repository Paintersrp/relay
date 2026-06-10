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
	err := NextActionCard(run, previews).Render(context.Background(), &buf)
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
	if workflowStepState("intake", "prompt", previews, artifacts, checks, nil) != "pending" {
		t.Errorf("expected pending for fresh intake")
	}
	artifacts = []store.Artifact{{Kind: "original_handoff"}}
	if workflowStepState("intake", "intake", previews, artifacts, checks, nil) != "active" {
		t.Errorf("expected active for current intake")
	}
	checks = []store.Check{{Kind: "validation", Status: "pass"}}
	if workflowStepState("intake", "prompt", previews, artifacts, checks, nil) != "done" {
		t.Errorf("expected done for validated intake")
	}
}

func TestWorkflowStepStatePromptBlocked(t *testing.T) {
	previews := RunPreviews{}
	artifacts := []store.Artifact{}
	checks := []store.Check{}
	review := &pipeline.IntakeReview{Blockers: []string{"missing goal"}}
	if workflowStepState("prompt", "prompt", previews, artifacts, checks, review) != "blocked" {
		t.Errorf("expected blocked when intake has blockers")
	}
}
