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
  workflowPlanDetailQueryOptions,
  type WorkflowCanonicalValidation,
} from "@/features/relay-plans";
import {
  createWorkflowRun,
  validateWorkflowExecutionSpec,
  workflowRunKeys,
  RelayApiError,
} from "@/features/relay-runs";

interface RelayRunSubmissionWorkbenchProps {
  planId?: string;
  passId?: string;
  passNumber?: number;
  remediatesRunId?: string;
}

interface ValidationSnapshot {
  fileName: string;
  canonicalContent: string;
  validation: WorkflowCanonicalValidation;
}

function errorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  return error instanceof Error ? error.message : "Run creation failed.";
}

export function RelayRunSubmissionWorkbench({
  planId,
  passId,
  passNumber,
  remediatesRunId,
}: RelayRunSubmissionWorkbenchProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const managedRequested =
    planId !== undefined || passId !== undefined || passNumber !== undefined;
  const associationComplete =
    !!planId && !!passId && typeof passNumber === "number" && passNumber > 0;
  const planQuery = useQuery({
    ...workflowPlanDetailQueryOptions(planId ?? ""),
    enabled: associationComplete,
  });
  const selectedPass = planQuery.data?.passes.find(
    (pass) => pass.passId === passId && pass.number === passNumber,
  );
  const managedContextFailed = associationComplete && planQuery.isError;
  const associationValid =
    !managedRequested ||
    (associationComplete &&
      !managedContextFailed &&
      !!planQuery.data &&
      !!selectedPass);

  const [fileName, setFileName] = React.useState("");
  const [canonicalContent, setCanonicalContent] = React.useState("");
  const [snapshot, setSnapshot] = React.useState<ValidationSnapshot | null>(null);
  const [operationError, setOperationError] = React.useState<string | null>(null);

  const validateMutation = useMutation({
    mutationFn: () => validateWorkflowExecutionSpec(fileName, canonicalContent),
    onSuccess: (validation) => {
      setOperationError(null);
      setSnapshot({ fileName, canonicalContent, validation });
    },
    onError: (error) => setOperationError(errorMessage(error)),
  });

  const createMutation = useMutation({
    mutationFn: () => {
      if (!snapshot) throw new Error("Validate the Execution Spec first.");
      return createWorkflowRun({
        fileName: snapshot.fileName,
        canonicalContent: snapshot.canonicalContent,
        expectedSha256: snapshot.validation.sha256,
        ...(associationComplete ? { planId, passNumber } : {}),
        ...(remediatesRunId ? { remediatesRunId } : {}),
      });
    },
    onSuccess: (response) => {
      setOperationError(null);
      void queryClient.invalidateQueries({ queryKey: workflowRunKeys.all });
      void navigate({
        to: "/runs/$runId/specification",
        params: { runId: response.run.runId },
      });
    },
    onError: (error) => setOperationError(errorMessage(error)),
  });

  const currentSnapshot =
    snapshot?.fileName === fileName &&
    snapshot.canonicalContent === canonicalContent
      ? snapshot
      : null;
  const validationReady =
    currentSnapshot?.validation.ok === true &&
    currentSnapshot.validation.kind === "execution_spec";
  const busy = validateMutation.isPending || createMutation.isPending;

  const loadFile = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        setFileName(file.name);
        setCanonicalContent(reader.result);
        setSnapshot(null);
        setOperationError(null);
      }
    };
    reader.onerror = () =>
      setOperationError("The selected Execution Spec file could not be read.");
    reader.readAsText(file);
  };

  return (
    <div
      data-testid="run-submission-layout"
      className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_24rem]"
    >
      <section className="flex min-h-0 flex-col border-b border-[var(--relay-row-border)] lg:border-r lg:border-b-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
          <div>
            <h2 className="text-sm font-semibold">
              Canonical Execution Spec JSON
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Create a {managedRequested ? "Managed" : "Standalone"} Run without
              starting execution.
            </p>
          </div>
          <Label
            htmlFor="execution-spec-file-input"
            className="inline-flex cursor-pointer items-center gap-2 rounded border border-[var(--relay-row-border)] px-3 py-2 text-xs focus-within:ring-2 focus-within:ring-[var(--relay-accent)]"
          >
            <Upload className="size-3.5" />
            Load Execution Spec file
            <input
              id="execution-spec-file-input"
              aria-label="Load Execution Spec file"
              type="file"
              accept=".json,application/json"
              className="sr-only"
              onChange={loadFile}
              disabled={busy}
            />
          </Label>
        </div>
        <div className="space-y-2 border-b border-[var(--relay-row-border)] p-4">
          <Label htmlFor="execution-spec-file-name">Canonical filename</Label>
          <Input
            id="execution-spec-file-name"
            value={fileName}
            onChange={(event) => {
              setFileName(event.target.value);
              setSnapshot(null);
            }}
            placeholder={
              managedRequested
                ? "feature.pass-4.execution-spec.json"
                : "feature.execution-spec.json"
            }
            disabled={busy}
          />
        </div>
        <Textarea
          aria-label="Canonical Execution Spec JSON"
          value={canonicalContent}
          onChange={(event) => {
            setCanonicalContent(event.target.value);
            setSnapshot(null);
          }}
          placeholder="Paste reviewed canonical Execution Spec JSON."
          className="min-h-[28rem] flex-1 resize-none rounded-none border-0 bg-[var(--relay-code-bg)] p-4 font-mono text-xs focus-visible:ring-2"
          disabled={busy}
        />
      </section>

      <aside className="min-h-0 overflow-y-auto bg-[var(--relay-panel-bg)] p-4">
        <div className="space-y-5">
          {operationError ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {operationError}
            </div>
          ) : null}

          {managedRequested && !associationComplete ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              Managed Run creation requires planId, passId, and passNumber
              together.
            </div>
          ) : null}

          {managedContextFailed ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm"
            >
              <p className="font-medium text-destructive">
                Managed Run context failed to load
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Relay could not verify the selected Plan and pass. The request
                remains unresolved until this recoverable context request succeeds.
              </p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="mt-3"
                onClick={() => void planQuery.refetch()}
              >
                <RefreshCw className="size-3.5" />
                Retry Managed context
              </Button>
            </div>
          ) : null}

          {associationComplete &&
          !managedContextFailed &&
          planQuery.data &&
          !selectedPass ? (
            <div
              role="alert"
              className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              The selected pass identifier and number do not match this Plan.
            </div>
          ) : null}

          {associationComplete &&
          !managedContextFailed &&
          planQuery.data &&
          selectedPass ? (
            <div
              data-testid="managed-run-context"
              className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4"
            >
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="info">Managed Run</Badge>
                <Badge
                  variant={
                    planQuery.data.plan.project.status === "archived"
                      ? "secondary"
                      : "success"
                  }
                >
                  Project {planQuery.data.plan.project.status}
                </Badge>
              </div>
              <dl className="mt-3 space-y-2 text-xs">
                <div>
                  <dt className="text-muted-foreground">Project</dt>
                  <dd>{planQuery.data.plan.project.name}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Plan</dt>
                  <dd>{planQuery.data.plan.featureSlug}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Pass</dt>
                  <dd>
                    {selectedPass.number}. {selectedPass.name}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Repository target</dt>
                  <dd className="font-mono">{selectedPass.repoTarget}</dd>
                </div>
              </dl>
            </div>
          ) : !managedRequested ? (
            <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4">
              <Badge variant="outline">Standalone Run</Badge>
              <p className="mt-3 text-xs text-muted-foreground">
                No Plan, pass, or Project association will be stored.
              </p>
            </div>
          ) : null}

          {remediatesRunId ? (
            <div
              data-testid="remediation-run-context"
              className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4 text-xs"
            >
              <span className="text-muted-foreground">Remediates Run</span>
              <p className="mt-1 break-all font-mono">{remediatesRunId}</p>
            </div>
          ) : null}

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
              Validate Execution Spec
            </Button>
            <Button
              type="button"
              disabled={
                busy ||
                managedContextFailed ||
                !associationValid ||
                !validationReady
              }
              onClick={() => createMutation.mutate()}
            >
              {createMutation.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : null}
              Create {managedRequested ? "Managed" : "Standalone"} Run
            </Button>
          </div>
        </div>
      </aside>
    </div>
  );
}
