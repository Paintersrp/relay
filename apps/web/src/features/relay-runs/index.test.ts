import { describe, expect, it } from "vitest";

import {
  auditStatusQueryOptions,
  getAuditStatus,
  runArtifactContentQueryOptions,
  submitManualAuditPacket,
} from "./index";

describe("relay-runs barrel exports", () => {
  it("exports audit query and API helpers", () => {
    expect(typeof auditStatusQueryOptions).toBe("function");
    expect(typeof runArtifactContentQueryOptions).toBe("function");
    expect(typeof getAuditStatus).toBe("function");
    expect(typeof submitManualAuditPacket).toBe("function");
  });
});
