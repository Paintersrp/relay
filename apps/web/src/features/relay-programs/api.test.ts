import { afterEach, describe, expect, it, vi } from "vitest";
import { createProgramDispatch, getProgramDispatch, getProgramHandoff, listProgramMembers, prepareProgramMember, recordProgramDispatchResult } from "./api";

const member = { ID: "member-1", PackageID: "package-1", RunID: "run-1", AssignmentArtifactID: "artifact-1", RepoTarget: "relay", Branch: "main", BaseCommit: "a".repeat(40), State: "prepared", TicketRevisionRowID: 1, Outcome: "", ResultBranch: "", BranchHeadSHA: "", Blocker: "" };
const dispatch = { ID: "dispatch-1", WorkspaceID: "workspace-1", RepoTarget: "relay", Branch: "main", BaseCommit: "a".repeat(40), Status: "dispatched", LaterIntegrationRisks: "", Members: [{ ...member, ResultBranch: "program/package-1" }, { ...member, ID: "member-2", PackageID: "package-2" }] };
const response = (body: unknown) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
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
});
