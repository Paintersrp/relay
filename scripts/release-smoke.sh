#!/usr/bin/env bash
set -euo pipefail

validation_tmpdir="$(mktemp -d)"
trap 'rm -rf "$validation_tmpdir"' EXIT

snapshot_worktree() {
  git diff --binary --no-ext-diff HEAD
  while IFS= read -r -d '' path; do
    printf '\0UNTRACKED\0%s\0%s\0' "$path" "$(git hash-object "$path")"
  done < <(git ls-files --others --exclude-standard -z)
}

snapshot_worktree > "$validation_tmpdir/before-generate"

if command -v sqlc &> /dev/null; then
  sqlc generate
elif command -v sqlc.exe &> /dev/null; then
  sqlc.exe generate
else
  echo "sqlc not found"
  exit 1
fi

snapshot_worktree > "$validation_tmpdir/after-generate"
if ! cmp -s "$validation_tmpdir/before-generate" "$validation_tmpdir/after-generate"; then
  echo "sqlc generate changed the working tree"
  git status --short
  exit 1
fi

npm run test:local-scripts

for profile in planner auditor local_operator; do
  RELAY_WORKFLOW_DB_PATH="$validation_tmpdir/$profile.sqlite"     RELAY_WORKFLOW_ARTIFACTS_DIR="$validation_tmpdir/$profile-artifacts"     RELAY_MCP_PROFILE="$profile"     node scripts/local/relay-mcp-stdio.mjs --self-test
done

go run ./cmd/mcp-smoke
npm --prefix apps/web run typecheck
npm --prefix apps/web run test
npm --prefix apps/web run build
go test ./... -count=1
go vet ./...
git diff --check

echo "workflow release smoke passed"
