import { describe, expect, it } from "vitest";

import {
  auditStatusQueryOptions,
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
} from "./queries";

import {
  planDetailQueryOptions,
  planPassDetailQueryOptions,
} from "../relay-plans/queries";

describe("public query options barrel/query exports", () => {
  it("exports run-related query options correctly", () => {
    expect(auditStatusQueryOptions).toBeTypeOf("function");
    expect(runDetailQueryOptions).toBeTypeOf("function");
    expect(runArtifactsQueryOptions).toBeTypeOf("function");
    expect(runEventsQueryOptions).toBeTypeOf("function");
  });

  it("exports plan-related query options correctly", () => {
    expect(planDetailQueryOptions).toBeTypeOf("function");
    expect(planPassDetailQueryOptions).toBeTypeOf("function");
  });
});
