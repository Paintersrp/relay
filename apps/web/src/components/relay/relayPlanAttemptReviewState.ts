/**
 * relayPlanAttemptReviewState.ts
 *
 * Pure helpers for deriving plan attempt review workbench states and validating actions.
 */

export interface ProjectSettings {
  driftReviewMode: string;
  modelTier: string;
}

export interface PlanAttempt {
  planAttemptId: string;
  projectId: string;
  status: string;
  reviewState: string;
  driftReviewMode: string;
  modelTier: string;
}

export interface PlanReviewGate {
  workflowState: string;
  driftReviewMode: string;
  modelTier: string;
  reviewRequired: boolean;
  modelCallAllowed: boolean;
  allowedActions: string[];
  externalReviewInstructions?: string;
}

/**
 * Returns true if the plan attempt creation/revision can proceed.
 * Guards against stale settings (must match exact project ID) and validation states.
 */
export function canCreateAttempt({
  projectId,
  settingsLoadState,
  settingsProjectId,
  hasSettings,
  planPath,
  planSha,
  userRequest,
  state,
  revisionMode,
}: {
  projectId: string;
  settingsLoadState: string;
  settingsProjectId: string;
  hasSettings: boolean;
  planPath: string;
  planSha: string;
  userRequest: string;
  state: string;
  revisionMode: boolean;
}): boolean {
  const trimmedPid = projectId.trim();
  const hasCurrentProjectSettings =
    settingsLoadState === "loaded" &&
    settingsProjectId === trimmedPid &&
    hasSettings;

  return (
    trimmedPid.length > 0 &&
    hasCurrentProjectSettings &&
    planPath.trim().length > 0 &&
    planSha.trim().length > 0 &&
    userRequest.trim().length > 0 &&
    (state === "validated" || revisionMode)
  );
}

/**
 * Derives the policy fields to include when creating/revising an attempt.
 * Never falls back to default manual/standard values if settings are missing or stale.
 */
export function getPolicyFields({
  projectId,
  settingsLoadState,
  settingsProjectId,
  settings,
}: {
  projectId: string;
  settingsLoadState: string;
  settingsProjectId: string;
  settings: ProjectSettings | null | undefined;
}): Partial<ProjectSettings> {
  const trimmedPid = projectId.trim();
  const hasCurrentProjectSettings =
    settingsLoadState === "loaded" &&
    settingsProjectId === trimmedPid &&
    !!settings;

  if (hasCurrentProjectSettings && settings) {
    return {
      driftReviewMode: settings.driftReviewMode,
      modelTier: settings.modelTier,
    };
  }
  return {};
}

/**
 * Returns the project ID to use for revision/mutation.
 * Ensures revision mode cannot be redirected by editing the input field.
 */
export function getProjectIdForMutation({
  revisionMode,
  planAttempt,
  inputProjectId,
}: {
  revisionMode: boolean;
  planAttempt: PlanAttempt | null | undefined;
  inputProjectId: string;
}): string {
  if (revisionMode && planAttempt) {
    return planAttempt.projectId;
  }
  return inputProjectId.trim();
}

/**
 * Returns a human-readable display label for the workbench state.
 */
export function getUIStateLabel(state: string, workflowState?: string): string {
  if (state === "attempt_ready" && workflowState) {
    switch (workflowState) {
      case "review_not_required":
        return "Review not required";
      case "manual_review_available":
        return "Manual review available";
      case "automatic_review_pending_or_failed":
        return "Automatic review required";
      case "external_review_required":
        return "External review required";
      case "approval_ready":
        return "Ready for approval";
      case "drift_acknowledgement_required":
        return "Acknowledgement required";
      case "revision_required":
        return "Revision required";
      case "drift_review_blocked":
        return "Review blocked";
      case "ready_for_submission":
        return "Ready for submission";
      case "submitted":
        return "Submitted";
      case "voided":
        return "Voided";
      case "superseded":
        return "Superseded";
    }
  }

  switch (state) {
    case "draft":
      return "Draft Editor";
    case "validating":
      return "Validating Plan...";
    case "validated":
      return "Plan Validated";
    case "validation_failed":
      return "Validation Failed";
    case "parse_failed":
      return "JSON Parse Failed";
    case "creating_attempt":
      return "Creating Review Attempt...";
    case "review_running":
      return "Running Drift Review...";
    case "revising_attempt":
      return "Revising Attempt...";
    case "voiding_attempt":
      return "Voiding Attempt...";
    case "submitting_approved_attempt":
      return "Submitting Plan...";
    case "submitted":
      return "Submitted";
    case "action_failed":
      return "Action Failed";
    default:
      return state;
  }
}
