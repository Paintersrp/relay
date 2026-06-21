import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useEffect } from "react";
import {
  runDetailQueryOptions,
  runArtifactsQueryOptions,
  runEventsQueryOptions,
  approveIntake,
} from "@/features/relay-runs";
import { RunWorkbenchLayout } from "@/components/relay/RunWorkbenchLayout";
import {
  RelayInlineState,
  RelayStateBanner,
} from "@/components/relay/RelayStateSurface";
import {
  RunWorkbenchLoadFailedState,
  RunWorkbenchLoadingState,
} from "@/components/relay/RunWorkbenchStates";
import { ValidationPanel } from "@/components/relay/ValidationPanel";
import { LogPreviewPanel } from "@/components/relay/LogPreviewPanel";
import { RunEvidenceBrowser } from "@/components/relay/RunEvidenceBrowser";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  CheckCircle2,
  AlertTriangle,
  Server,
  FolderGit2,
  ArrowRight,
  ShieldCheck,
  ShieldX,
  ExternalLink,
} from "lucide-react";

export const Route = createFileRoute("/runs/$runId/intake")({
  component: IntakePage,
});

function IntakePage() {
  const { runId } = Route.useParams();

  const {
    data: run,
    isLoading: isLoadingRun,
    error: errorRun,
  } = useQuery(runDetailQueryOptions(runId));
  const { data: artifacts, isLoading: isLoadingArtifacts } = useQuery(
    runArtifactsQueryOptions(runId),
  );
  const { data: events, isLoading: isLoadingEvents } = useQuery(
    runEventsQueryOptions(runId),
  );

  if (isLoadingRun || isLoadingArtifacts || isLoadingEvents) {
    return <RunWorkbenchLoadingState label="Loading run" />;
  }

  // Handle run details missing or load errors
  if (errorRun || !run) {
    return (
      <RunWorkbenchLoadFailedState
        title="Run failed to load"
        description="Relay could not load this run. Return to the runs registry and reopen the workbench."
        backToRuns
      />
    );
  }

  // Format events as log preview lines
  const formattedLogs = events
    ? events.map((e) => {
        const timeStr = new Date(e.createdAt).toLocaleTimeString("en-US", {
          hour12: false,
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        });
        return `[${timeStr}] ${e.message}`;
      })
    : [];

  const logPreview = {
    lines: formattedLogs.slice(-50),
    truncated: formattedLogs.length > 50,
  };

  return (
    <RunWorkbenchLayout
      run={{
        ...run,
        artifacts: artifacts || [],
        latestEvents: events || [],
        logPreview,
      }}
      currentStep="intake"
      mainContent={<IntakeMainContent run={run} artifacts={artifacts || []} />}
      inspectorPanels={{
        logs: <LogPreviewPanel logPreview={logPreview} />,
        artifacts: (
          <RunEvidenceBrowser
            runId={run.id}
            artifacts={artifacts || []}
            events={events || []}
          />
        ),
        validation: <ValidationPanel summary={run.validationSummary} />,
      }}
    />
  );
}

function IntakeMainContent({ run, artifacts }: { run: any; artifacts: any[] }) {
  const queryClient = useQueryClient();
  const [notes, setNotes] = useState("");
  const [mutationError, setMutationError] = useState<string | null>(null);

  // Extract initial values from run and configurations
  const runConfigArt = artifacts.find(
    (a) => a.filename === "run_config.json" || a.kind === "run_config",
  );
  let initialValCommands = "";
  let runConfig: any = {};
  if (runConfigArt && runConfigArt.preview) {
    try {
      runConfig = JSON.parse(runConfigArt.preview);
      initialValCommands = runConfig.validation_commands || "";
    } catch {
      // ignore
    }
  }

  // Local state for configuration overrides
  const [model, setModel] = useState(run.model || "");
  const [repo, setRepo] = useState(run.repo || "");
  const [branch, setBranch] = useState(run.branch || "");
  const [worktree, setWorktree] = useState(run.worktree || "");
  const [executorAdapter, setExecutorAdapter] = useState(
    run.executorAdapter || "opencode_go",
  );
  const [validationCommands, setValidationCommands] =
    useState(initialValCommands);

  // Keep fields in sync if run shifts
  useEffect(() => {
    if (run.model) setModel(run.model);
    if (run.repo) setRepo(run.repo);
    if (run.branch) setBranch(run.branch);
    if (run.worktree) setWorktree(run.worktree);
    if (run.executorAdapter) setExecutorAdapter(run.executorAdapter);
  }, [run.model, run.repo, run.branch, run.worktree, run.executorAdapter]);

  useEffect(() => {
    if (runConfigArt && runConfigArt.preview) {
      try {
        const cfg = JSON.parse(runConfigArt.preview);
        if (cfg.validation_commands)
          setValidationCommands(cfg.validation_commands);
        if (cfg.worktree) setWorktree(cfg.worktree);
        if (cfg.executor_adapter) setExecutorAdapter(cfg.executor_adapter);
      } catch {
        // ignore
      }
    }
  }, [runConfigArt]);

  // Setup mutation for submitting review
  const { mutate, isPending } = useMutation({
    mutationFn: ({ requestPayload }: { requestPayload: any }) =>
      approveIntake(run.id, requestPayload),
    onSuccess: () => {
      setMutationError(null);
      setNotes("");
      // Invalidate queries to refresh route details
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (err: any) => {
      setMutationError(err.message || "Failed to submit intake review.");
    },
  });

  // Review is allowed only when run is in reviewable state
  const isReviewable =
    run.status === "intake_needs_review" || run.status === "intake_received";

  const handleSubmit = (action: "approve" | "needs_revision" | "blocked") => {
    setMutationError(null);
    const payload = {
      action,
      notes: notes.trim(),
      overrides: {
        model: model !== run.model ? model.trim() : undefined,
        repo: repo !== run.repo ? repo.trim() : undefined,
        branch: branch !== run.branch ? branch.trim() : undefined,
        worktree: worktree !== run.worktree ? worktree.trim() : undefined,
        executorAdapter:
          executorAdapter !== run.executorAdapter ? executorAdapter : undefined,
        validationCommands:
          validationCommands !== initialValCommands
            ? validationCommands.trim()
            : undefined,
      },
    };
    mutate({ requestPayload: payload });
  };

  const plannerHandoff = artifacts.find(
    (a) => a.filename === "planner_handoff.md" || a.kind === "handoff",
  );
  const parsedFrontmatter = artifacts.find(
    (a) => a.filename === "parsed_frontmatter.json",
  );

  // Parse repo target/path details
  const repoTarget = runConfig.repo_target || run.repo;
  const branchContext = runConfig.branch_context || run.branch;
  const configSource = runConfig.source || "unknown";
  const createdFrom = runConfig.created_from || "unknown";

  const isRepoNameOnly =
    !run.repo.includes("/") &&
    !run.repo.includes("\\") &&
    !run.repo.includes(":");
  const isLocalPath =
    repoTarget.includes("/") ||
    repoTarget.includes("\\") ||
    repoTarget.includes(":");
  const isGitHubRepo = /^[a-zA-Z0-9._-]+\/[a-zA-Z0-9._-]+$/.test(repoTarget);

  // Parse frontmatter details to determine presence/clarity
  let frontmatterObj: any = null;
  let hasFrontmatter = false;
  if (parsedFrontmatter && parsedFrontmatter.preview) {
    try {
      frontmatterObj = JSON.parse(parsedFrontmatter.preview);
      if (frontmatterObj && Object.keys(frontmatterObj).length > 0) {
        hasFrontmatter = true;
      }
    } catch {
      // ignore
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {mutationError && (
        <RelayStateBanner
          tone="danger"
          title="Intake review failed"
          description={mutationError}
        />
      )}

      {/* Section: Incoming Handoff */}
      <Section
        title="Incoming Handoff"
        icon={<FolderGit2 className="w-4 h-4 text-purple-400" />}
      >
        <div className="flex flex-col gap-1.5">
          <KeyValueRow label="Packet ID" value={run.packetId || "—"} mono />
          <KeyValueRow label="Title" value={run.title} />
          <KeyValueRow label="Status" value={run.status} mono />
        </div>

        {/* Planner Handoff Preview */}
        <div className="flex flex-col gap-2 mt-2">
          <span className="text-xs font-semibold text-muted-foreground">
            Planner Handoff Preview
          </span>
          {plannerHandoff?.preview ? (
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
              {plannerHandoff.preview}
            </pre>
          ) : (
            <div className="text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground flex flex-col gap-1">
              <span className="italic">
                Handoff preview content is unavailable.
              </span>
              {plannerHandoff && (
                <span className="text-[10px] opacity-70">
                  File: {plannerHandoff.filename} | Path: {plannerHandoff.path}{" "}
                  | Size: {plannerHandoff.sizeHint || "unknown"}
                </span>
              )}
            </div>
          )}
        </div>

        {/* Parsed Frontmatter Preview */}
        <div className="flex flex-col gap-2 mt-2">
          <span className="text-xs font-semibold text-muted-foreground">
            Parsed Frontmatter Preview
          </span>
          {parsedFrontmatter?.preview ? (
            <pre className="max-w-full overflow-x-auto text-[11px] font-mono bg-muted/40 p-2.5 rounded border border-border/40 max-h-48 overflow-y-auto whitespace-pre-wrap text-foreground">
              {parsedFrontmatter.preview}
            </pre>
          ) : (
            <div className="text-xs bg-muted/30 border border-dashed rounded p-3 text-muted-foreground flex flex-col gap-1">
              <span className="italic">
                Parsed frontmatter preview is unavailable.
              </span>
              {parsedFrontmatter && (
                <span className="text-[10px] opacity-70">
                  File: {parsedFrontmatter.filename} | Path:{" "}
                  {parsedFrontmatter.path} | Size:{" "}
                  {parsedFrontmatter.sizeHint || "unknown"}
                </span>
              )}
            </div>
          )}
        </div>

        {!hasFrontmatter && (
          <RelayStateBanner
            tone="warning"
            title="No YAML frontmatter parsed"
            description="No YAML frontmatter was parsed from the submitted handoff. Relay used explicit MCP/API arguments and fallback defaults where available."
            className="mt-3"
          />
        )}

        <div className="flex flex-col gap-2 mt-4">
          <span className="text-xs font-semibold text-muted-foreground">
            Configuration Provenance
          </span>
          <div className="overflow-x-auto rounded-lg border border-border/40 bg-muted/10 text-xs">
            <table className="w-full min-w-[36rem] border-collapse text-left">
              <thead>
                <tr className="border-b border-border/40 bg-muted/30">
                  <th className="p-2 font-medium text-muted-foreground w-1/4">
                    Field
                  </th>
                  <th className="p-2 font-medium text-muted-foreground w-2/4">
                    Value
                  </th>
                  <th className="p-2 font-medium text-muted-foreground w-1/4">
                    Source
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr className="border-b border-border/20">
                  <td className="p-2 font-mono text-muted-foreground">Repo</td>
                  <td className="p-2 font-mono text-foreground">
                    {repoTarget}
                  </td>
                  <td className="p-2 text-foreground">
                    {frontmatterObj?.repo
                      ? "parsed frontmatter"
                      : runConfig.repo_target
                        ? "explicit MCP arg"
                        : "resolved repo"}
                  </td>
                </tr>
                <tr className="border-b border-border/20">
                  <td className="p-2 font-mono text-muted-foreground">
                    Branch
                  </td>
                  <td className="p-2 font-mono text-foreground">
                    {branchContext}
                  </td>
                  <td className="p-2 text-foreground">
                    {frontmatterObj?.branch
                      ? "parsed frontmatter"
                      : runConfig.branch_context
                        ? "explicit MCP arg"
                        : "fallback default"}
                  </td>
                </tr>
                <tr>
                  <td className="p-2 font-mono text-muted-foreground">Title</td>
                  <td className="p-2 text-foreground truncate max-w-xs">
                    {run.title}
                  </td>
                  <td className="p-2 text-foreground">
                    {frontmatterObj?.title
                      ? "parsed frontmatter"
                      : "markdown H1"}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </Section>

      <Separator />

      {/* Section: Resolved Repository */}
      <Section
        title="Resolved Repository"
        icon={<FolderGit2 className="w-4 h-4 text-purple-400" />}
      >
        <div className="flex flex-col gap-1.5">
          <KeyValueRow
            label={isRepoNameOnly ? "Repo display name" : "Repo target"}
            value={run.repo}
          />
          {repoTarget !== run.repo && (
            <div className="flex items-baseline gap-2 text-xs">
              <span className="text-muted-foreground w-32 shrink-0">
                Resolved target/path
              </span>
              {isLocalPath ? (
                <span className="font-mono text-foreground bg-muted/40 px-1.5 py-0.5 rounded select-all border border-border/40">
                  {repoTarget}
                </span>
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
      <Section
        title="Parsed Metadata"
        icon={<CheckCircle2 className="w-4 h-4 text-emerald-400" />}
      >
        <KeyValueRow label="Repo" value={run.repo} mono />
        <KeyValueRow label="Branch" value={run.branch} mono />
        <KeyValueRow label="Worktree" value={run.worktree || "—"} mono />
        <KeyValueRow
          label="Executor Adapter"
          value={run.executorAdapter}
          mono
        />
        <KeyValueRow label="Model" value={run.model} mono />
      </Section>

      <Separator />

      {/* Section: Run Configuration */}
      <Section
        title="Run Configuration"
        icon={<Server className="w-4 h-4 text-blue-400" />}
      >
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-1">
          <div className="flex flex-col gap-1.5">
            <Label
              htmlFor="override-model"
              className="text-xs text-muted-foreground"
            >
              Target Model
            </Label>
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
            <Label
              htmlFor="override-executor"
              className="text-xs text-muted-foreground"
            >
              Executor Adapter
            </Label>
            <Select
              value={executorAdapter}
              onValueChange={setExecutorAdapter}
              disabled={isPending || !isReviewable}
            >
              <SelectTrigger
                id="override-executor"
                className="h-8 text-xs bg-background/50 w-full"
              >
                <SelectValue placeholder="Select executor adapter" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="opencode_go">OpenCode (Go)</SelectItem>
                  <SelectItem value="codex">Codex (TypeScript)</SelectItem>
                  <SelectItem value="antigravity">Antigravity (Go)</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            {executorAdapter === "codex" && (
              <div className="text-[10px] text-muted-foreground bg-muted/20 border border-border/40 rounded p-1.5 leading-tight">
                <strong>Note:</strong> Codex dispatch uses the local Codex CLI
                configuration and authentication available to the Relay daemon.
              </div>
            )}
            {executorAdapter === "antigravity" && (
              <div className="text-[10px] text-muted-foreground bg-muted/20 border border-border/40 rounded p-1.5 leading-tight">
                <strong>Note:</strong> Antigravity dispatch uses the local
                Antigravity CLI configuration and authentication available to
                the Relay daemon.
              </div>
            )}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label
              htmlFor="override-repo"
              className="text-xs text-muted-foreground"
            >
              Repository Target Path
            </Label>
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
            <Label
              htmlFor="override-branch"
              className="text-xs text-muted-foreground"
            >
              Branch / Worktree Context
            </Label>
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
            <Label
              htmlFor="override-worktree"
              className="text-xs text-muted-foreground"
            >
              Worktree Override
            </Label>
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
            <Label
              htmlFor="override-validation"
              className="text-xs text-muted-foreground"
            >
              Validation Commands
            </Label>
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
              {
                label: "Repo reachable",
                pass:
                  run.validationSummary.errors === 0 ||
                  !run.validationSummary.issues?.some((i: any) =>
                    i.message?.toLowerCase().includes("repo"),
                  ),
              },
              {
                label: "Branch exists",
                pass:
                  run.validationSummary.errors === 0 ||
                  !run.validationSummary.issues?.some((i: any) =>
                    i.message?.toLowerCase().includes("branch"),
                  ),
              },
              {
                label: "No uncommitted changes",
                pass: run.status !== "intake_needs_review",
              },
              {
                label: "Validation commands extractable",
                pass: run.validationSummary.errors === 0,
              },
            ].map((check) => (
              <div
                key={check.label}
                className="flex items-center gap-2 text-xs"
              >
                {check.pass ? (
                  <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
                ) : (
                  <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
                )}
                <span
                  className={check.pass ? "text-foreground" : "text-yellow-400"}
                >
                  {check.label}
                </span>
                <Badge
                  variant={check.pass ? "secondary" : "destructive"}
                  className="ml-auto text-[10px] h-5 py-0"
                >
                  {check.pass ? "OK" : "Review"}
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
      <Section
        title="Validation Results"
        icon={<AlertTriangle className="w-4 h-4 text-yellow-400" />}
      >
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-4 text-xs">
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full bg-red-500" /> Errors:{" "}
              {run.validationSummary?.errors ?? 0}
            </span>
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full bg-yellow-500" /> Warnings:{" "}
              {run.validationSummary?.warnings ?? 0}
            </span>
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full bg-green-500" /> Passed:{" "}
              {run.validationSummary?.passed ?? 0}
            </span>
          </div>
          {run.validationSummary?.issues &&
          run.validationSummary.issues.length > 0 ? (
            <div className="flex flex-col gap-1.5 mt-1 border border-border/40 rounded bg-muted/20 p-2 max-h-36 overflow-y-auto">
              {run.validationSummary.issues.map((issue: any, idx: number) => (
                <div
                  key={idx}
                  className="flex items-start gap-1.5 text-xs text-foreground/80 leading-normal"
                >
                  <span
                    className={
                      issue.severity === "error"
                        ? "text-red-400 font-bold shrink-0"
                        : "text-yellow-400 font-bold shrink-0"
                    }
                  >
                    [{issue.severity.toUpperCase()}]
                  </span>
                  <span>{issue.message}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground italic">
              No validation issues found.
            </p>
          )}
        </div>
      </Section>

      <Separator />

      {/* Section: Approval Gate */}
      <Section
        title="Approval Gate"
        icon={<ShieldCheck className="w-4 h-4 text-primary" />}
      >
        <div className="flex flex-col gap-3 mt-1">
          {!isReviewable && (
            <RelayInlineState
              tone="warning"
              title="Intake review inactive"
              description={`Run is currently in ${run.state || run.status} state.`}
            />
          )}

          {isReviewable && (
            <div className="flex flex-col gap-2">
              <Label
                htmlFor="review-notes"
                className="text-xs text-muted-foreground"
              >
                Review Notes (Optional)
              </Label>
              <Textarea
                id="review-notes"
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Provide details about approval or revision requirements..."
                className="h-16 text-xs bg-background/50 resize-none"
                disabled={isPending}
              />

              <div className="flex flex-wrap items-center gap-2 mt-1">
                <Button
                  variant="default"
                  size="sm"
                  onClick={() => handleSubmit("approve")}
                  disabled={isPending}
                  className="bg-emerald-600 hover:bg-emerald-700 text-white gap-1.5"
                >
                  <ShieldCheck className="w-3.5 h-3.5" />
                  Approve Intake
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleSubmit("needs_revision")}
                  disabled={isPending}
                  className="gap-1.5 text-yellow-500 hover:text-yellow-400 border-yellow-500/30 hover:border-yellow-500/50 hover:bg-yellow-500/10"
                >
                  <AlertTriangle className="w-3.5 h-3.5" />
                  Needs Revision
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => handleSubmit("blocked")}
                  disabled={isPending}
                  className="gap-1.5"
                >
                  <ShieldX className="w-3.5 h-3.5" />
                  Block Run
                </Button>
              </div>
            </div>
          )}

          {(run.status === "approved_for_prepare" ||
            run.activeStep === "prepare") && (
            <div className="flex flex-col gap-2 p-3 rounded bg-emerald-950/20 border border-emerald-950/40 text-xs text-foreground mt-2">
              <div className="flex items-center gap-2 text-emerald-400 font-medium">
                <CheckCircle2 className="w-4 h-4 shrink-0" />
                <span>Intake Approved Successfully!</span>
              </div>
              <p className="text-muted-foreground leading-normal">
                This run is now approved for brief compilation and environment
                preparation.
              </p>
              <Button
                size="sm"
                asChild
                className="w-full mt-1.5 gap-1.5 bg-emerald-600 hover:bg-emerald-700"
              >
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
  );
}

function Section({
  title,
  icon,
  children,
}: {
  title: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Card className="min-w-0 border-border/60 bg-card/20">
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="min-w-0 p-3 pt-0 flex flex-col gap-1.5">
        {children}
      </CardContent>
    </Card>
  );
}

function KeyValueRow({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-baseline gap-2 text-xs">
      <span className="text-muted-foreground w-32 shrink-0">{label}</span>
      <span
        className={mono ? "min-w-0 break-words font-mono text-foreground" : "min-w-0 break-words text-foreground"}
      >
        {value}
      </span>
    </div>
  );
}
