import { describe, expect, it } from "vitest";

import { resolveActiveScope, scopeOptionValue } from "./ScopeSwitcher";
import type { ScopeOption } from "@/features/relay-navigation/types";

const scopeOptions: ScopeOption[] = [
  {
    kind: "project",
    id: "proj-1",
    label: "Project One",
    to: "/projects/$projectId",
    params: { projectId: "proj-1" },
  },
  {
    kind: "plan",
    id: "plan-1",
    label: "Plan One",
    to: "/plans/$planId",
    params: { planId: "plan-1" },
  },
];

describe("scopeOptionValue", () => {
  it("prefixes the identifier with its kind so projects and plans never collide", () => {
    expect(scopeOptionValue({ kind: "project", id: "x" })).toBe("project:x");
    expect(scopeOptionValue({ kind: "plan", id: "x" })).toBe("plan:x");
  });
});

describe("resolveActiveScope", () => {
  it("returns null when no scope param is present (e.g. run-scoped route)", () => {
    expect(resolveActiveScope({}, scopeOptions)).toBeNull();
  });

  it("resolves a project scope from projectId and uses the matched label", () => {
    expect(resolveActiveScope({ projectId: "proj-1" }, scopeOptions)).toEqual({
      value: "project:proj-1",
      label: "Project One",
    });
  });

  it("resolves a plan scope from planId and uses the matched label", () => {
    expect(resolveActiveScope({ planId: "plan-1" }, scopeOptions)).toEqual({
      value: "plan:plan-1",
      label: "Plan One",
    });
  });

  it("prefers projectId over planId when both are present", () => {
    const result = resolveActiveScope(
      { projectId: "proj-1", planId: "plan-1" },
      scopeOptions,
    );
    expect(result?.value).toBe("project:proj-1");
  });

  it("falls back to the raw identifier when options have not loaded yet", () => {
    expect(resolveActiveScope({ planId: "plan-unknown" }, [])).toEqual({
      value: "plan:plan-unknown",
      label: "plan-unknown",
    });
  });
});
