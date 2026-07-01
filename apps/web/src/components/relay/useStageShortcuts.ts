// ============================================================
// Run_Workbench — next/previous stage keyboard shortcuts
// ============================================================
//
// Provides keyboard-first movement between the four Run_Pipeline_Stages
// (Intake -> Compile/Render -> Execute -> Audit) while a Run_Workbench route is
// active (Requirement 4.8). Movement is clamped at the boundaries: previous
// from Intake and next from Audit leave the active stage unchanged (Req 4.9).
//
// Design boundary: this is navigation-only. It resolves the adjacent stage via
// the pure `adjacentStage` helper and navigates to that stage's existing run
// route. It never changes Go_Daemon state or action-gating semantics.
//
// Shortcut keys: `]` (next stage) and `[` (previous stage). These are chosen to
// avoid colliding with the Ctrl/⌘K command palette and with browser/OS
// shortcuts (Alt+Arrow is browser back/forward on some platforms, so it is
// intentionally not used). The handler only fires for the bare bracket keys
// with no command modifiers, and it ignores events while focus is in an
// input/textarea/select/contenteditable element or while a modal/overlay is
// open, so it never interferes with typing.

import * as React from "react";
import { useRouter } from "@tanstack/react-router";
import type { RelayRunStep } from "@/features/relay-runs";
import { adjacentStage } from "@/features/relay-navigation/pipeline";

/**
 * TanStack Router-typed stage route templates keyed by pipeline stage. These
 * mirror `PIPELINE_STAGE_ROUTES` from `pipeline.ts` (which exposes them as
 * opaque strings) so navigation stays type-checked. The
 * `stageRouteMatchesPipeline` guard test asserts the two stay in sync.
 */
export type StageShortcutRoute =
  | "/runs/$runId/intake"
  | "/runs/$runId/prepare"
  | "/runs/$runId/execute"
  | "/runs/$runId/audit";

const STAGE_SHORTCUT_ROUTES: Record<RelayRunStep, StageShortcutRoute> = {
  intake: "/runs/$runId/intake",
  prepare: "/runs/$runId/prepare",
  execute: "/runs/$runId/execute",
  audit: "/runs/$runId/audit",
};

export interface StageShortcutTarget {
  step: RelayRunStep;
  to: StageShortcutRoute;
}

/**
 * Pure helper: resolve the navigation target for a next/previous stage shortcut
 * from the current stage. Returns `null` when the shortcut is a no-op because
 * the current stage is already at the clamped boundary — previous from Intake
 * or next from Audit (Requirement 4.9). Otherwise returns the adjacent stage
 * and its run-scoped route template.
 */
export function resolveStageShortcutTarget(
  currentStep: RelayRunStep,
  direction: "next" | "previous",
): StageShortcutTarget | null {
  const target = adjacentStage(currentStep, direction);
  // At a boundary `adjacentStage` clamps and returns the current stage; treat
  // that as a no-op so the active stage is left unchanged (Req 4.9).
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
  // Radix-backed dialogs/overlays expose data-state="open"; aria-modal covers
  // any other modal surface. If one is open we defer to it and do not navigate.
  return (
    document.querySelector('[role="dialog"][data-state="open"]') !== null ||
    document.querySelector('[aria-modal="true"]') !== null
  );
}

/**
 * Wire the next/previous stage keyboard shortcuts for an active Run_Workbench
 * route. Invoked by `RunWorkbenchLayout` with the active stage and run id.
 *
 * The listener is registered on `window` while a workbench route is active and
 * removed on unmount / when the stage or run changes, so the shortcuts only
 * fire on workbench routes (Req 4.8).
 */
export function useStageShortcuts(
  currentStep: RelayRunStep | undefined,
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
      // Ignore bracket keys pressed with command modifiers so we never shadow
      // the Ctrl/⌘K palette or browser/OS shortcuts.
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

      // Never intercept typing or interactions inside an open overlay.
      if (isEditableTarget(event.target)) {
        return;
      }
      if (isOverlayOpen()) {
        return;
      }

      const target = resolveStageShortcutTarget(currentStep!, direction);
      // Boundary no-op: leave the active stage unchanged (Req 4.9).
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
