package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	appoperations "relay/internal/app/operations"
	"relay/internal/operations/registry"
)

// TicketFrontierOperationIdentity is the strict, transport-independent
// identity for the only active Ticket MCP operation. It binds a Planner
// frontier read to the published semantic identity and exact active packet.
type TicketFrontierOperationIdentity struct {
	ExpectedPacketID string `json:"expected_packet_id"`
	OperationID      string `json:"operation_id"`
	Action           string `json:"action"`
	TicketID         string `json:"ticket_id"`
}

func (v TicketFrontierOperationIdentity) SemanticIdentityVersion() string {
	operation, ok := registry.TicketOperationForAction(registry.TicketActionReadFrontier)
	if !ok {
		return ""
	}
	return operation.PacketSemanticProjection
}

func (v TicketFrontierOperationIdentity) SemanticRequestSHA256() (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	operation, ok := registry.TicketOperationForAction(registry.TicketActionReadFrontier)
	if !ok {
		return "", errors.New("unregistered ticket frontier operation identity")
	}
	manifestSHA256, ok := registry.SurfaceManifestSHA256(operation.SurfaceContract)
	if !ok {
		return "", errors.New("ticket surface manifest is unavailable")
	}
	encoded, err := json.Marshal(struct {
		SemanticIdentityVersion string                          `json:"semantic_identity_version"`
		SurfaceContract         string                          `json:"surface_contract"`
		SurfaceManifestSHA256   string                          `json:"surface_manifest_sha256"`
		Identity                TicketFrontierOperationIdentity `json:"identity"`
	}{
		SemanticIdentityVersion: v.SemanticIdentityVersion(),
		SurfaceContract:         string(operation.SurfaceContract),
		SurfaceManifestSHA256:   manifestSHA256,
		Identity:                v,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (v TicketFrontierOperationIdentity) Validate() error {
	if v.Action != string(registry.TicketActionReadFrontier) {
		return errors.New("ticket frontier identity must use read_ticket_frontier action")
	}
	if v.OperationID != string(registry.PlannerTicketFrontierOperationID) {
		return errors.New("ticket frontier identity must use the planner frontier operation")
	}
	if v.ExpectedPacketID == "" || v.TicketID == "" {
		return errors.New("ticket frontier identity requires expected_packet_id and ticket_id")
	}
	if v.SemanticIdentityVersion() == "" {
		return errors.New("unregistered ticket frontier operation identity")
	}
	return nil
}

// DecodeTicketFrontierOperationIdentity rejects unknown fields and malformed
// identities before packet admission is attempted.
func DecodeTicketFrontierOperationIdentity(raw json.RawMessage) (TicketFrontierOperationIdentity, error) {
	var value TicketFrontierOperationIdentity
	if err := brokerDecodeStrict(raw, &value); err != nil {
		return TicketFrontierOperationIdentity{}, err
	}
	if err := value.Validate(); err != nil {
		return TicketFrontierOperationIdentity{}, err
	}
	return value, nil
}

func (v TicketFrontierOperationIdentity) admissionRequest() appoperations.TicketFrontierReadRequest {
	return appoperations.TicketFrontierReadRequest{PacketID: v.ExpectedPacketID, TicketID: v.TicketID}
}

// TicketRoleSurface exposes only published Planner frontier authority. It
// deliberately has no local-operator role or mutation inventory.
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

// TicketFrontierAdmitter bridges the strict MCP identity to the single
// Planner-frontier packet admission boundary. It cannot admit Ticket
// mutation, package, lease, or completion actions.
type TicketFrontierAdmitter struct {
	service *appoperations.TicketFrontierAdmissionService
}

func NewTicketFrontierAdmitter(service *appoperations.TicketFrontierAdmissionService) (*TicketFrontierAdmitter, error) {
	if service == nil {
		return nil, errors.New("ticket frontier admission service is required")
	}
	return &TicketFrontierAdmitter{service: service}, nil
}

func (a *TicketFrontierAdmitter) Admit(ctx context.Context, identity TicketFrontierOperationIdentity) (appoperations.MutationAuthorization, string, error) {
	if a == nil || a.service == nil {
		return appoperations.MutationAuthorization{}, "", errors.New("ticket frontier admission service is required")
	}
	fingerprint, err := identity.SemanticRequestSHA256()
	if err != nil {
		return appoperations.MutationAuthorization{}, "", err
	}
	authorization, err := a.service.Admit(ctx, identity.admissionRequest())
	return authorization, fingerprint, err
}
