import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Badge } from '@/components/ui/badge'
import type { RelayLogPreview } from '@/features/relay-runs'
import { RelayInlineState } from '@/components/relay/RelayStateSurface'
import { Terminal } from 'lucide-react'
import { cn } from '@/lib/utils'

interface LogPreviewPanelProps {
  logPreview: RelayLogPreview
  className?: string
}

export function LogPreviewPanel({ logPreview, className }: LogPreviewPanelProps) {
  return (
    <Card className={cn('min-w-0 border-border/60', className)}>
      <CardHeader className="p-3 pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <Terminal className="w-4 h-4 text-muted-foreground" />
          Log Preview
          {logPreview.truncated && (
            <Badge variant="secondary" className="ml-auto text-xs">Truncated</Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0">
        {logPreview.lines.length === 0 ? (
          <RelayInlineState
            tone="empty"
            title="No logs captured yet"
            description="Run events will appear here after Relay records stage activity."
            density="compact"
          />
        ) : (
          <ScrollArea className="h-36 w-full rounded-md border border-border/50 bg-[var(--relay-code-bg)]">
            <div className="min-w-0 p-3 font-mono text-xs">
              <div className="overflow-x-auto">
                <div className="min-w-max space-y-0.5">
                  {logPreview.lines.map((line, i) => (
                    <div
                      key={i}
                      className="text-emerald-300/80 leading-relaxed whitespace-pre"
                    >
                      {line}
                    </div>
                  ))}
                  {logPreview.truncated && (
                    <div className="text-muted-foreground/50 italic">
                      … output truncated. Inspect the run logs or artifacts for full output.
                    </div>
                  )}
                </div>
              </div>
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  )
}
