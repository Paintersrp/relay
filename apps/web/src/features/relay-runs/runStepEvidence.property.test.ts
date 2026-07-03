// Feature: run-workbench-refinement, Property 8: Step evidence partitions the artifact set
//
// For any Active_Route_Step and any `RelayArtifact[]`, `selectStepEvidence`
// returns disjoint `stepEvidence`/`otherArtifacts` partitions whose union
// equals the input array exactly (every artifact appears in exactly one
// partition, preserving source order), `stepEvidence` contains exactly the
// artifacts that `classifyArtifactStep` maps to `currentStep`, and
// `otherArtifacts` contains exactly everything else.
//
// Validates: Requirements 5.1, 5.4, 5.7, 5.8

import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { classifyArtifactStep, selectStepEvidence } from "./runStepEvidence";
import type { RelayArtifact, RelayRunStep } from "./runWorkbenchViews";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const currentStepArb: fc.Arbitrary<RelayRunStep> = fc.constantFrom(
  "intake",
  "prepare",
  "execute",
  "audit",
);

// Mix of known (step-distinguishing) kinds and arbitrary/unknown strings so
// the generator exercises both the enumerated sets and the `other` fallback.
const knownKindArb = fc.constantFrom(
  // intake
  "handoff",
  "planner_handoff",
  "parsed_frontmatter",
  "run_config",
  "intake_validation_report",
  // prepare
  "prompt",
  // execute
  "result",
  "diff",
  "executor_result",
  "executor_stdout",
  "executor_stderr",
  "command_log",
  "codex_last_message",
  // audit
  "audit",
  "mcp_audit_handback",
  // known-but-not-step-distinguishing (falls to 'other')
  "validation",
  "git_status_text",
);

const kindArb: fc.Arbitrary<string> = fc.oneof(
  knownKindArb,
  fc.string({ maxLength: 15 }),
);

let nextArtifactId = 0;

const artifactArb: fc.Arbitrary<RelayArtifact> = fc.record({
  id: fc.string({ minLength: 1, maxLength: 10 }).map((s) => {
    nextArtifactId += 1;
    return `${s}-${nextArtifactId}`;
  }),
  label: fc.string({ maxLength: 20 }),
  path: fc.string({ maxLength: 20 }),
  kind: kindArb,
  status: fc.string({ maxLength: 10 }),
  filename: fc.string({ maxLength: 15 }),
});

const artifactsArb = fc.array(artifactArb, { maxLength: 15 });

describe("selectStepEvidence — Property 8: partitions the artifact set", () => {
  it("returns disjoint stepEvidence/otherArtifacts partitions whose union equals the input, in source order (Req 5.1, 5.4, 5.7, 5.8)", () => {
    fc.assert(
      fc.property(currentStepArb, artifactsArb, (currentStep, artifacts) => {
        const { stepEvidence, otherArtifacts } = selectStepEvidence(
          currentStep,
          artifacts,
        );

        // 1. Total count preserved.
        expect(stepEvidence.length + otherArtifacts.length).toBe(artifacts.length);

        // 2. Every artifact appears in exactly one partition (no
        //    duplicates, no omissions) — checked via reference identity.
        const combined = [...stepEvidence, ...otherArtifacts];
        for (const artifact of artifacts) {
          const occurrences = combined.filter((a) => a === artifact).length;
          expect(occurrences).toBe(1);
        }
        // No extra artifacts introduced.
        for (const artifact of combined) {
          expect(artifacts.includes(artifact)).toBe(true);
        }

        // 3. stepEvidence contains exactly the artifacts classified to
        //    currentStep, in source order.
        const expectedStepEvidence = artifacts.filter(
          (artifact) => classifyArtifactStep(artifact) === currentStep,
        );
        expect(stepEvidence).toEqual(expectedStepEvidence);

        // 4. otherArtifacts contains exactly everything else, in source
        //    order.
        const expectedOtherArtifacts = artifacts.filter(
          (artifact) => classifyArtifactStep(artifact) !== currentStep,
        );
        expect(otherArtifacts).toEqual(expectedOtherArtifacts);

        // 5. Source order preserved within each partition: each partition,
        //    filtered from the original array by membership, matches the
        //    partition's own order (equivalent to checking indices are
        //    monotonically increasing per partition).
        const stepEvidenceIndices = stepEvidence.map((a) => artifacts.indexOf(a));
        const otherArtifactsIndices = otherArtifacts.map((a) => artifacts.indexOf(a));
        expect(stepEvidenceIndices).toEqual([...stepEvidenceIndices].sort((a, b) => a - b));
        expect(otherArtifactsIndices).toEqual(
          [...otherArtifactsIndices].sort((a, b) => a - b),
        );
      }),
      { numRuns: 100 },
    );
  });
});

// Feature: run-workbench-refinement, Property 9: Artifact classification is closed over an enumerated kind set
//
// For any `RelayArtifact` whose `kind` is a `RelayArtifactKind` string
// (including values outside the explicitly enumerated set used by the
// mapping), `classifyArtifactStep` maps it to a canonical step when the
// `kind` is a member of the fixed, explicitly enumerated set for that step,
// maps it to `other` for every `kind` value outside that enumerated set
// (the exhaustive fallback), and never emits a step value outside the four
// canonical steps plus `other`, nor introduces a new artifact kind.
//
// Validates: Requirements 5.5

const INTAKE_KINDS = [
  "handoff",
  "planner_handoff",
  "parsed_frontmatter",
  "run_config",
  "intake_validation_report",
];
const PREPARE_KINDS = ["prompt"];
const EXECUTE_KINDS = [
  "result",
  "diff",
  "executor_result",
  "executor_stdout",
  "executor_stderr",
  "command_log",
  "codex_last_message",
];
const AUDIT_KINDS = ["audit", "mcp_audit_handback"];

const ALL_ENUMERATED_KINDS = new Set<string>([
  ...INTAKE_KINDS,
  ...PREPARE_KINDS,
  ...EXECUTE_KINDS,
  ...AUDIT_KINDS,
]);

const CLOSED_OUTCOMES = ["intake", "prepare", "execute", "audit", "other"];

function expectedStepFor(kind: string): string {
  if (INTAKE_KINDS.includes(kind)) return "intake";
  if (PREPARE_KINDS.includes(kind)) return "prepare";
  if (EXECUTE_KINDS.includes(kind)) return "execute";
  if (AUDIT_KINDS.includes(kind)) return "audit";
  return "other";
}

// Kind arbitrary covering: every known enumerated kind (across all four
// per-step sets), arbitrary/unrecognized strings, the empty string, and
// other edge-case strings (whitespace, punctuation-heavy, very long).
const anyKindArb: fc.Arbitrary<string> = fc.oneof(
  fc.constantFrom(...Array.from(ALL_ENUMERATED_KINDS)),
  fc.constant(""),
  fc.constant(" "),
  fc.constant("Result"), // case-variant of a known kind, should NOT match
  fc.constant("audit "), // trailing-space variant of a known kind
  fc.string({ maxLength: 30 }),
);

const artifactWithKindArb = (kind: string): fc.Arbitrary<RelayArtifact> =>
  fc.record({
    id: fc.string({ minLength: 1, maxLength: 10 }).map((s) => {
      nextArtifactId += 1;
      return `${s}-${nextArtifactId}`;
    }),
    label: fc.string({ maxLength: 20 }),
    path: fc.string({ maxLength: 20 }),
    kind: fc.constant(kind),
    status: fc.string({ maxLength: 10 }),
    filename: fc.string({ maxLength: 15 }),
  });

const artifactArbAnyKind: fc.Arbitrary<RelayArtifact> = anyKindArb.chain((kind) =>
  artifactWithKindArb(kind),
);

describe("classifyArtifactStep — Property 9: closed over an enumerated kind set", () => {
  it("always returns one of the five closed outcomes, never throws, and maps any kind outside the enumerated per-step sets to 'other' (Req 5.5)", () => {
    fc.assert(
      fc.property(artifactArbAnyKind, (artifact) => {
        let result: string | undefined;
        expect(() => {
          result = classifyArtifactStep(artifact);
        }).not.toThrow();

        // 1. Result is always a member of the closed set.
        expect(CLOSED_OUTCOMES).toContain(result);

        // 2. Known kinds map to their expected per-step outcome; any kind
        //    not present in one of the four enumerated per-step sets maps
        //    to 'other' (the exhaustive fallback).
        expect(result).toBe(expectedStepFor(artifact.kind));
      }),
      { numRuns: 100 },
    );
  });

  it("maps every known kind from each enumerated set to its expected step, and unrecognized kinds to 'other' (Req 5.5)", () => {
    fc.assert(
      fc.property(
        fc.constantFrom(...INTAKE_KINDS),
        fc.constantFrom(...PREPARE_KINDS),
        fc.constantFrom(...EXECUTE_KINDS),
        fc.constantFrom(...AUDIT_KINDS),
        fc.string({ maxLength: 20 }).filter((s) => !ALL_ENUMERATED_KINDS.has(s)),
        (intakeKind, prepareKind, executeKind, auditKind, unrecognizedKind) => {
          expect(classifyArtifactStep(artifactFixture(intakeKind))).toBe("intake");
          expect(classifyArtifactStep(artifactFixture(prepareKind))).toBe("prepare");
          expect(classifyArtifactStep(artifactFixture(executeKind))).toBe("execute");
          expect(classifyArtifactStep(artifactFixture(auditKind))).toBe("audit");
          expect(classifyArtifactStep(artifactFixture(unrecognizedKind))).toBe("other");
        },
      ),
      { numRuns: 100 },
    );
  });
});

function artifactFixture(kind: string): RelayArtifact {
  nextArtifactId += 1;
  return {
    id: `fixture-${nextArtifactId}`,
    label: "label",
    path: "path",
    kind,
    status: "status",
    filename: "filename",
  };
}
