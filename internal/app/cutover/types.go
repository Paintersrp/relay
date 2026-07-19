package cutover

import (
	"errors"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrCutoverNotFound        = errors.New("cutover activation not found")
	ErrCutoverAlreadyActive   = errors.New("a cutover activation is already active")
	ErrCutoverNotReady        = errors.New("cutover activation is not ready")
	ErrCutoverNotActive       = errors.New("cutover activation is not active")
	ErrCutoverRollbackBlocked = errors.New("cutover rollback is blocked after first new execution")
	ErrCutoverBoundaryCrossed = errors.New("cutover execution boundary is already crossed")
	ErrLegacyAdmissionClosed  = errors.New("legacy admission is closed after cutover activation")
)

type State struct {
	ActivationID     string
	Status           string
	BoundaryStatus   string
	RollbackStatus   string
	RollForwardStatus string
	ActivatedAt      *string
}

func stateFrom(activation workflowstore.CutoverActivation) State {
	result := State{
		ActivationID:      activation.CutoverActivationID,
		Status:            activation.ActivationStatus,
		BoundaryStatus:    activation.ExecutionBoundaryStatus,
		RollbackStatus:    activation.RollbackStatus,
		RollForwardStatus: activation.RollForwardStatus,
	}
	if activation.ActivatedAt.Valid {
		result.ActivatedAt = &activation.ActivatedAt.String
	}
	return result
}

type Readiness struct {
	Ready               bool
	Prepared            bool
	Active              bool
	BoundaryCrossed     bool
	Prerequisites        []string
	Obligations          []string
	RollForwardCriteria   []string
	Evidence            []PrerequisiteEvidence
	ActivationEvidence  []ObligationEvidence
}

type PrerequisiteEvidence struct {
	Prerequisite string
	Evidence     string
}

type ObligationEvidence struct {
	Kind       string
	Obligation string
	Evidence   string
}

type ActivationRequest struct {
	ActivationID string
	ActivatedAt  string
}

type RollbackRequest struct {
	ActivationID string
	RolledBackAt string
}

type BoundaryRequest struct {
	ActivationID string
	RunID        string
	RunRowID     int64
	CrossedAt    string
}

type RollForwardEvidenceRequest struct {
	ActivationID      string
	CriterionSequence int64
	Evidence          string
}

type LegacyGateDecision struct {
	Allowed        bool
	Reason         string
	IsRead         bool
	IsRemediation  bool
	IsContinuation bool
}
