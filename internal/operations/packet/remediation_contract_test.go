package packet

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"relay/internal/operations/registry"
)

func TestCanonicalPacketGoldenMatrix(t *testing.T) {
	operations, err := registry.All()
	if err != nil {
		t.Fatal(err)
	}
	golden := map[registry.OperationID]string{
		"planner.requirements":                        "90ff04857c5945fbf0078b5c976c72cd6e7395cf6a6ad5c3ed6292c995e2310b",
		"planner.design":                              "5bba79687efccc65a3639e41497b0bd3fb97c01a8317dc35a33a0f2eaeb301c2",
		"planner.plan":                                "bc889e4ffa8360fd1cf1f00f73e6af72f1819a24fb429e567bd1d795b18bf738",
		"planner.one_shot_execution_spec":             "2c16438b739830a4e98a2e181bf0dd46a9a3b16d95114c4ba1058ef0ffee188e",
		"planner.selected_pass_design_brief":          "e674e8537a9730d0a366770ec01d9c34711aa526a66dc32ee3bf357b8eecc745",
		"planner.selected_pass_execution_spec":        "4aea289c60f81585a6c8748d141b08554337ae654addbd285769360be6c25100",
		"auditor.requirements_review":                 "bf6942960efb3a27ec1bdeeab3a518c6497bc160ebb4f2ab044e81a837dbd993",
		"auditor.design_review":                       "42535d99180a1eae078524c48cbffbdad71e97f08d7407fa1f6b40b48c59d179",
		"auditor.plan_review":                         "aef320ee665bf93a5b6be5fa37bd2af9c6b840dc81c4e93dbebb652e36f91302",
		"auditor.selected_pass_design_brief_review":   "230233533c4b8eff8aeb4b03111a0eb85ecb03aadb8095a4a566412e1a247abf",
		"auditor.one_shot_execution_spec_review":      "e4c12b2289fb109433ad0258ab3bad6e37c6a17ecade60a0b0ac65aa7e90c826",
		"auditor.selected_pass_execution_spec_review": "65dd6d198efc8165593accb5f26f187a33f9541214c4092406ac735e68c6828f",
		"auditor.audit":                               "b4d35b60bda1eeb8592500bf404095c26b01783a2cebb6766ed50112a8151d91",
		"auditor.remediation_execution_spec":          "26e037bf6722698117a8249c783c1be9df97fd866c052e869c6a96c9f1e4680e",
		"auditor.remediation_execution_spec_review":   "27b83089789fe8d838a01bb8a0340f0484f6e5e1a5fa8786db3ececdc7334228",
	}
	if len(operations) != len(golden) {
		t.Fatalf("operation count = %d, golden count = %d", len(operations), len(golden))
	}
	for _, operation := range operations {
		document := goldenDocument(t, operation)
		first, err := NewSnapshot(document)
		if err != nil {
			t.Fatalf("%s: %v", operation.OperationID, err)
		}
		second, err := NewSnapshot(document)
		if err != nil {
			t.Fatalf("%s second snapshot: %v", operation.OperationID, err)
		}
		if string(first.Bytes()) != string(second.Bytes()) || first.SHA256() != second.SHA256() {
			t.Fatalf("%s is not deterministic", operation.OperationID)
		}
		if got, want := first.SHA256(), golden[operation.OperationID]; got != want {
			t.Fatalf("%s sha256 = %s, want %s", operation.OperationID, got, want)
		}
		if first.SizeBytes() != int64(len(first.Bytes())) || first.MediaType() != MediaType {
			t.Fatalf("%s snapshot identity is inconsistent", operation.OperationID)
		}
	}
}

func TestWorkflowReferenceMultiplicityAndRelationships(t *testing.T) {
	operation, ok := registry.Lookup("planner.selected_pass_execution_spec")
	if !ok {
		t.Fatal("selected-pass operation is missing")
	}
	document := goldenDocument(t, operation)
	document.WorkflowReferences = append(document.WorkflowReferences,
		goldenRef("plan", "2"),
		goldenRef("pass", "2"),
	)
	if _, err := NewSnapshot(document); err != nil {
		t.Fatalf("multiple distinct references of one kind were rejected: %v", err)
	}

	duplicate := goldenDocument(t, operation)
	duplicate.WorkflowReferences = append(duplicate.WorkflowReferences, duplicate.WorkflowReferences[0])
	if _, err := NewSnapshot(duplicate); validationCode(err) != "workflow_reference_duplicate" {
		t.Fatalf("duplicate identity error = %v", err)
	}

	mismatchedPass := goldenDocument(t, operation)
	for index := range mismatchedPass.WorkflowReferences {
		if mismatchedPass.WorkflowReferences[index].Kind == "pass" {
			mismatchedPass.WorkflowReferences[index].PlanID = "plan-missing"
		}
	}
	if _, err := NewSnapshot(mismatchedPass); validationCode(err) != "workflow_reference_relationship" {
		t.Fatalf("mismatched plan/pass error = %v", err)
	}

	remediation, ok := registry.Lookup("auditor.remediation_execution_spec")
	if !ok {
		t.Fatal("remediation operation is missing")
	}
	mismatchedDecision := goldenDocument(t, remediation)
	for index := range mismatchedDecision.WorkflowReferences {
		if mismatchedDecision.WorkflowReferences[index].Kind == "audit_decision" {
			mismatchedDecision.WorkflowReferences[index].RunID = "run-missing"
		}
	}
	if _, err := NewSnapshot(mismatchedDecision); validationCode(err) != "workflow_reference_relationship" {
		t.Fatalf("mismatched run/decision error = %v", err)
	}
}

func TestWorkflowRecordMustBeRepresentedByPacketReferences(t *testing.T) {
	operations, err := registry.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		for slotIndex, slot := range operation.RequiredInputs {
			if !containsSourceKind(slot.AllowedSourceKinds, InputSourceWorkflowRecord) || len(operation.WorkflowReferenceKinds) == 0 {
				continue
			}
			document := goldenDocument(t, operation)
			inputIndex := requiredInputIndex(operation, slotIndex)
			input := &document.Inputs[inputIndex]
			input.SourceKind = InputSourceWorkflowRecord
			input.Source = InputSource{
				Kind:               InputSourceWorkflowRecord,
				WorkflowReference:  goldenRef(operation.WorkflowReferenceKinds[0], "missing"),
				SnapshotArtifactID: "artifact-unrepresented",
				SnapshotSHA256:     strings.Repeat("6", 64),
			}
			if _, err := NewSnapshot(document); validationCode(err) != "workflow_record_reference" {
				t.Fatalf("%s unrepresented workflow record error = %v", operation.OperationID, err)
			}
			return
		}
	}
	t.Fatal("registry has no workflow-record slot with packet workflow authority")
}

func TestPathIdentityBindsDomainSeparatedRawBytes(t *testing.T) {
	cases := [][]byte{
		{},
		[]byte("internal/example.go"),
		{0xff, 0xfe, 'x'},
		bytes.Repeat([]byte{'x'}, 8192),
	}
	for _, raw := range cases {
		value := pathFromBytes(raw)
		if err := validatePathIdentity(value); err != nil {
			t.Fatalf("valid path length %d: %v", len(raw), err)
		}
	}

	badDigest := pathFromBytes([]byte("internal/example.go"))
	badDigest.PathID = strings.Repeat("0", 64)
	if err := validatePathIdentity(badDigest); validationCode(err) != "path_id_mismatch" {
		t.Fatalf("bad path digest error = %v", err)
	}

	nul := pathFromBytes([]byte{'a', 0, 'b'})
	if err := validatePathIdentity(nul); validationCode(err) != "path_bytes_nul" {
		t.Fatalf("NUL path error = %v", err)
	}

	badBase64 := pathFromBytes([]byte("abc"))
	badBase64.PathBytesBase64 = "YWJj===="
	if err := validatePathIdentity(badBase64); validationCode(err) != "path_bytes_base64" {
		t.Fatalf("noncanonical base64 error = %v", err)
	}

	long := pathFromBytes(bytes.Repeat([]byte{'x'}, 8193))
	long.PathBytesBase64 = ""
	if err := validatePathIdentity(long); err != nil {
		t.Fatalf("long omitted path bytes: %v", err)
	}
	long.PathBytesBase64 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'x'}, 8193))
	if err := validatePathIdentity(long); validationCode(err) != "path_bytes_oversize" {
		t.Fatalf("oversize inline path error = %v", err)
	}
}

func TestCanonicalPacketConcurrentConstructionAndDefensiveCopies(t *testing.T) {
	operation, ok := registry.Lookup("auditor.audit")
	if !ok {
		t.Fatal("auditor.audit is missing")
	}
	document := goldenDocument(t, operation)
	baseline, err := NewSnapshot(document)
	if err != nil {
		t.Fatal(err)
	}
	bytesCopy := baseline.Bytes()
	bytesCopy[0] = '['
	documentCopy := baseline.Document()
	documentCopy.Repositories[0].RepositoryKey = "mutated"
	if baseline.Bytes()[0] != '{' || baseline.Document().Repositories[0].RepositoryKey == "mutated" {
		t.Fatal("snapshot exposed mutable state")
	}
	const workers = 64
	var wait sync.WaitGroup
	errorsOut := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := NewSnapshot(document)
			if err != nil {
				errorsOut <- err
				return
			}
			if value.SHA256() != baseline.SHA256() || string(value.Bytes()) != string(baseline.Bytes()) {
				errorsOut <- invalid("concurrent_identity")
			}
		}()
	}
	wait.Wait()
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
}

func goldenDocument(t *testing.T, op registry.OperationDefinition) Document {
	manifest, ok := registry.SurfaceManifestSHA256(op.SurfaceContract)
	if !ok {
		t.Fatal("manifest")
	}
	d := Document{
		SchemaVersion: SchemaVersion,
		CreatedAt:     "2026-07-15T16:04:05.123456789Z",
		Role:          op.Role, OperationID: op.OperationID, SurfaceContract: op.SurfaceContract,
		SurfaceManifestSHA256: manifest,
		Output:                OutputContract{OutputKind: op.OutputKind, OutputPersistence: op.OutputPersistence},
		Project:               ProjectBinding{ProjectID: "project-golden"},
		Repositories:          []RepositoryBinding{{RepositoryKey: "relay", RepositoryTarget: "relay", BindingOrder: 1, RevisionSource: RevisionSourceExplicitCommit, RepositoryTargetConfigurationVersion: 1, CommitOID: strings.Repeat("1", 40), TreeOID: strings.Repeat("2", 40)}},
		RelaySpecs:            GovernanceBinding{RepositoryKey: "relay-specs", RepositoryTarget: "relay-specs", Reserved: true, RevisionSource: RevisionSourceExplicitCommit, RepositoryTargetConfigurationVersion: 1, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40)},
		ManifestDomain:        ManifestDomainBinding{ManifestPath: goldenPath("auditor-source-manifest.json"), ManifestBlobOID: strings.Repeat("c", 40), ManifestSHA256: strings.Repeat("d", 64), Domain: op.ManifestDomain, Members: []ManifestMember{{MemberOrder: 1, Path: goldenPath("contracts/cross-cutting.md"), BlobOID: strings.Repeat("e", 40), ByteSize: 123, SHA256: strings.Repeat("f", 64)}}},
		SourcePolicy:          op.SourcePolicy, HistoricalAuthority: op.HistoricalAuthority,
		AllowedActions: append([]registry.AllowedAction(nil), op.AllowedNonSourceActions...), ReadinessState: ReadinessReady,
	}
	for _, kind := range op.WorkflowReferenceKinds {
		d.WorkflowReferences = append(d.WorkflowReferences, goldenRef(kind, "1"))
	}
	required := requiredPurposes(op.HistoricalAuthority)
	for i, p := range required {
		d.Repositories[0].Anchors = append(d.Repositories[0].Anchors, Anchor{AnchorName: fmt.Sprintf("anchor-%02d", i+1), Purpose: p, CommitOID: strings.Repeat("3", 40), TreeOID: strings.Repeat("4", 40)})
	}
	fileIndex := int64(0)
	for i, slot := range op.RequiredInputs {
		input := goldenInput(slot, fileIndex, d.WorkflowReferences, i)
		if input.SourceKind == InputSourceUploadedFile {
			fileIndex++
		}
		d.Inputs = append(d.Inputs, input)
		d.Attestations = append(d.Attestations, goldenAtt(slot, input))
		if input.SourceKind != InputSourceCommittedSource {
			d.Attestations = append(d.Attestations, goldenClearance(input))
		}
	}
	for i, slot := range op.DerivedInputs {
		d.Inputs = append(d.Inputs, goldenDerived(slot, i))
	}
	return d
}
func goldenRef(kind registry.WorkflowReferenceKind, suffix string) WorkflowReference {
	switch kind {
	case "plan":
		return WorkflowReference{Kind: kind, PlanID: "plan-" + suffix, CanonicalArtifactID: "artifact-plan-" + suffix, CanonicalArtifactSHA256: strings.Repeat("1", 64)}
	case "pass":
		return WorkflowReference{Kind: kind, PlanID: "plan-" + suffix, PassID: "pass-" + suffix, PassNumber: 1}
	case "run":
		return WorkflowReference{Kind: kind, RunID: "run-" + suffix, ExecutionSpecArtifactID: "artifact-spec-" + suffix, ExecutionSpecSHA256: strings.Repeat("2", 64)}
	case "audit_packet":
		return WorkflowReference{Kind: kind, RunID: "run-" + suffix, AuditPacketID: "audit-packet-" + suffix, AuditPacketSHA256: strings.Repeat("3", 64)}
	case "audit_decision":
		return WorkflowReference{Kind: kind, RunID: "run-" + suffix, AuditDecisionID: "audit-decision-" + suffix, Decision: "needs_revision", RecordedAt: "2026-07-15T16:04:05.123456789Z"}
	}
	return WorkflowReference{Kind: kind}
}
func goldenInput(slot registry.InputSlotDefinition, fileIndex int64, refs []WorkflowReference, index int) InputBinding {
	kind := registry.InputSourceKind("")
	pref := []registry.InputSourceKind{InputSourceCommittedSource, InputSourceRelayArtifact, InputSourceInlineText, InputSourceUploadedFile, InputSourceWorkflowRecord}
	for _, p := range pref {
		for _, a := range slot.AllowedSourceKinds {
			if p == a {
				kind = p
				break
			}
		}
		if kind != "" {
			break
		}
	}
	source := InputSource{Kind: kind}
	switch kind {
	case InputSourceCommittedSource:
		source.RepositoryBindingID = "binding-relay"
		source.CommitOID = strings.Repeat("5", 40)
		source.TreeOID = strings.Repeat("6", 40)
		source.Path = goldenPath("internal/example.go")
		source.BlobOID = strings.Repeat("7", 40)
	case InputSourceRelayArtifact, InputSourceInlineText:
		source.ArtifactID = "artifact-" + slot.InputName
	case InputSourceUploadedFile:
		source.FileIndex = fileIndex
		source.ArtifactID = fmt.Sprintf("artifact-upload-%d", fileIndex)
	case InputSourceWorkflowRecord:
		source.WorkflowReference = refForPolicy(slot.WorkflowRecordPolicy, refs)
		source.SnapshotArtifactID = "artifact-snapshot-" + slot.InputName
		source.SnapshotSHA256 = strings.Repeat("4", 64)
	}
	return InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: kind, DisplayName: slot.InputName, MediaType: "application/octet-stream", SHA256: fmt.Sprintf("%064x", index+1), SizeBytes: int64(index + 1), AttestationKind: slot.AttestationKind, Source: source}
}
func goldenDerived(slot registry.InputSlotDefinition, index int) InputBinding {
	return InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: InputSourceRelayArtifact, DisplayName: slot.InputName, MediaType: "application/octet-stream", SHA256: fmt.Sprintf("%064x", 100+index), SizeBytes: int64(index + 1), AttestationKind: slot.AttestationKind, Source: InputSource{Kind: InputSourceRelayArtifact, ArtifactID: "artifact-derived-" + slot.InputName}}
}
func goldenAtt(slot registry.InputSlotDefinition, input InputBinding) Attestation {
	a := Attestation{Kind: slot.AttestationKind, InputName: slot.InputName}
	switch slot.AttestationKind {
	case "confirmed_intent":
		a.SubjectSHA256 = input.SHA256
		a.Confirmed = true
	case "approved_artifact":
		a.SubjectSHA256 = input.SHA256
		a.Approved = true
	case "candidate_for_review":
		a.SubjectSHA256 = input.SHA256
		a.CompleteTransfer = true
	case "execution_mode_selection":
		a.SelectedMode = "plan"
	case "complete_review_result":
		a.SubjectSHA256 = input.SHA256
		a.ReviewedCandidateSHA256 = strings.Repeat("9", 64)
		a.ReviewResult = "ready_for_approval"
		a.Complete = true
	case "completed_dependency_outcomes", "exact_evidence":
		a.SubjectSHA256 = input.SHA256
		a.Complete = true
	case "operator_confirmation", "separate_session_authorship":
		a.Confirmed = true
	}
	return a
}
func goldenClearance(input InputBinding) Attestation {
	return Attestation{Kind: "sensitive_data_clearance", InputName: input.InputName, Clearance: &SensitiveDataClearance{PolicyVersion: "relay.canonical-artifact-sensitive-data.v1", SubjectSHA256: input.SHA256, Confirmed: true}}
}
func refForPolicy(policy string, refs []WorkflowReference) WorkflowReference {
	desired := registry.WorkflowReferenceKind("plan")
	switch policy {
	case "pass_or_artifact":
		desired = "pass"
	case "run_execution_spec":
		desired = "run"
	case "audit_packet":
		for _, r := range refs {
			if r.Kind == "run" {
				return WorkflowReference{Kind: "audit_packet", RunID: r.RunID, AuditPacketID: "audit-packet-1", AuditPacketSHA256: strings.Repeat("3", 64)}
			}
		}
	case "audit_decision":
		desired = "audit_decision"
	}
	for _, r := range refs {
		if r.Kind == desired {
			return r
		}
	}
	for _, r := range refs {
		return r
	}
	return WorkflowReference{}
}
func requiredPurposes(policy registry.HistoricalAuthorityPolicy) []registry.AnchorPurpose {
	switch policy {
	case "plan_and_completed_dependency_anchors":
		return []registry.AnchorPurpose{"plan_base"}
	case "reviewed_commits", "reviewed_source_basis", "candidate_base_anchor":
		return []registry.AnchorPurpose{"reviewed_source_basis"}
	case "candidate_plan_and_dependency_anchors":
		return []registry.AnchorPurpose{"reviewed_source_basis", "plan_base"}
	case "run_base_and_audited_commit", "audited_and_run_base_anchors":
		return []registry.AnchorPurpose{"run_base", "audited_commit"}
	case "candidate_audited_and_run_base_anchors":
		return []registry.AnchorPurpose{"reviewed_source_basis", "run_base", "audited_commit"}
	}
	return nil
}
func goldenPath(s string) PathIdentity {
	b := []byte(s)
	h := sha256.New()
	h.Write([]byte("relay.git-path.v1"))
	h.Write([]byte{0})
	h.Write(b)
	return PathIdentity{PathID: hex.EncodeToString(h.Sum(nil)), ByteLength: int64(len(b)), PathBytesBase64: base64.StdEncoding.EncodeToString(b)}
}

func requiredInputIndex(operation registry.OperationDefinition, slotIndex int) int {
	return slotIndex
}

func pathFromBytes(raw []byte) PathIdentity {
	hash := sha256.New()
	_, _ = hash.Write([]byte("relay.git-path.v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(raw)
	return PathIdentity{
		PathID:          hex.EncodeToString(hash.Sum(nil)),
		ByteLength:      int64(len(raw)),
		PathBytesBase64: base64.StdEncoding.EncodeToString(raw),
	}
}

func validationCode(err error) string {
	value, ok := err.(*ValidationError)
	if !ok {
		return ""
	}
	return value.Code
}
