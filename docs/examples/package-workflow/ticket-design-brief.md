# Ticket Design Brief

## Selected Ticket

- Ticket: `P2-T2`, revision `3`, feature slug `checkout`.
- The exact revision is approved and selected membership is `checkout-summary`.
- Repository target: `relay`.
- Branch: `main`.
- Base commit: `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`.

## Package Authority and Scope

This Brief is the complete semantic authority for the selected checkout-summary work. It may be packaged without a Deterministic Operations artifact. The package may also carry optional exact operations, but those operations must remain subordinate to this Brief.

Current context: checkout already has item and money types plus neighboring summary behavior. The selected change fills the missing deterministic summary cases without changing adjacent checkout responsibilities.

## Approved Decisions Carried Forward

- Keep the checkout summary calculation in the existing checkout package.
- Preserve the repository's existing public API and error conventions.
- Make the summary deterministic for empty, single-item, and multi-item carts.

## Forbidden Behavior

- Do not change pricing, tax, inventory, or payment behavior.
- Do not add a new external dependency or network call.
- Do not make the summary depend on wall-clock time, map iteration order, or environment-specific state.

## Required Invariants

- Empty carts return the documented empty summary.
- Item order is preserved in the rendered summary.
- Existing callers continue to compile and receive the same error values.
- The implementation must not mutate the input cart.

## Source Contracts

- Use the existing checkout item and money types in the repository.
- Keep formatting and package naming consistent with neighboring checkout code.
- Return errors through the package's existing error-wrapping convention.

## Required Proof Obligations

- Demonstrate the empty-cart, one-item, multiple-item, and error paths with focused tests.
- Demonstrate that the input slice is unchanged after summary generation.
- Demonstrate that the existing package validation command remains green.

## Blockers or Unresolved Source Facts

No blockers remain. If the named checkout types differ from this Brief at the base commit, use the closest existing type contract without broadening scope and record the mismatch in execution evidence.

## Implementation Goal

Implement the checkout summary behavior described above in the existing checkout package. Add focused tests for the required cases, preserve compatibility with current callers, and leave unrelated checkout behavior unchanged.

Completion criteria: the required summary behavior and focused tests are committed, the input remains unchanged, all required validation commands pass, and the diff contains no unrelated or generated-file changes.

## Files to Create or Modify

- Modify the existing checkout summary implementation file containing the summary behavior.
- Add or modify the focused checkout summary test file.
- Do not modify generated files, schemas, migrations, package manifests, or unrelated packages.

## New Types, Functions, Methods, or Fields

Prefer the smallest change to the existing summary function. Add no public type or field unless the current source requires it to express the approved behavior. Keep any helper private and local to the checkout package.

## Control Flow

Validate the cart input using existing package conventions, handle the empty case explicitly, preserve item order while rendering, and return the existing error form for invalid items. Keep the control flow synchronous and deterministic.

## State Mutations

The implementation must be pure with respect to its inputs. It may allocate result values but must not mutate the input cart or write files, databases, network state, or Git state.

## Error Behavior

Return existing validation errors unchanged where possible and wrap new context using the repository's established error convention. Do not silently discard invalid item data.

## Evidence or Artifact Behavior

The committed diff and focused tests must show the required summary behavior. Validation output is execution evidence owned by the attempt; it is not a separate authority artifact.

## Concurrency or Lifecycle Behavior

No new lifecycle, goroutine, lock, cache, retry, or background behavior is required. The change must be safe for concurrent callers under the existing package assumptions.

## Implementation Guidance

Inspect neighboring checkout code and tests before choosing names or formatting. Keep the patch minimal. If optional Deterministic Operations apply an exact mechanical subset, preserve those writes and complete all remaining obligations from this Brief rather than repeating them.

## Test Matrix

- Empty cart.
- One item and multiple items in stable input order.
- Invalid item or money input using the existing error contract.
- Input immutability.
- Existing checkout package tests.

## Validation Commands

- Working directory: .
  Command: go test ./internal/checkout/...
  Expected: all checkout package tests pass, including the focused summary cases.
- Working directory: .
  Command: go test ./internal/...
  Expected: all internal Go tests pass without generated-file or schema changes.

## Explicit Deferrals

Defer pricing, tax, inventory, payment, API, UI, migration, and dependency changes. Defer broad checkout refactoring.

## Non-Decisions

This Brief does not authorize a new endpoint, a new execution authority artifact, a deployment action, or a change to historical Plan/pass records.
