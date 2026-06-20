#!/usr/bin/env bash
set -u

COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown-commit)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${RELAY_VALIDATE_DIR:-data/validation/${COMMIT}/${STAMP}}"
mkdir -p "$OUT_DIR"

SUMMARY="$OUT_DIR/summary.txt"
: > "$SUMMARY"
{
  echo "commit: $COMMIT"
  echo "timestamp_utc: $STAMP"
  echo "out_dir: $OUT_DIR"
  echo
} >> "$SUMMARY"

COMMANDS_JSONL="$OUT_DIR/commands.jsonl"
: > "$COMMANDS_JSONL"

overall=0
step=0

run_step() {
  step=$((step + 1))
  name="$1"
  shift
  log="$OUT_DIR/$(printf '%02d' "$step")-${name}.log"
  echo "== $name ==" | tee -a "$SUMMARY"
  echo "$ $*" | tee "$log"
  "$@" >> "$log" 2>&1
  rc=$?
  echo "exit_code: $rc" >> "$log"
  echo "$name: $rc" | tee -a "$SUMMARY"
  printf '{"step":%d,"name":"%s","log":"%s","exit_code":%d}\n' "$step" "$name" "$log" "$rc" >> "$COMMANDS_JSONL"
  if [ "$rc" -ne 0 ]; then
    overall=1
  fi
}

run_shell_step() {
  step=$((step + 1))
  name="$1"
  command="$2"
  log="$OUT_DIR/$(printf '%02d' "$step")-${name}.log"
  echo "== $name ==" | tee -a "$SUMMARY"
  echo "$ $command" | tee "$log"
  bash -lc "$command" >> "$log" 2>&1
  rc=$?
  echo "exit_code: $rc" >> "$log"
  echo "$name: $rc" | tee -a "$SUMMARY"
  printf '{"step":%d,"name":"%s","log":"%s","exit_code":%d}\n' "$step" "$name" "$log" "$rc" >> "$COMMANDS_JSONL"
  if [ "$rc" -ne 0 ]; then
    overall=1
  fi
}

run_step go-fmt-executor go fmt ./internal/executor
run_step go-test-executor go test ./internal/executor/...
run_step go-test-all go test ./...
run_shell_step web-typecheck "cd apps/web && npm run typecheck"
run_shell_step web-build "cd apps/web && npm run build"

echo >> "$SUMMARY"
echo "overall: $overall" | tee -a "$SUMMARY"
echo "validation output: $OUT_DIR"

LATEST_DIR="$(dirname "$OUT_DIR")"
printf "%s\n" "$OUT_DIR" > "$LATEST_DIR/latest.txt"

exit "$overall"
