package mcp

import (
	"encoding/base64"

	operations "relay/internal/app/operations"
)

type ReplacementPacketIdentity struct {
	PacketID        string `json:"packet_id"`
	PacketSHA256    string `json:"packet_sha256"`
	Role            string `json:"role"`
	OperationID     string `json:"operation_id"`
	SurfaceContract string `json:"surface_contract"`
}
type OperationPacketSummary struct {
	PacketID          string                     `json:"packet_id"`
	PacketSHA256      string                     `json:"packet_sha256"`
	SchemaVersion     string                     `json:"schema_version"`
	Role              string                     `json:"role"`
	OperationID       string                     `json:"operation_id"`
	SurfaceContract   string                     `json:"surface_contract"`
	ProjectID         string                     `json:"project_id"`
	ReadinessState    string                     `json:"readiness_state"`
	LifecycleState    string                     `json:"lifecycle_state"`
	ReplacementPacket *ReplacementPacketIdentity `json:"replacement_packet"`
	SupersededAt      *string                    `json:"superseded_at"`
	ClosedAt          *string                    `json:"closed_at"`
}
type OperationPacketView struct {
	Summary             OperationPacketSummary `json:"summary"`
	DocumentMediaType   string                 `json:"document_media_type"`
	DocumentSizeBytes   int64                  `json:"document_size_bytes"`
	DocumentBytesBase64 string                 `json:"document_bytes_base64"`
}

func OperationPacketSummaryFromApplication(value operations.PacketSummary) OperationPacketSummary {
	result := OperationPacketSummary{PacketID: value.PacketID, PacketSHA256: value.PacketSHA256, SchemaVersion: value.SchemaVersion, Role: string(value.Role), OperationID: string(value.OperationID), SurfaceContract: string(value.SurfaceContract), ProjectID: value.ProjectID, ReadinessState: value.ReadinessState, LifecycleState: value.LifecycleState, SupersededAt: cloneString(value.SupersededAt), ClosedAt: cloneString(value.ClosedAt)}
	if value.ReplacementPacket != nil {
		result.ReplacementPacket = &ReplacementPacketIdentity{PacketID: value.ReplacementPacket.PacketID, PacketSHA256: value.ReplacementPacket.PacketSHA256, Role: string(value.ReplacementPacket.Role), OperationID: string(value.ReplacementPacket.OperationID), SurfaceContract: string(value.ReplacementPacket.SurfaceContract)}
	}
	return result
}
func OperationPacketViewFromApplication(value operations.PacketView) OperationPacketView {
	return OperationPacketView{Summary: OperationPacketSummaryFromApplication(value.Summary), DocumentMediaType: value.DocumentMediaType, DocumentSizeBytes: value.DocumentSizeBytes, DocumentBytesBase64: base64.StdEncoding.EncodeToString(value.DocumentBytes)}
}
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
