package workflowstore

import "github.com/google/uuid"

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

func NewAuditPacketID() string {
	return "packet-" + uuid.NewString()
}

func NewAuditDecisionID() string {
	return "audit-" + uuid.NewString()
}
