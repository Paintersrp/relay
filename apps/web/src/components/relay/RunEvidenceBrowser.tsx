import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import type { RelayArtifact, RelayRunEvent } from '@/features/relay-runs'
import {
  formatRunDate,
  runArtifactContentQueryOptionsForArtifact,
} from '@/features/relay-runs'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import { CheckCircle2, Clock, FileText, XCircle } from 'lucide-react'

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
    .filter((value): value is string => typeof value === 'string' && value.length > 0)
    .join(' ')
    .toLowerCase()
}

function getArtifactStage(artifact: RelayArtifact): EvidenceStageKey {
  const identity = artifactIdentity(artifact)
  for (const stage of EVIDENCE_STAGES) {
    if (STAGE_KEYWORDS[stage.key].some((keyword) => identity.includes(keyword))) {
      return stage.key
    }
  }
  return 'provenance'
}

function getArtifactStageLabel(artifact: RelayArtifact): string {
  return (
    EVIDENCE_STAGES.find((stage) => stage.key === getArtifactStage(artifact))?.label ??
    'Provenance'
  )
}

function deriveProducer(artifact: RelayArtifact): string {
  const identity = artifactIdentity(artifact)
  if (
    identity.includes('git_diff') ||
    identity.includes('git_status') ||
    identity.includes('provenance') ||
    identity.includes('diff')
  ) {
    return 'Provenance'
  }
  if (
    identity.includes('audit') ||
    identity.includes('closeout') ||
    identity.includes('decision') ||
    identity.includes('mcp_audit_handback')
  ) {
    return 'Auditor'
  }
  if (
    identity.includes('validation') ||
    identity.includes('packet_validation') ||
    identity.includes('brief_validation') ||
    identity.includes('intake_validation')
  ) {
    return 'Validator'
  }
  if (
    identity.includes('handoff') ||
    identity.includes('planner_handoff') ||
    identity.includes('frontmatter') ||
    identity.includes('run_config') ||
    identity.includes('intake')
  ) {
    return 'Intake'
  }
  if (
    identity.includes('canonical_packet') ||
    identity.includes('executor_brief') ||
    identity.includes('packet') ||
    identity.includes('brief') ||
    identity.includes('compile') ||
    identity.includes('render') ||
    identity.includes('repair')
  ) {
    return 'Compiler'
  }
  if (
    identity.includes('executor_result') ||
    identity.includes('agent_result') ||
    identity.includes('command_log') ||
    identity.includes('executor_stdout') ||
    identity.includes('executor_stderr') ||
    identity.includes('stdout') ||
    identity.includes('stderr') ||
    identity.includes('execute')
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
  const looseArtifact = artifact as RelayArtifact & Record<string, unknown>
  const direct =
    looseArtifact.sha256 ||
    looseArtifact.hash ||
    looseArtifact.digest ||
    looseArtifact.checksum ||
    looseArtifact.artifactHash

  if (typeof direct === 'string' && direct.length > 0) {
    return direct
  }

  if (typeof artifact.preview === 'string') {
    const match = artifact.preview.match(
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
  if (hash.length > 16) return `${hash.slice(0, 12)}…`
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
    .filter((token): token is string => typeof token === 'string' && token.length > 0)
    .map((token) => token.toLowerCase())

  if (tokens.length === 0) {
    return []
  }

  return events.filter((event) => {
    const haystack = `${event.message} ${
      event.details ? JSON.stringify(event.details) : ''
    }`.toLowerCase()
    return tokens.some((token) => haystack.includes(token))
  })
}

function formatEvidenceTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return formatRunDate(value)
}

function sortArtifacts(list: RelayArtifact[]): RelayArtifact[] {
  const stageOrder = new Map(
    EVIDENCE_STAGES.map((stage, index) => [stage.key, index] as const),
  )

  return [...list].sort((left, right) => {
    const leftStage = stageOrder.get(getArtifactStage(left)) ?? EVIDENCE_STAGES.length
    const rightStage =
      stageOrder.get(getArtifactStage(right)) ?? EVIDENCE_STAGES.length

    if (leftStage !== rightStage) {
      return leftStage - rightStage
    }

    const leftTime = left.createdAt ? new Date(left.createdAt).getTime() : 0
    const rightTime = right.createdAt ? new Date(right.createdAt).getTime() : 0

    if (leftTime !== rightTime) {
      return leftTime - rightTime
    }

    return (left.label || left.filename || left.id).localeCompare(
      right.label || right.filename || right.id,
    )
  })
}

function formatJsonIfPossible(raw: string): string {
  const trimmed = raw.trim()
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      return raw
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
    <div className='grid gap-1 rounded border border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)]/30 px-3 py-2 text-xs'>
      <span className='font-mono text-[10px] uppercase tracking-[0.14em] text-muted-foreground'>
        {label}
      </span>
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

function EvidenceArtifactRow({
  artifact,
  selected,
  onClick,
}: {
  artifact: RelayArtifact
  selected: boolean
  onClick: () => void
}) {
  const verification = deriveVerification(artifact)
  const name = artifact.label || artifact.filename || artifact.id || 'Unnamed artifact'
  const stageLabel = getArtifactStageLabel(artifact)
  const producer = deriveProducer(artifact)
  const metadata = [
    stageLabel,
    producer,
    artifact.sizeHint || null,
    compactHash(getArtifactHash(artifact)),
    formatEvidenceTime(artifact.createdAt),
  ].filter(Boolean) as string[]

  return (
    <button
      type='button'
      onClick={onClick}
      className={cn(
        'w-full rounded border px-3 py-2 text-left transition-colors',
        selected
          ? 'border-[var(--relay-accent)] bg-[var(--relay-panel-hover-bg)]'
          : 'border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] hover:bg-[var(--relay-panel-hover-bg)]',
      )}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <p className='truncate text-xs font-medium text-foreground' title={name}>
            {name}
          </p>
          <p className='mt-1 truncate font-mono text-[10px] text-muted-foreground'>
            {stageLabel} · {producer}
          </p>
        </div>
        <span
          className={cn(
            'flex shrink-0 items-center gap-0.5 font-mono text-[10px]',
            verification.className,
          )}
        >
          {verification.icon}
          {verification.label}
        </span>
      </div>

      <p className='mt-1 truncate font-mono text-[10px] text-muted-foreground/80'>
        {metadata.join(' · ')}
      </p>
    </button>
  )
}

function EvidenceArtifactDialog({
  runId,
  artifact,
  events,
  open,
  onOpenChange,
}: {
  runId: string
  artifact: RelayArtifact | null
  events: RelayRunEvent[]
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const selectedArtifact = artifact ?? FALLBACK_ARTIFACT
  const verification = artifact ? deriveVerification(artifact) : null
  const artifactHash = artifact ? getArtifactHash(artifact) : '—'
  const relatedEvents = React.useMemo(
    () => getRelatedEvents(artifact, events),
    [artifact, events],
  )

  const {
    data: selectedContent,
    isLoading: isLoadingContent,
    error: contentError,
  } = useQuery({
    ...runArtifactContentQueryOptionsForArtifact(runId, selectedArtifact),
    enabled: open && !!artifact,
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='grid max-h-[84vh] min-h-[480px] w-[min(92vw,1040px)] max-w-none grid-rows-[auto_auto_minmax(0,1fr)] gap-0 overflow-hidden border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)] p-0'>
        {artifact ? (
          <>
            <DialogHeader className='shrink-0 border-b border-[var(--relay-row-border)] px-5 py-4'>
              <div className='flex min-w-0 items-start justify-between gap-4 pr-8'>
                <div className='min-w-0'>
                  <DialogTitle className='truncate text-left text-base font-semibold text-foreground'>
                    {artifact.label || artifact.filename || artifact.id}
                  </DialogTitle>
                  <DialogDescription className='mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-left font-mono text-[11px] text-muted-foreground'>
                    <span>{getArtifactStageLabel(artifact)}</span>
                    <span className='text-muted-foreground/40'>·</span>
                    <span>{deriveProducer(artifact)}</span>
                    <span className='text-muted-foreground/40'>·</span>
                    <span>{artifact.sizeHint || '—'}</span>
                    <span className='text-muted-foreground/40'>·</span>
                    <span>{formatEvidenceTime(artifact.createdAt)}</span>
                    <span className='text-muted-foreground/40'>·</span>
                    <span>{compactHash(artifactHash)}</span>
                  </DialogDescription>
                </div>
                {verification ? (
                  <span
                    className={cn(
                      'mt-0.5 flex shrink-0 items-center gap-1 rounded-full border border-current/30 px-2 py-0.5 font-mono text-[10px]',
                      verification.className,
                    )}
                  >
                    {verification.icon}
                    {verification.label}
                  </span>
                ) : null}
              </div>
            </DialogHeader>

            <Tabs
              defaultValue='preview'
              className='row-span-2 grid min-h-0 grid-rows-[auto_minmax(0,1fr)]'
            >
              <div className='shrink-0 border-b border-[var(--relay-row-border)] px-5 py-2'>
                <TabsList variant='line' className='grid h-9 w-full grid-cols-4'>
                  <TabsTrigger value='preview' className='text-xs'>
                    Preview
                  </TabsTrigger>
                  <TabsTrigger value='event-chain' className='text-xs'>
                    Event Chain
                  </TabsTrigger>
                  <TabsTrigger value='metadata' className='text-xs'>
                    Metadata
                  </TabsTrigger>
                  <TabsTrigger value='raw' className='text-xs'>
                    Raw
                  </TabsTrigger>
                </TabsList>
              </div>

              <div className='min-h-0 overflow-hidden px-5 py-4'>
                <TabsContent value='preview' className='mt-0 h-full'>
                  <PreviewTab
                    artifact={artifact}
                    content={selectedContent}
                    isLoading={isLoadingContent}
                    error={contentError}
                  />
                </TabsContent>

                <TabsContent
                  value='event-chain'
                  className='mt-0 h-full'
                >
                  {relatedEvents.length > 0 ? (
                    <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3'>
                      <div className='flex flex-col gap-2'>
                        {relatedEvents.map((event) => (
                          <div
                            key={event.id}
                            className='rounded border border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)]/60 px-3 py-2'
                          >
                            <p className='font-mono text-[10px] text-muted-foreground/70'>
                              {formatEvidenceTime(event.createdAt)}
                            </p>
                            <p className='mt-0.5 text-xs text-foreground'>
                              {event.message}
                            </p>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : (
                    <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4'>
                      <p className='text-xs text-muted-foreground'>
                        No related events were captured for this artifact.
                      </p>
                    </div>
                  )}
                </TabsContent>

                <TabsContent value='metadata' className='mt-0 h-full'>
                  <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4'>
                    <div className='grid gap-3 md:grid-cols-2'>
                      <MetadataRow label='ID' value={artifact.id || '—'} />
                      <MetadataRow
                        label='Label'
                        value={artifact.label || '—'}
                        mono={false}
                      />
                      <MetadataRow
                        label='Filename'
                        value={artifact.filename || '—'}
                      />
                      <MetadataRow label='Kind' value={artifact.kind || '—'} />
                      <MetadataRow
                        label='Storage Kind'
                        value={artifact.storageKind || '—'}
                      />
                      <MetadataRow
                        label='Stage'
                        value={getArtifactStageLabel(artifact)}
                        mono={false}
                      />
                      <MetadataRow
                        label='Producer'
                        value={deriveProducer(artifact)}
                        mono={false}
                      />
                      <MetadataRow
                        label='Verification'
                        value={verification?.label || '—'}
                        mono={false}
                      />
                      <MetadataRow
                        label='Timestamp'
                        value={formatEvidenceTime(artifact.createdAt)}
                      />
                      <MetadataRow label='Hash' value={artifactHash} />
                      <MetadataRow label='Size' value={artifact.sizeHint || '—'} />
                      <MetadataRow label='Path' value={artifact.path || '—'} />
                      <MetadataRow
                        label='Content URL'
                        value={artifact.contentUrl || '—'}
                      />
                    </div>
                  </div>
                </TabsContent>

                <TabsContent value='raw' className='mt-0 h-full'>
                  <RawTab
                    artifact={artifact}
                    content={selectedContent}
                    isLoading={isLoadingContent}
                    error={contentError}
                  />
                </TabsContent>
              </div>
            </Tabs>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

export function RunEvidenceBrowser({
  runId,
  artifacts,
  events,
  className,
}: RunEvidenceBrowserProps) {
  const [selectedId, setSelectedId] = React.useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = React.useState(false)

  const sortedArtifacts = React.useMemo(() => sortArtifacts(artifacts), [artifacts])

  React.useEffect(() => {
    if (sortedArtifacts.length === 0) {
      if (selectedId !== null) setSelectedId(null)
      if (dialogOpen) setDialogOpen(false)
      return
    }

    const selectedStillExists = sortedArtifacts.some(
      (artifact) => artifact.id === selectedId,
    )

    if (!selectedStillExists) {
      setSelectedId(sortedArtifacts[0].id)
    }
  }, [dialogOpen, selectedId, sortedArtifacts])

  const selectedArtifact = React.useMemo(
    () =>
      sortedArtifacts.find((artifact) => artifact.id === selectedId) ?? null,
    [selectedId, sortedArtifacts],
  )

  const handleArtifactClick = React.useCallback((artifact: RelayArtifact) => {
    setSelectedId(artifact.id)
    setDialogOpen(true)
  }, [])

  if (artifacts.length === 0) {
    return (
      <div
        className={cn(
          'flex h-full min-h-0 flex-col rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4',
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

  return (
    <>
      <div className={cn('flex h-full min-h-0 flex-col', className)}>
        <div className='shrink-0 border-b border-[var(--relay-row-border)] pb-2'>
          <div className='flex items-center justify-between gap-2'>
            <p className='font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground'>
              Artifacts
            </p>
            <span className='font-mono text-[10px] text-muted-foreground'>
              {sortedArtifacts.length}
            </span>
          </div>
        </div>

        <div className='min-h-0 flex-1 overflow-y-auto pt-2 pr-1'>
          <div className='flex flex-col gap-1.5'>
            {sortedArtifacts.map((artifact) => (
              <EvidenceArtifactRow
                key={artifact.id}
                artifact={artifact}
                selected={artifact.id === selectedId}
                onClick={() => handleArtifactClick(artifact)}
              />
            ))}
          </div>
        </div>
      </div>

      <EvidenceArtifactDialog
        runId={runId}
        artifact={selectedArtifact}
        events={events}
        open={dialogOpen && !!selectedArtifact}
        onOpenChange={setDialogOpen}
      />
    </>
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
      <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 font-mono text-xs leading-relaxed text-muted-foreground'>
        Loading artifact content…
      </div>
    )
  }

  if (error && !content) {
    return (
      <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4'>
        <p className='text-xs text-red-400'>Failed to load artifact content.</p>
        <p className='mt-1 font-mono text-[11px] text-muted-foreground/70'>
          {(error as Error)?.message || 'Unknown error'}
        </p>
      </div>
    )
  }

  const raw = content || artifact.preview || ''
  if (!raw) {
    return (
      <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 font-mono text-xs leading-relaxed text-muted-foreground'>
        No preview available.
      </div>
    )
  }

  const formatted = formatJsonIfPossible(raw)

  return (
    <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 font-mono text-xs leading-relaxed text-foreground'>
      <pre className='whitespace-pre-wrap break-words'>
        {formatted}
      </pre>
    </div>
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
      <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 font-mono text-xs leading-relaxed text-muted-foreground'>
        Loading artifact content…
      </div>
    )
  }

  if (error && !content) {
    return (
      <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4'>
        <p className='text-xs text-red-400'>Failed to load artifact content.</p>
        <p className='mt-1 font-mono text-[11px] text-muted-foreground/70'>
          {(error as Error)?.message || 'Unknown error'}
        </p>
        <div className='mt-3 rounded border border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)]/60 p-3'>
          <pre className='overflow-auto font-mono text-xs leading-relaxed text-foreground'>
            {JSON.stringify(artifact, null, 2)}
          </pre>
        </div>
      </div>
    )
  }

  const raw = content || JSON.stringify(artifact, null, 2)

  return (
    <div className='h-full min-h-[260px] overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 font-mono text-xs leading-relaxed text-foreground'>
      <pre className='min-w-max'>
        {raw}
      </pre>
    </div>
  )
}
