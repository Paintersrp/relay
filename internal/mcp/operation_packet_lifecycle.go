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

type OperationPacketMutation struct {
	ResultKind   semanticidentity.ResultKind `json:"result_kind"`
	ResultSHA256 string                      `json:"result_sha256"`
	CommittedAt  string                      `json:"committed_at"`
	Replay       bool                        `json:"replay"`
}

type CreateOperationPacketResult struct {
	Packet   OperationPacketView     `json:"packet"`
	Mutation OperationPacketMutation `json:"mutation"`
}

type RefreshOperationPacketResult struct {
	PriorPacket OperationPacketSummary  `json:"prior_packet"`
	Packet      OperationPacketView     `json:"packet"`
	Mutation    OperationPacketMutation `json:"mutation"`
}

type CloseOperationPacketResult struct {
	Packet   OperationPacketSummary  `json:"packet"`
	Mutation OperationPacketMutation `json:"mutation"`
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

func (h *OperationPacketLifecycleHandler) Create(ctx context.Context, request CreateOperationPacketRequest) (CreateOperationPacketResult, error) {
	result, err := h.service.Create(ctx, appoperations.CreateLifecycleInput{MutationID: request.MutationID, Identity: request.Identity, Files: request.Files})
	if err != nil {
		return CreateOperationPacketResult{}, err
	}
	return CreateOperationPacketResult{Packet: OperationPacketViewFromApplication(result.Packet), Mutation: lifecycleMutation(result.Mutation, result.Replay)}, nil
}

func (h *OperationPacketLifecycleHandler) Refresh(ctx context.Context, request RefreshOperationPacketRequest) (RefreshOperationPacketResult, error) {
	result, err := h.service.Refresh(ctx, appoperations.RefreshLifecycleInput{MutationID: request.MutationID, PriorPacketID: request.PriorPacketID, Identity: request.Identity, Files: request.Files})
	if err != nil {
		return RefreshOperationPacketResult{}, err
	}
	return RefreshOperationPacketResult{PriorPacket: OperationPacketSummaryFromApplication(result.Prior), Packet: OperationPacketViewFromApplication(result.Packet), Mutation: lifecycleMutation(result.Mutation, result.Replay)}, nil
}

func (h *OperationPacketLifecycleHandler) Close(ctx context.Context, request CloseOperationPacketRequest) (CloseOperationPacketResult, error) {
	result, err := h.service.Close(ctx, appoperations.CloseLifecycleInput{MutationID: request.MutationID, Identity: request.Identity})
	if err != nil {
		return CloseOperationPacketResult{}, err
	}
	return CloseOperationPacketResult{Packet: OperationPacketSummaryFromApplication(result.Packet), Mutation: lifecycleMutation(result.Mutation, result.Replay)}, nil
}

func lifecycleMutation(result idempotency.StoredResult, replay bool) OperationPacketMutation {
	return OperationPacketMutation{ResultKind: result.ResultKind, ResultSHA256: result.ResultSHA256, CommittedAt: result.CommittedAt, Replay: replay}
}
