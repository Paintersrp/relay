// @vitest-environment jsdom

// ============================================================
// Run Workbench Refinement — action invocation composition (Requirement 4.9)
// ============================================================
//
// Confirms the full "derive -> render -> click -> invoke" pipeline composes
// correctly: `deriveExecuteActions`/`deriveAuditActions` (pure view-data
// derivation) feeding `RunStepActionBar` (presentational rendering) whose
// `onActionClick` is wired via `createActionClickHandler` over the
// `EXECUTE_ACTION_HANDLERS`/`AUDIT_ACTION_HANDLERS` maps (invocation wiring)
// calls the correct existing `api.ts` function exactly once with the run id,
// without altering that function's request contract.

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@/features/relay-runs/api", () => ({
  executeRun: vi.fn(),
  cancelRun: vi.fn(),
  recoverRun: vi.fn(),
  auditRun: vi.fn(),
  closeRun: vi.fn(),
  prepareCommitMessage: vi.fn(),
}));

import { executeRun, cancelRun, recoverRun, auditRun } from "@/features/relay-runs/api";
import {
  deriveExecuteActions,
  deriveAuditActions,
} from "@/features/relay-runs/runStepActions";
import {
  EXECUTE_ACTION_HANDLERS,
  AUDIT_ACTION_HANDLERS,
  createActionClickHandler,
} from "@/features/relay-runs/runStepActionHandlers";
import { RunStepActionBar } from "./RunStepActionBar";

describe("RunStepActionBar action invocation composition", () => {
  it("calls executeRun exactly once with the run id when the primary (start) control is clicked, and does not call cancelRun/recoverRun", async () => {
    const user = userEvent.setup();

    const view = deriveExecuteActions({
      canStart: true,
      canCancel: false,
      canRecover: false,
    });
    const onActionClick = createActionClickHandler(EXECUTE_ACTION_HANDLERS, "run-123");

    render(<RunStepActionBar view={view} onActionClick={onActionClick} />);

    // "start" is the only enabled candidate, so it is the designated
    // Next_Safe_Action and renders as the primary control.
    const startButton = screen.getByRole("button", { name: "Start" });
    await user.click(startButton);

    expect(executeRun).toHaveBeenCalledTimes(1);
    expect(executeRun).toHaveBeenCalledWith("run-123");
    expect(cancelRun).not.toHaveBeenCalled();
    expect(recoverRun).not.toHaveBeenCalled();
  });

  it("calls auditRun exactly once with the run id when the generateAudit control is clicked", async () => {
    const user = userEvent.setup();

    const view = deriveAuditActions({
      canGenerateAudit: true,
      canSubmitManual: false,
      canApproveAudit: false,
      canRequestRevision: false,
      canPrepareCommitMessage: false,
      canCloseRun: false,
    });
    const onActionClick = createActionClickHandler(AUDIT_ACTION_HANDLERS, "run-456");

    render(<RunStepActionBar view={view} onActionClick={onActionClick} />);

    const generateAuditButton = screen.getByRole("button", {
      name: "Generate Audit",
    });
    await user.click(generateAuditButton);

    expect(auditRun).toHaveBeenCalledTimes(1);
    expect(auditRun).toHaveBeenCalledWith("run-456");
  });
});
