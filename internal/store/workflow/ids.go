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
