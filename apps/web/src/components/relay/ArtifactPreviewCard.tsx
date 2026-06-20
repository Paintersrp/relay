import { useState } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { RelayArtifactPreview } from '@/features/relay-runs'
import { FileText, FileCode, GitMerge, ClipboardCheck, ShieldCheck, FileSearch } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ArtifactInspectorDialog } from './ArtifactInspectorDialog'

function getArtifactKindToken(artifact: RelayArtifactPreview): string {
  return (artifact.storageKind || artifact.kind || "").toLowerCase();
}

function getKindIcon(token: string): React.ReactNode {
  if (token.includes('validation')) {
    return <FileSearch className="w-4 h-4 text-orange-400" />
  }
  if (token.includes('git_diff') || token.includes('git_status')) {
    return <GitMerge className="w-4 h-4 text-pink-400" />
  }

  switch (token) {
    case 'prompt':
      return <FileText className="w-4 h-4 text-blue-400" />
    case 'handoff':
    case 'planner_handoff':
      return <FileCode className="w-4 h-4 text-violet-400" />
    case 'executor_result':
    case 'agent_result_raw':
    case 'codex_last_message':
    case 'result':
    case 'mcp_audit_handback':
      return <ClipboardCheck className="w-4 h-4 text-emerald-400" />
    case 'audit':
      return <ShieldCheck className="w-4 h-4 text-yellow-400" />
    case 'command_log':
      return <FileText className="w-4 h-4 text-slate-400" />
    case 'diff':
      return <GitMerge className="w-4 h-4 text-pink-400" />
    default:
      return <FileText className="w-4 h-4 text-slate-400" />
  }
}

function getKindLabel(token: string): string {
  if (token.includes('validation')) return 'Validation'
  if (token.includes('git_diff') || token.includes('git_status')) return 'Diff'

  switch (token) {
    case 'prompt':
      return 'Prompt'
    case 'handoff':
    case 'planner_handoff':
      return 'Handoff'
    case 'executor_result':
    case 'agent_result_raw':
    case 'codex_last_message':
    case 'result':
      return 'Result'
    case 'command_log':
      return 'Command Log'
    case 'executor_stdout':
      return 'Executor Stdout'
    case 'executor_stderr':
      return 'Executor Stderr'
    case 'audit':
      return 'Audit'
    case 'diff':
      return 'Diff'
    case 'parsed_frontmatter':
      return 'Frontmatter'
    case 'run_config':
      return 'Config'
    case 'mcp_audit_handback':
      return 'Handback'
    default:
      return token.charAt(0).toUpperCase() + token.slice(1).replace(/_/g, ' ')
  }
}

interface ArtifactPreviewCardProps {
  runId: string
  artifact: RelayArtifactPreview
  defaultOpen?: boolean
  className?: string
}

export function ArtifactPreviewCard({
  runId,
  artifact,
  defaultOpen = false,
  className,
}: ArtifactPreviewCardProps) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <>
      <Card
        onClick={() => setOpen(true)}
        className={cn(
          'border-border/60 bg-card/40 hover:bg-card/60 hover:border-purple-500/40 transition-all cursor-pointer group',
          className
        )}
      >
        <CardHeader className="p-3 pb-2">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              {getKindIcon(getArtifactKindToken(artifact))}
              <CardTitle className="text-sm font-medium truncate">{artifact.label}</CardTitle>
            </div>
            <Badge variant="outline" className="text-xs shrink-0 font-mono">
              {getKindLabel(getArtifactKindToken(artifact))}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="p-3 pt-0">
          <div className="flex items-center justify-between gap-2 mb-2">
            <p className="font-mono text-[10px] text-muted-foreground truncate">
              stored kind: {artifact.storageKind || artifact.kind}
            </p>
            {artifact.sizeHint && (
              <span className="text-[10px] text-muted-foreground shrink-0">{artifact.sizeHint}</span>
            )}
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={(e) => {
              e.stopPropagation()
              setOpen(true)
            }}
            className="w-full text-xs h-7 border-slate-800 hover:border-purple-500/40 hover:bg-purple-950/20 group-hover:bg-purple-950/25"
          >
            Inspect full artifact
          </Button>
        </CardContent>
      </Card>

      <ArtifactInspectorDialog
        runId={runId}
        artifact={artifact}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  )
}
