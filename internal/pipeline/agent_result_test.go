package pipeline

import (
	"testing"
)

func TestParseAgentResultDone(t *testing.T) {
	input := "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 42"
	result := ParseAgentResult(input)

	if result.Status != AgentResultDone {
		t.Errorf("expected status DONE, got %s", result.Status)
	}
	if result.BuildStatus != "PASS" {
		t.Errorf("expected build status PASS, got %s", result.BuildStatus)
	}
	if result.TestStatus != "PASS" {
		t.Errorf("expected test status PASS, got %s", result.TestStatus)
	}
	if result.LOCChanged != "42" {
		t.Errorf("expected LOC changed 42, got %s", result.LOCChanged)
	}
	if result.BlockerError != "" {
		t.Errorf("expected empty blocker error, got %s", result.BlockerError)
	}
	if result.Raw != input {
		t.Errorf("raw not preserved")
	}
}

func TestParseAgentResultBlocked(t *testing.T) {
	input := "BLOCKED\nBuild status: FAIL\nTest status: not run\nCount of LOC changed: 12\nBlocker/error only if BLOCKED: migration failed"
	result := ParseAgentResult(input)

	if result.Status != AgentResultBlocked {
		t.Errorf("expected status BLOCKED, got %s", result.Status)
	}
	if result.BuildStatus != "FAIL" {
		t.Errorf("expected build status FAIL, got %s", result.BuildStatus)
	}
	if result.TestStatus != "not run" {
		t.Errorf("expected test status 'not run', got %s", result.TestStatus)
	}
	if result.LOCChanged != "12" {
		t.Errorf("expected LOC changed 12, got %s", result.LOCChanged)
	}
	if result.BlockerError != "migration failed" {
		t.Errorf("expected blocker error 'migration failed', got %s", result.BlockerError)
	}
	if result.Raw != input {
		t.Errorf("raw not preserved")
	}
}

func TestParseAgentResultStatusPrefix(t *testing.T) {
	input := "Status: DONE\nBuild: pass\nTests: pass\nLOC changed: 7"
	result := ParseAgentResult(input)

	if result.Status != AgentResultDone {
		t.Errorf("expected status DONE, got %s", result.Status)
	}
	if result.BuildStatus != "pass" {
		t.Errorf("expected build status 'pass', got %s", result.BuildStatus)
	}
	if result.TestStatus != "pass" {
		t.Errorf("expected test status 'pass', got %s", result.TestStatus)
	}
	if result.LOCChanged != "7" {
		t.Errorf("expected LOC changed 7, got %s", result.LOCChanged)
	}
}

func TestParseAgentResultUnknown(t *testing.T) {
	input := "I changed some files and everything seems fine."
	result := ParseAgentResult(input)

	if result.Status != AgentResultUnknown {
		t.Errorf("expected status UNKNOWN, got %s", result.Status)
	}
	if result.Raw != input {
		t.Errorf("raw not preserved")
	}
}

func TestParseAgentResultNonCanonicalOutput(t *testing.T) {
	input := "DONE\nNo build changes (README-only)\nNo test changes\n1 LOC changed"
	result := ParseAgentResult(input)

	if result.Status != AgentResultDone {
		t.Errorf("expected status DONE, got %s", result.Status)
	}
	if result.BuildStatus != "No build changes (README-only)" {
		t.Errorf("expected build status 'No build changes (README-only)', got %s", result.BuildStatus)
	}
	if result.TestStatus != "No test changes" {
		t.Errorf("expected test status 'No test changes', got %s", result.TestStatus)
	}
	if result.LOCChanged != "1" {
		t.Errorf("expected LOC changed '1', got %s", result.LOCChanged)
	}
}

func TestAgentResultJSON(t *testing.T) {
	r := AgentResult{
		Status:      AgentResultDone,
		BuildStatus: "PASS",
		TestStatus:  "PASS",
		LOCChanged:  "42",
	}
	data, err := r.JSON()
	if err != nil {
		t.Fatalf("JSON() returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON output")
	}
	if data[len(data)-1] != '\n' {
		t.Errorf("expected trailing newline")
	}
}
