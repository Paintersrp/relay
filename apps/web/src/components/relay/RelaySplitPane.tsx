import type { ReactNode } from "react";

import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

interface RelaySplitPaneProps {
  sidebar: ReactNode;
  children: ReactNode;
  sidebarWidthClassName?: string;
  className?: string;
  sidebarClassName?: string;
  contentClassName?: string;
}

export function RelaySplitPane({
  sidebar,
  children,
  sidebarWidthClassName = "w-72",
  className,
  sidebarClassName,
  contentClassName,
}: RelaySplitPaneProps) {
  return (
    <div
      className={cn(
        "flex min-h-0 flex-1 overflow-hidden rounded-lg border border-[var(--relay-splitpane-border)] bg-[var(--relay-inspector-bg)]",
        className
      )}
    >
      <ScrollArea
        className={cn(
          "min-h-0 shrink-0 bg-[var(--relay-splitpane-sidebar-bg)]",
          sidebarWidthClassName,
          sidebarClassName
        )}
      >
        {sidebar}
      </ScrollArea>
      <Separator orientation="vertical" className="bg-[var(--relay-splitpane-border)]" />
      <ScrollArea className={cn("min-h-0 flex-1 bg-[var(--relay-inspector-bg)]", contentClassName)}>
        {children}
      </ScrollArea>
    </div>
  );
}
