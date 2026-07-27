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

func testPackageAuditEvidence(t *testing.T, mode executor.EffectiveExecutorBriefMode) (*packageEvidenceFixture, WorkflowPackageExecutionEvidence) {
	t.Helper()
	fixture := buildPackageEvidence(t, mode)
	loader, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := loader.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, evidence
}

func testPackageAuditDeliveryTicket() WorkflowPackageAuditEmbeddedArtifactInput {
	bytes := []byte(`{"ticket_id":"P2-T2","revision_number":1}`)
	return WorkflowPackageAuditEmbeddedArtifactInput{
		Filename:  "delivery-ticket.json",
		MediaType: "application/json",
		SHA256:    testPackageAuditSHA256(bytes),
		Bytes:     bytes,
	}
}

func testPackageAuditArtifacts() []WorkflowPackageAuditEmbeddedArtifactInput {
	bytes := []byte(`{"kind":"unified_diff","description":"complete diff"}`)
	return []WorkflowPackageAuditEmbeddedArtifactInput{
		{
			Filename:  "diff.json",
			MediaType: "application/json",
			SHA256:    testPackageAuditSHA256(bytes),
			Bytes:     bytes,
		},
	}
}

func testPackageAuditSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func testPackageAuditCommit(baseCommit, auditedCommit string) WorkflowPackageAuditCommitInput {
	return WorkflowPackageAuditCommitInput{
		RepoTarget:    "relay",
		Branch:        "main",
		BaseCommit:    baseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles: []WorkflowPackageAuditChangedFile{
			{Path: "internal/example.go", ChangeType: "added", Additions: 1, Deletions: 0},
		},
	}
}

func testPackageAuditExecution(status, committedSHA, summary string) WorkflowPackageAuditExecutionInput {
	return WorkflowPackageAuditExecutionInput{
		Status:            status,
		CommittedSHA:      committedSHA,
		CompletionSummary: summary,
	}
}

func testPackageAuditFixRequirementsJSON(evidence *WorkflowPackageExecutionEvidence) {
	for i := range evidence.Authority.AuthorityLayers {
		if evidence.Authority.AuthorityLayers[i].Kind == "requirements" {
			jsonBytes := []byte(`{"requirement":true}`)
			evidence.Authority.AuthorityLayers[i].Bytes = jsonBytes
			evidence.Authority.AuthorityLayers[i].SHA256 = testPackageAuditSHA256(jsonBytes)
			evidence.Authority.AuthorityLayers[i].MediaType = "application/json"
		}
	}
}

func testPackageAuditInput(t *testing.T, mode executor.EffectiveExecutorBriefMode, auditedCommit string) WorkflowPackageAuditPacketInput {
	t.Helper()
	_, evidence := testPackageAuditEvidence(t, mode)
	// The package evidence fixture stores the requirements layer as opaque text,
	// but the packet requires JSON-authored requirements. Replace it with
	// valid JSON bytes and a matching digest so the builder and decoded
	// validators agree on the content type.
	testPackageAuditFixRequirementsJSON(&evidence)
	return WorkflowPackageAuditPacketInput{
		Evidence:            evidence,
		UserIntent:          "Implement the package ticket.",
		DeliveryTicket:      testPackageAuditDeliveryTicket(),
		Commit:              testPackageAuditCommit(evidence.Run.BaseCommit, auditedCommit),
		Execution:           testPackageAuditExecution("completed", auditedCommit, "Implementation completed successfully."),
		RelevantSourcePaths: []string{"internal/example.go"},
		Artifacts:           testPackageAuditArtifacts(),
	}
}

func TestWorkflowPackageAuditPacketAbsentOperations(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, data, err := buildWorkflowPackageAuditPacket(input)
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

func setPackageAuditPreflightCoverage(t *testing.T, input *WorkflowPackageAuditPacketInput, coverage string) {
	t.Helper()
	if input.Evidence.Authority.DeterministicOperations == nil {
		t.Fatal("deterministic operations are absent")
	}
	ops := input.Evidence.Authority.DeterministicOperations
	newBytes := []byte(strings.Replace(string(ops.Bytes), `"coverage":"complete"`, `"coverage":"`+coverage+`"`, 1))
	ops.Bytes = newBytes
	ops.SHA256 = testPackageAuditSHA256(newBytes)
	ops.Coverage = coverage
	input.Evidence.Assignment.Assignment.DeterministicOperations.Coverage = coverage
	input.Evidence.Deterministic = packageEvidenceMutatePreflightCoverage(t, input.Evidence.Deterministic, coverage)
}

func TestWorkflowPackageAuditPacketPreflightFailedPartial(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed, strings.Repeat("c", 40))
	setPackageAuditPreflightCoverage(t, &input, "partial")
	input.Commit.ChangedFiles[0].ChangeType = "deleted"
	input.Commit.ChangedFiles[0].Additions = 0
	packet, data, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketPreflightFailedComplete(t *testing.T) {
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed)
	outcome := packageEvidenceMutatePreflightCoverage(t, fixture.outcome, "complete")
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptivePreflightFailed, strings.Repeat("c", 40))
	input.Evidence.Deterministic = outcome
	input.Evidence.EffectiveBrief = fixture.brief
	input.Evidence.Assignment = fixture.assignment
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketAppliedPartial(t *testing.T) {
	_, evidence := testPackageAuditEvidence(t, executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication)
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication, strings.Repeat("c", 40))
	input.Evidence = evidence
	testPackageAuditFixRequirementsJSON(&input.Evidence)
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketAppliedComplete(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketAllModesMapAdaptiveDispatch(t *testing.T) {
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
			input := testPackageAuditInput(t, test.mode, strings.Repeat("c", 40))
			packet, _, err := buildWorkflowPackageAuditPacket(input)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if packet.Execution.AdaptiveAttemptDispatched != test.adaptive {
				t.Fatalf("adaptive_attempt_dispatched = %v, want %v", packet.Execution.AdaptiveAttemptDispatched, test.adaptive)
			}
		})
	}
}

func TestWorkflowPackageAuditPacketDeterministicCompleteRequiresNoAttempt(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Execution.AdaptiveAttemptDispatched {
		t.Fatalf("deterministic complete must not require adaptive attempt dispatch")
	}
}

func TestWorkflowPackageAuditPacketDeliveryTicketEmbeddedAsJSON(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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
	if packet.Authority.DeliveryTicket.SHA256 != testPackageAuditSHA256(input.DeliveryTicket.Bytes) {
		t.Fatalf("delivery ticket SHA-256 must match original bytes")
	}
}

func TestWorkflowPackageAuditPacketRequirementsInAuthoritySequence(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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
		var decoded map[string]any
		if err := json.Unmarshal(req.Content, &decoded); err != nil {
			t.Fatalf("requirements[%d].content is not JSON: %v", index, err)
		}
		if decoded["requirement"] != true {
			t.Fatalf("requirements[%d].content mismatch", index)
		}
	}
}

func TestWorkflowPackageAuditPacketSharedDesignInAuthoritySequence(t *testing.T) {
	_, evidence := testPackageAuditEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	// Add a shared design layer to test sequence.
	sharedBytes := []byte("{\"shared\":true}")
	sharedLayer := workflowpackages.ApprovedAuthorityLayer{
		Kind:         "shared_design",
		Sequence:     2,
		RelativePath: "plans/checkout/shared-design.json",
		MediaType:    "application/json",
		SHA256:       testPackageAuditSHA256(sharedBytes),
		Bytes:        sharedBytes,
	}
	// Insert the shared design layer; fixture already has one requirements layer.
	authority := evidence.Authority
	authority.AuthorityLayers = append(authority.AuthorityLayers, sharedLayer)
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.Authority = authority
	testPackageAuditFixRequirementsJSON(&input.Evidence)
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketBriefMarkdownPreserved(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketOperationsOmittedWhenAbsent(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Authority.DeterministicOperations != nil {
		t.Fatalf("deterministic_operations must be omitted when absent")
	}
}

func TestWorkflowPackageAuditPacketOperationsEmbeddedWhenPresent(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketRuntimeReferencesExact(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketArtifactBytesDetermineDigest(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for index, artifact := range packet.Artifacts {
		want := testPackageAuditSHA256(input.Artifacts[index].Bytes)
		if artifact.SHA256 != want {
			t.Fatalf("artifact[%d].sha256 mismatch", index)
		}
	}
}

func TestWorkflowPackageAuditPacketChangedFileFieldsPreserved(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles = []WorkflowPackageAuditChangedFile{
		{Path: "a/b.go", PreviousPath: "old/b.go", ChangeType: "renamed", Additions: 3, Deletions: 1},
		{Path: "c/d.go", ChangeType: "added", Additions: 5, Deletions: 0},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketValidationOrderPreserved(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.Assignment.Assignment.ValidationCommands = []executor.ExecutionAssignmentValidationCommand{
		{Command: "first", Expected: "pass"},
		{Command: "second", Expected: "fail"},
		{Command: "third", Expected: "skip"},
	}
	input.Evidence.Validation = []WorkflowPackageAuditValidationResult{
		{Command: "first", Expected: "pass", Status: "passed", ConciseResult: "ok"},
		{Command: "second", Expected: "fail", Status: "failed", ConciseResult: "not ok"},
		{Command: "third", Expected: "skip", Status: "not_run", ConciseResult: "skipped"},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(packet.Validation) != 3 {
		t.Fatalf("validation count = %d", len(packet.Validation))
	}
	for index, want := range input.Evidence.Validation {
		if packet.Validation[index] != want {
			t.Fatalf("validation[%d] not preserved", index)
		}
	}
}

func TestWorkflowPackageAuditPacketArtifactOrderPreserved(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Artifacts = []WorkflowPackageAuditEmbeddedArtifactInput{
		{Filename: "first.json", MediaType: "application/json", SHA256: testPackageAuditSHA256([]byte(`{"first":true}`)), Bytes: []byte(`{"first":true}`)},
		{Filename: "second.json", MediaType: "application/json", SHA256: testPackageAuditSHA256([]byte(`{"second":true}`)), Bytes: []byte(`{"second":true}`)},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketOutputTrailingNewline(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketRepeatedBuildsIdentical(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, first, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	_, second, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("repeated builds produced different bytes")
	}
}

func TestWorkflowPackageAuditPacketDecodedRemarshalIsCanonical(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var packet WorkflowPackageAuditPacket
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

func TestWorkflowPackageAuditPacketMalformedDeliveryTicketRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.DeliveryTicket.Bytes = []byte("not json")
	input.DeliveryTicket.SHA256 = testPackageAuditSHA256(input.DeliveryTicket.Bytes)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketUnknownAuthorityLayerRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	badLayer := workflowpackages.ApprovedAuthorityLayer{
		Kind:         "unknown_kind",
		Sequence:     2,
		RelativePath: "plans/checkout/bad.json",
		MediaType:    "application/json",
		SHA256:       testPackageAuditSHA256([]byte(`{}`)),
		Bytes:        []byte(`{}`),
	}
	input.Evidence.Authority.AuthorityLayers = append(input.Evidence.Authority.AuthorityLayers, badLayer)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketMalformedOrMismatchedDigestRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.DeliveryTicket.SHA256 = strings.Repeat("0", 64)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketUnsafeFilenameOrPathRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.DeliveryTicket.Filename = "../escape.json"
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for unsafe filename, got %v", err)
	}

	input = testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles[0].Path = "/absolute/path.go"
	_, _, err = buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for unsafe path, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketDuplicatePathOrArtifactFilenameRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles = []WorkflowPackageAuditChangedFile{
		{Path: "same.go", ChangeType: "added", Additions: 1, Deletions: 0},
		{Path: "same.go", ChangeType: "modified", Additions: 2, Deletions: 1},
	}
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for duplicate path, got %v", err)
	}

	input = testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Artifacts = append(input.Artifacts, input.Artifacts[0])
	_, _, err = buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for duplicate artifact filename, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketInvalidOutcomeCoverageRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	// Force outcome coverage to conflict with absent operations.
	input.Evidence.Deterministic.Outcome.Outcome.Coverage = "complete"
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for invalid outcome coverage, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketModeDispatchDisagreementRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.Attempt = nil
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for mode/dispatch disagreement, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketCommittedAuditedShaDisagreementRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Execution.CommittedSHA = strings.Repeat("d", 40)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for SHA disagreement, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketEmptyRequiredCollectionsRejected(t *testing.T) {
	cases := []func(*WorkflowPackageAuditPacketInput){
		func(i *WorkflowPackageAuditPacketInput) { i.Commit.ChangedFiles = nil },
		func(i *WorkflowPackageAuditPacketInput) { i.RelevantSourcePaths = nil },
		func(i *WorkflowPackageAuditPacketInput) {
			i.Evidence.Validation = []WorkflowPackageAuditValidationResult{{Command: "invalid"}}
		},
		func(i *WorkflowPackageAuditPacketInput) { i.Artifacts = nil },
	}
	for index, mutate := range cases {
		input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
		mutate(&input)
		_, _, err := buildWorkflowPackageAuditPacket(input)
		if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
			t.Fatalf("case %d: expected invalid error, got %v", index, err)
		}
	}
}

func TestWorkflowPackageAuditPacketNoLegacyKeys(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacket(input)
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

func TestWorkflowPackageAuditPacketNoRequirementsLayerRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	// Replace the only requirements layer with shared_design.
	for index := range input.Evidence.Authority.AuthorityLayers {
		input.Evidence.Authority.AuthorityLayers[index].Kind = "shared_design"
	}
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketAuthorityLayerSequenceRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	// Append a duplicate sequence layer.
	duplicate := input.Evidence.Authority.AuthorityLayers[0]
	input.Evidence.Authority.AuthorityLayers = append(input.Evidence.Authority.AuthorityLayers, duplicate)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketTextArtifactUtf8Rejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Artifacts[0].MediaType = "text/plain"
	input.Artifacts[0].Bytes = []byte{0xff}
	input.Artifacts[0].SHA256 = testPackageAuditSHA256(input.Artifacts[0].Bytes)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketInvalidChangedFileTypeRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles[0].ChangeType = "unknown"
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketValidationStatusRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.Validation[0].Status = "unknown"
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketRepeatedBuildsPacketObjectEqual(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	first, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("packet objects differ")
	}
}

func TestWorkflowPackageAuditPacketInvalidAuditedCommitRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, "not-a-sha")
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketInvalidBaseCommitRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.BaseCommit = strings.Repeat("G", 40)
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketInvalidEffectiveBriefSHARejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.EffectiveBrief.Artifact.SHA256 = "bad"
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketInvalidUserIntentRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.UserIntent = "   "
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func testPackageAuditMarshalCanonical(p WorkflowPackageAuditPacket) []byte {
	data, _ := json.MarshalIndent(p, "", "  ")
	return append(data, '\n')
}

func TestWorkflowPackageAuditPacketDecodedRejectsUnsafeChangedFilePath(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.ChangedFiles[0].Path = "../escape.go"
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected unsafe changed file path to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedRejectsUnsafePreviousPath(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Commit.ChangedFiles = []WorkflowPackageAuditChangedFile{
		{Path: "a/b.go", PreviousPath: "old/b.go", ChangeType: "renamed", Additions: 1, Deletions: 0},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.ChangedFiles[0].PreviousPath = "../old.go"
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected unsafe previous_path to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedRejectsUnsafeRelevantSourcePath(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.RelevantSourcePaths[0] = "/absolute/path.go"
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected unsafe relevant source path to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedRejectsUnsafeArtifactFilename(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.Artifacts[0].Filename = "../escape.json"
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected unsafe artifact filename to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedRejectsWhitespaceStrings(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*WorkflowPackageAuditPacket)
	}{
		{"user_intent whitespace", func(p *WorkflowPackageAuditPacket) { p.Run.UserIntent = "   " }},
		{"user_intent outer", func(p *WorkflowPackageAuditPacket) { p.Run.UserIntent = " intent " }},
		{"repo_target outer", func(p *WorkflowPackageAuditPacket) { p.Repository.RepoTarget = " relay " }},
		{"branch outer", func(p *WorkflowPackageAuditPacket) { p.Repository.Branch = " main " }},
		{"execution status whitespace", func(p *WorkflowPackageAuditPacket) { p.Execution.Status = "   " }},
		{"execution status outer", func(p *WorkflowPackageAuditPacket) { p.Execution.Status = " status " }},
		{"completion summary whitespace", func(p *WorkflowPackageAuditPacket) { p.Execution.CompletionSummary = "   " }},
		{"validation command whitespace", func(p *WorkflowPackageAuditPacket) { p.Validation[0].Command = "   " }},
		{"validation command outer", func(p *WorkflowPackageAuditPacket) { p.Validation[0].Command = " cmd " }},
		{"validation expected whitespace", func(p *WorkflowPackageAuditPacket) { p.Validation[0].Expected = "   " }},
		{"validation expected outer", func(p *WorkflowPackageAuditPacket) { p.Validation[0].Expected = " exp " }},
		{"validation concise whitespace", func(p *WorkflowPackageAuditPacket) { p.Validation[0].ConciseResult = "   " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := packet
			tc.mutate(&p)
			if err := validateWorkflowPackageAuditPacket(p); err == nil {
				t.Fatal("expected whitespace/blank string to be rejected")
			}
		})
	}
}

func TestWorkflowPackageAuditPacketDecodedDeliveryTicketRejectsTextString(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	encoded, _ := json.Marshal("text content")
	packet.Authority.DeliveryTicket.Content = json.RawMessage(encoded)
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected text-string delivery ticket to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedRequirementsRejectTextString(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	encoded, _ := json.Marshal("text content")
	packet.Authority.Requirements[0].Content = json.RawMessage(encoded)
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected text-string requirements to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedSharedDesignRejectTextString(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	sharedBytes := []byte(`{"shared":true}`)
	input.Evidence.Authority.AuthorityLayers = append(input.Evidence.Authority.AuthorityLayers, workflowpackages.ApprovedAuthorityLayer{
		Kind:         "shared_design",
		Sequence:     2,
		RelativePath: "plans/checkout/shared-design.json",
		MediaType:    "application/json",
		SHA256:       testPackageAuditSHA256(sharedBytes),
		Bytes:        sharedBytes,
	})
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	encoded, _ := json.Marshal("text content")
	packet.Authority.SharedDesign[0].Content = json.RawMessage(encoded)
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected text-string shared design to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedBriefRejectsNonStringJSON(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.Authority.TicketDesignBrief.Content = json.RawMessage(`{"not":"text"}`)
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected non-string ticket design brief content to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedTextArtifactRejectsDigestDisagreement(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	textBytes := []byte("artifact text")
	input.Artifacts = []WorkflowPackageAuditEmbeddedArtifactInput{
		{Filename: "note.txt", MediaType: "text/plain", SHA256: testPackageAuditSHA256(textBytes), Bytes: textBytes},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.Artifacts[0].SHA256 = strings.Repeat("0", 64)
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected text artifact digest disagreement to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedDeterministicOpsCoverageDisagreement(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	partial := "partial"
	packet.DeterministicApplication.Coverage = &partial
	packet.Execution.AdaptiveAttemptDispatched = true
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected deterministic operations coverage disagreement to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedNotPresentWithOperationsRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ops := packet.Authority.DeliveryTicket
	ops.Content = json.RawMessage(`{"coverage":"complete"}`)
	packet.Authority.DeterministicOperations = &ops
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected not_present outcome with deterministic operations to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedAppliedWithoutOperationsRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.DeterministicApplication.Outcome = "applied"
	partial := "partial"
	packet.DeterministicApplication.Coverage = &partial
	packet.Execution.AdaptiveAttemptDispatched = true
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected applied outcome without deterministic operations to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedPreflightFailedWithoutOperationsRejected(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.DeterministicApplication.Outcome = "preflight_failed"
	partial := "partial"
	packet.DeterministicApplication.Coverage = &partial
	packet.Execution.AdaptiveAttemptDispatched = true
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected preflight_failed outcome without deterministic operations to be rejected")
	}
}

func TestWorkflowPackageAuditPacketBytesRejectInvalidShapes(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	sharedBytes := []byte(`{"shared":true}`)
	input.Evidence.Authority.AuthorityLayers = append(input.Evidence.Authority.AuthorityLayers, workflowpackages.ApprovedAuthorityLayer{
		Kind:         "shared_design",
		Sequence:     2,
		RelativePath: "plans/checkout/shared-design.json",
		MediaType:    "application/json",
		SHA256:       testPackageAuditSHA256(sharedBytes),
		Bytes:        sharedBytes,
	})
	textBytes := []byte("artifact text")
	input.Artifacts = []WorkflowPackageAuditEmbeddedArtifactInput{
		{Filename: "note.txt", MediaType: "text/plain", SHA256: testPackageAuditSHA256(textBytes), Bytes: textBytes},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*WorkflowPackageAuditPacket)
	}{
		{"unsafe changed file path", func(p *WorkflowPackageAuditPacket) { p.ChangedFiles[0].Path = "../escape.go" }},
		{"unsafe renamed previous_path", func(p *WorkflowPackageAuditPacket) { p.ChangedFiles[0].PreviousPath = "../old.go" }},
		{"unsafe relevant source path", func(p *WorkflowPackageAuditPacket) { p.RelevantSourcePaths[0] = "/absolute/path.go" }},
		{"unsafe artifact filename", func(p *WorkflowPackageAuditPacket) { p.Artifacts[0].Filename = "../escape.json" }},
		{"whitespace user_intent", func(p *WorkflowPackageAuditPacket) { p.Run.UserIntent = "   " }},
		{"text delivery ticket", func(p *WorkflowPackageAuditPacket) {
			encoded, _ := json.Marshal("text")
			p.Authority.DeliveryTicket.Content = json.RawMessage(encoded)
		}},
		{"text requirements", func(p *WorkflowPackageAuditPacket) {
			encoded, _ := json.Marshal("text")
			p.Authority.Requirements[0].Content = json.RawMessage(encoded)
		}},
		{"text shared design", func(p *WorkflowPackageAuditPacket) {
			encoded, _ := json.Marshal("text")
			p.Authority.SharedDesign[0].Content = json.RawMessage(encoded)
		}},
		{"non-string brief", func(p *WorkflowPackageAuditPacket) {
			p.Authority.TicketDesignBrief.Content = json.RawMessage(`{"not":"text"}`)
		}},
		{"text artifact digest disagreement", func(p *WorkflowPackageAuditPacket) {
			p.Artifacts[0].SHA256 = strings.Repeat("0", 64)
		}},
		{"not_present with operations", func(p *WorkflowPackageAuditPacket) {
			ops := p.Authority.DeliveryTicket
			ops.Content = json.RawMessage(`{"coverage":"complete"}`)
			p.Authority.DeterministicOperations = &ops
		}},
		{"applied without operations", func(p *WorkflowPackageAuditPacket) {
			p.DeterministicApplication.Outcome = "applied"
			partial := "partial"
			p.DeterministicApplication.Coverage = &partial
			p.Execution.AdaptiveAttemptDispatched = true
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := packet
			tc.mutate(&p)
			if err := validateWorkflowPackageAuditPacketBytes(testPackageAuditMarshalCanonical(p)); err == nil {
				t.Fatal("expected byte validator to reject invalid shape")
			}
		})
	}
}

func TestWorkflowPackageAuditPacketJSONArtifactWithWhitespaceBuilds(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	whitespaceJSON := []byte("{\n  \"key\": \"value\"\n}")
	input.Artifacts = []WorkflowPackageAuditEmbeddedArtifactInput{
		{Filename: "formatted.json", MediaType: "application/json", SHA256: testPackageAuditSHA256(whitespaceJSON), Bytes: whitespaceJSON},
	}
	if _, _, err := buildWorkflowPackageAuditPacket(input); err != nil {
		t.Fatalf("build with whitespace JSON artifact: %v", err)
	}
}

func TestWorkflowPackageAuditPacketTextArtifactVerifiesDigest(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	textBytes := []byte("hello, text artifact")
	input.Artifacts = []WorkflowPackageAuditEmbeddedArtifactInput{
		{Filename: "note.txt", MediaType: "text/plain", SHA256: testPackageAuditSHA256(textBytes), Bytes: textBytes},
	}
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build with text artifact: %v", err)
	}
	if err := validateWorkflowPackageAuditPacket(packet); err != nil {
		t.Fatalf("decoded text artifact validation: %v", err)
	}
}

func TestWorkflowPackageAuditPacketBuilderRejectsCompletionSummaryOuterWhitespace(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Execution.CompletionSummary = " completed "
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for completion_summary outer whitespace, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketBuilderRejectsConciseResultOuterWhitespace(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Evidence.Validation[0].ConciseResult = " result "
	_, _, err := buildWorkflowPackageAuditPacket(input)
	if !errors.Is(err, ErrWorkflowPackageAuditPacketInvalid) {
		t.Fatalf("expected invalid error for concise_result outer whitespace, got %v", err)
	}
}

func TestWorkflowPackageAuditPacketDecodedRejectsCompletionSummaryOuterWhitespace(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.Execution.CompletionSummary = " completed "
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected completion_summary outer whitespace to be rejected")
	}
}

func TestWorkflowPackageAuditPacketDecodedRejectsConciseResultOuterWhitespace(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	packet.Validation[0].ConciseResult = " result "
	if err := validateWorkflowPackageAuditPacket(packet); err == nil {
		t.Fatal("expected concise_result outer whitespace to be rejected")
	}
}

func TestWorkflowPackageAuditPacketBytesRejectOuterWhitespaceFields(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*WorkflowPackageAuditPacket)
	}{
		{"completion_summary outer whitespace", func(p *WorkflowPackageAuditPacket) { p.Execution.CompletionSummary = " completed " }},
		{"concise_result outer whitespace", func(p *WorkflowPackageAuditPacket) { p.Validation[0].ConciseResult = " result " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := packet
			tc.mutate(&p)
			if err := validateWorkflowPackageAuditPacketBytes(testPackageAuditMarshalCanonical(p)); err == nil {
				t.Fatal("expected byte validator to reject outer whitespace field")
			}
		})
	}
}

func TestWorkflowPackageAuditPacketValidCompletionSummaryAndConciseResultUnchanged(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	input.Execution.CompletionSummary = "Implementation completed successfully."
	input.Evidence.Validation[0].ConciseResult = "ok"
	packet, _, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.Execution.CompletionSummary != "Implementation completed successfully." {
		t.Fatalf("completion_summary = %q, want unchanged", packet.Execution.CompletionSummary)
	}
	if packet.Validation[0].ConciseResult != "ok" {
		t.Fatalf("concise_result = %q, want unchanged", packet.Validation[0].ConciseResult)
	}
}

func TestWorkflowPackageAuditPacketSchemaVersionIsThreeZero(t *testing.T) {
	if WorkflowPackageAuditPacketSchemaVersion != "3.0" {
		t.Fatalf("schema version = %q, want 3.0", WorkflowPackageAuditPacketSchemaVersion)
	}
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefAdaptiveNoOperations, strings.Repeat("c", 40))
	packet, data, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if packet.SchemaVersion != "3.0" {
		t.Fatalf("packet schema version = %q, want 3.0", packet.SchemaVersion)
	}
	if !strings.Contains(string(data), "\"schema_version\": \"3.0\"") {
		t.Fatalf("serialized schema_version must be 3.0")
	}
}

func TestWorkflowPackageAuditPacketSerializedFieldNamesUnchanged(t *testing.T) {
	input := testPackageAuditInput(t, executor.EffectiveExecutorBriefDeterministicComplete, strings.Repeat("c", 40))
	_, data, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	expected := []string{
		"\"schema_version\"", "\"run\"", "\"run_id\"", "\"user_intent\"",
		"\"repository\"", "\"repo_target\"", "\"branch\"", "\"base_commit\"", "\"audited_commit\"",
		"\"authority\"", "\"delivery_ticket\"", "\"requirements\"", "\"shared_design\"",
		"\"ticket_design_brief\"", "\"deterministic_operations\"", "\"execution_assignment\"",
		"\"effective_executor_brief\"", "\"artifact_reference\"", "\"sha256\"",
		"\"deterministic_application\"", "\"outcome\"", "\"coverage\"", "\"evidence\"",
		"\"execution\"", "\"adaptive_attempt_dispatched\"", "\"status\"", "\"committed_sha\"",
		"\"completion_summary\"", "\"changed_files\"", "\"path\"", "\"change_type\"",
		"\"additions\"", "\"deletions\"", "\"relevant_source_paths\"", "\"validation\"",
		"\"command\"", "\"expected\"", "\"concise_result\"", "\"artifacts\"",
		"\"filename\"", "\"content\"",
	}
	for _, field := range expected {
		if !strings.Contains(string(data), field) {
			t.Fatalf("serialized output missing field %s", field)
		}
	}
}
