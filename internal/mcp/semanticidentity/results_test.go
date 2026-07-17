package semanticidentity

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"relay/internal/operations/registry"
)

type rogueResultIdentity struct {
	Kind string `json:"kind"`
	Body []byte `json:"artifact_body"`
}

func (rogueResultIdentity) resultIdentity() {}
func (rogueResultIdentity) MutationTool() registry.MutationTool {
	return registry.MutationToolSubmitPlan
}
func (rogueResultIdentity) ResultKind() ResultKind {
	return ResultKindSubmitPlan
}

func TestSixExactResultIdentitiesEncodeDecodeAndReconstruct(t *testing.T) {
	for _, test := range validResultIdentities() {
		encoded, err := EncodeResultIdentity(test.surface, test.tool, test.identity)
		if err != nil {
			t.Fatalf("%s: %v", test.tool, err)
		}
		if encoded.Kind != ResultKindForTool(test.tool) || len(encoded.JSON) == 0 || len(encoded.JSON) > MaxResultIdentityBytes || !validSHA256(encoded.SHA256) {
			t.Fatalf("%s encoded = %#v", test.tool, encoded)
		}
		decoded, err := DecodeResultIdentity(test.surface, test.tool, encoded.Kind, encoded.JSON)
		if err != nil {
			t.Fatalf("%s decode: %v", test.tool, err)
		}
		reencoded, err := EncodeResultIdentity(test.surface, test.tool, decoded)
		if err != nil {
			t.Fatalf("%s reencode: %v", test.tool, err)
		}
		if reencoded.Kind != encoded.Kind || reencoded.SHA256 != encoded.SHA256 || !bytes.Equal(reencoded.JSON, encoded.JSON) {
			t.Fatalf("%s replay identity drifted", test.tool)
		}
	}
}

func TestResultIdentityRejectsArbitraryCrossToolCrossSurfaceAndBodies(t *testing.T) {
	if _, err := EncodeResultIdentity("planner-plan.v1", registry.MutationToolSubmitPlan, rogueResultIdentity{Kind: "submit_plan_result", Body: []byte("forbidden")}); err == nil {
		t.Fatal("arbitrary result identity was accepted")
	}
	submit := validSubmitPlanResult()
	if _, err := EncodeResultIdentity("planner-execution.v1", registry.MutationToolSubmitPlan, submit); err == nil {
		t.Fatal("cross-surface result was accepted")
	}
	if _, err := EncodeResultIdentity("planner-plan.v1", registry.MutationToolCreateRun, submit); err == nil {
		t.Fatal("cross-tool result was accepted")
	}
	encoded, err := EncodeResultIdentity("planner-plan.v1", registry.MutationToolSubmitPlan, submit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeResultIdentity("planner-plan.v1", registry.MutationToolSubmitPlan, ResultKindCreateRun, encoded.JSON); err == nil {
		t.Fatal("wrong result kind was accepted")
	}
	extra := append(encoded.JSON[:len(encoded.JSON)-1], []byte(`,"artifact_body":"forbidden"}`)...)
	if _, err := DecodeResultIdentity("planner-plan.v1", registry.MutationToolSubmitPlan, encoded.Kind, extra); err == nil {
		t.Fatal("extra result body was accepted")
	}
	oversized := submit
	oversized.WorkflowState = strings.Repeat("x", 4097)
	if _, err := EncodeResultIdentity("planner-plan.v1", registry.MutationToolSubmitPlan, oversized); err == nil {
		t.Fatal("oversized result identity was accepted")
	}
	run := validCreateRunResult()
	run.BaseRepositories = append(run.BaseRepositories, make([]BaseRepositoryIdentity, 64)...)
	if _, err := EncodeResultIdentity("planner-execution.v1", registry.MutationToolCreateRun, run); err == nil {
		t.Fatal("oversized repository identity was accepted")
	}
}

func TestExactResultTypesContainNoBodyOrByteSliceFields(t *testing.T) {
	for _, value := range []any{
		CreateOperationPacketResult{},
		RefreshOperationPacketResult{},
		CloseOperationPacketResult{},
		SubmitPlanResult{},
		CreateRunResult{},
		RecordAuditDecisionResult{},
	} {
		if path, ok := forbiddenResultField(reflect.TypeOf(value), ""); ok {
			t.Fatalf("%T contains forbidden result field %s", value, path)
		}
	}
}

func TestEveryPublicSuccessIdentityFieldChangesBytesOrFailsClosed(t *testing.T) {
	submit := validSubmitPlanResult()
	assertResultChanges(t, "planner-plan.v1", registry.MutationToolSubmitPlan, submit,
		func(v *SubmitPlanResult) { v.PlanID = "plan-2" },
		func(v *SubmitPlanResult) { v.ArtifactID = "artifact-2" },
		func(v *SubmitPlanResult) { v.ArtifactSHA256 = strings.Repeat("b", 64) },
		func(v *SubmitPlanResult) { v.ProjectID = "project-2" },
		func(v *SubmitPlanResult) { v.SubmissionID = "submission-2" },
		func(v *SubmitPlanResult) { v.WorkflowState = "submitted" },
	)

	run := validCreateRunResult()
	assertResultChanges(t, "planner-execution.v1", registry.MutationToolCreateRun, run,
		func(v *CreateRunResult) { v.RunID = "run-2" },
		func(v *CreateRunResult) { v.ArtifactID = "artifact-2" },
		func(v *CreateRunResult) { v.ArtifactSHA256 = strings.Repeat("b", 64) },
		func(v *CreateRunResult) { v.OperationID = "planner.one_shot_execution_spec" },
		func(v *CreateRunResult) { v.ProjectID = "project-2" },
		func(v *CreateRunResult) { v.BaseRepositories[0].RepositoryKey = "other" },
		func(v *CreateRunResult) { v.BaseRepositories[0].CommitOID = strings.Repeat("d", 40) },
		func(v *CreateRunResult) { v.InitialState = "setup_ready" },
	)

	audit := validAuditResult()
	assertResultChanges(t, "auditor-audit.v1", registry.MutationToolRecordAuditDecision, audit,
		func(v *RecordAuditDecisionResult) { v.AuditDecisionID = "decision-2" },
		func(v *RecordAuditDecisionResult) { v.AuditPacketID = "audit-2" },
		func(v *RecordAuditDecisionResult) { v.AuditPacketSHA256 = strings.Repeat("b", 64) },
		func(v *RecordAuditDecisionResult) { v.AuditedCommitOID = strings.Repeat("d", 40) },
		func(v *RecordAuditDecisionResult) { v.Decision = "needs_revision" },
		func(v *RecordAuditDecisionResult) { v.RunID = "run-2" },
		func(v *RecordAuditDecisionResult) { v.RecordedAt = "2026-07-16T01:02:04Z" },
	)

	create := validCreatePacketResult()
	assertResultChanges(t, "planner-authoring.v1", registry.MutationToolCreateOperationPacket, create,
		func(v *CreateOperationPacketResult) { v.Packet.Summary.PacketID = "packet-2" },
		func(v *CreateOperationPacketResult) {
			v.Packet.Summary.PacketSHA256 = strings.Repeat("b", 64)
			v.Packet.Document.SHA256 = strings.Repeat("b", 64)
		},
		func(v *CreateOperationPacketResult) { v.Packet.Document.ArtifactID = "artifact-2" },
		func(v *CreateOperationPacketResult) { v.Packet.Document.SizeBytes = 2 },
		func(v *CreateOperationPacketResult) { v.SurfaceManifestSHA256 = strings.Repeat("b", 64) },
	)
}

func validResultIdentities() []struct {
	surface  registry.SurfaceContractID
	tool     registry.MutationTool
	identity ResultIdentity
} {
	return []struct {
		surface  registry.SurfaceContractID
		tool     registry.MutationTool
		identity ResultIdentity
	}{
		{"planner-authoring.v1", registry.MutationToolCreateOperationPacket, validCreatePacketResult()},
		{"planner-authoring.v1", registry.MutationToolRefreshOperationPacket, validRefreshPacketResult()},
		{"planner-authoring.v1", registry.MutationToolCloseOperationPacket, validClosePacketResult()},
		{"planner-plan.v1", registry.MutationToolSubmitPlan, validSubmitPlanResult()},
		{"planner-execution.v1", registry.MutationToolCreateRun, validCreateRunResult()},
		{"auditor-audit.v1", registry.MutationToolRecordAuditDecision, validAuditResult()},
	}
}

func validCreatePacketResult() CreateOperationPacketResult {
	return CreateOperationPacketResult{
		Packet:                validPacketView(),
		SurfaceManifestSHA256: strings.Repeat("a", 64),
		Complete:              true,
	}
}

func validRefreshPacketResult() RefreshOperationPacketResult {
	prior := validPacketSummary()
	prior.PacketID = "packet-prior"
	return RefreshOperationPacketResult{
		PriorPacket:           prior,
		Packet:                validPacketView(),
		SurfaceManifestSHA256: strings.Repeat("a", 64),
		Complete:              true,
	}
}

func validClosePacketResult() CloseOperationPacketResult {
	summary := validPacketSummary()
	summary.LifecycleState = "closed"
	closed := "2026-07-16T01:02:03Z"
	summary.ClosedAt = &closed
	return CloseOperationPacketResult{Packet: summary, Complete: true}
}

func validPacketView() OperationPacketViewIdentity {
	return OperationPacketViewIdentity{
		Summary: validPacketSummary(),
		Document: PacketDocumentIdentity{
			ArtifactID: "artifact-packet-1",
			MediaType:  "application/vnd.relay.operation-packet+json;version=1",
			SizeBytes:  1,
			SHA256:     strings.Repeat("a", 64),
		},
	}
}

func validPacketSummary() PacketSummaryIdentity {
	return PacketSummaryIdentity{
		PacketID:          "packet-1",
		PacketSHA256:      strings.Repeat("a", 64),
		SchemaVersion:     "relay.operation-packet.v1",
		Role:              "planner",
		OperationID:       "planner.requirements",
		SurfaceContractID: "planner-authoring.v1",
		ProjectID:         "project-1",
		ReadinessState:    "ready",
		LifecycleState:    "active",
		ReplacementPacket: nil,
		SupersededAt:      nil,
		ClosedAt:          nil,
	}
}

func validSubmitPlanResult() SubmitPlanResult {
	return SubmitPlanResult{
		PlanID: "plan-1", ArtifactID: "artifact-1", ArtifactSHA256: strings.Repeat("a", 64),
		ProjectID: "project-1", SubmissionID: "submission-1", WorkflowState: "active", Complete: true,
	}
}

func validCreateRunResult() CreateRunResult {
	return CreateRunResult{
		RunID: "run-1", ArtifactID: "artifact-1", ArtifactSHA256: strings.Repeat("a", 64),
		OperationID: "planner.selected_pass_execution_spec", ProjectID: "project-1",
		BaseRepositories: []BaseRepositoryIdentity{{RepositoryKey: "relay", CommitOID: strings.Repeat("c", 40)}},
		InitialState:     "created", Complete: true,
	}
}

func validAuditResult() RecordAuditDecisionResult {
	return RecordAuditDecisionResult{
		AuditDecisionID: "decision-1", AuditPacketID: "audit-1", AuditPacketSHA256: strings.Repeat("a", 64),
		AuditedCommitOID: strings.Repeat("c", 40), Decision: "accepted", RunID: "run-1",
		RecordedAt: "2026-07-16T01:02:03Z", Complete: true,
	}
}

func assertResultChanges[T ResultIdentity](t *testing.T, surface registry.SurfaceContractID, tool registry.MutationTool, base T, edits ...func(*T)) {
	t.Helper()
	baseline, err := EncodeResultIdentity(surface, tool, base)
	if err != nil {
		t.Fatal(err)
	}
	for index, edit := range edits {
		candidate := deepCopy(t, base)
		edit(&candidate)
		changed, err := EncodeResultIdentity(surface, tool, candidate)
		if err != nil {
			continue
		}
		if changed.SHA256 == baseline.SHA256 || bytes.Equal(changed.JSON, baseline.JSON) {
			t.Fatalf("edit %d did not change result identity", index)
		}
	}
}

func forbiddenResultField(value reflect.Type, prefix string) (string, bool) {
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Slice:
		if value.Elem().Kind() == reflect.Uint8 {
			return prefix, true
		}
		return forbiddenResultField(value.Elem(), prefix+"[]")
	case reflect.Array:
		return forbiddenResultField(value.Elem(), prefix+"[]")
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			name := field.Name
			lower := strings.ToLower(name)
			if strings.Contains(lower, "body") || strings.Contains(lower, "url") || strings.Contains(lower, "header") || strings.Contains(lower, "credential") || strings.Contains(lower, "request") || strings.Contains(lower, "provider") {
				return prefix + "." + name, true
			}
			if path, ok := forbiddenResultField(field.Type, prefix+"."+name); ok {
				return path, true
			}
		}
	}
	return "", false
}
