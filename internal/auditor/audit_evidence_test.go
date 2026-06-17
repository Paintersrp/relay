package auditor

import (
	"encoding/json"
	"testing"
)

func TestParseFileTargetsFlexibleFormats(t *testing.T) {
	// 1. Single string format
	rawSingleStr := json.RawMessage(`"pkg/x.go"`)
	targets, err := parseFileTargets(rawSingleStr)
	if err != nil {
		t.Fatalf("unexpected error parsing single string: %v", err)
	}
	if len(targets) != 1 || targets[0] != "pkg/x.go" {
		t.Errorf("expected [pkg/x.go], got %+v", targets)
	}

	// 2. Array of strings format
	rawStrArr := json.RawMessage(`["pkg/x.go", "pkg/y.go"]`)
	targets, err = parseFileTargets(rawStrArr)
	if err != nil {
		t.Fatalf("unexpected error parsing string array: %v", err)
	}
	if len(targets) != 2 || targets[0] != "pkg/x.go" || targets[1] != "pkg/y.go" {
		t.Errorf("expected [pkg/x.go, pkg/y.go], got %+v", targets)
	}

	// 3. Array of objects format
	rawObjArr := json.RawMessage(`[{"path": "pkg/x.go"}, {"path": "pkg/y.go"}]`)
	targets, err = parseFileTargets(rawObjArr)
	if err != nil {
		t.Fatalf("unexpected error parsing object array: %v", err)
	}
	if len(targets) != 2 || targets[0] != "pkg/x.go" || targets[1] != "pkg/y.go" {
		t.Errorf("expected [pkg/x.go, pkg/y.go], got %+v", targets)
	}

	// 4. Single object format
	rawSingleObj := json.RawMessage(`{"path": "pkg/x.go"}`)
	targets, err = parseFileTargets(rawSingleObj)
	if err != nil {
		t.Fatalf("unexpected error parsing single object: %v", err)
	}
	if len(targets) != 1 || targets[0] != "pkg/x.go" {
		t.Errorf("expected [pkg/x.go], got %+v", targets)
	}
}

func TestGenerateRevisionRequirements(t *testing.T) {
	c := &Collector{}

	ev := &Evidence{
		Warnings: []EvidenceWarning{
			{Message: "Some critical blocker warning", Severity: SeverityBlocker},
			{Message: "Some non-blocking warning", Severity: SeverityWarning},
		},
		ValidationResults: []ValidationCommandResult{
			{ID: "V1", Command: "go test", Required: true, Status: CheckFail, ExitResult: "exit 1"},
			{ID: "V2", Command: "go vet", Required: false, Status: CheckFail, ExitResult: "exit 1"},
		},
	}

	c.generateRevisionRequirements(ev)

	if len(ev.RevisionRequirements) != 2 {
		t.Fatalf("expected 2 revision requirements, got %d: %+v", len(ev.RevisionRequirements), ev.RevisionRequirements)
	}

	hasBlockerWarningReq := false
	hasValidationFailReq := false

	for _, req := range ev.RevisionRequirements {
		if req.Severity == SeverityBlocker {
			hasBlockerWarningReq = true
		}
		if req.Severity == SeverityError {
			hasValidationFailReq = true
		}
	}

	if !hasBlockerWarningReq {
		t.Error("expected a requirement for the blocker warning")
	}
	if !hasValidationFailReq {
		t.Error("expected a requirement for the failed validation command")
	}
}
