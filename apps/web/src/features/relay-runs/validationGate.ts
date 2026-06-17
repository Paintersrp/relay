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

  const hasAcceptanceArtifact = artifacts.some(
    (a) => a.storageKind === 'validation_failure_acceptance_json'
  );

  const validationAllowsAudit =
    hasFinalValidationEvidence &&
    (runStatus === 'validation_passed' ||
      (runStatus === 'validation_failed_accepted' && hasAcceptanceArtifact));

  const auditBlockedByValidation =
    runStatus === 'local_validation_running' || !validationAllowsAudit;

  return {
    hasFinalValidationEvidence,
    validationAllowsAudit,
    auditBlockedByValidation,
  };
}

export function isAuditCandidateStatus(runStatus: string): boolean {
  return (
    runStatus === 'executor_done' ||
    runStatus === 'executor_blocked' ||
    runStatus === 'validation_passed' ||
    runStatus === 'validation_failed' ||
    runStatus === 'validation_failed_accepted' ||
    runStatus === 'local_validation_running'
  );
}

export function evaluateExecuteValidationAction(
  artifacts: Artifact[],
  runStatus: string
): boolean {
  const { hasFinalValidationEvidence } = evaluateValidationGate(artifacts, runStatus);
  const localValidationIsRunning = runStatus === 'local_validation_running';
  const isPostExecutor =
    runStatus === 'executor_done' ||
    runStatus === 'executor_blocked' ||
    runStatus === 'validation_passed' ||
    runStatus === 'validation_failed' ||
    runStatus === 'validation_failed_accepted';
  return (
    isPostExecutor &&
    !localValidationIsRunning &&
    (!hasFinalValidationEvidence || runStatus === 'validation_failed')
  );
}

