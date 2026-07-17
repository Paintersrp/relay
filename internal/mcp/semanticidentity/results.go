package semanticidentity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"relay/internal/operations/registry"
)

const MaxResultIdentityBytes = 64 * 1024

var ErrInvalidResultIdentity = errors.New("invalid mutation result identity")

type ResultKind string

const (
	ResultKindCreateOperationPacket  ResultKind = "create_operation_packet_result"
	ResultKindRefreshOperationPacket ResultKind = "refresh_operation_packet_result"
	ResultKindCloseOperationPacket   ResultKind = "close_operation_packet_result"
	ResultKindSubmitPlan             ResultKind = "submit_plan_result"
	ResultKindCreateRun              ResultKind = "create_run_result"
	ResultKindRecordAuditDecision    ResultKind = "record_audit_decision_result"
)

type ResultIdentity interface {
	resultIdentity()
	MutationTool() registry.MutationTool
	ResultKind() ResultKind
}

type EncodedResult struct {
	Kind   ResultKind
	JSON   []byte
	SHA256 string
}

type ReplacementPacketIdentity struct {
	PacketID          string                     `json:"packet_id"`
	PacketSHA256      string                     `json:"packet_sha256"`
	Role              string                     `json:"role"`
	OperationID       registry.OperationID       `json:"operation_id"`
	SurfaceContractID registry.SurfaceContractID `json:"surface_contract"`
}

type PacketSummaryIdentity struct {
	PacketID          string                     `json:"packet_id"`
	PacketSHA256      string                     `json:"packet_sha256"`
	SchemaVersion     string                     `json:"schema_version"`
	Role              string                     `json:"role"`
	OperationID       registry.OperationID       `json:"operation_id"`
	SurfaceContractID registry.SurfaceContractID `json:"surface_contract"`
	ProjectID         string                     `json:"project_id"`
	ReadinessState    string                     `json:"readiness_state"`
	LifecycleState    string                     `json:"lifecycle_state"`
	ReplacementPacket *ReplacementPacketIdentity `json:"replacement_packet"`
	SupersededAt      *string                    `json:"superseded_at"`
	ClosedAt          *string                    `json:"closed_at"`
}

type PacketDocumentIdentity struct {
	ArtifactID string `json:"artifact_id"`
	MediaType  string `json:"media_type"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

type OperationPacketViewIdentity struct {
	Summary  PacketSummaryIdentity  `json:"summary"`
	Document PacketDocumentIdentity `json:"document"`
}

type CreateOperationPacketResult struct {
	Packet                OperationPacketViewIdentity `json:"packet"`
	SurfaceManifestSHA256 string                      `json:"surface_manifest_sha256"`
	Complete              bool                        `json:"complete"`
}

func (CreateOperationPacketResult) resultIdentity() {}
func (CreateOperationPacketResult) MutationTool() registry.MutationTool {
	return registry.MutationToolCreateOperationPacket
}
func (CreateOperationPacketResult) ResultKind() ResultKind {
	return ResultKindCreateOperationPacket
}

type RefreshOperationPacketResult struct {
	PriorPacket           PacketSummaryIdentity       `json:"prior_packet"`
	Packet                OperationPacketViewIdentity `json:"packet"`
	SurfaceManifestSHA256 string                      `json:"surface_manifest_sha256"`
	Complete              bool                        `json:"complete"`
}

func (RefreshOperationPacketResult) resultIdentity() {}
func (RefreshOperationPacketResult) MutationTool() registry.MutationTool {
	return registry.MutationToolRefreshOperationPacket
}
func (RefreshOperationPacketResult) ResultKind() ResultKind {
	return ResultKindRefreshOperationPacket
}

type CloseOperationPacketResult struct {
	Packet   PacketSummaryIdentity `json:"packet"`
	Complete bool                  `json:"complete"`
}

func (CloseOperationPacketResult) resultIdentity() {}
func (CloseOperationPacketResult) MutationTool() registry.MutationTool {
	return registry.MutationToolCloseOperationPacket
}
func (CloseOperationPacketResult) ResultKind() ResultKind {
	return ResultKindCloseOperationPacket
}

type SubmitPlanResult struct {
	PlanID         string `json:"plan_id"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	ProjectID      string `json:"project_id"`
	SubmissionID   string `json:"submission_id"`
	WorkflowState  string `json:"workflow_state"`
	Complete       bool   `json:"complete"`
}

func (SubmitPlanResult) resultIdentity() {}
func (SubmitPlanResult) MutationTool() registry.MutationTool {
	return registry.MutationToolSubmitPlan
}
func (SubmitPlanResult) ResultKind() ResultKind {
	return ResultKindSubmitPlan
}

type BaseRepositoryIdentity struct {
	RepositoryKey string `json:"repository_key"`
	CommitOID     string `json:"commit_oid"`
}

type CreateRunResult struct {
	RunID            string                   `json:"run_id"`
	ArtifactID       string                   `json:"artifact_id"`
	ArtifactSHA256   string                   `json:"artifact_sha256"`
	OperationID      registry.OperationID     `json:"operation_id"`
	ProjectID        string                   `json:"project_id"`
	BaseRepositories []BaseRepositoryIdentity `json:"base_repositories"`
	InitialState     string                   `json:"initial_state"`
	Complete         bool                     `json:"complete"`
}

func (CreateRunResult) resultIdentity() {}
func (CreateRunResult) MutationTool() registry.MutationTool {
	return registry.MutationToolCreateRun
}
func (CreateRunResult) ResultKind() ResultKind {
	return ResultKindCreateRun
}

type RecordAuditDecisionResult struct {
	AuditDecisionID   string `json:"audit_decision_id"`
	AuditPacketID     string `json:"audit_packet_id"`
	AuditPacketSHA256 string `json:"audit_packet_sha256"`
	AuditedCommitOID  string `json:"audited_commit_oid"`
	Decision          string `json:"decision"`
	RunID             string `json:"run_id"`
	RecordedAt        string `json:"recorded_at"`
	Complete          bool   `json:"complete"`
}

func (RecordAuditDecisionResult) resultIdentity() {}
func (RecordAuditDecisionResult) MutationTool() registry.MutationTool {
	return registry.MutationToolRecordAuditDecision
}
func (RecordAuditDecisionResult) ResultKind() ResultKind {
	return ResultKindRecordAuditDecision
}

func EncodeResultIdentity(surface registry.SurfaceContractID, tool registry.MutationTool, identity ResultIdentity) (EncodedResult, error) {
	normalized, err := normalizeResultIdentity(identity)
	if err != nil || normalized.MutationTool() != tool || !registry.IsStateChangingToolForSurface(surface, string(tool)) {
		return EncodedResult{}, ErrInvalidResultIdentity
	}
	if err := validateResultIdentity(tool, normalized); err != nil {
		return EncodedResult{}, ErrInvalidResultIdentity
	}
	raw, err := json.Marshal(normalized)
	if err != nil || len(raw) < 2 || len(raw) > MaxResultIdentityBytes || raw[0] != '{' || !json.Valid(raw) {
		return EncodedResult{}, ErrInvalidResultIdentity
	}
	sum := sha256.Sum256(raw)
	return EncodedResult{Kind: identity.ResultKind(), JSON: raw, SHA256: hex.EncodeToString(sum[:])}, nil
}

func DecodeResultIdentity(surface registry.SurfaceContractID, tool registry.MutationTool, kind ResultKind, raw []byte) (ResultIdentity, error) {
	if !registry.IsStateChangingToolForSurface(surface, string(tool)) || kind != ResultKindForTool(tool) || len(raw) < 2 || len(raw) > MaxResultIdentityBytes || raw[0] != '{' || !json.Valid(raw) {
		return nil, ErrInvalidResultIdentity
	}
	var target ResultIdentity
	switch tool {
	case registry.MutationToolCreateOperationPacket:
		target = &CreateOperationPacketResult{}
	case registry.MutationToolRefreshOperationPacket:
		target = &RefreshOperationPacketResult{}
	case registry.MutationToolCloseOperationPacket:
		target = &CloseOperationPacketResult{}
	case registry.MutationToolSubmitPlan:
		target = &SubmitPlanResult{}
	case registry.MutationToolCreateRun:
		target = &CreateRunResult{}
	case registry.MutationToolRecordAuditDecision:
		target = &RecordAuditDecisionResult{}
	default:
		return nil, ErrInvalidResultIdentity
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, ErrInvalidResultIdentity
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, ErrInvalidResultIdentity
	}
	encoded, err := EncodeResultIdentity(surface, tool, target)
	if err != nil || encoded.Kind != kind || !bytes.Equal(encoded.JSON, raw) {
		return nil, ErrInvalidResultIdentity
	}
	normalized, err := normalizeResultIdentity(target)
	if err != nil {
		return nil, ErrInvalidResultIdentity
	}
	return normalized, nil
}

func ResultKindForTool(tool registry.MutationTool) ResultKind {
	switch tool {
	case registry.MutationToolCreateOperationPacket:
		return ResultKindCreateOperationPacket
	case registry.MutationToolRefreshOperationPacket:
		return ResultKindRefreshOperationPacket
	case registry.MutationToolCloseOperationPacket:
		return ResultKindCloseOperationPacket
	case registry.MutationToolSubmitPlan:
		return ResultKindSubmitPlan
	case registry.MutationToolCreateRun:
		return ResultKindCreateRun
	case registry.MutationToolRecordAuditDecision:
		return ResultKindRecordAuditDecision
	default:
		return ""
	}
}

func validateResultIdentity(tool registry.MutationTool, identity ResultIdentity) error {
	switch tool {
	case registry.MutationToolCreateOperationPacket:
		value, ok := identity.(CreateOperationPacketResult)
		if !ok || !value.Complete || !validSHA256(value.SurfaceManifestSHA256) || validatePacketView(value.Packet) != nil {
			return ErrInvalidResultIdentity
		}
	case registry.MutationToolRefreshOperationPacket:
		value, ok := identity.(RefreshOperationPacketResult)
		if !ok || !value.Complete || !validSHA256(value.SurfaceManifestSHA256) || validatePacketSummary(value.PriorPacket) != nil || validatePacketView(value.Packet) != nil {
			return ErrInvalidResultIdentity
		}
	case registry.MutationToolCloseOperationPacket:
		value, ok := identity.(CloseOperationPacketResult)
		if !ok || !value.Complete || validatePacketSummary(value.Packet) != nil {
			return ErrInvalidResultIdentity
		}
	case registry.MutationToolSubmitPlan:
		value, ok := identity.(SubmitPlanResult)
		if !ok || !value.Complete || !validOpaque(value.PlanID) || !validOpaque(value.ArtifactID) || !validSHA256(value.ArtifactSHA256) || !validOpaque(value.ProjectID) || !validOpaque(value.SubmissionID) || !validBounded(value.WorkflowState, 4096) {
			return ErrInvalidResultIdentity
		}
	case registry.MutationToolCreateRun:
		value, ok := identity.(CreateRunResult)
		if !ok || !value.Complete || !validOpaque(value.RunID) || !validOpaque(value.ArtifactID) || !validSHA256(value.ArtifactSHA256) || value.OperationID == "" || !validOpaque(value.ProjectID) || len(value.BaseRepositories) < 1 || len(value.BaseRepositories) > 64 || !validBounded(value.InitialState, 4096) {
			return ErrInvalidResultIdentity
		}
		for _, repository := range value.BaseRepositories {
			if !validRepositoryKey(repository.RepositoryKey) || !validGitOID(repository.CommitOID) {
				return ErrInvalidResultIdentity
			}
		}
	case registry.MutationToolRecordAuditDecision:
		value, ok := identity.(RecordAuditDecisionResult)
		if !ok || !value.Complete || !validOpaque(value.AuditDecisionID) || !validOpaque(value.AuditPacketID) || !validSHA256(value.AuditPacketSHA256) || !validGitOID(value.AuditedCommitOID) || (value.Decision != "accepted" && value.Decision != "needs_revision") || !validOpaque(value.RunID) || !validRFC3339(value.RecordedAt) {
			return ErrInvalidResultIdentity
		}
	default:
		return ErrInvalidResultIdentity
	}
	if identity.ResultKind() != ResultKindForTool(tool) {
		return ErrInvalidResultIdentity
	}
	return nil
}

func normalizeResultIdentity(identity ResultIdentity) (ResultIdentity, error) {
	switch value := identity.(type) {
	case CreateOperationPacketResult:
		return value, nil
	case *CreateOperationPacketResult:
		if value != nil {
			return *value, nil
		}
	case RefreshOperationPacketResult:
		return value, nil
	case *RefreshOperationPacketResult:
		if value != nil {
			return *value, nil
		}
	case CloseOperationPacketResult:
		return value, nil
	case *CloseOperationPacketResult:
		if value != nil {
			return *value, nil
		}
	case SubmitPlanResult:
		return value, nil
	case *SubmitPlanResult:
		if value != nil {
			return *value, nil
		}
	case CreateRunResult:
		return value, nil
	case *CreateRunResult:
		if value != nil {
			return *value, nil
		}
	case RecordAuditDecisionResult:
		return value, nil
	case *RecordAuditDecisionResult:
		if value != nil {
			return *value, nil
		}
	}
	return nil, ErrInvalidResultIdentity
}

func validatePacketView(value OperationPacketViewIdentity) error {
	if validatePacketSummary(value.Summary) != nil || !validOpaque(value.Document.ArtifactID) || value.Document.MediaType != "application/vnd.relay.operation-packet+json;version=1" || value.Document.SizeBytes < 0 || !validSHA256(value.Document.SHA256) || value.Document.SHA256 != value.Summary.PacketSHA256 {
		return ErrInvalidResultIdentity
	}
	return nil
}

func validatePacketSummary(value PacketSummaryIdentity) error {
	if !validOpaque(value.PacketID) || !validSHA256(value.PacketSHA256) || value.SchemaVersion != "relay.operation-packet.v1" || (value.Role != "planner" && value.Role != "auditor") || value.OperationID == "" || value.SurfaceContractID == "" || !validOpaque(value.ProjectID) || value.ReadinessState != "ready" {
		return ErrInvalidResultIdentity
	}
	switch value.LifecycleState {
	case "active":
		if value.ReplacementPacket != nil || value.SupersededAt != nil || value.ClosedAt != nil {
			return ErrInvalidResultIdentity
		}
	case "superseded":
		if value.ReplacementPacket == nil || value.SupersededAt == nil || value.ClosedAt != nil || validateReplacementPacket(*value.ReplacementPacket) != nil || !validRFC3339(*value.SupersededAt) {
			return ErrInvalidResultIdentity
		}
	case "closed":
		if value.ReplacementPacket != nil || value.SupersededAt != nil || value.ClosedAt == nil || !validRFC3339(*value.ClosedAt) {
			return ErrInvalidResultIdentity
		}
	default:
		return ErrInvalidResultIdentity
	}
	return nil
}

func validateReplacementPacket(value ReplacementPacketIdentity) error {
	if !validOpaque(value.PacketID) || !validSHA256(value.PacketSHA256) || (value.Role != "planner" && value.Role != "auditor") || value.OperationID == "" || value.SurfaceContractID == "" {
		return ErrInvalidResultIdentity
	}
	return nil
}

func validRFC3339(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}
