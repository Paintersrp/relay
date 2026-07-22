package audits

import (
	"context"
	"fmt"

	workflowstore "relay/internal/store/workflow"
	workflowgenerated "relay/internal/store/workflowgenerated"
)

type AuditEffects struct {
	AuditDecision           workflowstore.AuditDecision                        `json:"audit_decision"`
	TicketRevisionDecisions []workflowstore.AuditTicketRevisionDecision        `json:"ticket_revision_decisions"`
	TicketSatisfactions     []workflowstore.DeliveryTicketRevisionSatisfaction `json:"ticket_satisfactions"`
	RemediationSeeds        []workflowstore.AuditRemediationSeed               `json:"remediation_seeds"`
}

type RemediationSeedDetail struct {
	RemediationSeed  workflowstore.AuditRemediationSeed          `json:"remediation_seed"`
	MaterialFindings []workflowstore.AuditRemediationSeedFinding `json:"material_findings"`
}

func (s *WorkflowAuditService) GetAuditEffects(ctx context.Context, auditDecisionID string) (any, error) {
	decision, err := s.store.GetAuditDecisionByDecisionID(ctx, auditDecisionID)
	if err != nil {
		return nil, err
	}
	decisions, err := workflowgenerated.New(s.store.DB()).ListAuditTicketRevisionDecisions(ctx, decision.ID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB().QueryContext(ctx, `
SELECT id, delivery_ticket_revision_row_id, audit_ticket_revision_decision_row_id, created_at
FROM delivery_ticket_revision_satisfactions
WHERE audit_ticket_revision_decision_row_id IN (
    SELECT id FROM audit_ticket_revision_decisions WHERE audit_decision_row_id = ?
)
ORDER BY id`, decision.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	satisfactions := []workflowstore.DeliveryTicketRevisionSatisfaction{}
	for rows.Next() {
		var value workflowstore.DeliveryTicketRevisionSatisfaction
		if err := rows.Scan(&value.ID, &value.DeliveryTicketRevisionRowID, &value.AuditTicketRevisionDecisionRowID, &value.CreatedAt); err != nil {
			return nil, err
		}
		satisfactions = append(satisfactions, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seedRows, err := s.store.DB().QueryContext(ctx, `
SELECT id, remediation_seed_id, audit_ticket_revision_decision_row_id, audit_packet_row_id,
       execution_package_row_id, audited_commit, decision_rationale, created_at
FROM audit_remediation_seeds
WHERE audit_ticket_revision_decision_row_id IN (
    SELECT id FROM audit_ticket_revision_decisions WHERE audit_decision_row_id = ?
)
ORDER BY id`, decision.ID)
	if err != nil {
		return nil, err
	}
	defer seedRows.Close()
	seeds := []workflowstore.AuditRemediationSeed{}
	for seedRows.Next() {
		var value workflowstore.AuditRemediationSeed
		if err := seedRows.Scan(&value.ID, &value.RemediationSeedID, &value.AuditTicketRevisionDecisionRowID, &value.AuditPacketRowID, &value.ExecutionPackageRowID, &value.AuditedCommit, &value.DecisionRationale, &value.CreatedAt); err != nil {
			return nil, err
		}
		seeds = append(seeds, value)
	}
	if err := seedRows.Err(); err != nil {
		return nil, err
	}
	return AuditEffects{decision, decisions, satisfactions, seeds}, nil
}

func (s *WorkflowAuditService) GetRemediationSeed(ctx context.Context, remediationSeedID string) (any, error) {
	seed, err := s.store.GetAuditRemediationSeedBySeedID(ctx, remediationSeedID)
	if err != nil {
		return nil, err
	}
	findings, err := s.store.ListAuditRemediationSeedFindings(ctx, seed.ID)
	if err != nil {
		return nil, err
	}
	if seed.RemediationSeedID != remediationSeedID {
		return nil, fmt.Errorf("remediation seed identity mismatch")
	}
	return RemediationSeedDetail{seed, findings}, nil
}
