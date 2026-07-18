package mcp

import (
	"context"
	"fmt"

	"relay/internal/app/idempotency"
	appoperations "relay/internal/app/operations"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/semanticidentity"
)

type OperationPacketLifecycleHandler struct {
	service *appoperations.LifecycleService
}

type OperationPacketLifecycleEnvelope struct {
	ResultKind         semanticidentity.ResultKind `json:"result_kind"`
	ResultIdentityJSON []byte                      `json:"result_identity_json"`
	ResultSHA256       string                      `json:"result_sha256"`
	CommittedAt        string                      `json:"committed_at"`
	Replay             bool                        `json:"replay"`
}

type CreateOperationPacketRequest struct {
	MutationID string
	Identity   semanticidentity.CreateOperationPacket
	Files      []fileacquisition.FileParameter
}

type RefreshOperationPacketRequest struct {
	MutationID    string
	PriorPacketID string
	Identity      semanticidentity.RefreshOperationPacket
	Files         []fileacquisition.FileParameter
}

type CloseOperationPacketRequest struct {
	MutationID string
	Identity   semanticidentity.CloseOperationPacket
}

func NewOperationPacketLifecycleHandler(service *appoperations.LifecycleService) (*OperationPacketLifecycleHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("operation packet lifecycle service is required")
	}
	return &OperationPacketLifecycleHandler{service: service}, nil
}

func (h *OperationPacketLifecycleHandler) Create(ctx context.Context, request CreateOperationPacketRequest) (OperationPacketLifecycleEnvelope, error) {
	result, err := h.service.Create(ctx, appoperations.CreateLifecycleInput{MutationID: request.MutationID, Identity: request.Identity, Files: request.Files})
	if err != nil {
		return OperationPacketLifecycleEnvelope{}, err
	}
	return lifecycleEnvelope(result.Mutation, result.Replay), nil
}

func (h *OperationPacketLifecycleHandler) Refresh(ctx context.Context, request RefreshOperationPacketRequest) (OperationPacketLifecycleEnvelope, error) {
	result, err := h.service.Refresh(ctx, appoperations.RefreshLifecycleInput{MutationID: request.MutationID, PriorPacketID: request.PriorPacketID, Identity: request.Identity, Files: request.Files})
	if err != nil {
		return OperationPacketLifecycleEnvelope{}, err
	}
	return lifecycleEnvelope(result.Mutation, result.Replay), nil
}

func (h *OperationPacketLifecycleHandler) Close(ctx context.Context, request CloseOperationPacketRequest) (OperationPacketLifecycleEnvelope, error) {
	result, err := h.service.Close(ctx, appoperations.CloseLifecycleInput{MutationID: request.MutationID, Identity: request.Identity})
	if err != nil {
		return OperationPacketLifecycleEnvelope{}, err
	}
	return lifecycleEnvelope(result.Mutation, result.Replay), nil
}

func lifecycleEnvelope(result idempotency.StoredResult, replay bool) OperationPacketLifecycleEnvelope {
	return OperationPacketLifecycleEnvelope{ResultKind: result.ResultKind, ResultIdentityJSON: append([]byte(nil), result.ResultIdentityJSON...), ResultSHA256: result.ResultSHA256, CommittedAt: result.CommittedAt, Replay: replay}
}
