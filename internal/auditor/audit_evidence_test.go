package auditor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseFileTargetsFlexibleFormats(t *testing.T) {
	// 1. Single string format
	rawSingleStr := json.RawMessage(`"pkg/x.go"`)
	targets, warnings := parseFileTargets(rawSingleStr)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(targets) != 1 || targets[0] != "pkg/x.go" {
		t.Errorf("expected [pkg/x.go], got %+v", targets)
	}

	// 2. Array of strings format
	rawStrArr := json.RawMessage(`["pkg/x.go", "pkg/y.go"]`)
	targets, warnings = parseFileTargets(rawStrArr)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(targets) != 2 || targets[0] != "pkg/x.go" || targets[1] != "pkg/y.go" {
		t.Errorf("expected [pkg/x.go, pkg/y.go], got %+v", targets)
	}

	// 3. Array of objects format (schema-valid canonical form)
	rawObjArr := json.RawMessage(`[{"path": "pkg/x.go"}, {"path": "pkg/y.go"}]`)
	targets, warnings = parseFileTargets(rawObjArr)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(targets) != 2 || targets[0] != "pkg/x.go" || targets[1] != "pkg/y.go" {
		t.Errorf("expected [pkg/x.go, pkg/y.go], got %+v", targets)
	}

	// 4. Single object format
	rawSingleObj := json.RawMessage(`{"path": "pkg/x.go"}`)
	targets, warnings = parseFileTargets(rawSingleObj)
	if len(warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if len(targets) != 1 || targets[0] != "pkg/x.go" {
		t.Errorf("expected [pkg/x.go], got %+v", targets)
	}

	// 5. Array of objects without path field — should warn and produce no targets
	rawObjNoPath := json.RawMessage(`[{"role": "docs"}]`)
	targets, warnings = parseFileTargets(rawObjNoPath)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for object without path, got %d", len(targets))
	}
	if len(warnings) == 0 {
		t.Error("expected warning for object without path field")
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
			{ID: "V3", Command: "go build", Required: true, Status: CheckUnknown, ExitResult: "not_run", EvidenceSummary: "No output artifact found"},
		},
	}

	c.generateRevisionRequirements(ev)

	if len(ev.RevisionRequirements) != 3 {
		t.Fatalf("expected 3 revision requirements, got %d: %+v", len(ev.RevisionRequirements), ev.RevisionRequirements)
	}

	hasBlockerWarningReq := false
	hasValidationFailReq := false
	hasUnknownReq := false

	for _, req := range ev.RevisionRequirements {
		if req.Severity == SeverityBlocker {
			hasBlockerWarningReq = true
		}
		if req.Severity == SeverityError && strings.Contains(req.Reason, "go test") {
			hasValidationFailReq = true
		}
		if req.Severity == SeverityError && strings.Contains(req.Reason, "go build") {
			hasUnknownReq = true
		}
	}

	if !hasBlockerWarningReq {
		t.Error("expected a requirement for the blocker warning")
	}
	if !hasValidationFailReq {
		t.Error("expected a requirement for the failed validation command")
	}
	if !hasUnknownReq {
		t.Error("expected a requirement for the missing required validation command (unknown)")
	}
}
