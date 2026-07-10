// ============================================================
// Relay Navigation — Shared shell view-model types
// ============================================================
//
// Presentation-only view models for the redesigned application shell
// (Activity_Rail, Top_Bar, Breadcrumb_Trail, Scope_Switcher, Command_Palette,
// Global_Search, Home_Overview, and the extended Run_Pipeline stage rail).
//
// These types describe frontend view models only. They do NOT redefine the
// canonical status contract: the canonical `RelayRunStep` and `RelayRunStatus`
// types are reused from the runs feature.

import type { WorkflowRunStage } from "@/features/relay-runs";

// ------------------------------------------------------------
// Primary domains
// ------------------------------------------------------------

export type PrimaryDomain = "projects" | "plans" | "runs";

// ------------------------------------------------------------
// Wayfinding
// ------------------------------------------------------------

export interface ResolvedHierarchy {
  project?: { id: string; label: string };
  plan?: { id: string; label: string };
  pass?: { id: string; label: string; sequence?: number };
  run?: { id: string; label: string };
}

export interface BreadcrumbSegment {
  level: "project" | "plan" | "pass" | "run";
  label: string;
  to?: string; // present => navigable ancestor; absent => current leaf
  params?: Record<string, string>;
}

// ------------------------------------------------------------
// Scope switching
// ------------------------------------------------------------

export interface ScopeOption {
  kind: "project" | "plan";
  id: string;
  label: string;
  to: string;
  params: Record<string, string>;
}

// ------------------------------------------------------------
// Search + command
// ------------------------------------------------------------

export interface SearchableEntity {
  type: "project" | "plan" | "pass" | "run";
  id: string; // identifier
  name: string; // display name (name/title/label)
  to: string;
  params: Record<string, string>;
}

export interface SearchResult {
  entity: SearchableEntity;
}

export type CommandEntry =
  | { kind: "nav-domain"; id: PrimaryDomain; label: string; to: string }
  | {
      kind: "nav-recent";
      entity: "run" | "plan" | "project";
      id: string;
      label: string;
      to: string;
      params: Record<string, string>;
    }
  | { kind: "action"; id: "new-run" | "new-plan"; label: string; run: () => void };
// The "action" entry set is frozen/closed to exactly "new-run" and "new-plan"
// (Requirement 4.3). No lifecycle-mutating run action is or may be added as a
// CommandEntry (Requirement 4.10).

// ------------------------------------------------------------
// Pipeline
// ------------------------------------------------------------

export type PipelineStageStatus = "completed" | "current" | "pending" | "attention";

export interface PipelineStageView {
  step: WorkflowRunStage; // "specification" | "execute" | "audit"
  label: string; // "Specification" | "Execute" | "Audit"
  status: PipelineStageStatus;
  to: string; // stage route
  navigable: boolean;
}

// ------------------------------------------------------------
// Attention / recents
// ------------------------------------------------------------

export interface RecentActivityItem {
  type: "run" | "plan" | "project";
  id: string;
  label: string;
  updatedAt: string; // ISO-8601
  to: string;
  params: Record<string, string>;
}

// ------------------------------------------------------------
// Home overview section state
// ------------------------------------------------------------

export interface HomeOverviewSectionState<T> {
  status: "loading" | "ready" | "empty" | "error";
  items: T[];
  totalCount?: number; // for attention overflow indicator
}
