import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { runArtifactContentQueryOptions, API_BASE_URL, type RelayArtifact } from '@/features/relay-runs'
import { Copy, Check, ExternalLink, Loader2, FileText, AlertCircle } from 'lucide-react'

interface ArtifactInspectorDialogProps {
  runId: string
  artifact: RelayArtifact
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ArtifactInspectorDialog({
  runId,
  artifact,
  open,
  onOpenChange,
}: ArtifactInspectorDialogProps) {
  const [copied, setCopied] = useState(false)

  const { data: content, isLoading, error } = useQuery({
    ...runArtifactContentQueryOptions(runId, artifact.kind),
    enabled: open && !!runId && !!artifact?.kind,
  })

  useEffect(() => {
    if (copied) {
      const timer = setTimeout(() => setCopied(false), 2000)
      return () => clearTimeout(timer)
    }
  }, [copied])

  const handleCopy = async () => {
    if (!content) return
    try {
      await navigator.clipboard.writeText(content)
      setCopied(true)
    } catch (err) {
      console.error('Failed to copy to clipboard', err)
    }
  }

  // Format content logic
  const getFormattedContent = () => {
    if (!content) return ''
    try {
      // If it looks like JSON or artifact kind suggests it, try parsing it
      if (
        artifact.kind.endsWith('json') ||
        artifact.filename.endsWith('.json') ||
        content.trim().startsWith('{') ||
        content.trim().startsWith('[')
      ) {
        const parsed = JSON.parse(content)
        return JSON.stringify(parsed, null, 2)
      }
    } catch {
      // Fallback to raw text if parsing fails
    }
    return content
  }

  const formattedContent = getFormattedContent()
  const rawUrl = `${API_BASE_URL}/api/runs/${runId}/artifacts/${artifact.kind}`

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[90vw] max-w-[1100px] h-[85vh] flex flex-col p-6 bg-slate-950/95 border-slate-800 text-slate-100 backdrop-blur-xl shadow-2xl overflow-hidden">
        <DialogHeader className="shrink-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle className="text-lg font-semibold tracking-tight text-white flex items-center gap-2">
              <FileText className="w-5 h-5 text-purple-400" />
              {artifact.label}
            </DialogTitle>
            <Badge variant="secondary" className="font-mono text-xs bg-slate-800 text-slate-300 border-slate-700">
              {artifact.kind}
            </Badge>
          </div>
          <DialogDescription className="text-xs text-slate-400 font-mono flex flex-wrap gap-x-4 gap-y-1 pt-1">
            <span>Path: {artifact.path || 'unknown'}</span>
            {artifact.sizeHint && <span>Size: {artifact.sizeHint}</span>}
            {artifact.createdAt && <span>Created: {new Date(artifact.createdAt).toLocaleString()}</span>}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 min-h-0 bg-slate-900/60 rounded-lg border border-slate-800/80 mt-4 relative flex flex-col">
          {isLoading ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-3 text-slate-400">
              <Loader2 className="w-8 h-8 animate-spin text-purple-500" />
              <span className="text-sm font-medium">Fetching artifact content...</span>
            </div>
          ) : error ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-3 text-red-400 p-6 text-center">
              <AlertCircle className="w-8 h-8 text-red-500" />
              <span className="text-sm font-semibold">Error Loading Artifact</span>
              <p className="text-xs text-slate-400 max-w-md">{(error as Error).message}</p>
            </div>
          ) : !content ? (
            <div className="flex-1 flex flex-col items-center justify-center gap-2 text-slate-500">
              <FileText className="w-8 h-8 opacity-40" />
              <span className="text-sm italic">Artifact is empty</span>
            </div>
          ) : (
            <ScrollArea className="flex-1 w-full h-full">
              <pre className="p-4 font-mono text-xs leading-relaxed text-slate-300 selection:bg-purple-950 selection:text-purple-200 overflow-x-auto whitespace-pre">
                {formattedContent}
              </pre>
            </ScrollArea>
          )}
        </div>

        <div className="shrink-0 flex items-center justify-between mt-4 border-t border-slate-800/80 pt-4">
          <Button
            variant="outline"
            size="sm"
            asChild
            className="text-xs text-slate-300 hover:text-white border-slate-800 hover:bg-slate-900 gap-1.5"
          >
            <a href={rawUrl} target="_blank" rel="noreferrer">
              <ExternalLink className="w-3.5 h-3.5" />
              Open raw endpoint
            </a>
          </Button>

          <div className="flex items-center gap-2">
            <Button
              variant="default"
              size="sm"
              onClick={handleCopy}
              disabled={!content || isLoading}
              className="text-xs bg-purple-600 hover:bg-purple-700 text-white gap-1.5 min-w-[90px]"
            >
              {copied ? (
                <>
                  <Check className="w-3.5 h-3.5" />
                  Copied
                </>
              ) : (
                <>
                  <Copy className="w-3.5 h-3.5" />
                  Copy
                </>
              )}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
              className="text-xs text-slate-300 hover:text-white border-slate-800 hover:bg-slate-900"
            >
              Close
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
