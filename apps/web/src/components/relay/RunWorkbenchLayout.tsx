import * as React from 'react'
import { Link } from '@tanstack/react-router'
import type { RelayRun } from '@/features/relay-runs'
import { formatRunDate, formatRunDateRelative } from '@/features/relay-runs'
import { Button } from '@/components/ui/button'
import { StatusBadge } from './StatusBadge'
import { RunStepper } from './RunStepper'
import { cn } from '@/lib/utils'
import { ArrowLeft } from 'lucide-react'

type InspectorPanelKey = 'logs' | 'artifacts' | 'validation' | 'audit'
type InspectorSize = 's' | 'm' | 'l'
type InspectorPanels = Partial<Record<InspectorPanelKey, React.ReactNode>>

interface RunWorkbenchLayoutProps {
  run: RelayRun
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

const INSPECTOR_SIZE_CLASS: Record<InspectorSize, string> = {
  s: 'lg:w-72',
  m: 'lg:w-96',
  l: 'lg:w-[30rem]',
}

function hasPanelContent(content: React.ReactNode): boolean {
  return content !== undefined && content !== null && content !== false
}

export function RunWorkbenchLayout({
  run,
  mainContent,
  sideContent,
  inspectorPanels,
  className,
}: RunWorkbenchLayoutProps) {
  const [inspectorSize, setInspectorSize] = React.useState<InspectorSize>('m')
  const [activeInspectorTab, setActiveInspectorTab] =
    React.useState<InspectorPanelKey>('logs')

  const isRunning =
    run.status === 'executor_dispatched' || run.status === 'executor_running'

  const resolvedPanels: InspectorPanels =
    inspectorPanels ?? (sideContent ? { logs: sideContent } : {})

  const visibleTabs = INSPECTOR_TABS.filter((tab) =>
    hasPanelContent(resolvedPanels[tab.key]),
  )

  const resolvedActiveTab = visibleTabs.some(
    (tab) => tab.key === activeInspectorTab,
  )
    ? activeInspectorTab
    : visibleTabs[0]?.key

  const activePanel = resolvedActiveTab ? resolvedPanels[resolvedActiveTab] : null

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
          activeStep={run.activeStep}
          isRunning={isRunning}
          className="px-4"
        />
      </div>

      {/* Full-height split pane */}
      <div className="flex min-h-0 flex-1 overflow-hidden">
        {/* Left: stage-specific main content */}
        <main className="min-w-0 flex-1 overflow-y-auto">
          <div className="px-4 py-4">{mainContent}</div>
        </main>

        {/* Right: run-local inspector */}
        {visibleTabs.length > 0 ? (
          <aside
            className={cn(
              'hidden min-h-0 shrink-0 flex-col border-l border-[var(--relay-row-border)] bg-[var(--relay-inspector-bg)] lg:flex',
              INSPECTOR_SIZE_CLASS[inspectorSize],
            )}
          >
            {/* Inspector tab bar */}
            <div className="flex h-10 shrink-0 items-center border-b border-[var(--relay-row-border)] px-3">
              <div className="flex min-w-0 flex-1 items-center gap-1">
                {visibleTabs.map((tab) => {
                  const active = tab.key === resolvedActiveTab
                  return (
                    <button
                      key={tab.key}
                      type="button"
                      onClick={() => setActiveInspectorTab(tab.key)}
                      className={cn(
                        'h-7 rounded px-2 font-mono text-[11px] transition-colors',
                        active
                          ? 'bg-[var(--relay-panel-hover-bg)] text-foreground'
                          : 'text-muted-foreground hover:text-foreground',
                      )}
                      aria-pressed={active}
                    >
                      {tab.label}
                    </button>
                  )
                })}
              </div>

              {/* S/M/L width controls */}
              <div className="ml-2 flex shrink-0 items-center rounded border border-[var(--relay-row-border)]">
                {(['s', 'm', 'l'] as const).map((size) => (
                  <button
                    key={size}
                    type="button"
                    onClick={() => setInspectorSize(size)}
                    className={cn(
                      'h-6 w-6 font-mono text-[10px] uppercase',
                      inspectorSize === size
                        ? 'bg-[var(--relay-panel-hover-bg)] text-foreground'
                        : 'text-muted-foreground hover:text-foreground',
                    )}
                    aria-pressed={inspectorSize === size}
                    aria-label={`Set inspector size ${size.toUpperCase()}`}
                  >
                    {size}
                  </button>
                ))}
              </div>
            </div>

            {/* Inspector panel content */}
            <div className="min-h-0 flex-1 overflow-y-auto p-3">
              <div className="flex flex-col gap-3">{activePanel}</div>
            </div>
          </aside>
        ) : null}
      </div>
    </section>
  )
}
