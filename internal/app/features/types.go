package features

import (
	"database/sql"

	workflowstore "relay/internal/store/workflow"
)

type (
	GoverningArtifactApproval = workflowstore.GoverningArtifactApproval

	RecordAuthorityApprovalInput struct {
		WorkspaceID                  string
		Family                       string
		ArtifactRowID                sql.NullInt64
		RetainedArtifact             sql.NullInt64
		ArtifactSHA256               string
		OperatorConfirmationEvidence string
	}

	RecordAuthorityApprovalResult struct {
		Approval  GoverningArtifactApproval
		Workspace workflowstore.FeatureWorkspace
	}

	PublishApprovedAuthorityInput struct {
		WorkspaceID     string
		ExpectedVersion int64
		SourceClosureID sql.NullInt64
		Layers          []AuthorityLayerInput
	}

	PublishApprovedAuthorityResult struct {
		Detail    AuthorityRevisionDetail
		Workspace workflowstore.FeatureWorkspace
	}

	ApprovalRevisionDetail struct {
		Revision workflowstore.FeatureWorkspaceAuthorityRevision
		Layers   []workflowstore.FeatureWorkspaceAuthorityLayer
	}
)
