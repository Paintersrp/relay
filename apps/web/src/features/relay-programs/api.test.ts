import { afterEach, describe, expect, it, vi } from "vitest";
import { admitIntegrationMergeResult, createProgramDispatch, generateIntegrationAssignment, getIntegrationAssignment, getIntegrationAssignmentResult, getIntegrationFailure, getIntegrationMergeResult, getIntegrationVerification, getProgramDispatch, getProgramHandoff, listProgramMembers, prepareProgramMember, recordProgramDispatchResult, verifyIntegrationAssignment } from "./api";

const member = { ID: "member-1", PackageID: "package-1", RunID: "run-1", AssignmentArtifactID: "artifact-1", RepoTarget: "relay", Branch: "main", BaseCommit: "a".repeat(40), State: "prepared", TicketRevisionRowID: 1, Outcome: "", ResultBranch: "", BranchHeadSHA: "", Blocker: "" };
const dispatch = { ID: "dispatch-1", WorkspaceID: "workspace-1", RepoTarget: "relay", Branch: "main", BaseCommit: "a".repeat(40), Status: "dispatched", LaterIntegrationRisks: "", Members: [{ ...member, ResultBranch: "program/package-1" }, { ...member, ID: "member-2", PackageID: "package-2" }] };
const response = (body: unknown) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
const document = { SchemaVersion: "1.0", Assignment: { AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", RepoTarget: "relay", Branch: "main", BaseCommit: "a".repeat(40) }, Constituents: [{ Sequence: 1, MemberID: "member-1", TicketID: "T-ONE", TicketRevision: 1, AcceptedCommit: "b".repeat(40), PushedBranch: "program/package-1", PackageID: "package-1", RunID: "run-1", ExecutionAssignment: { ArtifactID: "artifact-1", SHA256: "2".repeat(64) }, AuditDecisionID: "audit-1", EligibilityID: "integration-eligibility-1", SharedDesign: { RequiredInvariants: [], ForbiddenBehaviors: [], DependsOn: null }, ValidationCommands: [{ WorkingDirectory: "", Command: "go test ./internal/example", Expected: "Tests pass." }], RequiredEvidence: [{ Kind: "proof_obligation", Obligation: "Prove it." }] }], CombinedValidation: [{ Sequence: 1, ConstituentSequence: 1, WorkingDirectory: "", Command: "go test ./internal/example", Expected: "Tests pass." }], RequiredEvidence: [{ Sequence: 2, ConstituentSequence: 1, Kind: "proof_obligation", Obligation: "Prove it." }] };
const assignment = { AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", WorkspaceID: "workspace-1", RepoTarget: "relay", Branch: "main", BaseCommit: "a".repeat(40), Status: "generated", ContentSHA256: "3".repeat(64), Document: document };
const mergeResult = { ResultID: "integration-merge-result-1", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", IntegratedCommit: "d".repeat(40), PreservationIdentity: "preservation:parents", ConflictResolution: "clean", ConflictEvidence: "", Validations: [{ Sequence: 1, ConstituentSequence: 1, Command: "go test ./internal/example", Expected: "Tests pass.", Status: "passed", Evidence: "verified" }], Evidence: [{ Sequence: 2, ConstituentSequence: 1, Kind: "proof_obligation", Obligation: "Prove it.", Status: "passed", Evidence: "evidence recorded" }] };
const verification = { VerificationID: "integration-verification-1", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", Outcome: "passed", FailureReason: "", Completed: [{ MemberID: "member-1", TicketID: "T-ONE", TicketRevision: 1, Completed: true }] };
afterEach(() => vi.unstubAllGlobals());

describe("program workspace transport", () => {
  it("uses runtime member and immutable dispatch endpoints without client-assembled authority", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response([member])).mockResolvedValueOnce(response(member)).mockResolvedValueOnce(response(dispatch)).mockResolvedValueOnce(response(dispatch)).mockResolvedValueOnce(response({ recorded: true }));
    vi.stubGlobal("fetch", fetch);
    expect((await listProgramMembers("workspace-1"))[0]?.resultBranch).toBe("");
    await prepareProgramMember("workspace-1", { packageId: "package-1", expectedVersion: 2 });
    await createProgramDispatch("workspace-1", { expectedVersion: 2, memberIds: ["member-1", "member-2"] });
    expect((await getProgramDispatch("workspace-1", "dispatch-1")).members[0]?.resultBranch).toBe("program/package-1");
    await recordProgramDispatchResult("workspace-1", "dispatch-1", { expectedVersion: 2, members: [{ memberId: "member-1", outcome: "done", branch: "program/package-1", branchHeadSha: "b".repeat(40), blocker: "" }, { memberId: "member-2", outcome: "blocked", branch: "", branchHeadSha: "", blocker: "conflict" }], laterIntegrationRisks: "reconcile downstream" });
    expect(fetch.mock.calls.map((call) => call[0])).toEqual([
      "http://localhost:18080/api/feature-workspaces/workspace-1/program-members",
      "http://localhost:18080/api/feature-workspaces/workspace-1/program-members",
      "http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches",
      "http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1",
      "http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/result",
    ]);
    expect(JSON.parse(fetch.mock.calls[2]?.[1]?.body as string)).toEqual({ expectedVersion: 2, memberIds: ["member-1", "member-2"] });
    expect(JSON.parse(fetch.mock.calls[4]?.[1]?.body as string)).toEqual({ expectedVersion: 2, members: [{ memberId: "member-1", outcome: "done", branch: "program/package-1", branchHeadSha: "b".repeat(40), blocker: "" }, { memberId: "member-2", outcome: "blocked", branch: "", branchHeadSha: "", blocker: "conflict" }], laterIntegrationRisks: "reconcile downstream" });
  });

  it("reads the canonical backend Program handoff and preserves its exact raw payload", async () => {
    const raw = `{"DispatchID":"dispatch-1","WorkspaceID":"workspace-1","RepoTarget":"relay","Branch":"main","BaseCommit":"${"a".repeat(40)}","Members":[{"Sequence":1,"MemberID":"member-1","TicketID":"T-ONE","TicketRevision":1,"PackageID":"package-1","RunID":"run-1","AssignmentArtifactID":"artifact-1","AssignmentSHA256":"${"2".repeat(64)}","Assignment":{"schema_version":"1.0","run":{"run_id":"run-1"}},"RepoTarget":"relay","Branch":"main","BaseCommit":"${"a".repeat(40)}"}]}`;
    const fetch = vi.fn().mockResolvedValueOnce(new Response(raw, { headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetch);
    const result = await getProgramHandoff("workspace-1", "dispatch-1");
    expect(fetch.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/handoff");
    expect(result.text).toBe(raw);
    expect(result.handoff.members).toHaveLength(1);
    const member = result.handoff.members[0];
    if (!member) throw new Error("missing handoff member");
    expect(member.ticketId).toBe("T-ONE");
    expect(member.ticketRevision).toBe(1);
    expect(member.assignment).toEqual({ schema_version: "1.0", run: { run_id: "run-1" } });
    expect(member.assignmentArtifactId).toBe("artifact-1");
    expect(member.repoTarget).toBe("relay");
    expect(member.baseCommit).toBe("a".repeat(40));
    expect(result.handoff.dispatchId).toBe("dispatch-1");
  });

  it("generates and reads the exact immutable Integration Assignment without reconstructing constituent identities", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response(assignment)).mockResolvedValueOnce(response({ ...assignment, Status: "admitted" }));
    vi.stubGlobal("fetch", fetch);
    const generated = await generateIntegrationAssignment("workspace-1", "dispatch-1", { expectedVersion: 2, memberIds: ["member-1"] });
    expect(fetch.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments");
    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual({ expectedVersion: 2, memberIds: ["member-1"] });
    expect(generated.assignmentId).toBe("integration-assignment-1");
    expect(generated.contentSha256).toBe("3".repeat(64));
    expect(generated.document.assignment.dispatchId).toBe("dispatch-1");
    expect(generated.document.constituents[0]?.ticketId).toBe("T-ONE");
    expect(generated.document.constituents[0]?.acceptedCommit).toBe("b".repeat(40));
    expect(generated.document.constituents[0]?.sharedDesign.dependsOn).toEqual([]);
    expect(generated.document.combinedValidation[0]).toEqual({ sequence: 1, constituentSequence: 1, workingDirectory: "", command: "go test ./internal/example", expected: "Tests pass." });
    expect(generated.document.requiredEvidence[0]).toEqual({ sequence: 2, constituentSequence: 1, kind: "proof_obligation", obligation: "Prove it." });
    const read = await getIntegrationAssignment("workspace-1", "dispatch-1", "integration-assignment-1");
    expect(fetch.mock.calls[1]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1");
    expect(read.status).toBe("admitted");
  });

  it("preserves the exact raw Assignment payload for operator copy", async () => {
    const raw = JSON.stringify(assignment);
    const fetch = vi.fn().mockResolvedValueOnce(new Response(raw, { headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetch);
    const result = await getIntegrationAssignmentResult("workspace-1", "dispatch-1", "integration-assignment-1");
    expect(fetch.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1");
    expect(result.text).toBe(raw);
    expect(result.assignment.assignmentId).toBe("integration-assignment-1");
    expect(result.assignment.document.constituents[0]?.memberId).toBe("member-1");
  });

  it("admits the Merge result with exactly the bound combined validation and required evidence identities", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response(mergeResult));
    vi.stubGlobal("fetch", fetch);
    const result = await admitIntegrationMergeResult("workspace-1", "dispatch-1", "integration-assignment-1", { expectedVersion: 2, integratedCommit: "d".repeat(40), preservationIdentity: "preservation:parents", conflictResolution: "clean", conflictEvidence: "", validations: [{ command: "go test ./internal/example", expected: "Tests pass.", status: "passed", evidence: "verified" }], evidence: [{ kind: "proof_obligation", obligation: "Prove it.", status: "passed", evidence: "evidence recorded" }] });
    expect(fetch.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/merge-results");
    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual({ expectedVersion: 2, integratedCommit: "d".repeat(40), preservationIdentity: "preservation:parents", conflictResolution: "clean", conflictEvidence: "", validations: [{ command: "go test ./internal/example", expected: "Tests pass.", status: "passed", evidence: "verified" }], evidence: [{ kind: "proof_obligation", obligation: "Prove it.", status: "passed", evidence: "evidence recorded" }] });
    expect(result.resultId).toBe("integration-merge-result-1");
    expect(result.validations[0]).toEqual({ sequence: 1, constituentSequence: 1, command: "go test ./internal/example", expected: "Tests pass.", status: "passed", evidence: "verified" });
    expect(result.evidence[0]).toEqual({ sequence: 2, constituentSequence: 1, kind: "proof_obligation", obligation: "Prove it.", status: "passed", evidence: "evidence recorded" });
    const read = vi.fn().mockResolvedValueOnce(response(mergeResult));
    vi.stubGlobal("fetch", read);
    await getIntegrationMergeResult("workspace-1", "dispatch-1", "integration-assignment-1");
    expect(read.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/merge-results");
  });

  it("runs and reads Relay verification and reads immutable failure evidence", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(response(verification)).mockResolvedValueOnce(response(verification));
    vi.stubGlobal("fetch", fetch);
    const verified = await verifyIntegrationAssignment("workspace-1", "dispatch-1", "integration-assignment-1", 2);
    expect(fetch.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/verification");
    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body as string)).toEqual({ expectedVersion: 2 });
    expect(verified.outcome).toBe("passed");
    expect(verified.completed).toEqual([{ memberId: "member-1", ticketId: "T-ONE", ticketRevision: 1, completed: true }]);
    const read = await getIntegrationVerification("workspace-1", "dispatch-1", "integration-assignment-1");
    expect(read.verificationId).toBe("integration-verification-1");
    const failed = vi.fn().mockResolvedValueOnce(response({ VerificationID: "integration-verification-2", AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", FailureReason: "combined validation outcome 1 did not pass" }));
    vi.stubGlobal("fetch", failed);
    const failure = await getIntegrationFailure("workspace-1", "dispatch-1", "integration-assignment-1");
    expect(failed.mock.calls[0]?.[0]).toBe("http://localhost:18080/api/feature-workspaces/workspace-1/program-dispatches/dispatch-1/integration-assignments/integration-assignment-1/failure");
    expect(failure.failureReason).toBe("combined validation outcome 1 did not pass");
  });
});
