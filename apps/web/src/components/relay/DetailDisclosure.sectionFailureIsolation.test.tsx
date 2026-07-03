// @vitest-environment jsdom
//
// ============================================================
// Run Status Tracker Redesign — DetailDisclosure per-section
// failure isolation (Requirement 5.10)
// ============================================================
//
// Task 8.4: unit tests asserting that a `DetailSection.render()`
// throw/rejection surfaces an inline failure message scoped to that
// section only, with sibling sections and the regions above
// `DetailDisclosure` still rendering normally.
//
// Validates: Requirement 5.10

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

import type { DetailSection } from "@/features/relay-runs/runStatusTrackerViews";
import { DetailDisclosure } from "./DetailDisclosure";

// ------------------------------------------------------------
// Test lifecycle
// ------------------------------------------------------------
//
// The synchronous-throw-during-React-render case (test 2 below) causes
// React to log an error boundary warning to the console even though the
// boundary correctly recovers. Suppress that expected noise, matching the
// convention used elsewhere in this codebase's route tests
// (execute.test.tsx / prepare.test.tsx / audit.test.tsx).
beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// ------------------------------------------------------------
// Helpers
// ------------------------------------------------------------

function findSectionContainer(sectionKey: string): HTMLElement {
  const containers = screen.getAllByTestId("detail-section");
  const match = containers.find(
    (el) => el.getAttribute("data-section-key") === sectionKey,
  );
  if (!match) {
    throw new Error(`No detail-section container found for key "${sectionKey}"`);
  }
  return match;
}

function openSection(sectionKey: string, label: RegExp) {
  const container = findSectionContainer(sectionKey);
  const trigger = within(container).getByRole("button", { name: label });
  fireEvent.click(trigger);
  return container;
}

/** A component whose render throws — used to simulate an error thrown while
 * React renders the tree a section's `render()` returned, as opposed to a
 * throw from `render()` itself. */
function Boom(): React.ReactElement {
  throw new Error("boom during React render");
}

// ------------------------------------------------------------
// Test 1: synchronous throw from render() itself
// ------------------------------------------------------------

describe("DetailDisclosure — per-section failure isolation (Req 5.10)", () => {
  it("scopes a synchronous render() throw to just that section, leaving siblings and content above unaffected", () => {
    const sections: DetailSection[] = [
      {
        key: "section-a",
        label: "Section A",
        render: () => <span>SECTION_A_CONTENT</span>,
      },
      {
        key: "section-b",
        label: "Section B",
        render: () => {
          throw new Error("section-b render failed synchronously");
        },
      },
      {
        key: "section-c",
        label: "Section C",
        render: () => <span>SECTION_C_CONTENT</span>,
      },
    ];

    render(
      <div>
        <div data-testid="above-marker">ABOVE_DETAIL_DISCLOSURE_MARKER</div>
        <DetailDisclosure sections={sections} currentStep="execute" />
      </div>,
    );

    // Open the outer "Show details" disclosure.
    fireEvent.click(screen.getByRole("button", { name: /show details/i }));

    // Open the failing section (Section B).
    const sectionB = openSection("section-b", /section b/i);

    // The inline failure message is scoped to Section B's own container.
    expect(within(sectionB).getByTestId("detail-section-error")).toHaveTextContent(
      "Couldn't load Section B.",
    );
    expect(screen.queryByText("SECTION_B_CONTENT")).not.toBeInTheDocument();

    // Content above DetailDisclosure is unaffected.
    expect(screen.getByTestId("above-marker")).toHaveTextContent(
      "ABOVE_DETAIL_DISCLOSURE_MARKER",
    );

    // Sibling sections remain openable and render their own content
    // normally, with no failure message leaking into them.
    const sectionA = openSection("section-a", /section a/i);
    expect(within(sectionA).getByText("SECTION_A_CONTENT")).toBeInTheDocument();
    expect(within(sectionA).queryByTestId("detail-section-error")).not.toBeInTheDocument();

    const sectionC = openSection("section-c", /section c/i);
    expect(within(sectionC).getByText("SECTION_C_CONTENT")).toBeInTheDocument();
    expect(within(sectionC).queryByTestId("detail-section-error")).not.toBeInTheDocument();

    // Only one failure message exists on the page — the one scoped to
    // Section B.
    expect(screen.getAllByTestId("detail-section-error")).toHaveLength(1);
  });

  // ------------------------------------------------------------
  // Test 2: error thrown while React renders the returned tree
  // ------------------------------------------------------------

  it("scopes an error thrown while rendering the returned tree to just that section, via SectionErrorBoundary, without crashing the page or affecting siblings", () => {
    const sections: DetailSection[] = [
      {
        key: "section-a",
        label: "Section A",
        render: () => <span>SECTION_A_CONTENT</span>,
      },
      {
        key: "section-boom",
        label: "Section Boom",
        // `render()` itself does not throw — it returns a valid React
        // element whose component throws while React renders it.
        render: () => <Boom />,
      },
      {
        key: "section-c",
        label: "Section C",
        render: () => <span>SECTION_C_CONTENT</span>,
      },
    ];

    render(
      <div>
        <div data-testid="above-marker">ABOVE_DETAIL_DISCLOSURE_MARKER</div>
        <DetailDisclosure sections={sections} currentStep="execute" />
      </div>,
    );

    fireEvent.click(screen.getByRole("button", { name: /show details/i }));

    const sectionBoom = openSection("section-boom", /section boom/i);

    // SectionErrorBoundary catches the render-time throw and shows the same
    // inline failure message, scoped to that section only.
    expect(
      within(sectionBoom).getByTestId("detail-section-error"),
    ).toHaveTextContent("Couldn't load Section Boom.");

    // The page did not crash — content above DetailDisclosure is still
    // present and unaffected.
    expect(screen.getByTestId("above-marker")).toHaveTextContent(
      "ABOVE_DETAIL_DISCLOSURE_MARKER",
    );

    // Sibling sections still render their own content normally once
    // opened.
    const sectionA = openSection("section-a", /section a/i);
    expect(within(sectionA).getByText("SECTION_A_CONTENT")).toBeInTheDocument();
    expect(within(sectionA).queryByTestId("detail-section-error")).not.toBeInTheDocument();

    const sectionC = openSection("section-c", /section c/i);
    expect(within(sectionC).getByText("SECTION_C_CONTENT")).toBeInTheDocument();
    expect(within(sectionC).queryByTestId("detail-section-error")).not.toBeInTheDocument();

    expect(screen.getAllByTestId("detail-section-error")).toHaveLength(1);
  });
});
