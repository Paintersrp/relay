// Package guidedapp carries the narrow owner contracts the guided Feature
// journey consumes. It is deliberately free of persistence models so owner
// application packages can implement the contract without importing the
// Feature application.
package guidedapp

import "context"

// PackageState is the packages-owner semantic read of the current execution
// package for one workspace: none | prepared | approved. The approved state
// carries the linked Run's public identity and status so downstream owners can
// be composed without row re-derivation. PackageApprovalID is the packages
// owner's public package-approval identity resolved server-side; consumers
// never reconstruct it from rows.
type PackageState struct {
	State             string
	PackageID         string
	PackageSHA256     string
	PackageApprovalID string
	RunID             string
	RunStatus         string
	RunRepoTarget     string
	RunBranch         string
	RunBaseCommit     string
}

// ApprovePackageInput carries only workspace-level guided inputs; the package
// identity and digest are resolved server-side by the package owner.
type ApprovePackageInput struct {
	WorkspaceID string
	Evidence    string
}

// PreparePackageInput carries only workspace-level guided inputs; the active
// selection and the selected approved Delivery Ticket are resolved
// server-side by the package owner.
type PreparePackageInput struct {
	WorkspaceID string
}

// PreparePackageResult carries only the prepared package identity resolved
// server-side by the package owner.
type PreparePackageResult struct {
	PackageID string
	State     string
}

// PackageOwner is the package-owner surface required by the guided journey.
type PackageOwner interface {
	ReadWorkspacePackageState(context.Context, string) (PackageState, error)
	ApproveCurrentPackage(context.Context, ApprovePackageInput) error
	PrepareCurrentSelection(context.Context, PreparePackageInput) (PreparePackageResult, error)
}

// RunAuditState is the audits-owner semantic read of one package Run's audit
// progression: none | awaiting_audit | packet_recorded | decision_recorded.
// It carries only public audit identities resolved by the audit owner,
// including the recorded audit decision identity when a decision exists.
type RunAuditState struct {
	RunID           string
	RunStatus       string
	State           string
	AuditPacketID   string
	AuditDecisionID string
	AuditedCommit   string
	Diagnostics     []string
}

// RemediationState is the audits-owner semantic read of audit remediation
// seeds for one workspace: none | open | reopened.
type RemediationState struct {
	State   string
	SeedIDs []string
}

// AuditOwner is the audit/remediation-owner surface required by the guided
// journey. Consumers must not reconstruct audit or remediation state from
// packet, decision, or seed rows; the owner resolves those semantics.
type AuditOwner interface {
	ReadRunAuditState(context.Context, string) (RunAuditState, error)
	ReadWorkspaceRemediationState(context.Context, string) (RemediationState, error)
}

// ProgramState is the program-owner read used by the guided workspace. It is
// descriptive only: recording a program result does not advance delivery.
type ProgramState struct {
	Prepared []ProgramMember
	Dispatch []ProgramDispatch
	// Eligible are the Program Dispatch members whose accepted isolated audit
	// recorded durable integration eligibility and which no current Assignment
	// binds yet. They are the exact constituents available for a next
	// Integration Assignment subset.
	Eligible []ProgramEligibleMember
	// Integration are the immutable Relay-generated Integration Assignments of
	// the workspace's Program dispatches with their recorded verification
	// disposition. They are transport state only and never a second lifecycle.
	Integration []ProgramIntegrationAssignment
}
type ProgramMember struct {
	MemberID, State, Outcome, Branch, BranchHeadSHA, Blocker string
}
type ProgramDispatch struct {
	DispatchID, Status, RepoTarget, Branch, BaseCommit, LaterIntegrationRisks string
	Members                                                                   []ProgramMember
}

// ProgramEligibleMember is one integration-eligible Program Dispatch member:
// the exact recorded accepted commit and pushed branch of its isolated audit,
// and the exact approved Ticket revision the audit audited.
type ProgramEligibleMember struct {
	MemberID       string
	TicketID       string
	TicketRevision int64
	AcceptedCommit string
	PushedBranch   string
}

// ProgramIntegrationAssignment is one immutable Integration Assignment with
// its exact bound constituents and recorded verification disposition
// (none | passed | failed).
type ProgramIntegrationAssignment struct {
	AssignmentID string
	DispatchID   string
	Status       string
	RepoTarget   string
	Branch       string
	BaseCommit   string
	Verification string
	Members      []ProgramIntegrationMember
}

// ProgramIntegrationMember is one exact bound constituent of an Integration
// Assignment: the dispatch member, its Ticket identity and revision.
type ProgramIntegrationMember struct {
	MemberID       string
	TicketID       string
	TicketRevision int64
}

type ProgramOwner interface {
	ReadWorkspaceProgramState(context.Context, string) (ProgramState, error)
}
