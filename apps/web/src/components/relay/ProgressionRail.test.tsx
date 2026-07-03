// @vitest-environment jsdom
//
// ============================================================
// Example/unit tests — ProgressionRail (task 7.2)
// ============================================================
//
// Covers Requirement 4.2 (most-recent-first ordering), 4.3 (default
// collapse to 3 entries inline), 4.4 (full-list reveal on expansion via
// the existing ProgressiveDisclosure "Show full history (N)" affordance),
// 4.8 ("No history yet" when entries is empty and events loaded
// successfully), 4.9 ("History unavailable" when the events query
// failed), and 4.10 (expansion-state persistence across Active_Route_Step
// navigation within the same run, keyed by runId rather than by step).

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ProgressionRail } from "./ProgressionRail";
import type { ProgressionEntry } from "@/features/relay-runs/runStatusTrackerViews";

function buildEntry(overrides: Partial<ProgressionEntry>): ProgressionEntry {
  return {
    id: "entry-0",
    timestamp: "2024-01-01T00:00:00.000Z",
    label: "Something happened",
    tone: "neutral",
    ...overrides,
  };
}

const FIVE_ENTRIES: ProgressionEntry[] = [
  buildEntry({ id: "e1", label: "Handoff received", timestamp: "2024-01-01T00:00:00.000Z" }),
  buildEntry({ id: "e2", label: "Frontmatter parsed", timestamp: "2024-01-02T00:00:00.000Z" }),
  buildEntry({ id: "e3", label: "Compile started", timestamp: "2024-01-03T00:00:00.000Z" }),
  buildEntry({ id: "e4", label: "Packet validated", timestamp: "2024-01-04T00:00:00.000Z" }),
  buildEntry({ id: "e5", label: "Brief rendered", timestamp: "2024-01-05T00:00:00.000Z" }),
];

// ------------------------------------------------------------
// Requirement 4.2 — most-recent-first ordering
// ------------------------------------------------------------

describe("ProgressionRail — ordering (Req 4.2)", () => {
  it("renders entries most-recent-first even when passed out of order", () => {
    const outOfOrder: ProgressionEntry[] = [
      buildEntry({ id: "old", label: "Oldest event", timestamp: "2024-01-01T00:00:00.000Z" }),
      buildEntry({ id: "newest", label: "Newest event", timestamp: "2024-01-03T00:00:00.000Z" }),
      buildEntry({ id: "middle", label: "Middle event", timestamp: "2024-01-02T00:00:00.000Z" }),
    ];

    render(<ProgressionRail runId="run-1" entries={outOfOrder} />);

    const rows = screen.getAllByTestId("progression-entry");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Newest event");
    expect(rows[1]).toHaveTextContent("Middle event");
    expect(rows[2]).toHaveTextContent("Oldest event");
  });
});

// ------------------------------------------------------------
// Requirement 4.3 / 4.4 — default collapse + full-list reveal
// ------------------------------------------------------------

describe("ProgressionRail — default collapse and expansion (Req 4.3, 4.4)", () => {
  it("shows only the most recent 3 entries inline with a 'Show full history (N)' toggle when there are more than 3", () => {
    render(<ProgressionRail runId="run-1" entries={FIVE_ENTRIES} />);

    const rows = screen.getAllByTestId("progression-entry");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Brief rendered");
    expect(rows[1]).toHaveTextContent("Packet validated");
    expect(rows[2]).toHaveTextContent("Compile started");

    expect(
      screen.getByRole("button", { name: "Show full history (5)" }),
    ).toBeInTheDocument();
  });

  it("reveals the full list when the toggle is clicked", async () => {
    const user = userEvent.setup();
    render(<ProgressionRail runId="run-1" entries={FIVE_ENTRIES} />);

    const toggle = screen.getByRole("button", { name: "Show full history (5)" });
    await user.click(toggle);

    const rows = screen.getAllByTestId("progression-entry");
    expect(rows).toHaveLength(5);
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringContaining("Brief rendered"),
      expect.stringContaining("Packet validated"),
      expect.stringContaining("Compile started"),
      expect.stringContaining("Frontmatter parsed"),
      expect.stringContaining("Handoff received"),
    ]);
  });

  it("does not render a 'Show full history' toggle when entries fit within collapsedCount", () => {
    render(<ProgressionRail runId="run-1" entries={FIVE_ENTRIES.slice(0, 2)} />);

    expect(screen.getAllByTestId("progression-entry")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: /show full history/i })).not.toBeInTheDocument();
  });

  it("honors a custom collapsedCount", () => {
    render(<ProgressionRail runId="run-1" entries={FIVE_ENTRIES} collapsedCount={1} />);

    expect(screen.getAllByTestId("progression-entry")).toHaveLength(1);
    expect(
      screen.getByRole("button", { name: "Show full history (5)" }),
    ).toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// Requirement 4.8 / 4.9 — empty / failed states
// ------------------------------------------------------------

describe("ProgressionRail — empty and failed states (Req 4.8, 4.9)", () => {
  it("renders 'No history yet' when entries is empty and events loaded successfully", () => {
    render(<ProgressionRail runId="run-1" entries={[]} eventsLoadFailed={false} />);

    expect(screen.getByText("No history yet")).toBeInTheDocument();
    expect(screen.queryByTestId("progression-entry")).not.toBeInTheDocument();
  });

  it("renders 'History unavailable' when eventsLoadFailed is true and entries is empty", () => {
    render(<ProgressionRail runId="run-1" entries={[]} eventsLoadFailed />);

    expect(screen.getByText("History unavailable")).toBeInTheDocument();
    expect(screen.queryByText("No history yet")).not.toBeInTheDocument();
  });

  it("renders 'History unavailable' when eventsLoadFailed is true even with entries present", () => {
    render(<ProgressionRail runId="run-1" entries={FIVE_ENTRIES} eventsLoadFailed />);

    expect(screen.getByText("History unavailable")).toBeInTheDocument();
    expect(screen.queryByTestId("progression-entry")).not.toBeInTheDocument();
  });
});

// ------------------------------------------------------------
// Requirement 4.10 — expansion-state persistence across step change,
// reset on genuine run change
// ------------------------------------------------------------

describe("ProgressionRail — expansion-state persistence (Req 4.10)", () => {
  it("preserves expansion state across a simulated step change (same runId, different entries), and resets when runId changes", async () => {
    const user = userEvent.setup();
    const { rerender } = render(<ProgressionRail runId="run-1" entries={FIVE_ENTRIES} />);

    const toggle = screen.getByRole("button", { name: "Show full history (5)" });
    await user.click(toggle);
    expect(screen.getAllByTestId("progression-entry")).toHaveLength(5);

    // Simulate navigating to a different step sub-route of the SAME run:
    // same runId, new entries array.
    const stepChangedEntries: ProgressionEntry[] = [
      ...FIVE_ENTRIES,
      buildEntry({ id: "e6", label: "Executor dispatched", timestamp: "2024-01-06T00:00:00.000Z" }),
    ];
    rerender(<ProgressionRail runId="run-1" entries={stepChangedEntries} />);

    // Expansion state persisted — full list (now 6 entries) still shown.
    expect(screen.getAllByTestId("progression-entry")).toHaveLength(6);
    expect(
      screen.getByRole("button", { name: "Hide full history" }),
    ).toBeInTheDocument();

    // Simulate navigating to a DIFFERENT run entirely.
    rerender(<ProgressionRail runId="run-2" entries={stepChangedEntries} />);

    // Resets back to collapsed for the new run.
    expect(screen.getAllByTestId("progression-entry")).toHaveLength(3);
    expect(
      screen.getByRole("button", { name: "Show full history (6)" }),
    ).toBeInTheDocument();
  });
});
