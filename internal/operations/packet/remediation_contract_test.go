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
	tests := []struct {
		name      string
		reference WorkflowReference
		want      string
	}{
		{"feature_workspace", WorkflowReference{Kind: "feature_workspace", WorkspaceID: "workspace-1", WorkspaceVersion: 2, RouteStateID: "route-1", RouteSequence: 3, RouteWorkspaceVersion: 1, RouteState: "ready"}, `{"kind":"feature_workspace","workspace_id":"workspace-1","workspace_version":2,"route_state_id":"route-1","route_sequence":3,"route_workspace_version":1,"route_state":"ready"}`},
		{"delivery_ticket", WorkflowReference{Kind: "delivery_ticket", WorkspaceID: "workspace-1", TicketID: "ticket-1", RevisionID: 7, RevisionNumber: 2, SourceClosureID: "closure-1"}, `{"kind":"delivery_ticket","workspace_id":"workspace-1","ticket_id":"ticket-1","revision_id":7,"revision_number":2,"source_closure_id":"closure-1"}`},
		{"run", WorkflowReference{Kind: "run", RunID: "run-1", ExecutionSpecArtifactID: "artifact-1", ExecutionSpecSHA256: strings.Repeat("a", 64)}, `{"kind":"run","run_id":"run-1","execution_spec_artifact_id":"artifact-1","execution_spec_sha256":"` + strings.Repeat("a", 64) + `"}`},
		{"audit_decision", WorkflowReference{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "decision-1", Decision: "accepted", RecordedAt: "2026-07-15T16:04:05.123456789Z"}, `{"kind":"audit_decision","run_id":"run-1","audit_decision_id":"decision-1","decision":"accepted","recorded_at":"2026-07-15T16:04:05.123456789Z"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateWorkflowReference(test.reference); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			writeWorkflowReference(&output, test.reference)
			if output.String() != test.want {
				t.Fatalf("canonical reference = %s, want %s", output.String(), test.want)
			}
		})
	}
}

func TestDerivedInputSourceIntegrity(t *testing.T) {
	for _, operationID := range []registry.OperationID{"auditor.audit", "planner.delivery_ticket_remediation", "planner.ticket_design_brief_remediation"} {
		t.Run(string(operationID), func(t *testing.T) {
			operation, ok := registry.Lookup(operationID)
			if !ok {
				t.Fatalf("%s is missing", operationID)
			}
			document := goldenDocument(t, operation)
			if operationID == "planner.delivery_ticket_remediation" {
				if len(document.WorkflowReferences) != 1 || document.WorkflowReferences[0].Kind != "audit_decision" || len(operation.DerivedInputs) != 2 {
					t.Fatalf("remediation packet authority = %#v, derived inputs = %d", document.WorkflowReferences, len(operation.DerivedInputs))
				}
			}
			if operationID == "planner.ticket_design_brief_remediation" {
				if len(document.WorkflowReferences) != 1 || document.WorkflowReferences[0].Kind != "audit_decision" || len(operation.DerivedInputs) != 4 {
					t.Fatalf("remediation brief authority = %#v, derived inputs = %d", document.WorkflowReferences, len(operation.DerivedInputs))
				}
			}
			if _, err := NewSnapshot(document); err != nil {
				t.Fatalf("canonical inline derived inputs rejected: %v", err)
			}

			for _, sourceKind := range []registry.InputSourceKind{InputSourceRelayArtifact, InputSourceUploadedFile, InputSourceWorkflowRecord, InputSourceCommittedSource} {
				t.Run(string(sourceKind), func(t *testing.T) {
					for _, slot := range operation.DerivedInputs {
						candidate := goldenDocument(t, operation)
						input := inputByName(candidate.Inputs, slot.InputName)
						input.SourceKind = sourceKind
						input.Source = derivedSource(sourceKind, document.WorkflowReferences)
						if _, err := NewSnapshot(candidate); validationCode(err) != "input_source_not_allowed" {
							t.Fatalf("%s source for %s error = %v", sourceKind, slot.InputName, err)
						}
					}
				})
			}

			candidate := goldenDocument(t, operation)
			input := inputByName(candidate.Inputs, operation.DerivedInputs[0].InputName)
			input.Source.FileIndex = 0
			input.Source.SnapshotArtifactID = "inactive-artifact"
			if _, err := NewSnapshot(candidate); validationCode(err) != "input_source_closed" {
				t.Fatalf("inline source with inactive fields error = %v", err)
			}
		})
	}
}

func TestRemediationHistoricalAuthorityPolicy(t *testing.T) {
	for _, policy := range []registry.HistoricalAuthorityPolicy{"audited_ticket_and_current_authority", "remediation_ticket_and_current_authority"} {
		required, allowed, err := historicalAnchorPolicy(policy)
		if err != nil || len(required) != 0 || len(allowed) != 0 {
			t.Fatalf("%s policy = required %v, allowed %v, err %v", policy, required, allowed, err)
		}
	}
	if _, _, err := historicalAnchorPolicy("unknown_remediation_policy"); validationCode(err) != "historical_authority" {
		t.Fatalf("unknown policy error = %v", err)
	}

	op, ok := registry.Lookup("planner.ticket_design_brief_remediation")
	if !ok {
		t.Fatal("remediation brief operation is missing")
	}
	document := goldenDocument(t, op)
	if _, err := NewSnapshot(document); err != nil {
		t.Fatalf("remediation brief without anchors rejected: %v", err)
	}
	document.Repositories[0].Anchors = []Anchor{{AnchorName: "undeclared", Purpose: "reviewed_source_basis", CommitOID: strings.Repeat("3", 40), TreeOID: strings.Repeat("4", 40)}}
	if _, err := NewSnapshot(document); validationCode(err) != "repository_anchor_purpose" {
		t.Fatalf("undeclared remediation anchor error = %v", err)
	}
}

func TestWorkflowReferenceMultiplicityAndRelationships(t *testing.T) {
	operation, ok := registry.Lookup("planner.transition_plan")
	if !ok {
		t.Fatal("transition-plan operation is missing")
	}
	duplicate := goldenDocument(t, operation)
	duplicate.WorkflowReferences = append(duplicate.WorkflowReferences, goldenRef("feature_workspace", "2"))
	if _, err := NewSnapshot(duplicate); validationCode(err) != "workflow_reference_duplicate" {
		t.Fatalf("duplicate kind error = %v", err)
	}

	mismatchedTicket := goldenDocument(t, operation)
	for index := range mismatchedTicket.WorkflowReferences {
		if mismatchedTicket.WorkflowReferences[index].Kind == "delivery_ticket" {
			mismatchedTicket.WorkflowReferences[index].WorkspaceID = "workspace-missing"
		}
	}
	if _, err := NewSnapshot(mismatchedTicket); validationCode(err) != "workflow_reference_relationship" {
		t.Fatalf("mismatched workspace/ticket error = %v", err)
	}

	mismatchedDecision := []WorkflowReference{goldenRef("run", "1"), goldenRef("audit_decision", "2")}
	if err := validateWorkflowReferenceRelationships(mismatchedDecision); validationCode(err) != "workflow_reference_relationship" {
		t.Fatalf("mismatched run/decision error = %v", err)
	}
}

func TestWorkflowRecordIsValidatedIndependentlyFromPacketReferences(t *testing.T) {
	operations, err := registry.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		for slotIndex, slot := range operation.RequiredInputs {
			if !containsSourceKind(slot.AllowedSourceKinds, InputSourceWorkflowRecord) {
				continue
			}
			document := goldenDocument(t, operation)
			inputIndex := requiredInputIndex(operation, slotIndex)
			input := &document.Inputs[inputIndex]
			input.SourceKind = InputSourceWorkflowRecord
			input.Source = InputSource{
				Kind:               InputSourceWorkflowRecord,
				WorkflowReference:  WorkflowRecordReference{Kind: "plan_artifact", PlanID: "plan-independent", ArtifactID: "artifact-independent", ArtifactSHA256: strings.Repeat("6", 64)},
				SnapshotArtifactID: "artifact-unrepresented",
				SnapshotSHA256:     strings.Repeat("6", 64),
			}
			if _, err := NewSnapshot(document); err != nil {
				t.Fatalf("%s independent workflow record error = %v", operation.OperationID, err)
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
	manifest, ok := registry.RouteContractSHA256(op.SurfaceContract)
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
	if op.Role == "wayfinder" && op.ManifestDomain == "" {
		d.RelaySpecs = GovernanceBinding{}
		d.ManifestDomain = ManifestDomainBinding{}
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
	case "feature_workspace":
		return WorkflowReference{Kind: kind, WorkspaceID: "workspace-" + suffix, WorkspaceVersion: 2, RouteStateID: "route-" + suffix, RouteSequence: 1, RouteWorkspaceVersion: 1, RouteState: "ready"}
	case "delivery_ticket":
		return WorkflowReference{Kind: kind, WorkspaceID: "workspace-" + suffix, TicketID: "ticket-" + suffix, RevisionID: 1, RevisionNumber: 1, SourceClosureID: "closure-" + suffix}
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
	return InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: InputSourceInlineText, DisplayName: slot.InputName + ".json", MediaType: "application/json", SHA256: fmt.Sprintf("%064x", 100+index), SizeBytes: int64(index + 1), AttestationKind: slot.AttestationKind, Source: InputSource{Kind: InputSourceInlineText, ArtifactID: "artifact-derived-" + slot.InputName}}
}

func derivedSource(kind registry.InputSourceKind, references []WorkflowReference) InputSource {
	source := InputSource{Kind: kind, ArtifactID: "artifact-derived-test"}
	switch kind {
	case InputSourceUploadedFile:
		source.FileIndex = 0
		source.ArtifactID = "artifact-uploaded-test"
	case InputSourceWorkflowRecord:
		source.WorkflowReference = WorkflowRecordReference{Kind: "plan_artifact", PlanID: "plan-derived", ArtifactID: "artifact-derived", ArtifactSHA256: strings.Repeat("4", 64)}
		source.SnapshotArtifactID = "artifact-snapshot-test"
		source.SnapshotSHA256 = strings.Repeat("4", 64)
	case InputSourceCommittedSource:
		source.RepositoryBindingID = "binding-relay"
		source.CommitOID = strings.Repeat("5", 40)
		source.TreeOID = strings.Repeat("6", 40)
		source.Path = goldenPath("internal/example.go")
		source.BlobOID = strings.Repeat("7", 40)
	}
	return source
}

func inputByName(inputs []InputBinding, name string) *InputBinding {
	for index := range inputs {
		if inputs[index].InputName == name {
			return &inputs[index]
		}
	}
	panic("input missing: " + name)
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
func refForPolicy(policy string, refs []WorkflowReference) WorkflowRecordReference {
	switch policy {
	case "pass_or_artifact":
		return WorkflowRecordReference{Kind: "pass_record", PlanID: "plan-1", PassID: "pass-1", PassNumber: 1}
	case "run_execution_spec":
		return WorkflowRecordReference{Kind: "run_execution_spec", RunID: "run-1", ArtifactID: "artifact-spec-1", ArtifactSHA256: strings.Repeat("2", 64)}
	case "audit_packet":
		return WorkflowRecordReference{Kind: "audit_packet", RunID: "run-1", AuditPacketID: "audit-packet-1", AuditPacketSHA256: strings.Repeat("3", 64)}
	case "audit_decision":
		return WorkflowRecordReference{Kind: "audit_decision", RunID: "run-1", AuditDecisionID: "audit-decision-1", Decision: "needs_revision", RecordedAt: "2026-07-15T16:04:05.123456789Z"}
	case "artifact", "plan_artifact":
		return WorkflowRecordReference{Kind: "plan_artifact", PlanID: "plan-1", ArtifactID: "artifact-plan-1", ArtifactSHA256: strings.Repeat("1", 64)}
	}
	return WorkflowRecordReference{}
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
