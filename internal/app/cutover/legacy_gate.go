package cutover

import (
	"context"
)

// LegacyGate provides typed admission decisions for new legacy entries.
// Historical reads and eligible persisted Run continuation are never gate decisions.
type LegacyGate struct {
	service *Service
}

func NewLegacyGate(service *Service) *LegacyGate {
	if service == nil {
		return nil
	}
	return &LegacyGate{service: service}
}

// AllowNewPlan returns a gate decision for a new Plan submission.
func (g *LegacyGate) AllowNewPlan(ctx context.Context) (LegacyGateDecision, error) {
	closed, err := g.service.IsLegacyAdmissionClosed(ctx)
	if err != nil {
		return LegacyGateDecision{Allowed: false, Reason: "cutover_unavailable"}, err
	}
	if closed {
		return LegacyGateDecision{Allowed: false, Reason: "legacy_admission_closed"}, nil
	}
	return LegacyGateDecision{Allowed: true}, nil
}

// AllowNewManagedRun returns a gate decision for a new managed Plan/pass Run.
func (g *LegacyGate) AllowNewManagedRun(ctx context.Context) (LegacyGateDecision, error) {
	return g.AllowNewPlan(ctx)
}

// AllowPlanMutation returns a gate decision for Plan mutation (read is always allowed).
func (g *LegacyGate) AllowPlanMutation(ctx context.Context) (LegacyGateDecision, error) {
	closed, err := g.service.IsLegacyAdmissionClosed(ctx)
	if err != nil {
		return LegacyGateDecision{Allowed: false, Reason: "cutover_unavailable"}, err
	}
	if closed {
		return LegacyGateDecision{Allowed: false, Reason: "legacy_admission_closed"}, nil
	}
	return LegacyGateDecision{Allowed: true}, nil
}
