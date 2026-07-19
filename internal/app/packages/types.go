package packages

import workflowstore "relay/internal/store/workflow"

// ArtifactInput carries exact caller-supplied bytes. The caller must provide
// the canonical filename and digest alongside the bytes; package preparation
// never normalizes or reserializes them.
type ArtifactInput struct {
	DisplayName    string
	ExpectedSHA256 string
	Bytes          []byte
}

type PrepareInput struct {
	SelectionID        string
	TicketDesignBriefs []ArtifactInput
	ExecutionSpec      ArtifactInput
}

type PackageArtifact struct {
	DisplayName  string
	RelativePath string
	SHA256       string
	SizeBytes    int64
}

type PrepareResult struct {
	Package       workflowstore.ExecutionPackage
	Members       []workflowstore.ExecutionPackageMember
	Briefs        []PackageArtifact
	ExecutionSpec PackageArtifact
}

type ApproveInput struct {
	PackageID                   string
	ExpectedPackageSha256       string
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
	Package           workflowstore.ExecutionPackage
	Members           []workflowstore.ExecutionPackageMember
	ApprovalBindings  []workflowstore.ExecutionPackageApprovalBinding
	Briefs            []PackageArtifact
	ExecutionSpec     PackageArtifact
	Run               *workflowstore.Run
	PackageApprovalID string
}
