import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ExternalLink,
  FileText,
  FolderGit2,
  Server,
  ShieldCheck,
  ShieldX,
} from "lucide-react";

import type { RelayArtifact, RelayRun } from "@/features/relay-runs";
import { approveIntake } from "@/features/relay-runs";
import {
  RelayInlineState,
  RelayStateBanner,
} from "@/components/relay/RelayStateSurface";
import {
  RunStageKeyValueGrid,
  RunStagePreviewBlock,
  RunStageProvenanceTable,
  RunStageSection,
  RunStageSummaryCard,
  RunStageSummaryChip,
} from "@/components/relay/RunStagePrimitives";
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

interface RunIntakeReviewPanelProps {
  run: RelayRun;
  artifacts: RelayArtifact[];
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

function renderUnavailablePreview(artifact: RelayArtifact | undefined, message: string) {
  return (
    <div className="flex flex-col gap-2 text-xs text-muted-foreground">
      <p className="italic">{message}</p>
      {artifact ? (
        <p className="font-mono text-[11px]">
          {artifact.filename} | {artifact.path} | {artifact.sizeHint || "unknown"}
        </p>
      ) : null}
    </div>
  );
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

export function RunIntakeReviewPanel({
  run,
  artifacts,
}: RunIntakeReviewPanelProps) {
  const queryClient = useQueryClient();
  const [notes, setNotes] = React.useState("");
  const [mutationError, setMutationError] = React.useState<string | null>(null);

  const runConfigArtifact = findArtifact(
    artifacts,
    (artifact) =>
      artifact.filename === "run_config.json" || artifact.kind === "run_config",
  );
  const plannerHandoff = findArtifact(
    artifacts,
    (artifact) =>
      artifact.filename === "planner_handoff.md" || artifact.kind === "handoff",
  );
  const parsedFrontmatter = findArtifact(
    artifacts,
    (artifact) => artifact.filename === "parsed_frontmatter.json",
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
      setNotes("");
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

  const repoSource = frontmatterObject?.repo
    ? "parsed frontmatter"
    : runConfig.repo_target
      ? "explicit MCP arg"
      : "resolved repo";
  const branchSource = frontmatterObject?.branch
    ? "parsed frontmatter"
    : runConfig.branch_context
      ? "explicit MCP arg"
      : "fallback default";
  const titleSource = frontmatterObject?.title
    ? "parsed frontmatter"
    : "markdown H1";

  const validationSummary = run.validationSummary;
  const validationIssues = validationSummary?.issues || [];
  const summaryStatusTone = getStatusTone(run.status);

  const contextRows = [
    {
      label:
        !run.repo.includes("/") &&
        !run.repo.includes("\\") &&
        !run.repo.includes(":")
          ? "Repo display name"
          : "Repo target",
      value: renderRepoValue(run.repo),
      mono: false,
    },
    ...(repoTarget !== run.repo
      ? [
          {
            label: "Resolved target/path",
            value: renderRepoValue(repoTarget),
            mono: false,
          },
        ]
      : []),
    {
      label: "Branch context",
      value: branchContext || " - ",
      mono: true,
    },
    {
      label: "Source",
      value: configSource,
    },
    {
      label: "Created by",
      value: createdFrom,
    },
    {
      label: "Worktree",
      value: run.worktree || " - ",
      mono: true,
    },
    {
      label: "Executor adapter",
      value: run.executorAdapter || " - ",
      mono: true,
    },
    {
      label: "Model",
      value: run.model || " - ",
      mono: true,
    },
  ];

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

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {mutationError ? (
        <RelayStateBanner
          tone="danger"
          title="Intake review failed"
          description={mutationError}
        />
      ) : null}

      <div className="flex flex-wrap gap-2">
        <RunStageSummaryChip
          label="Status"
          value={run.status}
          tone={summaryStatusTone}
          mono
        />
        <RunStageSummaryChip label="Packet" value={run.packetId || " - "} mono />
        <RunStageSummaryChip label="Repo" value={repoTarget || " - "} mono />
        <RunStageSummaryChip
          label="Branch"
          value={branchContext || " - "}
          mono
        />
        <RunStageSummaryChip label="Source" value={configSource} />
      </div>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <RunStageSummaryCard
          eyebrow="Incoming Handoff"
          title={run.title}
          description={run.packetId || "No packet ID captured"}
          icon={<FileText className="h-4 w-4" />}
          status={
            <RunStageSummaryChip
              value={run.status}
              tone={summaryStatusTone}
              mono
            />
          }
        />
        <RunStageSummaryCard
          eyebrow="Repository"
          title={repoTarget || " - "}
          description={branchContext || " - "}
          icon={<FolderGit2 className="h-4 w-4" />}
        />
        <RunStageSummaryCard
          eyebrow="Execution"
          title={model || run.model || " - "}
          description={executorAdapter || run.executorAdapter || " - "}
          icon={<Server className="h-4 w-4" />}
        />
        <RunStageSummaryCard
          eyebrow="Validation"
          title={`${validationSummary?.errors ?? 0} errors`}
          description={`${validationSummary?.warnings ?? 0} warnings · ${validationSummary?.passed ?? 0} passed`}
          icon={<ShieldCheck className="h-4 w-4" />}
        />
      </div>

      {!hasFrontmatter ? (
        <RelayStateBanner
          tone="warning"
          title="No YAML frontmatter parsed"
          description="No YAML frontmatter was parsed from the submitted handoff. Relay used explicit MCP/API arguments and fallback defaults where available."
        />
      ) : null}

      <RunStageSection
        title="Incoming Handoff"
        subtitle="Review the submitted handoff and parsed frontmatter before changing runtime configuration."
        icon={<FileText className="h-4 w-4" />}
        contentClassName="flex flex-col gap-4"
      >
        <RunStageKeyValueGrid
          rows={[
            {
              label: "Packet ID",
              value: run.packetId || " - ",
              mono: true,
            },
            {
              label: "Title",
              value: run.title || " - ",
              emphasis: true,
            },
            {
              label: "Status",
              value: run.status,
              mono: true,
            },
          ]}
          columns={2}
        />

        <div className="grid gap-4 xl:grid-cols-2">
          <RunStagePreviewBlock
            title="Planner Handoff Preview"
            subtitle="Captured markdown from the intake packet."
          >
            {plannerHandoff?.preview
              ? plannerHandoff.preview
              : renderUnavailablePreview(
                  plannerHandoff,
                  "Handoff preview content is unavailable.",
                )}
          </RunStagePreviewBlock>

          <RunStagePreviewBlock
            title="Parsed Frontmatter Preview"
            subtitle="Structured metadata extracted from the handoff."
          >
            {parsedFrontmatter?.preview
              ? parsedFrontmatter.preview
              : renderUnavailablePreview(
                  parsedFrontmatter,
                  "Parsed frontmatter preview is unavailable.",
                )}
          </RunStagePreviewBlock>
        </div>
      </RunStageSection>

      <RunStageSection
        title="Configuration Provenance"
        subtitle="Track where the key intake values were sourced before approval."
        icon={<FolderGit2 className="h-4 w-4" />}
      >
        <RunStageProvenanceTable
          rows={[
            {
              field: "Repo",
              value: repoTarget || " - ",
              source: repoSource,
              valueMono: true,
            },
            {
              field: "Branch",
              value: branchContext || " - ",
              source: branchSource,
              valueMono: true,
            },
            {
              field: "Title",
              value: run.title || " - ",
              source: titleSource,
            },
          ]}
        />
      </RunStageSection>

      <RunStageSection
        title="Resolved Intake Context"
        subtitle="This is the run context Relay will carry into prepare and execution."
        icon={<FolderGit2 className="h-4 w-4" />}
      >
        <RunStageKeyValueGrid rows={contextRows} columns={2} />
      </RunStageSection>

      <RunStageSection
        title="Run Configuration"
        subtitle="Adjust the execution target and workspace details before approving the intake."
        icon={<Server className="h-4 w-4" />}
      >
        <div className="grid gap-3 md:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-model" className="text-xs text-muted-foreground">
              Target Model
            </Label>
            <Input
              id="override-model"
              value={model}
              onChange={(event) => setModel(event.target.value)}
              placeholder="e.g. deepseek-v4-flash"
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
              <SelectTrigger id="override-executor">
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
            {executorAdapter === "codex" ? (
              <p className="text-xs text-muted-foreground">
                Codex dispatch uses the local Codex CLI configuration and
                authentication available to the Relay daemon.
              </p>
            ) : null}
            {executorAdapter === "antigravity" ? (
              <p className="text-xs text-muted-foreground">
                Antigravity dispatch uses the local Antigravity CLI
                configuration and authentication available to the Relay daemon.
              </p>
            ) : null}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-repo" className="text-xs text-muted-foreground">
              Repository Target Path
            </Label>
            <Input
              id="override-repo"
              value={repo}
              onChange={(event) => setRepo(event.target.value)}
              placeholder="e.g. d:\\Code\\relay"
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
              onChange={(event) => setBranch(event.target.value)}
              placeholder="e.g. main"
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
              onChange={(event) => setWorktree(event.target.value)}
              placeholder="e.g. my-worktree"
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
              onChange={(event) => setValidationCommands(event.target.value)}
              placeholder="e.g. go test ./..."
              disabled={isPending || !isReviewable}
            />
          </div>
        </div>
      </RunStageSection>

      <div className="grid gap-4 xl:grid-cols-2">
        <RunStageSection
          title="Repo Workspace Preflight"
          subtitle="Quick intake checks derived from the current validation summary."
          icon={<CheckCircle2 className="h-4 w-4" />}
        >
          {preflightChecks.length > 0 ? (
            <div className="flex flex-col gap-2">
              {preflightChecks.map((check) => (
                <div
                  key={check.label}
                  className="flex flex-wrap items-center justify-between gap-2 rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40 px-3 py-2"
                >
                  <div className="flex items-center gap-2 text-sm text-foreground">
                    {check.pass ? (
                      <CheckCircle2 className="h-4 w-4" />
                    ) : (
                      <AlertTriangle className="h-4 w-4" />
                    )}
                    <span>{check.label}</span>
                  </div>
                  <RunStageSummaryChip
                    value={check.pass ? "OK" : "Review"}
                    tone={check.pass ? "success" : "warning"}
                  />
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              Preflight not available from current intake data.
            </p>
          )}
        </RunStageSection>

        <RunStageSection
          title="Validation Results"
          subtitle="Keep the intake review compact while surfacing the current issues."
          icon={<AlertTriangle className="h-4 w-4" />}
          contentClassName="flex flex-col gap-3"
        >
          <div className="flex flex-wrap gap-2">
            <RunStageSummaryChip
              label="Errors"
              value={String(validationSummary?.errors ?? 0)}
              tone={(validationSummary?.errors ?? 0) > 0 ? "danger" : "default"}
            />
            <RunStageSummaryChip
              label="Warnings"
              value={String(validationSummary?.warnings ?? 0)}
              tone={(validationSummary?.warnings ?? 0) > 0 ? "warning" : "default"}
            />
            <RunStageSummaryChip
              label="Passed"
              value={String(validationSummary?.passed ?? 0)}
              tone="success"
            />
          </div>

          {validationIssues.length > 0 ? (
            <div className="max-h-48 overflow-y-auto rounded border border-[var(--relay-row-border)] bg-[var(--surface-inset)]/40">
              <div className="flex flex-col divide-y divide-[var(--relay-row-border)]">
                {validationIssues.map((issue, index) => (
                  <div
                    key={`${issue.code}-${index}`}
                    className="flex items-start gap-3 px-3 py-2 text-sm"
                  >
                    <RunStageSummaryChip
                      value={issue.severity.toUpperCase()}
                      tone={issue.severity === "error" ? "danger" : "warning"}
                    />
                    <span className="min-w-0 flex-1 break-words text-foreground">
                      {issue.message}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              No validation issues found.
            </p>
          )}
        </RunStageSection>
      </div>

      <RunStageSection
        title="Approval Gate"
        subtitle="Approve the intake as-is, send it back for revision, or block the run."
        icon={<ShieldCheck className="h-4 w-4" />}
        contentClassName="flex flex-col gap-3"
      >
        {!isReviewable ? (
          <RelayInlineState
            tone="warning"
            title="Intake review inactive"
            description={`Run is currently in ${run.state || run.status} state.`}
          />
        ) : null}

        {isReviewable ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="review-notes" className="text-xs text-muted-foreground">
                Review Notes (Optional)
              </Label>
              <Textarea
                id="review-notes"
                value={notes}
                onChange={(event) => setNotes(event.target.value)}
                placeholder="Provide details about approval or revision requirements..."
                className="min-h-24 resize-y"
                disabled={isPending}
              />
            </div>

            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                onClick={() => handleSubmit("approve")}
                disabled={isPending}
              >
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
          </div>
        ) : null}

        {run.status === "approved_for_prepare" || run.activeStep === "prepare" ? (
          <RelayStateBanner
            tone="success"
            title="Intake Approved Successfully!"
            description="This run is now approved for brief compilation and environment preparation."
            action={
              <Button size="sm" asChild>
                <Link to="/runs/$runId/prepare" params={{ runId: run.id }}>
                  Proceed to Compile / Render
                  <ArrowRight className="ml-1.5 h-3.5 w-3.5" />
                </Link>
              </Button>
            }
            icon={<CheckCircle2 className="h-4 w-4" />}
          />
        ) : null}
      </RunStageSection>
    </div>
  );
}
