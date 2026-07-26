package audits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	workflowpackages "relay/internal/app/packages"
	"relay/internal/executor"
)

func testPackageAuditV3Evidence(t *testing.T, mode executor.EffectiveExecutorBriefMode) (*packageEvidenceFixture, WorkflowPackageExecutionEvidence) {
	t.Helper()
	fixture := buildPackageEvidence(t, mode)
	packages, err := workflowpackages.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := packages.LoadApprovedAuthorityForRun(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := WorkflowPackageExecutionEvidence{
		Run:            fixture.run,
		Authority:      authority,
		Assignment:     fixture.assignment,
		Deterministic:  fixture.outcome,
		EffectiveBrief: fixture.brief,
	}
	return fixture, evidence
}

func testPackageAuditV3DeliveryTicket() WorkflowPackageAuditEmbeddedArtifactV3Input {
	bytes := []byte(`{"ticket_id":"P2-T2","revision_number":1}`)
	return WorkflowPackageAuditEmbeddedArtifactV3Input{
		Filename:  "delivery-ticket.json",
		MediaType: "application/json",
		SHA256:    testPackageAuditV3SHA256(bytes),
		Bytes:     bytes,
	}
}

func testPackageAuditV3Artifacts() []WorkflowPackageAuditEmbeddedArtifactV3Input {
	bytes := []byte(`{"kind":"unified_diff","description":"complete diff"}`)
	return []WorkflowPackageAuditEmbeddedArtifactV3Input{
		{
			Filename:  "diff.json",
			MediaType: "application/json",
			SHA256:    testPackageAuditV3SHA256(bytes),
			Bytes:     bytes,
		},
	}
}

func testPackageAuditV3SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func testPackageAuditV3Commit(baseCommit, auditedCommit string) WorkflowPackageAuditCommitInputV3 {
	return WorkflowPackageAuditCommitInputV3{
		RepoTarget:    "relay",
		Branch:        "main",
		BaseCommit:    baseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles: []WorkflowPackageAuditChangedFileV3{
			{Path: "internal/example.go", ChangeType: "added", Additions: 1, Deletions: 0},
		},
	}
}

func testPackageAuditV3Execution(adaptive bool, status, committedSHA, summary string) WorkflowPackageAuditExecutionInputV3 {
	return WorkflowPackageAuditExecutionInputV3{
		AdaptiveAttemptDispatched: adaptive,
		Status:                    status,
		CommittedSHA:              committedSHA,
		CompletionSummary:         summary,
	}
}

func testPackageAuditV3Input(t *testing.T, mode executor.EffectiveExecutorBriefMode, auditedCommit string) WorkflowPackageAuditPacketV3Input {
	t.Helper()
	_, evidence := testPackageAuditV3Evidence(t, mode)
	adaptive := mode != executor.EffectiveExecutorBriefDeterministicComplete
	return WorkflowPackageAuditPacketV3Input{
		Evidence:            evidence,
		UserIntent:          "Implement the package ticket.",
		DeliveryTicket:      testPackageAuditV3DeliveryTicket(),
		Commit:              testPackageAuditV3Commit(evidence.Run.BaseCommit, auditedCommit),
		Execution:           testPackageAuditV3Execution(adaptive, "completed", auditedCommit, "Implementation completed successfully."),
		RelevantSourcePaths: []string{"internal/example.go"},
		Validation: []WorkflowPackageAuditValidationResultV3{
			{Command: "go test ./internal/example", Expected: "pass", Status: "passed", ConciseResult: "ok"},
		},
		Artifacts: testPackageAuditV3Artifacts(),
	}
}

func TestWorkflowPackageAuditPacketV3AbsentOperations(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, data, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.DeterministicApplication.Outcome != "not_present" {
		t.Fatalf("outcome = %q, want not_present", packet.DeterministicApplication.Outcome)
	}
	if packet.DeterministicApplication.Coverage != nil {
		t.Fatalf("coverage must be nil for not_present")
	}
	if packet.Authority.DeterministicOperations != nil {
		t.Fatalf("deterministic_operations must be nil for not_present")
	}
	if !packet.Execution.AdaptiveAttemptDispatched {
		t.Fatalf("adaptive_attempt_dispatched must be true for not_present")
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("output must end with newline")
	}
}

func setPackageAuditV3PreflightCoverage(t *testing.T, input *WorkflowPackageAuditPacketV3Input, coverage string) {
	t.Helper()
	if input.Evidence.Authority.DeterministicOperations == nil {
		t.Fatal("deterministic operations are absent")
	}
	ops := input.Evidence.Authority.DeterministicOperations
	newBytes := []byte(strings.Replace(string(ops.Bytes), `"coverage":"complete"`, `"coverage":"`+coverage+`"`, 1))
	ops.Bytes = newBytes
	ops.SHA256 = testPackageAuditV3SHA256(newBytes)
	ops.Coverage = coverage
	input.Evidence.Assignment.Assignment.DeterministicOperations.Coverage = coverage
	input.Evidence.Deterministic = packageEvidenceMutatePreflightCoverage(t, input.Evidence.Deterministic, coverage)
}

func TestWorkflowPackageAuditPacketV3PreflightFailedPartial(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed, strings.Repeat("c", 40))
	setPackageAuditV3PreflightCoverage(t, &input, "partial")
	input.Commit.ChangedFiles[0].ChangeType = "deleted"
	input.Commit.ChangedFiles[0].Additions = 0
	packet, data, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.DeterministicApplication.Outcome != "preflight_failed" {
		t.Fatalf("outcome = %q, want preflight_failed", packet.DeterministicApplication.Outcome)
	}
	if packet.DeterministicApplication.Coverage == nil || *packet.DeterministicApplication.Coverage != "partial" {
		t.Fatalf("coverage must be partial")
	}
	if packet.Authority.DeterministicOperations == nil {
		t.Fatalf("deterministic_operations must be present")
	}
	if !packet.Execution.AdaptiveAttemptDispatched {
		t.Fatalf("adaptive_attempt_dispatched must be true for preflight_failed")
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("output must end with newline")
	}
}

func TestWorkflowPackageAuditPacketV3PreflightFailedComplete(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed)
	outcome := packageEvidenceMutatePreflightCoverage(t, fixture.outcome, "complete")
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed, strings.Repeat("c", 40))
	input.Evidence.Deterministic = outcome
	input.Evidence.EffectiveBrief = fixture.brief
	input.Evidence.Assignment = fixture.assignment
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.DeterministicApplication.Outcome != "preflight_failed" {
		t.Fatalf("outcome = %q, want preflight_failed", packet.DeterministicApplication.Outcome)
	}
	if packet.DeterministicApplication.Coverage == nil || *packet.DeterministicApplication.Coverage != "complete" {
		t.Fatalf("coverage must be complete")
	}
}

func TestWorkflowPackageAuditPacketV3AppliedPartial(t *testing.T) {
	_, evidence := testPackageAuditV3Evidence(t, executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication)
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication, strings.Repeat("c", 40))
	input.Evidence = evidence
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.DeterministicApplication.Outcome != "applied" {
		t.Fatalf("outcome = %q, want applied", packet.DeterministicApplication.Outcome)
	}
	if packet.DeterministicApplication.Coverage == nil || *packet.DeterministicApplication.Coverage != "partial" {
		t.Fatalf("coverage must be partial")
	}
	if !packet.Execution.AdaptiveAttemptDispatched {
		t.Fatalf("adaptive_attempt_dispatched must be true for applied partial")
	}
}

func TestWorkflowPackageAuditPacketV3AppliedComplete(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.DeterministicApplication.Outcome != "applied" {
		t.Fatalf("outcome = %q, want applied", packet.DeterministicApplication.Outcome)
	}
	if packet.DeterministicApplication.Coverage == nil || *packet.DeterministicApplication.Coverage != "complete" {
		t.Fatalf("coverage must be complete")
	}
	if packet.Execution.AdaptiveAttemptDispatched {
		t.Fatalf("adaptive_attempt_dispatched must be false for applied complete")
	}
}

func TestWorkflowPackageAuditPacketV3AllModesMapAdaptiveDispatch(t *testing.T) {
	tests := []struct {
		mode     executor.EffectiveExecutorBriefMode
		adaptive bool
	}{
		{executor.EffectiveExecutorBriefAdaptiveNoOperations, true},
		{executor.EffectiveExecutorBriefAdaptivePreflightFailed, true},
		{executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication, true},
		{executor.EffectiveExecutorBriefDeterministicComplete, false},
	}
	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			input := testPackageAuditV3Input(t, test.mode, strings.Repeat("c", 40))
			packet, _, err := buildWorkflowPackageAuditPacketV3(input)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if packet.Execution.AdaptiveAttemptDispatched != test.adaptive {
				t.Fatalf("adaptive_attempt_dispatched = %v, want %v", packet.Execution.AdaptiveAttemptDispatched, test.adaptive)
			}
		})
	}
}

func TestWorkflowPackageAuditPacketV3DeterministicCompleteRequiresNoAttempt(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Execution.AdaptiveAttemptDispatched {
		t.Fatalf("deterministic complete must not require adaptive attempt dispatch")
	}
}

func TestWorkflowPackageAuditPacketV3DeliveryTicketEmbeddedAsJSON(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Authority.DeliveryTicket.Filename != "delivery-ticket.json" {
		t.Fatalf("filename = %q", packet.Authority.DeliveryTicket.Filename)
	}
	var value map[string]any
	if err := json.Unmarshal(packet.Authority.DeliveryTicket.Content, &value); err != nil {
		t.Fatalf("delivery ticket content is not JSON: %v", err)
	}
	if value["ticket_id"] != "P2-T2" {
		t.Fatalf("ticket_id = %v", value["ticket_id"])
	}
	if packet.Authority.DeliveryTicket.SHA256 != testPackageAuditV3SHA256(input.DeliveryTicket.Bytes) {
		t.Fatalf("delivery ticket SHA-256 must match original bytes")
	}
}

func TestWorkflowPackageAuditPacketV3RequirementsInAuthoritySequence(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(packet.Authority.Requirements) == 0 {
		t.Fatalf("requirements must be present")
	}
	for index, req := range packet.Authority.Requirements {
		want := input.Evidence.Authority.AuthorityLayers[index]
		if req.Filename != filepath.Base(want.RelativePath) {
			t.Fatalf("requirements[%d].filename = %q, want %q", index, req.Filename, filepath.Base(want.RelativePath))
		}
		if req.SHA256 != want.SHA256 {
			t.Fatalf("requirements[%d].sha256 mismatch", index)
		}
		var decoded string
		if err := json.Unmarshal(req.Content, &decoded); err != nil {
			t.Fatalf("requirements[%d].content is not decodable: %v", index, err)
		}
		if decoded != string(want.Bytes) {
			t.Fatalf("requirements[%d].content mismatch", index)
		}
	}
}

func TestWorkflowPackageAuditPacketV3SharedDesignInAuthoritySequence(t *testing.T) {
	_, evidence := testPackageAuditV3Evidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	// Add a shared design layer to test sequence.
	sharedBytes := []byte("{\"shared\":true}")
	sharedLayer := workflowpackages.ApprovedAuthorityLayer{
		Kind:         "shared_design",
		Sequence:     2,
		RelativePath: "plans/checkout/shared-design.json",
		MediaType:    "application/json",
		SHA256:       testPackageAuditV3SHA256(sharedBytes),
		Bytes:        sharedBytes,
	}
	// Insert the shared design layer; fixture already has one requirements layer.
	authority := evidence.Authority
	authority.AuthorityLayers = append(authority.AuthorityLayers, sharedLayer)
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.Authority = authority
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(packet.Authority.SharedDesign) != 1 {
		t.Fatalf("shared design count = %d, want 1", len(packet.Authority.SharedDesign))
	}
	if packet.Authority.SharedDesign[0].Filename != "shared-design.json" {
		t.Fatalf("shared design filename = %q", packet.Authority.SharedDesign[0].Filename)
	}
	if packet.Authority.SharedDesign[0].SHA256 != sharedLayer.SHA256 {
		t.Fatalf("shared design SHA-256 mismatch")
	}
}

func TestWorkflowPackageAuditPacketV3BriefMarkdownPreserved(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	brief := packet.Authority.TicketDesignBrief
	want := input.Evidence.Authority.TicketDesignBrief
	if brief.Filename != filepath.Base(want.RelativePath) {
		t.Fatalf("filename = %q, want %q", brief.Filename, filepath.Base(want.RelativePath))
	}
	if brief.SHA256 != want.SHA256 {
		t.Fatalf("SHA-256 mismatch")
	}
	// Content is stored as a JSON string; unwrapping it should equal the original bytes.
	var text string
	if err := json.Unmarshal(brief.Content, &text); err != nil {
		t.Fatalf("brief content is not a JSON string: %v", err)
	}
	if []byte(text) != nil && text != string(want.Bytes) {
		t.Fatalf("brief content not preserved")
	}
	if string(want.Bytes) == "" && text != "" {
		t.Fatalf("brief content not preserved")
	}
	if string(want.Bytes) != "" && text != string(want.Bytes) {
		t.Fatalf("brief content not preserved")
	}
}

func TestWorkflowPackageAuditPacketV3OperationsOmittedWhenAbsent(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Authority.DeterministicOperations != nil {
		t.Fatalf("deterministic_operations must be omitted when absent")
	}
}

func TestWorkflowPackageAuditPacketV3OperationsEmbeddedWhenPresent(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Authority.DeterministicOperations == nil {
		t.Fatalf("deterministic_operations must be present")
	}
	want := input.Evidence.Authority.DeterministicOperations
	if packet.Authority.DeterministicOperations.SHA256 != want.SHA256 {
		t.Fatalf("SHA-256 mismatch")
	}
	var document struct {
		Coverage string `json:"coverage"`
	}
	if err := json.Unmarshal(packet.Authority.DeterministicOperations.Content, &document); err != nil {
		t.Fatalf("operations content is not JSON: %v", err)
	}
	if document.Coverage != "complete" {
		t.Fatalf("coverage = %q, want complete", document.Coverage)
	}
}

func TestWorkflowPackageAuditPacketV3RuntimeReferencesExact(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Authority.ExecutionAssignment.ArtifactReference != input.Evidence.Assignment.Artifact.ArtifactID {
		t.Fatalf("execution assignment reference mismatch")
	}
	if packet.Authority.ExecutionAssignment.SHA256 != input.Evidence.Assignment.Artifact.SHA256 {
		t.Fatalf("execution assignment SHA-256 mismatch")
	}
	if packet.Authority.EffectiveExecutorBrief.ArtifactReference != input.Evidence.EffectiveBrief.Artifact.ArtifactID {
		t.Fatalf("effective brief reference mismatch")
	}
	if packet.Authority.EffectiveExecutorBrief.SHA256 != input.Evidence.EffectiveBrief.Artifact.SHA256 {
		t.Fatalf("effective brief SHA-256 mismatch")
	}
	if packet.DeterministicApplication.Evidence.ArtifactReference != input.Evidence.Deterministic.Artifact.ArtifactID {
		t.Fatalf("deterministic outcome reference mismatch")
	}
	if packet.DeterministicApplication.Evidence.SHA256 != input.Evidence.Deterministic.Artifact.SHA256 {
		t.Fatalf("deterministic outcome SHA-256 mismatch")
	}
}

func TestWorkflowPackageAuditPacketV3ArtifactBytesDetermineDigest(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for index, artifact := range packet.Artifacts {
		want := testPackageAuditV3SHA256(input.Artifacts[index].Bytes)
		if artifact.SHA256 != want {
			t.Fatalf("artifact[%d].sha256 mismatch", index)
		}
	}
}

func TestWorkflowPackageAuditPacketV3ChangedFileFieldsPreserved(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles = []WorkflowPackageAuditChangedFileV3{
		{Path: "a/b.go", PreviousPath: "old/b.go", ChangeType: "renamed", Additions: 3, Deletions: 1},
		{Path: "c/d.go", ChangeType: "added", Additions: 5, Deletions: 0},
	}
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(packet.ChangedFiles) != 2 {
		t.Fatalf("changed files count = %d", len(packet.ChangedFiles))
	}
	if packet.ChangedFiles[0].Path != "a/b.go" || packet.ChangedFiles[0].PreviousPath != "old/b.go" || packet.ChangedFiles[0].ChangeType != "renamed" || packet.ChangedFiles[0].Additions != 3 || packet.ChangedFiles[0].Deletions != 1 {
		t.Fatalf("changed file 0 fields not preserved")
	}
	if packet.ChangedFiles[1].Path != "c/d.go" || packet.ChangedFiles[1].ChangeType != "added" || packet.ChangedFiles[1].Additions != 5 || packet.ChangedFiles[1].Deletions != 0 {
		t.Fatalf("changed file 1 fields not preserved")
	}
}

func TestWorkflowPackageAuditPacketV3ValidationOrderPreserved(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Validation = []WorkflowPackageAuditValidationResultV3{
		{Command: "first", Expected: "pass", Status: "passed", ConciseResult: "ok"},
		{Command: "second", Expected: "fail", Status: "failed", ConciseResult: "not ok"},
		{Command: "third", Expected: "skip", Status: "not_run", ConciseResult: "skipped"},
	}
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(packet.Validation) != 3 {
		t.Fatalf("validation count = %d", len(packet.Validation))
	}
	for index, want := range input.Validation {
		if packet.Validation[index] != want {
			t.Fatalf("validation[%d] not preserved", index)
		}
	}
}

func TestWorkflowPackageAuditPacketV3ArtifactOrderPreserved(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Artifacts = []WorkflowPackageAuditEmbeddedArtifactV3Input{
		{Filename: "first.json", MediaType: "application/json", SHA256: testPackageAuditV3SHA256([]byte(`{"first":true}`)), Bytes: []byte(`{"first":true}`)},
		{Filename: "second.json", MediaType: "application/json", SHA256: testPackageAuditV3SHA256([]byte(`{"second":true}`)), Bytes: []byte(`{"second":true}`)},
	}
	packet, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(packet.Artifacts) != 2 {
		t.Fatalf("artifact count = %d", len(packet.Artifacts))
	}
	if packet.Artifacts[0].Filename != "first.json" || packet.Artifacts[1].Filename != "second.json" {
		t.Fatalf("artifact order not preserved")
	}
}

func TestWorkflowPackageAuditPacketV3OutputTrailingNewline(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("output must end with newline")
	}
	if len(data) > 1 && data[len(data)-2] == '\n' {
		t.Fatalf("output must have exactly one trailing newline")
	}
}

func TestWorkflowPackageAuditPacketV3RepeatedBuildsIdentical(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, first, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, second, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("repeated builds produced different bytes")
	}
}

func TestWorkflowPackageAuditPacketV3DecodedRemarshalIsCanonical(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var packet WorkflowPackageAuditPacketV3
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatalf("decode: %v", err)
	}
	remarshaled, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	remarshaled = append(remarshaled, '\n')
	if string(data) != string(remarshaled) {
		t.Fatalf("re-marshaled bytes differ from canonical")
	}
}

func TestWorkflowPackageAuditPacketV3MalformedDeliveryTicketRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.DeliveryTicket.Bytes = []byte("not json")
	input.DeliveryTicket.SHA256 = testPackageAuditV3SHA256(input.DeliveryTicket.Bytes)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3UnknownAuthorityLayerRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	badLayer := workflowpackages.ApprovedAuthorityLayer{
		Kind:         "unknown_kind",
		Sequence:     2,
		RelativePath: "plans/checkout/bad.json",
		MediaType:    "application/json",
		SHA256:       testPackageAuditV3SHA256([]byte(`{}`)),
		Bytes:        []byte(`{}`),
	}
	input.Evidence.Authority.AuthorityLayers = append(input.Evidence.Authority.AuthorityLayers, badLayer)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3MalformedOrMismatchedDigestRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.DeliveryTicket.SHA256 = strings.Repeat("0", 64)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3UnsafeFilenameOrPathRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.DeliveryTicket.Filename = "../escape.json"
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for unsafe filename, got %v", err)
	}

	input = testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles[0].Path = "/absolute/path.go"
	_, _, err = buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for unsafe path, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3DuplicatePathOrArtifactFilenameRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles = []WorkflowPackageAuditChangedFileV3{
		{Path: "same.go", ChangeType: "added", Additions: 1, Deletions: 0},
		{Path: "same.go", ChangeType: "modified", Additions: 2, Deletions: 1},
	}
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for duplicate path, got %v", err)
	}

	input = testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Artifacts = append(input.Artifacts, input.Artifacts[0])
	_, _, err = buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for duplicate artifact filename, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3InvalidOutcomeCoverageRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	// Force outcome coverage to conflict with absent operations.
	input.Evidence.Deterministic.Outcome.Outcome.Coverage = "complete"
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for invalid outcome coverage, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3ModeDispatchDisagreementRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Execution.AdaptiveAttemptDispatched = false
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for mode/dispatch disagreement, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3CommittedAuditedShaDisagreementRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Execution.AdaptiveAttemptDispatched = true
	input.Execution.CommittedSHA = strings.Repeat("d", 40)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error for SHA disagreement, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3EmptyRequiredCollectionsRejected(t *testing.T) {
	cases := []func(*WorkflowPackageAuditPacketV3Input){
		func(i *WorkflowPackageAuditPacketV3Input) { i.Commit.ChangedFiles = nil },
		func(i *WorkflowPackageAuditPacketV3Input) { i.RelevantSourcePaths = nil },
		func(i *WorkflowPackageAuditPacketV3Input) { i.Validation = nil },
		func(i *WorkflowPackageAuditPacketV3Input) { i.Artifacts = nil },
	}
	for index, mutate := range cases {
		input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
		mutate(&input)
		_, _, err := buildWorkflowPackageAuditPacketV3(input)
		if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
			t.Fatalf("case %d: expected invalid error, got %v", index, err)
		}
	}
}

func TestWorkflowPackageAuditPacketV3NoLegacyKeys(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	forbidden := []string{"\"execution_spec\"", "\"executor_brief\"", "\"applier\"", "\"residual\"", "\"selected_pass\"", "\"managed_context\""}
	for _, key := range forbidden {
		if strings.Contains(string(data), key) {
			t.Fatalf("output contains forbidden key %s", key)
		}
	}
}

func TestWorkflowPackageAuditPacketV3NoRequirementsLayerRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	// Replace the only requirements layer with shared_design.
	for index := range input.Evidence.Authority.AuthorityLayers {
		input.Evidence.Authority.AuthorityLayers[index].Kind = "shared_design"
	}
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3AuthorityLayerSequenceRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	// Append a duplicate sequence layer.
	duplicate := input.Evidence.Authority.AuthorityLayers[0]
	input.Evidence.Authority.AuthorityLayers = append(input.Evidence.Authority.AuthorityLayers, duplicate)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3TextArtifactUtf8Rejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Artifacts[0].MediaType = "text/plain"
	input.Artifacts[0].Bytes = []byte{0xff}
	input.Artifacts[0].SHA256 = testPackageAuditV3SHA256(input.Artifacts[0].Bytes)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3InvalidChangedFileTypeRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles[0].ChangeType = "unknown"
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3ValidationStatusRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Validation[0].Status = "unknown"
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3RepeatedBuildsPacketObjectEqual(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	first, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, _, err := buildWorkflowPackageAuditPacketV3(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("packet objects differ")
	}
}

func TestWorkflowPackageAuditPacketV3InvalidAuditedCommitRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, "not-a-sha")
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3InvalidBaseCommitRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.BaseCommit = strings.Repeat("G", 40)
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3InvalidEffectiveBriefSHARejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.EffectiveBrief.Artifact.SHA256 = "bad"
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketV3InvalidUserIntentRejected(t *testing.T) {
	input := testPackageAuditV3Input(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.UserIntent = "   "
	_, _, err := buildWorkflowPackageAuditPacketV3(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketV3Invalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}
