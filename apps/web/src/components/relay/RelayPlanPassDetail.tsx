import * as React from "react";
import { Link } from "@tanstack/react-router";
import {
  ArrowLeft,
  CheckCircle2,
  Copy,
  ExternalLink,
  Play,
} from "lucide-react";

import { PlanPassContextPanel } from "@/components/relay/PlanPassContextPanel";
import { RelayStateBanner } from "@/components/relay/RelayStateSurface";
import {
  buildPassContextText,
  canCreateRunForPass,
  getCreateRunSearch,
  getPassBlockingDependencies,
  getPassDetailState,
  type PassBlockingDependency,
  type RelayPlanPassDetailState,
} from "@/components/relay/relayPlanPassDetailState";
import {
  getPassStatusCounts,
  getPassStatusLabel,
  getPassStatusVariant,
  getPlanStatusLabel,
  getPlanStatusVariant,
  sortPassesBySequence,
} from "@/components/relay/relayPlanVisualState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  getPassNextWorkPreview,
  type NextPassWorkResponse,
  type PlanAPIPass,
  type PlanAPIPlan,
} from "@/features/relay-plans";
import { cn } from "@/lib/utils";

interface RelayPlanPassDetailProps {
  plan: PlanAPIPlan;
  pass: PlanAPIPass;
  passes: PlanAPIPass[];
  completionReady: boolean;
}

type CopyState = "idle" | "copied" | "failed";

function copyText(text: string, onStateChange: (state: CopyState) => void) {
  void (async () => {
    try {
      if (!navigator.clipboard) {
        throw new Error("Clipboard API unavailable");
      }

      await navigator.clipboard.writeText(text);
      onStateChange("copied");
    } catch {
      onStateChange("failed");
    }
  })();
}

function getStateCopy(state: RelayPlanPassDetailState, blocking: PassBlockingDependency[]) {
  switch (state) {
    case "ready_for_planner":
      return {
        eyebrow: "READY FOR PLANNER",
        title: "Ready for planner handoff",
        description: "All dependencies are terminal. Use Continue Plan to request next-pass work.",
        tone: "info" as const,
        accentClassName: "bg-[var(--relay-accent)]",
      };
    case "handoff_ready":
      return {
        eyebrow: "HANDOFF READY",
        title: "Reviewed handoff exists",
        description: "A reviewed Planner handoff is ready for run submission.",
        tone: "success" as const,
        accentClassName: "bg-[var(--success)]",
      };
    case "run_created":
      return {
        eyebrow: "RUN CREATED",
        title: "Pass-associated run exists",
        description: "A run has been created for this pass. View run workbench.",
        tone: "info" as const,
        accentClassName: "bg-[var(--relay-accent)]",
      };
    case "in_progress":
      return {
        eyebrow: "PASS IN PROGRESS",
        title: "Work is underway",
        description: "Relay owns the current run state for this pass.",
        tone: "info" as const,
        accentClassName: "bg-[var(--relay-accent)]",
      };
    case "audit_ready":
      return {
        eyebrow: "AUDIT READY",
        title: "Audit packet/evidence ready",
        description: "Run is ready for audit review. Use Audit Ready to request next-audit work.",
        tone: "warning" as const,
        accentClassName: "bg-[var(--warning)]",
      };
    case "revision_required":
      return {
        eyebrow: "REVISION REQUIRED",
        title: "Same pass needs repair/follow-up",
        description: "This pass must be repaired or followed up before any later pass can proceed.",
        tone: "warning" as const,
        accentClassName: "bg-[var(--warning)]",
      };
    case "blocked":
      return {
        eyebrow: "PASS BLOCKED",
        title: "Pass is blocked",
        description: "Show blocker; no hidden continuation.",
        tone: "blocked" as const,
        accentClassName: "bg-destructive",
      };
    case "completed":
      return {
        eyebrow: "PASS COMPLETE",
        title: "This pass is terminal",
        description: "Run creation is disabled for completed passes.",
        tone: "success" as const,
        accentClassName: "bg-[var(--success)]",
      };
    case "skipped":
      return {
        eyebrow: "PASS SKIPPED",
        title: "This pass was skipped",
        description: "Run creation is disabled for skipped passes.",
        tone: "empty" as const,
        accentClassName: "bg-muted-foreground/45",
      };
    case "dependency_blocked":
      return {
        eyebrow: "DEPENDENCY BLOCKED",
        title: "Waiting on dependency",
        description: `Blocking dependencies: ${blocking.map((item) => item.passId).join(", ")}`,
        tone: "blocked" as const,
        accentClassName: "bg-destructive",
      };
  }
}

function DetailRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-[var(--relay-row-border)] py-2.5 last:border-b-0">
      <span className="shrink-0 text-[11px] text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words text-right font-mono text-[11px] text-foreground">
        {value}
      </span>
    </div>
  );
}

function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="border-b border-[var(--relay-row-border)] px-5 py-2.5">
      <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
        {children}
      </span>
    </div>
  );
}

export function RelayPlanPassDetail({
  plan,
  pass,
  passes,
  completionReady,
}: RelayPlanPassDetailProps) {
  const sortedPasses = React.useMemo(() => sortPassesBySequence(passes), [passes]);
  const passCount = sortedPasses.length;
  const counts = React.useMemo(() => getPassStatusCounts(sortedPasses), [sortedPasses]);
  const passMap = React.useMemo(
    () => new Map(sortedPasses.map((candidate) => [candidate.passId, candidate])),
    [sortedPasses],
  );
  const blockingDependencies = React.useMemo(
    () => getPassBlockingDependencies(pass, sortedPasses),
    [pass, sortedPasses],
  );
  const detailState = React.useMemo(
    () => getPassDetailState(pass, sortedPasses),
    [pass, sortedPasses],
  );
  const stateCopy = getStateCopy(detailState, blockingDependencies);
  const runnable = canCreateRunForPass(pass, sortedPasses);
  const associatedRuns = pass.associatedRuns ?? [];
  const [passIdCopyState, setPassIdCopyState] = React.useState<CopyState>("idle");
  const [planIdCopyState, setPlanIdCopyState] = React.useState<CopyState>("idle");
  const [contextCopyState, setContextCopyState] = React.useState<CopyState>("idle");
  const [previewCopyState, setPreviewCopyState] = React.useState<CopyState>("idle");
  const [previewPayload, setPreviewPayload] = React.useState<NextPassWorkResponse | null>(null);
  const [previewError, setPreviewError] = React.useState<string | null>(null);
  const [previewLoading, setPreviewLoading] = React.useState(false);
  const projectId = plan.projectId?.trim() ?? "";

  const contextText = React.useMemo(
    () => buildPassContextText({ plan, pass, blockingDependencies }),
    [plan, pass, blockingDependencies],
  );
  const previewJSON = React.useMemo(
    () => (previewPayload ? JSON.stringify(previewPayload, null, 2) : ""),
    [previewPayload],
  );

  React.useEffect(() => {
    setPreviewPayload(null);
    setPreviewError(null);
    setPreviewCopyState("idle");
  }, [plan.planId, pass.passId, projectId]);

  const handleGeneratePreview = async () => {
    if (!projectId) {
      setPreviewError("Project ID is required to generate a pass preview.");
      return;
    }

    setPreviewLoading(true);
    setPreviewError(null);
    setPreviewCopyState("idle");
    try {
      const payload = await getPassNextWorkPreview(projectId, plan.planId, pass.passId);
      setPreviewPayload(payload);
    } catch (err: any) {
      setPreviewError(err?.message ?? "Failed to generate Planner Jumpstart preview.");
    } finally {
      setPreviewLoading(false);
    }
  };

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 px-4 py-4 sm:px-6 sm:py-5">
      <section className="border-b border-[var(--relay-row-border)] pb-4">
        <div className="mb-3 flex flex-wrap items-center gap-1.5 text-xs">
          <Link
            to="/plans"
            className="inline-flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="size-3" />
            Plans
          </Link>
          <span className="text-muted-foreground/60">/</span>
          <Link
            to="/plans/$planId"
            params={{ planId: plan.planId }}
            className="max-w-xs truncate text-muted-foreground transition-colors hover:text-foreground"
          >
            {plan.title}
          </Link>
          <span className="text-muted-foreground/60">/</span>
          <span className="max-w-xs truncate text-foreground">{pass.name}</span>
        </div>

        <div className="mb-2.5 flex flex-wrap items-start gap-2.5">
          <h1 className="min-w-0 text-xl font-semibold tracking-tight text-foreground">
            {pass.name}
          </h1>
          <Badge
            variant={getPassStatusVariant(pass.status)}
            className="h-auto rounded-sm px-2 py-0.5 text-[10px] font-medium tracking-wide"
          >
            {getPassStatusLabel(pass.status)}
          </Badge>
        </div>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px]">
          <button
            type="button"
            onClick={() => copyText(pass.passId, setPassIdCopyState)}
            className="group inline-flex items-center gap-1 font-mono text-muted-foreground transition-colors hover:text-foreground"
            title="Copy pass ID"
          >
            <span>{pass.passId}</span>
            <Copy className="size-3 opacity-50 transition-opacity group-hover:opacity-100" />
            {passIdCopyState === "copied" ? (
              <span className="text-[10px] text-[var(--success)]">Copied</span>
            ) : null}
            {passIdCopyState === "failed" ? (
              <span className="text-[10px] text-destructive">Copy failed</span>
            ) : null}
          </button>
          <span className="text-muted-foreground/60">/</span>
          <button
            type="button"
            onClick={() => copyText(plan.planId, setPlanIdCopyState)}
            className="group inline-flex items-center gap-1 font-mono text-muted-foreground transition-colors hover:text-foreground"
            title="Copy plan ID"
          >
            <span>{plan.planId}</span>
            <Copy className="size-3 opacity-50 transition-opacity group-hover:opacity-100" />
            {planIdCopyState === "copied" ? (
              <span className="text-[10px] text-[var(--success)]">Copied</span>
            ) : null}
            {planIdCopyState === "failed" ? (
              <span className="text-[10px] text-destructive">Copy failed</span>
            ) : null}
          </button>
          <span className="text-muted-foreground/60">/</span>
          <span className="font-mono text-muted-foreground">{plan.repoTarget}</span>
          <span className="text-muted-foreground/60">/</span>
          <span className="font-mono text-muted-foreground">{plan.branchContext}</span>
          <span className="text-muted-foreground/60">/</span>
          <span className="text-muted-foreground">
            Pass {pass.sequence} of {passCount}
          </span>
        </div>
      </section>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <main className="flex min-w-0 flex-col gap-4">
          <section className="relative border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-5 py-4">
            <div className={cn("absolute inset-y-0 left-0 w-[2px]", stateCopy.accentClassName)} />
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 flex-1 pl-1">
                <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                  {stateCopy.eyebrow}
                </div>
                <div className="mt-1 text-sm font-medium leading-snug text-foreground">
                  {stateCopy.title}
                </div>
                <div className="mt-1 max-w-xl text-xs leading-relaxed text-muted-foreground">
                  {stateCopy.description}
                </div>
              </div>

              <div className="flex shrink-0 flex-wrap items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  className="rounded-sm px-3 text-[11px]"
                  onClick={() => copyText(contextText, setContextCopyState)}
                >
                  <Copy className="size-3" />
                  {contextCopyState === "copied"
                    ? "Copied"
                    : contextCopyState === "failed"
                      ? "Copy failed"
                      : "Copy context"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  disabled={previewLoading || !projectId}
                  className="rounded-sm px-3 text-[11px]"
                  onClick={handleGeneratePreview}
                >
                  {previewLoading ? "Generating..." : "Generate Planner Jumpstart Preview"}
                </Button>
                {runnable ? (
                  <Button asChild size="xs" className="rounded-sm px-3 text-[11px]">
                    <Link
                      to="/runs/new"
                      search={getCreateRunSearch(plan.planId, pass.passId)}
                    >
                      <Play className="size-3" />
                      Create run for this pass
                    </Link>
                  </Button>
                ) : (
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    disabled
                    className="rounded-sm px-3 text-[11px]"
                  >
                    Create run disabled
                  </Button>
                )}
              </div>
            </div>

            {detailState === "blocked" && blockingDependencies[0] ? (
              <div className="mt-4 pl-1">
                <Button asChild variant="outline" size="xs" className="rounded-sm px-3 text-[11px]">
                  <Link
                    to="/plans/$planId/passes/$passId"
                    params={{
                      planId: plan.planId,
                      passId: blockingDependencies[0].passId,
                    }}
                  >
                    Open blocking pass
                  </Link>
                </Button>
              </div>
            ) : null}
          </section>

          {(previewError || previewPayload) && (
            <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
              <div className="flex flex-col gap-2 border-b border-[var(--relay-row-border)] px-5 py-2.5 sm:flex-row sm:items-center sm:justify-between">
                <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                  Planner Jumpstart Preview
                </span>
                {previewPayload ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    className="w-fit rounded-sm px-3 text-[11px]"
                    onClick={() => copyText(previewJSON, setPreviewCopyState)}
                  >
                    <Copy className="size-3" />
                    {previewCopyState === "copied"
                      ? "Copied"
                      : previewCopyState === "failed"
                        ? "Copy failed"
                        : "Copy JSON"}
                  </Button>
                ) : null}
              </div>
              <div className="px-5 py-4">
                {previewError ? (
                  <RelayStateBanner
                    tone="blocked"
                    density="compact"
                    title="Preview unavailable"
                    description={previewError}
                  />
                ) : null}
                {previewPayload ? (
                  <pre className="max-h-[32rem] overflow-auto rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-3 font-mono text-[11px] leading-relaxed text-foreground">
                    {previewJSON}
                  </pre>
                ) : null}
              </div>
            </section>
          )}

          <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
            <SectionHeader>Parent Plan</SectionHeader>
            <div className="flex flex-col gap-3 px-5 py-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-sm font-semibold text-foreground">{plan.title}</h2>
                  <Badge
                    variant={getPlanStatusVariant(plan.status)}
                    className="h-auto rounded-sm px-2 py-0.5 text-[10px]"
                  >
                    {getPlanStatusLabel(plan.status)}
                  </Badge>
                  {completionReady ? (
                    <Badge variant="warning" className="h-auto rounded-sm px-2 py-0.5 text-[10px]">
                      Completion Ready
                    </Badge>
                  ) : null}
                </div>
                <div className="mt-1 max-w-2xl text-xs leading-relaxed text-muted-foreground">
                  {plan.goal || plan.sourceIntentSummary}
                </div>
                <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 font-mono text-[11px] text-muted-foreground">
                  <span>{plan.planId}</span>
                  <span>{plan.repoTarget}</span>
                  <span>{plan.branchContext}</span>
                  <span>{counts.completed} completed</span>
                  <span>{counts.inProgress} in progress</span>
                  <span>{counts.planned} planned</span>
                  <span>{counts.skipped} skipped</span>
                </div>
              </div>
              <Button asChild variant="outline" size="xs" className="rounded-sm px-3 text-[11px]">
                <Link to="/plans/$planId" params={{ planId: plan.planId }}>
                  Back to plan
                </Link>
              </Button>
            </div>
          </section>

          <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
            <SectionHeader>Pass Execution Contract</SectionHeader>
            <div className="space-y-4 px-5 py-4">
              <div>
                <div className="mb-1 text-[11px] text-muted-foreground">Goal</div>
                <p className="text-sm leading-relaxed text-foreground">{pass.goal}</p>
              </div>
              <div>
                <div className="mb-2 text-[11px] text-muted-foreground">
                  Intended execution scope
                </div>
                {pass.intendedExecutionScope.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5">
                    {pass.intendedExecutionScope.map((scope) => (
                      <span
                        key={scope}
                        className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-2 py-1 font-mono text-[11px] text-foreground"
                      >
                        {scope}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">No execution scope listed.</p>
                )}
              </div>
              <div>
                <div className="mb-2 text-[11px] text-muted-foreground">Non-goals</div>
                {pass.nonGoals.length > 0 ? (
                  <div className="flex flex-wrap gap-1.5">
                    {pass.nonGoals.map((nonGoal) => (
                      <span
                        key={nonGoal}
                        className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-2 py-1 text-xs text-muted-foreground"
                      >
                        {nonGoal}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">No non-goals listed.</p>
                )}
              </div>
              <div>
                <div className="mb-2 text-[11px] text-muted-foreground">
                  Dependency summary
                </div>
                <p className="text-xs text-muted-foreground">
                  {pass.dependencies.length === 0
                    ? "No dependencies."
                    : `${pass.dependencies.length} dependencies, ${blockingDependencies.length} blocking.`}
                </p>
              </div>
            </div>
          </section>

          <PlanPassContextPanel pass={pass} />

          <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
            <SectionHeader>Dependencies</SectionHeader>
            <div className="divide-y divide-[var(--relay-row-border)]">
              {pass.dependencies.length === 0 ? (
                <div className="px-5 py-4 text-xs text-muted-foreground">No dependencies</div>
              ) : (
                pass.dependencies.map((dependencyId) => {
                  const dependency = passMap.get(dependencyId);
                  const isBlocking = blockingDependencies.some(
                    (candidate) => candidate.passId === dependencyId,
                  );

                  return (
                    <div
                      key={dependencyId}
                      className="flex flex-col gap-2 px-5 py-3 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div className="min-w-0">
                        <Link
                          to="/plans/$planId/passes/$passId"
                          params={{ planId: plan.planId, passId: dependencyId }}
                          className="font-mono text-xs text-foreground transition-colors hover:text-[var(--relay-accent)]"
                        >
                          {dependencyId}
                        </Link>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {dependency?.name ?? "Dependency pass is missing from this plan response."}
                        </div>
                      </div>
                      <Badge
                        variant={isBlocking ? "destructive" : "success"}
                        className="h-auto w-fit rounded-sm px-2 py-0.5 text-[10px]"
                      >
                        {isBlocking
                          ? "Blocking"
                          : dependency
                            ? getPassStatusLabel(dependency.status)
                            : "Missing"}
                      </Badge>
                    </div>
                  );
                })
              )}
            </div>
          </section>

          <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
            <SectionHeader>Associated Runs</SectionHeader>
            <div className="px-5 py-4">
              {associatedRuns.length > 0 ? (
                <div className="grid gap-2">
                  {associatedRuns.map((run) => (
                    <a
                      key={run.id}
                      href={run.workbenchPath}
                      className="flex items-center justify-between gap-3 rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2 text-xs text-foreground hover:bg-[var(--relay-panel-hover-bg)]"
                    >
                      <div className="min-w-0">
                        <div className="font-mono">{run.id}</div>
                        <div className="mt-1 text-muted-foreground">
                          {run.title || run.status}
                        </div>
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <span className="text-[11px] text-muted-foreground">
                          {run.status}
                        </span>
                        <ExternalLink className="size-3" />
                      </div>
                    </a>
                  ))}
                </div>
              ) : (
                <RelayStateBanner
                  tone="empty"
                  density="compact"
                  title="No associated runs"
                  description="Relay has not recorded any runs for this pass yet."
                />
              )}
            </div>
          </section>
        </main>

        <aside className="flex min-w-0 flex-col gap-4">
          <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
            <SectionHeader>Details</SectionHeader>
            <div className="px-5 py-2">
              <DetailRow label="Pass ID" value={pass.passId} />
              <DetailRow label="Status" value={getPassStatusLabel(pass.status)} />
              <DetailRow label="Sequence" value={`${pass.sequence} of ${passCount}`} />
              <DetailRow label="Plan ID" value={plan.planId} />
              <DetailRow label="Repo" value={plan.repoTarget} />
              <DetailRow label="Dependencies" value={pass.dependencies.length} />
              <DetailRow label="Blocking" value={blockingDependencies.length} />
              <DetailRow label="Runs" value={associatedRuns.length} />
            </div>
          </section>

          <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
            <SectionHeader>Runs</SectionHeader>
            <div className="space-y-3 px-5 py-4">
              {associatedRuns.length > 0 ? (
                associatedRuns.map((run) => (
                  <a
                    key={run.id}
                    href={run.workbenchPath}
                    className="flex items-center justify-between gap-2 rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2 text-xs text-foreground hover:bg-[var(--relay-panel-hover-bg)]"
                  >
                    <span className="font-mono">{run.id}</span>
                    <ExternalLink className="size-3 text-muted-foreground" />
                  </a>
                ))
              ) : (
                <div className="text-xs leading-relaxed text-muted-foreground">
                  No associated runs are present in the pass payload.
                </div>
              )}
              {runnable ? (
                <Button asChild size="xs" className="w-full rounded-sm px-3 text-[11px]">
                  <Link
                    to="/runs/new"
                    search={getCreateRunSearch(plan.planId, pass.passId)}
                  >
                    <Play className="size-3" />
                    Create run
                  </Link>
                </Button>
              ) : (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <CheckCircle2 className="size-3" />
                  Run creation unavailable for this pass state.
                </div>
              )}
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}
