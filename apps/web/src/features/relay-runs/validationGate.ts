export interface Artifact {
  storageKind: string;
}

export interface ValidationGateResult {
  hasFinalValidationEvidence: boolean;
  validationAllowsAudit: boolean;
  auditBlockedByValidation: boolean;
}

export function evaluateValidationGate(
  artifacts: Artifact[],
  runStatus: string
): ValidationGateResult {
  const hasFinalValidationEvidence = artifacts.some(
    (a) => a.storageKind === 'validation_run_json'
  );

  const validationAllowsAudit =
    hasFinalValidationEvidence &&
    (runStatus === 'validation_passed' || runStatus === 'validation_failed_accepted');

  const auditBlockedByValidation =
    runStatus === 'local_validation_running' || !validationAllowsAudit;

  return {
    hasFinalValidationEvidence,
    validationAllowsAudit,
    auditBlockedByValidation,
  };
}
