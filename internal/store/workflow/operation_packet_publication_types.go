package workflowstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	OperationPacketPublicationStateCommitted = "committed"

	OperationPacketRetainedArtifactDirectUploadedInput = "direct_uploaded_input"
	OperationPacketRetainedArtifactInlineInput         = "inline_input"
	OperationPacketRetainedArtifactWorkflowSnapshot    = "workflow_snapshot"
)

type OperationPacketPublication struct {
	ID                             int64
	PublicationID                  string
	PacketRowID                    int64
	PacketArtifactRowID            int64
	MutationResultRowID            int64
	Namespace                      string
	ManifestSHA256                 string
	ExpectedRetainedArtifactCount  int64
	ExpectedBindingCount           int64
	ExpectedDependencyCount        int64
	ExpectedVaultRelationshipCount int64
	State                          string
	CreatedAt                      string
}

type OperationPacketRetainedArtifact struct {
	ID            int64
	PublicationID string
	ArtifactID    string
	Kind          string
	RelativePath  string
	MediaType     string
	SHA256        string
	SizeBytes     int64
	CreatedAt     string
}

type OperationPacketArtifactBinding struct {
	ID                    int64
	PublicationID         string
	PacketRowID           int64
	Sequence              int64
	DependencyClass       string
	DependencyKey         string
	PacketArtifactRowID   sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
	CreatedAt             string
}

type OperationPacketVaultRelationship struct {
	ID              int64
	PublicationID   string
	PacketRowID     int64
	DependencyClass string
	DependencyKey   string
	OwnerIdentity   string
	RetentionRowID  int64
	ClosureRowID    int64
	VaultRowID      int64
	CommitOID       string
	TreeOID         string
	CreatedAt       string
}

type OperationPacketPublicationIntegrity struct {
	Publication        OperationPacketPublication
	Packet             OperationPacket
	PacketArtifact     OperationPacketArtifact
	MutationResult     MCPMutationResult
	RetainedArtifacts  []OperationPacketRetainedArtifact
	Bindings           []OperationPacketArtifactBinding
	Dependencies       []OperationPacketRetentionDependency
	VaultRelationships []OperationPacketVaultRelationship
}

type CreateOperationPacketRetainedArtifactParams struct {
	PublicationID string
	ArtifactID    string
	Kind          string
	RelativePath  string
	MediaType     string
	SHA256        string
	SizeBytes     int64
}

type CreateOperationPacketArtifactBindingParams struct {
	PublicationID         string
	PacketRowID           int64
	Sequence              int64
	DependencyClass       string
	DependencyKey         string
	PacketArtifactRowID   sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
}

type CreateOperationPacketVaultRelationshipParams struct {
	PublicationID   string
	PacketRowID     int64
	DependencyClass string
	DependencyKey   string
	OwnerIdentity   string
	RetentionRowID  int64
	ClosureRowID    int64
	VaultRowID      int64
	CommitOID       string
	TreeOID         string
}

type CreateOperationPacketPublicationParams struct {
	PublicationID                  string
	PacketRowID                    int64
	PacketArtifactRowID            int64
	MutationResultRowID            int64
	Namespace                      string
	ManifestSHA256                 string
	ExpectedRetainedArtifactCount  int64
	ExpectedBindingCount           int64
	ExpectedDependencyCount        int64
	ExpectedVaultRelationshipCount int64
}

func SourceVaultRetentionOwnerIdentity(packetID, dependencyClass, dependencyKey string) (string, error) {
	if strings.TrimSpace(packetID) != packetID || packetID == "" || strings.TrimSpace(dependencyKey) != dependencyKey || dependencyKey == "" || len(dependencyKey) > 512 || !validSourceVaultPublicationDependencyClass(dependencyClass) {
		return "", fmt.Errorf("source-vault retention edge identity is invalid")
	}
	raw := []byte("relay.operation-packet.source-retention.v1\x00" + packetID + "\x00" + dependencyClass + "\x00" + dependencyKey)
	digest := sha256.Sum256(raw)
	return "opkt-edge:" + hex.EncodeToString(digest[:]), nil
}

func validSourceVaultPublicationDependencyClass(value string) bool {
	switch value {
	case OperationPacketDependencyRepositoryVault,
		OperationPacketDependencyGitPathObject,
		OperationPacketDependencyManifestMember:
		return true
	default:
		return false
	}
}
