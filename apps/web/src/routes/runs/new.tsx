import { useState } from 'react'
import { createFileRoute, Link, useRouter } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import { ArrowLeft, AlertCircle, Loader2 } from 'lucide-react'
import { submitPlannerHandoff, RelayApiError } from '@/features/relay-runs'

export const Route = createFileRoute('/runs/new')({
  component: NewRunPage,
})

function NewRunPage() {
  const router = useRouter()
  const [markdown, setMarkdown] = useState('')
  const [repo, setRepo] = useState('')
  const [branch, setBranch] = useState('')
  const [name, setName] = useState('')
  const [source, setSource] = useState('react_workbench')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return

    const reader = new FileReader()
    reader.onload = (event) => {
      const text = event.target?.result
      if (typeof text === 'string') {
        setMarkdown(text)
        setErrorMsg(null)
      }
    }
    reader.onerror = () => {
      setErrorMsg('Failed to read the handoff file.')
    }
    reader.readAsText(file)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!markdown.trim()) {
      setErrorMsg('Planner handoff markdown is required.')
      return
    }

    setIsSubmitting(true)
    setErrorMsg(null)

    try {
      const response = await submitPlannerHandoff({
        planner_handoff_markdown: markdown,
        repo_target: repo.trim() || undefined,
        branch_context: branch.trim() || undefined,
        name: name.trim() || undefined,
        source: source.trim() || undefined,
      })

      if (response.review_url) {
        window.location.href = response.review_url
      } else {
        void router.navigate({
          to: '/runs/$runId/intake',
          params: { runId: response.runID || response.run_id || '' }
        })
      }
    } catch (err: any) {
      if (err instanceof RelayApiError) {
        setErrorMsg(err.errorShape?.message || err.message)
      } else {
        setErrorMsg(err.message || 'An unexpected error occurred during submission.')
      }
    } finally {
      setIsSubmitting(false)
    }
  }

  const isFormValid = markdown.trim().length > 0

  return (
    <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-y-auto">
      {/* Page header */}
      <div className="flex items-center gap-3 px-6 py-4 border-b border-border/60">
        <Button variant="ghost" size="sm" asChild className="gap-1.5 h-7 text-xs -ml-1">
          <Link to="/runs">
            <ArrowLeft className="w-3.5 h-3.5" />
            Back to Runs
          </Link>
        </Button>
        <Separator orientation="vertical" className="h-4" />
        <div>
          <h1 className="text-base font-semibold">New Run</h1>
          <p className="text-xs text-muted-foreground">Submit a handoff packet to start a relay run.</p>
        </div>
      </div>

      <div className="max-w-2xl mx-auto w-full p-6 flex flex-col gap-6">
        {/* Error Alert */}
        {errorMsg && (
          <Alert variant="destructive">
            <AlertCircle className="w-4 h-4" />
            <AlertTitle>Submission Failed</AlertTitle>
            <AlertDescription className="text-xs">
              {errorMsg}
            </AlertDescription>
          </Alert>
        )}

        {/* Incoming handoff section */}
        <Card className="border-border/60">
          <CardHeader className="p-4 pb-2">
            <CardTitle className="text-sm font-medium">Incoming Handoff</CardTitle>
            <CardDescription className="text-xs">
              Paste or upload a surgical implementation handoff packet.
            </CardDescription>
          </CardHeader>
          <CardContent className="p-4 pt-2 flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="handoff-paste" className="text-xs">Paste Handoff</Label>
              <Textarea
                id="handoff-paste"
                placeholder="Paste handoff content here…"
                className="font-mono text-xs min-h-[180px] resize-y"
                value={markdown}
                onChange={(e) => setMarkdown(e.target.value)}
                aria-label="Handoff paste input"
              />
            </div>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <span>or</span>
              <Separator className="flex-1" />
              <span>upload file</span>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="handoff-file" className="text-xs">Upload Handoff File</Label>
              <Input
                id="handoff-file"
                type="file"
                accept=".md,.txt,.json"
                className="text-xs cursor-pointer"
                onChange={handleFileChange}
                aria-label="Handoff file upload"
              />
            </div>
          </CardContent>
        </Card>

        {/* Run configuration section */}
        <Card className="border-border/60">
          <CardHeader className="p-4 pb-2">
            <CardTitle className="text-sm font-medium">Run Configuration</CardTitle>
            <CardDescription className="text-xs">
              Override detected values from the handoff metadata.
            </CardDescription>
          </CardHeader>
          <CardContent className="p-4 pt-2 flex flex-col gap-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="repo-input" className="text-xs">Repository</Label>
                <Input
                  id="repo-input"
                  placeholder="owner/repo (or auto-detected)"
                  value={repo}
                  onChange={(e) => setRepo(e.target.value)}
                  className="text-xs"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="branch-input" className="text-xs">Branch</Label>
                <Input
                  id="branch-input"
                  placeholder="feature/my-branch (or auto-detected)"
                  value={branch}
                  onChange={(e) => setBranch(e.target.value)}
                  className="text-xs"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="name-input" className="text-xs">Run Name / Title</Label>
                <Input
                  id="name-input"
                  placeholder="My Relay Run (or auto-detected)"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="text-xs"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="source-input" className="text-xs">Source</Label>
                <Input
                  id="source-input"
                  placeholder="react_workbench"
                  value={source}
                  onChange={(e) => setSource(e.target.value)}
                  className="text-xs"
                />
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Submit area */}
        <div className="flex items-center justify-end gap-2">
          <Button variant="outline" size="sm" asChild disabled={isSubmitting}>
            <Link to="/runs">Cancel</Link>
          </Button>
          <Button
            type="submit"
            size="sm"
            disabled={!isFormValid || isSubmitting}
            className="min-w-[120px]"
          >
            {isSubmitting ? (
              <>
                <Loader2 className="w-3 h-3 mr-2 animate-spin" />
                Submitting...
              </>
            ) : (
              'Submit Handoff'
            )}
          </Button>
        </div>
      </div>
    </form>
  )
}
