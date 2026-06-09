package instructions

import (
	"strings"
	"testing"
)

func TestHandoffInstructionsNonEmpty(t *testing.T) {
	if HandoffInstructions == "" {
		t.Fatal("HandoffInstructions is empty")
	}
}

func TestHandoffInstructionsContainsRequiredSections(t *testing.T) {
	checks := []string{
		"## Required handoff .txt structure",
		"## Relay Agent Prompt transformation rules",
		"Validation commands are part of the original handoff",
	}
	for _, s := range checks {
		if !strings.Contains(HandoffInstructions, s) {
			t.Errorf("HandoffInstructions missing: %q", s)
		}
	}
}

func TestAgentsMDNonEmpty(t *testing.T) {
	if AgentsMD == "" {
		t.Fatal("AgentsMD is empty")
	}
}

func TestAgentsMDContainsRequiredSections(t *testing.T) {
	checks := []string{
		"Validation responsibility",
		"rtk.exe --version",
		"Count of LOC changed",
	}
	for _, s := range checks {
		if !strings.Contains(AgentsMD, s) {
			t.Errorf("AgentsMD missing: %q", s)
		}
	}
}

func TestClineRulesNonEmpty(t *testing.T) {
	if ClineRules == "" {
		t.Fatal("ClineRules is empty")
	}
}

func TestClineRulesContainsRequiredSections(t *testing.T) {
	checks := []string{
		"Always prefer `rtk.exe`",
		"Do not run validation commands",
		"Completion response format",
	}
	for _, s := range checks {
		if !strings.Contains(ClineRules, s) {
			t.Errorf("ClineRules missing: %q", s)
		}
	}
}
