import { describe, expect, it } from "vitest";

import {
  auditStatusQueryOptions,
  relayRunKeys,
  runArtifactContentQueryOptions,
} from "./queries";

describe("relay-runs queries", () => {
  it("exports audit status query options with stable key shape", () => {
    const options = auditStatusQueryOptions("123");

    expect(options.queryKey).toEqual([
      ...relayRunKeys.all,
      "detail",
      "123",
      "audit-status",
    ]);
    expect(typeof options.queryFn).toBe("function");
  });

  it("exports run artifact content query options", () => {
    const options = runArtifactContentQueryOptions("123", "audit_packet");

    expect(options.queryKey).toEqual([
      ...relayRunKeys.all,
      "detail",
      "123",
      "artifacts",
      "audit_packet",
    ]);
    expect(typeof options.queryFn).toBe("function");
  });
});
