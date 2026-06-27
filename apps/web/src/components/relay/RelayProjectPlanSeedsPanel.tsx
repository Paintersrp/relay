import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Calendar,
  Check,
  Clock3,
  Edit2,
  ExternalLink,
  Loader2,
  Plus,
  XCircle,
} from "lucide-react";

import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { formatPlanDate } from "@/components/relay/relayPlanVisualState";
import { cn } from "@/lib/utils";
import { RelayApiError } from "@/features/relay-runs";
import {
  createPlanSeed,
  deferPlanSeed,
  planSeedsListQueryOptions,
  rejectPlanSeed,
  relayProjectKeys,
  updatePlanSeed,
} from "@/features/relay-projects";
import type {
  PlanSeedAPIRequest,
  PlanSeedStatus,
  PlanSeedUpdateAPIRequest,
  RelayPlanSeed,
} from "@/features/relay-projects/types";

type FilterValue = "all" | PlanSeedStatus;
type FormMode = "create" | "edit";
type LifecycleMode = "defer" | "reject";

interface SeedFormState {
  title: string;
  quickContext: string;
  priority: string;
  constraints: string;
  nonGoals: string;
  tags: string;
  sourceLabel: string;
}

interface ValidationIssue {
  field?: string;
  code?: string;
  message?: string;
}

const FILTERS: Array<{ value: FilterValue; label: string }> = [
  { value: "all", label: "All" },
  { value: "captured", label: "Captured" },
  { value: "planned", label: "Planned" },
  { value: "deferred", label: "Deferred" },
  { value: "rejected", label: "Rejected" },
];

function splitLines(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function joinLines(values: string[] | undefined): string {
  return Array.isArray(values) ? values.join("\n") : "";
}

function formatSeedStatus(status: string): string {
  if (!status) return "Unknown";
  return status
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function planSeedStatusVariant(status: string): BadgeProps["variant"] {
  switch (status) {
    case "captured":
      return "info";
    case "planned":
      return "success";
    case "deferred":
      return "warning";
    case "rejected":
      return "destructive";
    default:
      return "outline";
  }
}

function canEditSeed(seed: RelayPlanSeed): boolean {
  return seed.status === "captured" || seed.status === "deferred";
}

function canDeferSeed(seed: RelayPlanSeed): boolean {
  return seed.status === "captured";
}

function canRejectSeed(seed: RelayPlanSeed): boolean {
  return seed.status === "captured" || seed.status === "deferred";
}

function createEmptyForm(): SeedFormState {
  return {
    title: "",
    quickContext: "",
    priority: "normal",
    constraints: "",
    nonGoals: "",
    tags: "",
    sourceLabel: "Project Details UI",
  };
}

function formFromSeed(seed: RelayPlanSeed): SeedFormState {
  return {
    title: seed.title,
    quickContext: seed.quickContext,
    priority: seed.priority || "normal",
    constraints: joinLines(seed.constraints),
    nonGoals: joinLines(seed.nonGoals),
    tags: joinLines(seed.tags),
    sourceLabel: seed.sourceLabel,
  };
}

function extractValidationIssues(error: unknown): ValidationIssue[] {
  if (!(error instanceof RelayApiError)) return [];
  const validation = error.errorShape?.details?.validation;
  return Array.isArray(validation) ? validation : [];
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message || fallback;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return fallback;
}

function FieldList({ label, values }: { label: string; values: string[] }) {
  if (values.length === 0) return null;

  return (
    <div className="space-y-1">
      <span className="text-[10px] font-semibold uppercase text-muted-foreground">
        {label}
      </span>
      <div className="flex flex-wrap gap-1.5">
        {values.map((value) => (
          <Badge key={value} variant="outline" className="max-w-full break-all rounded px-2 py-0 text-[10px]">
            {value}
          </Badge>
        ))}
      </div>
    </div>
  );
}

function ValidationErrors({
  error,
  fallback,
}: {
  error: unknown;
  fallback: string;
}) {
  if (!error) return null;
  const validationIssues = extractValidationIssues(error);

  return (
    <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
      <div className="flex items-start gap-2">
        <AlertCircle className="mt-0.5 size-4 shrink-0" />
        <div className="min-w-0 space-y-2">
          <p>{errorMessage(error, fallback)}</p>
          {validationIssues.length > 0 && (
            <ul className="space-y-1 text-xs">
              {validationIssues.map((issue, index) => (
                <li key={`${issue.field ?? "field"}-${issue.code ?? "code"}-${index}`}>
                  <span className="font-mono">{issue.field || "request"}</span>
                  {": "}
                  {issue.message || "Invalid value"}
                  {issue.code && (
                    <span className="ml-1 font-mono text-destructive/80">
                      ({issue.code})
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}

export function RelayProjectPlanSeedsPanel({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient();
  const [activeFilter, setActiveFilter] = React.useState<FilterValue>("all");
  const [formOpen, setFormOpen] = React.useState(false);
  const [formMode, setFormMode] = React.useState<FormMode>("create");
  const [editingSeed, setEditingSeed] = React.useState<RelayPlanSeed | null>(null);
  const [formState, setFormState] = React.useState<SeedFormState>(createEmptyForm);
  const [clientError, setClientError] = React.useState<string | null>(null);
  const [lifecycleSeed, setLifecycleSeed] = React.useState<RelayPlanSeed | null>(null);
  const [lifecycleMode, setLifecycleMode] = React.useState<LifecycleMode>("defer");
  const [lifecycleReason, setLifecycleReason] = React.useState("");
  const [lifecycleClientError, setLifecycleClientError] = React.useState<string | null>(null);

  const seedsQuery = useQuery(planSeedsListQueryOptions(projectId, { limit: 100 }));

  const invalidatePlanSeeds = React.useCallback(() => {
    void queryClient.invalidateQueries({
      queryKey: relayProjectKeys.planSeeds(projectId),
    });
  }, [projectId, queryClient]);

  const createMutation = useMutation({
    mutationFn: (request: PlanSeedAPIRequest) => createPlanSeed(projectId, request),
    onSuccess: () => {
      invalidatePlanSeeds();
      setFormOpen(false);
    },
  });

  const updateMutation = useMutation({
    mutationFn: (variables: { seedId: string; request: PlanSeedUpdateAPIRequest }) =>
      updatePlanSeed(projectId, variables.seedId, variables.request),
    onSuccess: () => {
      invalidatePlanSeeds();
      setFormOpen(false);
    },
  });

  const deferMutation = useMutation({
    mutationFn: (variables: { seedId: string; reason: string }) =>
      deferPlanSeed(projectId, variables.seedId, { defer_reason: variables.reason }),
    onSuccess: () => {
      invalidatePlanSeeds();
      setLifecycleSeed(null);
    },
  });

  const rejectMutation = useMutation({
    mutationFn: (variables: { seedId: string; reason: string }) =>
      rejectPlanSeed(projectId, variables.seedId, { reject_reason: variables.reason }),
    onSuccess: () => {
      invalidatePlanSeeds();
      setLifecycleSeed(null);
    },
  });

  const seeds = seedsQuery.data?.seeds ?? [];
  const counts = React.useMemo(
    () =>
      seeds.reduce<Record<FilterValue, number>>(
        (acc, seed) => {
          acc.all += 1;
          if (seed.status === "captured" || seed.status === "planned" || seed.status === "deferred" || seed.status === "rejected") {
            acc[seed.status] += 1;
          }
          return acc;
        },
        { all: 0, captured: 0, planned: 0, deferred: 0, rejected: 0 },
      ),
    [seeds],
  );
  const filteredSeeds = activeFilter === "all"
    ? seeds
    : seeds.filter((seed) => seed.status === activeFilter);

  const formMutation = formMode === "create" ? createMutation : updateMutation;
  const lifecycleMutation = lifecycleMode === "defer" ? deferMutation : rejectMutation;

  const openCreateForm = () => {
    createMutation.reset();
    updateMutation.reset();
    setClientError(null);
    setEditingSeed(null);
    setFormMode("create");
    setFormState(createEmptyForm());
    setFormOpen(true);
  };

  const openEditForm = (seed: RelayPlanSeed) => {
    createMutation.reset();
    updateMutation.reset();
    setClientError(null);
    setEditingSeed(seed);
    setFormMode("edit");
    setFormState(formFromSeed(seed));
    setFormOpen(true);
  };

  const openLifecycleDialog = (mode: LifecycleMode, seed: RelayPlanSeed) => {
    deferMutation.reset();
    rejectMutation.reset();
    setLifecycleClientError(null);
    setLifecycleMode(mode);
    setLifecycleSeed(seed);
    setLifecycleReason("");
  };

  const handleFormSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    createMutation.reset();
    updateMutation.reset();
    setClientError(null);

    const title = formState.title.trim();
    const quickContext = formState.quickContext.trim();
    if (!title || !quickContext) {
      setClientError("Title and quick context are required.");
      return;
    }

    const baseRequest = {
      title,
      quick_context: quickContext,
      priority: formState.priority.trim() || "normal",
      constraints: splitLines(formState.constraints),
      non_goals: splitLines(formState.nonGoals),
      tags: splitLines(formState.tags),
    };

    if (formMode === "create") {
      createMutation.mutate({
        ...baseRequest,
        source_label: formState.sourceLabel.trim() || "Project Details UI",
      });
      return;
    }

    if (editingSeed) {
      updateMutation.mutate({
        seedId: editingSeed.seedId,
        request: baseRequest,
      });
    }
  };

  const handleLifecycleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    deferMutation.reset();
    rejectMutation.reset();
    setLifecycleClientError(null);

    const reason = lifecycleReason.trim();
    if (!lifecycleSeed) return;

    lifecycleMutation.mutate({
      seedId: lifecycleSeed.seedId,
      reason,
    });
  };

  return (
    <section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-4 border-b border-[var(--relay-row-border)] pb-4">
        <div className="min-w-0 space-y-1">
          <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
            Project Planning Backlog
          </div>
          <h3 className="text-base font-semibold text-foreground">Plan Seeds</h3>
          <p className="max-w-2xl text-sm text-muted-foreground">
            Capture rough planning ideas before converting them into reviewed managed plans.
          </p>
        </div>
        <Button size="sm" onClick={openCreateForm} className="gap-1">
          <Plus className="size-3.5" />
          New Seed
        </Button>
      </div>

      <div className="space-y-4 pt-4">
        <Tabs value={activeFilter} onValueChange={(value) => setActiveFilter(value as FilterValue)}>
          <TabsList className="flex h-auto w-full flex-wrap justify-start gap-1 bg-muted/70 p-1 sm:w-fit">
            {FILTERS.map((filter) => (
              <TabsTrigger
                key={filter.value}
                value={filter.value}
                className="h-7 flex-none gap-1.5 px-2 text-xs"
              >
                {filter.label}
                <span className="font-mono text-[10px] text-muted-foreground">
                  {counts[filter.value]}
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        {seedsQuery.isLoading && (
          <div className="flex items-center gap-2 rounded border border-dashed border-[var(--relay-row-border)] p-6 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            Loading plan seeds...
          </div>
        )}

        {seedsQuery.isError && (
          <ValidationErrors
            error={seedsQuery.error}
            fallback="Failed to load plan seeds."
          />
        )}

        {!seedsQuery.isLoading && !seedsQuery.isError && seeds.length === 0 && (
          <div className="rounded border border-dashed border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]/40 p-8 text-center">
            <p className="text-sm font-medium text-muted-foreground">No plan seeds captured yet.</p>
            <p className="mt-1 text-xs text-muted-foreground/75">
              Create a seed to park project planning work without starting a plan attempt.
            </p>
            <Button size="sm" variant="outline" className="mt-4 gap-1" onClick={openCreateForm}>
              <Plus className="size-3.5" />
              New Seed
            </Button>
          </div>
        )}

        {!seedsQuery.isLoading && !seedsQuery.isError && seeds.length > 0 && filteredSeeds.length === 0 && (
          <div className="rounded border border-dashed border-[var(--relay-row-border)] p-6 text-sm text-muted-foreground">
            No {formatSeedStatus(activeFilter).toLowerCase()} plan seeds match this filter.
          </div>
        )}

        {filteredSeeds.length > 0 && (
          <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
            {filteredSeeds.map((seed) => (
              <div
                key={seed.seedId}
                className="rounded border border-[var(--relay-row-border)] bg-background/35 p-4"
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h4 className="break-words text-sm font-semibold text-foreground">
                        {seed.title || "Untitled seed"}
                      </h4>
                      <Badge variant={planSeedStatusVariant(seed.status)}>
                        {formatSeedStatus(seed.status)}
                      </Badge>
                      <Badge variant="outline" className="rounded px-2 py-0 text-[10px]">
                        {seed.priority || "normal"}
                      </Badge>
                    </div>
                    <p className="font-mono text-[10px] text-muted-foreground break-all">
                      ID: {seed.seedId}
                    </p>
                  </div>

                  <div className="flex flex-wrap justify-end gap-1.5">
                    {canEditSeed(seed) && (
                      <Button
                        variant="ghost"
                        size="xs"
                        className="gap-1"
                        onClick={() => openEditForm(seed)}
                        disabled={formMutation.isPending || lifecycleMutation.isPending}
                      >
                        <Edit2 className="size-3" />
                        Edit
                      </Button>
                    )}
                    {canDeferSeed(seed) && (
                      <Button
                        variant="ghost"
                        size="xs"
                        className="gap-1 text-warning"
                        onClick={() => openLifecycleDialog("defer", seed)}
                        disabled={formMutation.isPending || lifecycleMutation.isPending}
                      >
                        <Clock3 className="size-3" />
                        Defer
                      </Button>
                    )}
                    {canRejectSeed(seed) && (
                      <Button
                        variant="ghost"
                        size="xs"
                        className="gap-1 text-destructive"
                        onClick={() => openLifecycleDialog("reject", seed)}
                        disabled={formMutation.isPending || lifecycleMutation.isPending}
                      >
                        <XCircle className="size-3" />
                        Reject
                      </Button>
                    )}
                  </div>
                </div>

                {seed.quickContext && (
                  <p className="mt-3 whitespace-pre-wrap text-sm leading-relaxed text-foreground/85">
                    {seed.quickContext}
                  </p>
                )}

                <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <FieldList label="Tags" values={seed.tags} />
                  <FieldList label="Constraints" values={seed.constraints} />
                  <FieldList label="Non-goals" values={seed.nonGoals} />
                </div>

                <div className="mt-4 grid grid-cols-1 gap-2 border-t border-[var(--relay-row-border)] pt-3 text-xs text-muted-foreground sm:grid-cols-2">
                  <div>
                    <span className="font-semibold text-foreground/75">Source: </span>
                    {seed.sourceType || "manual"}
                    {seed.sourceLabel ? ` - ${seed.sourceLabel}` : ""}
                    {seed.sourceRefId ? ` (${seed.sourceRefId})` : ""}
                  </div>
                  {seed.planAttemptId && (
                    <div className="font-mono break-all">
                      Plan attempt: {seed.planAttemptId}
                    </div>
                  )}
                  {seed.managedPlanId && (
                    <div className="flex min-w-0 items-center gap-1">
                      <span className="shrink-0">Managed plan:</span>
                      <Link
                        to="/plans/$planId"
                        params={{ planId: seed.managedPlanId }}
                        className="inline-flex min-w-0 items-center gap-1 text-primary hover:underline"
                      >
                        <span className="truncate font-mono">{seed.managedPlanId}</span>
                        <ExternalLink className="size-3 shrink-0" />
                      </Link>
                    </div>
                  )}
                  {seed.deferReason && (
                    <div className="sm:col-span-2">
                      <span className="font-semibold text-foreground/75">Defer reason: </span>
                      {seed.deferReason}
                    </div>
                  )}
                  {seed.rejectReason && (
                    <div className="sm:col-span-2">
                      <span className="font-semibold text-foreground/75">Reject reason: </span>
                      {seed.rejectReason}
                    </div>
                  )}
                  <div className="flex items-center gap-1">
                    <Calendar className="size-3" />
                    Created: {formatPlanDate(seed.createdAt)}
                  </div>
                  <div>Updated: {formatPlanDate(seed.updatedAt)}</div>
                  {seed.plannedAt && <div>Planned: {formatPlanDate(seed.plannedAt)}</div>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-2xl">
          <form onSubmit={handleFormSubmit} className="space-y-4">
            <DialogHeader>
              <DialogTitle>{formMode === "create" ? "New Plan Seed" : "Edit Plan Seed"}</DialogTitle>
              <DialogDescription>
                Capture rough planning work as structured project backlog material.
              </DialogDescription>
            </DialogHeader>

            {clientError && (
              <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                {clientError}
              </div>
            )}
            <ValidationErrors
              error={formMutation.error}
              fallback={formMode === "create" ? "Failed to create seed." : "Failed to save seed."}
            />

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5 sm:col-span-2">
                <Label htmlFor="plan-seed-title">Title</Label>
                <Input
                  id="plan-seed-title"
                  value={formState.title}
                  onChange={(event) => setFormState((state) => ({ ...state, title: event.target.value }))}
                  disabled={formMutation.isPending}
                />
              </div>

              <div className="space-y-1.5 sm:col-span-2">
                <Label htmlFor="plan-seed-context">Quick context</Label>
                <Textarea
                  id="plan-seed-context"
                  className="min-h-24"
                  value={formState.quickContext}
                  onChange={(event) => setFormState((state) => ({ ...state, quickContext: event.target.value }))}
                  disabled={formMutation.isPending}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="plan-seed-priority">Priority</Label>
                <Input
                  id="plan-seed-priority"
                  value={formState.priority}
                  onChange={(event) => setFormState((state) => ({ ...state, priority: event.target.value }))}
                  disabled={formMutation.isPending}
                />
              </div>

              {formMode === "create" && (
                <div className="space-y-1.5">
                  <Label htmlFor="plan-seed-source-label">Source label</Label>
                  <Input
                    id="plan-seed-source-label"
                    value={formState.sourceLabel}
                    onChange={(event) => setFormState((state) => ({ ...state, sourceLabel: event.target.value }))}
                    disabled={formMutation.isPending}
                  />
                </div>
              )}

              <div className="space-y-1.5">
                <Label htmlFor="plan-seed-constraints">Constraints</Label>
                <Textarea
                  id="plan-seed-constraints"
                  value={formState.constraints}
                  onChange={(event) => setFormState((state) => ({ ...state, constraints: event.target.value }))}
                  disabled={formMutation.isPending}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="plan-seed-non-goals">Non-goals</Label>
                <Textarea
                  id="plan-seed-non-goals"
                  value={formState.nonGoals}
                  onChange={(event) => setFormState((state) => ({ ...state, nonGoals: event.target.value }))}
                  disabled={formMutation.isPending}
                />
              </div>

              <div className="space-y-1.5 sm:col-span-2">
                <Label htmlFor="plan-seed-tags">Tags</Label>
                <Textarea
                  id="plan-seed-tags"
                  value={formState.tags}
                  onChange={(event) => setFormState((state) => ({ ...state, tags: event.target.value }))}
                  disabled={formMutation.isPending}
                />
              </div>
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setFormOpen(false)} disabled={formMutation.isPending}>
                Cancel
              </Button>
              <Button type="submit" disabled={formMutation.isPending} className="gap-1">
                {formMutation.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Check className="size-3.5" />
                )}
                {formMode === "create" ? "Create Seed" : "Save Seed"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!lifecycleSeed} onOpenChange={(open) => !open && setLifecycleSeed(null)}>
        <DialogContent>
          <form onSubmit={handleLifecycleSubmit} className="space-y-4">
            <DialogHeader>
              <DialogTitle>
                {lifecycleMode === "defer" ? "Defer Plan Seed" : "Reject Plan Seed"}
              </DialogTitle>
              <DialogDescription>
                {lifecycleSeed?.title}
              </DialogDescription>
            </DialogHeader>

            {lifecycleClientError && (
              <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
                {lifecycleClientError}
              </div>
            )}
            <ValidationErrors
              error={lifecycleMutation.error}
              fallback={lifecycleMode === "defer" ? "Failed to defer seed." : "Failed to reject seed."}
            />

            <div className="space-y-1.5">
              <Label htmlFor="plan-seed-lifecycle-reason">Reason (optional)</Label>
              <Textarea
                id="plan-seed-lifecycle-reason"
                className="min-h-24"
                value={lifecycleReason}
                onChange={(event) => setLifecycleReason(event.target.value)}
                disabled={lifecycleMutation.isPending}
              />
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setLifecycleSeed(null)}
                disabled={lifecycleMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant={lifecycleMode === "reject" ? "destructive" : "default"}
                disabled={lifecycleMutation.isPending}
                className={cn("gap-1", lifecycleMode === "defer" && "bg-warning text-warning-foreground hover:bg-warning/90")}
              >
                {lifecycleMutation.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : lifecycleMode === "defer" ? (
                  <Clock3 className="size-3.5" />
                ) : (
                  <XCircle className="size-3.5" />
                )}
                {lifecycleMode === "defer" ? "Defer" : "Reject"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}
