import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import type { RelayArtifact, RelayRunEvent } from '@/features/relay-runs'
import {
  runArtifactContentQueryOptionsForArtifact,
  formatRunDate,
} from '@/features/relay-runs'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import {
  CheckCircle2,
  Clock,
  FileText,
  XCircle,
} from 'lucide-react'

type EvidenceStageKey = 'intake' | 'compile' | 'execute' | 'audit' | 'provenance'

interface RunEvidenceBrowserProps {
  runId: string
  artifacts: RelayArtifact[]
  events: RelayRunEvent[]
  className?: string
}

const EVIDENCE_STAGES: {
  key: EvidenceStageKey
  label: string
  description: string
}[] = [
  {
    key: 'intake',
    label: 'Intake',
    description:
      'Submitted handoff, parsed metadata, and intake validation evidence.',
  },
  {
    key: 'compile',
    label: 'Compile / Render',
    description:
      'Canonical packet, validation reports, repaired packets, and rendered executor artifacts.',
  },
  {
    key: 'execute',
    label: 'Execute',
    description:
      'Executor result, command logs, stdout, stderr, and execution validation artifacts.',
  },
  {
    key: 'audit',
    label: 'Audit',
    description:
      'Audit packets, audit input summaries, manual handbacks, and closeout decisions.',
  },
  {
    key: 'provenance',
    label: 'Provenance',
    description: 'Git status, diffs, patches, and source-change evidence.',
  },
]

const STAGE_KEYWORDS: Record<EvidenceStageKey, string[]> = {
  intake: [
    'handoff',
    'planner_handoff',
    'parsed_frontmatter',
    'run_config',
    'intake',
    'frontmatter',
  ],
  compile: [
    'canonical_packet',
    'packet_validation',
    'brief_validation',
    'executor_brief',
    'compile',
    'render',
    'packet',
    'brief',
    'repair',
  ],
  execute: [
    'executor_result',
    'agent_result',
    'executor_stdout',
    'executor_stderr',
    'command_log',
    'execute',
    'stdout',
    'stderr',
  ],
  audit: [
    'audit',
    'audit_packet',
    'audit_input_summary',
    'mcp_audit_handback',
    'closeout',
    'decision',
  ],
  provenance: [
    'git_status',
    'git_diff',
    'git_diff_stat',
    'git_diff_numstat',
    'git_diff_patch',
    'git_diff_name_status',
    'provenance',
    'diff',
  ],
}

const FALLBACK_ARTIFACT: RelayArtifact = {
  id: '',
  label: '',
  path: '',
  kind: '',
  status: '',
  filename: '',
}

function artifactIdentity(artifact: RelayArtifact): string {
  return [
    artifact.kind,
    artifact.storageKind,
    artifact.filename,
    artifact.label,
    artifact.path,
    artifact.contentUrl,
  ]
    .filter((v): v is string => typeof v === 'string' && v.length > 0)
    .join(' ')
    .toLowerCase()
}

function getArtifactStage(artifact: RelayArtifact): EvidenceStageKey {
  const id = artifactIdentity(artifact)
  for (const stage of EVIDENCE_STAGES) {
    if (STAGE_KEYWORDS[stage.key].some((kw) => id.includes(kw))) {
      return stage.key
    }
  }
  return 'provenance'
}

function deriveProducer(artifact: RelayArtifact): string {
  const id = artifactIdentity(artifact)
  if (
    id.includes('git_diff') ||
    id.includes('git_status') ||
    id.includes('provenance') ||
    id.includes('diff')
  ) {
    return 'Provenance'
  }
  if (
    id.includes('audit') ||
    id.includes('closeout') ||
    id.includes('decision') ||
    id.includes('mcp_audit_handback')
  ) {
    return 'Auditor'
  }
  if (
    id.includes('validation') ||
    id.includes('packet_validation') ||
    id.includes('brief_validation') ||
    id.includes('intake_validation')
  ) {
    return 'Validator'
  }
  if (
    id.includes('handoff') ||
    id.includes('planner_handoff') ||
    id.includes('frontmatter') ||
    id.includes('run_config') ||
    id.includes('intake')
  ) {
    return 'Intake'
  }
  if (
    id.includes('canonical_packet') ||
    id.includes('executor_brief') ||
    id.includes('packet') ||
    id.includes('brief') ||
    id.includes('compile') ||
    id.includes('render') ||
    id.includes('repair')
  ) {
    return 'Compiler'
  }
  if (
    id.includes('executor_result') ||
    id.includes('agent_result') ||
    id.includes('command_log') ||
    id.includes('executor_stdout') ||
    id.includes('executor_stderr') ||
    id.includes('stdout') ||
    id.includes('stderr') ||
    id.includes('execute')
  ) {
    return 'Executor'
  }
  return 'Relay'
}

interface VerificationInfo {
  label: string
  className: string
  icon: React.ReactNode
}

function deriveVerification(artifact: RelayArtifact): VerificationInfo {
  const status = (artifact.status || '').toLowerCase()
  if (
    status.includes('failed') ||
    status.includes('error') ||
    status.includes('invalid')
  ) {
    return {
      label: 'failed',
      className: 'text-red-400',
      icon: <XCircle className='h-3 w-3' />,
    }
  }
  if (
    status.includes('pending') ||
    status.includes('queued') ||
    status.includes('running')
  ) {
    return {
      label: 'pending',
      className: 'text-yellow-400',
      icon: <Clock className='h-3 w-3' />,
    }
  }
  if (
    status.includes('verified') ||
    status.includes('passed') ||
    status.includes('ready') ||
    status.includes('captured') ||
    status.includes('complete') ||
    status.includes('accepted')
  ) {
    return {
      label: 'verified',
      className: 'text-emerald-400',
      icon: <CheckCircle2 className='h-3 w-3' />,
    }
  }
  return {
    label: 'captured',
    className: 'text-muted-foreground',
    icon: <FileText className='h-3 w-3' />,
  }
}

function getArtifactHash(artifact: RelayArtifact): string {
  const a = artifact as any
  const direct =
    a.sha256 || a.hash || a.digest || a.checksum || a.artifactHash
  if (direct && typeof direct === 'string' && direct.length > 0) {
    return direct
  }
  const preview = artifact.preview
  if (preview && typeof preview === 'string') {
    const match = preview.match(
      /(?:sha256|hash|digest)["':\s]*([a-fA-F0-9]{40,64})/i,
    )
    if (match) {
      return match[1]
    }
  }
  return '—'
}

function compactHash(hash: string): string {
  if (hash === '—') return hash
  if (hash.length > 16) return hash.slice(0, 12) + '…'
  return hash
}

function getRelatedEvents(
  artifact: RelayArtifact | null,
  events: RelayRunEvent[],
): RelayRunEvent[] {
  if (!artifact) {
    return events.slice(-20)
  }
  const tokens = [
    artifact.filename,
    artifact.label,
    artifact.kind,
    artifact.storageKind,
    artifact.path,
  ]
    .filter((t): t is string => typeof t === 'string' && t.length > 0)
    .map((t) => t.toLowerCase())
  if (tokens.length === 0) {
    return []
  }
  return events.filter((e) => {
    const haystack = `${e.message} ${
      e.details ? JSON.stringify(e.details) : ''
    }`.toLowerCase()
    return tokens.some((t) => haystack.includes(t))
  })
}

function formatEvidenceTime(value?: string): string {
  if (!value) return '—'
  const d = new Date(value)
  if (isNaN(d.getTime())) return '—'
  return formatRunDate(value)
}

function sortArtifacts(list: RelayArtifact[]): RelayArtifact[] {
  return [...list].sort((a, b) => {
    const ta = a.createdAt ? new Date(a.createdAt).getTime() : 0
    const tb = b.createdAt ? new Date(b.createdAt).getTime() : 0
    return ta - tb
  })
}

function formatJsonIfPossible(raw: string): string {
  const trimmed = raw.trim()
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      // fall through to raw
    }
  }
  return raw
}

function MetadataRow({
  label,
  value,
  mono = true,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className='flex items-baseline gap-2 text-xs'>
      <span className='w-28 shrink-0 text-muted-foreground'>{label}</span>
      <span
        className={cn(
          'min-w-0 break-all text-foreground',
          mono && 'font-mono',
        )}
      >
        {value}
      </span>
    </div>
  )
}

export function RunEvidenceBrowser({
  runId,
  artifacts,
  events,
  className,
}: RunEvidenceBrowserProps) {
  const [selectedId, setSelectedId] = React.useState<string | null>(null)

  const groupedArtifacts = React.useMemo(() => {
    const groups: Record<EvidenceStageKey, RelayArtifact[]> = {
      intake: [],
      compile: [],
      execute: [],
      audit: [],
      provenance: [],
    }
    for (const a of artifacts) {
      groups[getArtifactStage(a)].push(a)
    }
    for (const key of Object.keys(groups) as EvidenceStageKey[]) {
      groups[key] = sortArtifacts(groups[key])
    }
    return groups
  }, [artifacts])

  React.useEffect(() => {
    if (artifacts.length === 0) {
      if (selectedId !== null) setSelectedId(null)
      return
    }
    const exists = artifacts.some((a) => a.id === selectedId)
    if (exists) return
    for (const stage of EVIDENCE_STAGES) {
      const first = groupedArtifacts[stage.key][0]
      if (first) {
        setSelectedId(first.id)
        return
      }
    }
  }, [artifacts, groupedArtifacts, selectedId])

  const selectedArtifact = React.useMemo(
    () => artifacts.find((a) => a.id === selectedId) ?? null,
    [artifacts, selectedId],
  )

  const { data: selectedContent, isLoading: isLoadingContent, error: contentError } =
    useQuery({
      ...runArtifactContentQueryOptionsForArtifact(
        runId,
        selectedArtifact ?? FALLBACK_ARTIFACT,
      ),
      enabled: !!selectedArtifact,
    })

  if (artifacts.length === 0) {
    return (
      <div
        className={cn(
          'rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4',
          className,
        )}
      >
        <p className='font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground'>
          No evidence artifacts captured
        </p>
        <p className='mt-2 text-sm text-muted-foreground'>
          This run has no artifacts available yet.
        </p>
      </div>
    )
  }

  const verification = selectedArtifact
    ? deriveVerification(selectedArtifact)
    : null
  const relatedEvents = selectedArtifact
    ? getRelatedEvents(selectedArtifact, events)
    : []

  return (
    <div
      className={cn(
        'flex h-[calc(100dvh-14rem)] min-h-[440px] flex-row gap-3',
        className,
      )}
    >
      {/* Left column: grouped artifact list by stage */}
      <div className='flex w-[42%] min-w-[150px] flex-col gap-3 overflow-y-auto pr-1'>
        {EVIDENCE_STAGES.map((stage) => {
          const stageArtifacts = groupedArtifacts[stage.key]
          return (
            <div key={stage.key} className='flex flex-col gap-1'>
              <div
                className='flex items-center gap-1.5 px-1 py-0.5'
                title={stage.description}
              >
                <span className='font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground'>
                  {stage.label}
                </span>
                <span className='text-[10px] text-muted-foreground/50'>
                  {stageArtifacts.length}
                </span>
              </div>
              {stageArtifacts.length === 0 ? (
                <p className='px-2 py-1 text-[10px] italic text-muted-foreground/60'>
                  No artifacts captured for this stage.
                </p>
              ) : (
                stageArtifacts.map((a) => {
                  const isSelected = a.id === selectedId
                  const prod = deriveProducer(a)
                  const ver = deriveVerification(a)
                  const hash = compactHash(getArtifactHash(a))
                  return (
                    <button
                      key={a.id}
                      type='button'
                      onClick={() => setSelectedId(a.id)}
                      className={cn(
                        'flex flex-col gap-1 rounded border px-2 py-1.5 text-left transition-colors',
                        isSelected
                          ? 'border-[var(--relay-accent)] bg-[var(--relay-panel-hover-bg)]'
                          : 'border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] hover:bg-[var(--relay-panel-hover-bg)]',
                      )}
                    >
                      <span className='truncate text-xs font-medium text-foreground'>
                        {a.label || a.filename}
                      </span>
                      <div className='flex flex-wrap items-center gap-1.5 text-[10px] text-muted-foreground'>
                        <span className='font-mono'>{prod}</span>
                        <span className='opacity-40'>·</span>
                        <span
                          className={cn(
                            'flex items-center gap-0.5',
                            ver.className,
                          )}
                        >
                          {ver.icon}
                          {ver.label}
                        </span>
                      </div>
                      <div className='flex items-center gap-1.5 font-mono text-[10px] text-muted-foreground/70'>
                        <span>{formatEvidenceTime(a.createdAt)}</span>
                        {a.sizeHint && (
                          <span className='opacity-60'>({a.sizeHint})</span>
                        )}
                      </div>
                      <div className='truncate font-mono text-[10px] text-muted-foreground/60'>
                        {hash}
                      </div>
                    </button>
                  )
                })
              )}
            </div>
          )
        })}
      </div>

      {/* Right column: selected artifact detail tabs */}
      <div className='flex min-w-0 flex-1 flex-col'>
        {selectedArtifact && verification ? (
          <Tabs
            defaultValue='preview'
            className='flex h-full min-h-0 flex-col'
          >
            <TabsList
              variant='line'
              className='shrink-0 self-start'
            >
              <TabsTrigger value='preview'>Preview</TabsTrigger>
              <TabsTrigger value='event-chain'>Event Chain</TabsTrigger>
              <TabsTrigger value='metadata'>Metadata</TabsTrigger>
              <TabsTrigger value='raw'>Raw</TabsTrigger>
            </TabsList>

            {/* Preview tab */}
            <TabsContent
              value='preview'
              className='min-h-0 flex-1 overflow-y-auto'
            >
              <PreviewTab
                artifact={selectedArtifact}
                content={selectedContent}
                isLoading={isLoadingContent}
                error={contentError}
              />
            </TabsContent>

            {/* Event Chain tab */}
            <TabsContent
              value='event-chain'
              className='min-h-0 flex-1 overflow-y-auto'
            >
              {relatedEvents.length > 0 ? (
                <div className='flex flex-col gap-1.5'>
                  {relatedEvents.map((e) => (
                    <div
                      key={e.id}
                      className='flex flex-col gap-0.5 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 py-1.5'
                    >
                      <span className='font-mono text-[10px] text-muted-foreground/70'>
                        {formatEvidenceTime(e.createdAt)}
                      </span>
                      <span className='text-xs text-foreground'>
                        {e.message}
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <p className='p-2 text-xs text-muted-foreground'>
                  No related events were captured for this artifact.
                </p>
              )}
            </TabsContent>

            {/* Metadata tab */}
            <TabsContent
              value='metadata'
              className='min-h-0 flex-1 overflow-y-auto'
            >
              <div className='flex flex-col gap-1.5 p-1'>
                <MetadataRow label='ID' value={selectedArtifact.id || '—'} />
                <MetadataRow
                  label='Label'
                  value={selectedArtifact.label || '—'}
                  mono={false}
                />
                <MetadataRow
                  label='Filename'
                  value={selectedArtifact.filename || '—'}
                />
                <MetadataRow
                  label='Kind'
                  value={selectedArtifact.kind || '—'}
                />
                <MetadataRow
                  label='Storage Kind'
                  value={selectedArtifact.storageKind || '—'}
                />
                <MetadataRow
                  label='Producer'
                  value={deriveProducer(selectedArtifact)}
                  mono={false}
                />
                <MetadataRow
                  label='Verification'
                  value={verification.label}
                  mono={false}
                />
                <MetadataRow
                  label='Timestamp'
                  value={formatEvidenceTime(selectedArtifact.createdAt)}
                />
                <MetadataRow
                  label='Hash'
                  value={getArtifactHash(selectedArtifact)}
                />
                <MetadataRow
                  label='Size'
                  value={selectedArtifact.sizeHint || '—'}
                />
                <MetadataRow
                  label='Path'
                  value={selectedArtifact.path || '—'}
                />
                <MetadataRow
                  label='Content URL'
                  value={selectedArtifact.contentUrl || '—'}
                />
              </div>
            </TabsContent>

            {/* Raw tab */}
            <TabsContent
              value='raw'
              className='min-h-0 flex-1 overflow-y-auto'
            >
              <RawTab
                artifact={selectedArtifact}
                content={selectedContent}
                isLoading={isLoadingContent}
                error={contentError}
              />
            </TabsContent>
          </Tabs>
        ) : (
          <div className='flex flex-1 items-center justify-center p-4'>
            <p className='text-xs text-muted-foreground'>
              Select an artifact to view evidence details.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

function PreviewTab({
  artifact,
  content,
  isLoading,
  error,
}: {
  artifact: RelayArtifact
  content?: string | null
  isLoading: boolean
  error: unknown
}) {
  if (isLoading && !content) {
    return (
      <p className='p-2 text-xs text-muted-foreground'>
        Loading artifact content…
      </p>
    )
  }
  if (error && !content) {
    return (
      <div className='flex flex-col gap-1 p-2'>
        <p className='text-xs text-red-400'>
          Failed to load artifact content.
        </p>
        <p className='font-mono text-[10px] text-muted-foreground/70'>
          {(error as Error)?.message || 'Unknown error'}
        </p>
      </div>
    )
  }
  const raw = content || artifact.preview || ''
  if (!raw) {
    return (
      <p className='p-2 text-xs text-muted-foreground'>
        No preview available.
      </p>
    )
  }
  const formatted = formatJsonIfPossible(raw)
  return (
    <pre className='overflow-x-auto whitespace-pre-wrap break-words rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 font-mono text-[11px] leading-relaxed text-foreground'>
      {formatted}
    </pre>
  )
}

function RawTab({
  artifact,
  content,
  isLoading,
  error,
}: {
  artifact: RelayArtifact
  content?: string | null
  isLoading: boolean
  error: unknown
}) {
  if (isLoading && !content) {
    return (
      <p className='p-2 text-xs text-muted-foreground'>
        Loading artifact content…
      </p>
    )
  }
  if (error && !content) {
    return (
      <div className='flex flex-col gap-1 p-2'>
        <p className='text-xs text-red-400'>
          Failed to load artifact content.
        </p>
        <p className='font-mono text-[10px] text-muted-foreground/70'>
          {(error as Error)?.message || 'Unknown error'}
        </p>
        <pre className='mt-1 overflow-x-auto whitespace-pre-wrap break-words rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 font-mono text-[11px] leading-relaxed text-foreground'>
          {JSON.stringify(artifact, null, 2)}
        </pre>
      </div>
    )
  }
  const raw = content || JSON.stringify(artifact, null, 2)
  return (
    <pre className='overflow-x-auto whitespace-pre-wrap break-words rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 font-mono text-[11px] leading-relaxed text-foreground'>
      {raw}
    </pre>
  )
}
