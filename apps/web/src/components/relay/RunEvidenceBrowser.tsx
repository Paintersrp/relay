import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import type { RelayArtifact, RelayRunEvent } from '@/features/relay-runs'
import {
  formatRunDate,
  runArtifactContentQueryOptionsForArtifact,
} from '@/features/relay-runs'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { RelayStateSurface } from '@/components/relay/RelayStateSurface'
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
      className: 'text-[var(--destructive)]',
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
      className: 'text-[var(--warning)]',
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
      className: 'text-[var(--success)]',
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

function ArtifactSummaryRow({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div>
      <dt className='font-mono text-[10px] uppercase tracking-[0.16em] text-muted-foreground'>
        {label}
      </dt>
      <dd
        className={cn(
          'mt-1 break-words text-xs text-foreground',
          mono && 'font-mono',
        )}
      >
        {value || '—'}
      </dd>
    </div>
  )
}

function ArtifactModalFrame({ children }: { children: React.ReactNode }) {
  return (
    <div className='flex h-full min-h-0 flex-col overflow-hidden rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]'>
      <div className='min-h-0 min-w-0 max-w-full flex-1 overflow-auto p-4'>
        {children}
      </div>
    </div>
  )
}

function EventChainTab({ events }: { events: RelayRunEvent[] }) {
  if (events.length === 0) {
    return (
      <p className='text-sm text-muted-foreground'>
        No related events were captured for this artifact.
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-2'>
      {events.map((event) => (
        <div
          key={event.id}
          className='rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2'
        >
          <p className='font-mono text-[10px] text-muted-foreground'>
            {formatEvidenceTime(event.createdAt)}
          </p>
          <p className='mt-1 text-xs text-foreground'>{event.message}</p>
        </div>
      ))}
    </div>
  )
}

function ArtifactMetadataTab({
  artifact,
  verificationLabel,
}: {
  artifact: RelayArtifact
  verificationLabel: string
}) {
  const rows: { label: string; value: string; mono?: boolean }[] = [
    { label: 'ID', value: artifact.id || '—', mono: true },
    { label: 'Label', value: artifact.label || '—' },
    { label: 'Filename', value: artifact.filename || '—', mono: true },
    { label: 'Kind', value: artifact.kind || '—', mono: true },
    { label: 'Storage Kind', value: artifact.storageKind || '—', mono: true },
    { label: 'Stage', value: getArtifactStageLabel(artifact) },
    { label: 'Producer', value: deriveProducer(artifact) },
    { label: 'Verification', value: verificationLabel },
    {
      label: 'Timestamp',
      value: formatEvidenceTime(artifact.createdAt),
      mono: true,
    },
    { label: 'Hash', value: getArtifactHash(artifact), mono: true },
    { label: 'Size', value: artifact.sizeHint || '—', mono: true },
    { label: 'Path', value: artifact.path || '—', mono: true },
    { label: 'Content URL', value: artifact.contentUrl || '—', mono: true },
  ]

  return (
    <div className='divide-y divide-[var(--relay-row-border)] rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]'>
      {rows.map((row) => (
        <div
          key={row.label}
          className='grid grid-cols-1 gap-2 px-3 py-2 sm:grid-cols-[160px_minmax(0,1fr)] sm:gap-4'
        >
          <span className='font-mono text-[10px] uppercase tracking-[0.16em] text-muted-foreground'>
            {row.label}
          </span>
          <span
            className={cn(
              'min-w-0 break-words text-xs text-foreground',
              row.mono && 'font-mono',
            )}
          >
            {row.value}
          </span>
        </div>
      ))}
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

  if (!artifact) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent showCloseButton={false}>
          <p className='text-sm text-muted-foreground'>No artifact selected.</p>
        </DialogContent>
      </Dialog>
    )
  }

  const title = artifact.label || artifact.filename || artifact.id
  const stageLabel = getArtifactStageLabel(artifact)
  const producer = deriveProducer(artifact)
  const timestamp = formatEvidenceTime(artifact.createdAt)
  const size = artifact.sizeHint || '—'
  const hash = getArtifactHash(artifact)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className='flex h-[min(82vh,calc(100vh-1rem))] max-h-[calc(100vh-1rem)] w-[calc(100vw-1rem)] !max-w-[1120px] flex-col gap-0 overflow-hidden rounded-lg border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-0 shadow-2xl sm:h-[min(82vh,calc(100vh-2rem))] sm:max-h-[calc(100vh-2rem)] sm:w-[calc(100vw-2rem)] sm:!max-w-[1120px]'
      >
        <DialogHeader className='shrink-0 border-b border-[var(--relay-row-border)] px-4 py-4 sm:px-6'>
          <div className='flex min-w-0 items-start justify-between gap-4'>
            <div className='min-w-0 flex-1'>
              <div className='flex min-w-0 items-center gap-3'>
                <DialogTitle className='truncate text-left text-base font-semibold leading-tight text-foreground'>
                  {title}
                </DialogTitle>
                {verification ? (
                  <span
                    className={cn(
                      'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 font-mono text-[10px]',
                      verification.className,
                      'border-current/40 bg-transparent',
                    )}
                  >
                    {verification.icon}
                    {verification.label}
                  </span>
                ) : null}
              </div>
              <DialogDescription className='mt-2 break-words text-left font-mono text-[11px] text-muted-foreground'>
                {stageLabel} <span className='opacity-45'>·</span> {producer}{' '}
                <span className='opacity-45'>·</span> {size}{' '}
                <span className='opacity-45'>·</span> {timestamp}
              </DialogDescription>
            </div>

            <DialogClose className='inline-flex h-8 w-8 shrink-0 items-center justify-center rounded border border-[var(--relay-row-border)] text-muted-foreground hover:bg-[var(--relay-panel-hover-bg)] hover:text-foreground'>
              <span aria-hidden='true'>×</span>
              <span className='sr-only'>Close</span>
            </DialogClose>
          </div>
        </DialogHeader>

        <div className='grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[280px_minmax(0,1fr)]'>
          <aside className='min-h-0 border-b border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-3 lg:border-r lg:border-b-0 lg:p-4'>
            <div className='rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3'>
              <p className='font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground'>
                Artifact
              </p>
              <p className='mt-2 break-words text-sm font-medium text-foreground'>
                {title}
              </p>
              <dl className='mt-3 space-y-2 lg:mt-4 lg:space-y-3'>
                <ArtifactSummaryRow label='Stage' value={stageLabel} />
                <ArtifactSummaryRow label='Producer' value={producer} />
                <ArtifactSummaryRow label='Time' value={timestamp} />
                <ArtifactSummaryRow label='Size' value={size} />
                <ArtifactSummaryRow label='Hash' value={hash} mono />
              </dl>
            </div>
          </aside>

          <Tabs defaultValue='preview' className='flex min-h-0 min-w-0 flex-col'>
            <div className='shrink-0 border-b border-[var(--relay-row-border)] px-3 pt-3 sm:px-5'>
              <div className='overflow-x-auto'>
                <TabsList variant='line' className='h-10 min-w-max gap-6'>
                <TabsTrigger value='preview' className='h-10 px-0 text-xs'>
                  Preview
                </TabsTrigger>
                <TabsTrigger value='event-chain' className='h-10 px-0 text-xs'>
                  Event Chain
                </TabsTrigger>
                <TabsTrigger value='metadata' className='h-10 px-0 text-xs'>
                  Metadata
                </TabsTrigger>
                <TabsTrigger value='raw' className='h-10 px-0 text-xs'>
                  Raw
                </TabsTrigger>
                </TabsList>
              </div>
            </div>

            <div className='min-h-0 min-w-0 flex-1 p-3 sm:p-5'>
              <TabsContent value='preview' className='mt-0 h-full min-w-0'>
                <ArtifactModalFrame>
                  <PreviewTab
                    artifact={artifact}
                    content={selectedContent}
                    isLoading={isLoadingContent}
                    error={contentError}
                  />
                </ArtifactModalFrame>
              </TabsContent>

              <TabsContent value='event-chain' className='mt-0 h-full min-w-0'>
                <ArtifactModalFrame>
                  <EventChainTab events={relatedEvents} />
                </ArtifactModalFrame>
              </TabsContent>

              <TabsContent value='metadata' className='mt-0 h-full min-w-0'>
                <ArtifactModalFrame>
                  <ArtifactMetadataTab
                    artifact={artifact}
                    verificationLabel={verification?.label || '—'}
                  />
                </ArtifactModalFrame>
              </TabsContent>

              <TabsContent value='raw' className='mt-0 h-full min-w-0'>
                <ArtifactModalFrame>
                  <RawTab
                    artifact={artifact}
                    content={selectedContent}
                    isLoading={isLoadingContent}
                    error={contentError}
                  />
                </ArtifactModalFrame>
              </TabsContent>
            </div>
          </Tabs>
        </div>
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
      <RelayStateSurface
        tone='empty'
        title='No artifacts captured'
        description='This run has no evidence artifacts available yet.'
        className={className}
      />
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
      <div className='font-mono text-xs leading-relaxed text-muted-foreground'>
        Loading artifact content…
      </div>
    )
  }

  if (error && !content) {
    return (
      <div>
        <p className='text-xs text-[var(--destructive)]'>Failed to load artifact content.</p>
        <p className='mt-1 font-mono text-[11px] text-muted-foreground/70'>
          {(error as Error)?.message || 'Unknown error'}
        </p>
      </div>
    )
  }

  const raw = content || artifact.preview || ''
  if (!raw) {
    return (
      <div className='font-mono text-xs leading-relaxed text-muted-foreground'>
        No preview available.
      </div>
    )
  }

  const formatted = formatJsonIfPossible(raw)

  return (
    <div className='min-w-0 max-w-full font-mono text-xs leading-relaxed text-foreground'>
      <pre className='max-w-full overflow-auto whitespace-pre-wrap break-words'>
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
      <div className='font-mono text-xs leading-relaxed text-muted-foreground'>
        Loading artifact content…
      </div>
    )
  }

  if (error && !content) {
    return (
      <div>
        <p className='text-xs text-[var(--destructive)]'>Failed to load artifact content.</p>
        <p className='mt-1 font-mono text-[11px] text-muted-foreground/70'>
          {(error as Error)?.message || 'Unknown error'}
        </p>
        <div className='mt-3 rounded border border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)]/60 p-3'>
          <pre className='max-w-full overflow-auto font-mono text-xs leading-relaxed text-foreground'>
            {JSON.stringify(artifact, null, 2)}
          </pre>
        </div>
      </div>
    )
  }

  const raw = content || JSON.stringify(artifact, null, 2)

  return (
    <div className='min-w-0 max-w-full overflow-auto font-mono text-xs leading-relaxed text-foreground'>
      <pre className='min-w-max max-w-full'>{raw}</pre>
    </div>
  )
}
