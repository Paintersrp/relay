import { describe, expect, it } from "vitest";

import type { RelayArtifact, RelayRun } from "@/features/relay-runs";
import { buildRunSourceVisibilitySummary } from "./RunSourceContextPanel";

function buildRun(partial: Partial<RelayRun>): RelayRun {
  return {
    id: "42",
    name: "Run 42",
    repo: "relay",
    branch: "main",
    activeStep: "intake",
    status: "intake_needs_review",
    lifecycleState: "intake",
    createdAt: "2026-06-23T00:00:00Z",
    updatedAt: "2026-06-23T00:00:00Z",
    summary: "",
    model: "gpt-4o",
    riskLevel: "low",
    validation: { errors: 0, warnings: 0, passed: 0, issues: [] },
    artifacts: [],
    latestEvents: [],
    statusSeverity: "neutral",
    state: "Draft",
    title: "Run 42",
    packetId: "",
    executor: "opencode_go",
    executorAdapter: "opencode_go",
    validationSummary: { errors: 0, warnings: 0, passed: 0, issues: [] },
    approvalGate: { label: "Intake Review", state: "pending" },
    logPreview: { lines: [], truncated: false },
    stepLabels: {
      intake: "Intake / Configure",
      prepare: "Compile / Render",
      execute: "Execute",
      audit: "Audit / Close",
    },
    ...partial,
  };
}

function buildArtifact(partial: Partial<RelayArtifact>): RelayArtifact {
  return {
    id: "1",
    label: "Artifact",
    path: "/api/runs/42/artifacts/1",
    kind: "handoff",
    status: "ready",
    filename: "artifact.json",
    ...partial,
  };
}

describe("buildRunSourceVisibilitySummary", () => {
  it("derives provenance ids and safe artifacts", () => {
    const run = buildRun({
      planContext: {
        planId: "plan-1",
        passId: "PASS-009",
      },
      provenance: {
        plannerHandoffSha256: "abc123",
        sourceArtifactPath: "handoffs/planner/pass-009.md",
        contextPacketId: "ctxpkt-123",
        sourceSnapshotId: "srcsnap-456",
      },
    });
    const artifacts = [
      buildArtifact({
        label: "Provenance",
        storageKind: "planner_handoff_provenance_json",
      }),
      buildArtifact({
        id: "2",
        label: "Context Packet",
        storageKind: "context_packet_json",
      }),
      buildArtifact({
        id: "3",
        label: "Coverage",
        storageKind: "context_coverage_report_json",
      }),
    ];

    const summary = buildRunSourceVisibilitySummary(run, artifacts);

    expect(summary).toMatchObject({
      plannerHandoffSha256: "abc123",
      sourceArtifactPath: "handoffs/planner/pass-009.md",
      contextPacketId: "ctxpkt-123",
      sourceSnapshotId: "srcsnap-456",
    });
    expect(summary.provenanceArtifact?.storageKind).toBe(
      "planner_handoff_provenance_json",
    );
    expect(summary.contextPacketArtifact?.storageKind).toBe("context_packet_json");
    expect(summary.coverageReportArtifact?.storageKind).toBe(
      "context_coverage_report_json",
    );
  });

  it("reports an empty-state warning when no source visibility exists", () => {
    const summary = buildRunSourceVisibilitySummary(buildRun({}), []);

    expect(summary.warnings).toContain(
      "No submission provenance or source-context metadata is stored for this run.",
    );
  });
});
