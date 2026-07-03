// Feature: run-workbench-refinement, Property 2: Identity field visibility for branch and model
//
// For any `RelayRunDetail`, `deriveRunIdentity` marks a field visible
// (`showBranch` / `showModel`) if and only if the corresponding source value
// (`branch` / `model`) is a non-empty string, and when not visible the field
// value is omitted from the view with no placeholder substituted.
//
// Validates: Requirements 1.4, 1.7, 1.8

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { deriveRunIdentity } from "./runIdentity";
import type { RelayRunDetail } from "./types";

// ------------------------------------------------------------
// Base run factory
// ------------------------------------------------------------
//
// Only `branch` and `model` vary across test cases; every other field is
// held to a fixed, minimal-but-valid `RelayRunDetail` shape so the
// generators can focus purely on the branch/model visibility property.

function makeBaseRun(overrides: Partial<RelayRunDetail>): RelayRunDetail {
  const base: RelayRunDetail = {
    id: "run-123",
    name: "Sample run",
    repo: "Paintersrp/relay",
    branch: "main",
    activeStep: "execute",
    status: "executor_running",
    lifecycleState: "execute",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
    summary: "Sample summary",
    model: "some/model",
    riskLevel: "low",
    validation: { errors: 0, warnings: 0, passed: 0 },
    artifacts: [],
    latestEvents: [],
    statusSeverity: "info",
    state: "Running",
    title: "Sample title",
    packetId: "packet-1",
    executor: "executor",
    executorAdapter: "opencode_go",
    validationSummary: { errors: 0, warnings: 0, passed: 0 },
    approvalGate: { label: "Gate", state: "pending" },
    logPreview: { lines: [], truncated: false },
    stepLabels: {
      intake: "Intake",
      prepare: "Prepare",
      execute: "Execute",
      audit: "Audit",
    },
    validations: [],
    logs: [],
  };

  return { ...base, ...overrides };
}

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------
//
// Varies branch/model between non-empty strings, empty string,
// whitespace-only strings, and undefined/absent.

const nonEmptyStringArb = fc
  .string({ minLength: 1, maxLength: 30 })
  .filter((s) => s.trim().length > 0);

const whitespaceOnlyStringArb = fc
  .array(fc.constantFrom(" ", "\t", "\n"), { minLength: 1, maxLength: 10 })
  .map((chars) => chars.join(""))
  .filter((s) => s.trim().length === 0);

const fieldValueArb: fc.Arbitrary<string | undefined> = fc.oneof(
  nonEmptyStringArb,
  fc.constant(""),
  whitespaceOnlyStringArb,
  fc.constant(undefined),
);

describe("deriveRunIdentity — Property 2: identity field visibility for branch and model", () => {
  it("showBranch/branch and showModel/model are visible iff the trimmed source value is non-empty (Req 1.4, 1.7, 1.8)", () => {
    fc.assert(
      fc.property(fieldValueArb, fieldValueArb, (branch, model) => {
        const run = makeBaseRun({
          branch: branch as unknown as string,
          model: model as unknown as string,
        });

        const view = deriveRunIdentity(run);

        const branchIsNonEmpty = (branch?.trim().length ?? 0) > 0;
        const modelIsNonEmpty = (model?.trim().length ?? 0) > 0;

        // showBranch iff branch (trimmed) is non-empty.
        expect(view.showBranch).toBe(branchIsNonEmpty);
        // branch field is present on the view iff showBranch is true.
        expect("branch" in view).toBe(view.showBranch);
        if (view.showBranch) {
          expect(view.branch).toBe(branch);
        } else {
          // Not visible: branch is undefined, never empty string or a placeholder.
          expect(view.branch).toBeUndefined();
        }

        // showModel iff model (trimmed) is non-empty.
        expect(view.showModel).toBe(modelIsNonEmpty);
        // model field is present on the view iff showModel is true.
        expect("model" in view).toBe(view.showModel);
        if (view.showModel) {
          expect(view.model).toBe(model);
        } else {
          // Not visible: model is undefined, never empty string or a placeholder.
          expect(view.model).toBeUndefined();
        }
      }),
      { numRuns: 100 },
    );
  });
});

// ============================================================
// Feature: run-workbench-refinement, Property 1: Run identity primary text and id
//
// For any `RelayRunDetail`, `deriveRunIdentity` sets `primaryText` to the run
// title when the title is a non-empty string and to the run `id` when the
// title is empty, whitespace-only, or absent, and always exposes the run
// `id` in `runId`.
//
// Validates: Requirements 1.1, 1.2, 1.6
// ============================================================

// Varies title between non-empty strings, empty string, whitespace-only
// strings, and undefined/absent; varies id independently.
const titleArb: fc.Arbitrary<string | undefined> = fc.oneof(
  nonEmptyStringArb,
  fc.constant(""),
  whitespaceOnlyStringArb,
  fc.constant(undefined),
);

const idArb = fc.string({ minLength: 1, maxLength: 30 });

describe("deriveRunIdentity — Property 1: run identity primary text and id", () => {
  it("primaryText is the trimmed title when non-empty, else the run id; runId always equals run.id (Req 1.1, 1.2, 1.6)", () => {
    fc.assert(
      fc.property(titleArb, idArb, (title, id) => {
        const run = makeBaseRun({
          title: title as unknown as string,
          id,
        });

        const view = deriveRunIdentity(run);

        const trimmedTitle = title?.trim() ?? "";
        const titleIsNonEmpty = trimmedTitle.length > 0;

        if (titleIsNonEmpty) {
          expect(view.primaryText).toBe(trimmedTitle);
        } else {
          expect(view.primaryText).toBe(id);
        }

        // runId always equals run.id, regardless of title.
        expect(view.runId).toBe(id);
      }),
      { numRuns: 100 },
    );
  });
});
