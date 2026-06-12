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

func TestParseAgentResultDoneStandaloneAfterPreamble(t *testing.T) {
	input := "final result. There were no concrete blockers found - the entire workflow passed.\n" +
		"Let me provide the final DONE report.\n\n" +
		"DONE\n" +
		"Build status: PASS (go vet ./..., go test ./... both exit 0)\n" +
		"Test status: PASS (go test ./... passed, 24048ms)\n" +
		"Count of LOC changed: 0\n"
	result := ParseAgentResult(input)

	if result.Status != AgentResultDone {
		t.Fatalf("expected status DONE, got %s", result.Status)
	}
	if result.BuildStatus != "PASS (go vet ./..., go test ./... both exit 0)" {
		t.Fatalf("expected build status with details, got %s", result.BuildStatus)
	}
	if result.TestStatus != "PASS (go test ./... passed, 24048ms)" {
		t.Fatalf("expected test status with details, got %s", result.TestStatus)
	}
	if result.LOCChanged != "0" {
		t.Fatalf("expected LOC changed 0, got %s", result.LOCChanged)
	}
}

func TestParseAgentResultBlockedStandaloneAfterPreamble(t *testing.T) {
	input := "I could not safely finish the task.\nHere is the final report.\n\n" +
		"BLOCKED\n" +
		"Build status: PASS\n" +
		"Test status: FAIL\n" +
		"Blocker/error only if blocked: validation command failed\n"
	result := ParseAgentResult(input)

	if result.Status != AgentResultBlocked {
		t.Fatalf("expected status BLOCKED, got %s", result.Status)
	}
	if result.BuildStatus != "PASS" {
		t.Fatalf("expected build status PASS, got %s", result.BuildStatus)
	}
	if result.TestStatus != "FAIL" {
		t.Fatalf("expected test status FAIL, got %s", result.TestStatus)
	}
	if result.BlockerError != "validation command failed" {
		t.Fatalf("expected blocker error to be preserved, got %s", result.BlockerError)
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

func TestParseAgentResultDoesNotTreatProseDoneAsDone(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "mentions not done",
			input: "The task is not done yet.\nBuild status: PASS",
		},
		{
			name:  "done in prose",
			input: "I am done reviewing the files, now continuing implementation.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseAgentResult(tt.input)
			if result.Status != AgentResultUnknown {
				t.Fatalf("expected status UNKNOWN, got %s", result.Status)
			}
		})
	}
}

func TestParseAgentResultDoesNotTreatProseBlockedAsBlocked(t *testing.T) {
	input := "No blockers were found and nothing is blocked.\nTest status: PASS"
	result := ParseAgentResult(input)

	if result.Status != AgentResultUnknown {
		t.Fatalf("expected status UNKNOWN, got %s", result.Status)
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

func TestParseAgentResultLatestStandaloneStatusWins(t *testing.T) {
	input := "Initial notes.\nDONE\nBuild status: PASS\n\nLater report.\nBLOCKED\nTest status: FAIL"
	result := ParseAgentResult(input)

	if result.Status != AgentResultBlocked {
		t.Fatalf("expected latest standalone status BLOCKED, got %s", result.Status)
	}
	if result.BuildStatus != "PASS" {
		t.Fatalf("expected build status PASS, got %s", result.BuildStatus)
	}
	if result.TestStatus != "FAIL" {
		t.Fatalf("expected test status FAIL, got %s", result.TestStatus)
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
