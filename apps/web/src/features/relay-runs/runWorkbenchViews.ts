// ============================================================
// Run Workbench Refinement — Frontend-only view types
// ============================================================
//
// These types are pure, presentation-only view shapes produced by the
// derivation helpers introduced by the run-workbench-refinement feature.
// They are NOT part of any backend/API contract, are never persisted, and
// are computed exclusively from existing types already defined in
// `types.ts`. See design.md ("Data Models" and per-requirement sections)
// for the authoritative shape definitions.

import type {
  RelayRun,
  RelayRunDetail,
  RelayRunStep,
  RelayExecuteActions,
  RelayAuditActions,
  RelayArtifact,
  RelayRunPlanContext,
} from "./types";

// Re-exported for convenience so consumers of this module don't need to
// separately import the underlying types just to reference them.
export type {
  RelayRun,
  RelayRunDetail,
  RelayRunStep,
  RelayExecuteActions,
  RelayAuditActions,
  RelayArtifact,
  RelayRunPlanContext,
};

// ------------------------------------------------------------
// Requirement 1: Run identity
// ------------------------------------------------------------

export interface RunIdentityView {
  primaryText: string; // title || id
  runId: string;
  repo: string;
  branch?: string; // present only when non-empty
  model?: string; // present only when non-empty
  showBranch: boolean;
  showModel: boolean;
  // Status is intentionally NOT part of this view. StatusBadge is bound
  // directly to RelayRunDetail.status (Canonical_Run_Status); statusSeverity
  // / state are read directly by StatusBadge only for display-only styling,
  // never through this view.
}

// ------------------------------------------------------------
// Requirement 3: Attention surfacing
// ------------------------------------------------------------

export type AttentionCategory =
  | "blocker"
  | "blocking state"
  | "revision requirement"
  | "warning";

export interface AttentionItem {
  category: AttentionCategory;
  label: string; // observable category label
  message: string; // source text
}

export interface StepAttentionInput {
  currentStep: RelayRunStep; // represents Active_Route_Step, not Canonical_Run_Status
  blockers?: string[];
  warnings?: string[];
  revisionRequirements?: string[]; // audit only
  visualStateIsBlockedOrFailed: boolean;
  blockingStateCopy?: string; // Visual_State_Module state-card copy when blocked/failed
}

// ------------------------------------------------------------
// Requirement 4: Next safe action
// ------------------------------------------------------------

export interface ActionControlView {
  id: string; // stable action key, e.g. 'start', 'generateAudit'
  label: string;
  enabled: boolean; // mirrors the can* flag exactly
  unavailableReason?: string; // shown only when disabled and reason is non-empty
  isPrimary: boolean; // exactly one true when a Next_Safe_Action exists
  // Deliberately no `invoke`/callable field: this is pure view data only.
  // Mapping an action id to its concrete api.ts request function, and
  // triggering it in response to Operator interaction, is owned by the
  // consuming route/component (introduced separately in task 5.6), not by
  // this view type or the derivation helpers that produce it.
}

export interface StepActionsView {
  controls: ActionControlView[];
  nextSafeActionId?: string;
}

// ------------------------------------------------------------
// Requirement 5: Step-scoped evidence
// ------------------------------------------------------------

export interface StepEvidenceSplit {
  stepEvidence: RelayArtifact[]; // artifacts classified to currentStep (Active_Route_Step)
  otherArtifacts: RelayArtifact[]; // everything else (for disclosure)
}

// ------------------------------------------------------------
// Requirement 6: Jump to Plan/Pass
// ------------------------------------------------------------

export interface PlanPassLinkView {
  present: boolean; // false when no planId
  to?: "/plans/$planId" | "/plans/$planId/passes/$passId";
  params?: { planId: string } | { planId: string; passId: string };
  displayLabel: string; // truncated to 120 chars + ellipsis when longer
  accessibleName: string; // untruncated
}
