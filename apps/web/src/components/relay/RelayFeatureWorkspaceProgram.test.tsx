// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RelayFeatureWorkspaceProgram } from "./RelayFeatureWorkspaceProgram";

const api = vi.hoisted(() => ({ listProgramMembers: vi.fn(), prepareProgramMember: vi.fn(), cancelProgramMember: vi.fn(), createProgramDispatch: vi.fn(), getProgramDispatch: vi.fn(), getProgramHandoff: vi.fn(), recordProgramDispatchResult: vi.fn(), generateIntegrationAssignment: vi.fn(), getIntegrationAssignment: vi.fn(), getIntegrationAssignmentResult: vi.fn(), admitIntegrationMergeResult: vi.fn(), getIntegrationMergeResult: vi.fn(), verifyIntegrationAssignment: vi.fn(), getIntegrationVerification: vi.fn(), getIntegrationFailure: vi.fn() }));
vi.mock("@/features/relay-programs", () => api);
const member = (id: string, packageId: string) => ({ id, packageId, runId: `run-${id}`, assignmentArtifactId: `artifact-${id}`, repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), state: "prepared", ticketRevisionRowId: 1, outcome: "", resultBranch: "", branchHeadSha: "", blocker: "" });
const dispatch = { id: "dispatch-1", workspaceId: "workspace-1", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), status: "dispatched", laterIntegrationRisks: "", members: [member("member-1", "package-1"), member("member-2", "package-2")] };
const doneMember = (id: string, packageId: string, resultBranch: string, branchHeadSha: string) => ({ ...member(id, packageId), state: "dispatched", outcome: "done", resultBranch, branchHeadSha });
const reportedDispatch = { ...dispatch, status: "reported", laterIntegrationRisks: "reconcile downstream", members: [doneMember("member-1", "package-1", "program/package-1", "b".repeat(40)), doneMember("member-2", "package-2", "program/package-2", "c".repeat(40))] };
const document = (assignmentId: string) => ({ schemaVersion: "1.0", assignment: { assignmentId, dispatchId: "dispatch-1", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40) }, constituents: [{ sequence: 1, memberId: "member-1", ticketId: "T-ONE", ticketRevision: 1, acceptedCommit: "b".repeat(40), pushedBranch: "program/package-1", packageId: "package-1", runId: "run-1", executionAssignment: { artifactId: "artifact-1", sha256: "2".repeat(64) }, auditDecisionId: "audit-1", eligibilityId: "integration-eligibility-1", sharedDesign: { requiredInvariants: [], forbiddenBehaviors: [], dependsOn: [] }, validationCommands: [{ workingDirectory: "", command: "go test ./internal/example", expected: "Tests pass." }], requiredEvidence: [{ kind: "proof_obligation", obligation: "Prove it." }] }], combinedValidation: [{ sequence: 1, constituentSequence: 1, workingDirectory: "", command: "go test ./internal/example", expected: "Tests pass." }], requiredEvidence: [{ sequence: 2, constituentSequence: 1, kind: "proof_obligation", obligation: "Prove it." }] });
const assignment = (assignmentId: string, status: "generated" | "admitted" | "verified" | "failed") => ({ assignmentId, dispatchId: "dispatch-1", workspaceId: "workspace-1", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), status, contentSha256: "3".repeat(64), document: document(assignmentId) });
const mergeResult = { resultId: "integration-merge-result-1", assignmentId: "integration-assignment-1", dispatchId: "dispatch-1", integratedCommit: "d".repeat(40), preservationIdentity: "preservation:parents", conflictEvidence: "", validations: [{ sequence: 1, constituentSequence: 1, command: "go test ./internal/example", expected: "Tests pass.", status: "passed", evidence: "verified" }], evidence: [{ sequence: 2, constituentSequence: 1, kind: "proof_obligation", obligation: "Prove it.", status: "passed", evidence: "evidence recorded" }] };
const verification = { verificationId: "integration-verification-1", assignmentId: "integration-assignment-1", dispatchId: "dispatch-1", outcome: "passed", failureReason: "", completed: [{ memberId: "member-1", ticketId: "T-ONE", ticketRevision: 1, completed: true }] };
function renderProgram() { return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><RelayFeatureWorkspaceProgram workspaceId="workspace-1" expectedVersion={2} /></QueryClientProvider>); }
async function reachReportedDispatch(user: ReturnType<typeof userEvent.setup>) {
  api.createProgramDispatch.mockResolvedValue(dispatch);
  api.getProgramDispatch.mockResolvedValue(reportedDispatch);
  api.recordProgramDispatchResult.mockResolvedValue(undefined);
  await user.click(await screen.findByLabelText("Select package-1")); await user.click(screen.getByLabelText("Select package-2")); await user.click(screen.getByRole("button", { name: "Create immutable dispatch" }));
  fireEvent.change(screen.getByLabelText("Branch for package-1"), { target: { value: "program/package-1" } }); fireEvent.change(screen.getByLabelText("Branch head SHA for package-1"), { target: { value: "b".repeat(40) } }); fireEvent.change(screen.getByLabelText("Branch for package-2"), { target: { value: "program/package-2" } }); fireEvent.change(screen.getByLabelText("Branch head SHA for package-2"), { target: { value: "c".repeat(40) } });
  await user.click(screen.getByRole("button", { name: "Record terminal results" }));
  await screen.findByText("Terminal result recorded.");
}

describe("Feature Workspace Program journey", () => {
  it("renders the standalone workspace journey and invokes preparation, immutable dispatch, and terminal record calls", async () => {
    api.listProgramMembers.mockResolvedValue([member("member-1", "package-1"), member("member-2", "package-2")]); api.createProgramDispatch.mockResolvedValue(dispatch); api.getProgramDispatch.mockResolvedValue({ ...dispatch, status: "reported", members: [{ ...dispatch.members[0], outcome: "done", branch: "main", resultBranch: "program/package-1", branchHeadSha: "b".repeat(40) }, { ...dispatch.members[1], outcome: "blocked", branch: "main", blocker: "conflict" }] }); api.recordProgramDispatchResult.mockResolvedValue(undefined);
    const user = userEvent.setup({ delay: null }); renderProgram();
    expect(await screen.findByRole("heading", { name: "Program workspace" })).toBeInTheDocument();
    expect(await screen.findByText(/Common baseline: relay @ main/)).toBeInTheDocument();
    api.prepareProgramMember.mockResolvedValue(member("member-3", "package-3"));
    fireEvent.change(screen.getByLabelText("Approved package ID"), { target: { value: "package-3" } }); await user.click(screen.getByRole("button", { name: "Prepare package" }));
    expect(api.prepareProgramMember).toHaveBeenCalledWith("workspace-1", { packageId: "package-3", expectedVersion: 2 });
    await user.click(screen.getByLabelText("Select package-1")); await user.click(screen.getByLabelText("Select package-2")); await user.click(screen.getByRole("button", { name: "Create immutable dispatch" }));
    expect(api.createProgramDispatch).toHaveBeenCalledWith("workspace-1", { expectedVersion: 2, memberIds: ["member-1", "member-2"] });
    await screen.findByText("Dispatch dispatch-1");
    fireEvent.change(screen.getByLabelText("Branch for package-1"), { target: { value: "program/package-1" } }); fireEvent.change(screen.getByLabelText("Branch head SHA for package-1"), { target: { value: "b".repeat(40) } }); fireEvent.change(screen.getByLabelText("Branch for package-2"), { target: { value: "program/package-2" } }); fireEvent.change(screen.getByLabelText("Branch head SHA for package-2"), { target: { value: "c".repeat(40) } }); await user.click(screen.getAllByLabelText("Blocked")[1]); fireEvent.change(screen.getByLabelText("Blocker for package-2"), { target: { value: "conflict" } }); fireEvent.change(screen.getByLabelText("Later integration risks"), { target: { value: "reconcile downstream" } }); await user.click(screen.getByRole("button", { name: "Record terminal results" }));
    expect(api.recordProgramDispatchResult).toHaveBeenCalledWith("workspace-1", "dispatch-1", { expectedVersion: 2, members: [{ memberId: "member-1", outcome: "done", branch: "program/package-1", branchHeadSha: "b".repeat(40), blocker: "" }, { memberId: "member-2", outcome: "blocked", branch: "", branchHeadSha: "", blocker: "conflict" }], laterIntegrationRisks: "reconcile downstream" });
    expect(await screen.findByText("Terminal result recorded.")).toBeInTheDocument();
    expect(screen.getByText(/package-1: done · program\/package-1/)).toBeInTheDocument();
    expect(screen.queryByText(/package-2: blocked · main/)).not.toBeInTheDocument();
  });

  it("copies the exact backend-provided Program handoff instead of reconstructing it in the browser", async () => {
    const raw = `{"DispatchID":"dispatch-1","WorkspaceID":"workspace-1","RepoTarget":"relay","Branch":"main","BaseCommit":"${"a".repeat(40)}","Members":[{"Sequence":1,"MemberID":"member-1","TicketID":"T-ONE","TicketRevision":1,"PackageID":"package-1","RunID":"run-1","AssignmentArtifactID":"artifact-1","AssignmentSHA256":"${"2".repeat(64)}","Assignment":{"schema_version":"1.0","run":{"run_id":"run-1"}},"RepoTarget":"relay","Branch":"main","BaseCommit":"${"a".repeat(40)}"}]}`;
    api.listProgramMembers.mockResolvedValue([member("member-1", "package-1"), member("member-2", "package-2")]);
    api.createProgramDispatch.mockResolvedValue(dispatch);
    api.getProgramHandoff.mockResolvedValue({ text: raw, handoff: { dispatchId: "dispatch-1", workspaceId: "workspace-1", repoTarget: "relay", branch: "main", baseCommit: "a".repeat(40), members: [] } });
    const writeText = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup({ delay: null });
    // user-event installs its own clipboard stub during setup(); install the
    // assertion mock afterwards so the component copies via exactly this stub.
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    renderProgram();
    await user.click(await screen.findByLabelText("Select package-1")); await user.click(screen.getByLabelText("Select package-2")); await user.click(screen.getByRole("button", { name: "Create immutable dispatch" }));
    const copyButton = await screen.findByRole("button", { name: "Copy Program handoff" });
    expect(screen.queryByRole("button", { name: "Copy dispatch ID" })).not.toBeInTheDocument();
    await user.click(copyButton);
    expect(api.getProgramHandoff).toHaveBeenCalledWith("workspace-1", "dispatch-1");
    expect(await screen.findByText("Complete Program Orchestrator handoff copied.")).toBeInTheDocument();
    expect(writeText).toHaveBeenCalledWith(raw);
  });

  it("walks the visible integration sequence: eligible subset, exact Assignment copy, Merge result, Relay verification completion", async () => {
    api.listProgramMembers.mockResolvedValue([member("member-1", "package-1"), member("member-2", "package-2")]);
    api.generateIntegrationAssignment.mockResolvedValue(assignment("integration-assignment-1", "generated"));
    api.getIntegrationAssignmentResult.mockResolvedValue({ assignment: assignment("integration-assignment-1", "generated"), text: JSON.stringify(assignment("integration-assignment-1", "generated")) });
    api.admitIntegrationMergeResult.mockResolvedValue(mergeResult);
    api.verifyIntegrationAssignment.mockResolvedValue(verification);
    const user = userEvent.setup({ delay: null }); renderProgram();
    await reachReportedDispatch(user);
    expect(screen.getByText("Integration Assignments")).toBeInTheDocument();
    // Isolated accepted eligibility is visibly distinct from integration completion.
    expect(screen.getByText("Accepted constituents eligible for integration")).toBeInTheDocument();
    expect(screen.queryByText("Integrated completed")).not.toBeInTheDocument();
    await user.click(screen.getByLabelText("Integrate package-1"));
    await user.click(screen.getByRole("button", { name: "Generate Integration Assignment" }));
    expect(api.generateIntegrationAssignment).toHaveBeenCalledWith("workspace-1", "dispatch-1", { expectedVersion: 2, memberIds: ["member-1"] });
    await screen.findByText("Assignment integration-assignment-1");
    expect(screen.getByText(/T-ONE rev 1 · accepted/)).toBeInTheDocument();
    expect(screen.getByText(/go test \.\/internal\/example — expected: Tests pass\./)).toBeInTheDocument();
    // The copied payload is the exact backend Assignment bytes; the browser never reassembles it.
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    await user.click(screen.getByRole("button", { name: "Copy exact Assignment" }));
    expect(api.getIntegrationAssignmentResult).toHaveBeenCalledWith("workspace-1", "dispatch-1", "integration-assignment-1");
    expect(await screen.findByText("Exact Assignment copied.")).toBeInTheDocument();
    expect(writeText).toHaveBeenCalledWith(JSON.stringify(assignment("integration-assignment-1", "generated")));
    // The Merge result records exactly the document-bound combined validation and required evidence.
    fireEvent.change(screen.getByLabelText("Integrated commit SHA"), { target: { value: "d".repeat(40) } });
    fireEvent.change(screen.getByLabelText("Preservation identity"), { target: { value: "preservation:parents" } });
    fireEvent.change(screen.getByLabelText("Evidence for validation 1"), { target: { value: "verified" } });
    fireEvent.change(screen.getByLabelText("Evidence for evidence 2"), { target: { value: "evidence recorded" } });
    await user.click(screen.getByRole("button", { name: "Record Merge result" }));
    expect(api.admitIntegrationMergeResult).toHaveBeenCalledWith("workspace-1", "dispatch-1", "integration-assignment-1", { expectedVersion: 2, integratedCommit: "d".repeat(40), preservationIdentity: "preservation:parents", conflictEvidence: "", validations: [{ command: "go test ./internal/example", expected: "Tests pass.", status: "passed", evidence: "verified" }], evidence: [{ kind: "proof_obligation", obligation: "Prove it.", status: "passed", evidence: "evidence recorded" }] });
    expect(await screen.findByText(/Merge result recorded/)).toBeInTheDocument();
    // Relay verification of the admitted result is the only completion basis.
    await user.click(screen.getByRole("button", { name: "Run Relay verification" }));
    expect(api.verifyIntegrationAssignment).toHaveBeenCalledWith("workspace-1", "dispatch-1", "integration-assignment-1", 2);
    expect(await screen.findByText("Relay verification passed.")).toBeInTheDocument();
    expect(screen.getByText("Integrated completed")).toBeInTheDocument();
    expect(screen.getByText(/T-ONE rev 1 · member-1 · program\/package-1 — completed/)).toBeInTheDocument();
    // The verified constituent is no longer an eligible candidate; member-2 remains one.
    expect(screen.queryByLabelText("Integrate package-1")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Integrate package-2")).toBeInTheDocument();
  });

  it("shows Relay verification failure evidence and retries only with a fresh Assignment", async () => {
    api.listProgramMembers.mockResolvedValue([member("member-1", "package-1"), member("member-2", "package-2")]);
    api.generateIntegrationAssignment.mockResolvedValue(assignment("integration-assignment-1", "generated"));
    api.admitIntegrationMergeResult.mockResolvedValue(mergeResult);
    api.verifyIntegrationAssignment.mockResolvedValue({ ...verification, verificationId: "integration-verification-2", outcome: "failed", failureReason: "combined validation outcome 1 did not pass", completed: [] });
    api.getIntegrationFailure.mockResolvedValue({ verificationId: "integration-verification-2", assignmentId: "integration-assignment-1", dispatchId: "dispatch-1", failureReason: "combined validation outcome 1 did not pass" });
    const user = userEvent.setup({ delay: null }); renderProgram();
    await reachReportedDispatch(user);
    await user.click(screen.getByLabelText("Integrate package-1"));
    await user.click(screen.getByRole("button", { name: "Generate Integration Assignment" }));
    await screen.findByText("Assignment integration-assignment-1");
    fireEvent.change(screen.getByLabelText("Integrated commit SHA"), { target: { value: "d".repeat(40) } });
    fireEvent.change(screen.getByLabelText("Preservation identity"), { target: { value: "preservation:parents" } });
    fireEvent.change(screen.getByLabelText("Evidence for validation 1"), { target: { value: "verified" } });
    fireEvent.change(screen.getByLabelText("Evidence for evidence 2"), { target: { value: "evidence recorded" } });
    await user.click(screen.getByRole("button", { name: "Record Merge result" }));
    await screen.findByText(/Merge result recorded/);
    await user.click(screen.getByRole("button", { name: "Run Relay verification" }));
    expect(await screen.findByText("Relay verification failed.")).toBeInTheDocument();
    expect(screen.getByText(/combined validation outcome 1 did not pass/)).toBeInTheDocument();
    // Immutable failure evidence is readable without any rerun.
    await user.click(screen.getByRole("button", { name: "Read failure evidence" }));
    expect(api.getIntegrationFailure).toHaveBeenCalledWith("workspace-1", "dispatch-1", "integration-assignment-1");
    // Retry only through a fresh Assignment; the failed one is never patched or reused.
    api.generateIntegrationAssignment.mockResolvedValue(assignment("integration-assignment-2", "generated"));
    await user.click(screen.getByRole("button", { name: "Generate fresh Assignment" }));
    expect(api.generateIntegrationAssignment).toHaveBeenCalledWith("workspace-1", "dispatch-1", { expectedVersion: 2, memberIds: ["member-1"] });
    expect(await screen.findByText("Assignment integration-assignment-2")).toBeInTheDocument();
    const failedCard = screen.getByText("Assignment integration-assignment-1").closest("[data-assignment-id]") as HTMLElement | null;
    expect(failedCard).not.toBeNull();
    expect(within(failedCard!).queryByRole("button", { name: "Record Merge result" })).not.toBeInTheDocument();
    expect(within(failedCard!).queryByRole("button", { name: "Run Relay verification" })).not.toBeInTheDocument();
  });
});
