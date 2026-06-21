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
    <Card className={cn('border-border/60', className)}>
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
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
          />
        ) : (
          <ScrollArea className="h-36 w-full rounded-md border border-border/50 bg-black/30">
            <div className="p-3 font-mono text-xs space-y-0.5">
              {logPreview.lines.map((line, i) => (
                <div key={i} className="text-emerald-300/80 leading-relaxed whitespace-pre-wrap break-all">
                  {line}
                </div>
              ))}
              {logPreview.truncated && (
                <div className="text-muted-foreground/50 italic">
                  … output truncated. Full log available in Pass 3.
                </div>
              )}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  )
}
