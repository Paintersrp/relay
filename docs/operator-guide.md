# Relay Operator Guide

Relay operates on repositories and branches prepared by the operator. It does not perform Git delivery.

## Start Relay

```bash
go run ./cmd/relay
npm run dev:web
```

The web application defaults to `http://localhost:3000` and the API to `http://localhost:8080`.

## Work through a package

1. Create or revise a Delivery Ticket in its Feature Workspace.
2. Approve the exact Ticket revision.
3. Create the selection.
4. Complete the Ticket Design Brief, and add zero or one Deterministic Operations artifact.
5. Prepare and approve the execution package.
6. Use the package-linked Run created by approval.
7. Execute required validation and prepare an audit packet against the audited commit.
8. Submit the audit decision with the Auditor role app.

The Ticket Design Brief is the complete semantic implementation authority. Deterministic Operations are optional exact-execution data. Package approval is the only active Run-creation path. Authored Execution Spec submission and standalone or Plan/pass-associated Run creation are retired.

Plans and passes are read-only historical records. Projects retain Feature Workspaces, repositories, Tickets, and historical records; they are not active Plan containers.

## Execute

Select an adapter and model on the Run execution surface. The effective mode is one of:

- `adaptive_no_operations`: one adaptive Executor attempt receives the complete Brief.
- `adaptive_preflight_failed`: no deterministic writes occur; the attempt receives the complete Brief and verified failure result.
- `adaptive_after_partial_application`: the attempt receives the complete Brief and exact applied/residual evidence.
- `deterministic_complete`: no adaptive Executor attempt is launched.

There is at most one adaptive Executor attempt. Required validation commands originate in the approved assignment, flow into admission and launch, and are retained as structured `execution_evidence` owned by that attempt.

## Audit and remediation

Prepare an audit packet using the full audited commit SHA. Packet readback verifies exact stored bytes, ownership, digest, current execution evidence, and repository evidence. A stale or superseded packet cannot receive a decision.

Use the Auditor role app to record `accepted` or `needs_revision`, including material findings attributed to `implementation`, `governing_package`, or `both`. A revision decision creates immutable remediation evidence for a fresh-context Planner pass. The previous Executor transcript is excluded. Remediation follows the ordinary Ticket revision, approval, package, and Run lifecycle.

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
