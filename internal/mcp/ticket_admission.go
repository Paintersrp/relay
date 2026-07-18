package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	appoperations "relay/internal/app/operations"
	"relay/internal/operations/registry"
)

// TicketPacketDependency is an exact retained packet dependency. It contains
// only the class/key identity that the operation-packet owner can verify; it
// never grants source browsing or package authority.
type TicketPacketDependency struct {
	Class string `json:"class"`
	Key   string `json:"key"`
}

// TicketSelectionMemberIdentity binds a selection request to the exact ticket
// revisions observed in the frontier.
type TicketSelectionMemberIdentity struct {
	TicketID      string `json:"ticket_id"`
	RevisionRowID int64  `json:"revision_row_id"`
}

// TicketOperationIdentity is the strict, transport-independent identity for a
// P4 ticket route operation. Mutations include a caller-supplied mutation ID
// and a digest of the complete shared-owner payload; repeating the exact
// identity is replay-safe, while changed ticket, authority, source, workflow,
// or payload facts produce a different semantic request identity.
type TicketOperationIdentity struct {
	MutationID             string                          `json:"mutation_id,omitempty"`
	ExpectedPacketID       string                          `json:"expected_packet_id"`
	OperationID            string                          `json:"operation_id"`
	Action                 string                          `json:"action"`
	WorkspaceID            string                          `json:"workspace_id"`
	TicketID               string                          `json:"ticket_id,omitempty"`
	RevisionRowID          int64                           `json:"revision_row_id,omitempty"`
	ExpectedRevisionNumber int64                           `json:"expected_revision_number,omitempty"`
	AuthorityRevisionID    string                          `json:"authority_revision_id,omitempty"`
	SourceClosureRowID     int64                           `json:"source_closure_row_id,omitempty"`
	ExternalPriority       int64                           `json:"external_priority,omitempty"`
	PayloadSHA256          string                          `json:"payload_sha256,omitempty"`
	SelectionMembers       []TicketSelectionMemberIdentity `json:"selection_members,omitempty"`
	RequiredDependencies   []TicketPacketDependency        `json:"required_dependencies,omitempty"`
}

func (v TicketOperationIdentity) SemanticIdentityVersion() string {
	operation, ok := registry.TicketOperationForAction(registry.AllowedAction(v.Action))
	if !ok {
		return ""
	}
	return operation.PacketSemanticProjection
}

func (v TicketOperationIdentity) SemanticRequestSHA256() (string, error) {
	canonical, err := v.canonicalized()
	if err != nil {
		return "", err
	}
	operation, ok := registry.TicketOperationForAction(registry.AllowedAction(canonical.Action))
	if !ok {
		return "", errors.New("unregistered ticket operation identity")
	}
	manifestSHA256, ok := registry.SurfaceManifestSHA256(operation.SurfaceContract)
	if !ok {
		return "", errors.New("ticket surface manifest is unavailable")
	}
	encoded, err := json.Marshal(struct {
		SemanticIdentityVersion string                  `json:"semantic_identity_version"`
		SurfaceContract         string                  `json:"surface_contract"`
		SurfaceManifestSHA256   string                  `json:"surface_manifest_sha256"`
		Identity                TicketOperationIdentity `json:"identity"`
	}{
		SemanticIdentityVersion: canonical.SemanticIdentityVersion(),
		SurfaceContract:         string(operation.SurfaceContract),
		SurfaceManifestSHA256:   manifestSHA256,
		Identity:                canonical,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (v TicketOperationIdentity) Validate() error {
	request, err := v.admissionRequest()
	if err != nil || appoperations.ValidateTicketOperationRequest(request) != nil {
		return errors.New("invalid ticket operation identity")
	}
	if request.Action == registry.TicketActionReadFrontier {
		if v.MutationID != "" {
			return errors.New("frontier read must not carry a mutation id")
		}
	} else if registry.ValidateMutationID(v.MutationID) != nil {
		return errors.New("invalid ticket mutation identity")
	}
	if v.SemanticIdentityVersion() == "" {
		return errors.New("unregistered ticket operation identity")
	}
	return nil
}

// DecodeTicketOperationIdentity rejects unknown fields and malformed or
// incomplete route identities before application admission is attempted.
func DecodeTicketOperationIdentity(raw json.RawMessage) (TicketOperationIdentity, error) {
	var value TicketOperationIdentity
	if err := brokerDecodeStrict(raw, &value); err != nil {
		return TicketOperationIdentity{}, err
	}
	if err := value.Validate(); err != nil {
		return TicketOperationIdentity{}, err
	}
	return value, nil
}

func (v TicketOperationIdentity) admissionRequest() (appoperations.TicketOperationRequest, error) {
	dependencies := make([]appoperations.DependencyRequirement, len(v.RequiredDependencies))
	for index, dependency := range v.RequiredDependencies {
		dependencies[index] = appoperations.DependencyRequirement{Class: dependency.Class, Key: dependency.Key}
	}
	members := make([]appoperations.TicketSelectionMember, len(v.SelectionMembers))
	for index, member := range v.SelectionMembers {
		members[index] = appoperations.TicketSelectionMember{TicketID: member.TicketID, RevisionRowID: member.RevisionRowID}
	}
	return appoperations.TicketOperationRequest{
		PacketID: v.ExpectedPacketID, OperationID: registry.OperationID(v.OperationID), Action: registry.AllowedAction(v.Action),
		WorkspaceID: v.WorkspaceID, TicketID: v.TicketID, RevisionRowID: v.RevisionRowID,
		ExpectedRevisionNumber: v.ExpectedRevisionNumber, AuthorityRevisionID: v.AuthorityRevisionID,
		SourceClosureRowID: v.SourceClosureRowID, ExternalPriority: v.ExternalPriority, PayloadSHA256: v.PayloadSHA256,
		SelectionMembers: members, RequiredDependencies: dependencies,
	}, nil
}

func (v TicketOperationIdentity) canonicalized() (TicketOperationIdentity, error) {
	if err := v.Validate(); err != nil {
		return TicketOperationIdentity{}, err
	}
	result := v
	result.RequiredDependencies = append([]TicketPacketDependency(nil), v.RequiredDependencies...)
	sort.Slice(result.RequiredDependencies, func(left, right int) bool {
		if result.RequiredDependencies[left].Class != result.RequiredDependencies[right].Class {
			return result.RequiredDependencies[left].Class < result.RequiredDependencies[right].Class
		}
		return result.RequiredDependencies[left].Key < result.RequiredDependencies[right].Key
	})
	result.SelectionMembers = append([]TicketSelectionMemberIdentity(nil), v.SelectionMembers...)
	sort.Slice(result.SelectionMembers, func(left, right int) bool {
		if result.SelectionMembers[left].TicketID != result.SelectionMembers[right].TicketID {
			return result.SelectionMembers[left].TicketID < result.SelectionMembers[right].TicketID
		}
		return result.SelectionMembers[left].RevisionRowID < result.SelectionMembers[right].RevisionRowID
	})
	return result, nil
}

// TicketPacketAdmitter bridges strict MCP identities to the shared packet
// boundary. It exposes no package, Run, source, or filesystem operation.
type TicketPacketAdmitter struct {
	service *appoperations.TicketAdmissionService
}

// TicketRoleSurface exposes the exact ticket-route role inventories consumed
// by MCP callers. Planner has the one read-only frontier surface; all durable
// ticket route changes are local-operator operations.
type TicketRoleSurface struct {
	Role            registry.Role
	SurfaceContract registry.SurfaceContractID
	Operations      []registry.OperationID
	ManifestSHA256  string
}

func TicketRoleSurfaces() []TicketRoleSurface {
	profiles := registry.TicketRoleProfiles()
	out := make([]TicketRoleSurface, len(profiles))
	for index, profile := range profiles {
		out[index] = TicketRoleSurface{
			Role:            profile.Role,
			SurfaceContract: profile.SurfaceContract,
			Operations:      append([]registry.OperationID(nil), profile.Operations...),
			ManifestSHA256:  profile.ManifestSHA256,
		}
	}
	return out
}

func NewTicketPacketAdmitter(service *appoperations.TicketAdmissionService) (*TicketPacketAdmitter, error) {
	if service == nil {
		return nil, errors.New("ticket packet admission service is required")
	}
	return &TicketPacketAdmitter{service: service}, nil
}

func (a *TicketPacketAdmitter) Admit(ctx context.Context, identity TicketOperationIdentity) (appoperations.MutationAuthorization, string, error) {
	if a == nil || a.service == nil {
		return appoperations.MutationAuthorization{}, "", errors.New("ticket packet admission service is required")
	}
	fingerprint, err := identity.SemanticRequestSHA256()
	if err != nil {
		return appoperations.MutationAuthorization{}, "", err
	}
	request, err := identity.admissionRequest()
	if err != nil {
		return appoperations.MutationAuthorization{}, "", err
	}
	authorization, err := a.service.Admit(ctx, request)
	return authorization, fingerprint, err
}
