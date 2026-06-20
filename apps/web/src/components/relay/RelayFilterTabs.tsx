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
}

export function RelayFilterTabs({
  value,
  items,
  onValueChange,
  className,
}: RelayFilterTabsProps) {
  return (
    <Tabs value={value} onValueChange={onValueChange} className={className}>
      <TabsList
        variant="line"
        className="w-full justify-start gap-3 overflow-x-auto rounded-none border-b border-[var(--relay-row-border)] px-0 pb-1"
      >
        {items.map((item) => (
          <TabsTrigger
            key={item.value}
            value={item.value}
            className="shrink-0 rounded-none px-0 pb-2 font-mono text-xs"
          >
            <span>{item.label}</span>
            {typeof item.count === "number" ? (
              <span className={cn("text-[11px] text-muted-foreground")}>
                {item.count}
              </span>
            ) : null}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
