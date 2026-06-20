import type { ComponentType, ReactNode } from "react";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

export interface RelayPanelTabItem {
  value: string;
  label: string;
  icon?: ComponentType<{ className?: string }>;
  content: ReactNode;
}

interface RelayPanelTabsProps {
  value?: string;
  defaultValue?: string;
  items: RelayPanelTabItem[];
  onValueChange?: (value: string) => void;
  className?: string;
}

export function RelayPanelTabs({
  value,
  defaultValue,
  items,
  onValueChange,
  className,
}: RelayPanelTabsProps) {
  const initialValue = value ?? defaultValue ?? items[0]?.value;

  return (
    <Tabs
      value={value}
      defaultValue={initialValue}
      onValueChange={onValueChange}
      className={cn("min-h-0", className)}
    >
      <TabsList
        variant="line"
        className="w-full justify-start gap-2 overflow-x-auto rounded-none border-b border-[var(--relay-row-border)] px-0 pb-1"
      >
        {items.map((item) => {
          const Icon = item.icon;

          return (
            <TabsTrigger
              key={item.value}
              value={item.value}
              className="shrink-0 rounded-none px-0 pb-2 font-mono text-xs"
            >
              {Icon ? <Icon className="size-3.5" /> : null}
              <span>{item.label}</span>
            </TabsTrigger>
          );
        })}
      </TabsList>
      {items.map((item) => (
        <TabsContent key={item.value} value={item.value} className="min-h-0">
          {item.content}
        </TabsContent>
      ))}
    </Tabs>
  );
}
