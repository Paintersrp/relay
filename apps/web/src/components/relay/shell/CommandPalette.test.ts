import { describe, expect, it } from "vitest";

import { commandEntryKey, resolveCommandNavigation } from "./CommandPalette";
import type { CommandEntry } from "@/features/relay-navigation/types";

const domainEntry: CommandEntry = {
  kind: "nav-domain",
  id: "runs",
  label: "Runs",
  to: "/runs",
};

const recentEntry: CommandEntry = {
  kind: "nav-recent",
  entity: "run",
  id: "run-1",
  label: "Run One",
  to: "/runs/$runId",
  params: { runId: "run-1" },
};

const actionEntry: CommandEntry = {
  kind: "action",
  id: "new-run",
  label: "New Run",
  run: () => {},
};

describe("resolveCommandNavigation", () => {
  it("returns the static route for a primary-domain entry", () => {
    expect(resolveCommandNavigation(domainEntry)).toEqual({ to: "/runs" });
  });

  it("returns the parameterized route for a recent-entity entry", () => {
    expect(resolveCommandNavigation(recentEntry)).toEqual({
      to: "/runs/$runId",
      params: { runId: "run-1" },
    });
  });

  it("returns null for an action entry (run callback is invoked instead)", () => {
    expect(resolveCommandNavigation(actionEntry)).toBeNull();
  });
});

describe("commandEntryKey", () => {
  it("produces distinct, collision-free keys per entry kind", () => {
    const keys = new Set([
      commandEntryKey(domainEntry),
      commandEntryKey(recentEntry),
      commandEntryKey(actionEntry),
    ]);
    expect(keys.size).toBe(3);
    expect(commandEntryKey(domainEntry)).toBe("nav-domain:runs");
    expect(commandEntryKey(recentEntry)).toBe("nav-recent:run:run-1");
    expect(commandEntryKey(actionEntry)).toBe("action:new-run");
  });
});
