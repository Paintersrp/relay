package mcp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	operations "relay/internal/app/operations"
	"relay/internal/operations/registry"
)

func TestOperationPacketViewUsesExactApplicationBytesAndLifecycle(t *testing.T) {
	supersededAt := "2026-07-15T16:04:06.000000000Z"
	application := operations.PacketView{
		Summary: operations.PacketSummary{
			PacketID: "opkt-prior", PacketSHA256: strings.Repeat("a", 64), SchemaVersion: "relay.operation-packet.v1",
			Role: registry.Role("planner"), OperationID: registry.OperationID("planner.requirements"), SurfaceContract: registry.SurfaceContractID("planner-authoring.v1"), ProjectID: "project-1", ReadinessState: "ready", LifecycleState: "superseded",
			ReplacementPacket: &operations.ReplacementPacketIdentity{PacketID: "opkt-replacement", PacketSHA256: strings.Repeat("b", 64), Role: registry.Role("planner"), OperationID: registry.OperationID("planner.requirements"), SurfaceContract: registry.SurfaceContractID("planner-authoring.v1")}, SupersededAt: &supersededAt,
		},
		DocumentMediaType: "application/vnd.relay.operation-packet+json;version=1", DocumentSizeBytes: 2, DocumentBytes: []byte("{}"),
	}
	view := OperationPacketViewFromApplication(application)
	if view.DocumentBytesBase64 != base64.StdEncoding.EncodeToString(application.DocumentBytes) || view.DocumentSizeBytes != 2 || view.Summary.ReplacementPacket == nil || view.Summary.SupersededAt == nil || view.Summary.ClosedAt != nil {
		t.Fatalf("unexpected packet view: %+v", view)
	}
	application.DocumentBytes[0] = '['
	application.Summary.ReplacementPacket.PacketID = "mutated"
	supersededAt = "mutated"
	if view.DocumentBytesBase64 != "e30=" || view.Summary.ReplacementPacket.PacketID != "opkt-replacement" || *view.Summary.SupersededAt != "2026-07-15T16:04:06.000000000Z" {
		t.Fatal("packet conversion retained mutable application aliases")
	}
}

func TestOperationPacketSummaryEmitsRequiredNullableFields(t *testing.T) {
	summary := OperationPacketSummaryFromApplication(operations.PacketSummary{PacketID: "opkt-closed", PacketSHA256: strings.Repeat("c", 64), SchemaVersion: "relay.operation-packet.v1", Role: registry.Role("auditor"), OperationID: registry.OperationID("auditor.audit"), SurfaceContract: registry.SurfaceContractID("auditor-audit.v1"), ProjectID: "project-1", ReadinessState: "ready", LifecycleState: "closed"})
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"replacement_packet":null`, `"superseded_at":null`, `"closed_at":null`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("required nullable field %s is missing", field)
		}
	}
}
