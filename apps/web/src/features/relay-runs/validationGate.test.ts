import { describe, it, expect } from 'vitest';
import { evaluateValidationGate, evaluateExecuteValidationAction, evaluateRepairEligibility, isAuditCandidateStatus } from './validationGate';

describe('Validation Gate Predicate Matrix', () => {
  it('validation_progress_json without validation_run_json keeps Generate Audit disabled', () => {
    const artifacts = [{ storageKind: 'validation_progress_json' }];
    const status = 'validation_passed';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(false);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('validation_failed with validation_run_json keeps Generate Audit disabled', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
    const status = 'validation_failed';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(true);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('validation_failed_accepted without validation_run_json keeps Generate Audit disabled', () => {
    const artifacts: any[] = [];
    const status = 'validation_failed_accepted';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(false);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('validation_failed_accepted with validation_run_json but missing validation_failure_acceptance_json keeps Generate Audit disabled', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
    const status = 'validation_failed_accepted';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(true);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('validation_failed_accepted with validation_run_json and validation_failure_acceptance_json enables Generate Audit', () => {
    const artifacts = [
      { storageKind: 'validation_run_json' },
      { storageKind: 'validation_failure_acceptance_json' },
    ];
    const status = 'validation_failed_accepted';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(true);
    expect(result.validationAllowsAudit).toBe(true);
    expect(result.auditBlockedByValidation).toBe(false);
  });

  it('validation_passed with validation_run_json enables Generate Audit', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
    const status = 'validation_passed';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(true);
    expect(result.validationAllowsAudit).toBe(true);
    expect(result.auditBlockedByValidation).toBe(false);
  });

  it('Local validation running keeps Generate Audit disabled', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
    const status = 'local_validation_running';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('executor_done with validation_run_json is blocked by validation gate', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
    const status = 'executor_done';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(true);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('executor_blocked with validation_run_json is blocked by validation gate', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
    const status = 'executor_blocked';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(true);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('executor_done without any validation artifacts is blocked by validation gate', () => {
    const artifacts: any[] = [];
    const status = 'executor_done';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(false);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  it('validation_passed with only progress evidence is blocked by validation gate', () => {
    const artifacts = [{ storageKind: 'validation_progress_json' }];
    const status = 'validation_passed';
    const result = evaluateValidationGate(artifacts, status);

    expect(result.hasFinalValidationEvidence).toBe(false);
    expect(result.validationAllowsAudit).toBe(false);
    expect(result.auditBlockedByValidation).toBe(true);
  });

  describe('Audit candidate status predicate', () => {
    const candidateStatuses = ['executor_done', 'executor_blocked', 'validation_passed', 'validation_failed', 'validation_failed_accepted', 'local_validation_running'];
    const nonCandidateStatuses = ['pending', 'preparing', 'briefing', 'approved', 'executor_running', 'accepted', 'accepted_with_warnings', 'audit_ready', 'audit_ready_for_review', 'revision_required', 'blocked', 'completed'];

    for (const status of candidateStatuses) {
      it(`returns true for ${status}`, () => {
        expect(isAuditCandidateStatus(status)).toBe(true);
      });
    }

    for (const status of nonCandidateStatuses) {
      it(`returns false for ${status}`, () => {
        expect(isAuditCandidateStatus(status)).toBe(false);
      });
    }
  });

  describe('Execute Route Local Validation Rerun & Visibility Matrix', () => {
    it('progress-only evidence does not suppress validation need (canRunValidation should be true)', () => {
      const artifacts = [{ storageKind: 'validation_progress_json' }];
      const status = 'executor_done';
      const canRun = evaluateExecuteValidationAction(artifacts, status);
      expect(canRun).toBe(true);
    });

    it('validation failed with final evidence still permits rerun need (canRunValidation should be true)', () => {
      const artifacts = [{ storageKind: 'validation_run_json' }];
      const status = 'validation_failed';
      const canRun = evaluateExecuteValidationAction(artifacts, status);
      expect(canRun).toBe(true);
    });

    it('validation passed with final evidence does NOT permit rerun (canRunValidation should be false)', () => {
      const artifacts = [{ storageKind: 'validation_run_json' }];
      const status = 'validation_passed';
      const canRun = evaluateExecuteValidationAction(artifacts, status);
      expect(canRun).toBe(false);
    });

    it('validation running suppresses validation run capability (canRunValidation should be false)', () => {
      const artifacts: any[] = [];
      const status = 'local_validation_running';
      const canRun = evaluateExecuteValidationAction(artifacts, status);
      expect(canRun).toBe(false);
    });
  });

  describe('Repair eligibility gate', () => {
    it('requires coded, repair-eligible issues before offering repair', () => {
      const report = {
        repair_eligible: true,
        errors: [
          { code: 'CANONICAL_PACKET_MISSING_REQUIRED_FIELD', repair_eligible: true },
          { code: 'CANONICAL_PACKET_INVALID_ENUM', repair_eligible: true },
        ],
      };

      const result = evaluateRepairEligibility(report);
      expect(result.canOfferRepair).toBe(true);
      expect(result.reason).toBe('');
    });

    it('blocks repair when an issue is uncoded', () => {
      const report = {
        repair_eligible: true,
        errors: [
          { code: '', repair_eligible: true },
        ],
      };

      const result = evaluateRepairEligibility(report);
      expect(result.canOfferRepair).toBe(false);
      expect(result.reason).toContain('uncoded');
    });

    it('blocks repair when the report is not repair-eligible', () => {
      const report = {
        repair_eligible: false,
        errors: [
          { code: 'CANONICAL_PACKET_UNSAFE_PATH', repair_eligible: false },
        ],
      };

      const result = evaluateRepairEligibility(report);
      expect(result.canOfferRepair).toBe(false);
      expect(result.reason).toContain('not repair-eligible');
    });
  });
});
