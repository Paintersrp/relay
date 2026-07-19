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

// PackageOperationIdentity is the strict MCP identity for package composition,
// package approval, and lease recovery. It carries packet evidence and exact
// durable identities only; local repository paths and process state never cross
// this boundary.
type PackageOperationIdentity struct {
	MutationID                   string                   `json:"mutation_id"`
	ExpectedPacketID             string                   `json:"expected_packet_id"`
	OperationID                  string                   `json:"operation_id"`
	Action                       string                   `json:"action"`
	SelectionID                  string                   `json:"selection_id,omitempty"`
	PackageID                    string                   `json:"package_id,omitempty"`
	RunID                        string                   `json:"run_id,omitempty"`
	LeaseID                      string                   `json:"lease_id,omitempty"`
	PayloadSHA256                string                   `json:"payload_sha256"`
	ExpectedPackageSha256        string                   `json:"expected_package_sha256,omitempty"`
	OperatorConfirmationEvidence string                   `json:"operator_confirmation_evidence,omitempty"`
	RequiredDependencies         []TicketPacketDependency `json:"required_dependencies"`
}

func (v PackageOperationIdentity) SemanticIdentityVersion() string {
	operation, ok := registry.PackageOperationForAction(registry.AllowedAction(v.Action))
	if !ok {
		return ""
	}
	return operation.PacketSemanticProjection
}

func (v PackageOperationIdentity) Validate() error {
	request := v.admissionRequest()
	if registry.ValidateMutationID(v.MutationID) != nil || appoperations.ValidatePackageOperationRequest(request) != nil || v.SemanticIdentityVersion() == "" {
		return errors.New("invalid execution package operation identity")
	}
	return nil
}

func (v PackageOperationIdentity) SemanticRequestSHA256() (string, error) {
	canonical, err := v.canonicalized()
	if err != nil {
		return "", err
	}
	operation, ok := registry.PackageOperationForAction(registry.AllowedAction(canonical.Action))
	if !ok {
		return "", errors.New("unregistered execution package operation identity")
	}
	manifestSHA256, ok := registry.SurfaceManifestSHA256(operation.SurfaceContract)
	if !ok {
		return "", errors.New("execution package surface manifest is unavailable")
	}
	raw, err := json.Marshal(struct {
		SemanticIdentityVersion string                   `json:"semantic_identity_version"`
		SurfaceContract         string                   `json:"surface_contract"`
		SurfaceManifestSHA256   string                   `json:"surface_manifest_sha256"`
		Identity                PackageOperationIdentity `json:"identity"`
	}{canonical.SemanticIdentityVersion(), string(operation.SurfaceContract), manifestSHA256, canonical})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func DecodePackageOperationIdentity(raw json.RawMessage) (PackageOperationIdentity, error) {
	var value PackageOperationIdentity
	if err := brokerDecodeStrict(raw, &value); err != nil {
		return PackageOperationIdentity{}, err
	}
	if err := value.Validate(); err != nil {
		return PackageOperationIdentity{}, err
	}
	return value, nil
}

func (v PackageOperationIdentity) admissionRequest() appoperations.PackageOperationRequest {
	dependencies := make([]appoperations.DependencyRequirement, len(v.RequiredDependencies))
	for index, dependency := range v.RequiredDependencies {
		dependencies[index] = appoperations.DependencyRequirement{Class: dependency.Class, Key: dependency.Key}
	}
	return appoperations.PackageOperationRequest{
		PacketID: v.ExpectedPacketID, OperationID: registry.OperationID(v.OperationID), Action: registry.AllowedAction(v.Action),
		SelectionID: v.SelectionID, PackageID: v.PackageID, RunID: v.RunID, LeaseID: v.LeaseID,
		PayloadSHA256:                v.PayloadSHA256,
		ExpectedPackageSha256:        v.ExpectedPackageSha256,
		OperatorConfirmationEvidence: v.OperatorConfirmationEvidence,
		RequiredDependencies:         dependencies,
	}
}

func (v PackageOperationIdentity) canonicalized() (PackageOperationIdentity, error) {
	if err := v.Validate(); err != nil {
		return PackageOperationIdentity{}, err
	}
	result := v
	result.RequiredDependencies = append([]TicketPacketDependency(nil), v.RequiredDependencies...)
	sort.Slice(result.RequiredDependencies, func(left, right int) bool {
		if result.RequiredDependencies[left].Class != result.RequiredDependencies[right].Class {
			return result.RequiredDependencies[left].Class < result.RequiredDependencies[right].Class
		}
		return result.RequiredDependencies[left].Key < result.RequiredDependencies[right].Key
	})
	return result, nil
}

type PackagePacketAdmitter struct {
	service *appoperations.PackageAdmissionService
}

func NewPackagePacketAdmitter(service *appoperations.PackageAdmissionService) (*PackagePacketAdmitter, error) {
	if service == nil {
		return nil, errors.New("execution package packet admission service is required")
	}
	return &PackagePacketAdmitter{service: service}, nil
}

func (a *PackagePacketAdmitter) Admit(ctx context.Context, identity PackageOperationIdentity) (appoperations.MutationAuthorization, string, error) {
	if a == nil || a.service == nil {
		return appoperations.MutationAuthorization{}, "", errors.New("execution package packet admission service is required")
	}
	if err := identity.Validate(); err != nil {
		return appoperations.MutationAuthorization{}, "", err
	}
	digest, err := identity.SemanticRequestSHA256()
	if err != nil {
		return appoperations.MutationAuthorization{}, "", err
	}
	authorization, err := a.service.Admit(ctx, identity.admissionRequest())
	return authorization, digest, err
}

// PackageRoleSurface is the bounded local-operator inventory for package and
// lease actions. It is separate from ticket reads while sharing the same
// registered packet operation identity.
type PackageRoleSurface struct {
	Role            registry.Role
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	Actions         []registry.AllowedAction
	ManifestSHA256  string
}

func PackageRoleSurfaces() []PackageRoleSurface {
	operation, ok := registry.PackageOperationForAction(registry.PackageActionPrepare)
	if !ok {
		return nil
	}
	manifestSHA256, _ := registry.SurfaceManifestSHA256(operation.SurfaceContract)
	return []PackageRoleSurface{{
		Role: operation.Role, SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID,
		Actions:        []registry.AllowedAction{registry.PackageActionPrepare, registry.PackageActionApprove, registry.MutationLeaseActionReconcile},
		ManifestSHA256: manifestSHA256,
	}}
}
