#!/usr/bin/env bash
set -u

COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown-commit)"
FULL_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown-commit)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
ISO_STAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

OUT_DIR="${RELAY_VALIDATE_DIR:-data/validation/${COMMIT}/${STAMP}}"
TRACKED_DIR="${RELAY_VALIDATE_TRACKED_DIR:-handoffs/validation}"
TRACKED_JSON="${TRACKED_DIR}/latest.validation-report.json"
TRACKED_MD="${TRACKED_DIR}/latest.validation-summary.md"

mkdir -p "$OUT_DIR" "$TRACKED_DIR"

SUMMARY="$OUT_DIR/summary.txt"
: > "$SUMMARY"
{
  echo "commit: $COMMIT"
  echo "full_commit: $FULL_COMMIT"
  echo "timestamp_utc: $STAMP"
  echo "out_dir: $OUT_DIR"
  echo "tracked_report: $TRACKED_JSON"
  echo "tracked_summary: $TRACKED_MD"
  echo
} >> "$SUMMARY"

COMMANDS_JSONL="$OUT_DIR/commands.jsonl"
: > "$COMMANDS_JSONL"

overall=0
step=0

write_command_record() {
  local record_step="$1"
  local record_name="$2"
  local record_command="$3"
  local record_log="$4"
  local record_rc="$5"

  node - "$COMMANDS_JSONL" "$record_step" "$record_name" "$record_command" "$record_log" "$record_rc" <<'NODE'
const fs = require('fs')
const [path, step, name, command, log, rc] = process.argv.slice(2)
fs.appendFileSync(
  path,
  JSON.stringify({
    step: Number(step),
    name,
    command,
    log,
    exit_code: Number(rc),
  }) + '\n',
)
NODE
}

run_step() {
  step=$((step + 1))
  name="$1"
  shift
  command="$*"
  log="$OUT_DIR/$(printf '%02d' "$step")-${name}.log"
  echo "== $name ==" | tee -a "$SUMMARY"
  echo "$ $command" | tee "$log"
  "$@" >> "$log" 2>&1
  rc=$?
  echo "exit_code: $rc" >> "$log"
  echo "$name: $rc" | tee -a "$SUMMARY"
  write_command_record "$step" "$name" "$command" "$log" "$rc"
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
  write_command_record "$step" "$name" "$command" "$log" "$rc"
  if [ "$rc" -ne 0 ]; then
    overall=1
  fi
}

write_tracked_reports() {
  node - "$TRACKED_JSON" "$TRACKED_MD" "$COMMANDS_JSONL" "$COMMIT" "$FULL_COMMIT" "$ISO_STAMP" "$OUT_DIR" "$overall" <<'NODE'
const fs = require('fs')
const [jsonPath, mdPath, commandsPath, commitShort, commitSha, createdAt, outDir, overallRaw] = process.argv.slice(2)

function redact(value) {
  return String(value ?? '')
    .replace(/(Authorization:\s*Bearer\s+)[^\s]+/gi, '$1[REDACTED_TOKEN]')
    .replace(/([?&](?:token|access_token|auth|signature|X-Amz-Signature)=)[^&\s]+/gi, '$1[REDACTED_TOKEN]')
    .replace(/\b[A-Za-z0-9+/]{48,}={0,2}\b/g, '[REDACTED_SECRET]')
    .replace(/([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASS|API_KEY|ACCESS_KEY|PRIVATE_KEY|AUTH|COOKIE|SESSION|CSRF|JWT)[A-Z0-9_]*=)[^\s]+/gi, '$1[REDACTED_SECRET]')
}

const commandRecords = fs.existsSync(commandsPath)
  ? fs.readFileSync(commandsPath, 'utf8').trim().split(/\n/).filter(Boolean).map((line) => JSON.parse(line))
  : []

const commands = commandRecords.map((record) => {
  let outputTail = ''
  try {
    outputTail = fs.readFileSync(record.log, 'utf8').split(/\r?\n/).slice(-40).join('\n')
  } catch {
    outputTail = ''
  }

  return {
    step: record.step,
    name: record.name,
    command: record.command,
    exit_code: record.exit_code,
    status: record.exit_code === 0 ? 'passed' : 'failed',
    local_log: record.log,
    output_tail: redact(outputTail),
  }
})

const overall = Number(overallRaw)
const report = {
  schema_version: '1.0.0',
  report_kind: 'relay_make_validate_latest',
  status: overall === 0 ? 'passed' : 'failed',
  created_at: createdAt,
  commit_short: commitShort,
  commit_sha: commitSha,
  local_output_dir: outDir,
  commands,
}

fs.writeFileSync(jsonPath, JSON.stringify(report, null, 2) + '\n')

const markdown = [
  '# Latest Relay Validation Report',
  '',
  `- status: ${report.status}`,
  `- commit: ${report.commit_sha}`,
  `- created_at: ${report.created_at}`,
  `- local_output_dir: \`${report.local_output_dir}\``,
  '',
  '## Commands',
  '',
  '| Step | Name | Exit | Status |',
  '|---:|---|---:|---|',
  ...commands.map((command) => `| ${command.step} | \`${command.name}\` | ${command.exit_code} | ${command.status} |`),
  '',
  '## Failure output tails',
  '',
]

const failedCommands = commands.filter((command) => command.exit_code !== 0)
if (failedCommands.length === 0) {
  markdown.push('No command failures captured.', '')
} else {
  for (const command of failedCommands) {
    markdown.push(`### ${command.name}`, '', '```text', command.output_tail || '(no output captured)', '```', '')
  }
}

fs.writeFileSync(mdPath, markdown.join('\n'))
NODE
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

write_tracked_reports

echo "tracked validation report: $TRACKED_JSON"
echo "tracked validation summary: $TRACKED_MD"

exit "$overall"
