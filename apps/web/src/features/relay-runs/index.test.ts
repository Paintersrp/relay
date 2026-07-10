import { describe, expect, it } from "vitest";

import {
  workflowAuditStatusQueryOptions,
  getWorkflowAuditStatus,
  workflowArtifactContentQueryOptions,
  prepareWorkflowAudit,
} from "./index";

describe("relay-runs barrel exports", () => {
  it("exports audit query and API helpers", () => {
    expect(typeof workflowAuditStatusQueryOptions).toBe("function");
    expect(typeof workflowArtifactContentQueryOptions).toBe("function");
    expect(typeof getWorkflowAuditStatus).toBe("function");
    expect(typeof prepareWorkflowAudit).toBe("function");
  });
});
