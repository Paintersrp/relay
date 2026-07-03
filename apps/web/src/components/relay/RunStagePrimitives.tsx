import * as React from "react";
import {
  AlertCircle,
  CheckCircle2,
  Circle,
  Clock3,
  Loader2,
  MinusCircle,
  XCircle,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import {
  getRunStageStepLabel,
  getRunStageStepStatus,
  isRunStageStepAttention,
  type RunStageStepDefinition,
  type RunStageStepStatus,
  type RunStageStepStatusMap,
} from "./runStageVisualState";

export type {
  RunStageStepDefinition,
  RunStageStepStatus,
  RunStageStepStatusMap,
} from "./runStageVisualState";

export type RunStageTone =
  | "default"
  | "info"
  | "success"
  | "warning"
  | "danger";

const TONE_CLASS: Record<
  RunStageTone,
  { border: string; text: string; bg: string }
> = {
  default: {
    border: "border-[var(--relay-row-border)]",
    text: "text-muted-foreground",
    bg: "bg-[var(--relay-panel-bg)]",
  },
  info: {
    border: "border-[var(--info)]/35",
    text: "text-[var(--info)]",
    bg: "bg-[var(--info)]/10",
  },
  success: {
    border: "border-[var(--success)]/35",
    text: "text-[var(--success)]",
    bg: "bg-[var(--success)]/10",
  },
  warning: {
    border: "border-[var(--warning)]/35",
    text: "text-[var(--warning)]",
    bg: "bg-[var(--warning)]/10",
  },
  danger: {
    border: "border-[var(--destructive)]/35",
    text: "text-[var(--destructive)]",
    bg: "bg-[var(--destructive)]/10",
  },
};

const STEP_STATUS_CLASS: Record<
  RunStageStepStatus,
  { dot: string; row: string; icon: React.ReactNode; badge?: React.ReactNode }
> = {
  success: {
    dot: "text-[var(--success)]",
    row: "border-[var(--success)]/35 bg-[var(--success)]/10",
    icon: <CheckCircle2 className="size-4" />,
    badge: <Badge variant="success">Complete</Badge>,
  },
  active: {
    dot: "text-[var(--relay-accent)]",
    row: "border-[var(--relay-accent)]/35 bg-[var(--relay-accent)]/10",
    icon: <Circle className="size-4 fill-current" />,
    badge: <Badge variant="info">Ready</Badge>,
  },
  running: {
    dot: "text-[var(--relay-accent)]",
    row: "border-[var(--relay-accent)]/35 bg-[var(--relay-accent)]/10",
    icon: <Loader2 className="size-4 animate-spin" />,
    badge: <Badge variant="running">Running</Badge>,
  },
  blocked: {
    dot: "text-[var(--warning)]",
    row: "border-[var(--warning)]/35 bg-[var(--warning)]/10",
    icon: <AlertCircle className="size-4" />,
    badge: <Badge variant="warning">Blocked</Badge>,
  },
  failed: {
    dot: "text-[var(--destructive)]",
    row: "border-[var(--destructive)]/35 bg-[var(--destructive)]/10",
    icon: <XCircle className="size-4" />,
    badge: <Badge variant="destructive">Failed</Badge>,
  },
  accepted: {
    dot: "text-[var(--success)]",
    row: "border-[var(--success)]/35 bg-[var(--success)]/10",
    icon: <CheckCircle2 className="size-4" />,
    badge: <Badge variant="success">Accepted</Badge>,
  },
  warning: {
    dot: "text-[var(--warning)]",
    row: "border-[var(--warning)]/35 bg-[var(--warning)]/10",
    icon: <AlertCircle className="size-4" />,
    badge: <Badge variant="warning">Accepted w/ warnings</Badge>,
  },
  revision: {
    dot: "text-[var(--warning)]",
    row: "border-[var(--warning)]/35 bg-[var(--warning)]/10",
    icon: <AlertCircle className="size-4" />,
    badge: <Badge variant="warning">Revision required</Badge>,
  },
  waiting: {
    dot: "text-muted-foreground/45",
    row: "border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
    icon: <Clock3 className="size-4" />,
  },
  na: {
    dot: "text-muted-foreground/45",
    row: "border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40",
    icon: <MinusCircle className="size-4" />,
    badge: <Badge variant="outline">n/a</Badge>,
  },
};

export interface RunStageSummaryChipProps {
  label?: React.ReactNode;
  value: React.ReactNode;
  tone?: RunStageTone;
  mono?: boolean;
  className?: string;
}

export function RunStageSummaryChip({
  label,
  value,
  tone = "default",
  mono = false,
  className,
}: RunStageSummaryChipProps) {
  const toneClass = TONE_CLASS[tone];

  return (
    <div
      className={cn(
        "inline-flex min-w-0 max-w-full items-center gap-1.5 rounded border px-2.5 py-1 text-xs",
        toneClass.border,
        toneClass.bg,
        className,
      )}
    >
      {label ? (
        <span className="shrink-0 text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
          {label}
        </span>
      ) : null}
      <span
        className={cn(
          "min-w-0 max-w-full truncate break-words",
          mono ? "font-mono" : "font-medium",
          tone === "default" ? "text-foreground" : toneClass.text,
        )}
      >
        {value}
      </span>
    </div>
  );
}

export interface RunStageSummaryCardProps {
  eyebrow: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  icon?: React.ReactNode;
  status?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
}

export function RunStageSummaryCard({
  eyebrow,
  title,
  description,
  icon,
  status,
  children,
  className,
}: RunStageSummaryCardProps) {
  return (
    <section
      className={cn(
        "min-w-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3",
        className,
      )}
    >
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-start gap-3">
            {icon ? (
              <div className="mt-0.5 shrink-0 text-muted-foreground">{icon}</div>
            ) : null}
            <div className="min-w-0 flex-1">
              <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                {eyebrow}
              </p>
              <p className="mt-1 text-sm font-semibold text-foreground">{title}</p>
              {description ? (
                <div className="mt-1 text-xs text-muted-foreground">
                  {description}
                </div>
              ) : null}
            </div>
          </div>
        </div>
        {status ? <div className="shrink-0">{status}</div> : null}
      </div>

      {children ? <div className="mt-3 min-w-0">{children}</div> : null}
    </section>
  );
}

export interface RunStageStateCardProps {
  tone?: RunStageTone;
  eyebrow: React.ReactNode;
  title: React.ReactNode;
  message?: React.ReactNode;
  action?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
}

export function RunStageStateCard({
  tone = "default",
  eyebrow,
  title,
  message,
  action,
  children,
  className,
}: RunStageStateCardProps) {
  const toneClass = TONE_CLASS[tone];

  return (
    <section
      className={cn(
        "min-w-0 rounded border border-l-4 bg-[var(--relay-panel-bg)] px-4 py-3",
        toneClass.border,
        className,
      )}
    >
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <RunStageSectionLabel className={toneClass.text}>
            {eyebrow}
          </RunStageSectionLabel>
          <h3 className="mt-1 text-sm font-semibold text-foreground">{title}</h3>
          {message ? (
            <div className="mt-1 text-sm text-muted-foreground">{message}</div>
          ) : null}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
      {children ? <div className="mt-3 min-w-0">{children}</div> : null}
    </section>
  );
}

export interface RunStagePipelineProps {
  steps: RunStageStepDefinition[];
  statuses: RunStageStepStatusMap;
  className?: string;
}

export function RunStagePipeline({
  steps,
  statuses,
  className,
}: RunStagePipelineProps) {
  return (
    <ol className={cn("flex min-w-0 flex-col gap-2", className)}>
      {steps.map((step) => (
        <RunStagePipelineRow
          key={step.id}
          step={step}
          status={getRunStageStepStatus(statuses, step.id)}
        />
      ))}
    </ol>
  );
}

export interface RunStagePipelineRowProps {
  step: RunStageStepDefinition;
  status: RunStageStepStatus;
  className?: string;
}

export function RunStagePipelineRow({
  step,
  status,
  className,
}: RunStagePipelineRowProps) {
  const statusClass = STEP_STATUS_CLASS[status];
  const statusLabel = getRunStageStepLabel(status);
  const helper = status === "na" ? step.naNote : step.helperText;
  const showHelper = helper && (status === "na" || isRunStageStepAttention(status));

  return (
    <li
      className={cn(
        "grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-3 rounded border px-3 py-2.5",
        statusClass.row,
        className,
      )}
    >
      <span
        className={cn(
          "mt-0.5 flex size-5 shrink-0 items-center justify-center",
          statusClass.dot,
        )}
        aria-hidden="true"
      >
        {statusClass.icon}
      </span>

      <div className="min-w-0">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <p className="min-w-0 text-sm font-medium text-foreground">
            {step.label}
          </p>
          {statusLabel && !statusClass.badge ? (
            <span className="text-xs text-muted-foreground">{statusLabel}</span>
          ) : null}
        </div>
        {showHelper ? (
          <div className="mt-1 text-xs text-muted-foreground">{helper}</div>
        ) : null}
      </div>

      {statusClass.badge ? (
        <div className="shrink-0">{statusClass.badge}</div>
      ) : null}
    </li>
  );
}

export interface RunStageInspectorSectionProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
  contentClassName?: string;
}

export function RunStageInspectorSection({
  title,
  description,
  actions,
  children,
  className,
  contentClassName,
}: RunStageInspectorSectionProps) {
  return (
    <section
      className={cn(
        "min-w-0 rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
        className,
      )}
    >
      <div className="flex min-w-0 items-start justify-between gap-2 border-b border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2">
        <div className="min-w-0">
          <h3 className="text-xs font-semibold text-foreground">{title}</h3>
          {description ? (
            <div className="mt-1 text-xs text-muted-foreground">
              {description}
            </div>
          ) : null}
        </div>
        {actions ? <div className="shrink-0">{actions}</div> : null}
      </div>
      {children ? (
        <div className={cn("min-w-0 px-3 py-2", contentClassName)}>
          {children}
        </div>
      ) : null}
    </section>
  );
}

export interface RunStageKeyValueRowProps {
  label: React.ReactNode;
  value?: React.ReactNode;
  children?: React.ReactNode;
  mono?: boolean;
  stacked?: boolean;
  className?: string;
}

export function RunStageKeyValueRow({
  label,
  value,
  children,
  mono,
  stacked,
  className,
}: RunStageKeyValueRowProps) {
  const body =
    value && children ? (
      <>
        {value}
        {children}
      </>
    ) : (
      children ?? value
    );

  if (!body) {
    return null;
  }

  return (
    <div
      className={cn(
        "min-w-0 gap-2 py-1",
        stacked ? "flex flex-col" : "grid grid-cols-[4.5rem_minmax(0,1fr)]",
        className,
      )}
    >
      <dt className="text-[10px] font-medium uppercase tracking-normal text-muted-foreground">
        {label}
      </dt>
      <dd
        className={cn(
          "min-w-0 text-xs text-foreground",
          mono ? "font-mono" : "font-medium",
        )}
      >
        {body}
      </dd>
    </div>
  );
}

export interface RunStageSectionLabelProps {
  children: React.ReactNode;
  className?: string;
}

export function RunStageSectionLabel({
  children,
  className,
}: RunStageSectionLabelProps) {
  return (
    <p
      className={cn(
        "text-[10px] font-semibold uppercase tracking-normal text-muted-foreground",
        className,
      )}
    >
      {children}
    </p>
  );
}

export interface RunStageHeaderProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  status?: React.ReactNode;
  className?: string;
}

export function RunStageHeader({
  title,
  description,
  status,
  className,
}: RunStageHeaderProps) {
  return (
    <div
      className={cn(
        "border-b border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3",
        className,
      )}
    >
      <div className="flex min-w-0 items-center justify-between gap-3">
        <div className="min-w-0">
          <RunStageSectionLabel className="text-[var(--relay-accent)]">
            {title}
          </RunStageSectionLabel>
          {description ? (
            <p className="mt-1 text-sm text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {status ? <div className="shrink-0">{status}</div> : null}
      </div>
    </div>
  );
}

export interface RunStageSectionProps {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  icon?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  contentClassName?: string;
}

export function RunStageSection({
  title,
  subtitle,
  icon,
  actions,
  children,
  className,
  contentClassName,
}: RunStageSectionProps) {
  return (
    <section
      className={cn(
        "min-w-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--relay-row-border)] p-5">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-start gap-3">
            {icon ? (
              <div className="mt-0.5 shrink-0 text-muted-foreground">{icon}</div>
            ) : null}
            <div className="min-w-0 flex-1">
              <p className="text-sm font-semibold text-foreground">{title}</p>
              {subtitle ? (
                <div className="mt-1 text-xs text-muted-foreground">
                  {subtitle}
                </div>
              ) : null}
            </div>
          </div>
        </div>
        {actions ? <div className="shrink-0">{actions}</div> : null}
      </div>

      <div className={cn("min-w-0 p-5", contentClassName)}>{children}</div>
    </section>
  );
}

export interface RunStageKeyValueRow {
  label: React.ReactNode;
  value: React.ReactNode;
  source?: React.ReactNode;
  mono?: boolean;
  emphasis?: boolean;
}

export interface RunStageKeyValueGridProps {
  rows: RunStageKeyValueRow[];
  columns?: 1 | 2;
  className?: string;
}

export function RunStageKeyValueGrid({
  rows,
  columns = 1,
  className,
}: RunStageKeyValueGridProps) {
  return (
    <div
      className={cn(
        "grid min-w-0 gap-3",
        columns === 2 && "md:grid-cols-2",
        className,
      )}
    >
      {rows.map((row, index) => (
        <div
          key={index}
          className="min-w-0 rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2.5"
        >
          <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
            <p className="min-w-0 flex-1 text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              {row.label}
            </p>
            {row.source ? (
              <span className="inline-flex shrink-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 py-0.5 text-xs text-muted-foreground">
                {row.source}
              </span>
            ) : null}
          </div>
          <div
            className={cn(
              "mt-2 min-w-0 break-words text-sm text-foreground",
              row.mono && "font-mono text-[14px]",
              row.emphasis && "font-semibold",
            )}
          >
            {row.value}
          </div>
        </div>
      ))}
    </div>
  );
}

export interface RunStagePreviewBlockProps {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  value?: string;
  children?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
  maxHeightClassName?: string;
}

export function RunStagePreviewBlock({
  title,
  subtitle,
  value,
  children,
  action,
  className,
  maxHeightClassName,
}: RunStagePreviewBlockProps) {
  return (
    <section
      className={cn(
        "min-w-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-foreground">{title}</p>
          {subtitle ? (
            <div className="mt-1 text-xs text-muted-foreground">{subtitle}</div>
          ) : null}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>

      <div className="p-4">
        <div
          className={cn(
            "min-w-0 overflow-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-code-bg)] p-3",
            maxHeightClassName ?? "max-h-48",
          )}
        >
          {children ? (
            <div className="min-w-0 font-mono text-xs whitespace-pre-wrap break-words text-foreground">
              {children}
            </div>
          ) : (
            <pre className="min-w-0 font-mono text-xs whitespace-pre-wrap break-words text-foreground">
              {value ?? ""}
            </pre>
          )}
        </div>
      </div>
    </section>
  );
}

export interface RunStageProvenanceRow {
  field: React.ReactNode;
  value: React.ReactNode;
  source: React.ReactNode;
  valueMono?: boolean;
}

export interface RunStageProvenanceTableProps {
  rows: RunStageProvenanceRow[];
  className?: string;
}

export function RunStageProvenanceTable({
  rows,
  className,
}: RunStageProvenanceTableProps) {
  return (
    <div
      className={cn(
        "min-w-0 overflow-x-auto rounded border border-[var(--relay-row-border)]",
        className,
      )}
    >
      <table className="min-w-[40rem] w-full border-collapse text-sm">
        <thead className="bg-[var(--surface-inset)]/60">
          <tr>
            <th className="px-3 py-2 text-left text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              Field
            </th>
            <th className="px-3 py-2 text-left text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              Value
            </th>
            <th className="px-3 py-2 text-left text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              Source
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr
              key={index}
              className="border-t border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] align-top"
            >
              <td className="px-3 py-2.5 text-sm text-muted-foreground">
                {row.field}
              </td>
              <td
                className={cn(
                  "px-3 py-2.5 text-sm text-foreground",
                  row.valueMono && "font-mono text-[14px]",
                )}
              >
                {row.value}
              </td>
              <td className="px-3 py-2.5">
                <span className="inline-flex rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)] px-2 py-0.5 text-xs text-muted-foreground">
                  {row.source}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export interface RunStageContentSectionProps {
  eyebrow?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  status?: React.ReactNode;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
  contentClassName?: string;
}

export function RunStageContentSection({
  eyebrow,
  title,
  description,
  status,
  actions,
  children,
  className,
  contentClassName,
}: RunStageContentSectionProps) {
  return (
    <section
      className={cn(
        "min-w-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]",
        className,
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--relay-row-border)] p-4">
        <div className="min-w-0 flex-1">
          {eyebrow ? (
            <p className="text-[10px] font-semibold uppercase tracking-normal text-muted-foreground mb-1">
              {eyebrow}
            </p>
          ) : null}
          <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
            {title}
            {status ? <span className="shrink-0">{status}</span> : null}
          </h3>
          {description ? (
            <p className="mt-1 text-xs text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {actions ? <div className="shrink-0 flex items-center gap-2">{actions}</div> : null}
      </div>
      {children ? (
        <div className={cn("min-w-0 p-4", contentClassName)}>{children}</div>
      ) : null}
    </section>
  );
}

export interface RunStageEvidenceRowProps {
  icon?: React.ReactNode;
  label: React.ReactNode;
  value?: React.ReactNode;
  status?: React.ReactNode;
  className?: string;
}

export function RunStageEvidenceRow({
  icon,
  label,
  value,
  status,
  className,
}: RunStageEvidenceRowProps) {
  return (
    <div
      className={cn(
        "flex items-center justify-between p-2 bg-[var(--surface-inset)]/40 rounded border border-[var(--relay-row-border)] text-xs font-mono min-w-0 gap-3",
        className,
      )}
    >
      <div className="flex items-center gap-2 min-w-0">
        {icon ? <span className="text-muted-foreground shrink-0">{icon}</span> : null}
        <span className="text-muted-foreground truncate">{label}</span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        {value ? <span className="text-foreground">{value}</span> : null}
        {status ?? null}
      </div>
    </div>
  );
}

export interface RunStageEvidenceListProps {
  children: React.ReactNode;
  className?: string;
}

export function RunStageEvidenceList({
  children,
  className,
}: RunStageEvidenceListProps) {
  return <div className={cn("flex flex-col gap-2", className)}>{children}</div>;
}

export interface RunStageFindingRowProps {
  severity: "warning" | "error" | "info" | "default";
  code?: React.ReactNode;
  message: React.ReactNode;
  className?: string;
}

export function RunStageFindingRow({
  severity,
  code,
  message,
  className,
}: RunStageFindingRowProps) {
  const toneMap: Record<RunStageFindingRowProps["severity"], RunStageTone> = {
    error: "danger",
    warning: "warning",
    info: "info",
    default: "default",
  };
  const toneClass = TONE_CLASS[toneMap[severity]];
  return (
    <div
      className={cn(
        "rounded border bg-[var(--relay-panel-bg)] p-3 flex flex-col gap-2",
        toneClass.border,
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <RunStageSummaryChip
          value={severity}
          tone={toneMap[severity]}
          className="capitalize"
        />
        {code ? (
          <span className="font-mono text-xs text-muted-foreground">
            {code}
          </span>
        ) : null}
      </div>
      <p className="text-xs text-foreground leading-normal">{message}</p>
    </div>
  );
}

export interface RunStageFindingListProps {
  children: React.ReactNode;
  className?: string;
}

export function RunStageFindingList({
  children,
  className,
}: RunStageFindingListProps) {
  return <div className={cn("flex flex-col gap-2", className)}>{children}</div>;
}

export interface RunStageActivityRowProps {
  timestamp: React.ReactNode;
  message: React.ReactNode;
  className?: string;
}

export function RunStageActivityRow({
  timestamp,
  message,
  className,
}: RunStageActivityRowProps) {
  return (
    <div
      className={cn(
        "flex items-start gap-3 py-1.5 text-xs border-b border-[var(--relay-row-border)] last:border-b-0 leading-relaxed text-foreground/85 font-mono",
        className,
      )}
    >
      <span className="text-muted-foreground shrink-0 select-none">
        {timestamp}
      </span>
      <span className="min-w-0 flex-1 break-words">{message}</span>
    </div>
  );
}

export interface RunStageActivityListProps {
  children: React.ReactNode;
  className?: string;
}

export function RunStageActivityList({
  children,
  className,
}: RunStageActivityListProps) {
  return <div className={cn("flex flex-col", className)}>{children}</div>;
}

export interface RunStageMainStackProps {
  children: React.ReactNode;
  className?: string;
}

export function RunStageMainStack({
  children,
  className,
}: RunStageMainStackProps) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-4 max-w-5xl mx-auto w-full", className)}>
      {children}
    </div>
  );
}

