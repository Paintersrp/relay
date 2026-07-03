// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { CurrentStatusBlock } from "./CurrentStatusBlock";
import type { CurrentStatusView, Tone } from "@/features/relay-runs/runStatusTrackerViews";

function isoMinutesAgo(minutes: number): string {
  return new Date(Date.now() - minutes * 60 * 1000).toISOString();
}

describe("CurrentStatusBlock", () => {
  it("renders headline, detail, tone styling, and relative updated-at time when detail is present", () => {
    const view: CurrentStatusView = {
      tone: "warning",
      headline: "Waiting on your review",
      detail: "Two blockers need attention before this can continue.",
      updatedAt: isoMinutesAgo(5),
    };

    render(<CurrentStatusBlock view={view} />);

    expect(screen.getByTestId("current-status-headline")).toHaveTextContent(
      "Waiting on your review",
    );
    expect(screen.getByTestId("current-status-detail")).toHaveTextContent(
      "Two blockers need attention before this can continue.",
    );

    const block = screen.getByTestId("current-status-block");
    expect(block).toHaveAttribute("data-tone", "warning");

    expect(screen.getByTestId("current-status-updated-at")).toHaveTextContent(
      "Updated 5 minutes ago",
    );
  });

  it("omits the detail element when detail is not provided, while still rendering headline/tone/updatedAt", () => {
    const view: CurrentStatusView = {
      tone: "neutral",
      headline: "Run is queued",
      updatedAt: isoMinutesAgo(1),
    };

    render(<CurrentStatusBlock view={view} />);

    expect(screen.getByTestId("current-status-headline")).toHaveTextContent(
      "Run is queued",
    );
    expect(screen.queryByTestId("current-status-detail")).not.toBeInTheDocument();

    const block = screen.getByTestId("current-status-block");
    expect(block).toHaveAttribute("data-tone", "neutral");

    expect(screen.getByTestId("current-status-updated-at")).toBeInTheDocument();
  });

  it.each<Tone>(["danger", "success"])(
    "reflects the %s tone via the data-tone attribute",
    (tone) => {
      const view: CurrentStatusView = {
        tone,
        headline: `Headline for ${tone}`,
        updatedAt: isoMinutesAgo(10),
      };

      render(<CurrentStatusBlock view={view} />);

      expect(screen.getByTestId("current-status-block")).toHaveAttribute(
        "data-tone",
        tone,
      );
    },
  );
});
