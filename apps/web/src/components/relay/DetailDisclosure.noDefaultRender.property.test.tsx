// @vitest-environment jsdom
//
// Feature: run-status-tracker-redesign, Property 4: Detail content never renders by default
//
// For any arbitrarily generated array of `DetailSection` (varying count —
// zero, one, or many — varying `key`/`label` values, each backed by a
// `vi.fn()` render spy that returns a unique marker node), rendering
// `DetailDisclosure` in its default (collapsed) state must never invoke any
// section's `render()` function, and none of the marker content each
// `render()` would have produced may appear anywhere in the rendered DOM.
// This must hold regardless of the number of sections generated and
// regardless of which Active_Route_Step (`currentStep`) is supplied.
//
// Validates: Requirement 5.3

import { describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import fc from "fast-check";

import { DetailDisclosure } from "./DetailDisclosure";
import type {
  DetailSection,
  RelayRunStep,
} from "@/features/relay-runs/runStatusTrackerViews";

// ------------------------------------------------------------
// Arbitraries
// ------------------------------------------------------------

const currentStepArb: fc.Arbitrary<RelayRunStep> = fc.constantFrom(
  "intake",
  "prepare",
  "execute",
  "audit",
);

// Raw section data (key/label/marker text). Each generated section carries
// a distinct marker string so we can assert none of them ever reach the
// DOM. Keys are de-duplicated below (by appending the array index) so React
// never warns about duplicate keys — that's an unrelated test-hygiene
// concern, not part of what this property verifies.
const sectionDataArb = fc.record({
  key: fc.string({ minLength: 1, maxLength: 12 }),
  label: fc.string({ minLength: 0, maxLength: 24 }),
  marker: fc.string({ minLength: 6, maxLength: 24 }),
});

const sectionsDataArb = fc.array(sectionDataArb, { maxLength: 10 });

// Also cover the zero-, one-, and many-section cases explicitly rather than
// leaving it purely to fast-check's array-length shrinking/exploration.
const sectionsDataArbWithBoundaries = fc.oneof(
  fc.constant([]),
  sectionDataArb.map((s) => [s]),
  sectionsDataArb,
);

describe("DetailDisclosure — Property 4: Detail content never renders by default", () => {
  it("never invokes any section's render() and never leaks marker content into the DOM in the default collapsed state (Req 5.3)", () => {
    fc.assert(
      fc.property(
        currentStepArb,
        sectionsDataArbWithBoundaries,
        (currentStep, sectionsData) => {
          const markers = sectionsData.map((d, index) => `MARKER_${index}_${d.marker}`);

          const sections: DetailSection[] = sectionsData.map((data, index) => ({
            key: `${data.key}-${index}`,
            label: data.label,
            render: vi.fn(() => markers[index]),
          }));

          const { unmount } = render(
            <DetailDisclosure sections={sections} currentStep={currentStep} />,
          );

          try {
            // No section's render() was invoked by default.
            for (const section of sections) {
              expect(section.render).not.toHaveBeenCalled();
            }

            // None of the marker content any render() would have produced
            // appears anywhere in the rendered DOM.
            for (const marker of markers) {
              expect(screen.queryByText(marker)).not.toBeInTheDocument();
            }

            // The outer disclosure affordance itself is present and
            // collapsed (the toggle exists, but the content it would reveal
            // does not render until activated).
            expect(
              screen.getByRole("button", { name: /show details/i }),
            ).toHaveAttribute("aria-expanded", "false");
          } finally {
            unmount();
            cleanup();
          }
        },
      ),
      { numRuns: 100 },
    );
  });
});
