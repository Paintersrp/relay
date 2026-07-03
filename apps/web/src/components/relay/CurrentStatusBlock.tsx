import type { CurrentStatusView, Tone } from "@/features/relay-runs/runStatusTrackerViews";
import { formatRunDate, formatRunDateRelative } from "@/features/relay-runs";
import { cn } from "@/lib/utils";

// ============================================================
// Run Status Tracker Redesign — CurrentStatusBlock (Requirement 2)
// ============================================================
//
// The single, plain-language "where is it now" statement. This is the
// visual anchor of the page: exactly one `CurrentStatusView` renders
// here (`headline` as the prominent sentence, optional `detail`,
// `tone`-driven styling, `updatedAt` rendered as relative time). No
// other region on the page independently restates current-status
// prose (see design.md "Current_Status_Block").

const TONE_CLASS: Record<
  Tone,
  { border: string; text: string; bg: string }
> = {
  neutral: {
    border: "border-l-[var(--relay-row-border)]",
    text: "text-muted-foreground",
    bg: "bg-[var(--relay-panel-bg)]",
  },
  info: {
    border: "border-l-[var(--info)]",
    text: "text-[var(--info)]",
    bg: "bg-[var(--info)]/8",
  },
  success: {
    border: "border-l-[var(--success)]",
    text: "text-[var(--success)]",
    bg: "bg-[var(--success)]/8",
  },
  warning: {
    border: "border-l-[var(--warning)]",
    text: "text-[var(--warning)]",
    bg: "bg-[var(--warning)]/8",
  },
  danger: {
    border: "border-l-[var(--destructive)]",
    text: "text-[var(--destructive)]",
    bg: "bg-[var(--destructive)]/8",
  },
};

export interface CurrentStatusBlockProps {
  view: CurrentStatusView;
  className?: string;
}

export function CurrentStatusBlock({ view, className }: CurrentStatusBlockProps) {
  const toneClass = TONE_CLASS[view.tone];

  return (
    <section
      className={cn(
        "min-w-0 rounded border border-l-4 px-5 py-4",
        toneClass.border,
        toneClass.bg,
        className,
      )}
      data-testid="current-status-block"
      data-tone={view.tone}
    >
      <p
        className="text-lg font-semibold leading-snug text-foreground"
        data-testid="current-status-headline"
      >
        {view.headline}
      </p>

      {view.detail ? (
        <p
          className={cn("mt-1.5 text-sm leading-normal", toneClass.text)}
          data-testid="current-status-detail"
        >
          {view.detail}
        </p>
      ) : null}

      <p
        className="mt-2 text-xs text-muted-foreground"
        title={formatRunDate(view.updatedAt)}
        data-testid="current-status-updated-at"
      >
        Updated {formatRunDateRelative(view.updatedAt)}
      </p>
    </section>
  );
}
