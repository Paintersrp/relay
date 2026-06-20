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
import { cn } from '@/lib/utils'
import { ArrowLeft } from 'lucide-react'

type InspectorPanelKey = 'logs' | 'artifacts' | 'validation' | 'audit'
type InspectorPanels = Partial<Record<InspectorPanelKey, React.ReactNode>>

interface RunWorkbenchLayoutProps {
  run: RelayRun
  currentStep?: RelayRunStep
  /** Left/main content area (step-specific sections) */
  mainContent: React.ReactNode
  /** Legacy fallback: stacked side content. Placed under the `logs` panel when `inspectorPanels` is not provided. */
  sideContent?: React.ReactNode
  /** Run-local inspector panels. When provided, overrides `sideContent`. */
  inspectorPanels?: InspectorPanels
  className?: string
}

const INSPECTOR_TABS: { key: InspectorPanelKey; label: string }[] = [
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
  const label = INSPECTOR_TABS.find((item) => item.key === tab)?.label ?? tab
  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3">
      <p className="font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
        {label}
      </p>
      <p className="mt-2 text-sm text-muted-foreground">
        No {label.toLowerCase()} data is available for this run.
      </p>
    </div>
  )
}

export function RunWorkbenchLayout({
  run,
  currentStep,
  mainContent,
  sideContent,
  inspectorPanels,
  className,
}: RunWorkbenchLayoutProps) {
  const [activeInspectorTab, setActiveInspectorTab] =
    React.useState<InspectorPanelKey>('logs')

  const isRunning =
    run.status === 'executor_dispatched' || run.status === 'executor_running'

  const activeShellStep = currentStep ?? run.activeStep
  const activeStageCopy = STAGE_COPY[activeShellStep]

  const resolvedPanels: InspectorPanels =
    inspectorPanels ?? (sideContent ? { logs: sideContent } : {})

  const visibleTabs = INSPECTOR_TABS

  const resolvedActiveTab = visibleTabs.some(
    (tab) => tab.key === activeInspectorTab,
  )
    ? activeInspectorTab
    : visibleTabs[0]?.key

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
        <div className="flex min-h-16 items-center justify-between gap-4 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              <Button
                variant="ghost"
                size="sm"
                asChild
                className="-ml-2 h-7 shrink-0 gap-1.5 px-2 text-xs"
              >
                <Link to="/runs">
                  <ArrowLeft className="h-3.5 w-3.5" />
                  Runs
                </Link>
              </Button>
              <h1 className="truncate text-base font-semibold leading-tight text-foreground">
                {run.title}
              </h1>
              <StatusBadge status={run.status} className="shrink-0" />
            </div>

            <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] text-muted-foreground">
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
          </div>

          <div className="hidden shrink-0 items-center gap-4 text-right font-mono text-[11px] text-muted-foreground lg:flex">
            <span>{run.executor}</span>
            <span title={formatRunDate(run.updatedAt)}>
              Updated {formatRunDateRelative(run.updatedAt)}
            </span>
          </div>
        </div>
      </header>

      {/* Stage rail */}
      <div className="shrink-0 border-b border-[var(--relay-row-border)]">
        <RunStepper
          runId={run.id}
          activeStep={activeShellStep}
          isRunning={isRunning}
          className="px-4"
        />
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
            <div className="border-b border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3">
              <div className="flex min-w-0 items-center justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="font-mono text-sm font-semibold text-[var(--relay-accent)]">
                    {activeStageCopy.title}
                  </h2>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {activeStageCopy.description}
                  </p>
                </div>
                <StatusBadge status={run.status} className="shrink-0" />
              </div>
            </div>
            <div className="px-4 py-4">{mainContent}</div>
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
              <div className="flex h-10 shrink-0 items-center border-b border-[var(--relay-row-border)] px-3">
                <div className="flex min-w-0 flex-1 items-center gap-4">
                  {visibleTabs.map((tab) => {
                    const active = tab.key === resolvedActiveTab
                    return (
                      <button
                        key={tab.key}
                        type="button"
                        onClick={() => setActiveInspectorTab(tab.key)}
                        className={cn(
                          'flex h-10 items-center border-b-2 font-mono text-[11px] transition-colors',
                          active
                            ? 'border-[var(--relay-accent)] text-foreground'
                            : 'border-transparent text-muted-foreground hover:text-foreground',
                        )}
                        aria-pressed={active}
                      >
                        {tab.label}
                      </button>
                    )
                  })}
                </div>
              </div>

              <div className="min-h-0 flex-1 overflow-y-auto p-3">
                <div className="flex flex-col gap-3">{activePanel}</div>
              </div>
            </aside>
          </ResizablePanel>
        </>
      </ResizablePanelGroup>
    </section>
  )
}
