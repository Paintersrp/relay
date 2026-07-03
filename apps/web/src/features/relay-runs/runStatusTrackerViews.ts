// ============================================================
// Run Status Tracker Redesign — Frontend-only view types
// ============================================================
//
// These types are pure, presentation-only view shapes produced by the
// derivation helpers introduced by the run-status-tracker-redesign
// feature. They are NOT part of any backend/API contract, are never
// persisted, and are computed exclusively from existing types already
// defined in `types.ts`. See design.md ("Core Types (pseudocode)" and
// "Data Models" sections) for the authoritative shape definitions.

import type * as React from "react";

import type {
  RelayRun,
  RelayRunStep,
  RelayRunEvent,
  RelayExecuteActions,
  RelayAuditActions,
} from "./types";

import type {
  StepActionsView,
  ActionControlView,
  PlanPassLinkView,
  StepEvidenceSplit,
} from "./runWorkbenchViews";

// Re-exported for convenience so consumers of this module don't need to
// separately import the underlying types just to reference them.
export type {
  RelayRun,
  RelayRunStep,
  RelayRunEvent,
  RelayExecuteActions,
  RelayAuditActions,
};

// Reused unchanged from the run-workbench-refinement feature — NOT
// redeclared here. Re-exported so consumers of the tracker views module
// can reference them without a second import.
export type {
  StepActionsView,
  ActionControlView,
  PlanPassLinkView,
  StepEvidenceSplit,
};

// ------------------------------------------------------------
// Shared tone enum
// ------------------------------------------------------------

export type Tone = "neutral" | "info" | "success" | "warning" | "danger";

// ------------------------------------------------------------
// Requirement 2: Current_Status_Block
// ------------------------------------------------------------

export interface CurrentStatusView {
  tone: Tone;
  headline: string;
  detail?: string; // optional supporting sentence; must never contradict headline
  updatedAt: string; // ISO-8601, rendered as relative time
}

// ------------------------------------------------------------
// Requirement 4: Progression_Rail
// ------------------------------------------------------------

export interface ProgressionEntry {
  id: string;
  timestamp: string; // ISO-8601
  label: string; // plain-language, past-tense record
  tone: Tone;
}

// ------------------------------------------------------------
// Requirement 5: Detail_Disclosure
// ------------------------------------------------------------

export interface DetailSection {
  key: string; // e.g. "logs" | "artifacts" | "validation" | "packet-preview"
  label: string; // e.g. "Full logs", "Artifacts", "Validation report"
  render: () => React.ReactNode; // lazy — only invoked once the section is opened
}
