import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/lib/utils";

interface RelayMetaRowProps extends ComponentProps<"div"> {}

interface RelayMetaItemProps extends ComponentProps<"div"> {
  icon?: ReactNode;
  label?: ReactNode;
  mono?: boolean;
}

interface RelayMonoTextProps extends ComponentProps<"span"> {}

export function RelayMetaRow({ className, ...props }: RelayMetaRowProps) {
  return (
    <div
      className={cn("flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground", className)}
      {...props}
    />
  );
}

export function RelayMetaItem({
  icon,
  label,
  mono = false,
  className,
  children,
  ...props
}: RelayMetaItemProps) {
  return (
    <div className={cn("inline-flex items-center gap-1.5", className)} {...props}>
      {icon ? <span className="shrink-0">{icon}</span> : null}
      {label ? <span>{label}</span> : null}
      {children ? (
        <span className={cn(mono && "font-mono text-xs")}>{children}</span>
      ) : null}
    </div>
  );
}

export function RelayMonoText({ className, ...props }: RelayMonoTextProps) {
  return <span className={cn("font-mono text-xs", className)} {...props} />;
}
