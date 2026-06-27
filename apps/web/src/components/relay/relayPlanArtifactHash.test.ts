import { describe, expect, it } from "vitest";
import {
  stableStringify,
  sha256String,
  canonicalPlanJsonForHash,
  computePlanJsonSha256,
} from "./relayPlanArtifactHash";
import type { PlannerPassPlan } from "@/features/relay-plans";

// ─── stableStringify ──────────────────────────────────────────────────────────

describe("stableStringify", () => {
  it("serialises primitives like JSON.stringify", () => {
    expect(stableStringify(42)).toBe("42");
    expect(stableStringify("hello")).toBe('"hello"');
    expect(stableStringify(true)).toBe("true");
    expect(stableStringify(null)).toBe("null");
    expect(stableStringify(undefined)).toBe(undefined);
  });

  it("sorts object keys lexicographically", () => {
    const obj = { z: 1, a: 2, m: 3 };
    expect(stableStringify(obj)).toBe('{"a":2,"m":3,"z":1}');
  });

  it("sorts nested object keys", () => {
    const obj = { outer: { z: 1, a: 2 }, b: 0 };
    expect(stableStringify(obj)).toBe('{"b":0,"outer":{"a":2,"z":1}}');
  });

  it("preserves array element order", () => {
    const arr = [3, 1, 2];
    expect(stableStringify(arr)).toBe("[3,1,2]");
  });

  it("sorts keys in array-element objects while preserving array order", () => {
    const arr = [{ z: 1, a: 2 }, { y: 3, b: 4 }];
    expect(stableStringify(arr)).toBe('[{"a":2,"z":1},{"b":4,"y":3}]');
  });

  it("produces a deterministic result regardless of key insertion order", () => {
    const a = { z: 1, a: 2 };
    const b = { a: 2, z: 1 };
    expect(stableStringify(a)).toBe(stableStringify(b));
  });
});

// ─── sha256String ─────────────────────────────────────────────────────────────

describe("sha256String", () => {
  it("returns a string starting with 'sha256:'", async () => {
    const result = await sha256String("hello");
    expect(result).toMatch(/^sha256:[0-9a-f]+$/);
  });

  it("returns a 64-character hex portion after the prefix", async () => {
    const result = await sha256String("hello");
    const hex = result.slice("sha256:".length);
    expect(hex).toHaveLength(64);
  });

  it("is deterministic for the same input", async () => {
    const a = await sha256String("plan-json-content");
    const b = await sha256String("plan-json-content");
    expect(a).toBe(b);
  });

  it("produces different hashes for different inputs", async () => {
    const a = await sha256String("plan-a");
    const b = await sha256String("plan-b");
    expect(a).not.toBe(b);
  });

  it("uses lowercase hex", async () => {
    const result = await sha256String("test");
    const hex = result.slice("sha256:".length);
    expect(hex).toBe(hex.toLowerCase());
  });
});

// ─── canonicalPlanJsonForHash ─────────────────────────────────────────────────

describe("canonicalPlanJsonForHash", () => {
  const plan: PlannerPassPlan = {
    plan_meta: {
      plan_id: "plan-001",
      schema_version: "1.0.0",
      created_at: "2026-01-01T00:00:00Z",
      title: "Test Plan",
      goal: "Goal",
      repo_target: "owner/repo",
      branch_context: "main",
      status: "active",
    },
    source_intent: { summary: "Test summary" },
    passes: [
      {
        pass_id: "PASS-001",
        sequence: 1,
        name: "First",
        goal: "Pass goal",
        intended_execution_scope: ["src/"],
        non_goals: [],
        dependencies: [],
        status: "planned",
      },
    ],
  };

  it("returns a string", () => {
    expect(typeof canonicalPlanJsonForHash(plan)).toBe("string");
  });

  it("is stable across calls", () => {
    expect(canonicalPlanJsonForHash(plan)).toBe(canonicalPlanJsonForHash(plan));
  });

  it("contains the plan_id", () => {
    expect(canonicalPlanJsonForHash(plan)).toContain("plan-001");
  });
});

// ─── computePlanJsonSha256 ────────────────────────────────────────────────────

describe("computePlanJsonSha256", () => {
  const plan: PlannerPassPlan = {
    plan_meta: {
      plan_id: "plan-hash-test",
      schema_version: "1.0.0",
      created_at: "2026-01-01T00:00:00Z",
      title: "Hash Test",
      goal: "Goal",
      repo_target: "owner/repo",
      branch_context: "main",
      status: "active",
    },
    source_intent: { summary: "Summary" },
    passes: [],
  };

  it("returns a sha256: prefixed string", async () => {
    const result = await computePlanJsonSha256(plan);
    expect(result).toMatch(/^sha256:[0-9a-f]{64}$/);
  });

  it("is stable across calls", async () => {
    const a = await computePlanJsonSha256(plan);
    const b = await computePlanJsonSha256(plan);
    expect(a).toBe(b);
  });

  it("produces a different hash when the plan title changes (current-editor regression)", async () => {
    // This regression test proves that hashing the current editor plan (not stale
    // parsedPlan) produces content-sensitive hashes. If the hash helper were stale
    // it would produce the same hash for both plans.
    const planA: PlannerPassPlan = {
      plan_meta: {
        plan_id: "plan-hash-test",
        schema_version: "1.0.0",
        created_at: "2026-01-01T00:00:00Z",
        title: "Original",
        goal: "Goal",
        repo_target: "owner/repo",
        branch_context: "main",
        status: "active",
      },
      source_intent: { summary: "Summary" },
      passes: [],
    };
    const planB: PlannerPassPlan = {
      ...planA,
      plan_meta: { ...planA.plan_meta, title: "Edited" },
    };
    const hashA = await computePlanJsonSha256(planA);
    const hashB = await computePlanJsonSha256(planB);
    expect(hashA).not.toBe(hashB);
  });
});
