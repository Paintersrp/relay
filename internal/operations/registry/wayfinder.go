package registry

import (
	"crypto/sha256"
	"encoding/hex"
)

type WayfinderRoleProfile struct {
	Role            Role
	SurfaceContract SurfaceContractID
	Operations      []OperationID
	ManifestSHA256  string
}

func WayfinderOperations() []OperationDefinition {
	ids := []OperationID{"wayfinder.workspace", "wayfinder.discovery", "wayfinder.investigation"}
	operations := make([]OperationDefinition, 0, len(ids))
	for _, id := range ids {
		if value, ok := LookupPublishedOperation(id); ok {
			operations = append(operations, publishedOperationAsLegacy(value))
		}
	}
	return operations
}

func WayfinderRoleProfiles() []WayfinderRoleProfile {
	operations := WayfinderOperations()
	roleProfiles := make([]WayfinderRoleProfile, len(operations))
	for i, op := range operations {
		roleProfiles[i] = WayfinderRoleProfile{Role: op.Role, SurfaceContract: op.SurfaceContract, Operations: []OperationID{op.OperationID}, ManifestSHA256: roleProfileSHA256(op.Role, op.SurfaceContract, op.OperationID)}
	}
	return roleProfiles
}

func roleProfileSHA256(role Role, surface SurfaceContractID, operation OperationID) string {
	sum := sha256.Sum256([]byte(string(role) + "\x00" + string(surface) + "\x00" + string(operation)))
	return hex.EncodeToString(sum[:])
}

func publishedOperationAsLegacy(value PublishedOperationDefinition) OperationDefinition {
	return OperationDefinition{
		OperationID: value.OperationID, Role: value.Role, SurfaceContract: value.SurfaceContract,
		ManifestDomain: value.ManifestDomain, OutputKind: value.OutputKind, OutputPersistence: value.OutputPersistence,
		RequiredInputs: clonePublishedSlots(value.RequiredInputs), ConditionalRefreshInputs: clonePublishedSlots(value.ConditionalRefreshInputs), DerivedInputs: clonePublishedSlots(value.DerivedInputs),
		WorkflowReferenceKinds: append([]WorkflowReferenceKind(nil), value.WorkflowReferenceKinds...), ComparisonAnchorPurposes: append([]AnchorPurpose(nil), value.ComparisonAnchorPurposes...),
		SourcePolicy: value.SourcePolicy, HistoricalAuthority: value.HistoricalAuthority, AllowedNonSourceActions: append([]AllowedAction(nil), value.AllowedNonSourceActions...), PacketSemanticProjection: value.PacketSemanticProjection,
	}
}
