// ============================================================
// Run Workbench Refinement — Step-scoped evidence (Requirement 5)
// ============================================================
//
// Pure, presentation-only helpers that classify existing `RelayArtifact`
// entries to a Canonical_Pipeline_Step and partition an artifact set into
// the Active_Route_Step's Step_Evidence vs. everything else.
//
// `classifyArtifactStep` is a fixed, CLOSED mapping defined over an
// explicitly enumerated set of existing `RelayArtifactKind` literal values
// (grounded in current usage — see the per-step kind lists below). Any
// `kind` value outside that enumerated set — including values technically
// permitted by `RelayArtifactKind`'s `| string` escape hatch, and including
// artifact kinds that exist but are not step-distinguishing (e.g. the
// generic `validation` kind, or the git-diff/provenance kinds already
// tracked separately from the four canonical steps in
// `RunEvidenceBrowser.tsx`) — maps to `'other'` as the exhaustive fallback.
//
// This closure is enforced by explicit `Set` membership checks with a
// `default: return 'other'` branch, NOT by relying on `RelayArtifactKind`
// alone: that type includes `| string`, so it cannot by itself guarantee
// that every possible `kind` value is accounted for. No new artifact kind
// is introduced by this module, and no step outside the four canonical
// `RelayRunStep` values plus `'other'` is ever emitted.

import type {
  RelayArtifact,
  RelayRunStep,
  StepEvidenceSplit,
} from "./runWorkbenchViews";

// ------------------------------------------------------------
// Per-step enumerated kind sets (grounded in existing usage)
// ------------------------------------------------------------
//
// intake: the submitted handoff and its parsed/validated intake metadata.
//   - "handoff" — the original submitted handoff (mock-data.ts, types.ts).
//   - "planner_handoff" — the canonical planner handoff artifact kind.
//   - "parsed_frontmatter" — parsed intake frontmatter (RunIntakeReviewPanel.tsx).
//   - "run_config" — intake run configuration (RunIntakeReviewPanel.tsx).
//   - "intake_validation_report" — intake-stage validation report kind
//     (referenced alongside packet/brief validation reports in execute.tsx).
const INTAKE_ARTIFACT_KINDS = new Set<string>([
  "handoff",
  "planner_handoff",
  "parsed_frontmatter",
  "run_config",
  "intake_validation_report",
]);

// prepare: the compiled/rendered brief material produced during Compile/Render.
//   - "prompt" — compiled brief / executor brief artifacts (mock-data.ts:
//     "Compiled Brief", "Executor Brief").
const PREPARE_ARTIFACT_KINDS = new Set<string>(["prompt"]);

// execute: executor result/diff evidence and executor result/log kinds, per
// the design's own example enumeration ("execute -> result/diff and
// executor result/log kinds").
//   - "result" — generic result artifact (isResultArtifact in execute.tsx).
//   - "diff" — generic diff artifact (isDiffArtifact in execute.tsx).
//   - "executor_result" — structured executor result payload.
//   - "executor_stdout" / "executor_stderr" — executor stream logs.
//   - "command_log" — executor command log.
//   - "codex_last_message" — last-message log kind surfaced for the codex adapter.
const EXECUTE_ARTIFACT_KINDS = new Set<string>([
  "result",
  "diff",
  "executor_result",
  "executor_stdout",
  "executor_stderr",
  "command_log",
  "codex_last_message",
]);

// audit: audit-packet and input-summary kinds, per the design's own example
// enumeration ("audit -> audit-packet and input-summary kinds").
//   - "audit" — audit packet / audit input summary artifacts (audit.tsx
//     filters these by kind === "audit" plus filename/path discriminators).
//   - "mcp_audit_handback" — MCP audit handback artifact kind.
const AUDIT_ARTIFACT_KINDS = new Set<string>(["audit", "mcp_audit_handback"]);

// ------------------------------------------------------------
// classifyArtifactStep
// ------------------------------------------------------------

/**
 * Classifies a `RelayArtifact` to a Canonical_Pipeline_Step using a fixed,
 * closed mapping over explicitly enumerated `kind` literals. Any `kind`
 * value that is not a member of one of the four enumerated sets above —
 * including kinds that exist in `RelayArtifactKind` but are not
 * step-distinguishing (e.g. `"validation"`, the `validation_*` kinds, or the
 * `git_*` provenance kinds) and including arbitrary unrecognized strings —
 * maps to `'other'`, the exhaustive fallback.
 *
 * This function never emits a step outside the four canonical
 * `RelayRunStep` values plus `'other'`, and never adds a new artifact kind.
 */
export function classifyArtifactStep(
  artifact: RelayArtifact,
): RelayRunStep | "other" {
  const kind = artifact.kind;

  if (INTAKE_ARTIFACT_KINDS.has(kind)) {
    return "intake";
  }
  if (PREPARE_ARTIFACT_KINDS.has(kind)) {
    return "prepare";
  }
  if (EXECUTE_ARTIFACT_KINDS.has(kind)) {
    return "execute";
  }
  if (AUDIT_ARTIFACT_KINDS.has(kind)) {
    return "audit";
  }

  return "other";
}

// ------------------------------------------------------------
// selectStepEvidence
// ------------------------------------------------------------

/**
 * Partitions `artifacts` into the Active_Route_Step's Step_Evidence
 * (`stepEvidence`) and everything else (`otherArtifacts`), using
 * `classifyArtifactStep` to determine each artifact's step. The two
 * partitions are disjoint and their union is exactly the input array —
 * every artifact appears in exactly one of the two lists, in source order.
 *
 * `currentStep` represents Active_Route_Step (which step sub-route the
 * Operator is viewing), not Canonical_Run_Status.
 */
export function selectStepEvidence(
  currentStep: RelayRunStep,
  artifacts: RelayArtifact[],
): StepEvidenceSplit {
  const stepEvidence: RelayArtifact[] = [];
  const otherArtifacts: RelayArtifact[] = [];

  for (const artifact of artifacts) {
    if (classifyArtifactStep(artifact) === currentStep) {
      stepEvidence.push(artifact);
    } else {
      otherArtifacts.push(artifact);
    }
  }

  return { stepEvidence, otherArtifacts };
}
