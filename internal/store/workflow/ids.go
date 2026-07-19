package workflowstore

import "github.com/google/uuid"

func NewProjectID() string {
	return "project-" + uuid.NewString()
}

func NewProjectNoteID() string {
	return "note-" + uuid.NewString()
}

func NewPlanID() string {
	return "plan-" + uuid.NewString()
}

func NewPassID() string {
	return "pass-" + uuid.NewString()
}

func NewRunID() string {
	return "run-" + uuid.NewString()
}

func NewExecutionAttemptID() string {
	return "attempt-" + uuid.NewString()
}

func NewArtifactID() string {
	return "artifact-" + uuid.NewString()
}

func NewOperationPacketID() string {
	return "opkt-" + uuid.NewString()
}

func NewOperationPacketPublicationID() string {
	return "publication-" + uuid.NewString()
}

func NewSourceVaultID() string {
	return "vault-" + uuid.NewString()
}

func NewSourceVaultClosureID() string {
	return "closure-" + uuid.NewString()
}

func NewSourceVaultRetentionID() string {
	return "retention-" + uuid.NewString()
}

func NewAuditPacketID() string {
	return "packet-" + uuid.NewString()
}

func NewAuditDecisionID() string {
	return "audit-" + uuid.NewString()
}

func NewAuditRemediationSeedID() string {
	return "remediation-" + uuid.NewString()
}

func NewFeatureWorkspaceID() string {
	return "workspace-" + uuid.NewString()
}

func NewFeatureWorkspaceInputID() string {
	return "input-" + uuid.NewString()
}

func NewFeatureWorkspaceDestinationID() string {
	return "destination-" + uuid.NewString()
}

func NewFeatureWorkspaceDiscoveryTicketID() string {
	return "discovery-" + uuid.NewString()
}

func NewFeatureWorkspaceResolutionID() string {
	return "resolution-" + uuid.NewString()
}

func NewFeatureWorkspaceRouteStateID() string {
	return "route-" + uuid.NewString()
}

func NewFeatureWorkspaceAuthorityRevisionID() string {
	return "authority-" + uuid.NewString()
}

func NewFeatureWorkspaceCompletionDecisionID() string {
	return "completion-" + uuid.NewString()
}

func NewDeliveryTicketSelectionID() string {
	return "selection-" + uuid.NewString()
}

func NewDeliveryTicketApprovalID() string {
	return "approval-" + uuid.NewString()
}

func NewExecutionPackageID() string {
	return "package-" + uuid.NewString()
}

func NewRepositoryBranchMutationLeaseID() string {
	return "lease-" + uuid.NewString()
}
