import { describe, expect, it } from "vitest";

import { normalizeAuditStatus } from "./api";

describe("normalizeAuditStatus", () => {
  it("normalizes artifact-backed local audit status payloads", () => {
    const result = normalizeAuditStatus({
      runId: 42,
      runStatus: "audit_ready",
      auditState: "ready",
      canGenerateAudit: false,
      canSubmitDecision: true,
      canApprove: true,
      canRequestRevision: true,
      canCloseRun: false,
      evidenceManifestArtifact: {
        id: "7",
        label: "Audit Evidence Manifest (JSON)",
        path: "/api/runs/42/artifacts/audit_evidence_manifest_json",
        kind: "audit",
      },
      blockers: ["need one more review"],
      warnings: ["preview truncated"],
      revisionRequirements: ["clarify scope"],
      localOnly: true,
    });

    expect(result.runId).toBe("42");
    expect(result.auditState).toBe("ready");
    expect(result.canSubmitDecision).toBe(true);
    expect(result.evidenceManifestArtifact?.status).toBe("ready");
    expect(result.evidenceManifestArtifact?.filename).toBe(
      "audit_evidence_manifest_json",
    );
    expect(result.blockers).toEqual(["need one more review"]);
    expect(result.localOnly).toBe(true);
  });

  it("fills safe defaults when optional fields are missing", () => {
    const result = normalizeAuditStatus({});

    expect(result.auditState).toBe("not_ready");
    expect(result.blockers).toEqual([]);
    expect(result.warnings).toEqual([]);
    expect(result.revisionRequirements).toEqual([]);
    expect(result.localOnly).toBe(true);
  });
});
