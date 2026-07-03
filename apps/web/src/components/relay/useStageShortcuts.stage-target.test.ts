// Feature: frontend-shell-redesign
//
// Unit tests for `resolveStageShortcutTarget` — the pure helper backing the
// next/previous Run_Workbench stage keyboard shortcuts.
//
// Validates: Requirements 4.8, 4.9
//   - 4.8: next/previous move to the immediately adjacent stage in pipeline
//     order (Intake -> Compile/Render -> Execute -> Audit).
//   - 4.9: at the first stage (Intake) previous is a no-op; at the last stage
//     (Audit) next is a no-op.

import { describe, expect, it } from "vitest";

import {
  resolveStageShortcutTarget,
  type StageShortcutRoute,
} from "./useStageShortcuts";
import { PIPELINE_STAGE_ROUTES } from "@/features/relay-navigation/pipeline";
import type { RelayRunStep } from "@/features/relay-runs";

describe("resolveStageShortcutTarget — adjacent stage navigation (Req 4.8)", () => {
  it("moves forward through interior stages", () => {
    expect(resolveStageShortcutTarget("intake", "next")).toEqual({
      step: "prepare",
      to: "/runs/$runId/prepare",
    });
    expect(resolveStageShortcutTarget("prepare", "next")).toEqual({
      step: "execute",
      to: "/runs/$runId/execute",
    });
    expect(resolveStageShortcutTarget("execute", "next")).toEqual({
      step: "audit",
      to: "/runs/$runId/audit",
    });
  });

  it("moves backward through interior stages", () => {
    expect(resolveStageShortcutTarget("audit", "previous")).toEqual({
      step: "execute",
      to: "/runs/$runId/execute",
    });
    expect(resolveStageShortcutTarget("execute", "previous")).toEqual({
      step: "prepare",
      to: "/runs/$runId/prepare",
    });
    expect(resolveStageShortcutTarget("prepare", "previous")).toEqual({
      step: "intake",
      to: "/runs/$runId/intake",
    });
  });
});

describe("resolveStageShortcutTarget — clamped boundaries (Req 4.9)", () => {
  it("is a no-op for previous at the first stage (Intake)", () => {
    expect(resolveStageShortcutTarget("intake", "previous")).toBeNull();
  });

  it("is a no-op for next at the last stage (Audit)", () => {
    expect(resolveStageShortcutTarget("audit", "next")).toBeNull();
  });
});

describe("resolveStageShortcutTarget — route mapping stays in sync with pipeline", () => {
  it("resolves each target to its documented PIPELINE_STAGE_ROUTES template", () => {
    const cases: Array<{
      current: RelayRunStep;
      direction: "next" | "previous";
      expected: RelayRunStep;
    }> = [
      { current: "intake", direction: "next", expected: "prepare" },
      { current: "prepare", direction: "next", expected: "execute" },
      { current: "execute", direction: "next", expected: "audit" },
      { current: "audit", direction: "previous", expected: "execute" },
      { current: "execute", direction: "previous", expected: "prepare" },
      { current: "prepare", direction: "previous", expected: "intake" },
    ];

    for (const { current, direction, expected } of cases) {
      const target = resolveStageShortcutTarget(current, direction);
      expect(target).not.toBeNull();
      // The typed shortcut route must equal the pipeline's route template.
      expect(target?.to).toBe(
        PIPELINE_STAGE_ROUTES[expected] as StageShortcutRoute,
      );
    }
  });
});
