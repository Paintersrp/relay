import { useState, type ChangeEvent, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { ArrowLeft, AlertTriangle, CheckCircle2, Circle, Loader2, Upload } from "lucide-react";
import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RelayStateBanner } from "@/components/relay/RelayStateSurface";
import { relayPlanKeys } from "@/features/relay-plans";
import { submitPlannerHandoff, RelayApiError } from "@/features/relay-runs";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/runs/new")({
  validateSearch: (search) => ({
    planId: typeof search.planId === "string" ? search.planId : undefined,
    passId: typeof search.passId === "string" ? search.passId : undefined,
  }),
  component: NewRunPage,
});

interface DetectedHandoffMetadata {
  source?: string;
  repoTarget?: string;
  branchContext?: string;
  taskSlug?: string;
  targetExecutor?: string;
  schemaVersion?: string;
  recommendedModel?: string;
  executorModelProfile?: string;
  model?: string;
  detectedCount: number;
}

function findMetadataValue(markdown: string, keys: string[]): string | undefined {
  for (const key of keys) {
    const pattern = new RegExp(`^\\s*${key}\\s*:\\s*['"]?([^'"\\n]+)['"]?\\s*$`, "im");
    const match = markdown.match(pattern);
    if (match?.[1]) return match[1].trim();
  }
  return undefined;
}

function detectHandoffMetadata(markdown: string): DetectedHandoffMetadata {
  const metadata: Omit<DetectedHandoffMetadata, "detectedCount"> = {
    source: findMetadataValue(markdown, ["source"]),
    repoTarget: findMetadataValue(markdown, ["repo_target", "repository", "repo"]),
    branchContext: findMetadataValue(markdown, ["branch_context", "branch"]),
    taskSlug: findMetadataValue(markdown, ["task_slug", "task"]),
    targetExecutor: findMetadataValue(markdown, ["target_executor", "executor"]),
    schemaVersion: findMetadataValue(markdown, ["schema_version"]),
    recommendedModel: findMetadataValue(markdown, ["recommended_model"]),
    executorModelProfile: findMetadataValue(markdown, ["executor_model_profile"]),
    model: findMetadataValue(markdown, ["model"]),
  };

  const detectedCount = Object.values(metadata).filter(Boolean).length;
  return { ...metadata, detectedCount };
}

function IntakeStep({
  label,
  state,
}: {
  label: string;
  state: "idle" | "active" | "complete" | "ready";
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 font-mono text-xs",
        state === "active" && "text-foreground",
        state === "complete" && "text-[var(--success)]",
        state === "ready" && "text-muted-foreground",
        state === "idle" && "text-muted-foreground/50",
      )}
    >
      {state === "complete" ? (
        <CheckCircle2 className="h-3 w-3" />
      ) : (
        <Circle className="h-3 w-3" />
      )}
      {label}
    </span>
  );
}

function MetadataRow({
  label,
  value,
}: {
  label: string;
  value?: string;
}) {
  const detected = Boolean(value);

  return (
    <div className="flex items-start justify-between gap-3 border-b border-[var(--relay-row-border)] py-3 last:border-b-0">
      <span className="shrink-0 text-[11px] uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </span>
      <div
        className={cn(
          "flex min-w-0 items-start gap-2 text-right font-mono text-[11px]",
          detected ? "text-[var(--success)]" : "text-[var(--warning)]",
        )}
      >
        {detected ? (
          <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        ) : (
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        )}
        <span className="break-words">{value ?? "not detected"}</span>
      </div>
    </div>
  );
}

function NewRunPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { planId, passId } = Route.useSearch();
  const [markdown, setMarkdown] = useState("");
  const [repo, setRepo] = useState("");
  const [branch, setBranch] = useState("");
  const [name, setName] = useState("");
  const [source, setSource] = useState("react_workbench");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const detectedMetadata = detectHandoffMetadata(markdown);
  const hasHandoff = markdown.trim().length > 0;
  const hasInvalidAssociation = Boolean(passId && !planId);
  const isFormValid = hasHandoff && !hasInvalidAssociation;

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (event) => {
      const text = event.target?.result;
      if (typeof text === "string") {
        setMarkdown(text);
        setErrorMsg(null);
      }
    };
    reader.onerror = () => {
      setErrorMsg("Failed to read the handoff file.");
    };
    reader.readAsText(file);
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (hasInvalidAssociation) {
      setErrorMsg("Invalid managed-plan association: passId requires planId.");
      return;
    }

    if (!markdown.trim()) {
      setErrorMsg("Planner handoff markdown is required.");
      return;
    }

    setIsSubmitting(true);
    setErrorMsg(null);

    try {
      const associationPayload = planId
        ? {
            planId,
            plan_id: planId,
            ...(passId ? { passId, pass_id: passId } : {}),
          }
        : {};
      const response = await submitPlannerHandoff({
        planner_handoff_markdown: markdown,
        repo_target: repo.trim() || undefined,
        branch_context: branch.trim() || undefined,
        name: name.trim() || undefined,
        source: source.trim() || undefined,
        ...associationPayload,
      });

      if (planId) {
        await queryClient.invalidateQueries({ queryKey: relayPlanKeys.detail(planId) });
        if (passId) {
          await queryClient.invalidateQueries({
            queryKey: relayPlanKeys.pass(planId, passId),
          });
        }
      }

      if (response.review_url) {
        window.location.href = response.review_url;
      } else {
        void router.navigate({
          to: "/runs/$runId/intake",
          params: { runId: response.runId || response.run_id || "" },
        });
      }
    } catch (err: unknown) {
      if (err instanceof RelayApiError) {
        setErrorMsg(err.errorShape?.message || err.message);
      } else if (err instanceof Error) {
        setErrorMsg(err.message || "An unexpected error occurred during submission.");
      } else {
        setErrorMsg("An unexpected error occurred during submission.");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
    >
      <AppPageFrame
        title="New Run"
        description="Submit a Planner handoff to create a Relay run."
        headerClassName="px-4 py-3"
        leading={
          <Button
            variant="ghost"
            size="sm"
            asChild
            className="-ml-1 h-6 gap-1.5 px-1 text-xs"
          >
            <Link to="/runs">
              <ArrowLeft className="h-3.5 w-3.5" />
              Back to Runs
            </Link>
          </Button>
        }
        bodyClassName="flex min-h-0 flex-col overflow-hidden p-0"
      >
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div className="shrink-0 border-b border-[var(--relay-row-border)] px-4 py-2">
            <div className="overflow-x-auto">
              <div className="flex min-w-max items-center gap-2 lg:justify-end">
                <IntakeStep
                  label="1 Handoff Intake"
                  state={hasHandoff ? "complete" : "active"}
                />
                <span className="font-mono text-xs text-muted-foreground/30">→</span>
                <IntakeStep
                  label="2 Validate Metadata"
                  state={hasHandoff && !isSubmitting ? "active" : "idle"}
                />
                <span className="font-mono text-xs text-muted-foreground/30">→</span>
                <IntakeStep
                  label="3 Create Run"
                  state={isFormValid ? "ready" : "idle"}
                />
              </div>
            </div>
          </div>

          <div className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden lg:grid-cols-[minmax(0,1fr)_22rem] xl:grid-cols-[minmax(0,1fr)_24rem]">
            <section className="flex min-h-0 flex-1 flex-col border-b border-[var(--relay-row-border)] lg:border-r lg:border-b-0">
              <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
                <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto">
                  {errorMsg && (
                    <div className="shrink-0 px-4 pt-4">
                      <RelayStateBanner
                        tone="danger"
                        title="Handoff submission failed"
                        description={errorMsg}
                        density="compact"
                      />
                    </div>
                  )}

                  {hasInvalidAssociation ? (
                    <div className="shrink-0 px-4 pt-4">
                      <RelayStateBanner
                        tone="danger"
                        title="Invalid managed-plan association"
                        description="Invalid managed-plan association: passId requires planId."
                        density="compact"
                      />
                    </div>
                  ) : null}

                  {planId && !hasInvalidAssociation ? (
                    <div className="shrink-0 px-4 pt-4">
                      <RelayStateBanner
                        tone="info"
                        title="Managed Plan Association"
                        description="This run will be associated with the selected managed plan pass after Relay accepts the handoff."
                        metadata={
                          <span className="flex flex-wrap gap-x-3 gap-y-1">
                            <span>planId: {planId}</span>
                            {passId ? <span>passId: {passId}</span> : null}
                          </span>
                        }
                        density="compact"
                      />
                    </div>
                  ) : null}

                  <div className="flex min-h-0 flex-1 flex-col px-4 pt-3 pb-4">
                    <div>
                      <h2 className="text-base font-semibold text-foreground">
                        Paste or upload Planner handoff
                      </h2>
                      <p className="mt-1 text-sm text-muted-foreground">
                        Accepts Markdown, YAML, JSON, or structured text. Relay will derive a packet after metadata is reviewed.
                      </p>
                    </div>

                    <div className="mt-3 flex min-h-0 flex-1 flex-col gap-4">
                      <div className="flex min-h-0 flex-1 flex-col gap-2">
                        <Label
                          htmlFor="handoff-paste"
                          className="text-[11px] uppercase tracking-[0.12em] text-muted-foreground"
                        >
                          Planner handoff Markdown
                        </Label>
                        <div className="relative min-h-0 flex-1">
                          <Textarea
                            id="handoff-paste"
                            placeholder="Paste Planner handoff Markdown here..."
                            className="h-full min-h-[260px] flex-1 resize-none border-[var(--relay-row-border)] bg-background/70 font-mono text-xs sm:min-h-[340px] lg:min-h-[420px]"
                            value={markdown}
                            onChange={(e) => setMarkdown(e.target.value)}
                            aria-label="Planner handoff paste input"
                          />
                          {!hasHandoff ? (
                            <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                              <div className="flex flex-col items-center gap-2 text-muted-foreground/35">
                                <Upload className="h-7 w-7" />
                                <span className="font-mono text-[11px]">or drop a file</span>
                              </div>
                            </div>
                          ) : null}
                        </div>
                      </div>

                      <div className="flex flex-wrap items-center gap-3">
                        <Label
                          htmlFor="handoff-file"
                          className="shrink-0 cursor-pointer rounded border border-[var(--relay-row-border)] bg-background/60 px-3 py-1.5 font-mono text-[11px] text-foreground hover:bg-[var(--relay-panel-hover-bg)]"
                        >
                          Upload file
                        </Label>
                        <Input
                          id="handoff-file"
                          type="file"
                          accept=".md,.txt,.json"
                          className="sr-only"
                          onChange={handleFileChange}
                          aria-label="Planner handoff file upload"
                        />
                        <p className="font-mono text-[11px] text-muted-foreground">
                          Accepts .md, .txt, or .json
                        </p>
                      </div>
                    </div>
                  </div>

                  <details className="shrink-0 border-t border-[var(--relay-row-border)] px-4 py-3">
                    <summary className="cursor-pointer select-none text-sm font-semibold text-foreground">
                      Review / override
                      <span className="ml-2 font-normal text-xs text-muted-foreground">
                        Optional fields override values Relay may derive from the handoff.
                      </span>
                    </summary>
                    <div className="mt-4 grid gap-3 md:grid-cols-2">
                      <div className="flex flex-col gap-1.5">
                        <Label htmlFor="name-input" className="text-xs">
                          Run Label
                        </Label>
                        <Input
                          id="name-input"
                          placeholder="Optional run label"
                          value={name}
                          onChange={(e) => setName(e.target.value)}
                          className="border-[var(--relay-row-border)] bg-background/70 text-xs"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <Label htmlFor="repo-input" className="text-xs">
                          Repository Override
                        </Label>
                        <Input
                          id="repo-input"
                          placeholder="owner/repo"
                          value={repo}
                          onChange={(e) => setRepo(e.target.value)}
                          className="border-[var(--relay-row-border)] bg-background/70 text-xs"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <Label htmlFor="branch-input" className="text-xs">
                          Branch Override
                        </Label>
                        <Input
                          id="branch-input"
                          placeholder="feature/my-branch"
                          value={branch}
                          onChange={(e) => setBranch(e.target.value)}
                          className="border-[var(--relay-row-border)] bg-background/70 text-xs"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <Label htmlFor="source-input" className="text-xs">
                          Source
                        </Label>
                        <Input
                          id="source-input"
                          placeholder="react_workbench"
                          value={source}
                          onChange={(e) => setSource(e.target.value)}
                          className="border-[var(--relay-row-border)] bg-background/70 text-xs"
                        />
                      </div>
                    </div>
                  </details>
                </div>
              </div>

              <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-[var(--relay-row-border)] px-4 py-3 sm:min-h-12 sm:flex-nowrap sm:py-0">
                <p className="min-w-0 text-xs text-muted-foreground">
                  Create a run from this Planner handoff.
                </p>
                <div className="flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto sm:flex-nowrap">
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
                        <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                        Creating run...
                      </>
                    ) : (
                      "Create Run"
                    )}
                  </Button>
                </div>
              </div>
            </section>

            <aside className="flex min-h-0 flex-col bg-[var(--relay-panel-bg)]">
              <div className="border-b border-[var(--relay-row-border)] px-5 py-3">
                <div className="flex items-center justify-between gap-3">
                  <h2 className="min-w-0 text-sm font-semibold text-foreground">
                    Detected Handoff Metadata
                  </h2>
                  <span className="shrink-0 rounded-full border border-[var(--relay-row-border)] px-2 py-1 font-mono text-[11px] text-muted-foreground">
                    {detectedMetadata.detectedCount}/9 detected
                  </span>
                </div>
              </div>

              <div className="flex min-h-0 flex-1 flex-col">
                {!hasHandoff ? (
                  <>
                    <div className="flex flex-1 items-start justify-center px-5 pt-6 lg:pt-[24vh]">
                      <RelayStateBanner
                        tone="empty"
                        title="Awaiting Planner handoff"
                        description="Paste or upload a Planner handoff to review derived fields before creating the run."
                        density="compact"
                        className="w-full max-w-sm"
                      />
                    </div>
                    <div className="border-t border-[var(--relay-row-border)] px-5 py-3 text-xs text-muted-foreground">
                      Metadata extraction pending
                    </div>
                  </>
                ) : (
                  <>
                    <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
                      {hasHandoff && detectedMetadata.detectedCount === 0 ? (
                        <RelayStateBanner
                          tone="warning"
                          title="Metadata incomplete"
                          description="Relay did not detect structured handoff metadata. Submission can continue, but defaults or explicit overrides may be used."
                          className="mb-4"
                          density="compact"
                        />
                      ) : null}
                      <MetadataRow label="source" value={detectedMetadata.source} />
                      <MetadataRow
                        label="repo_target"
                        value={detectedMetadata.repoTarget}
                      />
                      <MetadataRow
                        label="branch_context"
                        value={detectedMetadata.branchContext}
                      />
                      <MetadataRow label="task_slug" value={detectedMetadata.taskSlug} />
                      <MetadataRow
                        label="target_executor"
                        value={detectedMetadata.targetExecutor}
                      />
                      <MetadataRow
                        label="schema_version"
                        value={detectedMetadata.schemaVersion}
                      />
                      <MetadataRow
                        label="recommended_model"
                        value={detectedMetadata.recommendedModel}
                      />
                      <MetadataRow
                        label="executor_model_profile"
                        value={detectedMetadata.executorModelProfile}
                      />
                      <MetadataRow label="model" value={detectedMetadata.model} />
                    </div>
                    <div className="border-t border-[var(--relay-row-border)] px-5 py-3 text-xs text-muted-foreground">
                      Review detected fields before creating the run.
                    </div>
                  </>
                )}
              </div>
            </aside>
          </div>
        </div>
      </AppPageFrame>
    </form>
  );
}
