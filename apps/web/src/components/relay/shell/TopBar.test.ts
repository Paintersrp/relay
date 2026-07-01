import { describe, expect, it } from "vitest";

import { shortcutHintLabel } from "./TopBar";

describe("shortcutHintLabel", () => {
  it("renders the ⌘K hint on macOS (Req 4.1)", () => {
    expect(shortcutHintLabel(true)).toBe("⌘K");
  });

  it("renders the Ctrl K hint on non-macOS platforms (Req 4.1)", () => {
    expect(shortcutHintLabel(false)).toBe("Ctrl K");
  });
});
