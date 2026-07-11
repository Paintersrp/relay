import { describe, expect, it } from "vitest";

import type {
  WorkflowRunStage,
  WorkflowRunStatus,
} from "@/features/relay-runs";
import {
  adjacentStage,
  derivePipelineStages,
  PIPELINE_STAGE_LABELS,
  PIPELINE_STAGE_ORDER,
  PIPELINE_STAGE_ROUTES,
  resolveWorkflowAvailableThroughStage,
  resolveWorkflowStage,
} from "./pipeline";

const STATUS_CASES: readonly [
  WorkflowRunStatus,
  WorkflowRunStage,
  WorkflowRunStage,
][] = [
  ["created", "specification", "specification"],
  ["setup_ready", "specification", "execute"],
  ["executing", "execute", "execute"],
  ["execution_failed", "execute", "execute"],
  ["cancelled", "execute", "execute"],
  ["validating", "execute", "execute"],
  ["validation_failed", "execute", "execute"],
  ["audit_ready", "audit", "audit"],
  ["needs_revision", "audit", "audit"],
  ["completed", "audit", "audit"],
];

const ATTENTION_CASES: readonly [WorkflowRunStatus, boolean][] = [
  ["created", false],
  ["setup_ready", false],
  ["executing", false],
  ["execution_failed", true],
  ["cancelled", true],
  ["validating", false],
  ["validation_failed", false],
  ["audit_ready", true],
  ["needs_revision", true],
  ["completed", false],
];

interface PresentationCase {
  name: string;
  durableStage: WorkflowRunStage;
  selectedRouteStage: WorkflowRunStage;
  availableThroughStage: WorkflowRunStage;
  expectedStatuses: readonly [
    "completed" | "current" | "pending",
    "completed" | "current" | "pending",
    "completed" | "current" | "pending",
  ];
  expectedNavigable: readonly [boolean, boolean, boolean];
}

const PRESENTATION_CASES: readonly PresentationCase[] = [
  {
    name: "Specification selected at the Specification boundary",
    durableStage: "specification",
    selectedRouteStage: "specification",
    availableThroughStage: "specification",
    expectedStatuses: ["current", "pending", "pending"],
    expectedNavigable: [true, false, false],
  },
  {
    name: "setup-ready Specification selected with Execute available",
    durableStage: "specification",
    selectedRouteStage: "specification",
    availableThroughStage: "execute",
    expectedStatuses: ["current", "pending", "pending"],
    expectedNavigable: [true, true, false],
  },
  {
    name: "setup-ready Execute selected above the durable Specification stage",
    durableStage: "specification",
    selectedRouteStage: "execute",
    availableThroughStage: "execute",
    expectedStatuses: ["completed", "current", "pending"],
    expectedNavigable: [true, true, false],
  },
  {
    name: "Execute selected at the Execute boundary",
    durableStage: "execute",
    selectedRouteStage: "execute",
    availableThroughStage: "execute",
    expectedStatuses: ["completed", "current", "pending"],
    expectedNavigable: [true, true, false],
  },
  {
    name: "Specification selected while Execute remains reachable",
    durableStage: "execute",
    selectedRouteStage: "specification",
    availableThroughStage: "execute",
    expectedStatuses: ["current", "completed", "pending"],
    expectedNavigable: [true, true, false],
  },
  {
    name: "Audit selected at the Audit boundary",
    durableStage: "audit",
    selectedRouteStage: "audit",
    availableThroughStage: "audit",
    expectedStatuses: ["completed", "completed", "current"],
    expectedNavigable: [true, true, true],
  },
  {
    name: "Execute selected while Audit remains reachable",
    durableStage: "audit",
    selectedRouteStage: "execute",
    availableThroughStage: "audit",
    expectedStatuses: ["completed", "current", "completed"],
    expectedNavigable: [true, true, true],
  },
  {
    name: "Specification selected while Audit remains reachable",
    durableStage: "audit",
    selectedRouteStage: "specification",
    availableThroughStage: "audit",
    expectedStatuses: ["current", "completed", "completed"],
    expectedNavigable: [true, true, true],
  },
];

describe("canonical pipeline derivation", () => {
  it("owns the exact canonical stage order, labels, and routes", () => {
    expect(PIPELINE_STAGE_ORDER).toEqual([
      "specification",
      "execute",
      "audit",
    ]);
    expect(PIPELINE_STAGE_LABELS).toEqual({
      specification: "Specification",
      execute: "Execute",
      audit: "Audit",
    });
    expect(PIPELINE_STAGE_ROUTES).toEqual({
      specification: "/runs/$runId/specification",
      execute: "/runs/$runId/execute",
      audit: "/runs/$runId/audit",
    });
  });

  it.each(STATUS_CASES)(
    "maps %s to durable %s and available-through %s",
    (status, durableStage, availableThroughStage) => {
      expect(resolveWorkflowStage(status)).toBe(durableStage);
      expect(resolveWorkflowAvailableThroughStage(status)).toBe(
        availableThroughStage,
      );
    },
  );

  it.each(PRESENTATION_CASES)(
    "derives $name",
    ({
      durableStage,
      selectedRouteStage,
      availableThroughStage,
      expectedStatuses,
      expectedNavigable,
    }) => {
      const stages = derivePipelineStages(
        durableStage,
        selectedRouteStage,
        undefined,
        availableThroughStage,
      );

      expect(stages.map((stage) => stage.step)).toEqual(
        PIPELINE_STAGE_ORDER,
      );
      expect(stages.map((stage) => stage.label)).toEqual([
        "Specification",
        "Execute",
        "Audit",
      ]);
      expect(stages.map((stage) => stage.to)).toEqual([
        "/runs/$runId/specification",
        "/runs/$runId/execute",
        "/runs/$runId/audit",
      ]);
      expect(stages.map((stage) => stage.status)).toEqual(
        expectedStatuses,
      );
      expect(stages.map((stage) => stage.navigable)).toEqual(
        expectedNavigable,
      );
    },
  );

  it.each(ATTENTION_CASES)(
    "classifies %s attention as %s",
    (status, expectedAttention) => {
      const durableStage = resolveWorkflowStage(status);
      const stages = derivePipelineStages(
        durableStage,
        durableStage,
        status,
        resolveWorkflowAvailableThroughStage(status, durableStage),
      );
      const attentionStages = stages.filter(
        (stage) => stage.status === "attention",
      );
      const currentStages = stages.filter(
        (stage) => stage.status === "current",
      );

      expect(attentionStages).toHaveLength(expectedAttention ? 1 : 0);
      expect(currentStages).toHaveLength(expectedAttention ? 0 : 1);
      if (expectedAttention) {
        expect(attentionStages[0]?.step).toBe(durableStage);
      }
    },
  );

  it.each([
    ["specification", "previous", "specification"],
    ["specification", "next", "execute"],
    ["execute", "previous", "specification"],
    ["execute", "next", "audit"],
    ["audit", "previous", "execute"],
    ["audit", "next", "audit"],
  ] as const)(
    "moves %s %s to %s",
    (current, direction, expected) => {
      expect(adjacentStage(current, direction)).toBe(expected);
    },
  );
});
