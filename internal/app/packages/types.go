package packages

import (
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

// PrepareInput carries only the active selection identity and the optional
// Deterministic Operations artifact. The selected approved Delivery Ticket is
// the sole ticket semantic authority: its exact source-vault bytes and
// deterministic projection are resolved server-side from the selection, never
// accepted from the caller.
type PrepareInput struct {
	SelectionID             string
	DeterministicOperations *ArtifactInput
}

type PackageArtifact struct {
	DisplayName  string
	RelativePath string
	SHA256       string
	SizeBytes    int64
}

// PrepareResult exposes the immutable package plus the server-resolved
// selected approved Delivery Ticket basis: the retained Ticket, its selected
// revision, the exact source-vault document metadata, and the deterministic
// projection. No Brief identity, bytes, digest, or projection is present.
type PrepareResult struct {
	Package          workflowstore.ExecutionPackage
	Members          []workflowstore.ExecutionPackageMember
	Ticket           workflowstore.DeliveryTicket
	TicketRevision   workflowstore.DeliveryTicketRevision
	TicketDocument   PackageArtifact
	TicketProjection speccompiler.DeliveryTicketProjection
	Operations       *PackageArtifact
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
// audit owners. A nil Run means the immutable package is still unapproved. It
// carries no Brief identity, bytes, digest, or projection.
type Detail struct {
	Package                 workflowstore.ExecutionPackage
	Members                 []workflowstore.ExecutionPackageMember
	ApprovalBindings        []workflowstore.ExecutionPackageApprovalBinding
	Ticket                  workflowstore.DeliveryTicket
	TicketRevision          workflowstore.DeliveryTicketRevision
	TicketDocument          PackageArtifact
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

// ApprovedSourceDocument is the exact retained Git source document for the
// selected Delivery Ticket revision. It is intentionally distinct from
// ApprovedDocument because the bytes come from the source-vault closure rather
// than the managed artifact store.
type ApprovedSourceDocument struct {
	DisplayName  string
	RelativePath string
	MediaType    string
	SHA256       string
	ObjectOID    string
	SizeBytes    int64
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

// ApprovedCompletedDependency is the canonical completed-dependency record for
// one satisfied dependency of the selected Delivery Ticket revision, as loaded
// and verified by the approved package basis. It carries the dependency
// sequence, the depends-on Ticket ID, its revision number, and its stored
// outcome ("satisfied"). The package basis requires every dependency to be a
// completed, current outcome.
type ApprovedCompletedDependency struct {
	Sequence int64
	TicketID string
	Revision int64
	Outcome  string
}

type ApprovedDeterministicOperations struct {
	ApprovedDocument
	Coverage string
	Document *speccompiler.DeterministicOperationsDocument
}

// ApprovedAuthority is the bounded authority surface for an approved package
// Run. It exposes the retained Delivery Ticket (exact source-vault bytes) and
// its deterministic projection; the Ticket Design Brief is no longer an
// authority surface and never appears here.
type ApprovedAuthority struct {
	Run             workflowstore.Run
	Package         workflowstore.ExecutionPackage
	PackageApproval workflowstore.ExecutionPackageApproval

	Workspace workflowstore.FeatureWorkspace
	Authority workflowstore.FeatureWorkspaceAuthorityRevision
	Source    workflowstore.SourceVaultClosure

	Ticket                workflowstore.DeliveryTicket
	TicketRevision        workflowstore.DeliveryTicketRevision
	TicketMembers         []workflowstore.DeliveryTicketRevisionMember
	TicketDependencies    []workflowstore.DeliveryTicketRevisionDependency
	CompletedDependencies []ApprovedCompletedDependency
	TicketApproval        workflowstore.DeliveryTicketRevisionApproval

	AuthorityLayers []ApprovedAuthorityLayer

	// RepositoryInstructions is the exact verified repository-instruction
	// basis: every AGENTS.md applicable to the selected Ticket's inspected
	// source paths, resolved from the exact selected source closure, in
	// deterministic repository-relative-path order. The package identity and
	// the compound approval bind the same ordered identities.
	RepositoryInstructions []ApprovedRepositoryInstruction

	DeliveryTicket   ApprovedSourceDocument
	TicketProjection speccompiler.DeliveryTicketProjection

	DeterministicOperations *ApprovedDeterministicOperations
}
