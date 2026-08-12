# Relay Operator Guide

Relay operates on repositories and branches prepared by the operator. It does not perform Git delivery.

## Start Relay

```bash
go run ./cmd/relay
npm run dev:web
```

The web application defaults to `http://localhost:3000` and the API to `http://localhost:18080`.

## Work through a package

1. Create or revise a Delivery Ticket in its Feature Workspace.
2. Approve the exact Ticket revision.
3. Create the selection.
4. Publish the complete Delivery Ticket v2 revision, and add zero or one Deterministic Operations artifact.
5. Prepare the immutable execution package and review its exact digest and contents.
6. Approve that exact package. Approval creates the package-linked Run; there is no extra chat confirmation step.
7. The Orchestrator executes the Run's immutable ExecutionAssignment.
8. Complete execution; for adaptive execution, Relay runs the assigned validations and stores attempt-owned evidence; for deterministic-complete execution, assigned commands appear as `not_run` in audit evidence; then prepare an audit packet against the audited commit.
9. Submit the audit decision with the Auditor role app.

Before package preparation, the exact Delivery Ticket v2 revision must be approved and its Ticket membership must be selected. Deterministic Operations are optional and must be one exact accepted artifact when present. The approved Delivery Ticket v2 is the complete semantic implementation authority; operations never replace it. Package approval is the only active Run-creation path. Ticket Design Brief authoring, authored Execution Spec submission, and standalone or Plan/pass-associated Run creation are retired.

Plans and passes are read-only historical records. Projects retain Feature Workspaces, repositories, Tickets, and historical records; they are not active Plan containers.

## Execute

Select an adapter and model on the Run execution surface. The effective mode is one of:

- `adaptive_no_operations`: the Orchestrator executes the immutable ExecutionAssignment containing the approved Delivery Ticket v2.
- `adaptive_preflight_failed`: no deterministic writes occur; the Orchestrator receives the assignment and the verified failure result.
- `adaptive_after_partial_application`: the Orchestrator receives the assignment and exact applied/residual evidence.
- `deterministic_complete`: no adaptive Orchestrator dispatch is launched.

There is at most one adaptive Executor attempt. Package approval derives the immutable ExecutionAssignment for the package-linked Run. Deterministic operations run before adaptive execution when present. For adaptive modes, required validation commands originate in that assignment, flow through admission and launch, and are retained as structured `execution_evidence` owned by the adaptive attempt. In deterministic-complete execution, no adaptive attempt is launched; assigned validation commands appear as `not_run` in audit evidence because no adaptive Executor attempt was dispatched. Validation is not a separate Run-level authority.

## Audit and remediation

Prepare an audit packet using the full audited commit SHA. Packet readback verifies exact stored bytes, ownership, digest, current execution evidence, and repository evidence. A stale or superseded packet cannot receive a decision.

Use the Auditor role app to record `accepted` or `needs_revision`, including material findings attributed to `implementation`, `governing_package`, or `both`. A revision decision creates immutable remediation evidence for a fresh-context Planner revision. The previous Executor transcript is excluded. Remediation follows the ordinary Ticket revision, approval, package, and Run lifecycle.

If the packet is stale or superseded, do not decide it. Prepare a fresh packet against the current execution evidence and exact audited commit; packet replacement does not rewrite the prior immutable packet.

See the [package-native workflow](package-workflow.md) for the authority chain, mode evidence, freshness rules, and [schema-grounded examples](examples/package-workflow/).

## Program integration

The Feature Workspace program section runs the visible Program dispatch and integration journey. Prepare one eligible approved current package at a time, select at least two members of a common repository, branch, and base commit, and create the immutable dispatch. Copy the Program handoff for the external Program Orchestrator, then record each member's terminal result (`done` with its pushed branch and 40-hex branch head SHA, or `blocked` with a reason).

An accepted Program-bound isolated audit is eligibility only: it never completes the Ticket revision by itself. Once the dispatch is reported, the section lists the accepted constituents eligible for integration, visibly distinct from integrated completion. Select any nonempty valid subset — Relay re-verifies every exact recorded fact and blocks the whole Assignment when any selected or omitted Ticket dependency or Shared Design constraint would require a missing member.

Generate the Integration Assignment, copy its exact payload, and run the external Merge against it. Record the returned Merge result exactly: the outcome list must be precisely the Assignment's combined validation commands and required evidence (same count, same order, identical identities), each with a passed/failed status and outcome evidence. Then run Relay verification. Relay never reruns the combined validation; it re-verifies the bound authority and recorded evidence, and only a passing verification records the ordinary completed outcome of each bound constituent whose Ticket revision is still current.

If verification fails, the failed Assignment is immutable evidence and is never patched or reused: retry by generating a fresh Assignment from the recorded facts. You see the ordinary completed outcome only after Relay verification passes.

## Local ChatGPT registrations

Register Wayfinder, Planner, and Auditor independently:

```bash
npm run chatgpt-mcp:init:all
npm run chatgpt-mcp:doctor:all
npm run chatgpt-mcp:start:all
npm run chatgpt-mcp:status:all
npm run chatgpt-mcp:stop:all
```

These commands supervise the three role registrations; they do not provide another MCP URL. See [chatgpt-mcp-local.md](chatgpt-mcp-local.md).
