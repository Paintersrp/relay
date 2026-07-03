// @vitest-environment jsdom

// Feature: run-status-tracker-redesign, Property 2: Zero/one/many next-action rendering
//
// For an arbitrarily generated `StepActionsView` (0..6 candidate controls,
// with at most one `isPrimary: true` control per the existing
// `runStepActions.ts` contract — a Next_Safe_Action is only ever the single
// first-enabled candidate in fixed priority order):
//
//   - When zero controls are enabled (zero Action_Gating_Flag values true),
//     `NextActionArea` renders no primary control, and renders every
//     candidate disabled in the secondary row, each showing its
//     `unavailableReason` when present.
//   - When exactly one control is enabled (and therefore `isPrimary`),
//     `NextActionArea` renders that control as the single prominent primary
//     control, and every other candidate renders disabled in the secondary
//     row.
//   - When more than one control is enabled, `NextActionArea` still renders
//     exactly the one control marked `isPrimary` as the primary control, and
//     every other candidate (enabled or disabled) renders in the secondary
//     row, still reachable.
//
// Validates: Requirements 3.2, 3.3, 3.4

import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { render, within } from "@testing-library/react";

import { NextActionArea } from "./NextActionArea";
import type {
  ActionControlView,
  StepActionsView,
} from "@/features/relay-runs/runStatusTrackerViews";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

interface CandidateTemplate {
  label: string;
  unavailableReason?: string;
}

// Restricted to a plain alphanumeric alphabet so generated labels/reasons
// never contain whitespace variants, control characters, or other unicode
// that could interfere with DOM text-content comparisons — the property
// under test is about primary/secondary placement, not string fidelity.
const safeTextArb = (minLength: number, maxLength: number) =>
  fc
    .array(
      fc.constantFrom(
        ..."abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
      ),
      { minLength, maxLength },
    )
    .map((chars) => chars.join(""));

const candidateTemplateArb: fc.Arbitrary<CandidateTemplate> = fc.record({
  label: safeTextArb(1, 10),
  unavailableReason: fc.option(safeTextArb(1, 20), {
    nil: undefined,
  }),
});

const templatesArb: fc.Arbitrary<CandidateTemplate[]> = fc.array(
  candidateTemplateArb,
  { minLength: 0, maxLength: 6 },
);

type ScenarioMode = "zero" | "one" | "many";

interface Scenario {
  mode: ScenarioMode;
  templates: CandidateTemplate[];
  primaryIndex: number; // -1 when no control is primary
  enabledIndices: Set<number>;
}

// Builds a scenario consistent with the real `StepActionsView` contract
// (see `runStepActions.ts::buildStepActionsView`): at most one control is
// ever `isPrimary`, and a control is only ever primary when it is enabled.
const scenarioArb: fc.Arbitrary<Scenario> = templatesArb.chain((templates) => {
  const n = templates.length;
  const modes: ScenarioMode[] = ["zero"];
  if (n >= 1) modes.push("one");
  if (n >= 2) modes.push("many");

  return fc.constantFrom(...modes).chain((mode): fc.Arbitrary<Scenario> => {
    if (mode === "zero") {
      return fc.constant({
        mode,
        templates,
        primaryIndex: -1,
        enabledIndices: new Set<number>(),
      });
    }

    if (mode === "one") {
      return fc.integer({ min: 0, max: n - 1 }).map((primaryIndex) => ({
        mode,
        templates,
        primaryIndex,
        enabledIndices: new Set<number>([primaryIndex]),
      }));
    }

    // mode === "many": one primary plus at least one other enabled control.
    return fc.integer({ min: 0, max: n - 1 }).chain((primaryIndex) => {
      const others = Array.from({ length: n }, (_, i) => i).filter(
        (i) => i !== primaryIndex,
      );
      return fc
        .subarray(others, { minLength: 1 })
        .map((extraEnabled) => ({
          mode,
          templates,
          primaryIndex,
          enabledIndices: new Set<number>([primaryIndex, ...extraEnabled]),
        }));
    });
  });
});

function buildView(scenario: Scenario): {
  view: StepActionsView;
  controls: ActionControlView[];
} {
  const controls: ActionControlView[] = scenario.templates.map(
    (template, index) => {
      const enabled = scenario.enabledIndices.has(index);
      const isPrimary = index === scenario.primaryIndex;
      return {
        id: `action-${index}`,
        label: `${template.label}__${index}`,
        enabled,
        isPrimary,
        ...(template.unavailableReason
          ? { unavailableReason: template.unavailableReason }
          : {}),
      };
    },
  );

  const view: StepActionsView = {
    controls,
    ...(scenario.primaryIndex >= 0
      ? { nextSafeActionId: `action-${scenario.primaryIndex}` }
      : {}),
  };

  return { view, controls };
}

describe("NextActionArea — Property 2: Zero/one/many next-action rendering", () => {
  it("renders no/one/exactly-one primary control matching the zero/one/many gating-flag contract (Req 3.2, 3.3, 3.4)", () => {
    fc.assert(
      fc.property(scenarioArb, (scenario) => {
        const { view, controls } = buildView(scenario);
        const { container, unmount } = render(
          <NextActionArea actionsView={view} />,
        );

        try {
          const primaryRegion = container.querySelector(
            '[data-slot="run-step-action-bar-primary"]',
          );
          const secondaryRegion = container.querySelector(
            '[data-slot="run-step-action-bar-secondary"]',
          );

          const primaryControl = controls.find((c) => c.isPrimary);
          const secondaryControls = controls.filter((c) => !c.isPrimary);

          // At most one control is ever primary, matching the real
          // StepActionsView contract this component trusts.
          expect(controls.filter((c) => c.isPrimary).length).toBeLessThanOrEqual(
            1,
          );

          if (!primaryControl) {
            // Zero Action_Gating_Flag values true: no primary control.
            expect(primaryRegion).toBeNull();
          } else {
            // Exactly one Action_Gating_Flag value true (or more, with one
            // selected by priority order): exactly one prominent primary
            // control, and it is enabled.
            expect(primaryRegion).not.toBeNull();
            const primaryButtons = within(
              primaryRegion as HTMLElement,
            ).getAllByRole("button");
            expect(primaryButtons).toHaveLength(1);
            expect(primaryButtons[0]).toHaveTextContent(primaryControl.label);
            expect(primaryButtons[0]).not.toBeDisabled();
          }

          if (secondaryControls.length === 0) {
            expect(secondaryRegion).toBeNull();
          } else {
            expect(secondaryRegion).not.toBeNull();
            const secondaryButtons = within(
              secondaryRegion as HTMLElement,
            ).getAllByRole("button");
            expect(secondaryButtons).toHaveLength(secondaryControls.length);

            secondaryControls.forEach((control, i) => {
              const button = secondaryButtons[i];
              expect(button).toHaveTextContent(control.label);
              // Every non-primary candidate is disabled exactly when its own
              // `enabled` flag is false — still reachable, never hidden.
              if (control.enabled) {
                expect(button).not.toBeDisabled();
              } else {
                expect(button).toBeDisabled();
              }

              const showsReason =
                !control.enabled && !!control.unavailableReason;
              if (showsReason) {
                expect(secondaryRegion).toHaveTextContent(
                  control.unavailableReason!,
                );
              }
            });
          }

          // Zero controls at all: nothing renders.
          if (controls.length === 0) {
            expect(container).toBeEmptyDOMElement();
          }
        } finally {
          unmount();
        }
      }),
      { numRuns: 100 },
    );
  });
});
