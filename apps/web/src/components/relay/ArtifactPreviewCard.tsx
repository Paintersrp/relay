import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import type { RelayArtifactPreview } from '@/features/relay-runs'
import { FileText, FileCode, GitMerge, ClipboardCheck, ShieldCheck, FileSearch } from 'lucide-react'
import { cn } from '@/lib/utils'

const KIND_ICON: Record<RelayArtifactPreview['kind'], React.ReactNode> = {
  prompt: <FileText className="w-4 h-4 text-blue-400" />,
  handoff: <FileCode className="w-4 h-4 text-violet-400" />,
  result: <ClipboardCheck className="w-4 h-4 text-emerald-400" />,
  audit: <ShieldCheck className="w-4 h-4 text-yellow-400" />,
  validation: <FileSearch className="w-4 h-4 text-orange-400" />,
  diff: <GitMerge className="w-4 h-4 text-pink-400" />,
}

const KIND_BADGE: Record<RelayArtifactPreview['kind'], string> = {
  prompt: 'Prompt',
  handoff: 'Handoff',
  result: 'Result',
  audit: 'Audit',
  validation: 'Validation',
  diff: 'Diff',
}

interface ArtifactPreviewCardProps {
  artifact: RelayArtifactPreview
  className?: string
}

export function ArtifactPreviewCard({ artifact, className }: ArtifactPreviewCardProps) {
  return (
    <Card className={cn('border-border/60 bg-card/40 hover:bg-card/60 transition-colors', className)}>
      <CardHeader className="p-3 pb-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            {KIND_ICON[artifact.kind]}
            <CardTitle className="text-sm font-medium truncate">{artifact.label}</CardTitle>
          </div>
          <Badge variant="outline" className="text-xs shrink-0 font-mono">
            {KIND_BADGE[artifact.kind]}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="p-3 pt-0">
        <div className="flex items-center justify-between gap-2">
          <p className="font-mono text-xs text-muted-foreground truncate">{artifact.path}</p>
          {artifact.sizeHint && (
            <span className="text-xs text-muted-foreground shrink-0">{artifact.sizeHint}</span>
          )}
        </div>
        {/* Pass 1: artifact viewing is mock/read-only. Download/view is Pass 3+. */}
        <p className="mt-1.5 text-xs text-muted-foreground/60 italic">
          Artifact stored on disk — preview available in Pass 3.
        </p>
      </CardContent>
    </Card>
  )
}
