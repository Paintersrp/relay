import { describe, expect, it } from "vitest";

import type { RelayRun } from "@/features/relay-runs";
import {
  AUDIT_PIPELINE_STEPS,
  getAuditDisplayState,
  getAuditPipelineStatuses,
  type AuditVisualStateInput,
} from "./runAuditVisualState";

function input(
  status: RelayRun["status"] | "local_validation_running",
  overrides: Partial<AuditVisualStateInput> = {},
): AuditVisualStateInput {
  return {
    run: {
      status: status as RelayRun["status"],
      lifecycleState: "audit",
    },
    hasFinalValidationEvidence: false,
    validationAllowsAudit: false,
    hasAuditPacket: false,
    hasInputSummary: false,
    hasWarnings: false,
    generatePending: false,
    validatePending: false,
    manualSubmitPending: false,
    approvePending: false,
    revisionPending: false,
    commitMessagePending: false,
    closePending: false,
    acceptFailurePending: false,
    ...overrides,
  };
}

describe("runAuditVisualState", () => {
  it("defines the stable audit pipeline steps", () => {
    expect(AUDIT_PIPELINE_STEPS.map((step) => step.id)).toEqual([
      "result-captured",
      "validation-reviewed",
      "scope-reviewed",
      "evidence-reviewed",
      "audit-decision",
    ]);
  });

  it("maps executor_done without final validation evidence to validation_required", () => {
    expect(getAuditDisplayState(input("executor_done"))).toBe(
      "validation_required",
    );
    expect(getAuditPipelineStatuses(input("executor_done"))).toEqual({
      "result-captured": "success",
      "validation-reviewed": "active",
      "scope-reviewed": "waiting",
      "evidence-reviewed": "waiting",
      "audit-decision": "waiting",
    });
  });

  it("maps local_validation_running to validation_running", () => {
    expect(getAuditDisplayState(input("local_validation_running"))).toBe(
      "validation_running",
    );
    expect(
      getAuditPipelineStatuses(input("local_validation_running"))[
        "validation-reviewed"
      ],
    ).toBe("running");
  });

  it("maps validation_failed to validation_failed and blocks decision progression", () => {
    expect(getAuditDisplayState(input("validation_failed"))).toBe(
      "validation_failed",
    );
    expect(getAuditPipelineStatuses(input("validation_failed"))).toMatchObject({
      "validation-reviewed": "failed",
      "audit-decision": "blocked",
    });
  });

  it("maps validation_failed_accepted to validation_accepted", () => {
    expect(
      getAuditDisplayState(
        input("validation_failed_accepted", {
          hasFinalValidationEvidence: true,
        }),
      ),
    ).toBe("validation_accepted");
    expect(
      getAuditPipelineStatuses(
        input("validation_failed_accepted", {
          hasFinalValidationEvidence: true,
        }),
      )["validation-reviewed"],
    ).toBe("warning");
  });

  it("maps validation_passed and eligible candidates to audit generation readiness", () => {
    expect(
      getAuditDisplayState(
        input("validation_passed", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
        }),
      ),
    ).toBe("validation_passed");

    expect(
      getAuditDisplayState(
        input("executor_done", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
        }),
      ),
    ).toBe("audit_candidate");

    expect(
      getAuditPipelineStatuses(
        input("executor_done", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
        }),
      ),
    ).toMatchObject({
      "result-captured": "success",
      "validation-reviewed": "success",
      "scope-reviewed": "active",
      "audit-decision": "waiting",
    });
  });

  it("maps audit_ready to an active audit decision", () => {
    expect(
      getAuditDisplayState(
        input("audit_ready", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      ),
    ).toBe("audit_ready");
    expect(
      getAuditPipelineStatuses(
        input("audit_ready", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      ),
    ).toMatchObject({
      "scope-reviewed": "success",
      "evidence-reviewed": "success",
      "audit-decision": "active",
    });
  });

  it("maps accepted to accepted", () => {
    expect(
      getAuditDisplayState(
        input("accepted", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      ),
    ).toBe("accepted");
    expect(
      getAuditPipelineStatuses(
        input("accepted", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      )["audit-decision"],
    ).toBe("accepted");
  });

  it("maps accepted_with_warnings to accepted_with_warnings", () => {
    expect(
      getAuditDisplayState(
        input("accepted_with_warnings", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
          hasWarnings: true,
        }),
      ),
    ).toBe("accepted_with_warnings");
    expect(
      getAuditPipelineStatuses(
        input("accepted_with_warnings", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
          hasWarnings: true,
        }),
      )["audit-decision"],
    ).toBe("warning");
  });

  it("maps revision_required to revision_required", () => {
    expect(
      getAuditDisplayState(
        input("revision_required", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      ),
    ).toBe("revision_required");
    expect(
      getAuditPipelineStatuses(
        input("revision_required", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      )["audit-decision"],
    ).toBe("revision");
  });

  it("maps completed to completed with terminal pipeline steps", () => {
    const statuses = getAuditPipelineStatuses(
      input("completed", {
        hasFinalValidationEvidence: true,
        validationAllowsAudit: true,
        hasAuditPacket: true,
        hasInputSummary: true,
      }),
    );

    expect(
      getAuditDisplayState(
        input("completed", {
          hasFinalValidationEvidence: true,
          validationAllowsAudit: true,
          hasAuditPacket: true,
          hasInputSummary: true,
        }),
      ),
    ).toBe("completed");
    expect(statuses["result-captured"]).toBe("success");
    expect(statuses["validation-reviewed"]).toBe("success");
    expect(statuses["scope-reviewed"]).toBe("success");
    expect(statuses["evidence-reviewed"]).toBe("success");
    expect(statuses["audit-decision"]).toBe("accepted");
  });
});
