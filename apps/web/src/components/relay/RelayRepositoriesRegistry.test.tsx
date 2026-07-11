// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RelayRepositoriesRegistry } from "./RelayRepositoriesRegistry";

describe("RelayRepositoriesRegistry", () => {
  it("renders loading, error, empty, and populated registry states", async () => {
    const onRegister = vi.fn();
    const { rerender } = render(
      <RelayRepositoriesRegistry
        isLoading
        error={null}
        repositories={undefined}
        onRegister={onRegister}
      />,
    );
    expect(document.querySelectorAll("[data-slot='skeleton']").length).toBeGreaterThan(0);

    rerender(
      <RelayRepositoriesRegistry
        isLoading={false}
        error={new Error("load failed")}
        repositories={undefined}
        onRegister={onRegister}
      />,
    );
    expect(screen.getByText("Repositories failed to load")).toBeInTheDocument();

    rerender(
      <RelayRepositoriesRegistry
        isLoading={false}
        error={null}
        repositories={[]}
        onRegister={onRegister}
      />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Register repository" }));
    expect(onRegister).toHaveBeenCalledTimes(1);

    rerender(
      <RelayRepositoriesRegistry
        isLoading={false}
        error={null}
        repositories={[
          {
            repoTarget: "relay",
            localPath: "D:/Code/relay",
            createdAt: "2026-07-11T00:00:00Z",
            updatedAt: "2026-07-11T00:00:00Z",
          },
        ]}
        onRegister={onRegister}
      />,
    );
    expect(screen.getByText("relay")).toBeInTheDocument();
    expect(screen.getByText("D:/Code/relay")).toBeInTheDocument();
  });
});
