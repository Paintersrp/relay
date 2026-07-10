import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FileJson, Loader2, RefreshCw, Upload } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  submitWorkflowPlan,
  validateWorkflowPlan,
  workflowPlanKeys,
  type WorkflowCanonicalValidation,
} from "@/features/relay-plans";
import { workflowProjectsListQueryOptions } from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

interface RelayPlanSubmissionWorkbenchProps {
  initialProjectId?: string;
}

interface ValidatedPlanSnapshot {
  fileName: string;
  canonicalContent: string;
  validation: WorkflowCanonicalValidation;
}

function apiErrorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  return error instanceof Error ? error.message : "Plan submission failed.";
}

function compilerEntryText(entry: Record<string, unknown>): string {
  const values = [entry.code, entry.path, entry.message]
    .filter((value): value is string => typeof value === "string" && value.length > 0);
  return values.length > 0 ? values.join(" · ") : JSON.stringify(entry);
}

function CompilerEntryList({
  title,
  entries,
}: {
  title: string;
  entries: Record<string, unknown>[];
}) {
  return (
    <section aria-label={title}>
      <h3 className="text-xs font-semibold">{title}</h3>
      {entries.length > 0 ? (
        <ul className="mt-2 space-y-1.5">
          {entries.map((entry, index) => (
            <li
              key={`${title}-${index}`}
              className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 font-mono text-[10px]"
            >
              {compilerEntryText(entry)}
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-2 text-xs text-muted-foreground">None.</p>
      )}
    </section>
  );
}

export function RelayPlanSubmissionWorkbench({
  initialProjectId = "",
}: RelayPlanSubmissionWorkbenchProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const projectsQuery = useQuery(
    workflowProjectsListQueryOptions({ status: "active", limit: 100 }),
  );
  const [projectId, setProjectId] = React.useState("");
  const [fileName, setFileName] = React.useState("");
  const [canonicalContent, setCanonicalContent] = React.useState("");
  const [snapshot, setSnapshot] = React.useState<ValidatedPlanSnapshot | null>(null);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (projectId || !initialProjectId || !projectsQuery.data) return;
    if (
      projectsQuery.data.projects.some(
        (project) => project.projectId === initialProjectId,
      )
    ) {
      setProjectId(initialProjectId);
    }
  }, [initialProjectId, projectId, projectsQuery.data]);

  const validateMutation = useMutation({
    mutationFn: () => validateWorkflowPlan(fileName, canonicalContent),
    onSuccess: (validation) => {
      setErrorMessage(null);
      setSnapshot({ fileName, canonicalContent, validation });
    },
    onError: (error) => setErrorMessage(apiErrorMessage(error)),
  });

  const submitMutation = useMutation({
    mutationFn: () => {
      if (!snapshot) throw new Error("Validate the Plan before submission.");
      return submitWorkflowPlan({
        projectId,
        fileName: snapshot.fileName,
        canonicalContent: snapshot.canonicalContent,
        expectedSha256: snapshot.validation.sha256,
      });
    },
    onSuccess: (response) => {
      setErrorMessage(null);
      void queryClient.invalidateQueries({ queryKey: workflowPlanKeys.all });
      void navigate({
        to: "/plans/$planId",
        params: { planId: response.plan.planId },
      });
    },
    onError: (error) => setErrorMessage(apiErrorMessage(error)),
  });

  const currentSnapshot =
    snapshot?.fileName === fileName &&
    snapshot.canonicalContent === canonicalContent
      ? snapshot
      : null;
  const validationReady =
    currentSnapshot?.validation.ok === true &&
    currentSnapshot.validation.kind === "plan";
  const projectContextFailed = projectsQuery.isError;
  const busy = validateMutation.isPending || submitMutation.isPending;

  const loadFile = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        setFileName(file.name);
        setCanonicalContent(reader.result);
        setSnapshot(null);
        setErrorMessage(null);
      }
    };
    reader.onerror = () =>
      setErrorMessage("The selected Plan file could not be read.");
    reader.readAsText(file);
  };

  return (
    <div
      data-testid="plan-submission-layout"
      className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_22rem]"
    >
      <section className="flex min-h-0 flex-col border-b border-[var(--relay-row-border)] lg:border-r lg:border-b-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
          <div>
            <h2 className="text-sm font-semibold">Canonical Plan JSON</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Relay validates and submits these exact UTF-8 bytes.
            </p>
          </div>
          <Label
            htmlFor="plan-file-input"
            className="inline-flex cursor-pointer items-center gap-2 rounded border border-[var(--relay-row-border)] px-3 py-2 text-xs focus-within:ring-2 focus-within:ring-[var(--relay-accent)]"
          >
            <Upload className="size-3.5" />
            Load Plan file
            <input
              id="plan-file-input"
              aria-label="Load Plan file"
              type="file"
              accept=".json,application/json"
              className="sr-only"
              onChange={loadFile}
              disabled={busy}
            />
          </Label>
        </div>
        <div className="space-y-2 border-b border-[var(--relay-row-border)] p-4">
          <Label htmlFor="plan-file-name">Canonical filename</Label>
          <Input
            id="plan-file-name"
            value={fileName}
            onChange={(event) => {
              setFileName(event.target.value);
              setSnapshot(null);
            }}
            placeholder="feature.plan.json"
            disabled={busy}
          />
        </div>
        <Textarea
          aria-label="Canonical Plan JSON"
          value={canonicalContent}
          onChange={(event) => {
            setCanonicalContent(event.target.value);
            setSnapshot(null);
          }}
          placeholder="Paste reviewed canonical Plan JSON."
          className="min-h-[28rem] flex-1 resize-none rounded-none border-0 bg-[var(--relay-code-bg)] p-4 font-mono text-xs focus-visible:ring-2"
          disabled={busy}
        />
      </section>

      <aside className="min-h-0 overflow-y-auto bg-[var(--relay-panel-bg)] p-4">
        <div className="space-y-5">
          {projectContextFailed ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm"
            >
              <p className="font-medium text-destructive">
                Active Projects failed to load
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Required Project context failed to load. No empty Project set or
                unavailable submission state is inferred from this transport failure.
              </p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="mt-3"
                onClick={() => void projectsQuery.refetch()}
              >
                <RefreshCw className="size-3.5" />
                Retry Projects
              </Button>
            </div>
          ) : null}

          {errorMessage ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {errorMessage}
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="plan-project">Active Project</Label>
            <select
              id="plan-project"
              value={projectId}
              onChange={(event) => setProjectId(event.target.value)}
              disabled={busy || projectsQuery.isLoading || projectContextFailed}
              className="h-9 w-full rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 text-sm"
            >
              <option value="">
                {projectsQuery.isLoading ? "Loading Projects" : "Select a Project"}
              </option>
              {(projectsQuery.data?.projects ?? []).map((project) => (
                <option key={project.projectId} value={project.projectId}>
                  {project.name}
                </option>
              ))}
            </select>
            <p className="text-[10px] text-muted-foreground">
              Project selection is Relay metadata, not canonical Plan JSON.
            </p>
          </div>

          <div
            role="status"
            aria-live="polite"
            className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4"
          >
            <div className="flex items-center gap-2">
              <FileJson className="size-4 text-muted-foreground" />
              <span className="text-sm font-medium">Validation</span>
              {currentSnapshot ? (
                <Badge
                  variant={
                    currentSnapshot.validation.ok ? "success" : "destructive"
                  }
                >
                  {currentSnapshot.validation.status}
                </Badge>
              ) : (
                <Badge variant="outline">Not validated</Badge>
              )}
            </div>
            {currentSnapshot ? (
              <div
                data-testid="validated-plan-snapshot"
                className="mt-3 space-y-4"
              >
                <p
                  data-testid="validated-plan-sha"
                  className="break-all font-mono text-[10px] text-muted-foreground"
                >
                  {currentSnapshot.validation.sha256}
                </p>
                <section aria-label="Validated Plan preview">
                  <div>
                    <h3 className="text-xs font-semibold">Plan preview</h3>
                    <p className="mt-1 text-[10px] text-muted-foreground">
                      Exact canonical bytes from the current validated filename and
                      editor snapshot.
                    </p>
                  </div>
                  <pre
                    data-testid="validated-plan-preview"
                    className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded border border-[var(--relay-row-border)] bg-[var(--relay-code-bg)] p-3 font-mono text-[10px]"
                  >
                    {currentSnapshot.canonicalContent}
                  </pre>
                </section>
                <CompilerEntryList
                  title="Compiler diagnostics"
                  entries={currentSnapshot.validation.diagnostics}
                />
                <CompilerEntryList
                  title="Compiler notices"
                  entries={currentSnapshot.validation.notices}
                />
              </div>
            ) : (
              <p className="mt-3 text-xs text-muted-foreground">
                Any filename or editor change invalidates the preview, diagnostics,
                notices, hash, and prior validation.
              </p>
            )}
          </div>

          <div className="grid gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={
                busy ||
                fileName.trim().length === 0 ||
                canonicalContent.length === 0
              }
              onClick={() => validateMutation.mutate()}
            >
              {validateMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : null}
              Validate Plan
            </Button>
            <Button
              type="button"
              disabled={
                busy ||
                projectContextFailed ||
                !projectId ||
                !validationReady
              }
              onClick={() => submitMutation.mutate()}
            >
              {submitMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : null}
              Submit Plan
            </Button>
          </div>
        </div>
      </aside>
    </div>
  );
}
