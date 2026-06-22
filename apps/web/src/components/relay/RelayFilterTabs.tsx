import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

export interface RelayFilterTabItem {
  value: string;
  label: string;
  count?: number;
}

interface RelayFilterTabsProps {
  value: string;
  items: RelayFilterTabItem[];
  onValueChange: (value: string) => void;
  className?: string;
  listClassName?: string;
  triggerClassName?: string;
  countClassName?: string;
}

export function RelayFilterTabs({
  value,
  items,
  onValueChange,
  className,
  listClassName,
  triggerClassName,
  countClassName,
}: RelayFilterTabsProps) {
  return (
    <Tabs value={value} onValueChange={onValueChange} className={className}>
      <TabsList
        variant="line"
        className={cn(
          "w-full justify-start gap-3 overflow-x-auto rounded-none border-b border-[var(--relay-row-border)] px-0 pb-1",
          listClassName,
        )}
      >
        {items.map((item) => (
          <TabsTrigger
            key={item.value}
            value={item.value}
            className={cn(
              "shrink-0 rounded-none px-0 pb-2 text-xs font-medium",
              triggerClassName,
            )}
          >
            <span>{item.label}</span>
            {typeof item.count === "number" ? (
              <span
                data-active={item.value === value ? "" : undefined}
                className={cn(
                  "inline-flex items-center justify-center font-mono text-[10px] leading-none",
                  countClassName,
                )}
              >
                {item.count}
              </span>
            ) : null}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
