# Executor Brief

> Derived from canonical JSON. Do not edit this Markdown independently.

## Target

- Repository: `relay`
- Branch: `main`
- Base commit: `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`

## Goal

Compile a representative Execution Spec v2.0 fixture.

## Context

Exercise versioned rendering and evolving path-chain projection.

## Scope

### In Scope

- Exercise every v2.0 metadata form and same-path-chain replay.

### Out of Scope

- Do not mutate a repository.

## Implementation

### Step 1: Render and project versioned substeps.

#### Substep 1.1

##### Files

- `modify` `internal/example/config.go` - Exercise metadata-free v2.0 rendering.

##### Instruction

Establish the first intermediate selector without metadata.

##### Implementation

###### `modify` `internal/example/config.go`

- replace, expected occurrences: 1

  Old text:

  ```text
  const enabled = false
  ```

  New text:

  ```text
  const enabled = true
  ```

##### Completion Criteria

- The metadata-free substep renders without an execution-constraints section.

#### Substep 1.2

##### Files

- `modify` `internal/example/config.go` - Exercise dependency-only rendering and projection.

##### Instruction

Consume the first selector with dependency-only metadata.

##### Execution Constraints

- Depends on: `1.1`

##### Implementation

###### `modify` `internal/example/config.go`

- insert_after, expected occurrences: 1

  Anchor:

  ```text
  const enabled = true
  ```

  Content:

  ```text
  const mode = `strict`
  ```

##### Completion Criteria

- The dependency-only constraint is preserved in authored order.

#### Substep 1.3

##### Files

- `modify` `internal/example/config.go` - Exercise explicit atomic false rendering.

##### Instruction

Consume the next selector with atomic-only metadata.

##### Execution Constraints

- Atomic deterministic preflight: not required

##### Implementation

###### `modify` `internal/example/config.go`

- replace, expected occurrences: 1

  Old text:

  ```text
  const mode = `strict`
  ```

  New text:

  ```text
  const mode = `reviewed`
  ```

##### Completion Criteria

- Explicit atomic false renders as not required.

#### Substep 1.4

##### Files

- `modify` `internal/example/config.go` - Exercise combined explicit and implicit dependency merging.

##### Instruction

Consume the final selector with combined metadata.

##### Execution Constraints

- Depends on: `1.3`
- Atomic deterministic preflight: required

##### Implementation

###### `modify` `internal/example/config.go`

- replace, expected occurrences: 1

  Old text:

  ```text
  const mode = `reviewed`
  ```

  New text:

  ```text
  const mode = `safe`
  ```

##### Completion Criteria

- Combined dependency and atomic metadata render in canonical order.

#### Step Completion Criteria

- Versioned rendering and projection remain deterministic.

## Validation

### Commands

1. Command:

   ```text
   go test ./internal/speccompiler
   ```

   - Expected: The focused compiler tests pass.

### Executor Checks

- Verify v2.0 constraints render only where metadata is authored.

## Completion Criteria

- The v2.0 fixture compiles without errors.
- The rendered brief matches the v2.0 golden file.

## Execution Instructions

- Treat this effective brief as the sole implementation authority for this attempt.
- This canonical brief is full mode; every declared implementation directive remains required.
- Apply the declared implementation exactly, using only necessary source-compatible adaptation that preserves behavior, architecture, scope, and material code shape.
- Preserve unrelated work and avoid unrelated cleanup or refactoring.
- Run the specified validation and report exact results, blockers, or incomplete work.
