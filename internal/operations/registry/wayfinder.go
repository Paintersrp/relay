package registry

import (
	"crypto/sha256"
	"encoding/hex"
)

// WayfinderRoleProfile is the closed inventory consumed by the Wayfinder and
// Features mutation surfaces.  It is deliberately separate from the legacy
// Planner/Auditor public-contract catalog: these operations are admitted by a
// packet before a durable owner is selected, not exposed as source tooling.
type WayfinderRoleProfile struct {
	Role            Role
	SurfaceContract SurfaceContractID
	Operations      []OperationID
	ManifestSHA256  string
}

var wayfinderOperations = []OperationDefinition{
	{OperationID: "wayfinder.workspace", Role: "wayfinder", SurfaceContract: "wayfinder-workspace.v1", ManifestDomain: "feature_workspace", OutputKind: "feature_workspace", OutputPersistence: "durable_workspace", SourcePolicy: "current_clean_project_required_source", HistoricalAuthority: "none", AllowedNonSourceActions: []AllowedAction{"create_workspace", "admit_workspace_input", "add_workspace_destination", "route_workspace"}, PacketSemanticProjection: "relay.semantic.wayfinder-mutation.v1"},
	{OperationID: "wayfinder.discovery", Role: "wayfinder", SurfaceContract: "wayfinder-discovery.v1", ManifestDomain: "feature_discovery", OutputKind: "feature_discovery", OutputPersistence: "durable_workspace", SourcePolicy: "current_clean_project_required_source", HistoricalAuthority: "none", AllowedNonSourceActions: []AllowedAction{"create_discovery_ticket", "resolve_discovery_ticket"}, PacketSemanticProjection: "relay.semantic.wayfinder-mutation.v1"},
	{OperationID: "wayfinder.investigation", Role: "wayfinder", SurfaceContract: "wayfinder-investigation.v1", ManifestDomain: "feature_investigation", OutputKind: "feature_investigation", OutputPersistence: "durable_workspace", SourcePolicy: "current_clean_project_required_source", HistoricalAuthority: "none", AllowedNonSourceActions: []AllowedAction{"attach_investigation"}, PacketSemanticProjection: "relay.semantic.wayfinder-mutation.v1"},
	{OperationID: "features.authority", Role: "features", SurfaceContract: "features-authority.v1", ManifestDomain: "feature_authority", OutputKind: "feature_authority_revision", OutputPersistence: "durable_workspace", SourcePolicy: "current_clean_project_required_source", HistoricalAuthority: "none", AllowedNonSourceActions: []AllowedAction{"publish_authority"}, PacketSemanticProjection: "relay.semantic.wayfinder-mutation.v1"},
}

// WayfinderOperations returns a defensive copy of the stable operation IDs
// and their packet-authorized mutation inventories.
func WayfinderOperations() []OperationDefinition {
	out := make([]OperationDefinition, len(wayfinderOperations))
	for i, operation := range wayfinderOperations {
		out[i] = cloneOperation(operation)
	}
	return out
}

func WayfinderRoleProfiles() []WayfinderRoleProfile {
	profiles := []WayfinderRoleProfile{
		{Role: "wayfinder", SurfaceContract: "wayfinder-workspace.v1", Operations: []OperationID{"wayfinder.workspace"}},
		{Role: "wayfinder", SurfaceContract: "wayfinder-discovery.v1", Operations: []OperationID{"wayfinder.discovery"}},
		{Role: "wayfinder", SurfaceContract: "wayfinder-investigation.v1", Operations: []OperationID{"wayfinder.investigation"}},
		{Role: "features", SurfaceContract: "features-authority.v1", Operations: []OperationID{"features.authority"}},
	}
	for i := range profiles {
		hash := sha256.Sum256([]byte(string(profiles[i].Role) + "\x00" + string(profiles[i].SurfaceContract) + "\x00" + string(profiles[i].Operations[0])))
		profiles[i].ManifestSHA256 = hex.EncodeToString(hash[:])
	}
	return profiles
}
