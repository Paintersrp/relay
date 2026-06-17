import { describe, it, expect } from 'vitest';
import { evaluateValidationGate } from './validationGate';

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

  it('validation_failed_accepted with validation_run_json enables Generate Audit', () => {
    const artifacts = [{ storageKind: 'validation_run_json' }];
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
});
