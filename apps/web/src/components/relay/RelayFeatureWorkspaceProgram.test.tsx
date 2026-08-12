// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RelayFeatureWorkspaceProgram } from "./RelayFeatureWorkspaceProgram";

const api = vi.hoisted(() => ({ listProgramMembers: vi.fn(), prepareProgramMember: vi.fn(), cancelProgramMember: vi.fn(), createProgramDispatch: vi.fn(), getProgramDispatch: vi.fn(), recordProgramDispatchResult: vi.fn() }));
vi.mock("@/features/relay-programs", () => api);
const member = (id: string, packageId: string) => ({ id, packageId, runId: `run-${id}`, assignmentArtifactId: `artifact-${id}`, repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), state: "prepared", ticketRevisionRowId: 1, outcome: "", resultBranch: "", branchHeadSha: "", blocker: "" });
const dispatch = { id: "dispatch-1", workspaceId: "workspace-1", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), status: "dispatched", laterIntegrationRisks: "", members: [member("member-1", "package-1"), member("member-2", "package-2")] };
function renderProgram() { return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><RelayFeatureWorkspaceProgram workspaceId="workspace-1" expectedVersion={2} /></QueryClientProvider>); }

describe("Feature Workspace Program journey", () => {
  it("renders the standalone workspace journey and invokes preparation, immutable dispatch, and terminal record calls", async () => {
    api.listProgramMembers.mockResolvedValue([member("member-1", "package-1"), member("member-2", "package-2")]); api.createProgramDispatch.mockResolvedValue(dispatch); api.getProgramDispatch.mockResolvedValue({ ...dispatch, status: "reported", members: [{ ...dispatch.members[0], outcome: "done", branch: "main", resultBranch: "program/package-1", branchHeadSha: "b".repeat(40) }, { ...dispatch.members[1], outcome: "blocked", branch: "main", blocker: "conflict" }] }); api.recordProgramDispatchResult.mockResolvedValue(undefined);
    const user = userEvent.setup(); renderProgram();
    expect(await screen.findByRole("heading", { name: "Program workspace" })).toBeInTheDocument();
    expect(await screen.findByText(/Common baseline: relay @ main/)).toBeInTheDocument();
    api.prepareProgramMember.mockResolvedValue(member("member-3", "package-3"));
    await user.type(screen.getByLabelText("Approved package ID"), "package-3"); await user.click(screen.getByRole("button", { name: "Prepare package" }));
    expect(api.prepareProgramMember).toHaveBeenCalledWith("workspace-1", { packageId: "package-3", expectedVersion: 2 });
    await user.click(screen.getByLabelText("Select package-1")); await user.click(screen.getByLabelText("Select package-2")); await user.click(screen.getByRole("button", { name: "Create immutable dispatch" }));
    expect(api.createProgramDispatch).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, memberIds: ["member-1", "member-2"] });
    await screen.findByText("Dispatch dispatch-1");
    await user.type(screen.getByLabelText("Branch for package-1"), "program/package-1"); await user.type(screen.getByLabelText("Branch head SHA for package-1"), "b".repeat(40)); await user.type(screen.getByLabelText("Branch for package-2"), "program/package-2"); await user.type(screen.getByLabelText("Branch head SHA for package-2"), "c".repeat(40)); await user.click(screen.getAllByLabelText("Blocked")[1]); await user.type(screen.getByLabelText("Blocker for package-2"), "conflict"); await user.type(screen.getByLabelText("Later integration risks"), "reconcile downstream"); await user.click(screen.getByRole("button", { name: "Record terminal results" }));
    expect(api.recordProgramDispatchResult).toHaveBeenCalledWith("workspace-1", "dispatch-1", { expectedVersion: 2, members: [{ memberId: "member-1", outcome: "done", branch: "program/package-1", branchHeadSha: "b".repeat(40), blocker: "" }, { memberId: "member-2", outcome: "blocked", branch: "", branchHeadSha: "", blocker: "conflict" }], laterIntegrationRisks: "reconcile downstream" });
    expect(await screen.findByText("Terminal result recorded.")).toBeInTheDocument();
    expect(screen.getByText(/package-1: done · program\/package-1/)).toBeInTheDocument();
    expect(screen.queryByText(/package-2: blocked · main/)).not.toBeInTheDocument();
  });
});
