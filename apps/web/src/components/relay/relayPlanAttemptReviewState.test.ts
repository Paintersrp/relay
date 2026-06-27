import { describe, expect, it } from "vitest";
import {
  canCreateAttempt,
  getPolicyFields,
  getProjectIdForMutation,
  getUIStateLabel,
} from "./relayPlanAttemptReviewState";

describe("relayPlanAttemptReviewState", () => {
  describe("canCreateAttempt", () => {
    const defaultParams = {
      projectId: "project-1",
      settingsLoadState: "loaded",
      settingsProjectId: "project-1",
      hasSettings: true,
      planPath: "handoffs/plan.json",
      planSha: "sha256:abc",
      userRequest: "Start PASS-007",
      state: "validated",
      revisionMode: false,
    };

    it("allows creation when all fields are valid and settings are loaded", () => {
      expect(canCreateAttempt(defaultParams)).toBe(true);
    });

    it("disables creation if project settings are still loading", () => {
      expect(
        canCreateAttempt({
          ...defaultParams,
          settingsLoadState: "loading",
        })
      ).toBe(false);
    });

    it("disables creation if settings project ID does not match exact project ID", () => {
      expect(
        canCreateAttempt({
          ...defaultParams,
          settingsProjectId: "project-2",
        })
      ).toBe(false);
    });

    it("disables creation if plan path or plan sha is empty", () => {
      expect(canCreateAttempt({ ...defaultParams, planPath: "" })).toBe(false);
      expect(canCreateAttempt({ ...defaultParams, planSha: "" })).toBe(false);
    });

    it("disables creation if literal user request is empty", () => {
      expect(canCreateAttempt({ ...defaultParams, userRequest: "" })).toBe(false);
    });

    it("allows creation in revision mode even if state is not validated", () => {
      expect(
        canCreateAttempt({
          ...defaultParams,
          state: "draft",
          revisionMode: true,
        })
      ).toBe(true);
    });
  });

  describe("getPolicyFields", () => {
    const settings = {
      driftReviewMode: "external",
      modelTier: "high_assurance",
    };

    it("returns correct policy fields when settings match project ID", () => {
      const fields = getPolicyFields({
        projectId: "project-1",
        settingsLoadState: "loaded",
        settingsProjectId: "project-1",
        settings,
      });
      expect(fields).toEqual(settings);
    });

    it("returns empty policy fields if settings are stale or mismatched", () => {
      const fields = getPolicyFields({
        projectId: "project-1",
        settingsLoadState: "loaded",
        settingsProjectId: "project-2",
        settings,
      });
      expect(fields).toEqual({});
    });

    it("returns empty policy fields if settings are loading", () => {
      const fields = getPolicyFields({
        projectId: "project-1",
        settingsLoadState: "loading",
        settingsProjectId: "project-1",
        settings,
      });
      expect(fields).toEqual({});
    });
  });

  describe("getProjectIdForMutation", () => {
    const attempt = {
      planAttemptId: "attempt-1",
      projectId: "project-attempt",
      status: "draft",
      reviewState: "review_packet_ready",
      driftReviewMode: "manual",
      modelTier: "standard",
    };

    it("uses input project ID when not in revision mode", () => {
      const result = getProjectIdForMutation({
        revisionMode: false,
        planAttempt: attempt,
        inputProjectId: "project-input",
      });
      expect(result).toBe("project-input");
    });

    it("forces current attempt project ID in revision mode to prevent redirection", () => {
      const result = getProjectIdForMutation({
        revisionMode: true,
        planAttempt: attempt,
        inputProjectId: "project-input",
      });
      expect(result).toBe("project-attempt");
    });
  });

  describe("getUIStateLabel", () => {
    it("distinguishes basic workbench editor states", () => {
      expect(getUIStateLabel("draft")).toBe("Draft Editor");
      expect(getUIStateLabel("validating")).toBe("Validating Plan...");
      expect(getUIStateLabel("validated")).toBe("Plan Validated");
      expect(getUIStateLabel("validation_failed")).toBe("Validation Failed");
      expect(getUIStateLabel("creating_attempt")).toBe("Creating Review Attempt...");
      expect(getUIStateLabel("review_running")).toBe("Running Drift Review...");
      expect(getUIStateLabel("revising_attempt")).toBe("Revising Attempt...");
      expect(getUIStateLabel("voiding_attempt")).toBe("Voiding Attempt...");
      expect(getUIStateLabel("submitting_approved_attempt")).toBe("Submitting Plan...");
      expect(getUIStateLabel("action_failed")).toBe("Action Failed");
    });

    it("distinguishes attempt ready states by workflow gate state", () => {
      expect(getUIStateLabel("attempt_ready", "review_not_required")).toBe("Review not required");
      expect(getUIStateLabel("attempt_ready", "manual_review_available")).toBe("Manual review available");
      expect(getUIStateLabel("attempt_ready", "automatic_review_pending_or_failed")).toBe("Automatic review required");
      expect(getUIStateLabel("attempt_ready", "external_review_required")).toBe("External review required");
      expect(getUIStateLabel("attempt_ready", "approval_ready")).toBe("Ready for approval");
      expect(getUIStateLabel("attempt_ready", "drift_acknowledgement_required")).toBe("Acknowledgement required");
      expect(getUIStateLabel("attempt_ready", "revision_required")).toBe("Revision required");
      expect(getUIStateLabel("attempt_ready", "drift_review_blocked")).toBe("Review blocked");
      expect(getUIStateLabel("attempt_ready", "ready_for_submission")).toBe("Ready for submission");
      expect(getUIStateLabel("attempt_ready", "submitted")).toBe("Submitted");
      expect(getUIStateLabel("attempt_ready", "voided")).toBe("Voided");
      expect(getUIStateLabel("attempt_ready", "superseded")).toBe("Superseded");
    });
  });
});
