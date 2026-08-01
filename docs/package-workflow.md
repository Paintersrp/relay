# Package-Native Workflow

Relay executes only approved immutable packages. The package is prepared from one exact approved Delivery Ticket revision and its selection; package approval is the only active path that creates a Run.

## Authority Chain

Read the package from the outside in:

1. Retained governing authority, including the applicable requirements and Shared Design records.
2. The exact approved Delivery Ticket revision.
3. The selected Ticket membership that identifies the revision obligations.
4. The complete Ticket Design Brief.
5. Zero or one optional Deterministic Operations artifact.
6. The approved immutable execution package.
7. The package-linked Run and its derived execution assignment and runtime envelope.
8. Runtime execution evidence, including attempt-owned `execution_evidence` and structured validation results when adaptive execution runs.
9. The audit packet and audit decision.
10. Immutable remediation evidence when the decision is `needs_revision`.

The Ticket Design Brief is the complete semantic implementation authority. It must be sufficient for adaptive execution on its own. Deterministic Operations are exact-execution data only: they may perform a bounded set of mechanical writes, but they do not narrow, replace, or reinterpret the Brief.

## Responsibilities

- The Planner authors the complete Ticket Design Brief and may provide zero or one Deterministic Operations artifact.
- Relay validates both artifacts, verifies their package identity, and stores the immutable package.
- The operator reviews and approves the exact immutable package, including its digest and selected membership.
- Package approval creates the package-linked Run. No additional chat confirmation is required.
- Relay derives the execution assignment and runtime envelope from the approved package.
- When operations are present, deterministic execution runs before adaptive execution.
- At most one adaptive Executor attempt may launch for a package-linked Run.
- The Auditor reviews the committed implementation and records `accepted` or `needs_revision`.
- `needs_revision` creates immutable remediation evidence and returns work to a fresh-context Planner revision. The previous Executor transcript is not supplied to that Planner.

## Effective Modes

Every package resolves to exactly one effective mode:

| Mode | Deterministic writes | Evidence supplied to adaptive execution | Adaptive Executor attempt |
| --- | --- | --- | --- |
| `adaptive_no_operations` | None. No operations artifact was supplied. | The complete approved Brief. | Yes, at most once. |
| `adaptive_preflight_failed` | None. Preflight failed before any write. | The complete approved Brief plus the verified preflight failure as source-state evidence. | Yes, at most once. |
| `adaptive_after_partial_application` | Yes. The exact preflighted operations were applied successfully, but coverage is partial. | The complete approved Brief plus exact applied-operation and residual-work evidence. Adaptive execution preserves the applied work and completes the remaining Brief obligations. | Yes, at most once. |
| `deterministic_complete` | Yes. Complete deterministic coverage was applied successfully. | No adaptive execution context is launched. Audit still evaluates the resulting implementation against the complete Brief. | No. |

Preflight failure falls through to adaptive execution and does not automatically create a Ticket revision. A partial deterministic application may be completed adaptively; the adaptive Executor must not repeat, revert, repair, or reinterpret the applied operations. Deterministic-complete execution launches no adaptive Executor attempt.

The submitted Deterministic Operations artifact contains exact operations and declares only `complete` or `partial` coverage. Relay records preflight, application, applied paths, and residual implications in runtime evidence; those outcome records are not extra authored operations. See [the partial-coverage example](examples/package-workflow/deterministic-operations.json).

## Validation Evidence

Required validation commands originate in the approved execution assignment and propagate through execution admission and launch. Under adaptive execution, one adaptive Executor attempt exists and structured command results belong to that attempt's `execution_evidence`. Under deterministic-complete execution, no adaptive attempt or attempt-owned `execution_evidence` artifact exists; assigned validation commands are represented as `not_run` in audit evidence because no adaptive Executor attempt was dispatched. There is no parallel validation-artifact authority family and validation is not an independent Run-level authority.

## Audit Packets

An audit packet binds all of the evidence needed to review one exact result:

- approved package authority;
- exact Ticket revision;
- execution evidence;
- retained governing authority;
- repository evidence; and
- the exact audited commit.

Packet readback rechecks the stored bytes, ownership, digest, current execution evidence, and repository evidence. A stale or superseded packet cannot receive a decision. Prepare a fresh packet against the current evidence and audited commit. Replacing a packet does not rewrite the prior immutable packet.

## Findings And Remediation

Material findings use one of three sources:

- `implementation`: the committed implementation is materially deficient.
- `governing_package`: the approved Brief, Deterministic Operations instructions, or retained governing authority is materially deficient.
- `both`: responsibility is materially shared between the implementation and the approved package.

An `accepted` decision has no material findings. A `needs_revision` decision includes at least one material finding with concise rationale, evidence, and required remediation. Relay stores immutable remediation evidence, then the work returns through a fresh Ticket revision, exact revision approval, selection, complete Brief, optional operations, package preparation, exact package approval, package-linked Run, execution, and audit. The previous Executor transcript is not part of the Planner's fresh context.

## Historical Compatibility

Plans and passes remain readable historical records only. Historical identifiers may still appear in DTOs or review surfaces. `/runs/{runId}/specification` is a retained historical-review transport name. Retained properties such as `executionSpec` or `executorBrief` do not establish active Execution Spec authority for package-linked Runs. Authored Execution Specs, newly authored Executor Briefs, standalone Run creation, and Plan/pass-associated Run creation are retired; no active workflow creates or depends on a newly authored Execution Spec.

## MCP Boundaries

Relay exposes exactly three public role apps:

- Wayfinder: discovery, workspace context, and retained-source investigation.
- Planner: Ticket Design Brief, optional Deterministic Operations, and package authoring.
- Auditor: audit packet review and audit decisions.

Each role app exposes only its compiled role-specific catalog. `/mcp/v1/...` values are internal route identities, not public connector URLs. The `*:all` commands supervise the three registrations and do not create a fourth aggregate MCP endpoint. Manual fallback is exceptional rather than the normal handoff mechanism.

## Operator Walkthrough

1. The Planner publishes a complete Brief and, when useful, one exact operations artifact.
2. Relay validates both inputs and prepares the immutable package.
3. The operator approves the exact package digest and contents.
4. Relay creates the package-linked Run from that approval.
5. Relay determines the effective mode and derives the assignment and runtime envelope.
6. Under adaptive execution, the one adaptive attempt records structured validation results in its `execution_evidence`. Under deterministic-complete execution, no adaptive attempt runs and assigned validation commands appear as `not_run` in audit evidence because no adaptive Executor attempt was dispatched.
7. The operator prepares an audit packet against the exact committed SHA.
8. The Auditor accepts the implementation or records `needs_revision` with material findings.
9. Revision-required work returns to a fresh Planner context through an ordinary Ticket revision.

Failure-path callouts:

- Deterministic preflight failure: no deterministic writes occurred; adaptive execution receives the complete Brief and failure evidence. No revision is created automatically.
- Partial deterministic application: applied writes remain; adaptive execution receives exact applied and residual evidence and completes the Brief without repeating those writes.
- Stale audit packet: do not decide it; prepare a new packet against current evidence and the exact audited commit. The old packet remains immutable.
- Package-attributed finding: attribute the finding to `governing_package`; `needs_revision` creates remediation evidence and returns through a fresh Ticket revision.

See the [complete Brief example](examples/package-workflow/ticket-design-brief.md) and [needs-revision example](examples/package-workflow/audit-needs-revision.json).
