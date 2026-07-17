package semanticidentity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"relay/internal/mcp/fileacquisition"
	"relay/internal/operations/registry"
)

type rogueRequestIdentity struct {
	Surface registry.SurfaceContractID
	Bytes   []byte
}

func (rogueRequestIdentity) requestIdentity()                                {}
func (v rogueRequestIdentity) SurfaceContractID() registry.SurfaceContractID { return v.Surface }
func (rogueRequestIdentity) MutationTool() registry.MutationTool {
	return registry.MutationToolSubmitPlan
}
func (rogueRequestIdentity) SemanticIdentityVersion() string {
	version, _ := registry.SemanticProjectionVersion(string(registry.MutationToolSubmitPlan))
	return version
}

func TestSixExactRequestIdentitiesAreClosedDeterministicAndTransportFree(t *testing.T) {
	for _, identity := range validRequestIdentities() {
		first, err := BuildFingerprint(identity)
		if err != nil {
			t.Fatalf("%T: %v", identity, err)
		}
		second, err := BuildFingerprint(identity)
		if err != nil {
			t.Fatalf("%T: %v", identity, err)
		}
		if first != second || first.SurfaceContractID() != identity.SurfaceContractID() || first.Tool() != identity.MutationTool() || first.SemanticIdentityVersion() == "" || !validSHA256(first.SemanticRequestSHA256()) {
			t.Fatalf("%T produced invalid fingerprint %#v", identity, first)
		}
		raw, err := json.Marshal(identity)
		if err != nil {
			t.Fatal(err)
		}
		for _, excluded := range []string{`"mutation_id"`, `"download_url"`, `"file_id"`, `"mime_type_hint"`, `"file_name_hint"`, `"trace_id"`, `"request_id"`, `"authorization"`, `"acquired_bytes"`} {
			if strings.Contains(string(raw), excluded) {
				t.Fatalf("%T leaked excluded field %q: %s", identity, excluded, raw)
			}
		}
	}
	if _, err := BuildFingerprint(rogueRequestIdentity{Surface: "planner-plan.v1", Bytes: []byte("forbidden")}); err == nil {
		t.Fatal("arbitrary request identity was accepted")
	}
}

func TestRequestIdentitySurfaceToolAndRequiredFieldClosure(t *testing.T) {
	sha := strings.Repeat("a", 64)
	clearance := validClearance(sha)
	cases := []RequestIdentity{
		CreateOperationPacket{SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", ProjectID: "", InputFileCount: 0, DeclaredFiles: []DeclaredFile{}, Inputs: []InputBinding{}, WorkflowReferences: []WorkflowReferenceRequest{}, Attestations: []AttestationRequest{}},
		RefreshOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: "", InputFileCount: 0, DeclaredFiles: []DeclaredFile{}, Inputs: []InputBinding{}, WorkflowReferences: []WorkflowReferenceRequest{}, Attestations: []AttestationRequest{}},
		CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: ""},
		SubmitPlan{CanonicalArtifactMutation: CanonicalArtifactMutation{SurfaceContract: "planner-plan.v1", ExpectedPacketID: "packet-1", ArtifactName: "", MediaType: "application/json", ExpectedSHA256: sha, SensitiveDataClearance: clearance}},
		CreateRun{CanonicalArtifactMutation: CanonicalArtifactMutation{SurfaceContract: "planner-execution.v1", ExpectedPacketID: "packet-1", ArtifactName: "feature.execution-spec.json", MediaType: "application/json", ExpectedSHA256: sha, SensitiveDataClearance: registry.SensitiveDataClearance{PolicyVersion: clearance.PolicyVersion, SubjectSHA256: strings.Repeat("b", 64), Confirmed: true}}},
		RecordAuditDecision{SurfaceContract: "auditor-audit.v1", ExpectedPacketID: "packet-1", RunID: "run-1", AuditPacketID: "audit-1", ExpectedAuditPacketSHA256: sha, AuditedCommitOID: strings.Repeat("c", 40), WorkflowScope: AuditWorkflowScope{Kind: "one_shot"}, Decision: "accepted", Rationale: "", MaterialFindings: []AuditFinding{}, NonBlockingObservations: []AuditObservation{}, OperatorConfirmed: true},
	}
	for _, value := range cases {
		if _, err := BuildFingerprint(value); err == nil {
			t.Fatalf("incomplete %T was accepted", value)
		}
	}
	crossSurface := SubmitPlan{CanonicalArtifactMutation: CanonicalArtifactMutation{
		SurfaceContract: "planner-execution.v1", ExpectedPacketID: "packet-1", ArtifactName: "feature.plan.json",
		MediaType: "application/json", ExpectedSHA256: sha, SensitiveDataClearance: clearance,
	}}
	if _, err := BuildFingerprint(crossSurface); err == nil {
		t.Fatal("cross-surface submit_plan identity was accepted")
	}
}

func TestEveryNestedRequestProjectionFieldAffectsFingerprintOrValidity(t *testing.T) {
	base := validCreateOperationPacket()
	assertFingerprintChanges(t, base,
		func(v *CreateOperationPacket) { v.OperationID = "planner.design" },
		func(v *CreateOperationPacket) { v.ProjectID = "project-2" },
		func(v *CreateOperationPacket) {
			v.DeclaredFiles[0].ExpectedSHA256 = strings.Repeat("b", 64)
			v.Inputs[0].ExpectedSHA256 = strings.Repeat("b", 64)
		},
		func(v *CreateOperationPacket) { v.Inputs[0].InputName = "approved_requirements" },
		func(v *CreateOperationPacket) { v.Inputs[0].DisplayName = "renamed.md" },
		func(v *CreateOperationPacket) { v.Inputs[0].MediaType = "text/plain" },
		func(v *CreateOperationPacket) {
			v.Inputs[0].Source.FileIndex = pointer(int64(1))
			v.DeclaredFiles[0].FileIndex = 1
		},
		func(v *CreateOperationPacket) { v.WorkflowReferences[0].PlanID = "plan-2" },
		func(v *CreateOperationPacket) { v.Attestations[0].SubjectSHA256 = strings.Repeat("b", 64) },
		func(v *CreateOperationPacket) { v.PrimaryRevisions[0].CommitOID = strings.Repeat("f", 40) },
		func(v *CreateOperationPacket) { v.ComparisonAnchors[0].AnchorName = "other" },
		func(v *CreateOperationPacket) { v.RelaySpecsRevision = strings.Repeat("f", 40) },
	)

	refresh := validRefreshOperationPacket()
	assertFingerprintChanges(t, refresh,
		func(v *RefreshOperationPacket) { v.ExpectedPacketID = "packet-2" },
		func(v *RefreshOperationPacket) {
			v.InputFileCount = 1
			v.DeclaredFiles = []DeclaredFile{{FileIndex: 0, ExpectedSHA256: strings.Repeat("a", 64)}}
		},
		func(v *RefreshOperationPacket) { v.RelaySpecsRevision = strings.Repeat("f", 40) },
	)
	closeValue := CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: "packet-1"}
	assertFingerprintChanges(t, closeValue, func(v *CloseOperationPacket) { v.ExpectedPacketID = "packet-2" })

	submit := validSubmitPlan()
	assertFingerprintChanges(t, submit,
		func(v *SubmitPlan) { v.ExpectedPacketID = "packet-2" },
		func(v *SubmitPlan) { v.ArtifactName = "other.plan.json" },
		func(v *SubmitPlan) { v.MediaType = "application/octet-stream" },
		func(v *SubmitPlan) {
			v.ExpectedSHA256 = strings.Repeat("b", 64)
			v.SensitiveDataClearance.SubjectSHA256 = strings.Repeat("b", 64)
		},
		func(v *SubmitPlan) { v.SensitiveDataClearance.Declaration.Credential = true },
	)

	run := validCreateRun()
	assertFingerprintChanges(t, run,
		func(v *CreateRun) { v.ExpectedPacketID = "packet-2" },
		func(v *CreateRun) { v.ArtifactName = "other.execution-spec.json" },
		func(v *CreateRun) {
			v.ExpectedSHA256 = strings.Repeat("b", 64)
			v.SensitiveDataClearance.SubjectSHA256 = strings.Repeat("b", 64)
		},
	)

	audit := validRecordAuditDecision()
	assertFingerprintChanges(t, audit,
		func(v *RecordAuditDecision) { v.ExpectedPacketID = "packet-2" },
		func(v *RecordAuditDecision) { v.RunID = "run-2" },
		func(v *RecordAuditDecision) { v.AuditPacketID = "audit-2" },
		func(v *RecordAuditDecision) { v.ExpectedAuditPacketSHA256 = strings.Repeat("b", 64) },
		func(v *RecordAuditDecision) { v.AuditedCommitOID = strings.Repeat("d", 40) },
		func(v *RecordAuditDecision) {
			v.WorkflowScope = AuditWorkflowScope{Kind: "selected_pass", PlanID: "plan-1", PassID: "pass-1"}
		},
		func(v *RecordAuditDecision) { v.Decision = "needs_revision" },
		func(v *RecordAuditDecision) { v.Rationale = "different" },
		func(v *RecordAuditDecision) {
			v.MaterialFindings = []AuditFinding{{Source: "implementation", Location: "file", Summary: "summary", Evidence: "evidence", RequiredRevision: "revision"}}
		},
		func(v *RecordAuditDecision) {
			v.NonBlockingObservations = []AuditObservation{{Summary: "summary", Evidence: "evidence"}}
		},
	)
	audit.OperatorConfirmed = false
	if _, err := BuildFingerprint(audit); err == nil {
		t.Fatal("operator_confirmed false was accepted")
	}
}

func TestAllClosedNestedBranchesAreRepresented(t *testing.T) {
	sha := strings.Repeat("a", 64)
	index := int64(0)
	inputs := []InputBinding{
		{InputName: "uploaded", SourceKind: "uploaded_file", DisplayName: "upload.bin", MediaType: "application/octet-stream", ExpectedSHA256: sha, Source: InputBindingSource{FileIndex: &index}},
		{InputName: "artifact", SourceKind: "relay_artifact", DisplayName: "artifact.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{ArtifactID: "artifact-1"}},
		{InputName: "inline", SourceKind: "inline_text", DisplayName: "inline.txt", MediaType: "text/plain", ExpectedSHA256: sha, Source: InputBindingSource{Text: "body"}},
		{InputName: "workflow", SourceKind: "workflow_record", DisplayName: "workflow.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{WorkflowRecord: &WorkflowRecordInputReference{Kind: "plan_artifact", PlanID: "plan-1", ArtifactID: "artifact-1", ExpectedSHA256: sha}}},
		{InputName: "source", SourceKind: "committed_source", DisplayName: "source.go", MediaType: "text/plain", ExpectedSHA256: sha, Source: InputBindingSource{RepositoryKey: "relay", Revision: "primary", Path: &SourcePathSelector{PathID: sha}, ExpectedBlobOID: strings.Repeat("b", 40)}},
	}
	attestations := []AttestationRequest{
		{Kind: "confirmed_intent", InputName: "uploaded", SubjectSHA256: sha, Confirmed: true},
		{Kind: "approved_artifact", InputName: "artifact", SubjectSHA256: sha, Approved: true},
		{Kind: "candidate_for_review", InputName: "inline", SubjectSHA256: sha, CompleteTransfer: true},
		{Kind: "execution_mode_selection", InputName: "workflow", SelectedMode: "plan"},
		{Kind: "complete_review_result", InputName: "source", SubjectSHA256: sha, ReviewedCandidateSHA256: sha, ReviewResult: "needs_revision", Complete: true},
		{Kind: "completed_dependency_outcomes", InputName: "source", SubjectSHA256: sha, Complete: true},
		{Kind: "operator_confirmation", InputName: "source", Confirmed: true},
		{Kind: "separate_session_authorship", InputName: "source", Confirmed: true},
		{Kind: "exact_evidence", InputName: "source", SubjectSHA256: sha, Complete: true},
		{Kind: "sensitive_data_clearance", InputName: "artifact", Clearance: pointer(validClearance(sha))},
	}
	value := CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", ProjectID: "project-1",
		InputFileCount: 1, DeclaredFiles: []DeclaredFile{{FileIndex: 0, ExpectedSHA256: sha}}, Inputs: inputs,
		WorkflowReferences: []WorkflowReferenceRequest{
			{Kind: "plan", PlanID: "plan-1"},
			{Kind: "pass", PlanID: "plan-1", PassID: "pass-1"},
			{Kind: "run", RunID: "run-1"},
			{Kind: "audit_packet", RunID: "run-1", AuditPacketID: "packet-1", ExpectedAuditPacketSHA256: sha},
			{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1"},
		},
		Attestations: attestations,
	}
	if _, err := BuildFingerprint(value); err != nil {
		t.Fatal(err)
	}
	value.Inputs[0].Source.ArtifactID = "forbidden-extra"
	if _, err := BuildFingerprint(value); err == nil {
		t.Fatal("open input-source union was accepted")
	}
}

func TestTransportHintsDoNotAffectCanonicalFileProjection(t *testing.T) {
	values := []fileacquisition.DeclaredFile{
		{FileIndex: 1, ExpectedSHA256: strings.Repeat("b", 64), DisplayName: "provider-one", MediaType: "application/provider"},
		{FileIndex: 0, ExpectedSHA256: strings.Repeat("a", 64), DisplayName: "provider-two", MediaType: "text/provider"},
	}
	got := CanonicalDeclaredFiles(values)
	if !reflect.DeepEqual(got, []DeclaredFile{
		{FileIndex: 0, ExpectedSHA256: strings.Repeat("a", 64)},
		{FileIndex: 1, ExpectedSHA256: strings.Repeat("b", 64)},
	}) {
		t.Fatalf("projection = %#v", got)
	}
}

func validRequestIdentities() []RequestIdentity {
	return []RequestIdentity{
		validCreateOperationPacket(),
		validRefreshOperationPacket(),
		CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: "packet-1"},
		validSubmitPlan(),
		validCreateRun(),
		validRecordAuditDecision(),
	}
}

func validCreateOperationPacket() CreateOperationPacket {
	sha := strings.Repeat("a", 64)
	index := int64(0)
	return CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", ProjectID: "project-1",
		InputFileCount: 1, DeclaredFiles: []DeclaredFile{{FileIndex: 0, ExpectedSHA256: sha}},
		Inputs:             []InputBinding{{InputName: "confirmed_intent", SourceKind: "uploaded_file", DisplayName: "intent.md", MediaType: "text/markdown", ExpectedSHA256: sha, Source: InputBindingSource{FileIndex: &index}}},
		WorkflowReferences: []WorkflowReferenceRequest{{Kind: "plan", PlanID: "plan-1"}},
		Attestations:       []AttestationRequest{{Kind: "confirmed_intent", InputName: "confirmed_intent", SubjectSHA256: sha, Confirmed: true}},
		PrimaryRevisions:   []PrimaryRevisionRequest{{RepositoryKey: "relay", CommitOID: strings.Repeat("c", 40)}},
		ComparisonAnchors:  []ComparisonAnchorRequest{{RepositoryKey: "relay", AnchorName: "base", Purpose: "plan_base", CommitOID: strings.Repeat("c", 40), ExpectedTreeOID: strings.Repeat("d", 40)}},
		RelaySpecsRevision: strings.Repeat("e", 40),
	}
}

func validRefreshOperationPacket() RefreshOperationPacket {
	return RefreshOperationPacket{
		SurfaceContract: "planner-authoring.v1", ExpectedPacketID: "packet-1", InputFileCount: 0,
		DeclaredFiles: []DeclaredFile{}, Inputs: []InputBinding{}, WorkflowReferences: []WorkflowReferenceRequest{}, Attestations: []AttestationRequest{},
	}
}

func validSubmitPlan() SubmitPlan {
	sha := strings.Repeat("a", 64)
	return SubmitPlan{CanonicalArtifactMutation: CanonicalArtifactMutation{
		SurfaceContract: "planner-plan.v1", ExpectedPacketID: "packet-1", ArtifactName: "feature.plan.json",
		MediaType: "application/json", ExpectedSHA256: sha, SensitiveDataClearance: validClearance(sha),
	}}
}

func validCreateRun() CreateRun {
	sha := strings.Repeat("a", 64)
	return CreateRun{CanonicalArtifactMutation: CanonicalArtifactMutation{
		SurfaceContract: "planner-execution.v1", ExpectedPacketID: "packet-1", ArtifactName: "feature.execution-spec.json",
		MediaType: "application/json", ExpectedSHA256: sha, SensitiveDataClearance: validClearance(sha),
	}}
}

func validRecordAuditDecision() RecordAuditDecision {
	return RecordAuditDecision{
		SurfaceContract: "auditor-audit.v1", ExpectedPacketID: "packet-1", RunID: "run-1", AuditPacketID: "audit-1",
		ExpectedAuditPacketSHA256: strings.Repeat("a", 64), AuditedCommitOID: strings.Repeat("c", 40),
		WorkflowScope: AuditWorkflowScope{Kind: "one_shot"}, Decision: "accepted", Rationale: "complete",
		MaterialFindings: []AuditFinding{}, NonBlockingObservations: []AuditObservation{}, OperatorConfirmed: true,
	}
}

func validClearance(sha string) registry.SensitiveDataClearance {
	return registry.SensitiveDataClearance{
		PolicyVersion: registry.SensitiveDataClearancePolicyVersion,
		SubjectSHA256: sha,
		Confirmed:     true,
	}
}

func assertFingerprintChanges[T RequestIdentity](t *testing.T, base T, edits ...func(*T)) {
	t.Helper()
	baseline, err := BuildFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	for index, edit := range edits {
		candidate := deepCopy(t, base)
		edit(&candidate)
		changed, err := BuildFingerprint(candidate)
		if err != nil {
			continue
		}
		if changed == baseline {
			t.Fatalf("edit %d did not change fingerprint", index)
		}
	}
}

func deepCopy[T any](t *testing.T, value T) T {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func pointer[T any](value T) *T { return &value }

func TestEveryJSONLeafInEveryExactRequestProjectionChangesFingerprintOrFailsClosed(t *testing.T) {
	values := []RequestIdentity{
		completeNestedCreateIdentity(),
		validRefreshOperationPacket(),
		CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: "packet-1"},
		validSubmitPlan(),
		validCreateRun(),
		completeAuditDecisionIdentity(),
	}
	for _, value := range values {
		t.Run(reflect.TypeOf(value).Name(), func(t *testing.T) {
			baseline, err := BuildFingerprint(value)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var document any
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			paths := scalarLeafPaths(document, nil)
			if len(paths) == 0 {
				t.Fatal("identity has no scalar leaves")
			}
			for _, path := range paths {
				mutated := deepCopyJSON(t, document)
				mutateScalarLeaf(mutated, path)
				mutatedRaw, err := json.Marshal(mutated)
				if err != nil {
					t.Fatal(err)
				}
				candidate := newRequestIdentityOf(value)
				if err := json.Unmarshal(mutatedRaw, candidate); err != nil {
					t.Fatalf("%v unmarshal: %v", path, err)
				}
				changed, err := BuildFingerprint(candidate)
				if err == nil && changed == baseline {
					t.Fatalf("%T leaf %v did not affect fingerprint", value, path)
				}
			}
		})
	}
}

func completeNestedCreateIdentity() CreateOperationPacket {
	sha := strings.Repeat("a", 64)
	index := int64(0)
	inputs := []InputBinding{
		{InputName: "uploaded", SourceKind: "uploaded_file", DisplayName: "upload.bin", MediaType: "application/octet-stream", ExpectedSHA256: sha, Source: InputBindingSource{FileIndex: &index}},
		{InputName: "artifact", SourceKind: "relay_artifact", DisplayName: "artifact.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{ArtifactID: "artifact-1"}},
		{InputName: "inline", SourceKind: "inline_text", DisplayName: "inline.txt", MediaType: "text/plain", ExpectedSHA256: sha, Source: InputBindingSource{Text: "body"}},
		{InputName: "workflow_plan", SourceKind: "workflow_record", DisplayName: "plan.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{WorkflowRecord: &WorkflowRecordInputReference{Kind: "plan_artifact", PlanID: "plan-1", ArtifactID: "artifact-plan", ExpectedSHA256: sha}}},
		{InputName: "workflow_pass", SourceKind: "workflow_record", DisplayName: "pass.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{WorkflowRecord: &WorkflowRecordInputReference{Kind: "pass_record", PlanID: "plan-1", PassID: "pass-1"}}},
		{InputName: "workflow_run", SourceKind: "workflow_record", DisplayName: "run.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{WorkflowRecord: &WorkflowRecordInputReference{Kind: "run_execution_spec", RunID: "run-1", ArtifactID: "artifact-run", ExpectedSHA256: sha}}},
		{InputName: "workflow_packet", SourceKind: "workflow_record", DisplayName: "packet.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{WorkflowRecord: &WorkflowRecordInputReference{Kind: "audit_packet", RunID: "run-1", AuditPacketID: "audit-packet-1", ExpectedSHA256: sha}}},
		{InputName: "workflow_decision", SourceKind: "workflow_record", DisplayName: "decision.json", MediaType: "application/json", ExpectedSHA256: sha, Source: InputBindingSource{WorkflowRecord: &WorkflowRecordInputReference{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1"}}},
		{InputName: "source_bytes", SourceKind: "committed_source", DisplayName: "source.go", MediaType: "text/plain", ExpectedSHA256: sha, Source: InputBindingSource{RepositoryKey: "relay", Revision: "primary", Path: &SourcePathSelector{PathBytesBase64: "YS5nbw=="}, ExpectedBlobOID: strings.Repeat("b", 40)}},
		{InputName: "source_id", SourceKind: "committed_source", DisplayName: "source-id.go", MediaType: "text/plain", ExpectedSHA256: sha, Source: InputBindingSource{RepositoryKey: "relay", Revision: "commit:" + strings.Repeat("c", 40), Path: &SourcePathSelector{PathID: sha}, ExpectedBlobOID: strings.Repeat("d", 40)}},
	}
	return CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.requirements",
		ProjectID:       "project-1",
		InputFileCount:  1,
		DeclaredFiles:   []DeclaredFile{{FileIndex: 0, ExpectedSHA256: sha}},
		Inputs:          inputs,
		WorkflowReferences: []WorkflowReferenceRequest{
			{Kind: "plan", PlanID: "plan-1"},
			{Kind: "pass", PlanID: "plan-1", PassID: "pass-1"},
			{Kind: "run", RunID: "run-1"},
			{Kind: "audit_packet", RunID: "run-1", AuditPacketID: "audit-packet-1", ExpectedAuditPacketSHA256: sha},
			{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1"},
		},
		Attestations: []AttestationRequest{
			{Kind: "confirmed_intent", InputName: "uploaded", SubjectSHA256: sha, Confirmed: true},
			{Kind: "approved_artifact", InputName: "artifact", SubjectSHA256: sha, Approved: true},
			{Kind: "candidate_for_review", InputName: "inline", SubjectSHA256: sha, CompleteTransfer: true},
			{Kind: "execution_mode_selection", InputName: "workflow_plan", SelectedMode: "plan"},
			{Kind: "complete_review_result", InputName: "workflow_pass", SubjectSHA256: sha, ReviewedCandidateSHA256: sha, ReviewResult: "needs_revision", Complete: true},
			{Kind: "completed_dependency_outcomes", InputName: "workflow_run", SubjectSHA256: sha, Complete: true},
			{Kind: "operator_confirmation", InputName: "workflow_packet", Confirmed: true},
			{Kind: "separate_session_authorship", InputName: "workflow_decision", Confirmed: true},
			{Kind: "exact_evidence", InputName: "source_bytes", SubjectSHA256: sha, Complete: true},
			{Kind: "sensitive_data_clearance", InputName: "artifact", Clearance: pointer(validClearance(sha))},
		},
		PrimaryRevisions:   []PrimaryRevisionRequest{{RepositoryKey: "relay", CommitOID: strings.Repeat("c", 40)}},
		ComparisonAnchors:  []ComparisonAnchorRequest{{RepositoryKey: "relay", AnchorName: "base", Purpose: "plan_base", CommitOID: strings.Repeat("c", 40), ExpectedTreeOID: strings.Repeat("d", 40)}},
		RelaySpecsRevision: strings.Repeat("e", 40),
	}
}

func completeAuditDecisionIdentity() RecordAuditDecision {
	value := validRecordAuditDecision()
	value.WorkflowScope = AuditWorkflowScope{Kind: "selected_pass", PlanID: "plan-1", PassID: "pass-1"}
	value.MaterialFindings = []AuditFinding{{Source: "implementation", Location: "file.go:1", Summary: "summary", Evidence: "evidence", RequiredRevision: "revision"}}
	value.NonBlockingObservations = []AuditObservation{{Summary: "observation", Evidence: "evidence"}}
	return value
}

func scalarLeafPaths(value any, prefix []any) [][]any {
	switch typed := value.(type) {
	case map[string]any:
		var paths [][]any
		for key, child := range typed {
			paths = append(paths, scalarLeafPaths(child, append(append([]any(nil), prefix...), key))...)
		}
		return paths
	case []any:
		var paths [][]any
		for index, child := range typed {
			paths = append(paths, scalarLeafPaths(child, append(append([]any(nil), prefix...), index))...)
		}
		return paths
	case nil:
		return nil
	default:
		return [][]any{append([]any(nil), prefix...)}
	}
}

func mutateScalarLeaf(root any, path []any) {
	current := root
	for _, segment := range path[:len(path)-1] {
		switch typed := segment.(type) {
		case string:
			current = current.(map[string]any)[typed]
		case int:
			current = current.([]any)[typed]
		}
	}
	last := path[len(path)-1]
	var currentValue any
	switch typed := last.(type) {
	case string:
		currentValue = current.(map[string]any)[typed]
	case int:
		currentValue = current.([]any)[typed]
	}
	var replacement any
	switch typed := currentValue.(type) {
	case string:
		replacement = typed + "x"
	case bool:
		replacement = !typed
	case float64:
		replacement = typed + 1
	default:
		panic("unsupported scalar")
	}
	switch typed := last.(type) {
	case string:
		current.(map[string]any)[typed] = replacement
	case int:
		current.([]any)[typed] = replacement
	}
}

func newRequestIdentityOf(value RequestIdentity) RequestIdentity {
	switch value.(type) {
	case CreateOperationPacket:
		return &CreateOperationPacket{}
	case RefreshOperationPacket:
		return &RefreshOperationPacket{}
	case CloseOperationPacket:
		return &CloseOperationPacket{}
	case SubmitPlan:
		return &SubmitPlan{}
	case CreateRun:
		return &CreateRun{}
	case RecordAuditDecision:
		return &RecordAuditDecision{}
	default:
		panic("unknown request identity")
	}
}

func deepCopyJSON(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
