// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RepositoriesListPage } from "./index";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: { repositories: [] },
    isLoading: false,
    error: null,
  }),
  queryOptions: (options: unknown) => options,
}));

vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (configuration: unknown) => configuration,
}));

vi.mock("@/components/relay/AppPageFrame", () => ({
  AppPageFrame: ({
    actions,
    children,
  }: {
    actions: React.ReactNode;
    children: React.ReactNode;
  }) => (
    <section>
      <div>{actions}</div>
      <div>{children}</div>
    </section>
  ),
}));

vi.mock("@/components/relay/RelayRepositoriesRegistry", () => ({
  RelayRepositoriesRegistry: ({ onRegister }: { onRegister: () => void }) => (
    <button type="button" onClick={onRegister}>
      Empty registry registration
    </button>
  ),
}));

vi.mock("@/components/relay/RelayRepositoryRegistrationDialog", () => ({
  RelayRepositoryRegistrationDialog: ({
    open,
    onCompleted,
  }: {
    open: boolean;
    onCompleted: (result: {
      outcome: "created" | "reused";
      repository: {
        repoTarget: string;
        localPath: string;
        createdAt: string;
        updatedAt: string;
      };
    }) => void;
  }) =>
    open ? (
      <div role="dialog" aria-label="Register local repository">
        <button
          type="button"
          onClick={() =>
            onCompleted({
              outcome: "created",
              repository: {
                repoTarget: "relay",
                localPath: "D:/Code/relay",
                createdAt: "2026-07-11T00:00:00Z",
                updatedAt: "2026-07-11T00:00:00Z",
              },
            })
          }
        >
          Complete created registration
        </button>
        <button
          type="button"
          onClick={() =>
            onCompleted({
              outcome: "reused",
              repository: {
                repoTarget: "relay",
                localPath: "D:/Code/relay",
                createdAt: "2026-07-11T00:00:00Z",
                updatedAt: "2026-07-11T00:00:00Z",
              },
            })
          }
        >
          Complete reused registration
        </button>
      </div>
    ) : null,
}));

describe("RepositoriesListPage", () => {
  it.each([
    ["created", "Complete created registration"],
    ["reused", "Complete reused registration"],
  ] as const)(
    "keeps the actual %s outcome visible after the registration dialog closes",
    async (outcome, completionButton) => {
      const user = userEvent.setup();
      render(<RepositoriesListPage />);

      await user.click(screen.getByRole("button", { name: "Register repository" }));
      expect(
        screen.getByRole("dialog", { name: "Register local repository" }),
      ).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: completionButton }));

      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(screen.getByRole("status")).toHaveTextContent(
        `Repository relay was ${outcome}.`,
      );
    },
  );
});
