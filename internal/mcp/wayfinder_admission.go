package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	appoperations "relay/internal/app/operations"
	"relay/internal/operations/registry"
)

// WayfinderMutationIdentity is the strict, transport-independent identity for
// every Wayfinder/Features mutation. It contains only packet and retained
// dependency identities; it deliberately contains no path, shell, or source
// browsing capability.
type WayfinderMutationIdentity struct {
	MutationID           string `json:"mutation_id"`
	ExpectedPacketID     string `json:"expected_packet_id"`
	OperationID          string `json:"operation_id"`
	Action               string `json:"action"`
	RequiredDependencies []struct {
		Class string `json:"class"`
		Key   string `json:"key"`
	} `json:"required_dependencies,omitempty"`
}

func (v WayfinderMutationIdentity) SemanticIdentityVersion() string {
	return "relay.semantic.wayfinder-mutation.v1"
}

func (v WayfinderMutationIdentity) SemanticRequestSHA256() (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (v WayfinderMutationIdentity) Validate() error {
	if registry.ValidateMutationID(v.MutationID) != nil || strings.TrimSpace(v.ExpectedPacketID) != v.ExpectedPacketID || v.ExpectedPacketID == "" {
		return errors.New("invalid mutation identity")
	}
	operation, ok := registry.Lookup(registry.OperationID(v.OperationID))
	if !ok || (operation.Role != "wayfinder" && operation.Role != "features") || !containsWayfinderAction(operation.AllowedNonSourceActions, registry.AllowedAction(v.Action)) {
		return errors.New("unregistered wayfinder operation action")
	}
	seen := map[string]struct{}{}
	for _, dependency := range v.RequiredDependencies {
		if dependency.Class == "" || strings.TrimSpace(dependency.Class) != dependency.Class || dependency.Key == "" || strings.TrimSpace(dependency.Key) != dependency.Key {
			return errors.New("invalid retained dependency")
		}
		key := dependency.Class + "\x00" + dependency.Key
		if _, duplicate := seen[key]; duplicate {
			return errors.New("duplicate retained dependency")
		}
		seen[key] = struct{}{}
	}
	return nil
}

type WayfinderPacketAdmitter struct {
	service *appoperations.WayfinderAdmissionService
}

// WayfinderRoleSurface exposes the exact role-owned MCP admission surfaces.
// Callers must obtain an admission before invoking an application owner; this
// inventory deliberately contains no source capability.
type WayfinderRoleSurface struct {
	Role            registry.Role
	SurfaceContract registry.SurfaceContractID
	Operations      []registry.OperationID
	ManifestSHA256  string
}

func WayfinderRoleSurfaces() []WayfinderRoleSurface {
	profiles := registry.WayfinderRoleProfiles()
	out := make([]WayfinderRoleSurface, len(profiles))
	for i, profile := range profiles {
		out[i] = WayfinderRoleSurface{
			Role:            profile.Role,
			SurfaceContract: profile.SurfaceContract,
			Operations:      append([]registry.OperationID(nil), profile.Operations...),
			ManifestSHA256:  profile.ManifestSHA256,
		}
	}
	return out
}

func NewWayfinderPacketAdmitter(service *appoperations.WayfinderAdmissionService) (*WayfinderPacketAdmitter, error) {
	if service == nil {
		return nil, errors.New("wayfinder packet admission service is required")
	}
	return &WayfinderPacketAdmitter{service: service}, nil
}

func (a *WayfinderPacketAdmitter) Admit(ctx context.Context, identity WayfinderMutationIdentity) (appoperations.MutationAuthorization, string, error) {
	if a == nil || a.service == nil {
		return appoperations.MutationAuthorization{}, "", errors.New("wayfinder packet admission service is required")
	}
	fingerprint, err := identity.SemanticRequestSHA256()
	if err != nil {
		return appoperations.MutationAuthorization{}, "", err
	}
	requirements := make([]appoperations.DependencyRequirement, len(identity.RequiredDependencies))
	for i, dependency := range identity.RequiredDependencies {
		requirements[i] = appoperations.DependencyRequirement{Class: dependency.Class, Key: dependency.Key}
	}
	authorization, err := a.service.AdmitWayfinderMutation(ctx, appoperations.WayfinderMutationRequest{PacketID: identity.ExpectedPacketID, OperationID: registry.OperationID(identity.OperationID), Action: registry.AllowedAction(identity.Action), RequiredDependencies: requirements})
	return authorization, fingerprint, err
}

func containsWayfinderAction(actions []registry.AllowedAction, action registry.AllowedAction) bool {
	for _, candidate := range actions {
		if candidate == action {
			return true
		}
	}
	return false
}
