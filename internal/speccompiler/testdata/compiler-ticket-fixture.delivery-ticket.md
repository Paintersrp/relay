# Delivery Ticket

> Derived from canonical JSON. Do not edit this Markdown independently.

## Identity

- Ticket: `P2-T3`
- Revision: 1

## Target

- Repository: `Paintersrp/relay`
- Branch: `main`
- Base commit: `bea7c45781bc3b6b306c6f9cf75e5cbe58ba2690`

## Goal

Compile and render a Delivery Ticket.

## Context

The fixture covers the canonical Delivery Ticket sections.

## Scope

### In Scope

- Validate the ticket structure.

### Out of Scope

- Mutate workflow state.

## Dependencies

- `P2-T1` revision 1
- `P2-T2` revision 1

## Implementation Obligations

- `internal/speccompiler/compiler.go` - Compile canonical Delivery Ticket JSON.
- `internal/speccompiler/render.go` - Render deterministic Delivery Ticket Markdown.

## Validation Intent

- Focused compiler tests pass.

## Transition Applicability

not_required

## Replacement

None

## Cancellation

None

## Completion Criteria

- Valid ticket output is byte-identical across repeated compilation.
