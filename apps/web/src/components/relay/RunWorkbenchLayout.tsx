import * as React from 'react'
import { Link } from '@tanstack/react-router'
import type { RelayRun, RelayRunStep } from '@/features/relay-runs'
import { formatRunDate, formatRunDateRelative } from '@/features/relay-runs'
import { Button } from '@/components/ui/button'
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '@/components/ui/resizable'
import { StatusBadge } from './StatusBadge'
import { RunStepper } from './RunStepper'
import { RelayStateSurface } from './RelayStateSurface'
import {
  RunPlanContextCard,
  RunPlanContextHeader,
  hasRunPlanContext,
} from './RunPlanContext'
import {
  RunStageHeader,
  RunStageInspectorTabStrip,
} from './RunStagePrimitives'
import { cn } from '@/lib/utils'
import { ArrowLeft } from 'lucide-react'

type InspectorPanelKey =
  | 'details'
  | 'source'
  | 'logs'
  | 'artifacts'
  | 'validation'
  | 'audit'
type InspectorPanels = Partial<Record<InspectorPanelKey, React.ReactNode>>
type InspectorTabConfig = {
  key: InspectorPanelKey
  label: string
}

interface RunWorkbenchLayoutProps {
  run: RelayRun
  currentStep?: RelayRunStep
  stageActions?: React.ReactNode
  /** Left/main content area (step-specific sections) */
  mainContent: React.ReactNode
  /** Legacy fallback: stacked side content. Placed under the `logs` panel when `inspectorPanels` is not provided. */
  sideContent?: React.ReactNode
  /** Run-local inspector panels. When provided, overrides `sideContent`. */
  inspectorPanels?: InspectorPanels
  inspectorTabs?: InspectorTabConfig[]
  initialInspectorTab?: InspectorPanelKey
  className?: string
}

const DEFAULT_INSPECTOR_TABS: InspectorTabConfig[] = [
  { key: 'logs', label: 'Logs' },
  { key: 'artifacts', label: 'Artifacts' },
  { key: 'validation', label: 'Validation' },
  { key: 'audit', label: 'Audit' },
]

const STAGE_COPY: Record<RelayRunStep, { title: string; description: string }> = {
  intake: {
    title: 'Intake',
    description: 'Review incoming handoff metadata before compile.',
  },
  prepare: {
    title: 'Compile / Render',
    description: 'Compile the canonical packet and render executor artifacts.',
  },
  execute: {
    title: 'Execute',
    description: 'Dispatch the executor and monitor run output.',
  },
  audit: {
    title: 'Audit',
    description: 'Review validation evidence and close out the run.',
  },
}

function hasPanelContent(content: React.ReactNode): boolean {
  return content !== undefined && content !== null && content !== false
}

function getInspectorFallback(tab: InspectorPanelKey): React.ReactNode {
  const fallbackCopy: Record<
    InspectorPanelKey,
    { title: string; description: string }
  > = {
    details: {
      title: 'No intake details available',
      description: 'Relay has not captured detailed intake context for this run yet.',
    },
    source: {
      title: 'No source context recorded',
      description:
        'Relay has not stored planner provenance, source snapshot metadata, or context packet visibility for this run yet.',
    },
    logs: {
      title: 'No logs captured',
      description: 'Relay has not recorded log events for this run yet.',
    },
    artifacts: {
      title: 'No artifacts captured',
      description:
        'Artifacts will appear after intake, compile, execute, or audit stages produce evidence.',
    },
    validation: {
      title: 'Validation not run',
      description:
        'Validation results will appear after Relay captures validation output.',
    },
    audit: {
      title: 'No audit data',
      description:
        'Audit evidence and decisions will appear when the run reaches audit.',
    },
  }

  const copy = fallbackCopy[tab]
  return (
    <RelayStateSurface
      tone="empty"
      title={copy.title}
      description={copy.description}
      className="bg-[var(--relay-inspector-bg)]"
    />
  )
}

export function RunWorkbenchLayout({
  run,
  currentStep,
  stageActions,
  mainContent,
  sideContent,
  inspectorPanels,
  inspectorTabs,
  initialInspectorTab,
  className,
}: RunWorkbenchLayoutProps) {
  const resolvedTabs = React.useMemo(
    () =>
      inspectorTabs && inspectorTabs.length > 0
        ? inspectorTabs
        : DEFAULT_INSPECTOR_TABS,
    [inspectorTabs],
  )
  const [activeInspectorTab, setActiveInspectorTab] =
    React.useState<InspectorPanelKey>(
      () => initialInspectorTab ?? resolvedTabs[0]?.key ?? 'logs',
    )

  const isRunning =
    run.status === 'executor_dispatched' || run.status === 'executor_running'

  const activeShellStep = currentStep ?? run.activeStep
  const activeStageCopy = STAGE_COPY[activeShellStep]

  const resolvedPanels: InspectorPanels =
    inspectorPanels ?? (sideContent ? { logs: sideContent } : {})

  React.useEffect(() => {
    if (!resolvedTabs.some((tab) => tab.key === activeInspectorTab)) {
      setActiveInspectorTab(resolvedTabs[0]?.key ?? 'logs')
    }
  }, [activeInspectorTab, resolvedTabs])

  const resolvedActiveTab = resolvedTabs.some(
    (tab) => tab.key === activeInspectorTab,
  )
    ? activeInspectorTab
    : resolvedTabs[0]?.key

  const activePanelContent = resolvedActiveTab
    ? resolvedPanels[resolvedActiveTab]
    : null
  const activePanel =
    resolvedActiveTab && hasPanelContent(activePanelContent)
      ? activePanelContent
      : resolvedActiveTab
        ? getInspectorFallback(resolvedActiveTab)
        : null

  return (
    <section
      className={cn(
        'flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-[var(--relay-content-bg)]',
        className,
      )}
    >
      {/* Compact run header */}
      <header className="shrink-0 border-b border-[var(--relay-row-border)] bg-[var(--relay-page-header-bg)]">
        <div className="flex min-h-[4.5rem] items-center justify-between gap-4 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                asChild
                className="-ml-2 h-8 shrink-0 gap-1.5 px-2 text-sm"
              >
                <Link to="/runs">
                  <ArrowLeft className="h-4 w-4" />
                  Runs
                </Link>
              </Button>
              <h1 className="truncate text-lg font-semibold leading-tight text-foreground">
                {run.title}
              </h1>
              <StatusBadge status={run.status} className="shrink-0" />
            </div>

            <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 font-mono text-xs text-muted-foreground">
              <span>{run.id}</span>
              {run.packetId ? (
                <>
                  <span className="text-muted-foreground/40">•</span>
                  <span className="truncate">{run.packetId}</span>
                </>
              ) : null}
              <span className="text-muted-foreground/40">•</span>
              <span className="truncate">{run.repo}</span>
              {run.branch ? (
                <>
                  <span className="text-muted-foreground/40">•</span>
                  <span className="truncate">{run.branch}</span>
                </>
              ) : null}
            </div>
            {hasRunPlanContext(run.planContext) ? (
              <RunPlanContextHeader context={run.planContext} />
            ) : null}
          </div>

          <div className="hidden shrink-0 items-center gap-4 text-right text-xs text-muted-foreground lg:flex">
            <span className="font-mono">{run.executor}</span>
            <span className="font-medium" title={formatRunDate(run.updatedAt)}>
              Updated {formatRunDateRelative(run.updatedAt)}
            </span>
          </div>
        </div>
      </header>

      {/* Stage rail */}
      <div className="shrink-0 border-b border-[var(--relay-row-border)]">
        <div className="flex min-h-12 min-w-0 flex-wrap items-center justify-between gap-3 px-4">
          <RunStepper
            runId={run.id}
            activeStep={activeShellStep}
            isRunning={isRunning}
            className="min-w-0 flex-1 px-0"
          />
          {stageActions ? (
            <div className="flex shrink-0 items-center gap-2">{stageActions}</div>
          ) : null}
        </div>
      </div>

      {/* Full-height split pane */}
      <ResizablePanelGroup
        orientation="horizontal"
        className="min-h-0 flex-1 overflow-hidden"
      >
        <ResizablePanel
          id="run-workbench-main"
          defaultSize="72%"
          minSize="45%"
          className="min-w-0"
        >
          <main className="h-full min-w-0 overflow-y-auto">
            <RunStageHeader
              title={activeStageCopy.title}
              description={activeStageCopy.description}
              status={<StatusBadge status={run.status} className="shrink-0" />}
            />
            <div className="min-w-0 px-6 py-5">
              {mainContent}

              <section className="mt-4 lg:hidden">
                <div className="overflow-hidden rounded border border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)]">
                  <div className="border-b border-[var(--relay-row-border)] px-3 pt-2">
                    <RunStageInspectorTabStrip
                      tabs={resolvedTabs}
                      activeTab={resolvedActiveTab}
                      onTabChange={setActiveInspectorTab}
                    />
                  </div>
                  <div className="p-4">
                    <div className="flex min-w-0 flex-col gap-3">
                      {resolvedActiveTab === 'details' &&
                      hasRunPlanContext(run.planContext) ? (
                        <RunPlanContextCard context={run.planContext} />
                      ) : null}
                      {activePanel}
                    </div>
                  </div>
                </div>
              </section>
            </div>
          </main>
        </ResizablePanel>

        <>
          <ResizableHandle
            withHandle
            className="hidden bg-[var(--relay-row-border)] lg:flex"
          />

          <ResizablePanel
            id="run-workbench-inspector"
            defaultSize="28%"
            minSize="20%"
            maxSize="42%"
            className="hidden min-h-0 lg:flex"
          >
            <aside className="flex h-full min-h-0 w-full flex-col border-l border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)]">
              <div className="flex h-12 shrink-0 items-center border-b border-[var(--relay-row-border)] px-3">
                <RunStageInspectorTabStrip
                  tabs={resolvedTabs}
                  activeTab={resolvedActiveTab}
                  onTabChange={setActiveInspectorTab}
                  className="min-w-0 flex-1"
                />
              </div>

              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                <div className="flex flex-col gap-3">
                  {resolvedActiveTab === 'details' &&
                  hasRunPlanContext(run.planContext) ? (
                    <RunPlanContextCard context={run.planContext} />
                  ) : null}
                  {activePanel}
                </div>
              </div>
            </aside>
          </ResizablePanel>
        </>
      </ResizablePanelGroup>
    </section>
  )
}
