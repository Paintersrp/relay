import { describe, it, expect } from 'vitest';
import { evaluateValidationGate, evaluateExecuteValidationAction } from './validationGate';

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
});

