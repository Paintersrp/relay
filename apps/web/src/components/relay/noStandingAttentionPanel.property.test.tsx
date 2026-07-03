// @vitest-environment jsdom

// Feature: run-status-tracker-redesign, Property 3: No permanently-visible empty findings widget
//
// For any combination of a `CurrentStatusView` (varying tone including
// danger/warning, varying `detail` presence/absence) and a `StepActionsView`
// (varying number/state of controls, including zero controls) — spanning
// both blocked/failed-shaped states (danger/warning tone, non-empty detail
// simulating blocker/warning/revision-requirement counts) and
// not-blocked/not-failed-shaped states (neutral/info/success tone, no
// detail) — rendering `CurrentStatusBlock` + `NextActionArea` together never
// produces any element resembling a standing "attention"/"findings"/
// "blockers" panel, including an empty-state card for the "nothing wrong"
// case. This holds because neither component imports or renders
// `RunStepAttentionPanel`, regardless of the tone/detail/controls
// combination fed to them.
//
// Validates: Requirements 3.5, 3.6, 3.7, 3.8

import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";

import { CurrentStatusBlock } from "./CurrentStatusBlock";
import { NextActionArea } from "./NextActionArea";
import type {
  ActionControlView,
  CurrentStatusView,
  StepActionsView,
  Tone,
} from "@/features/relay-runs/runStatusTrackerViews";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const toneArb: fc.Arbitrary<Tone> = fc.constantFrom(
  "neutral",
  "info",
  "success",
  "warning",
  "danger",
);

// Detail text is sometimes generated to *mention* attention-shaped
// vocabulary (blockers/warnings/revision requirements) — the same
// vocabulary a standing attention panel would use — to make sure the
// assertions below are checking for a structural panel/empty-state
// element, not merely absent because the words never appear at all.
const attentionMentioningDetailArb: fc.Arbitrary<string> = fc.constantFrom(
  "2 blockers must be resolved before this run can proceed.",
  "1 warning was recorded for this step.",
  "3 revision requirements are outstanding.",
  "Blocked — review before retrying.",
);

const plainDetailArb = fc.string({ maxLength: 40 });

const optionalDetailArb: fc.Arbitrary<string | undefined> = fc.oneof(
  fc.constant(undefined),
  plainDetailArb,
  attentionMentioningDetailArb,
);

const isoDateArb: fc.Arbitrary<string> = fc
  .integer({ min: 0, max: 2_000_000_000_000 })
  .map((ms) => new Date(ms).toISOString());

const currentStatusViewArb: fc.Arbitrary<CurrentStatusView> = fc.record({
  tone: toneArb,
  headline: fc.string({ minLength: 1, maxLength: 60 }),
  detail: optionalDetailArb,
  updatedAt: isoDateArb,
});

// A control that may or may not be disabled and may or may not carry an
// `unavailableReason` mentioning blocker/attention-shaped vocabulary — this
// is the existing, permitted way blocker information surfaces (folded into
// the control itself), which must NOT be confused with a standing panel.
const actionControlViewArb = (isPrimary: boolean): fc.Arbitrary<ActionControlView> =>
  fc.record({
    id: fc.string({ minLength: 1, maxLength: 10 }),
    label: fc.string({ minLength: 1, maxLength: 20 }),
    enabled: fc.boolean(),
    unavailableReason: fc.oneof(
      fc.constant(undefined),
      fc.constant(""),
      attentionMentioningDetailArb,
      plainDetailArb,
    ),
    isPrimary: fc.constant(isPrimary),
  });

// Zero, one, or many controls — including the zero-control case, which
// must render nothing at all from NextActionArea.
const stepActionsViewArb: fc.Arbitrary<StepActionsView | undefined> = fc.oneof(
  fc.constant(undefined),
  fc.constant<StepActionsView>({ controls: [] }),
  fc
    .integer({ min: 1, max: 4 })
    .chain((count) =>
      fc
        .integer({ min: 0, max: count - 1 })
        .chain((primaryIndex) =>
          fc
            .tuple(
              ...Array.from({ length: count }, (_, index) =>
                actionControlViewArb(index === primaryIndex),
              ),
            )
            .map((controls) => ({ controls })),
        ),
    ),
);

// ------------------------------------------------------------
// Attention-shaped panel detection
// ------------------------------------------------------------

// Text fragments a standing attention/findings panel (RunStepAttentionPanel)
// would use, including its own empty-state copy. None of these should ever
// appear when only CurrentStatusBlock + NextActionArea are rendered.
const ATTENTION_PANEL_TEXT_PATTERNS = [
  /no attention items/i,
  /nothing to review/i,
  /nothing needs review/i,
  /^attention$/i,
];

function assertNoStandingAttentionPanel(container: HTMLElement) {
  // No element carries a testid/attribute suggesting a permanent attention
  // widget.
  expect(container.querySelector('[data-testid*="attention" i]')).toBeNull();
  expect(container.querySelector('[data-slot*="attention" i]')).toBeNull();
  expect(container.querySelector('[class*="attention" i]')).toBeNull();

  // No text resembling an attention-panel heading or its empty-state copy.
  for (const pattern of ATTENTION_PANEL_TEXT_PATTERNS) {
    expect(screen.queryByText(pattern)).toBeNull();
  }
}

// ------------------------------------------------------------
// Property
// ------------------------------------------------------------

describe("CurrentStatusBlock + NextActionArea — Property 3: No permanently-visible empty findings widget", () => {
  it("never renders a standing attention/findings panel or an empty-state attention card, across blocked/failed and not-blocked/not-failed tones, and empty/non-empty control sets (Req 3.5, 3.6, 3.7, 3.8)", () => {
    fc.assert(
      fc.property(
        currentStatusViewArb,
        stepActionsViewArb,
        (currentStatus, actionsView) => {
          cleanup();

          const { container } = render(
            <div>
              <CurrentStatusBlock view={currentStatus} />
              <NextActionArea actionsView={actionsView} />
            </div>,
          );

          assertNoStandingAttentionPanel(container);

          // When there are zero controls (undefined actionsView, or an
          // actionsView with an empty controls array), NextActionArea must
          // render nothing at all — no DOM output, not even an empty-state
          // indicator.
          if (!actionsView || actionsView.controls.length === 0) {
            expect(
              container.querySelector('[data-slot="run-step-action-bar"]'),
            ).toBeNull();
          }

          cleanup();
        },
      ),
      { numRuns: 100 },
    );
  });
});
