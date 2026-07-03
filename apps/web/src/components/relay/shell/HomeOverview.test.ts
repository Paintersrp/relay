import { describe, expect, it } from "vitest";

import { deriveSectionStatus } from "./HomeOverview";
import type { ShellDataQueryState } from "@/features/relay-navigation/useShellData";

function queryState(overrides: Partial<ShellDataQueryState> = {}): ShellDataQueryState {
  return {
    isLoading: false,
    isError: false,
    error: undefined,
    refetch: () => {},
    ...overrides,
  };
}

describe("deriveSectionStatus", () => {
  it("returns 'error' when any backing query errors, even while another loads (Req 3.7)", () => {
    const status = deriveSectionStatus(
      [queryState({ isError: true }), queryState({ isLoading: true })],
      5,
    );
    expect(status).toBe("error");
  });

  it("prefers error over loading and items so the retryable error state shows (Req 3.7)", () => {
    expect(deriveSectionStatus([queryState({ isError: true })], 10)).toBe("error");
  });

  it("returns 'loading' while a source loads and none have errored", () => {
    expect(deriveSectionStatus([queryState({ isLoading: true }), queryState()], 0)).toBe(
      "loading",
    );
  });

  it("returns 'empty' only after a successful load with no items (Req 3.5, 3.6)", () => {
    expect(deriveSectionStatus([queryState(), queryState()], 0)).toBe("empty");
  });

  it("returns 'ready' when loaded with at least one item", () => {
    expect(deriveSectionStatus([queryState()], 1)).toBe("ready");
  });
});
