import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CircleAlert,
  ExternalLink,
  FileText,
  Server,
  ShieldCheck,
  ShieldX,
} from "lucide-react";

import type { RelayArtifact, RelayRun } from "@/features/relay-runs";
import { approveIntake } from "@/features/relay-runs";
import { RelayStateBanner } from "@/components/relay/RelayStateSurface";
import { RunStageSummaryChip } from "@/components/relay/RunStagePrimitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface RunIntakeReviewPanelProps {
  run: RelayRun;
  artifacts: RelayArtifact[];
}

export type RunIntakeReviewController = ReturnType<
  typeof useRunIntakeReviewController
>;

interface RunIntakeReviewPanelViewProps {
  controller: RunIntakeReviewController;
}

function findArtifact(
  artifacts: RelayArtifact[],
  predicate: (artifact: RelayArtifact) => boolean,
) {
  return artifacts.find(predicate);
}

function parsePreviewObject(preview?: string): Record<string, any> | null {
  if (!preview) {
    return null;
  }

  try {
    const parsed = JSON.parse(preview);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, any>;
    }
  } catch {
    return null;
  }

  return null;
}

function getStatusTone(status: string) {
  if (status === "approved_for_prepare") {
    return "success" as const;
  }
  if (status === "blocked") {
    return "danger" as const;
  }
  if (status === "intake_needs_review") {
    return "warning" as const;
  }
  if (status === "intake_received") {
    return "info" as const;
  }
  return "default" as const;
}

function renderRepoValue(value: string) {
  const isLocalPath =
    value.includes("/") || value.includes("\\") || value.includes(":");
  const isGitHubRepo = /^[a-zA-Z0-9._-]+\/[a-zA-Z0-9._-]+$/.test(value);

  if (isLocalPath) {
    return <span className="font-mono text-[13px]">{value}</span>;
  }

  if (isGitHubRepo) {
    return (
      <a
        href={`https://github.com/${value}`}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 font-mono text-[13px] underline-offset-4 hover:underline"
      >
        {value}
        <ExternalLink className="h-3.5 w-3.5" />
      </a>
    );
  }

  return value;
}

function renderStatusTone(
  tone: "default" | "info" | "success" | "warning" | "danger",
) {
  if (tone === "success") {
    return "success" as const;
  }
  if (tone === "warning" || tone === "danger") {
    return "warning" as const;
  }
  return "default" as const;
}

function InlineHint({
  source,
  detail,
}: {
  source?: string;
  detail?: React.ReactNode;
}) {
  if (!source && !detail) {
    return null;
  }

  return (
    <div className="flex flex-col gap-1 text-xs text-muted-foreground">
      {source ? <p>Source: {source}</p> : null}
      {detail ? <p>{detail}</p> : null}
    </div>
  );
}

export function useRunIntakeReviewController({
  run,
  artifacts,
}: RunIntakeReviewPanelProps) {
  const queryClient = useQueryClient();
  const [mutationError, setMutationError] = React.useState<string | null>(null);

  const runConfigArtifact = findArtifact(
    artifacts,
    (artifact) =>
      artifact.filename === "run_config.json" || artifact.kind === "run_config",
  );
  const parsedFrontmatter = findArtifact(
    artifacts,
    (artifact) =>
      artifact.filename === "parsed_frontmatter.json" ||
      artifact.kind === "parsed_frontmatter",
  );

  const runConfig = parsePreviewObject(runConfigArtifact?.preview) || {};
  const frontmatterObject = parsePreviewObject(parsedFrontmatter?.preview);
  const hasFrontmatter = Boolean(
    frontmatterObject && Object.keys(frontmatterObject).length > 0,
  );

  const initialValCommands =
    typeof runConfig.validation_commands === "string"
      ? runConfig.validation_commands
      : "";

  const [model, setModel] = React.useState(run.model || "");
  const [repo, setRepo] = React.useState(run.repo || "");
  const [branch, setBranch] = React.useState(run.branch || "");
  const [worktree, setWorktree] = React.useState(run.worktree || "");
  const [executorAdapter, setExecutorAdapter] = React.useState(
    run.executorAdapter || "opencode_go",
  );
  const [validationCommands, setValidationCommands] =
    React.useState(initialValCommands);

  React.useEffect(() => {
    if (run.model) {
      setModel(run.model);
    }
    if (run.repo) {
      setRepo(run.repo);
    }
    if (run.branch) {
      setBranch(run.branch);
    }
    if (run.worktree) {
      setWorktree(run.worktree);
    }
    if (run.executorAdapter) {
      setExecutorAdapter(run.executorAdapter);
    }
  }, [run.model, run.repo, run.branch, run.worktree, run.executorAdapter]);

  React.useEffect(() => {
    if (runConfigArtifact?.preview) {
      const parsedConfig = parsePreviewObject(runConfigArtifact.preview);
      if (!parsedConfig) {
        return;
      }

      if (typeof parsedConfig.validation_commands === "string") {
        setValidationCommands(parsedConfig.validation_commands);
      }
      if (typeof parsedConfig.worktree === "string") {
        setWorktree(parsedConfig.worktree);
      }
      if (typeof parsedConfig.executor_adapter === "string") {
        setExecutorAdapter(parsedConfig.executor_adapter);
      }
    }
  }, [runConfigArtifact]);

  const { mutate, isPending } = useMutation({
    mutationFn: ({ requestPayload }: { requestPayload: any }) =>
      approveIntake(run.id, requestPayload),
    onSuccess: () => {
      setMutationError(null);
      void queryClient.invalidateQueries({ queryKey: ["relay-runs"] });
    },
    onError: (error: unknown) => {
      setMutationError(
        error instanceof Error
          ? error.message
          : "Failed to submit intake review.",
      );
    },
  });

  const isReviewable =
    run.status === "intake_needs_review" || run.status === "intake_received";

  const handleSubmit = (action: "approve" | "needs_revision" | "blocked") => {
    setMutationError(null);

    mutate({
      requestPayload: {
        action,
        notes: "",
        overrides: {
          model: model !== run.model ? model.trim() : undefined,
          repo: repo !== run.repo ? repo.trim() : undefined,
          branch: branch !== run.branch ? branch.trim() : undefined,
          worktree: worktree !== run.worktree ? worktree.trim() : undefined,
          executorAdapter:
            executorAdapter !== run.executorAdapter
              ? executorAdapter
              : undefined,
          validationCommands:
            validationCommands !== initialValCommands
              ? validationCommands.trim()
              : undefined,
        },
      },
    });
  };

  const repoTarget =
    typeof runConfig.repo_target === "string" && runConfig.repo_target
      ? runConfig.repo_target
      : run.repo;
  const branchContext =
    typeof runConfig.branch_context === "string" && runConfig.branch_context
      ? runConfig.branch_context
      : run.branch;
  const configSource =
    typeof runConfig.source === "string" && runConfig.source
      ? runConfig.source
      : "unknown";
  const createdFrom =
    typeof runConfig.created_from === "string" && runConfig.created_from
      ? runConfig.created_from
      : "unknown";

  const repoSource =
    frontmatterObject?.repo || frontmatterObject?.repo_target
      ? "parsed frontmatter"
      : runConfig.repo_target
        ? "explicit MCP arg"
        : "resolved repo";
  const branchSource =
    frontmatterObject?.branch || frontmatterObject?.branch_context
      ? "parsed frontmatter"
      : runConfig.branch_context
        ? "explicit MCP arg"
        : "fallback default";
  const worktreeSource =
    typeof runConfig.worktree === "string" && runConfig.worktree
      ? "run config"
      : run.worktree
        ? "current run value"
        : undefined;
  const modelSource = run.model ? "current run value" : undefined;
  const executorSource =
    typeof runConfig.executor_adapter === "string" && runConfig.executor_adapter
      ? "run config"
      : run.executorAdapter
        ? "current run value"
        : "default adapter";
  const validationSource =
    typeof runConfig.validation_commands === "string" &&
    runConfig.validation_commands
      ? "run config"
      : validationCommands
        ? "current run value"
        : undefined;

  const validationSummary = run.validationSummary;
  const validationIssues = validationSummary?.issues || [];
  const summaryStatusTone = getStatusTone(run.status);
  const readinessIssues = validationIssues.slice(0, 3);

  const preflightChecks = validationSummary
    ? [
        {
          label: "Repo reachable",
          pass:
            validationSummary.errors === 0 ||
            !validationIssues.some((issue) =>
              issue.message?.toLowerCase().includes("repo"),
            ),
        },
        {
          label: "Branch exists",
          pass:
            validationSummary.errors === 0 ||
            !validationIssues.some((issue) =>
              issue.message?.toLowerCase().includes("branch"),
            ),
        },
        {
          label: "No uncommitted changes",
          pass: run.status !== "intake_needs_review",
        },
        {
          label: "Validation commands extractable",
          pass: validationSummary.errors === 0,
        },
      ]
    : [];
  const preflightPassedCount = preflightChecks.filter((check) => check.pass).length;
  const preflightSummary =
    preflightChecks.length > 0
      ? `${preflightPassedCount}/${preflightChecks.length} checks OK`
      : "Preflight pending";
  const isApproved =
    run.status === "approved_for_prepare" || run.activeStep === "prepare";

  return {
    run,
    mutationError,
    isPending,
    isReviewable,
    hasFrontmatter,
    model,
    setModel,
    repo,
    setRepo,
    branch,
    setBranch,
    worktree,
    setWorktree,
    executorAdapter,
    setExecutorAdapter,
    validationCommands,
    setValidationCommands,
    handleSubmit,
    repoTarget,
    branchContext,
    configSource,
    createdFrom,
    repoSource,
    branchSource,
    worktreeSource,
    modelSource,
    executorSource,
    validationSource,
    validationSummary,
    readinessIssues,
    summaryStatusTone,
    preflightChecks,
    preflightPassedCount,
    preflightSummary,
    isApproved,
  };
}

export function RunIntakeStageActions({
  controller,
}: {
  controller: RunIntakeReviewController;
}) {
  const { run, isPending, isReviewable, handleSubmit } = controller;

  if (run.status === "approved_for_prepare" || run.activeStep === "prepare") {
    return (
      <Button size="sm" asChild>
        <Link to="/runs/$runId/prepare" params={{ runId: run.id }}>
          Proceed to Compile / Render
          <ArrowRight className="ml-1.5 h-3.5 w-3.5" />
        </Link>
      </Button>
    );
  }

  if (!isReviewable) {
    return null;
  }

  return (
    <div className="flex flex-wrap justify-end gap-2 py-2">
      <Button size="sm" onClick={() => handleSubmit("approve")} disabled={isPending}>
        <ShieldCheck className="mr-1.5 h-3.5 w-3.5" />
        Approve Intake
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={() => handleSubmit("needs_revision")}
        disabled={isPending}
      >
        <AlertTriangle className="mr-1.5 h-3.5 w-3.5" />
        Needs Revision
      </Button>
      <Button
        variant="destructive"
        size="sm"
        onClick={() => handleSubmit("blocked")}
        disabled={isPending}
      >
        <ShieldX className="mr-1.5 h-3.5 w-3.5" />
        Block Run
      </Button>
    </div>
  );
}

export function RunIntakeReviewPanel({
  controller,
}: RunIntakeReviewPanelViewProps) {
  const {
    run,
    mutationError,
    isPending,
    isReviewable,
    hasFrontmatter,
    model,
    setModel,
    repo,
    setRepo,
    branch,
    setBranch,
    worktree,
    setWorktree,
    executorAdapter,
    setExecutorAdapter,
    validationCommands,
    setValidationCommands,
    repoTarget,
    branchContext,
    configSource,
    createdFrom,
    repoSource,
    branchSource,
    worktreeSource,
    modelSource,
    executorSource,
    validationSource,
    validationSummary,
    readinessIssues,
    summaryStatusTone,
    preflightChecks,
    preflightPassedCount,
    preflightSummary,
    isApproved,
  } = controller;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {mutationError ? (
        <RelayStateBanner
          tone="danger"
          title="Intake review failed"
          description={mutationError}
        />
      ) : null}

      <section className="overflow-hidden rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
        <div className="grid gap-0 lg:grid-cols-[minmax(13rem,0.8fr)_minmax(28rem,1.7fr)_minmax(15rem,0.95fr)]">
          <aside className="min-w-0 border-b border-[var(--relay-row-border)] px-4 py-4 lg:border-r lg:border-b-0">
            <div className="flex min-w-0 items-start gap-3">
              <div className="mt-0.5 shrink-0 text-muted-foreground">
                <FileText className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                  Handoff
                </p>
                <div className="mt-2 space-y-2">
                  <div>
                    <p className="text-sm font-semibold text-foreground">
                      {run.title}
                    </p>
                    <p className="mt-1 font-mono text-[12px] text-muted-foreground">
                      {run.packetId || "No packet ID captured"}
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <RunStageSummaryChip
                      value={run.status}
                      tone={summaryStatusTone}
                      mono
                    />
                    <RunStageSummaryChip
                      value={
                        hasFrontmatter ? "Frontmatter parsed" : "No frontmatter"
                      }
                      tone={hasFrontmatter ? "success" : "warning"}
                    />
                  </div>
                </div>
              </div>
            </div>

            <div className="mt-4 grid gap-2">
              <div className="rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2.5">
                <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                  Source
                </p>
                <div className="mt-2 text-sm text-foreground">{configSource}</div>
              </div>
              <div className="rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2.5">
                <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                  Created By
                </p>
                <div className="mt-2 text-sm text-foreground">{createdFrom}</div>
              </div>
              <div className="rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2.5">
                <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                  Target Snapshot
                </p>
                <div className="mt-2 space-y-1 text-sm text-foreground">
                  <div className="min-w-0 break-words">
                    {renderRepoValue(repo || repoTarget || " - ")}
                  </div>
                  <div className="font-mono text-[12px] text-muted-foreground">
                    {branch || branchContext || " - "}
                  </div>
                  <div className="font-mono text-[12px] text-muted-foreground">
                    Worktree: {worktree || "default"}
                  </div>
                </div>
              </div>
            </div>
          </aside>

          <section className="min-w-0 border-b border-[var(--relay-row-border)] px-4 py-4 lg:border-r lg:border-b-0">
            <div className="flex min-w-0 items-start gap-3">
              <div className="mt-0.5 shrink-0 text-muted-foreground">
                <Server className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                      Run Configuration
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      Adjust the execution target and workspace details before
                      approving the intake.
                    </p>
                  </div>
                  <RunStageSummaryChip
                    label="Preflight"
                    value={preflightSummary}
                    tone={
                      preflightChecks.length > 0 &&
                      preflightPassedCount === preflightChecks.length
                        ? "success"
                        : "warning"
                    }
                  />
                </div>
              </div>
            </div>

            <p className="mt-4 text-xs text-muted-foreground">
              Review the editable intake configuration first. Provenance stays
              inline with each control so the operating surface remains focused.
            </p>

            <div className="mt-4 grid gap-3 md:grid-cols-2">
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
                  onChange={(event) => setRepo(event.target.value)}
                  placeholder="e.g. d:\\Code\\relay"
                  disabled={isPending || !isReviewable}
                />
                <InlineHint
                  source={repoSource}
                  detail={
                    repoTarget && repoTarget !== repo ? (
                      <>
                        Resolved intake target:{" "}
                        <span className="font-mono text-[11px]">{repoTarget}</span>
                      </>
                    ) : undefined
                  }
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
                  onChange={(event) => setBranch(event.target.value)}
                  placeholder="e.g. main"
                  disabled={isPending || !isReviewable}
                />
                <InlineHint
                  source={branchSource}
                  detail={
                    branchContext && branchContext !== branch ? (
                      <>
                        Resolved intake branch:{" "}
                        <span className="font-mono text-[11px]">
                          {branchContext}
                        </span>
                      </>
                    ) : undefined
                  }
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
                  onChange={(event) => setWorktree(event.target.value)}
                  placeholder="e.g. my-worktree"
                  disabled={isPending || !isReviewable}
                />
                <InlineHint
                  source={worktreeSource}
                  detail={
                    !worktree && !worktreeSource
                      ? "Optional override; Relay will use the run workspace when left blank."
                      : undefined
                  }
                />
              </div>

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
                  onChange={(event) => setModel(event.target.value)}
                  placeholder="e.g. deepseek-v4-flash"
                  disabled={isPending || !isReviewable}
                />
                <InlineHint source={modelSource} />
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
                  <SelectTrigger id="override-executor">
                    <SelectValue placeholder="Select executor adapter" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="opencode_go">OpenCode (Go)</SelectItem>
                      <SelectItem value="codex">Codex (TypeScript)</SelectItem>
                      <SelectItem value="antigravity">
                        Antigravity (Go)
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                {executorAdapter === "codex" ? (
                  <p className="text-xs text-muted-foreground">
                    Codex dispatch uses the local Codex CLI configuration and
                    authentication available to the Relay daemon.
                  </p>
                ) : null}
                {executorAdapter === "antigravity" ? (
                  <p className="text-xs text-muted-foreground">
                    Antigravity dispatch uses the local Antigravity CLI
                    configuration and authentication available to the Relay
                    daemon.
                  </p>
                ) : null}
                <InlineHint source={executorSource} />
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
                  onChange={(event) => setValidationCommands(event.target.value)}
                  placeholder="e.g. go test ./..."
                  disabled={isPending || !isReviewable}
                />
                <InlineHint source={validationSource} />
              </div>
            </div>
          </section>

          <aside className="min-w-0 px-4 py-4">
            <div className="flex min-w-0 items-start gap-3">
              <div className="mt-0.5 shrink-0 text-muted-foreground">
                <ShieldCheck className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <p className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
                  Readiness
                </p>
                <div className="mt-2 flex flex-wrap gap-2">
                  <RunStageSummaryChip
                    label="Frontmatter"
                    value={hasFrontmatter ? "Parsed" : "Missing"}
                    tone={hasFrontmatter ? "success" : "warning"}
                  />
                  <RunStageSummaryChip
                    label="Validation"
                    value={`${validationSummary?.errors ?? 0} errors`}
                    tone={
                      (validationSummary?.errors ?? 0) > 0 ? "warning" : "success"
                    }
                  />
                </div>
              </div>
            </div>

            <div className="mt-4 grid gap-2">
              {preflightChecks.length > 0 ? (
                preflightChecks.map((check) => (
                  <div
                    key={check.label}
                    className="flex flex-wrap items-center justify-between gap-2 rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2"
                  >
                    <div className="flex min-w-0 items-center gap-2 text-sm text-foreground">
                      {check.pass ? (
                        <CheckCircle2 className="h-4 w-4 shrink-0" />
                      ) : (
                        <AlertTriangle className="h-4 w-4 shrink-0" />
                      )}
                      <span className="min-w-0 flex-1">{check.label}</span>
                    </div>
                    <RunStageSummaryChip
                      value={check.pass ? "OK" : "Review"}
                      tone={check.pass ? "success" : "warning"}
                    />
                  </div>
                ))
              ) : (
                <p className="text-xs text-muted-foreground">
                  Preflight not available from current intake data.
                </p>
              )}
            </div>

            <div className="mt-4 rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-3">
              <p className="text-[10px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
                Validation Summary
              </p>
              <div className="mt-3 grid grid-cols-3 gap-2">
                <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2.5 py-2">
                  <p className="text-[10px] uppercase tracking-[0.06em] text-muted-foreground">
                    Errors
                  </p>
                  <p className="mt-1 text-base font-semibold text-foreground">
                    {validationSummary?.errors ?? 0}
                  </p>
                </div>
                <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2.5 py-2">
                  <p className="text-[10px] uppercase tracking-[0.06em] text-muted-foreground">
                    Warnings
                  </p>
                  <p className="mt-1 text-base font-semibold text-foreground">
                    {validationSummary?.warnings ?? 0}
                  </p>
                </div>
                <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2.5 py-2">
                  <p className="text-[10px] uppercase tracking-[0.06em] text-muted-foreground">
                    Passed
                  </p>
                  <p className="mt-1 text-base font-semibold text-foreground">
                    {validationSummary?.passed ?? 0}
                  </p>
                </div>
              </div>

              {readinessIssues.length > 0 ? (
                <div className="mt-3 space-y-2">
                  {readinessIssues.map((issue, index) => (
                    <div
                      key={`${issue.code}-${index}`}
                      className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <RunStageSummaryChip
                          value={issue.severity}
                          tone={renderStatusTone(
                            issue.severity === "error"
                              ? "danger"
                              : issue.severity === "warning"
                                ? "warning"
                                : "default",
                          )}
                        />
                        <span className="font-mono text-[11px] text-muted-foreground">
                          {issue.code}
                        </span>
                      </div>
                      <p className="mt-2 text-xs text-foreground">
                        {issue.message}
                      </p>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          </aside>
        </div>
      </section>

      {isApproved ? (
        <div className="rounded border border-[var(--success)]/35 bg-[var(--success)]/10 px-4 py-3">
          <div className="flex items-start gap-3">
            <CircleAlert className="mt-0.5 h-4 w-4 shrink-0 text-[var(--success)]" />
            <div className="min-w-0">
              <p className="text-sm font-semibold text-foreground">
                Intake approved
              </p>
              <p className="mt-1 text-sm text-muted-foreground">
                This run is ready to move into Compile / Render with the current
                configuration.
              </p>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
