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

TRACKED_DIR="${RELAY_VALIDATE_TRACKED_DIR:-handoffs/validation}"
TRACKED_JSON="${TRACKED_DIR}/latest.validation-report.json"
TRACKED_MD="${TRACKED_DIR}/latest.validation-summary.md"

mkdir -p "$TRACKED_DIR"

node - "$TRACKED_JSON" "$TRACKED_MD" <<'NODE'
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { spawnSync } = require('child_process')

const [jsonPath, mdPath] = process.argv.slice(2)
const repoRoot = process.cwd()

const commandsToRun = [
  { step: 1, name: 'go-fmt-executor', command: 'go fmt ./internal/executor', argv: ['go', ['fmt', './internal/executor']] },
  { step: 2, name: 'go-test-executor', command: 'go test ./internal/executor/...', argv: ['go', ['test', './internal/executor/...']] },
  { step: 3, name: 'go-test-all', command: 'go test ./...', argv: ['go', ['test', './...']] },
  { step: 4, name: 'web-typecheck', command: 'cd apps/web && npm run typecheck', shell: true },
  { step: 5, name: 'web-build', command: 'cd apps/web && npm run build', shell: true },
]

function redactCommandOutput(value) {
  return String(value ?? '')
    .replace(/(Authorization:\s*Bearer\s+)[^\s]+/gi, '$1[REDACTED_TOKEN]')
    .replace(/([?&](?:token|access_token|auth|signature|X-Amz-Signature)=)[^&\s]+/gi, '$1[REDACTED_TOKEN]')
    .replace(/\b[A-Za-z0-9+_-]{48,}={0,2}\b/g, '[REDACTED_SECRET]')
    .replace(/([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASS|API_KEY|ACCESS_KEY|PRIVATE_KEY|AUTH|COOKIE|SESSION|CSRF|JWT)[A-Z0-9_]*=)[^\s]+/gi, '$1[REDACTED_SECRET]')
}

function normalizeRepoPath(value) {
  return path.relative(repoRoot, path.resolve(repoRoot, value)).replace(/\\/g, '/')
}

function normalizeInputPath(value) {
  return String(value).replace(/\\/g, '/')
}

const trackedJsonRepoPath = normalizeRepoPath(jsonPath)
const trackedMdRepoPath = normalizeRepoPath(mdPath)

function isExcludedPath(repoPath) {
  const normalized = normalizeInputPath(repoPath)
  return normalized === trackedJsonRepoPath || normalized === trackedMdRepoPath
}

function runGit(args, options = {}) {
  const result = spawnSync('git', args, {
    cwd: repoRoot,
    encoding: 'utf8',
    maxBuffer: 20 * 1024 * 1024,
    ...options,
  })
  if (result.error) throw result.error
  if (result.status !== 0) {
    throw new Error(`git ${args.join(' ')} failed with exit code ${result.status}: ${result.stderr || result.stdout}`.trim())
  }
  return result.stdout ?? ''
}

function parseStatusLines(raw) {
  return raw
    .split(/\r?\n/)
    .filter(Boolean)
    .filter((line) => {
      const pathPart = line.slice(3)
      const candidatePaths = pathPart.split(' -> ').map((value) => normalizeInputPath(value))
      return !candidatePaths.some((candidate) => isExcludedPath(candidate))
    })
    .sort((left, right) => left.localeCompare(right))
}

function parseTrackedNameStatus(raw) {
  const entries = []
  const parts = raw.split('\0').filter(Boolean)
  for (let index = 0; index < parts.length; ) {
    const statusToken = parts[index++] || ''
    if (!statusToken) continue
    const kind = statusToken[0]
    if (kind === 'R' || kind === 'C') {
      const previousPath = normalizeInputPath(parts[index++] || '')
      const nextPath = normalizeInputPath(parts[index++] || '')
      if (!isExcludedPath(previousPath) && !isExcludedPath(nextPath)) {
        entries.push({ status: kind, path: nextPath, previous_path: previousPath })
      }
      continue
    }
    const entryPath = normalizeInputPath(parts[index++] || '')
    if (!isExcludedPath(entryPath)) entries.push({ status: kind, path: entryPath })
  }
  return entries
}

function sha256Text(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function captureSourceSnapshot(baseCommitSha) {
  const capturedAt = new Date().toISOString()
  const exclusionArgs = [
    `:(exclude)${trackedJsonRepoPath}`,
    `:(exclude)${trackedMdRepoPath}`,
  ]

  const statusPorcelain = parseStatusLines(runGit(['status', '--porcelain=v1', '--untracked-files=normal']))
  const trackedEntries = parseTrackedNameStatus(
    runGit(['diff', '--name-status', '--find-renames', '-z', 'HEAD', '--', '.', ...exclusionArgs]),
  )

  const untrackedPaths = runGit(['ls-files', '--others', '--exclude-standard', '-z', '--', '.', ...exclusionArgs])
    .split('\0')
    .filter(Boolean)
    .map((entry) => normalizeInputPath(entry))
    .filter((entry) => !isExcludedPath(entry))
    .sort((left, right) => left.localeCompare(right))

  const untrackedEntries = untrackedPaths.map((entry) => ({ status: '??', path: entry }))
  const diffNameStatus = [...trackedEntries, ...untrackedEntries].sort((left, right) => {
    const leftKey = `${left.path}\0${left.status}\0${left.previous_path || ''}`
    const rightKey = `${right.path}\0${right.status}\0${right.previous_path || ''}`
    return leftKey.localeCompare(rightKey)
  })

  const diffStat = runGit(['diff', '--stat', 'HEAD', '--', '.', ...exclusionArgs]).trim()
  const binaryDiff = runGit(['diff', '--binary', 'HEAD', '--', '.', ...exclusionArgs])

  const untrackedFileDigests = untrackedPaths.map((entry) => {
    const absolutePath = path.resolve(repoRoot, entry)
    return {
      path: entry,
      sha256: crypto.createHash('sha256').update(fs.readFileSync(absolutePath)).digest('hex'),
    }
  })

  const canonicalPayload = {
    base_commit_sha: baseCommitSha,
    status_porcelain: statusPorcelain,
    diff_name_status: diffNameStatus,
    diff_binary: binaryDiff,
    untracked_file_digests: untrackedFileDigests,
  }

  return {
    captured_at: capturedAt,
    model: 'base_commit_plus_worktree_diff_excluding_validation_report_artifacts',
    worktree_dirty: statusPorcelain.length > 0,
    diff_sha256: sha256Text(JSON.stringify(canonicalPayload)),
    diff_name_status: diffNameStatus,
    diff_stat: diffStat,
    status_porcelain: statusPorcelain,
  }
}

function capturePostReportStatus() {
  return runGit(['status', '--porcelain=v1', '--untracked-files=normal'])
    .split(/\r?\n/)
    .filter(Boolean)
    .sort((left, right) => left.localeCompare(right))
}

function outputTail(command, stdout, stderr, exitCode) {
  const combined = `$ ${command}\n${stdout || ''}${stderr || ''}exit_code: ${exitCode}\n`
  return redactCommandOutput(combined.split(/\r?\n/).slice(-40).join('\n'))
}

function runValidationCommand(spec) {
  const startedAt = new Date().toISOString()
  const result = spec.shell
    ? spawnSync('bash', ['-lc', spec.command], { cwd: repoRoot, encoding: 'utf8', maxBuffer: 20 * 1024 * 1024 })
    : spawnSync(spec.argv[0], spec.argv[1], { cwd: repoRoot, encoding: 'utf8', maxBuffer: 20 * 1024 * 1024 })
  const completedAt = new Date().toISOString()
  const exitCode = typeof result.status === 'number' ? result.status : 1
  const commandOutput = result.error ? `${result.error.message}\n${result.stderr || ''}` : `${result.stdout || ''}${result.stderr || ''}`
  return {
    step: spec.step,
    name: spec.name,
    command: spec.command,
    started_at: startedAt,
    completed_at: completedAt,
    exit_code: exitCode,
    status: exitCode === 0 ? 'passed' : 'failed',
    output_tail: outputTail(spec.command, commandOutput, '', exitCode),
  }
}

function renderMarkdown(report) {
  const markdown = [
    '# Latest Relay Validation Report',
    '',
    `- status: ${report.status}`,
    `- base_commit: ${report.base_commit_sha}`,
    `- validated_source_snapshot: ${report.validated_source_snapshot.diff_sha256}`,
    `- worktree_dirty: ${report.validated_source_snapshot.worktree_dirty}`,
    `- created_at: ${report.created_at}`,
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
    ...report.commands.map((command) => `| ${command.step} | \`${command.name}\` | ${command.exit_code} | ${command.status} |`),
    '',
    '## Failure output tails',
    '',
  )

  const failedCommands = report.commands.filter((command) => command.exit_code !== 0)
  if (failedCommands.length === 0) {
    markdown.push('No command failures captured.', '')
  } else {
    for (const command of failedCommands) {
      markdown.push(`### ${command.name}`, '', '```text', command.output_tail || '(no output captured)', '```', '')
    }
  }

  return markdown.join('\n')
}

const baseCommitSha = runGit(['rev-parse', 'HEAD']).trim()
const baseCommitShort = runGit(['rev-parse', '--short=12', 'HEAD']).trim()
const createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')

const commands = commandsToRun.map(runValidationCommand)
const overall = commands.some((command) => command.exit_code !== 0) ? 1 : 0

const report = {
  schema_version: '3.0.0',
  report_kind: 'relay_make_validate_latest',
  status: overall === 0 ? 'passed' : 'failed',
  created_at: createdAt,
  base_commit_short: baseCommitShort,
  base_commit_sha: baseCommitSha,
  validated_source_snapshot: captureSourceSnapshot(baseCommitSha),
  post_report_status_porcelain: [],
  report_files: [trackedJsonRepoPath, trackedMdRepoPath],
  commands,
}

fs.writeFileSync(jsonPath, JSON.stringify(report, null, 2) + '\n')
fs.writeFileSync(mdPath, renderMarkdown(report) + '\n')
report.post_report_status_porcelain = capturePostReportStatus()
fs.writeFileSync(jsonPath, JSON.stringify(report, null, 2) + '\n')

process.exit(overall)
NODE
