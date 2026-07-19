package operations

import (
	"database/sql"

	"relay/internal/app/packages"
	workflowstore "relay/internal/store/workflow"
)

// PackageIdentityView is the transport-safe identity and basis of an execution
// package. It is owned by the application service so transports never project
// workflow-store records directly.
type PackageIdentityView struct {
	PackageID              string `json:"packageId"`
	SelectionRowID         int64  `json:"selectionRowId"`
	WorkspaceRowID         int64  `json:"workspaceRowId"`
	RepoTarget             string `json:"repoTarget"`
	Branch                 string `json:"branch"`
	BaseCommit             string `json:"baseCommit"`
	SourceClosureRowID     int64  `json:"sourceClosureRowId"`
	AuthorityRevisionRowID int64  `json:"authorityRevisionRowId"`
	PackageSHA256          string `json:"packageSha256"`
	AuthoritySHA256        string `json:"authoritySha256"`
	SourceSHA256           string `json:"sourceSha256"`
	DesignBriefSHA256      string `json:"designBriefSha256"`
	ExecutionSpecSHA256    string `json:"executionSpecSha256"`
	CreatedAt              string `json:"createdAt"`
}

type PackageMemberView struct {
	SelectionMemberRowID int64  `json:"selectionMemberRowId"`
	Sequence             int64  `json:"sequence"`
	RevisionRowID        int64  `json:"revisionRowId"`
	MemberSHA256         string `json:"memberSha256"`
}

type PackageApprovalBindingView struct {
	PackageMemberRowID     int64  `json:"packageMemberRowId"`
	ApprovalRowID          int64  `json:"approvalRowId"`
	AuthorityRevisionRowID int64  `json:"authorityRevisionRowId"`
	SourceClosureRowID     int64  `json:"sourceClosureRowId"`
	ApprovalBasisSHA256    string `json:"approvalBasisSha256"`
	CreatedAt              string `json:"createdAt"`
}

type PackageArtifactView struct {
	DisplayName  string `json:"displayName"`
	RelativePath string `json:"relativePath"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type RunView struct {
	RunID       string `json:"runId"`
	FeatureSlug string `json:"featureSlug"`
	RepoTarget  string `json:"repoTarget"`
	Branch      string `json:"branch"`
	BaseCommit  string `json:"baseCommit"`
	Status      string `json:"status"`
}

type PackageDetailView struct {
	PackageIdentityView
	Members            []PackageMemberView          `json:"members"`
	ApprovalBindings   []PackageApprovalBindingView `json:"approvalBindings"`
	TicketDesignBriefs []PackageArtifactView        `json:"ticketDesignBriefs"`
	ExecutionSpec      PackageArtifactView          `json:"executionSpec"`
	Run                *RunView                     `json:"run"`
	PackageApprovalID  string                       `json:"packageApprovalId,omitempty"`
}

type PackageApprovalView struct {
	Package           PackageIdentityView `json:"package"`
	Run               RunView             `json:"run"`
	PackageApprovalID string              `json:"packageApprovalId"`
}

// MutationLeaseView intentionally contains durable Run and lease identities
// plus operator-safe state. It excludes repository paths, process data, and
// reconciliation notes.
type MutationLeaseView struct {
	LeaseID                 string  `json:"leaseId"`
	RunID                   string  `json:"runId"`
	OwnerRunID              string  `json:"ownerRunId"`
	RepoTarget              string  `json:"repoTarget"`
	Branch                  string  `json:"branch"`
	State                   string  `json:"state"`
	Certainty               string  `json:"certainty"`
	ReconciliationState     string  `json:"reconciliationState"`
	AcquiredAt              string  `json:"acquiredAt"`
	ReleasedAt              *string `json:"releasedAt"`
	ReconciliationStartedAt *string `json:"reconciliationStartedAt"`
	ReconciledAt            *string `json:"reconciledAt"`
}

type MutationLeaseReconcileView struct {
	Released bool               `json:"released"`
	Lease    *MutationLeaseView `json:"lease"`
}

func packageDetailView(value packages.Detail) PackageDetailView {
	view := PackageDetailView{
		PackageIdentityView: packageIdentityView(value.Package),
		Members:             make([]PackageMemberView, 0, len(value.Members)),
		ApprovalBindings:    make([]PackageApprovalBindingView, 0, len(value.ApprovalBindings)),
		TicketDesignBriefs:  make([]PackageArtifactView, 0, len(value.Briefs)),
		ExecutionSpec:       packageArtifactView(value.ExecutionSpec),
		PackageApprovalID:   value.PackageApprovalID,
	}
	for _, member := range value.Members {
		view.Members = append(view.Members, PackageMemberView{SelectionMemberRowID: member.SelectionMemberRowID, Sequence: member.Sequence, RevisionRowID: member.RevisionRowID, MemberSHA256: member.MemberSha256})
	}
	for _, binding := range value.ApprovalBindings {
		view.ApprovalBindings = append(view.ApprovalBindings, PackageApprovalBindingView{PackageMemberRowID: binding.PackageMemberRowID, ApprovalRowID: binding.ApprovalRowID, AuthorityRevisionRowID: binding.AuthorityRevisionRowID, SourceClosureRowID: binding.SourceClosureRowID, ApprovalBasisSHA256: binding.ApprovalBasisSha256, CreatedAt: binding.CreatedAt})
	}
	for _, brief := range value.Briefs {
		view.TicketDesignBriefs = append(view.TicketDesignBriefs, packageArtifactView(brief))
	}
	if value.Run != nil {
		run := runView(*value.Run)
		view.Run = &run
	}
	return view
}

func packageIdentityView(value workflowstore.ExecutionPackage) PackageIdentityView {
	return PackageIdentityView{PackageID: value.PackageID, SelectionRowID: value.SelectionRowID, WorkspaceRowID: value.WorkspaceRowID, RepoTarget: value.RepoTarget, Branch: value.Branch, BaseCommit: value.BaseCommit, SourceClosureRowID: value.SourceClosureRowID, AuthorityRevisionRowID: value.AuthorityRevisionRowID, PackageSHA256: value.PackageSha256, AuthoritySHA256: value.AuthoritySha256, SourceSHA256: value.SourceSha256, DesignBriefSHA256: value.DesignBriefSha256, ExecutionSpecSHA256: value.ExecutionSpecSha256, CreatedAt: value.CreatedAt}
}

func packageArtifactView(value packages.PackageArtifact) PackageArtifactView {
	return PackageArtifactView{DisplayName: value.DisplayName, RelativePath: value.RelativePath, SHA256: value.SHA256, SizeBytes: value.SizeBytes}
}

func runView(value workflowstore.Run) RunView {
	return RunView{RunID: value.RunID, FeatureSlug: value.FeatureSlug, RepoTarget: value.RepoTarget, Branch: value.Branch, BaseCommit: value.BaseCommit, Status: value.Status}
}

func mutationLeaseView(status MutationLeaseStatus) *MutationLeaseView {
	if status.Lease == nil {
		return nil
	}
	lease := status.Lease
	return &MutationLeaseView{LeaseID: lease.LeaseID, RunID: status.Run.RunID, OwnerRunID: lease.OwnerIdentity, RepoTarget: lease.RepoTarget, Branch: lease.Branch, State: lease.State, Certainty: lease.UncertaintyState, ReconciliationState: lease.ReconciliationState, AcquiredAt: lease.AcquiredAt, ReleasedAt: nullableLeaseString(lease.ReleasedAt), ReconciliationStartedAt: nullableLeaseString(lease.ReconciliationStartedAt), ReconciledAt: nullableLeaseString(lease.ReconciledAt)}
}

func nullableLeaseString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
