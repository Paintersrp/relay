import * as React from "react";
import { cn } from "@/lib/utils";

interface AppPageFrameProps {
  title: string;
  description?: string;
  leading?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  headerClassName?: string;
  bodyClassName?: string;
}

export function AppPageFrame({
  title,
  description,
  leading,
  actions,
  children,
  className,
  headerClassName,
  bodyClassName,
}: AppPageFrameProps) {
  return (
    <section
      className={cn(
        "flex min-h-0 flex-1 flex-col overflow-hidden bg-[var(--relay-page-body-bg)]",
        className,
      )}
    >
      <header
        className={cn(
          "shrink-0 border-b border-[var(--relay-page-border)] bg-[var(--relay-page-header-bg)] px-4 py-4",
          headerClassName,
        )}
      >
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 flex-1 items-start gap-3">
            {leading && <div className="shrink-0">{leading}</div>}
            <div className="min-w-0 flex-1">
              <h1 className="truncate text-lg font-semibold leading-tight text-[var(--relay-page-title-fg)]">
                {title}
              </h1>
              {description && (
                <p className="mt-1 text-sm leading-normal text-[var(--relay-page-description-fg)]">
                  {description}
                </p>
              )}
            </div>
          </div>
          {actions && (
            <div className="flex w-full flex-wrap justify-start gap-2 sm:w-auto sm:justify-end">
              {actions}
            </div>
          )}
        </div>
      </header>

      <div className={cn("min-h-0 flex-1 overflow-y-auto p-6", bodyClassName)}>
        {children}
      </div>
    </section>
  );
}
