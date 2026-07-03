import * as React from "react";

import type {
  DetailSection,
  PlanPassLinkView,
  RelayRunStep,
} from "@/features/relay-runs/runStatusTrackerViews";
import { cn } from "@/lib/utils";

import { ProgressiveDisclosure } from "./ProgressiveDisclosure";
import { PlanPassJumpLink } from "./PlanPassJumpLink";

// ============================================================
// Run Status Tracker Redesign — DetailDisclosure (Requirement 5)
// ============================================================
//
// The single, explicit gateway to everything that used to render inline:
// full logs, artifacts, diffs, packet/brief/commit-message previews,
// validation report dumps. Presented as one collapsed "Show details"
// affordance per run (Requirement 5.1). Once opened, `sections` render as a
// flat list/accordion — never a tab strip (Requirement 5.2) — and each
// section's `render()` is invoked lazily, only once that specific section
// is individually opened (Requirement 5.3). `PlanPassJumpLink` (existing,
// unchanged) renders inside this component as a navigation link only,
// never inlined plan/pass content (Requirement 5.4, 5.5). The outer
// open/closed state resets to collapsed whenever the Active_Route_Step
// changes (Requirement 5.11), via `currentStep` as the reset key — the same
// pattern `ProgressionRail` uses `runId` for and `RunStepEvidence` uses
// `currentStep` for. A failing `render()` (whether it throws synchronously
// or the returned tree throws during React's render) is caught and shown
// as an inline failure message scoped to that section only, leaving
// sibling sections and the regions above unaffected (Requirement 5.10).

export interface DetailDisclosureProps {
  sections: DetailSection[];
  planPassLinkView?: PlanPassLinkView;
  /**
   * Active_Route_Step — the step sub-route the Operator is currently
   * viewing. Used solely to reset the outer "Show details" affordance back
   * to collapsed when the Operator navigates to a different step
   * (Requirement 5.11). Never used to re-derive `sections`.
   */
  currentStep: RelayRunStep;
  className?: string;
}

interface SectionErrorBoundaryState {
  hasError: boolean;
}

/**
 * Per-section error boundary. Catches errors thrown while React renders the
 * tree returned by a `DetailSection.render()` call (errors thrown
 * synchronously by `render()` itself are caught separately, in
 * `DetailSectionContent`). Scoped to a single section so a failing section
 * never affects its siblings or the content above `DetailDisclosure`.
 */
class SectionErrorBoundary extends React.Component<
  { sectionLabel: string; children: React.ReactNode },
  SectionErrorBoundaryState
> {
  state: SectionErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(): SectionErrorBoundaryState {
    return { hasError: true };
  }

  render(): React.ReactNode {
    if (this.state.hasError) {
      return <DetailSectionFailure label={this.props.sectionLabel} />;
    }
    return this.props.children;
  }
}

function DetailSectionFailure({ label }: { label: string }) {
  return (
    <p
      role="alert"
      className="text-xs text-[var(--destructive)]"
      data-testid="detail-section-error"
    >
      Couldn&apos;t load {label}.
    </p>
  );
}

/**
 * Invokes `section.render()` lazily (only mounted once the section is
 * opened, via the parent `ProgressiveDisclosure`). Wrapped in a try/catch
 * so a synchronous throw from `render()` itself is caught here; anything
 * thrown later while React renders the returned tree is caught by the
 * surrounding `SectionErrorBoundary`.
 */
function DetailSectionContent({ section }: { section: DetailSection }) {
  let content: React.ReactNode;
  try {
    content = section.render();
  } catch {
    return <DetailSectionFailure label={section.label} />;
  }
  return <>{content}</>;
}

function DetailDisclosureSection({ section }: { section: DetailSection }) {
  return (
    <div
      role="listitem"
      className="border-b border-border/60 py-2 last:border-b-0"
      data-testid="detail-section"
      data-section-key={section.key}
    >
      <ProgressiveDisclosure
        label={section.label}
        triggerClassName="text-xs"
      >
        <SectionErrorBoundary sectionLabel={section.label}>
          <DetailSectionContent section={section} />
        </SectionErrorBoundary>
      </ProgressiveDisclosure>
    </div>
  );
}

export function DetailDisclosure({
  sections,
  planPassLinkView,
  currentStep,
  className,
}: DetailDisclosureProps) {
  return (
    <section
      className={cn("min-w-0", className)}
      data-testid="detail-disclosure"
    >
      <ProgressiveDisclosure
        resetKey={currentStep}
        label={(expanded) => (expanded ? "Hide details" : "Show details")}
        triggerClassName="text-sm font-medium"
      >
        <div className="flex flex-col gap-3" data-testid="detail-disclosure-content">
          {planPassLinkView ? (
            <PlanPassJumpLink view={planPassLinkView} className="self-start" />
          ) : null}

          {sections.length > 0 ? (
            <div
              role="list"
              className="flex flex-col divide-y-0"
              data-testid="detail-disclosure-sections"
            >
              {sections.map((section) => (
                <DetailDisclosureSection key={section.key} section={section} />
              ))}
            </div>
          ) : null}
        </div>
      </ProgressiveDisclosure>
    </section>
  );
}
