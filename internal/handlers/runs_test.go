package handlers

import (
	"testing"

	"relay/internal/store"
)

func TestDefaultActiveRunStep_StartsAtIntakeAfterAutoSetup(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
	}
	checks := []store.Check{
		{Kind: "validation", Status: "pass"},
	}

	got := defaultActiveRunStep(artifacts, checks)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeForFreshRun(t *testing.T) {
	got := defaultActiveRunStep(nil, nil)
	if got != "intake" {
		t.Fatalf("expected intake for fresh run, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeForArtifactsOnly(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
	}
	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_MovesToValidationAfterAgentResult(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
		{Kind: "agent_result_raw"},
	}

	got := defaultActiveRunStep(artifacts, nil)
	if got != "validation" {
		t.Fatalf("expected validation, got %q", got)
	}
}

func TestDefaultActiveRunStep_MovesToValidationAfterValidationRun(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
		{Kind: "validation_run_json"},
	}

	got := defaultActiveRunStep(artifacts, nil)
	if got != "validation" {
		t.Fatalf("expected validation, got %q", got)
	}
}

func TestDefaultActiveRunStep_MovesToValidationAfterValidationRunCheck(t *testing.T) {
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}

	got := defaultActiveRunStep(nil, checks)
	if got != "validation" {
		t.Fatalf("expected validation, got %q", got)
	}
}

func TestHasArtifactKind_ReturnsTrueWhenFound(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "agent_prompt"},
	}
	if !hasArtifactKind(artifacts, "agent_prompt") {
		t.Error("expected true for existing artifact kind")
	}
}

func TestHasArtifactKind_ReturnsFalseWhenNotFound(t *testing.T) {
	if hasArtifactKind(nil, "agent_prompt") {
		t.Error("expected false for nil slice")
	}
}

func TestHasCheckKind_ReturnsTrueWhenFound(t *testing.T) {
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}
	if !hasCheckKind(checks, "validation_run") {
		t.Error("expected true for existing check kind")
	}
}

func TestHasCheckKind_ReturnsFalseWhenNotFound(t *testing.T) {
	if hasCheckKind(nil, "validation_run") {
		t.Error("expected false for nil slice")
	}
}