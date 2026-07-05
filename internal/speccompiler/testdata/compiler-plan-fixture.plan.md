# Plan of Passes

> Derived from canonical JSON. Do not edit this Markdown independently.

## Goal

Render a representative Plan fixture.

## Context

The Plan fixture covers repository targets, dependencies, and pass sections.

## Scope

### In Scope

- Render all Plan sections.

### Out of Scope

- Do not add execution instructions.

## Repository Targets

### `relay`

- Branch: `feat/simplification`
- Planning base commit: `e9e1759821de943643f6ea7f6ae0ceb7db9db951`

## Passes

### Pass 1: Foundation

#### Repository Target

`relay`

#### Goal

Create the foundation.

#### Context

The first pass has no dependencies.

#### Scope

##### In Scope

- Create the bounded foundation.

##### Out of Scope

- Do not implement later integration.

#### Dependencies

None

#### Outcomes

- The foundation exists.

#### Source Targets

- `internal/speccompiler` - Compiler package.

#### Validation Intent

- Prove the package is deterministic.

#### Completion Criteria

- The foundation is complete.

### Pass 2: Integration

#### Repository Target

`relay`

#### Goal

Integrate the foundation.

#### Context

The second pass consumes the committed foundation.

#### Scope

##### In Scope

- Integrate the compiler.

##### Out of Scope

- Do not redesign the compiler.

#### Dependencies

- Pass 1

#### Outcomes

- The compiler is integrated.

#### Source Targets

- `internal/mcp` - Future integration surface.

#### Validation Intent

- Prove integration consumes the compiler.

#### Completion Criteria

- The integration is complete.

## Plan Completion Criteria

- The complete Plan renders deterministically.
