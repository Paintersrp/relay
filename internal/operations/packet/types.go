package packet

import "relay/internal/operations/registry"

const (
	SchemaVersion  = "relay.operation-packet.v1"
	MediaType      = "application/vnd.relay.operation-packet+json;version=1"
	ReadinessReady = "ready"

	RevisionSourceExplicitCommit          = "explicit_commit"
	RevisionSourceConfiguredWorkingBranch = "configured_working_branch"

	InputSourceUploadedFile    = registry.InputSourceKind("uploaded_file")
	InputSourceRelayArtifact   = registry.InputSourceKind("relay_artifact")
	InputSourceInlineText      = registry.InputSourceKind("inline_text")
	InputSourceWorkflowRecord  = registry.InputSourceKind("workflow_record")
	InputSourceCommittedSource = registry.InputSourceKind("committed_source")
)

type Document struct {
	SchemaVersion         string
	CreatedAt             string
	Role                  registry.Role
	OperationID           registry.OperationID
	SurfaceContract       registry.SurfaceContractID
	SurfaceManifestSHA256 string
	PriorPacket           *PriorPacketIdentity
	Output                OutputContract
	Project               ProjectBinding
	WorkflowReferences    []WorkflowReference
	Attestations          []Attestation
	Inputs                []InputBinding
	Repositories          []RepositoryBinding
	RelaySpecs            GovernanceBinding
	ManifestDomain        ManifestDomainBinding
	SourcePolicy          registry.SourcePolicy
	HistoricalAuthority   registry.HistoricalAuthorityPolicy
	AllowedActions        []registry.AllowedAction
	ReadinessState        string
}

type PriorPacketIdentity struct {
	PacketID     string
	PacketSHA256 string
}

type OutputContract struct {
	OutputKind        string
	OutputPersistence string
}

type ProjectBinding struct {
	ProjectID string
}

type WorkflowReference struct {
	Kind                    registry.WorkflowReferenceKind
	PlanID                  string
	CanonicalArtifactID     string
	CanonicalArtifactSHA256 string
	PassID                  string
	PassNumber              int64
	RunID                   string
	ExecutionSpecArtifactID string
	ExecutionSpecSHA256     string
	AuditPacketID           string
	AuditPacketSHA256       string
	AuditDecisionID         string
	Decision                string
	RecordedAt              string
}

type Attestation struct {
	Kind                    registry.AttestationKind
	InputName               string
	SubjectSHA256           string
	Confirmed               bool
	Approved                bool
	CompleteTransfer        bool
	SelectedMode            string
	ReviewedCandidateSHA256 string
	ReviewResult            string
	Complete                bool
	Clearance               *SensitiveDataClearance
}

type SensitiveDataClearance = registry.SensitiveDataClearance

type SensitiveDataDeclaration = registry.SensitiveDataDeclaration

type InputBinding struct {
	InputName       string
	InputRole       registry.InputRole
	SourceKind      registry.InputSourceKind
	DisplayName     string
	MediaType       string
	SHA256          string
	SizeBytes       int64
	AttestationKind registry.AttestationKind
	Source          InputSource
}

type InputSource struct {
	Kind                registry.InputSourceKind
	FileIndex           int64
	ArtifactID          string
	WorkflowReference   WorkflowReference
	SnapshotArtifactID  string
	SnapshotSHA256      string
	RepositoryBindingID string
	CommitOID           string
	TreeOID             string
	Path                PathIdentity
	BlobOID             string
}

type RepositoryBinding struct {
	RepositoryKey                        string
	RepositoryTarget                     string
	BindingOrder                         int64
	RevisionSource                       string
	ConfiguredWorkingBranchRef           string
	RepositoryTargetConfigurationVersion int64
	CommitOID                            string
	TreeOID                              string
	Anchors                              []Anchor
}

type GovernanceBinding struct {
	RepositoryKey                        string
	RepositoryTarget                     string
	Reserved                             bool
	RevisionSource                       string
	ConfiguredWorkingBranchRef           string
	RepositoryTargetConfigurationVersion int64
	CommitOID                            string
	TreeOID                              string
}

type Anchor struct {
	AnchorName string
	Purpose    registry.AnchorPurpose
	CommitOID  string
	TreeOID    string
}

type ManifestDomainBinding struct {
	ManifestPath    PathIdentity
	ManifestBlobOID string
	ManifestSHA256  string
	Domain          registry.ManifestDomain
	Members         []ManifestMember
}

type ManifestMember struct {
	MemberOrder int64
	Path        PathIdentity
	BlobOID     string
	ByteSize    int64
	SHA256      string
}

type PathIdentity struct {
	PathID          string
	ByteLength      int64
	PathBytesBase64 string
}
