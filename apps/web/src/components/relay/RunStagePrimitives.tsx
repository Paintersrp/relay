import * as React from "react";

import { cn } from "@/lib/utils";

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
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
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
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[var(--relay-row-border)] p-4">
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

      <div className={cn("min-w-0 p-4", contentClassName)}>{children}</div>
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
            <p className="min-w-0 flex-1 text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              {row.label}
            </p>
            {row.source ? (
              <span className="inline-flex shrink-0 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 py-0.5 text-[11px] text-muted-foreground">
                {row.source}
              </span>
            ) : null}
          </div>
          <div
            className={cn(
              "mt-2 min-w-0 break-words text-sm text-foreground",
              row.mono && "font-mono text-[13px]",
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
            <th className="px-3 py-2 text-left text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              Field
            </th>
            <th className="px-3 py-2 text-left text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
              Value
            </th>
            <th className="px-3 py-2 text-left text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
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
                  row.valueMono && "font-mono text-[13px]",
                )}
              >
                {row.value}
              </td>
              <td className="px-3 py-2.5">
                <span className="inline-flex rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)] px-2 py-0.5 text-[11px] text-muted-foreground">
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
