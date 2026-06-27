import { describe, expect, it } from "vitest";
import type { RelayRunEvent } from "@/features/relay-runs";
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
  it("includes event messages and does not include raw artifact previews", () => {
    const events = [
      {
        id: "1",
        runId: "42",
        kind: "log" as const,
        message: "Executor started",
        createdAt: "2026-06-27T10:00:00.000Z",
      },
      {
        id: "2",
        runId: "42",
        kind: "log" as const,
        message: "Read file executor.go",
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];
    const artifacts = [
      {
        id: "a1",
        label: "Executor Stdout",
        path: "/api/runs/42/artifacts/executor_stdout",
        kind: "executor_stdout",
        status: "ready",
        filename: "executor_stdout.txt",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
      {
        id: "a2",
        label: "Command Log",
        path: "/api/runs/42/artifacts/command_log",
        kind: "command_log",
        status: "ready",
        filename: "command_log.txt",
        preview: "Command: opencode run...",
        createdAt: "2026-06-27T10:00:02.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    expect(lines.length).toBe(2);
    expect(lines[0]).toContain("Executor started");
    expect(lines[1]).toContain("Read file executor.go");
  });

  it("does not render raw JSON artifact previews in live progress", () => {
    const events: RelayRunEvent[] = [];
    const artifacts = [
      {
        id: "a1",
        label: "Executor Stdout",
        path: "/api/runs/42/artifacts/executor_stdout",
        kind: "executor_stdout",
        status: "ready",
        filename: "executor_stdout.txt",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    expect(lines.length).toBe(0);
  });

  it("does not contain raw JSON or protocol fields in any line", () => {
    const events = [
      {
        id: "1",
        runId: "42",
        kind: "log" as const,
        message: "Executor dispatched: opencode ...",
        createdAt: "2026-06-27T10:00:00.000Z",
      },
    ];
    const artifacts = [
      {
        id: "a1",
        label: "Executor Stdout",
        path: "/api/runs/42/artifacts/executor_stdout",
        kind: "executor_stdout",
        status: "ready",
        filename: "executor_stdout.txt",
        preview: sampleToolUsePacket,
        createdAt: "2026-06-27T10:00:01.000Z",
      },
    ];

    const lines = deriveLiveExecutorProgress(events, artifacts);
    for (const line of lines) {
      expect(line).not.toContain("sessionID");
      expect(line).not.toContain('"timestamp"');
      expect(line).not.toContain('"type": "tool_use"');
      expect(line).not.toContain('"part"');
    }
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
