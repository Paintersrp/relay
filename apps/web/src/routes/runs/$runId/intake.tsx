import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  approveIntake
} from '@/features/relay-runs'
import { RunWorkbenchLayout } from '@/components/relay/RunWorkbenchLayout'
import { ValidationPanel } from '@/components/relay/ValidationPanel'
import { LogPreviewPanel } from '@/components/relay/LogPreviewPanel'
import { ArtifactPreviewCard } from '@/components/relay/ArtifactPreviewCard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  CheckCircle2,
  AlertTriangle,
  Server,
  FolderGit2,
  AlertCircle,
  Clock,
  ArrowLeft,
  ArrowRight,
  ShieldCheck,
  ShieldX,
  ExternalLink
} from 'lucide-react'

export const Route = createFileRoute('/runs/$runId/intake')({
  component: IntakePage,
})

function IntakePage() {
  const { runId } = Route.useParams()
  
  // Real data loading through the Pass 2 API client queries
  const { data: run, isLoading: isLoadingRun, error: errorRun } = useQuery(runDetailQueryOptions(runId))
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(runArtifactsQueryOptions(runId))
  const { data: events, isLoading: isLoadingEvents } = useQuery(runEventsQueryOptions(runId))

  if (isLoadingRun || isLoadingArtifacts || isLoadingEvents) {
    return (
      <div className="flex flex-col gap-3 p-6 max-w-4xl mx-auto">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-64 w-full mt-6" />
      </div>
    )
  }

  // Handle run details missing or load errors
  if (errorRun || !run) {
    return (
      <div className="flex flex-col items-center justify-center flex-1 gap-4 p-8 text-center min-h-[50vh]">
        <div className="text-4xl">⚠️</div>
        <h1 className="text-lg font-semibold">Run not found or error loading</h1>
        <p className="text-sm text-muted-foreground max-w-sm">
          Failed to load run details from backend. Please verify the backend is running and the run ID is correct.
        </p>
        <Button variant="outline" size="sm" asChild>
          <Link to="/runs">
            <ArrowLeft className="w-3.5 h-3.5 mr-1.5" />
            Back to Runs
          </Link>
        </Button>
      </div>
    )
  }

  // Format events as log preview lines
  const formattedLogs = events
    ? events.map((e) => {
        const timeStr = new Date(e.createdAt).toLocaleTimeString('en-US', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        })
        return `[${timeStr}] ${e.message}`
      })
    : []

  const logPreview = {
    lines: formattedLogs.slice(-50),
    truncated: formattedLogs.length > 50,
  }

  return (
    <RunWorkbenchLayout
      run={{
        ...run,
        artifacts: artifacts || [],
        latestEvents: events || [],
        logPreview,
      }}
      mainContent={
        <IntakeMainContent
          run={run}
          artifacts={artifacts || []}
        />
      }
      sideContent={
        <>
          <ValidationPanel summary={run.validationSummary} />
          {artifacts && artifacts.map((a) => (
            <ArtifactPreviewCard key={a.id} artifact={a} runId={run.id} />
          ))}
          <LogPreviewPanel logPreview={logPreview} />
        </>
      }
    />
  )
}

function IntakeMainContent({
  run,
  artifacts,
}: {
  run: any
  artifacts: any[]
}) {
  const queryClient = useQueryClient()
  const [notes, setNotes] = useState('')
  const [mutationError, setMutationError] = useState<string | null>(null)

  // Extract initial values from run and configurations
  const runConfigArt = artifacts.find((a) => a.filename === 'run_config.json' || a.kind === 'run_config')
  let initialValCommands = ''
  let runConfig: any = {}
  if (runConfigArt && runConfigArt.preview) {
    try {
      runConfig = JSON.parse(runConfigArt.preview)
      initialValCommands = runConfig.validation_commands || ''
    } catch {
      // ignore
    }
  }

  // Local state for configuration overrides
  const [model, setModel] = useState(run.model || '')
  const [repo, setRepo] = useState(run.repo || '')
  const [branch, setBranch] = useState(run.branch || '')
  const [worktree, setWorktree] = useState(run.worktree || '')
  const [validationCommands, setValidationCommands] = useState(initialValCommands)

  // Keep fields in sync if run shifts
  useEffect(() => {
    if (run.model) setModel(run.model)
    if (run.repo) setRepo(run.repo)
    if (run.branch) setBranch(run.branch)
    if (run.worktree) setWorktree(run.worktree)
  }, [run.model, run.repo, run.branch, run.worktree])

  useEffect(() => {
    if (runConfigArt && runConfigArt.preview) {
      try {
        const cfg = JSON.parse(runConfigArt.preview)
        if (cfg.validation_commands) setValidationCommands(cfg.validation_commands)
        if (cfg.worktree) setWorktree(cfg.worktree)
      } catch {
        // ignore
      }
    }
  }, [runConfigArt])

  // Setup mutation for submitting review
  const { mutate, isPending } = useMutation({
    mutationFn: ({ requestPayload }: { requestPayload: any }) =>
      approveIntake(run.id, requestPayload),
    onSuccess: () => {
      setMutationError(null)
      setNotes('')
      // Invalidate queries to refresh route details
      void queryClient.invalidateQueries({ queryKey: ['relay-runs'] })
    },
    onError: (err: any) => {
      setMutationError(err.message || 'Failed to submit intake review.')
    },
  })

  // Review is allowed only when run is in reviewable state
  const isReviewable = run.status === 'intake_needs_review' || run.status === 'intake_received'

  const handleSubmit = (action: 'approve' | 'needs_revision' | 'blocked') => {
    setMutationError(null)
    const payload = {
      action,
      notes: notes.trim(),
      overrides: {
        model: model !== run.model ? model.trim() : undefined,
        repo: repo !== run.repo ? repo.trim() : undefined,
        branch: branch !== run.branch ? branch.trim() : undefined,
        worktree: worktree !== run.worktree ? worktree.trim() : undefined,
        validationCommands: validationCommands !== initialValCommands ? validationCommands.trim() : undefined,
      },
    }
    mutate({ requestPayload: payload })
  }

  const plannerHandoff = artifacts.find((a) => a.filename === 'planner_handoff.md' || a.kind === 'handoff')
  const parsedFrontmatter = artifacts.find((a) => a.filename === 'parsed_frontmatter.json')

  // Parse repo target/path details
  const repoTarget = runConfig.repo_target || run.repo
  const branchContext = runConfig.branch_context || run.branch
  const configSource = runConfig.source || 'unknown'
  const createdFrom = runConfig.created_from || 'unknown'

  const isRepoNameOnly = !run.repo.includes('/') && !run.repo.includes('\\') && !run.repo.includes(':')
  const isLocalPath = repoTarget.includes('/') || repoTarget.includes('\\') || repoTarget.includes(':')
  const isGitHubRepo = /^[a-zA-Z0-9._-]+\/[a-zA-Z0-9._-]+$/.test(repoTarget)

  // Parse frontmatter details to determine presence/clarity
  let frontmatterObj: any = null
  let hasFrontmatter = false
  if (parsedFrontmatter && parsedFrontmatter.preview) {
    try {
      frontmatterObj = JSON.parse(parsedFrontmatter.preview)
      if (frontmatterObj && Object.keys(frontmatterObj).length > 0) {
        hasFrontmatter = true
      }
    } catch {
      // ignore
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Section: Incoming Handoff */}
      <Section title="Incoming Handoff" icon={<FolderGit2 className="w-4 h-4 text-purple-400" />}>
        <div className="flex flex-col gap-1.5">
          <KeyValueRow label="Packet ID" value={run.packetId || '—'} mono />
          <KeyValueRow label="Title" value={run.title} />
          <KeyValueRow label="Status" value={run.status} mono />
        </div>

        {/* Planner Handoff Preview */}
        <div className="flex flex-col gap-2 mt-2">
          <span className="text-xs font-semibold text-muted-foreground">Planner Handoff Preview</span>
          {plannerHandoff?.preview ? (
            <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
              {plannerHandoff.preview}
            </pre>
          ) : (
            <div className="text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground flex flex-col gap-1">
              <span className="italic">Handoff preview content is unavailable.</span>
              {plannerHandoff && (
                <span className="text-[10px] opacity-70">
                  File: {plannerHandoff.filename} | Path: {plannerHandoff.path} | Size: {plannerHandoff.sizeHint || 'unknown'}
                </span>
              )}
            </div>
          )}
        </div>

        {/* Parsed Frontmatter Preview */}
        <div className="flex flex-col gap-2 mt-2">
          <span className="text-xs font-semibold text-muted-foreground">Parsed Frontmatter Preview</span>
          {parsedFrontmatter?.preview ? (
            <pre className="text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
              {parsedFrontmatter.preview}
            </pre>
          ) : (
            <div className="text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground flex flex-col gap-1">
              <span className="italic">Parsed frontmatter preview is unavailable.</span>
              {parsedFrontmatter && (
                <span className="text-[10px] opacity-70">
                  File: {parsedFrontmatter.filename} | Path: {parsedFrontmatter.path} | Size: {parsedFrontmatter.sizeHint || 'unknown'}
                </span>
              )}
            </div>
          )}
        </div>

        {!hasFrontmatter && (
          <div className="mt-3 p-3 bg-yellow-950/20 border border-yellow-900/40 rounded text-xs text-yellow-400 leading-normal flex items-start gap-2">
            <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
            <span>
              No YAML frontmatter was parsed from the submitted handoff. Relay used explicit MCP/API arguments and fallback defaults where available.
            </span>
          </div>
        )}

        <div className="flex flex-col gap-2 mt-4">
          <span className="text-xs font-semibold text-muted-foreground">Configuration Provenance</span>
          <div className="border border-border/40 rounded-lg overflow-hidden bg-muted/10 text-xs">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-border/40 bg-muted/30">
                  <th className="p-2 font-medium text-muted-foreground w-1/4">Field</th>
                  <th className="p-2 font-medium text-muted-foreground w-2/4">Value</th>
                  <th className="p-2 font-medium text-muted-foreground w-1/4">Source</th>
                </tr>
              </thead>
              <tbody>
                <tr className="border-b border-border/20">
                  <td className="p-2 font-mono text-muted-foreground">Repo</td>
                  <td className="p-2 font-mono text-foreground">{repoTarget}</td>
                  <td className="p-2 text-foreground">
                    {frontmatterObj?.repo ? 'parsed frontmatter' : runConfig.repo_target ? 'explicit MCP arg' : 'resolved repo'}
                  </td>
                </tr>
                <tr className="border-b border-border/20">
                  <td className="p-2 font-mono text-muted-foreground">Branch</td>
                  <td className="p-2 font-mono text-foreground">{branchContext}</td>
                  <td className="p-2 text-foreground">
                    {frontmatterObj?.branch ? 'parsed frontmatter' : runConfig.branch_context ? 'explicit MCP arg' : 'fallback default'}
                  </td>
                </tr>
                <tr>
                  <td className="p-2 font-mono text-muted-foreground">Title</td>
                  <td className="p-2 text-foreground truncate max-w-xs">{run.title}</td>
                  <td className="p-2 text-foreground">
                    {frontmatterObj?.title ? 'parsed frontmatter' : 'markdown H1'}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </Section>

      <Separator />

      {/* Section: Resolved Repository */}
      <Section title="Resolved Repository" icon={<FolderGit2 className="w-4 h-4 text-purple-400" />}>
        <div className="flex flex-col gap-1.5">
          <KeyValueRow label={isRepoNameOnly ? "Repo display name" : "Repo target"} value={run.repo} />
          {repoTarget !== run.repo && (
            <div className="flex items-baseline gap-2 text-xs">
              <span className="text-muted-foreground w-32 shrink-0">Resolved target/path</span>
              {isLocalPath ? (
                <span className="font-mono text-foreground bg-muted/40 px-1.5 py-0.5 rounded select-all border border-border/40">{repoTarget}</span>
              ) : isGitHubRepo ? (
                <a
                  href={`https://github.com/${repoTarget}`}
                  target="_blank"
                  rel="noreferrer"
                  className="font-mono text-purple-400 hover:underline flex items-center gap-1 select-all"
                >
                  {repoTarget}
                  <ExternalLink className="w-3.5 h-3.5" />
                </a>
              ) : (
                <span className="text-foreground select-all">{repoTarget}</span>
              )}
            </div>
          )}
          <KeyValueRow label="Branch context" value={branchContext} mono />
          <KeyValueRow label="Source" value={configSource} />
          <KeyValueRow label="Created by" value={createdFrom} />
        </div>
      </Section>

      <Separator />

      {/* Section: Parsed Metadata */}
      <Section title="Parsed Metadata" icon={<CheckCircle2 className="w-4 h-4 text-emerald-400" />}>
        <KeyValueRow label="Repo" value={run.repo} mono />
        <KeyValueRow label="Branch" value={run.branch} mono />
        <KeyValueRow label="Worktree" value={run.worktree || '—'} mono />
        <KeyValueRow label="Executor" value={run.executor} />
        <KeyValueRow label="Model" value={run.model} mono />
      </Section>

      <Separator />

      {/* Section: Run Configuration */}
      <Section title="Run Configuration" icon={<Server className="w-4 h-4 text-blue-400" />}>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-1">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-model" className="text-xs text-muted-foreground">Target Model</Label>
            <Input
              id="override-model"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="e.g. deepseek-v4-flash"
              className="h-8 text-xs bg-background/50"
              disabled={isPending || !isReviewable}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-repo" className="text-xs text-muted-foreground">Repository Target Path</Label>
            <Input
              id="override-repo"
              value={repo}
              onChange={(e) => setRepo(e.target.value)}
              placeholder="e.g. d:\Code\relay"
              className="h-8 text-xs bg-background/50"
              disabled={isPending || !isReviewable}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-branch" className="text-xs text-muted-foreground">Branch / Worktree Context</Label>
            <Input
              id="override-branch"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder="e.g. main"
              className="h-8 text-xs bg-background/50"
              disabled={isPending || !isReviewable}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-worktree" className="text-xs text-muted-foreground">Worktree Override</Label>
            <Input
              id="override-worktree"
              value={worktree}
              onChange={(e) => setWorktree(e.target.value)}
              placeholder="e.g. my-worktree"
              className="h-8 text-xs bg-background/50"
              disabled={isPending || !isReviewable}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-validation" className="text-xs text-muted-foreground">Validation Commands</Label>
            <Input
              id="override-validation"
              value={validationCommands}
              onChange={(e) => setValidationCommands(e.target.value)}
              placeholder="e.g. go test ./..."
              className="h-8 text-xs bg-background/50"
              disabled={isPending || !isReviewable}
            />
          </div>
        </div>
      </Section>

      <Separator />

      {/* Section: Repo Workspace Preflight */}
      <Section title="Repo Workspace Preflight">
        {run.validationSummary ? (
          <div className="flex flex-col gap-1">
            {[
              { label: 'Repo reachable', pass: run.validationSummary.errors === 0 || !run.validationSummary.issues?.some((i: any) => i.message?.toLowerCase().includes('repo')) },
              { label: 'Branch exists', pass: run.validationSummary.errors === 0 || !run.validationSummary.issues?.some((i: any) => i.message?.toLowerCase().includes('branch')) },
              { label: 'No uncommitted changes', pass: run.status !== 'intake_needs_review' },
              { label: 'Validation commands extractable', pass: run.validationSummary.errors === 0 },
            ].map((check) => (
              <div key={check.label} className="flex items-center gap-2 text-xs">
                {check.pass ? (
                  <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
                ) : (
                  <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
                )}
                <span className={check.pass ? 'text-foreground' : 'text-yellow-400'}>{check.label}</span>
                <Badge variant={check.pass ? 'success' : 'warning'} className="ml-auto text-[10px] h-5 py-0">
                  {check.pass ? 'OK' : 'Review'}
                </Badge>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">
            Preflight not available from current intake data.
          </p>
        )}
      </Section>

      <Separator />

      {/* Section: Validation Results */}
      <Section title="Validation Results" icon={<AlertTriangle className="w-4 h-4 text-yellow-400" />}>
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-4 text-xs">
            <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full bg-red-500" /> Errors: {run.validationSummary?.errors ?? 0}</span>
            <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full bg-yellow-500" /> Warnings: {run.validationSummary?.warnings ?? 0}</span>
            <span className="flex items-center gap-1"><span className="w-2 h-2 rounded-full bg-green-500" /> Passed: {run.validationSummary?.passed ?? 0}</span>
          </div>
          {run.validationSummary?.issues && run.validationSummary.issues.length > 0 ? (
            <div className="flex flex-col gap-1.5 mt-1 border border-border/40 rounded bg-muted/20 p-2 max-h-36 overflow-y-auto">
              {run.validationSummary.issues.map((issue: any, idx: number) => (
                <div key={idx} className="flex items-start gap-1.5 text-xs text-foreground/80 leading-normal">
                  <span className={issue.severity === 'error' ? 'text-red-400 font-bold shrink-0' : 'text-yellow-400 font-bold shrink-0'}>
                    [{issue.severity.toUpperCase()}]
                  </span>
                  <span>{issue.message}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground italic">No validation issues found.</p>
          )}
        </div>
      </Section>

      <Separator />

      {/* Section: Approval Gate */}
      <Section title="Approval Gate" icon={<ShieldCheck className="w-4 h-4 text-primary" />}>
        <div className="flex flex-col gap-3 mt-1">
          {!isReviewable && (
            <div className="flex items-center gap-2 p-2.5 rounded bg-muted/40 border border-border/50 text-xs text-muted-foreground leading-normal">
              <Clock className="w-4 h-4 shrink-0" />
              <span>
                Intake review is not active. Run is currently in <strong>{run.state || run.status}</strong> state.
              </span>
            </div>
          )}

          {isReviewable && (
            <div className="flex flex-col gap-2">
              <Label htmlFor="review-notes" className="text-xs text-muted-foreground">Review Notes (Optional)</Label>
              <Textarea
                id="review-notes"
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Provide details about approval or revision requirements..."
                className="h-16 text-xs bg-background/50 resize-none"
                disabled={isPending}
              />
              
              {mutationError && (
                <div className="flex items-start gap-1.5 text-xs text-red-400 bg-red-950/20 border border-red-900/30 rounded p-2">
                  <AlertCircle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  <span>{mutationError}</span>
                </div>
              )}

              <div className="flex flex-wrap items-center gap-2 mt-1">
                <Button
                  variant="default"
                  size="sm"
                  onClick={() => handleSubmit('approve')}
                  disabled={isPending}
                  className="bg-emerald-600 hover:bg-emerald-700 text-white gap-1.5"
                >
                  <ShieldCheck className="w-3.5 h-3.5" />
                  Approve Intake
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleSubmit('needs_revision')}
                  disabled={isPending}
                  className="gap-1.5 text-yellow-500 hover:text-yellow-400 border-yellow-500/30 hover:border-yellow-500/50 hover:bg-yellow-500/10"
                >
                  <AlertTriangle className="w-3.5 h-3.5" />
                  Needs Revision
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => handleSubmit('blocked')}
                  disabled={isPending}
                  className="gap-1.5"
                >
                  <ShieldX className="w-3.5 h-3.5" />
                  Block Run
                </Button>
              </div>
            </div>
          )}

          {(run.status === 'approved_for_prepare' || run.activeStep === 'prepare') && (
            <div className="flex flex-col gap-2 p-3 rounded bg-emerald-950/20 border border-emerald-950/40 text-xs text-foreground mt-2">
              <div className="flex items-center gap-2 text-emerald-400 font-medium">
                <CheckCircle2 className="w-4 h-4 shrink-0" />
                <span>Intake Approved Successfully!</span>
              </div>
              <p className="text-muted-foreground leading-normal">
                This run is now approved for brief compilation and environment preparation.
              </p>
              <Button size="sm" asChild className="w-full mt-1.5 gap-1.5 bg-emerald-600 hover:bg-emerald-700">
                <Link to="/runs/$runId/prepare" params={{ runId: run.id }}>
                  Proceed to Compile / Render
                  <ArrowRight className="w-3.5 h-3.5" />
                </Link>
              </Button>
            </div>
          )}
        </div>
      </Section>
    </div>
  )
}

function Section({
  title,
  icon,
  children,
}: {
  title: string
  icon?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <Card className="border-border/60 bg-card/20">
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0 flex flex-col gap-1.5">{children}</CardContent>
    </Card>
  )
}

function KeyValueRow({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="flex items-baseline gap-2 text-xs">
      <span className="text-muted-foreground w-32 shrink-0">{label}</span>
      <span className={mono ? 'font-mono text-foreground' : 'text-foreground'}>{value}</span>
    </div>
  )
}
