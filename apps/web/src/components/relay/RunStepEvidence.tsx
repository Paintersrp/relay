import * as React from "react";

import type { RelayRunStep } from "@/features/relay-runs/types";
import type { RelayLogPreview, RelayRun } from "@/features/relay-runs";
import type { StepEvidenceSplit } from "@/features/relay-runs/runWorkbenchViews";
import { ValidationPanel } from "./ValidationPanel";
import { LogPreviewPanel } from "./LogPreviewPanel";
import { ArtifactPreviewCard } from "./ArtifactPreviewCard";
import { ProgressiveDisclosure } from "./ProgressiveDisclosure";
import { RelayInlineState } from "./RelayStateSurface";
import { RunStageContentSection } from "./RunStagePrimitives";

// ============================================================
// Run Workbench Refinement — Step-scoped evidence (Requirement 5)
// ============================================================
//
// Thin presentational component: renders an already-computed
// `StepEvidenceSplit` (see `selectStepEvidence`) for the Active_Route_Step.
// This component does not classify or partition artifacts itself — that is
// the consuming route's responsibility (via `selectStepEvidence`).
//
// Default view (always visible, never gated behind disclosure):
//   - the Active_Route_Step's validation evidence via `ValidationPanel`
//   - the Active_Route_Step's log output via `LogPreviewPanel`
//   - the Active_Route_Step's own artifacts (`evidence.stepEvidence`)
// Disclosed view (behind a `ProgressiveDisclosure` affordance, hidden
// entirely when empty):
//   - every other artifact (`evidence.otherArtifacts`, including anything
//     classified as `other`), rendered via `ArtifactPreviewCard`

export interface RunStepEvidenceProps {
  runId: string;
  /** The Active_Route_Step this evidence view is scoped to. */
  currentStep: RelayRunStep;
  /** Pre-partitioned Step_Evidence vs. other-step artifacts (see `selectStepEvidence`). */
  evidence: StepEvidenceSplit;
  /** The Active_Route_Step's validation summary, when available. */
  validationSummary?: RelayRun["validationSummary"];
  /** The Active_Route_Step's log preview, when available. */
  logPreview?: RelayLogPreview;
  title?: React.ReactNode;
  eyebrow?: React.ReactNode;
  className?: string;
}

export function RunStepEvidence({
  runId,
  currentStep,
  evidence,
  validationSummary,
  logPreview,
  title = "Evidence",
  eyebrow = "Evidence",
  className,
}: RunStepEvidenceProps) {
  const hasValidation = validationSummary !== undefined;
  const hasLogs = logPreview !== undefined;
  const hasStepArtifacts = evidence.stepEvidence.length > 0;
  const hasAnyEvidence = hasValidation || hasLogs || hasStepArtifacts;
  const hasOtherArtifacts = evidence.otherArtifacts.length > 0;

  return (
    <RunStageContentSection eyebrow={eyebrow} title={title} className={className}>
      <div className="flex flex-col gap-3">
        {hasAnyEvidence ? (
          <>
            {hasValidation ? (
              <ValidationPanel summary={validationSummary} />
            ) : null}
            {hasLogs ? <LogPreviewPanel logPreview={logPreview} /> : null}
            {hasStepArtifacts ? (
              <div className="grid min-w-0 gap-2 sm:grid-cols-2">
                {evidence.stepEvidence.map((artifact) => (
                  <ArtifactPreviewCard
                    key={artifact.id}
                    runId={runId}
                    artifact={artifact}
                  />
                ))}
              </div>
            ) : null}
          </>
        ) : (
          <RelayInlineState
            tone="empty"
            title="No evidence yet"
            description="Relay has not captured validation, logs, or artifacts for this step yet."
            density="compact"
          />
        )}

        {hasOtherArtifacts ? (
          <ProgressiveDisclosure
            label={(expanded) =>
              expanded
                ? `Hide other step artifacts (${evidence.otherArtifacts.length})`
                : `Show other step artifacts (${evidence.otherArtifacts.length})`
            }
            resetKey={currentStep}
          >
            <div className="grid min-w-0 gap-2 sm:grid-cols-2">
              {evidence.otherArtifacts.map((artifact) => (
                <ArtifactPreviewCard
                  key={artifact.id}
                  runId={runId}
                  artifact={artifact}
                />
              ))}
            </div>
          </ProgressiveDisclosure>
        ) : null}
      </div>
    </RunStageContentSection>
  );
}
