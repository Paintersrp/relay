package cutover

import (
	"context"
	"fmt"
	"strings"
	"time"

	workflowstore "relay/internal/store/workflow"
)

type Service struct {
	store *workflowstore.Store
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return &Service{store: store}, nil
}

// State returns the current cutover state or nil when no current activation exists.
func (s *Service) State(ctx context.Context) (*State, bool, error) {
	activation, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		return nil, found, err
	}
	state := stateFrom(activation)
	return &state, true, nil
}

// Readiness returns a full readiness projection for one activation.
func (s *Service) Readiness(ctx context.Context, activationID string) (*Readiness, error) {
	activation, err := s.store.GetCutoverActivationByID(ctx, activationID)
	if err != nil {
		return nil, err
	}
	prereqs, err := s.store.ListCutoverActivationPrerequisites(ctx, activation.ID)
	if err != nil {
		return nil, err
	}
	obligations, err := s.store.ListCutoverActivationObligations(ctx, activation.ID)
	if err != nil {
		return nil, err
	}
	criteria, err := s.store.ListCutoverRollForwardCriteria(ctx, activation.ID)
	if err != nil {
		return nil, err
	}
	result := &Readiness{
		Prepared:            activation.ActivationStatus == "prepared",
		Active:              activation.ActivationStatus == "active",
		BoundaryCrossed:     activation.ExecutionBoundaryStatus == "crossed",
		Ready:               activation.ActivationStatus == "prepared",
		Prerequisites:        make([]string, 0, len(prereqs)),
		Obligations:          make([]string, 0, len(obligations)),
		RollForwardCriteria:   make([]string, 0, len(criteria)),
		Evidence:            make([]PrerequisiteEvidence, 0, len(prereqs)),
		ActivationEvidence:  make([]ObligationEvidence, 0, len(obligations)),
	}
	for _, p := range prereqs {
		result.Prerequisites = append(result.Prerequisites, p.Prerequisite)
		result.Evidence = append(result.Evidence, PrerequisiteEvidence{Prerequisite: p.Prerequisite, Evidence: p.Evidence})
	}
	for _, o := range obligations {
		result.Obligations = append(result.Obligations, o.Obligation)
		result.ActivationEvidence = append(result.ActivationEvidence, ObligationEvidence{Kind: o.ObligationKind, Obligation: o.Obligation, Evidence: o.Evidence})
	}
	for _, c := range criteria {
		result.RollForwardCriteria = append(result.RollForwardCriteria, c.CompletionCriterion)
	}
	return result, nil
}

// History returns all activation records.
func (s *Service) History(ctx context.Context) ([]State, error) {
	values, err := s.store.ListAllCutoverActivations(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]State, 0, len(values))
	for _, value := range values {
		result = append(result, stateFrom(value))
	}
	return result, nil
}

// Activate atomically transitions a prepared activation to active and sets the current state.
func (s *Service) Activate(ctx context.Context, request ActivationRequest) (*State, error) {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" {
		return nil, ErrCutoverNotFound
	}
	if request.ActivatedAt == "" {
		request.ActivatedAt = canonicalTime(time.Now())
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return nil, err
	}
	if activation.ActivationStatus != "prepared" {
		return nil, ErrCutoverNotReady
	}
	_, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil {
		return nil, err
	}
	if found {
		return nil, ErrCutoverAlreadyActive
	}
	var updated workflowstore.CutoverActivation
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		updated, err = tx.ActivateCutover(ctx, request.ActivationID, request.ActivatedAt, activation.RollbackEligibility)
		if err != nil {
			return err
		}
		return tx.SetCutoverCurrentState(ctx, updated.ID)
	})
	if err != nil {
		return nil, err
	}
	state := stateFrom(updated)
	return &state, nil
}

// Rollback reverts an active but pre-boundary activation to rolled_back.
func (s *Service) Rollback(ctx context.Context, request RollbackRequest) (*State, error) {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" {
		return nil, ErrCutoverNotFound
	}
	if request.RolledBackAt == "" {
		request.RolledBackAt = canonicalTime(time.Now())
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return nil, err
	}
	if activation.ExecutionBoundaryStatus == "crossed" {
		return nil, ErrCutoverRollbackBlocked
	}
	if activation.RollbackEligibility != "eligible" {
		return nil, ErrCutoverRollbackBlocked
	}
	var updated workflowstore.CutoverActivation
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		updated, err = tx.RollbackCutover(ctx, request.ActivationID, request.RolledBackAt)
		if err != nil {
			return err
		}
		return tx.ClearCutoverCurrentState(ctx)
	})
	if err != nil {
		return nil, err
	}
	state := stateFrom(updated)
	return &state, nil
}

// CrossExecutionBoundary atomically records the first qualifying ticket-oriented Run
// execution crossing. It validates the activation is active with an open boundary,
// the Run is ticket-oriented (has an execution package), and the conditional crossing
// query enforces post-activation timing, matching authority, and immutable package approval.
func (s *Service) CrossExecutionBoundary(ctx context.Context, request BoundaryRequest) error {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" || request.RunRowID < 1 {
		return fmt.Errorf("invalid boundary crossing request")
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return err
	}
	if activation.ActivationStatus != "active" {
		return ErrCutoverNotActive
	}
	if activation.ExecutionBoundaryStatus == "crossed" {
		return ErrCutoverBoundaryCrossed
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConditionalCrossCutoverExecutionBoundary(ctx, request.ActivationID, request.RunRowID)
		return err
	})
}

// RecordRollForwardEvidence records one roll-forward evidence item.
func (s *Service) RecordRollForwardEvidence(ctx context.Context, request RollForwardEvidenceRequest) error {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" || request.CriterionSequence < 1 || strings.TrimSpace(request.Evidence) == "" {
		return fmt.Errorf("invalid roll-forward evidence request")
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return err
	}
	if activation.RollForwardStatus != "required" {
		return ErrCutoverNotActive
	}
	criteria, err := s.store.ListCutoverRollForwardCriteria(ctx, activation.ID)
	if err != nil {
		return err
	}
	var criterionRowID int64
	found := false
	for _, c := range criteria {
		if c.Sequence == request.CriterionSequence {
			criterionRowID = c.ID
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("roll-forward criterion sequence %d not found", request.CriterionSequence)
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateCutoverRollForwardEvidence(ctx, activation.ID, criterionRowID, request.Evidence)
		if err != nil {
			return err
		}
		evidence, err := s.store.ListCutoverRollForwardEvidence(ctx, activation.ID)
		if err != nil {
			return err
		}
		if len(evidence) == len(criteria) {
			_, err = tx.CompleteCutoverRollForward(ctx, request.ActivationID)
		}
		return err
	})
}

// IsLegacyAdmissionClosed returns true when the cutover is active and legacy admission is closed.
func (s *Service) IsLegacyAdmissionClosed(ctx context.Context) (bool, error) {
	activation, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		return false, err
	}
	return activation.ActivationStatus == "active", nil
}

// IsBoundaryCrossed returns true when the cutover boundary has been crossed.
func (s *Service) IsBoundaryCrossed(ctx context.Context) (bool, error) {
	activation, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		return false, err
	}
	return activation.ExecutionBoundaryStatus == "crossed", nil
}

// ErrLegacyAdmission converts internal state to a typed admission error.
func ErrLegacyAdmission() error {
	return ErrLegacyAdmissionClosed
}

func canonicalTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}


