import { describe, expect, it } from "vitest";
import {
  formatExecutorPacket,
  deriveLiveExecutorProgress,
  isExecuteLiveStatus,
} from "./execute";

const sampleToolUsePacket = JSON.stringify({
  type: "tool_use",
  timestamp: 1782593090986,
  sessionID: "ses_abc123",
  part: {
    type: "tool",
    tool: "read",
    callID: "call_xyz789",
    state: {
      status: "completed",
      input: {
        filePath: "D:\\Code\\relay\\docs\\generated\\agent-references\\index.json",
        limit: 30,
      },
      output: "docs/generated/agent-references/index.json",
    },
  },
});

describe("isExecuteLiveStatus", () => {
  it("returns true for active execute statuses", () => {
    expect(isExecuteLiveStatus("executor_dispatched")).toBe(true);
    expect(isExecuteLiveStatus("executor_running")).toBe(true);
    expect(isExecuteLiveStatus("local_validation_running")).toBe(true);
  });

  it("returns false for terminal or unrelated statuses", () => {
    expect(isExecuteLiveStatus("executor_done")).toBe(false);
    expect(isExecuteLiveStatus("approved_for_executor")).toBe(false);
    expect(isExecuteLiveStatus(undefined)).toBe(false);
  });
});

describe("formatExecutorPacket", () => {
  it("renders the sample tool_use/read packet as a readable progress row", () => {
    const lines = formatExecutorPacket(sampleToolUsePacket);
    expect(lines.length).toBeGreaterThan(0);
    expect(lines[0]).toMatch(/tool\s+read\s+completed/);
    expect(lines[0]).toContain(
      "docs/generated/agent-references/index.json",
    );
    expect(lines[0]).not.toContain("D:\\\\Code\\\\relay");
    expect(lines[0]).not.toContain('"type": "tool_use"');
  });

  it("falls back to a trimmed text line for invalid JSON", () => {
    const lines = formatExecutorPacket("some raw text that is not json");
    expect(lines.length).toBe(1);
    expect(lines[0]).toContain("some raw text");
  });

  it("returns an empty array for empty input", () => {
    expect(formatExecutorPacket("")).toEqual([]);
  });
});

describe("deriveLiveExecutorProgress", () => {
  it("includes event messages and formatted artifact previews", () => {
    const events = [
      {
        id: "1",
        runId: "42",
        kind: "log" as const,
        message: "Executor started",
        createdAt: "2026-06-27T10:00:00.000Z",
      },
    ];
    const artifacts = [
      {
        id: "2",
        label: "Executor Result",
        path: "/api/runs/42/artifacts/executor_result",
        kind: "executor_result",
        status: "ready",
        filename: "executor_result.json",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    expect(lines.length).toBe(3);
    expect(lines[0]).toContain("Executor started");
    expect(lines[1]).toContain("tool read completed");
    expect(lines[2]).toContain("→");
  });

  it("limits output to the most recent 100 lines", () => {
    const events = Array.from({ length: 120 }, (_, i) => ({
      id: String(i),
      runId: "42",
      kind: "log" as const,
      message: `event ${i}`,
      createdAt: new Date(1e12 + i * 1000).toISOString(),
    }));
    const lines = deriveLiveExecutorProgress(events, []);
    expect(lines.length).toBe(100);
    expect(lines[lines.length - 1]).toContain("event 119");
  });
});
