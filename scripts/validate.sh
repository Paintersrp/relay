#!/usr/bin/env bash
set -u

if ! command -v git >/dev/null 2>&1; then
  echo "git is required for validation report generation" >&2
  exit 1
fi

if ! command -v node >/dev/null 2>&1; then
  echo "node is required for validation report generation" >&2
  exit 1
fi

COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown-commit)"
FULL_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown-commit)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
ISO_STAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [ "$COMMIT" = "unknown-commit" ] || [ "$FULL_COMMIT" = "unknown-commit" ]; then
  echo "git metadata is required for validation report generation" >&2
  exit 1
fi

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
const path = require('path')
const crypto = require('crypto')
const { spawnSync } = require('child_process')
const [jsonPath, mdPath, commandsPath, commitShort, commitSha, createdAt, outDir, overallRaw] = process.argv.slice(2)
const repoRoot = process.cwd()

function redact(value) {
  return String(value ?? '')
    .replace(/(Authorization:\s*Bearer\s+)[^\s]+/gi, '$1[REDACTED_TOKEN]')
    .replace(/([?&](?:token|access_token|auth|signature|X-Amz-Signature)=)[^&\s]+/gi, '$1[REDACTED_TOKEN]')
    .replace(/\b[A-Za-z0-9+/]{48,}={0,2}\b/g, '[REDACTED_SECRET]')
    .replace(/([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASS|API_KEY|ACCESS_KEY|PRIVATE_KEY|AUTH|COOKIE|SESSION|CSRF|JWT)[A-Z0-9_]*=)[^\s]+/gi, '$1[REDACTED_SECRET]')
}

function normalizeRepoPath(value) {
  return path.relative(repoRoot, path.resolve(repoRoot, value)).replace(/\\/g, '/')
}

function normalizeInputPath(value) {
  return String(value).replace(/\\/g, '/')
}

function isExcludedPath(repoPath) {
  const normalized = normalizeInputPath(repoPath)
  if (normalized === trackedJsonRepoPath || normalized === trackedMdRepoPath) {
    return true
  }
  if (normalized === outDirRepoPath || normalized.startsWith(`${outDirRepoPath}/`)) {
    return true
  }
  if (normalized === 'data/validation' || normalized.startsWith('data/validation/')) {
    return true
  }
  return false
}

function runGit(args, options = {}) {
  const result = spawnSync('git', args, {
    cwd: repoRoot,
    encoding: 'utf8',
    maxBuffer: 10 * 1024 * 1024,
    ...options,
  })

  if (result.error) {
    throw result.error
  }
  if (result.status !== 0) {
    throw new Error(`git ${args.join(' ')} failed with exit code ${result.status}: ${result.stderr || result.stdout}`.trim())
  }

  return result.stdout ?? ''
}

function parseTrackedNameStatus(raw) {
  const entries = []
  const parts = raw.split('\0').filter(Boolean)

  for (let index = 0; index < parts.length; ) {
    const statusToken = parts[index++] || ''
    if (!statusToken) {
      continue
    }

    const kind = statusToken[0]
    if (kind === 'R' || kind === 'C') {
      const previousPath = normalizeInputPath(parts[index++] || '')
      const nextPath = normalizeInputPath(parts[index++] || '')
      if (!isExcludedPath(previousPath) && !isExcludedPath(nextPath)) {
        entries.push({
          status: kind,
          path: nextPath,
          previous_path: previousPath,
        })
      }
      continue
    }

    const entryPath = normalizeInputPath(parts[index++] || '')
    if (!isExcludedPath(entryPath)) {
      entries.push({
        status: kind,
        path: entryPath,
      })
    }
  }

  return entries
}

function parseStatusLines(raw) {
  const lines = raw.split(/\r?\n/).filter(Boolean)
  const filtered = []

  for (const line of lines) {
    const pathPart = line.slice(3)
    const candidatePaths = pathPart.split(' -> ').map((value) => normalizeInputPath(value))
    if (candidatePaths.some((candidate) => isExcludedPath(candidate))) {
      continue
    }
    filtered.push(redact(line))
  }

  return filtered.sort((left, right) => left.localeCompare(right))
}

function sha256Text(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function captureSourceSnapshot() {
  const capturedAt = new Date().toISOString()
  const exclusionArgs = [
    `:(exclude)${trackedJsonRepoPath}`,
    `:(exclude)${trackedMdRepoPath}`,
    ':(exclude,glob)data/validation/**',
  ]

  if (outDirRepoPath !== 'data/validation' && !outDirRepoPath.startsWith('data/validation/')) {
    exclusionArgs.push(`:(exclude)${outDirRepoPath}`, `:(exclude,glob)${outDirRepoPath}/**`)
  }

  const filteredStatusLines = parseStatusLines(
    runGit(['status', '--porcelain=v1', '--untracked-files=normal']),
  )

  const trackedEntries = parseTrackedNameStatus(
    runGit(['diff', '--name-status', '--find-renames', '-z', 'HEAD', '--', '.', ...exclusionArgs]),
  )

  const untrackedPaths = runGit(['ls-files', '--others', '--exclude-standard', '-z', '--', '.', ...exclusionArgs])
    .split('\0')
    .filter(Boolean)
    .map((entry) => normalizeInputPath(entry))
    .filter((entry) => !isExcludedPath(entry))
    .sort((left, right) => left.localeCompare(right))

  const untrackedEntries = untrackedPaths.map((entry) => ({
    status: '??',
    path: entry,
  }))

  const diffNameStatus = [...trackedEntries, ...untrackedEntries].sort((left, right) => {
    const leftKey = `${left.path}\u0000${left.status}\u0000${left.previous_path || ''}`
    const rightKey = `${right.path}\u0000${right.status}\u0000${right.previous_path || ''}`
    return leftKey.localeCompare(rightKey)
  })

  const diffStat = redact(
    runGit(['diff', '--stat', 'HEAD', '--', '.', ...exclusionArgs]).trim(),
  )

  const binaryDiff = redact(
    runGit(['diff', '--binary', 'HEAD', '--', '.', ...exclusionArgs]),
  )

  const untrackedFileDigests = untrackedPaths.map((entry) => {
    const absolutePath = path.resolve(repoRoot, entry)
    return {
      path: entry,
      sha256: crypto.createHash('sha256').update(fs.readFileSync(absolutePath)).digest('hex'),
    }
  })

  const canonicalPayload = {
    base_commit_sha: commitSha,
    status_porcelain: filteredStatusLines,
    diff_name_status: diffNameStatus,
    diff_binary: binaryDiff,
    untracked_file_digests: untrackedFileDigests,
  }

  return {
    captured_at: capturedAt,
    model: 'base_commit_plus_worktree_diff_excluding_validation_report_artifacts',
    worktree_dirty: filteredStatusLines.length > 0,
    diff_sha256: sha256Text(JSON.stringify(canonicalPayload)),
    diff_name_status: diffNameStatus,
    diff_stat: diffStat,
    status_porcelain: filteredStatusLines,
  }
}

function capturePostReportStatus() {
  return runGit(['status', '--porcelain=v1', '--untracked-files=normal'])
    .split(/\r?\n/)
    .filter(Boolean)
    .map((line) => redact(line))
}

function renderMarkdown(report, commands) {
  const markdown = [
    '# Latest Relay Validation Report',
    '',
    `- status: ${report.status}`,
    `- base_commit: ${report.base_commit_sha}`,
    `- validated_source_snapshot: ${report.validated_source_snapshot.diff_sha256}`,
    `- worktree_dirty: ${report.validated_source_snapshot.worktree_dirty}`,
    `- created_at: ${report.created_at}`,
    `- local_output_dir: \`${report.local_output_dir}\``,
    '',
    '## Validated source changes',
    '',
  ]

  if (report.validated_source_snapshot.diff_name_status.length === 0) {
    markdown.push('No source changes relative to base commit.', '')
  } else {
    for (const entry of report.validated_source_snapshot.diff_name_status) {
      const previous = entry.previous_path ? ` (${entry.previous_path} -> ${entry.path})` : ''
      markdown.push(`- ${entry.status} ${entry.path}${previous}`)
    }
    markdown.push('')
  }

  markdown.push(
    '## Commands',
    '',
    '| Step | Name | Exit | Status |',
    '|---:|---|---:|---|',
    ...commands.map((command) => `| ${command.step} | \`${command.name}\` | ${command.exit_code} | ${command.status} |`),
    '',
    '## Failure output tails',
    '',
  )

  const failedCommands = commands.filter((command) => command.exit_code !== 0)
  if (failedCommands.length === 0) {
    markdown.push('No command failures captured.', '')
  } else {
    for (const command of failedCommands) {
      markdown.push(`### ${command.name}`, '', '```text', command.output_tail || '(no output captured)', '```', '')
    }
  }

  return markdown.join('\n')
}

const trackedJsonRepoPath = normalizeRepoPath(jsonPath)
const trackedMdRepoPath = normalizeRepoPath(mdPath)
const outDirRepoPath = normalizeRepoPath(outDir)

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
const sourceSnapshot = captureSourceSnapshot()
const report = {
  schema_version: '2.0.0',
  report_kind: 'relay_make_validate_latest',
  status: overall === 0 ? 'passed' : 'failed',
  created_at: createdAt,
  base_commit_short: commitShort,
  base_commit_sha: commitSha,
  local_output_dir: outDir,
  validated_source_snapshot: sourceSnapshot,
  post_report_status_porcelain: [],
  report_files: [trackedJsonRepoPath, trackedMdRepoPath],
  commands,
}

fs.writeFileSync(jsonPath, JSON.stringify(report, null, 2) + '\n')
fs.writeFileSync(mdPath, renderMarkdown(report, commands))
report.post_report_status_porcelain = capturePostReportStatus()
fs.writeFileSync(jsonPath, JSON.stringify(report, null, 2) + '\n')
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
