export interface Artifact {
  storageKind: string;
}

export interface PacketValidationIssueLike {
  code?: string;
  repair_eligible?: boolean;
  RepairEligible?: boolean;
}

export interface PacketValidationReportLike {
  repair_eligible?: boolean;
  RepairEligible?: boolean;
  errors?: PacketValidationIssueLike[];
}

export interface ValidationGateResult {
  hasFinalValidationEvidence: boolean;
  validationAllowsAudit: boolean;
  auditBlockedByValidation: boolean;
}

export interface RepairEligibilityResult {
  canOfferRepair: boolean;
  reason: string;
}

const ELIGIBLE_CODES = new Set([
  'CANONICAL_PACKET_JSON_SYNTAX',
  'CANONICAL_PACKET_MISSING_REQUIRED_FIELD',
  'CANONICAL_PACKET_INVALID_ENUM',
  'CANONICAL_PACKET_EXTRA_PROPERTY',
  'CANONICAL_PACKET_INVALID_TYPE',
  'CANONICAL_PACKET_STRING_PATTERN_MISMATCH',
  'CANONICAL_PACKET_MISSING_IMPLEMENTATION_STEPS',
  'CANONICAL_PACKET_MISSING_CODE_REQUIREMENTS',
]);

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

export function evaluateRepairEligibility(
  report?: PacketValidationReportLike | null
): RepairEligibilityResult {
  const errors = report?.errors ?? [];
  const reportedEligible = report?.repair_eligible ?? report?.RepairEligible ?? false;

  if (!Array.isArray(errors) || errors.length === 0) {
    return {
      canOfferRepair: false,
      reason: 'Validation report has no repairable errors.',
    };
  }

  for (const issue of errors) {
    const code = issue?.code?.trim?.() ?? '';
    const repairEligible = issue?.repair_eligible ?? issue?.RepairEligible;
    if (!code) {
      return {
        canOfferRepair: false,
        reason: 'Validation report contains an uncoded issue.',
      };
    }
    if (repairEligible !== true) {
      return {
        canOfferRepair: false,
        reason: `Validation issue ${code} is not repair-eligible.`,
      };
    }
    if (!ELIGIBLE_CODES.has(code)) {
      return {
        canOfferRepair: false,
        reason: `Validation issue ${code} is not in the repair allowlist.`,
      };
    }
  }

  if (!reportedEligible) {
    return {
      canOfferRepair: false,
      reason: 'Validation report is not repair-eligible.',
    };
  }

  return {
    canOfferRepair: true,
    reason: '',
  };
}
