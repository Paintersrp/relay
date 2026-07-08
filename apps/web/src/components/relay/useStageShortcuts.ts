// ============================================================
// Run Workbench — stage keyboard shortcuts (3-stage canonical model)
// ============================================================
//
// Provides keyboard-first movement between the three canonical
// Run_Pipeline_Stages (Specification -> Execute -> Audit) while a
// Run_Workbench route is active (Requirement 4.8). Movement is clamped at
// the boundaries: previous from Specification and next from Audit leave the
// active stage unchanged (Req 4.9).
//
// This module navigates between stages freely regardless of the durable stage.
// Navigability gating (non-navigable stages) is enforced by the `RunStepper`
// and IdentityStrip click handlers — keyboard shortcuts always navigate within
// the three-route set so the operator can review any stage they reached by
// route, including earlier stages.
//
// Shortcut keys: `]` (next stage) and `[` (previous stage).

import * as React from "react";
import { useRouter } from "@tanstack/react-router";
import type { WorkflowRunStage } from "@/features/relay-runs";
import { adjacentStage } from "@/features/relay-navigation/pipeline";

/**
 * TanStack Router-typed stage route templates keyed by canonical pipeline stage.
 */
export type StageShortcutRoute =
  | "/runs/$runId/specification"
  | "/runs/$runId/execute"
  | "/runs/$runId/audit";

const STAGE_SHORTCUT_ROUTES: Record<WorkflowRunStage, StageShortcutRoute> = {
  specification: "/runs/$runId/specification",
  execute: "/runs/$runId/execute",
  audit: "/runs/$runId/audit",
};

export interface StageShortcutTarget {
  step: WorkflowRunStage;
  to: StageShortcutRoute;
}

/**
 * Pure helper: resolve the navigation target for a next/previous stage shortcut
 * from the current stage. Returns `null` when the shortcut is a no-op because
 * the current stage is already at the clamped boundary.
 */
export function resolveStageShortcutTarget(
  currentStep: WorkflowRunStage,
  direction: "next" | "previous",
): StageShortcutTarget | null {
  const target = adjacentStage(currentStep, direction);
  if (target === currentStep) {
    return null;
  }
  return { step: target, to: STAGE_SHORTCUT_ROUTES[target] };
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tagName = target.tagName;
  if (tagName === "INPUT" || tagName === "TEXTAREA" || tagName === "SELECT") {
    return true;
  }
  return target.isContentEditable;
}

function isOverlayOpen(): boolean {
  if (typeof document === "undefined") {
    return false;
  }
  return (
    document.querySelector('[role="dialog"][data-state="open"]') !== null ||
    document.querySelector('[aria-modal="true"]') !== null
  );
}

/**
 * Wire the next/previous stage keyboard shortcuts for an active Run_Workbench
 * route. Invoked by route components with the active canonical stage and run id.
 */
export function useStageShortcuts(
  currentStep: WorkflowRunStage | undefined,
  runId: string | undefined,
): void {
  const router = useRouter();

  React.useEffect(() => {
    if (!currentStep || !runId) {
      return;
    }
    if (typeof window === "undefined") {
      return;
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented) {
        return;
      }
      if (event.ctrlKey || event.metaKey || event.altKey) {
        return;
      }

      let direction: "next" | "previous";
      if (event.key === "]") {
        direction = "next";
      } else if (event.key === "[") {
        direction = "previous";
      } else {
        return;
      }

      if (isEditableTarget(event.target)) {
        return;
      }
      if (isOverlayOpen()) {
        return;
      }

      const target = resolveStageShortcutTarget(currentStep!, direction);
      if (!target) {
        return;
      }

      event.preventDefault();
      void router.navigate({ to: target.to, params: { runId: runId! } });
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [currentStep, runId, router]);
}
