# Executor Brief

> Derived from canonical JSON. Do not edit this Markdown independently.

## Target

- Repository: `relay`
- Branch: `feat/simplification`
- Base commit: `e9e1759821de943643f6ea7f6ae0ceb7db9db951`

## Goal

Compile a representative Execution Spec fixture.

## Context

Representative context with `inline code`.

```go
package example
```

## Scope

### In Scope

- Exercise every supported file operation.

### Out of Scope

- Do not perform repository mutation.

## Implementation

### Step 1: Render exact implementation directives.

#### Substep 1.1

##### Files

- `modify` `internal/example/config.go` - Exercise modify rendering.
- `create` `internal/example/new.go` - Exercise create rendering.
- `delete` `internal/example/old.go` - Exercise delete rendering.
- `rename` `internal/example/name.go` -> `internal/example/new_name.go` - Exercise rename rendering.

##### Instruction

Render the declared file operations in source order.

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

- insert_after, expected occurrences: 1

  Anchor:

  ```text
  const enabled = true
  ```

  Content:

  ```text
  const mode = `strict`
  ```

- remove, expected occurrences: 1

  Old text:

  ```text
  const obsolete = true
  ```

###### `create` `internal/example/new.go`

Content:

```text
package example

func Enabled() bool {
	return true
}
```

###### `delete` `internal/example/old.go`

Delete file: true

###### `rename` `internal/example/name.go` -> `internal/example/new_name.go`

Preserve content: true

##### Completion Criteria

- Every file operation is rendered exactly once.

#### Step Completion Criteria

- The complete step renders deterministically.

## Validation

### Commands

1. Command:

   ```text
   go test ./internal/speccompiler
   ```

   - Expected: The focused compiler tests pass.

### Executor Checks

- Verify the rendered brief ends with exactly one newline.

## Completion Criteria

- The fixture compiles without errors.
- The rendered brief matches the golden file.

## Execution Instructions

- Treat this effective brief as the sole implementation authority for this attempt.
- This canonical brief is full mode; every declared implementation directive remains required.
- Apply the declared implementation exactly, using only necessary source-compatible adaptation that preserves behavior, architecture, scope, and material code shape.
- Preserve unrelated work and avoid unrelated cleanup or refactoring.
- Run the specified validation and report exact results, blockers, or incomplete work.
