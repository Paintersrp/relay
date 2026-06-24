import { describe, expect, it } from "vitest";

import { RelayApiError } from "@/features/relay-runs";

import { extractRefactorValidationIssues } from "./api";
import {
  formatLines,
  formatMetadata,
  parseLines,
  parseMetadata,
  parseTags,
} from "./form";

describe("refactor form parsing helpers", () => {
  it("parseLines trims and drops blank lines", () => {
    expect(parseLines("a\n  b  \n\n\r\nc")).toEqual(["a", "b", "c"]);
    expect(parseLines("   ")).toEqual([]);
  });

  it("formatLines round-trips line arrays", () => {
    expect(formatLines(["a", "b", "c"])).toBe("a\nb\nc");
    expect(formatLines(undefined)).toBe("");
  });

  it("parseTags splits on commas and newlines", () => {
    expect(parseTags("perf, cleanup\nrisk,, ")).toEqual([
      "perf",
      "cleanup",
      "risk",
    ]);
  });

  it("parseMetadata parses key=value lines and ignores malformed ones", () => {
    expect(parseMetadata("owner=alice\nrisk = high\n=novalue\nnoequals")).toEqual({
      owner: "alice",
      risk: "high",
    });
  });

  it("formatMetadata round-trips a record", () => {
    expect(formatMetadata({ owner: "alice", risk: "high" })).toBe(
      "owner=alice\nrisk=high",
    );
    expect(formatMetadata(undefined)).toBe("");
  });
});

describe("extractRefactorValidationIssues", () => {
  it("returns [] for non-RelayApiError values", () => {
    expect(extractRefactorValidationIssues(new Error("boom"))).toEqual([]);
    expect(extractRefactorValidationIssues(null)).toEqual([]);
    expect(extractRefactorValidationIssues("nope")).toEqual([]);
  });

  it("returns [] when no validation details are present", () => {
    const err = new RelayApiError("bad", 400, "/x", "POST", {
      error: "validation_error",
      message: "bad",
    });
    expect(extractRefactorValidationIssues(err)).toEqual([]);
  });

  it("maps backend validation issues into structured field/code/message entries", () => {
    const err = new RelayApiError("bad", 400, "/x", "POST", {
      error: "validation_error",
      message: "bad",
      details: {
        validation: [
          { field: "target_files", code: "not_pass_ready", message: "required" },
          { field: "risk_level", code: "invalid_risk_level", message: "bad risk" },
          // Non-object/garbage entries are filtered out.
          null,
          "garbage",
          { field: 123 },
        ],
      },
    });

    expect(extractRefactorValidationIssues(err)).toEqual([
      { field: "target_files", code: "not_pass_ready", message: "required" },
      { field: "risk_level", code: "invalid_risk_level", message: "bad risk" },
      { field: "", code: "", message: "" },
    ]);
  });
});
