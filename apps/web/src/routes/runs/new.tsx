import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import { ArrowLeft, Info } from 'lucide-react'

export const Route = createFileRoute('/runs/new')({
  component: NewRunPage,
})

function NewRunPage() {
  return (
    <div className="flex flex-col flex-1 overflow-y-auto">
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
        {/* Pass 4 notice */}
        <Alert variant="warning" className="border-yellow-600/40 bg-yellow-600/5">
          <Info className="w-4 h-4" />
          <AlertTitle className="text-yellow-400">Pass 1 — Intake Submission Disabled</AlertTitle>
          <AlertDescription className="text-muted-foreground">
            Real planner handoff submission is implemented in Pass 4. The controls below are
            illustrative only and do not submit to the backend. Approving or submitting will have no
            effect.
          </AlertDescription>
        </Alert>

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
                className="font-mono text-xs min-h-[120px] resize-none opacity-60 cursor-not-allowed"
                disabled
                aria-label="Handoff paste input — disabled in Pass 1"
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
                className="text-xs opacity-60 cursor-not-allowed"
                disabled
                aria-label="Handoff file upload — disabled in Pass 1"
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
                  placeholder="owner/repo"
                  disabled
                  className="text-xs opacity-60 cursor-not-allowed"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="branch-input" className="text-xs">Branch</Label>
                <Input
                  id="branch-input"
                  placeholder="feature/my-branch"
                  disabled
                  className="text-xs opacity-60 cursor-not-allowed"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="executor-input" className="text-xs">Executor</Label>
                <Input
                  id="executor-input"
                  placeholder="opencode / cline / codex"
                  disabled
                  className="text-xs opacity-60 cursor-not-allowed"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="model-input" className="text-xs">Model</Label>
                <Input
                  id="model-input"
                  placeholder="anthropic/claude-3-5-sonnet"
                  disabled
                  className="text-xs opacity-60 cursor-not-allowed"
                />
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Submit area */}
        <div className="flex items-center justify-end gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link to="/runs">Cancel</Link>
          </Button>
          <Button
            size="sm"
            disabled
            className="opacity-50 cursor-not-allowed"
            title="Handoff submission is not implemented in Pass 1"
          >
            Submit Handoff — Pass 4
          </Button>
        </div>
      </div>
    </div>
  )
}
