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

# Scope: tier | touched | changed (default: tier)
RELAY_VALIDATE_SCOPE="${RELAY_VALIDATE_SCOPE:-tier}"
# Tier: fast | broad | full (default: full)
RELAY_VALIDATE_TIER="${RELAY_VALIDATE_TIER:-full}"

TRACKED_DIR="${RELAY_VALIDATE_TRACKED_DIR:-handoffs/validation}"
TRACKED_JSON="${TRACKED_DIR}/latest.validation-report.json"
TRACKED_MD="${TRACKED_DIR}/latest.validation-summary.md"
CLOSEOUT_VALIDATION_JSON="$TRACKED_JSON"
CLOSEOUT_VALIDATION_MD="$TRACKED_MD"

case "$RELAY_VALIDATE_SCOPE" in
  tier)
    case "$RELAY_VALIDATE_TIER" in
      fast)
        TIER_JSON="${TRACKED_DIR}/latest.validation-report.fast.json"
        TIER_MD="${TRACKED_DIR}/latest.validation-summary.fast.md"
        ;;
      broad)
        TIER_JSON="${TRACKED_DIR}/latest.validation-report.broad.json"
        TIER_MD="${TRACKED_DIR}/latest.validation-summary.broad.md"
        ;;
      full)
        TIER_JSON="$TRACKED_JSON"
        TIER_MD="$TRACKED_MD"
        ;;
      *)
        echo "Unknown validation tier: $RELAY_VALIDATE_TIER (expected: fast, broad, full)" >&2
        exit 1
        ;;
    esac
    ;;
  touched|changed)
    TIER_JSON="${TRACKED_DIR}/latest.validation-report.${RELAY_VALIDATE_SCOPE}.json"
    TIER_MD="${TRACKED_DIR}/latest.validation-summary.${RELAY_VALIDATE_SCOPE}.md"
    ;;
  *)
    echo "Unknown validation scope: $RELAY_VALIDATE_SCOPE (expected: tier, touched, changed)" >&2
    exit 1
    ;;
esac

mkdir -p "$TRACKED_DIR"

node - "$TIER_JSON" "$TIER_MD" "$TRACKED_JSON" "$TRACKED_MD" "$RELAY_VALIDATE_TIER" "$RELAY_VALIDATE_SCOPE" <<'NODE'
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { spawnSync } = require('child_process')

const [tierJsonPath, tierMdPath, fullJsonPath, fullMdPath, tier, scope] = process.argv.slice(2)
const repoRoot = process.cwd()

const fastCommands = [
  { step: 1, name: 'go-fmt-agentrefs-executor', command: 'go fmt ./cmd/agentrefs ./internal/agentrefs ./internal/executor', argv: ['go', ['fmt', './cmd/agentrefs', './internal/agentrefs', './internal/executor']] },
  { step: 2, name: 'go-test-agentrefs', command: 'go test ./internal/agentrefs/... ./cmd/agentrefs/...', argv: ['go', ['test', './internal/agentrefs/...', './cmd/agentrefs/...']] },
  { step: 3, name: 'go-test-executor', command: 'go test ./internal/executor/...', argv: ['go', ['test', './internal/executor/...']] },
]

const broadCommands = [
  ...fastCommands,
  { step: 4, name: 'go-test-all', command: 'go test ./...', argv: ['go', ['test', './...']] },
  { step: 5, name: 'web-typecheck', command: 'cd apps/web && npm run typecheck', shell: true },
  { step: 6, name: 'web-test', command: 'cd apps/web && npm run test', shell: true },
]

const fullCommands = [
  ...broadCommands,
  { step: 7, name: 'web-build', command: 'cd apps/web && npm run build', shell: true },
  { step: 8, name: 'no-root-agentrefs-exe', command: 'test ! -e agentrefs.exe', shell: true },
]

const commandsByTier = { fast: fastCommands, broad: broadCommands, full: fullCommands }

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
  return String(value).replace(/\\/g, '/').replace(/^\.\//, '')
}

const reportArtifactPattern = /^handoffs\/validation\/latest\.validation-(?:report|summary)(?:\.[^.\/]+)?\.(?:json|md)$/

function isValidationReportPath(repoPath) {
  return reportArtifactPattern.test(normalizeInputPath(repoPath))
}

const excludedRepoPaths = new Set([
  normalizeRepoPath(tierJsonPath),
  normalizeRepoPath(tierMdPath),
  normalizeRepoPath(fullJsonPath),
  normalizeRepoPath(fullMdPath),
])

function isExcludedPath(repoPath) {
  const normalized = normalizeInputPath(repoPath)
  return excludedRepoPaths.has(normalized) || isValidationReportPath(normalized)
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

function captureSourceSnapshot(baseRef, baseCommitSha) {
  const capturedAt = new Date().toISOString()
  const exclusionArgs = [...excludedRepoPaths].map((p) => `:(exclude)${p}`)

  const statusPorcelain = parseStatusLines(runGit(['status', '--porcelain=v1', '--untracked-files=normal']))
  const trackedEntries = parseTrackedNameStatus(
    runGit(['diff', '--name-status', '--find-renames', '-z', baseRef, '--', '.', ...exclusionArgs]),
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

  const diffStat = runGit(['diff', '--stat', baseRef, '--', '.', ...exclusionArgs]).trim()
  const binaryDiff = runGit(['diff', '--binary', baseRef, '--', '.', ...exclusionArgs])

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

function stableUnique(values) {
  return [...new Set(values)].sort((left, right) => left.localeCompare(right))
}

function normalizeTouchedPaths(rawValues) {
  return rawValues
    .flatMap((value) => String(value || '').split(/\s+/))
    .map((value) => value.trim())
    .filter(Boolean)
    .map((value) => normalizeRepoPath(value))
    .filter((value) => value && value !== '..' && !value.startsWith('../'))
    .filter((value) => !isExcludedPath(value))
}

function changedPathsFromGit() {
  const paths = []
  const status = runGit(['status', '--porcelain=v1', '--untracked-files=normal'])
  for (const line of status.split(/\r?\n/).filter(Boolean)) {
    const pathPart = line.slice(3)
    const currentPath = pathPart.includes(' -> ') ? pathPart.split(' -> ').at(-1) : pathPart
    const normalized = normalizeInputPath(currentPath)
    if (!isExcludedPath(normalized)) paths.push(normalized)
  }
  return stableUnique(paths)
}

function isDocsPath(repoPath) {
  return repoPath.startsWith('docs/') || repoPath.endsWith('.md') || repoPath.endsWith('.mdx') || repoPath === 'AGENTS.md'
}

function isGoPath(repoPath) {
  return repoPath.endsWith('.go')
}

function isWebPath(repoPath) {
  return repoPath.startsWith('apps/web/')
}

function isValidationInfraPath(repoPath) {
  return repoPath === 'Makefile' || repoPath === 'scripts/validate.sh' || repoPath.startsWith('.githooks/')
}

function isDependencySensitivePath(repoPath) {
  return [
    'go.mod',
    'go.sum',
    'package.json',
    'package-lock.json',
    'apps/web/package.json',
    'apps/web/package-lock.json',
  ].includes(repoPath)
}

function isGlobalEscalationPath(repoPath) {
  if (isValidationInfraPath(repoPath) || isDependencySensitivePath(repoPath)) return false
  if (isWebPath(repoPath) || isGoPath(repoPath) || isDocsPath(repoPath)) return false
  return [
    'sqlc.yaml',
    'templ.yaml',
  ].includes(repoPath) ||
    repoPath.startsWith('internal/db/migrations/') ||
    repoPath.startsWith('web/') ||
    repoPath.endsWith('.sql') ||
    repoPath.endsWith('.templ')
}

function goPackageFor(repoPath) {
  const dir = path.posix.dirname(repoPath)
  return dir === '.' ? '.' : `./${dir}`
}

function classifyPaths(paths) {
  const classification = {
    docs_only: false,
    docs: [],
    go: [],
    go_packages: [],
    web: [],
    validation_infrastructure: [],
    dependency_sensitive: [],
    global_escalation: [],
    other: [],
    global_escalation_required: false,
  }

  for (const repoPath of paths) {
    if (isDocsPath(repoPath)) classification.docs.push(repoPath)
    if (isGoPath(repoPath)) {
      classification.go.push(repoPath)
      classification.go_packages.push(goPackageFor(repoPath))
    }
    if (isWebPath(repoPath)) classification.web.push(repoPath)
    if (isValidationInfraPath(repoPath)) classification.validation_infrastructure.push(repoPath)
    if (isDependencySensitivePath(repoPath)) classification.dependency_sensitive.push(repoPath)
    if (isGlobalEscalationPath(repoPath)) classification.global_escalation.push(repoPath)
    if (!isDocsPath(repoPath) && !isGoPath(repoPath) && !isWebPath(repoPath) && !isValidationInfraPath(repoPath) && !isDependencySensitivePath(repoPath) && !isGlobalEscalationPath(repoPath)) {
      classification.other.push(repoPath)
    }
  }

  for (const key of ['docs', 'go', 'go_packages', 'web', 'validation_infrastructure', 'dependency_sensitive', 'global_escalation', 'other']) {
    classification[key] = stableUnique(classification[key])
  }
  classification.global_escalation_required =
    classification.global_escalation.length > 0 ||
    classification.validation_infrastructure.length > 0 ||
    classification.dependency_sensitive.length > 0
  classification.docs_only = paths.length > 0 && paths.every((repoPath) => isDocsPath(repoPath))
  return classification
}

function affectedCommands(classification) {
  const specs = []
  let step = 1
  const seenCommands = new Set()

  function pushSpec(spec) {
    if (seenCommands.has(spec.command)) return
    seenCommands.add(spec.command)
    specs.push({ ...spec, step: step++ })
  }

  if (classification.validation_infrastructure.length > 0) {
    pushSpec({ name: 'validate-script-syntax', command: 'bash -n scripts/validate.sh', shell: true })
  }

  if (classification.global_escalation_required) {
    for (const spec of broadCommands) pushSpec(spec)
    return specs
  }

  if (classification.go.length > 0) {
    const goFiles = classification.go.join(' ')
    pushSpec({ name: 'gofmt-touched-files', command: `gofmt -w ${goFiles}`, argv: ['gofmt', ['-w', ...classification.go]] })
  }

  if (classification.go_packages.length > 0) {
    const packages = classification.go_packages.join(' ')
    pushSpec({ name: 'go-test-affected-packages', command: `go test ${packages}`, shell: true })
  }

  if (classification.web.length > 0) {
    pushSpec({ name: 'web-typecheck', command: 'cd apps/web && npm run typecheck', shell: true })
    pushSpec({ name: 'web-test', command: 'cd apps/web && npm run test', shell: true })
  }

  return specs
}

function renderMarkdown(report) {
  const titleScope = report.validation_scope || report.validation_tier
  const markdown = [
    `# Latest Relay Validation Report (${titleScope})`,
    '',
    `- status: ${report.status}`,
    `- validation_tier: ${report.validation_tier}`,
    `- validation_scope: ${report.validation_scope}`,
    `- base_commit: ${report.base_commit_sha}`,
    `- validated_source_snapshot: ${report.validated_source_snapshot.diff_sha256}`,
    `- worktree_dirty: ${report.validated_source_snapshot.worktree_dirty}`,
    `- created_at: ${report.created_at}`,
    '',
  ]

  if (report.normalized_paths) {
    markdown.push('## Affected paths', '')
    if (report.normalized_paths.length === 0) {
      markdown.push('No affected paths after normalization.', '')
    } else {
      for (const repoPath of report.normalized_paths) markdown.push(`- ${repoPath}`)
      markdown.push('')
    }
    markdown.push(`Global escalation required: ${report.path_classification.global_escalation_required}`, '')
  }

  markdown.push('## Validated source changes', '')

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
    '| Step | Name | Command | Exit | Status |',
    '|---:|---|---|---:|---|',
  )
  if (report.commands.length === 0) {
    markdown.push('| - | no-derived-commands | docs-only or no runnable affected-path checks | 0 | passed |')
  } else {
    markdown.push(...report.commands.map((command) => `| ${command.step} | \`${command.name}\` | \`${command.command.replace(/\|/g, '\\|')}\` | ${command.exit_code} | ${command.status} |`))
  }

  markdown.push('', '## Failure output tails', '')

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

const baseRef = process.env.RELAY_VALIDATE_BASE_REF || 'HEAD'
const baseCommitSha = runGit(['rev-parse', baseRef]).trim()
const baseCommitShort = runGit(['rev-parse', '--short=12', baseRef]).trim()
const createdAt = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')

let inputPaths = []
let normalizedPaths = []
let pathClassification = null
let commandSpecs = []

if (scope === 'touched') {
  inputPaths = normalizeTouchedPaths([process.env.RELAY_VALIDATE_PATHS, process.env.PATHS])
  if (inputPaths.length === 0) {
    console.error('validate-touched requires explicit paths through PATHS or RELAY_VALIDATE_PATHS')
    process.exit(2)
  }
  normalizedPaths = stableUnique(inputPaths)
  pathClassification = classifyPaths(normalizedPaths)
  commandSpecs = affectedCommands(pathClassification)
} else if (scope === 'changed') {
  inputPaths = changedPathsFromGit()
  normalizedPaths = inputPaths
  pathClassification = classifyPaths(normalizedPaths)
  commandSpecs = affectedCommands(pathClassification)
} else {
  commandSpecs = commandsByTier[tier] || fullCommands
}

const commands = commandSpecs.map(runValidationCommand)
const overall = commands.some((command) => command.exit_code !== 0) ? 1 : 0

const reportFiles = [normalizeRepoPath(tierJsonPath), normalizeRepoPath(tierMdPath)]
if (scope === 'tier' && tier !== 'full') {
  reportFiles.push(normalizeRepoPath(fullJsonPath), normalizeRepoPath(fullMdPath))
}

const report = {
  schema_version: scope === 'tier' ? '3.0.0' : '3.1.0',
  report_kind: scope === 'tier' ? 'relay_make_validate_latest' : 'relay_make_validate_affected_paths',
  validation_tier: scope === 'tier' ? tier : 'affected',
  validation_scope: scope,
  status: overall === 0 ? 'passed' : 'failed',
  created_at: createdAt,
  base_commit_short: baseCommitShort,
  base_commit_sha: baseCommitSha,
  validated_source_snapshot: captureSourceSnapshot(baseRef, baseCommitSha),
  post_report_status_porcelain: [],
  report_files: reportFiles,
  commands,
}

if (scope !== 'tier') {
  report.input_paths = inputPaths
  report.normalized_paths = normalizedPaths
  report.path_classification = pathClassification
  report.command_table = commandSpecs.map((spec) => ({ step: spec.step, name: spec.name, command: spec.command }))
  report.failure_output_tails = commands
    .filter((command) => command.exit_code !== 0)
    .map((command) => ({ name: command.name, output_tail: command.output_tail }))
}

fs.writeFileSync(tierJsonPath, JSON.stringify(report, null, 2) + '\n')
fs.writeFileSync(tierMdPath, renderMarkdown(report) + '\n')
report.post_report_status_porcelain = capturePostReportStatus()
fs.writeFileSync(tierJsonPath, JSON.stringify(report, null, 2) + '\n')

process.exit(overall)
NODE
