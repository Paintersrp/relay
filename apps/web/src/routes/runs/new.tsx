import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, createFileRoute, useRouter } from "@tanstack/react-router";
import { useState, type ChangeEvent } from "react";
import { ArrowLeft, Loader2, Upload, AlertCircle, CheckCircle2 } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { WorkflowCanonicalValidation } from "@/features/relay-plans";
import {
  createWorkflowRun,
  validateWorkflowExecutionSpec,
  workflowRunKeys,
} from "@/features/relay-runs";

interface NewRunSearch {
  planId?: string;
  passId?: string;
  passNumber?: number;
  remediatesRunId?: string;
}

export const Route = createFileRoute("/runs/new")({
  validateSearch: (search: Record<string, unknown>): NewRunSearch => {
    const planId = typeof search.planId === "string" ? search.planId.trim() : undefined;
    const passId = typeof search.passId === "string" ? search.passId.trim() : undefined;
    const passNumber =
      typeof search.passNumber === "number"
        ? search.passNumber
        : typeof search.passNumber === "string"
          ? parseInt(search.passNumber, 10) || undefined
          : undefined;
    const remediatesRunId =
      typeof search.remediatesRunId === "string" ? search.remediatesRunId.trim() : undefined;
    return { planId, passId, passNumber, remediatesRunId };
  },
  component: NewRunPage,
});

interface ValidatedSpecSnapshot {
  fileName: string;
  canonicalContent: string;
  validation: WorkflowCanonicalValidation;
}

function NewRunPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { planId, passNumber, remediatesRunId } = Route.useSearch();

  const [fileName, setFileName] = useState("");
  const [canonicalContent, setCanonicalContent] = useState("");
  const [snapshot, setSnapshot] = useState<ValidatedSpecSnapshot | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const validateMutation = useMutation({
    mutationFn: () => validateWorkflowExecutionSpec(fileName, canonicalContent),
    onSuccess: (validation) => {
      setErrorMessage(null);
      setSnapshot({ fileName, canonicalContent, validation });
    },
    onError: (err) => {
      setErrorMessage(err instanceof Error ? err.message : "Validation failed.");
    },
  });

  const submitMutation = useMutation({
    mutationFn: () => {
      if (!snapshot) throw new Error("Validate the spec before submission.");
      return createWorkflowRun({
        fileName: snapshot.fileName,
        canonicalContent: snapshot.canonicalContent,
        expectedSha256: snapshot.validation.sha256,
        planId,
        passNumber,
        remediatesRunId,
      });
    },
    onSuccess: (response) => {
      setErrorMessage(null);
      void queryClient.invalidateQueries({ queryKey: workflowRunKeys.all });
      void router.navigate({
        to: "/runs/$runId",
        params: { runId: response.run.runId },
      });
    },
    onError: (err) => {
      setErrorMessage(err instanceof Error ? err.message : "Run submission failed.");
    },
  });

  const currentSnapshot =
    snapshot?.fileName === fileName &&
    snapshot.canonicalContent === canonicalContent
      ? snapshot
      : null;

  const validationReady =
    currentSnapshot?.validation.ok === true &&
    (currentSnapshot.validation.kind === "execution_spec" ||
      currentSnapshot.validation.kind === "executor_spec");

  const busy = validateMutation.isPending || submitMutation.isPending;

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
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
    reader.onerror = () => {
      setErrorMessage("The selected file could not be read.");
    };
    reader.readAsText(file);
  };

  return (
    <AppPageFrame
      title="New Run"
      description="Create a Standalone or Managed Run from a canonical Execution Spec JSON artifact."
      leading={
        <Button asChild variant="ghost" size="icon-sm" aria-label="Back to Runs">
          <Link to="/runs">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
      }
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-y-auto p-0 bg-[var(--relay-panel-bg)]"
    >
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6 sm:px-6">
        
        {/* Association status banner */}
        {(planId || remediatesRunId) && (
          <div className="flex flex-col gap-2 rounded border border-blue-400/30 bg-blue-50/50 p-4 text-xs text-blue-700 dark:bg-blue-900/10 dark:text-blue-300">
            <h3 className="font-semibold uppercase tracking-wider text-[10px]">Plan / Remediate association</h3>
            <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
              {planId && <span>Plan ID: <code className="font-mono">{planId}</code></span>}
              {passNumber != null && <span>Pass Number: <code className="font-mono">{passNumber}</code></span>}
              {remediatesRunId && <span>Remediates Run ID: <code className="font-mono">{remediatesRunId}</code></span>}
            </div>
          </div>
        )}

        <div className="grid gap-6 md:grid-cols-1">
          <div className="flex flex-col gap-4 rounded border border-border bg-card p-4">
            <h3 className="text-sm font-semibold text-foreground">Upload Execution Spec</h3>
            
            <div className="flex flex-col gap-2">
              <Label htmlFor="spec-file" className="text-xs font-medium">
                Execution Spec JSON content
              </Label>
              <Textarea
                id="spec-content"
                placeholder="Paste canonical Execution Spec JSON here..."
                className="h-64 font-mono text-xs"
                value={canonicalContent}
                onChange={(e) => {
                  setCanonicalContent(e.target.value);
                  if (!fileName) setFileName("execution_spec.json");
                  setSnapshot(null);
                  setErrorMessage(null);
                }}
                disabled={busy}
              />
            </div>

            <div className="flex flex-wrap items-center gap-3">
              <Label
                htmlFor="spec-file"
                className="inline-flex h-8 cursor-pointer items-center justify-center rounded border border-input bg-background px-3 text-xs font-medium hover:bg-accent hover:text-accent-foreground"
              >
                <Upload className="mr-1.5 size-3.5" />
                Upload File
              </Label>
              <Input
                id="spec-file"
                type="file"
                accept=".json"
                className="sr-only"
                onChange={handleFileChange}
                disabled={busy}
              />
              <span className="text-xs text-muted-foreground">
                {fileName ? `Loaded: ${fileName}` : "Accepts .json"}
              </span>
            </div>

            {errorMessage && (
              <div className="flex items-start gap-2 rounded border border-destructive/30 bg-destructive/5 p-3 text-xs text-destructive">
                <AlertCircle className="mt-0.5 size-3.5 shrink-0" />
                <span>{errorMessage}</span>
              </div>
            )}

            <div className="flex gap-2 justify-end">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy || !canonicalContent.trim()}
                onClick={() => validateMutation.mutate()}
              >
                {validateMutation.isPending && <Loader2 className="mr-1.5 size-3.5 animate-spin" />}
                Validate Spec
              </Button>
              <Button
                type="button"
                size="sm"
                disabled={busy || !validationReady}
                onClick={() => submitMutation.mutate()}
              >
                {submitMutation.isPending && <Loader2 className="mr-1.5 size-3.5 animate-spin" />}
                Create Run
              </Button>
            </div>
          </div>

          {currentSnapshot && (
            <div className="flex flex-col gap-4 rounded border border-border bg-card p-4">
              <h3 className="text-sm font-semibold text-foreground">Validation Results</h3>
              <div className="flex items-center gap-2 text-xs">
                {currentSnapshot.validation.ok ? (
                  <CheckCircle2 className="size-4 text-emerald-500" />
                ) : (
                  <AlertCircle className="size-4 text-destructive" />
                )}
                <span>
                  Status: <span className="font-semibold">{currentSnapshot.validation.status}</span>
                </span>
                <span className="ml-auto font-mono text-[10px] text-muted-foreground/80">
                  SHA-256: {currentSnapshot.validation.sha256.slice(0, 12)}…
                </span>
              </div>

              {currentSnapshot.validation.diagnostics && currentSnapshot.validation.diagnostics.length > 0 && (
                <div className="flex flex-col gap-2">
                  <span className="text-xs font-semibold text-destructive">Diagnostics</span>
                  <ul className="space-y-1.5">
                    {currentSnapshot.validation.diagnostics.map((diag: any, index: number) => (
                      <li
                        key={`diag-${index}`}
                        className="rounded border border-destructive/20 bg-destructive/5 p-2 font-mono text-[10px] text-destructive"
                      >
                        {JSON.stringify(diag)}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </AppPageFrame>
  );
}
