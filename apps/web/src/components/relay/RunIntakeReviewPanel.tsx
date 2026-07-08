import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  ExternalLink,
  ListChecks,
  ShieldCheck,
  ShieldX,
} from "lucide-react";

import type { RelayArtifact, RelayRun } from "@/features/relay-runs";
import {
  approveIntake,
  EXECUTOR_ADAPTER_OPTIONS,
  getModelOptionsForAdapter,
} from "@/features/relay-runs";
import { RelayStateBanner } from "@/components/relay/RelayStateSurface";
import {
  RunStageInspectorSection,
  RunStageKeyValueRow,
  RunStagePipeline,
  RunStageStateCard,
  RunStageSummaryCard,
  RunStageSummaryChip,
  RunStageContentSection,
  RunStageEvidenceRow,
  RunStageEvidenceList,
  RunStageFindingRow,
  RunStageFindingList,
  RunStageActivityRow,
  RunStageActivityList,
  RunStageMainStack,
} from "@/components/relay/RunStagePrimitives";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  getIntakeDisplayState,
  getIntakePipelineStatuses,
  getIntakeStateCardCopy,
  INTAKE_PIPELINE_STEPS,
} from "./runIntakeVisualState";

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

type SelectOption = {
  value: string;
  label: string;
};

function findArtifact(
  artifacts: RelayArtifact[],
  predicate: (artifact: RelayArtifact) => boolean,
) {
  return artifacts.find(predicate);
}

function normalizeOptionValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function pushOption(options: SelectOption[], value: unknown, label?: unknown) {
  const normalizedValue = normalizeOptionValue(value);
  if (!normalizedValue || options.some((option) => option.value === normalizedValue)) {
    return;
  }

  const normalizedLabel = normalizeOptionValue(label);
  options.push({
    value: normalizedValue,
    label: normalizedLabel || normalizedValue,
  });
}

function collectStringOrObjectOptions(
  source: unknown,
  valueKeys: string[] = ["value", "label", "name"],
): SelectOption[] {
  const options: SelectOption[] = [];

  const pushFromEntry = (entry: unknown) => {
    if (typeof entry === "string") {
      pushOption(options, entry);
      return;
    }

    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      return;
    }

    const record = entry as Record<string, unknown>;
    const value =
      valueKeys
        .map((key) => normalizeOptionValue(record[key]))
        .find(Boolean) || "";
    const label = normalizeOptionValue(record.label);
    pushOption(options, value, label || value);
  };

  if (Array.isArray(source)) {
    source.forEach(pushFromEntry);
    return options;
  }

  pushFromEntry(source);
  return options;
}

function collectRepoOptions({
  repo,
  repoTarget,
  run,
  runConfig,
  frontmatterObject,
}: {
  repo: string;
  repoTarget: string;
  run: RelayRun;
  runConfig: Record<string, any>;
  frontmatterObject: Record<string, any> | null;
}) {
  const options: SelectOption[] = [];

  [
    repo,
    repoTarget,
    run.repo,
    runConfig.repo,
    runConfig.repo_target,
    frontmatterObject?.repo,
    frontmatterObject?.repo_target,
  ].forEach((value) => pushOption(options, value));

  [
    runConfig.repositories,
    runConfig.available_repos,
    runConfig.repo_options,
    frontmatterObject?.repositories,
    frontmatterObject?.available_repos,
    frontmatterObject?.repo_options,
  ].forEach((source) => {
    collectStringOrObjectOptions(source, [
      "value",
      "repo",
      "path",
      "name",
      "label",
    ]).forEach((option) => pushOption(options, option.value, option.label));
  });

  return options;
}

function collectBranchOptions({
  branch,
  branchContext,
  selectedRepo,
  run,
  runConfig,
  frontmatterObject,
}: {
  branch: string;
  branchContext: string;
  selectedRepo: string;
  run: RelayRun;
  runConfig: Record<string, any>;
  frontmatterObject: Record<string, any> | null;
}) {
  const options: SelectOption[] = [];
  const branchValueKeys = ["value", "branch", "name", "label"];

  [
    branch,
    branchContext,
    run.branch,
    runConfig.branch,
    runConfig.branch_context,
    frontmatterObject?.branch,
    frontmatterObject?.branch_context,
  ].forEach((value) => pushOption(options, value));

  const addBranchSource = (source: unknown) => {
    if (!source) {
      return;
    }

    if (
      source &&
      typeof source === "object" &&
      !Array.isArray(source) &&
      !branchValueKeys.some((key) => key in (source as Record<string, unknown>))
    ) {
      if (!selectedRepo) {
        return;
      }

      collectStringOrObjectOptions(
        (source as Record<string, unknown>)[selectedRepo],
        branchValueKeys,
      ).forEach((option) => pushOption(options, option.value, option.label));
      return;
    }

    collectStringOrObjectOptions(source, branchValueKeys).forEach((option) =>
      pushOption(options, option.value, option.label),
    );
  };

  [
    runConfig.branches,
    runConfig.available_branches,
    runConfig.branch_options,
    frontmatterObject?.branches,
    frontmatterObject?.available_branches,
    frontmatterObject?.branch_options,
  ].forEach(addBranchSource);

  return options;
}

function collectScopedBranchOptions({
  selectedRepo,
  runConfig,
  frontmatterObject,
}: {
  selectedRepo: string;
  runConfig: Record<string, any>;
  frontmatterObject: Record<string, any> | null;
}) {
  const options: SelectOption[] = [];
  const branchValueKeys = ["value", "branch", "name", "label"];

  const addBranchSource = (source: unknown) => {
    if (!source) {
      return;
    }

    if (
      source &&
      typeof source === "object" &&
      !Array.isArray(source) &&
      !branchValueKeys.some((key) => key in (source as Record<string, unknown>))
    ) {
      if (!selectedRepo) {
        return;
      }

      collectStringOrObjectOptions(
        (source as Record<string, unknown>)[selectedRepo],
        branchValueKeys,
      ).forEach((option) => pushOption(options, option.value, option.label));
      return;
    }

    collectStringOrObjectOptions(source, branchValueKeys).forEach((option) =>
      pushOption(options, option.value, option.label),
    );
  };

  [
    runConfig.branches,
    runConfig.available_branches,
    runConfig.branch_options,
    frontmatterObject?.branches,
    frontmatterObject?.available_branches,
    frontmatterObject?.branch_options,
  ].forEach(addBranchSource);

  return options;
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

  const [model, setModel] = React.useState(run.model || "");
  const [repo, setRepo] = React.useState(run.repo || "");
  const [branch, setBranch] = React.useState(run.branch || "");
  const [executorAdapter, setExecutorAdapter] = React.useState(
    run.executorAdapter || "opencode_go",
  );

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
    if (run.executorAdapter) {
      setExecutorAdapter(run.executorAdapter);
    }
  }, [run.model, run.repo, run.branch, run.executorAdapter]);

  React.useEffect(() => {
    if (runConfigArtifact?.preview) {
      const parsedConfig = parsePreviewObject(runConfigArtifact.preview);
      if (!parsedConfig) {
        return;
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
          executorAdapter:
            executorAdapter !== run.executorAdapter
              ? executorAdapter
              : undefined,
        },
      },
    });
  };

  const handleExecutionProfileChange = (nextProfile: string) => {
    setExecutorAdapter(nextProfile);

    const nextModelOptions = getModelOptionsForAdapter(nextProfile);
    if (!nextModelOptions.some((option) => option.value === model)) {
      setModel(nextModelOptions[0]?.value ?? "");
    }
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
  const targetWorktree =
    typeof runConfig.worktree === "string" && runConfig.worktree
      ? runConfig.worktree
      : run.worktree || "";

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
  const modelSource = run.model ? "current run value" : undefined;
  const executorSource =
    typeof runConfig.executor_adapter === "string" && runConfig.executor_adapter
      ? "run config"
      : run.executorAdapter
        ? "current run value"
        : "default adapter";
  const repoOptions = collectRepoOptions({
    repo,
    repoTarget,
    run,
    runConfig,
    frontmatterObject,
  });
  const branchOptions = collectBranchOptions({
    branch,
    branchContext,
    selectedRepo: repo,
    run,
    runConfig,
    frontmatterObject,
  });
  const scopedBranchOptions = collectScopedBranchOptions({
    selectedRepo: repo,
    runConfig,
    frontmatterObject,
  });
  const currentModelOptions = getModelOptionsForAdapter(executorAdapter, model);

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
  const intakeDisplayState = getIntakeDisplayState(run);
  const intakePipelineStatuses = getIntakePipelineStatuses({
    run,
    repo,
    branch,
    executorAdapter,
    model,
  });
  const intakeStateCardCopy = getIntakeStateCardCopy(intakeDisplayState);

  const previousRepoRef = React.useRef(repo);
  React.useEffect(() => {
    if (previousRepoRef.current === repo) {
      return;
    }

    previousRepoRef.current = repo;
    const resolvedBranchOptions =
      scopedBranchOptions.length > 0 ? scopedBranchOptions : branchOptions;
    if (
      branch &&
      resolvedBranchOptions.some((option) => option.value === branch)
    ) {
      return;
    }

    setBranch(resolvedBranchOptions[0]?.value || "");
  }, [repo, branch, branchOptions, scopedBranchOptions]);

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
    executorAdapter,
    handleExecutionProfileChange,
    handleSubmit,
    repoTarget,
    branchContext,
    configSource,
    createdFrom,
    repoSource,
    branchSource,
    modelSource,
    executorSource,
    repoOptions,
    branchOptions,
    currentModelOptions,
    targetWorktree,
    validationSummary,
    readinessIssues,
    summaryStatusTone,
    preflightChecks,
    preflightPassedCount,
    preflightSummary,
    isApproved,
    intakeDisplayState,
    intakePipelineStatuses,
    intakeStateCardCopy,
    artifacts: artifacts || [],
    latestEvents: run.latestEvents || [],
  };
}

export function RunIntakeDetailsPanel({
  controller,
}: {
  controller: RunIntakeReviewController;
}) {
  const {
    run,
    hasFrontmatter,
    repo,
    repoTarget,
    branch,
    branchContext,
    targetWorktree,
    executorAdapter,
    model,
    repoSource,
    branchSource,
    executorSource,
    modelSource,
    preflightSummary,
    validationSummary,
    summaryStatusTone,
  } = controller;

  return (
    <div className="flex flex-col gap-3">
      <RunStageInspectorSection
        title="Handoff"
        contentClassName="flex flex-col gap-2.5"
      >
        <div className="flex flex-wrap gap-2">
          <RunStageSummaryChip
            label="Status"
            value={run.status}
            tone={summaryStatusTone}
            mono
          />
          <RunStageSummaryChip
            label="Frontmatter"
            value={hasFrontmatter ? "Parsed" : "Missing"}
            tone={hasFrontmatter ? "success" : "warning"}
          />
        </div>
        <RunStageKeyValueRow label="Title" value={run.title} />
        <RunStageKeyValueRow
          label="Packet ID"
          value={run.packetId || "—"}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection
        title="Configuration"
        contentClassName="flex flex-col gap-2.5"
      >
        <RunStageKeyValueRow
          label="Repository"
          value={repo || repoTarget || "—"}
          mono
        />
        <RunStageKeyValueRow
          label="Branch"
          value={branch || branchContext || "—"}
          mono
        />
        <RunStageKeyValueRow
          label="Worktree"
          value={targetWorktree || "default"}
          mono
        />
        <RunStageKeyValueRow
          label="Execution Profile"
          value={executorAdapter || "—"}
          mono
        />
        <RunStageKeyValueRow
          label="Target Model"
          value={model || "—"}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection
        title="Provenance"
        contentClassName="flex flex-col gap-2.5"
      >
        <RunStageKeyValueRow
          label="Repository source"
          value={repoSource || "—"}
          mono
        />
        <RunStageKeyValueRow
          label="Branch source"
          value={branchSource || "—"}
          mono
        />
        <RunStageKeyValueRow
          label="Execution profile source"
          value={executorSource || "—"}
          mono
        />
        <RunStageKeyValueRow
          label="Model source"
          value={modelSource || "—"}
          mono
        />
      </RunStageInspectorSection>

      <RunStageInspectorSection
        title="Readiness Summary"
        contentClassName="flex flex-col gap-2.5"
      >
        <div className="flex flex-wrap gap-2">
          <RunStageSummaryChip
            label="Preflight"
            value={preflightSummary}
            tone={
              (validationSummary?.errors ?? 0) === 0 ? "success" : "warning"
            }
          />
          <RunStageSummaryChip
            label="Validation"
            value={`${validationSummary?.errors ?? 0} errors`}
            tone={
              (validationSummary?.errors ?? 0) > 0 ? "warning" : "success"
            }
          />
        </div>
        <RunStageKeyValueRow
          label="Errors"
          value={validationSummary?.errors ?? 0}
          mono
        />
        <RunStageKeyValueRow
          label="Warnings"
          value={validationSummary?.warnings ?? 0}
          mono
        />
        <RunStageKeyValueRow
          label="Passed"
          value={validationSummary?.passed ?? 0}
          mono
        />
      </RunStageInspectorSection>
    </div>
  );
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
        <Link to="/runs/$runId/specification" params={{ runId: run.id }}>
          Proceed to Specification
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
    model,
    setModel,
    repo,
    setRepo,
    branch,
    setBranch,
    executorAdapter,
    handleExecutionProfileChange,
    repoTarget,
    branchContext,
    configSource,
    createdFrom,
    repoSource,
    branchSource,
    modelSource,
    executorSource,
    repoOptions,
    branchOptions,
    currentModelOptions,
    targetWorktree,
    readinessIssues,
    summaryStatusTone,
    preflightChecks,
    preflightPassedCount,
    preflightSummary,
    isApproved,
    intakePipelineStatuses,
    intakeStateCardCopy,
    artifacts,
    latestEvents,
  } = controller;

  return (
    <RunStageMainStack>
      {mutationError ? (
        <RelayStateBanner
          tone="danger"
          title="Intake review failed"
          description={mutationError}
        />
      ) : null}

      <RunStageStateCard
        tone={intakeStateCardCopy.tone}
        eyebrow={intakeStateCardCopy.eyebrow}
        title={intakeStateCardCopy.title}
        message={intakeStateCardCopy.message}
      >
        <div className="flex flex-wrap gap-2">
          <RunStageSummaryChip value={run.status} tone={summaryStatusTone} mono />
          <RunStageSummaryChip
            value={isApproved ? "Ready for Compile / Render" : preflightSummary}
            tone={isApproved ? "success" : summaryStatusTone}
          />
        </div>
      </RunStageStateCard>

      {run.planContext ? (
        <RunStageContentSection
          eyebrow="Plan"
          title="Plan Context"
          description="Managed plan/pass association returned for this run."
          status={run.planContext.passStatus ? <Badge variant="outline">{run.planContext.passStatus}</Badge> : null}
        >
          <div className="grid gap-4 md:grid-cols-2">
            <RunStageKeyValueRow label="Plan Title" value={run.planContext.planTitle || run.planContext.planId || "—"} />
            <RunStageKeyValueRow label="Plan ID" value={run.planContext.planId || "—"} mono />
            <RunStageKeyValueRow label="Pass Name" value={run.planContext.passName || run.planContext.passId || "—"} />
            <RunStageKeyValueRow label="Pass ID" value={run.planContext.passId || "—"} mono />
          </div>
        </RunStageContentSection>
      ) : null}

      <RunStageSummaryCard
        eyebrow="Intake Pipeline"
        title="Review progression"
        description="Handoff, configuration, executor, model, and approval readiness."
        icon={<ListChecks className="h-4 w-4" />}
        status={
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
        }
      >
        <RunStagePipeline
          steps={INTAKE_PIPELINE_STEPS}
          statuses={intakePipelineStatuses}
        />
      </RunStageSummaryCard>

      <RunStageContentSection
        eyebrow="Handoff"
        title="Handoff Summary"
        description="Core details of the submitted run handoff."
      >
        <div className="grid gap-4 md:grid-cols-3">
          <div className="flex flex-col gap-2.5">
            <RunStageKeyValueRow label="Title" value={run.title} />
            <RunStageKeyValueRow label="Packet ID" value={run.packetId || "No packet ID captured"} mono />
            <RunStageKeyValueRow label="Status" value={run.status} mono />
          </div>
          <div className="flex flex-col gap-2.5">
            <RunStageKeyValueRow label="Source" value={configSource} />
            <RunStageKeyValueRow label="Created By" value={createdFrom} />
          </div>
          <div className="flex flex-col gap-2.5">
            <RunStageKeyValueRow label="Target Repo" value={repoTarget ? renderRepoValue(repoTarget) : "—"} />
            <RunStageKeyValueRow label="Target Branch" value={branchContext || "—"} mono />
            <RunStageKeyValueRow label="Worktree" value={targetWorktree || "default"} mono />
          </div>
        </div>
      </RunStageContentSection>

      <RunStageContentSection
        eyebrow="Configuration"
        title="Run Configuration Overrides"
        description="Adjust repository, branch, execution profile, and target model overrides before approval."
      >
        <div className="grid gap-4 md:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-repo" className="text-xs text-muted-foreground">
              Repository Override
            </Label>
            <Select value={repo} onValueChange={setRepo} disabled={isPending || !isReviewable}>
              <SelectTrigger id="override-repo">
                <SelectValue placeholder="Select repository" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {repoOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <InlineHint
              source={repoSource}
              detail={
                repoTarget && repoTarget !== repo ? (
                  <>
                    Resolved intake target: <span className="font-mono text-[11px]">{repoTarget}</span>
                  </>
                ) : undefined
              }
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-branch" className="text-xs text-muted-foreground">
              Branch Override
            </Label>
            <Select value={branch} onValueChange={setBranch} disabled={isPending || !isReviewable}>
              <SelectTrigger id="override-branch">
                <SelectValue placeholder="Select branch" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {branchOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <InlineHint
              source={branchSource}
              detail={
                branchContext && branchContext !== branch ? (
                  <>
                    Resolved intake branch: <span className="font-mono text-[11px]">{branchContext}</span>
                  </>
                ) : undefined
              }
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-executor" className="text-xs text-muted-foreground">
              Execution Profile
            </Label>
            <Select value={executorAdapter} onValueChange={handleExecutionProfileChange} disabled={isPending || !isReviewable}>
              <SelectTrigger id="override-executor">
                <SelectValue placeholder="Select execution profile" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {EXECUTOR_ADAPTER_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <InlineHint source={executorSource} />
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="override-model" className="text-xs text-muted-foreground">
              Target Model
            </Label>
            <Select value={model} onValueChange={setModel} disabled={isPending || !isReviewable}>
              <SelectTrigger id="override-model">
                <SelectValue placeholder="Select target model" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {currentModelOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <InlineHint source={modelSource} />
          </div>
        </div>
      </RunStageContentSection>

      <RunStageContentSection
        eyebrow="Readiness"
        title="Preflight Checks"
        description="Automatic health and access checks executed upon handoff ingestion."
        status={
          <RunStageSummaryChip
            value={preflightSummary}
            tone={
              preflightChecks.length > 0 &&
              preflightPassedCount === preflightChecks.length
                ? "success"
                : "warning"
            }
          />
        }
      >
        <div className="grid gap-2 sm:grid-cols-2">
          {preflightChecks.length > 0 ? (
            preflightChecks.map((check) => (
              <RunStageEvidenceRow
                key={check.label}
                label={check.label}
                status={
                  <RunStageSummaryChip
                    value={check.pass ? "OK" : "Review"}
                    tone={check.pass ? "success" : "warning"}
                  />
                }
              />
            ))
          ) : (
            <p className="text-xs text-muted-foreground italic col-span-2">
              Preflight not available from current intake data.
            </p>
          )}
        </div>
      </RunStageContentSection>

      {readinessIssues.length > 0 ? (
        <RunStageContentSection
          eyebrow="Issues"
          title="Current Validation Issues"
          description="Blocking errors or warning issues discovered during preflight validation."
        >
          <RunStageFindingList>
            {readinessIssues.map((issue, index) => (
              <RunStageFindingRow
                key={`${issue.code}-${index}`}
                severity={issue.severity === "error" ? "error" : "warning"}
                code={issue.code}
                message={issue.message}
              />
            ))}
          </RunStageFindingList>
        </RunStageContentSection>
      ) : null}

      <RunStageContentSection
        eyebrow="Activity"
        title="Recent Activity"
        description="Recent events logged for this run's intake stage."
      >
        {latestEvents && latestEvents.length > 0 ? (
          <RunStageActivityList>
            {latestEvents.slice(-5).map((e: any, i: number) => {
              const timeStr = new Date(e.createdAt).toLocaleTimeString("en-US", {
                hour12: false,
                hour: "2-digit",
                minute: "2-digit",
                second: "2-digit",
              });
              return (
                <RunStageActivityRow
                  key={i}
                  timestamp={timeStr}
                  message={e.message}
                />
              );
            })}
          </RunStageActivityList>
        ) : (
          <p className="text-xs text-muted-foreground italic">No recent activity.</p>
        )}
      </RunStageContentSection>

      <RunStageContentSection
        eyebrow="Artifacts"
        title="Intake Artifacts"
        description="Files generated or submitted during the intake stage."
      >
        {artifacts && artifacts.length > 0 ? (
          <RunStageEvidenceList>
            {artifacts.map((art: any) => (
              <RunStageEvidenceRow
                key={art.id}
                label={art.filename || art.label}
                value={art.sizeHint || ""}
              />
            ))}
          </RunStageEvidenceList>
        ) : (
          <p className="text-xs text-muted-foreground italic">No artifacts found.</p>
        )}
      </RunStageContentSection>
    </RunStageMainStack>
  );
}
