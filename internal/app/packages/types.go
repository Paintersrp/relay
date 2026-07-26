package packages

import (
	"relay/internal/planningartifacts"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

// ArtifactInput carries exact caller-supplied bytes. The caller must provide
// the canonical filename and digest alongside the bytes; package preparation
// never normalizes or reserializes them.
type ArtifactInput struct {
	DisplayName    string
	ExpectedSHA256 string
	Bytes          []byte
}

type PrepareInput struct {
	SelectionID             string
	TicketDesignBrief       ArtifactInput
	DeterministicOperations *ArtifactInput
}

type PackageArtifact struct {
	DisplayName  string
	RelativePath string
	SHA256       string
	SizeBytes    int64
}

type PrepareResult struct {
	Package                 workflowstore.ExecutionPackage
	Members                 []workflowstore.ExecutionPackageMember
	TicketDesignBrief       PackageArtifact
	DeterministicOperations *PackageArtifact
}

type ApproveInput struct {
	PackageID                    string
	ExpectedPackageSha256        string
	OperatorConfirmationEvidence string
}

type ApproveResult struct {
	Package         workflowstore.ExecutionPackage
	Run             workflowstore.Run
	RunArtifacts    []workflowstore.Artifact
	PackageApproval workflowstore.ExecutionPackageApproval
}

// Detail is the bounded package projection used by later operation, UI, and
// audit owners. A nil Run means the immutable package is still unapproved.
type Detail struct {
	Package                 workflowstore.ExecutionPackage
	Members                 []workflowstore.ExecutionPackageMember
	ApprovalBindings        []workflowstore.ExecutionPackageApprovalBinding
	TicketDesignBrief       PackageArtifact
	DeterministicOperations *PackageArtifact
	Run                     *workflowstore.Run
	PackageApprovalID       string
}

type ApprovedDocument struct {
	DisplayName  string
	RelativePath string
	MediaType    string
	SHA256       string
	Bytes        []byte
}

type ApprovedAuthorityLayer struct {
	Layer        workflowstore.FeatureWorkspaceAuthorityLayer
	Kind         string
	Sequence     int64
	RelativePath string
	MediaType    string
	SHA256       string
	SizeBytes    int64
	Bytes        []byte
}

type ApprovedDeterministicOperations struct {
	ApprovedDocument
	Coverage string
	Document *speccompiler.DeterministicOperationsDocument
}

type ApprovedAuthority struct {
	Run             workflowstore.Run
	Package         workflowstore.ExecutionPackage
	PackageApproval workflowstore.ExecutionPackageApproval

	Workspace workflowstore.FeatureWorkspace
	Authority workflowstore.FeatureWorkspaceAuthorityRevision
	Source    workflowstore.SourceVaultClosure

	Ticket             workflowstore.DeliveryTicket
	TicketRevision     workflowstore.DeliveryTicketRevision
	TicketMembers      []workflowstore.DeliveryTicketRevisionMember
	TicketDependencies []workflowstore.DeliveryTicketRevisionDependency
	TicketApproval     workflowstore.DeliveryTicketRevisionApproval

	AuthorityLayers []ApprovedAuthorityLayer

	TicketDesignBrief ApprovedDocument
	BriefProjection   planningartifacts.TicketDesignBriefProjection

	DeterministicOperations *ApprovedDeterministicOperations
}
