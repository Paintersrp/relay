// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ProgressiveDisclosure } from "./ProgressiveDisclosure";

describe("ProgressiveDisclosure", () => {
  it("defaults to collapsed on mount", () => {
    render(
      <ProgressiveDisclosure label="Show details">
        <div>hidden content</div>
      </ProgressiveDisclosure>,
    );

    const trigger = screen.getByRole("button", { name: "Show details" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("hidden content")).not.toBeInTheDocument();
  });

  it("expands the content when the trigger is clicked", async () => {
    const user = userEvent.setup();
    render(
      <ProgressiveDisclosure label="Show details">
        <div>hidden content</div>
      </ProgressiveDisclosure>,
    );

    const trigger = screen.getByRole("button", { name: "Show details" });
    await user.click(trigger);

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("hidden content")).toBeInTheDocument();
  });

  it("collapses the content again when the trigger is clicked a second time", async () => {
    const user = userEvent.setup();
    render(
      <ProgressiveDisclosure label="Show details">
        <div>hidden content</div>
      </ProgressiveDisclosure>,
    );

    const trigger = screen.getByRole("button", { name: "Show details" });
    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");

    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("hidden content")).not.toBeInTheDocument();
  });

  it("resets to collapsed when resetKey changes (Active_Route_Step change)", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ProgressiveDisclosure label="Show details" resetKey="execute">
        <div>hidden content</div>
      </ProgressiveDisclosure>,
    );

    const trigger = screen.getByRole("button", { name: "Show details" });
    await user.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("hidden content")).toBeInTheDocument();

    rerender(
      <ProgressiveDisclosure label="Show details" resetKey="audit">
        <div>hidden content</div>
      </ProgressiveDisclosure>,
    );

    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("hidden content")).not.toBeInTheDocument();
  });
});
