import * as React from "react";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  FileJson,
  Loader2,
  Send,
  Trash2,
} from "lucide-react";

import {
  getEditorLineCount,
  getPlanSubmissionPreview,
  parsePlanJson,
  type PlanSubmissionPreview,
  type PlanSubmissionState,
} from "@/components/relay/relayPlanSubmissionState";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelayApiError } from "@/features/relay-runs";
import {
  relayPlanKeys,
  submitPlan,
  validatePlan,
  type PlanValidationIssue,
  type SubmitPlanResponse,
  type ValidatePlanResponse,
} from "@/features/relay-plans";
import { cn } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";

const EXAMPLE_PLAN_JSON = `{
  "plan_meta": {
    "plan_id": "plan-example",
    "schema_version": "1.0.0",
    "created_at": "2026-06-21T00:00:00Z",
    "title": "Descriptive plan title",
    "goal": "High-level goal for this managed plan",
    "repo_target": "owner/repo",
    "branch_context": "main",
    "status": "active"
  },
  "source_intent": {
    "summary": "Brief description of what this plan achieves."
  },
  "passes": [
    {
      "pass_id": "PASS-001",
      "sequence": 1,
      "name": "First pass name",
      "goal": "Specific goal for this pass",
      "intended_execution_scope": ["path/to/file.ts"],
      "non_goals": ["Out-of-scope behavior for this pass"],
      "dependencies": [],
      "status": "planned"
    }
  ]
}`;

interface SubmissionError {
  message: string;
  issues: PlanValidationIssue[];
}

const stateLabels: Record<PlanSubmissionState, string> = {
  draft: "Draft",
  parse_failed: "Validation Failed",
  validating: "Validating",
  validated: "Validated",
  validation_failed: "Validation Failed",
  submitting: "Submitting",
  submitted: "Submitted",
  submit_failed: "Submission Failed",
};

function getRightPaneTitle(state: PlanSubmissionState): string {
  switch (state) {
    case "validated":
      return "Plan Preview";
    case "submitting":
      return "Submitting";
    case "submitted":
    case "submit_failed":
      return "Submission Result";
    default:
      return "Validation";
  }
}

function getStateBadgeVariant(
  state: PlanSubmissionState,
): React.ComponentProps<typeof Badge>["variant"] {
  switch (state) {
    case "validated":
    case "submitted":
      return "success";
    case "parse_failed":
    case "validation_failed":
    case "submit_failed":
      return "destructive";
    case "validating":
    case "submitting":
      return "info";
    default:
      return "outline";
  }
}

function extractIssues(value: unknown): PlanValidationIssue[] {
  if (!value || typeof value !== "object") return [];
  const record = value as Record<string, unknown>;
  const directIssues = record.issues;
  if (Array.isArray(directIssues)) return directIssues as PlanValidationIssue[];

  const validation = record.validation;
  if (validation && typeof validation === "object") {
    const validationIssues = (validation as Record<string, unknown>).issues;
    if (Array.isArray(validationIssues)) {
      return validationIssues as PlanValidationIssue[];
    }
  }

  const details = record.details;
  if (details && typeof details === "object") {
    return extractIssues(details);
  }

  return [];
}

function getSubmissionError(error: unknown): SubmissionError {
  if (error instanceof RelayApiError) {
    const shape = error.errorShape;
    return {
      message: shape?.message || shape?.error || error.message,
      issues: extractIssues(shape),
    };
  }

  if (error instanceof Error) {
    return { message: error.message, issues: [] };
  }

  return { message: "Submission failed.", issues: [] };
}

function IssueList({ issues }: { issues: PlanValidationIssue[] }) {
  if (issues.length === 0) {
    return (
      <p className="text-xs leading-relaxed text-muted-foreground">
        No detailed issue payload was returned.
      </p>
    );
  }

  return (
    <div className="space-y-2">
      {issues.map((issue, index) => (
        <div
          key={`${issue.path ?? "root"}-${issue.code ?? "issue"}-${index}`}
          className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2"
        >
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[10px] text-muted-foreground">
              {issue.path || "root"}
            </span>
            {issue.code ? (
              <Badge variant="outline" className="h-auto rounded-sm px-1.5 py-0 text-[10px]">
                {issue.code}
              </Badge>
            ) : null}
            {issue.severity ? (
              <span className="text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
                {issue.severity}
              </span>
            ) : null}
          </div>
          <p className="mt-1 text-xs leading-relaxed text-foreground">{issue.message}</p>
        </div>
      ))}
    </div>
  );
}

function PreviewSummary({ preview }: { preview: PlanSubmissionPreview }) {
  return (
    <div className="space-y-3">
      <div className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-foreground">{preview.title}</h3>
            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[11px] text-muted-foreground">
              <span>{preview.planId}</span>
              <span className="text-muted-foreground/60">/</span>
              <span>{preview.repoTarget}</span>
              <span className="text-muted-foreground/60">/</span>
              <span>{preview.branchContext}</span>
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Badge variant="info" className="rounded-sm">
              {preview.passCount} passes
            </Badge>
            <Badge variant="outline" className="rounded-sm">
              {preview.dependencyCount} dependencies
            </Badge>
          </div>
        </div>
        <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
          {preview.goal}
        </p>
        <div className="mt-3 border-t border-[var(--relay-row-border)] pt-3">
          <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Source Intent
          </div>
          <p className="mt-1 text-xs leading-relaxed text-foreground/85">
            {preview.sourceIntentSummary}
          </p>
        </div>
      </div>

      <div>
        <div className="mb-2 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Derived Passes
        </div>
        <div className="space-y-2">
          {preview.passes.map((pass) => (
            <div
              key={pass.passId}
              className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2.5"
            >
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-[10px] text-muted-foreground">
                      {String(pass.sequence).padStart(2, "0")}
                    </span>
                    <span className="font-mono text-[11px] text-foreground">
                      {pass.passId}
                    </span>
                    <Badge variant="outline" className="h-auto rounded-sm px-1.5 py-0 text-[10px]">
                      {pass.status}
                    </Badge>
                  </div>
                  <h4 className="mt-1 text-sm font-medium leading-snug text-foreground">
                    {pass.name}
                  </h4>
                </div>
              </div>
              <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                {pass.goal}
              </p>
              <div className="mt-2 flex flex-wrap gap-1.5">
                {pass.dependencies.length > 0 ? (
                  pass.dependencies.map((dependency) => (
                    <span
                      key={`${pass.passId}-${dependency}`}
                      className="rounded-sm border border-[var(--relay-row-border)] px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                    >
                      depends {dependency}
                    </span>
                  ))
                ) : (
                  <span className="rounded-sm border border-[var(--relay-row-border)] px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                    no dependencies
                  </span>
                )}
                {pass.intendedExecutionScope.slice(0, 3).map((scope) => (
                  <span
                    key={`${pass.passId}-${scope}`}
                    className="max-w-full truncate rounded-sm border border-[var(--relay-row-border)] px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                  >
                    {scope}
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function DraftPanel() {
  const checklist = [
    "JSON parses successfully",
    "Required plan fields present",
    "Passes derive correctly",
    "Dependencies resolve",
  ];

  return (
    <div className="space-y-4">
      <div className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
        <div className="font-medium text-foreground">Validation not run</div>
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
          Paste reviewed Plan of Passes JSON, then validate it against Relay before
          submission.
        </p>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        {checklist.map((item) => (
          <div
            key={item}
            className="flex items-center gap-2 border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2 text-xs text-muted-foreground"
          >
            <span className="h-1.5 w-1.5 rounded-full bg-[var(--relay-row-border)]" />
            {item}
          </div>
        ))}
      </div>

      {["Plan Preview", "Derived Passes", "Submission Result"].map((label) => (
        <div
          key={label}
          className="border border-dashed border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3"
        >
          <div className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
            {label}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Available after validation.
          </p>
        </div>
      ))}
    </div>
  );
}

function LoadingPanel({ label }: { label: string }) {
  return (
    <div className="flex min-h-[18rem] items-center justify-center border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]">
      <div className="flex flex-col items-center gap-3 text-center">
        <Loader2 className="size-6 animate-spin text-[var(--relay-accent)]" />
        <p className="text-sm font-medium text-foreground">{label}</p>
      </div>
    </div>
  );
}

interface RightPaneProps {
  state: PlanSubmissionState;
  validationResponse?: ValidatePlanResponse;
  preview?: PlanSubmissionPreview;
  issues: PlanValidationIssue[];
  submitResponse?: SubmitPlanResponse;
  submitError?: SubmissionError;
}

function RightPane({
  state,
  validationResponse,
  preview,
  issues,
  submitResponse,
  submitError,
}: RightPaneProps) {
  if (state === "validating") return <LoadingPanel label="Validating plan JSON..." />;
  if (state === "submitting") return <LoadingPanel label="Submitting plan..." />;

  if (state === "parse_failed" || state === "validation_failed") {
    return (
      <div className="space-y-3">
        <div className="border border-destructive/30 bg-destructive/10 px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-medium text-destructive">
            <AlertTriangle className="size-4" />
            Validation failed
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {issues.length} issue{issues.length === 1 ? "" : "s"} found.
          </p>
        </div>
        <IssueList issues={issues} />
      </div>
    );
  }

  if (state === "validated" && preview) {
    return (
      <div className="space-y-3">
        <div className="border border-success/30 bg-success/12 px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-medium text-success">
            <CheckCircle2 className="size-4" />
            Plan JSON is valid
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Backend validation returned{" "}
            {validationResponse?.validation.issues.length ?? 0} issue
            {(validationResponse?.validation.issues.length ?? 0) === 1 ? "" : "s"}.
          </p>
        </div>
        <PreviewSummary preview={preview} />
      </div>
    );
  }

  if (state === "submitted" && submitResponse) {
    return (
      <div className="space-y-3">
        <div className="border border-success/30 bg-success/12 px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-medium text-success">
            <CheckCircle2 className="size-4" />
            Plan and pass records created
          </div>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {submitResponse.passes.length} pass record
            {submitResponse.passes.length === 1 ? "" : "s"} created. No runs were
            created and no executor was dispatched.
          </p>
        </div>
        <div className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
          <div className="text-sm font-medium text-foreground">
            {submitResponse.plan.title}
          </div>
          <div className="mt-1 flex flex-wrap gap-x-2 gap-y-1 font-mono text-[11px] text-muted-foreground">
            <span>{submitResponse.plan.planId}</span>
            <span className="text-muted-foreground/60">/</span>
            <span>{submitResponse.plan.repoTarget}</span>
            <span className="text-muted-foreground/60">/</span>
            <span>{submitResponse.plan.branchContext}</span>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button asChild size="sm">
            <Link
              to="/plans/$planId"
              params={{ planId: submitResponse.plan.planId }}
            >
              Open Plan
            </Link>
          </Button>
          <Button asChild variant="outline" size="sm">
            <Link to="/plans">View Plans</Link>
          </Button>
        </div>
      </div>
    );
  }

  if (state === "submit_failed") {
    return (
      <div className="space-y-3">
        <div className="border border-destructive/30 bg-destructive/10 px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-medium text-destructive">
            <AlertTriangle className="size-4" />
            Submission failed
          </div>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {submitError?.message ?? "Relay could not submit this plan."} Edit or
            revalidate the plan, then retry submission.
          </p>
        </div>
        <IssueList issues={submitError?.issues ?? []} />
      </div>
    );
  }

  return <DraftPanel />;
}

export function RelayPlanSubmissionWorkbench() {
  const queryClient = useQueryClient();
  const [rawJson, setRawJson] = React.useState("");
  const [state, setState] = React.useState<PlanSubmissionState>("draft");
  const [validatedRawJson, setValidatedRawJson] = React.useState<string | undefined>();
  const [validationResponse, setValidationResponse] =
    React.useState<ValidatePlanResponse | undefined>();
  const [preview, setPreview] = React.useState<PlanSubmissionPreview | undefined>();
  const [issues, setIssues] = React.useState<PlanValidationIssue[]>([]);
  const [submitResponse, setSubmitResponse] =
    React.useState<SubmitPlanResponse | undefined>();
  const [submitError, setSubmitError] = React.useState<SubmissionError | undefined>();

  const trimmedJson = rawJson.trim();
  const lineCount = getEditorLineCount(rawJson);
  const isBusy = state === "validating" || state === "submitting";
  const canValidate = trimmedJson.length > 0 && !isBusy;
  const canSubmit =
    state === "validated" &&
    validatedRawJson === rawJson &&
    validationResponse?.validation.valid === true &&
    !isBusy;

  const resetResultState = React.useCallback(() => {
    setValidationResponse(undefined);
    setPreview(undefined);
    setIssues([]);
    setSubmitResponse(undefined);
    setSubmitError(undefined);
    setValidatedRawJson(undefined);
  }, []);

  const handleEditorChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => {
    setRawJson(event.target.value);
    if (state !== "draft" && state !== "validating" && state !== "submitting") {
      setState("draft");
      resetResultState();
    }
  };

  const clearEditor = () => {
    setRawJson("");
    setState("draft");
    resetResultState();
  };

  const handleValidate = async () => {
    const parsed = parsePlanJson(rawJson);
    setSubmitResponse(undefined);
    setSubmitError(undefined);

    if (!parsed.ok) {
      setState("parse_failed");
      setIssues([parsed.issue]);
      setValidationResponse(undefined);
      setPreview(undefined);
      setValidatedRawJson(undefined);
      return;
    }

    setState("validating");
    setIssues([]);

    try {
      const response = await validatePlan({ plan: parsed.plan });
      setValidationResponse(response);

      if (response.validation.valid) {
        setState("validated");
        setValidatedRawJson(rawJson);
        setPreview(getPlanSubmissionPreview(parsed.plan));
        setIssues(response.validation.issues);
      } else {
        setState("validation_failed");
        setValidatedRawJson(undefined);
        setPreview(undefined);
        setIssues(response.validation.issues);
      }
    } catch (error) {
      const nextError = getSubmissionError(error);
      setState("validation_failed");
      setIssues(
        nextError.issues.length > 0
          ? nextError.issues
          : [
              {
                severity: "error",
                code: "validation_request_failed",
                path: "root",
                message: nextError.message,
              },
            ],
      );
      setValidatedRawJson(undefined);
      setPreview(undefined);
    }
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    const parsed = parsePlanJson(rawJson);
    if (!parsed.ok) {
      setState("parse_failed");
      setIssues([parsed.issue]);
      return;
    }

    setState("submitting");
    setSubmitError(undefined);

    try {
      const response = await submitPlan({ plan: parsed.plan });
      setSubmitResponse(response);
      setState("submitted");
      void queryClient.invalidateQueries({ queryKey: relayPlanKeys.all });
    } catch (error) {
      setSubmitError(getSubmissionError(error));
      setState("submit_failed");
    }
  };

  const gutterLines = React.useMemo(
    () => Array.from({ length: lineCount }, (_, index) => index + 1),
    [lineCount],
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-[var(--relay-content-bg)]">
      <div className="shrink-0 border-b border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FileJson className="size-4 text-[var(--relay-accent)]" />
            <span>
              Submitting creates plan and pass records only. No runs, no executor
              dispatch.
            </span>
          </div>
          <Badge
            variant={getStateBadgeVariant(state)}
            className="h-auto rounded-sm px-2 py-0.5 text-[10px]"
          >
            {stateLabels[state]}
          </Badge>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto lg:grid-cols-[minmax(26rem,34rem)_minmax(0,1fr)] lg:overflow-hidden">
        <section className="flex min-h-[34rem] flex-col border-b border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] lg:min-h-0 lg:border-b-0 lg:border-r">
          <div className="flex shrink-0 items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">
                Plan of Passes JSON
              </h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                Reviewed structured Planner Pass Plan artifact
              </p>
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Badge variant="outline" className="rounded-sm font-mono text-[10px]">
                application/json
              </Badge>
              <Badge
                variant={getStateBadgeVariant(state)}
                className="rounded-sm text-[10px]"
              >
                {stateLabels[state]}
              </Badge>
            </div>
          </div>

          <div className="min-h-0 flex-1 overflow-hidden">
            <div className="flex h-full min-h-[24rem] overflow-hidden">
              <div className="w-12 shrink-0 overflow-hidden border-r border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-2 py-3 text-right font-mono text-[11px] leading-5 text-muted-foreground">
                {gutterLines.map((line) => (
                  <div key={line} className="h-5">
                    {line}
                  </div>
                ))}
              </div>
              <textarea
                value={rawJson}
                onChange={handleEditorChange}
                placeholder={EXAMPLE_PLAN_JSON}
                spellCheck={false}
                className={cn(
                  "h-full min-h-[24rem] flex-1 resize-none border-0 bg-[var(--relay-content-bg)] px-3 py-3 font-mono text-[12px] leading-5 text-foreground outline-none placeholder:text-muted-foreground/70 focus:ring-0",
                  isBusy && "opacity-80",
                )}
              />
            </div>
          </div>

          <div className="shrink-0 border-t border-[var(--relay-row-border)] px-4 py-3">
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted-foreground">
              <span className="font-mono">
                {rawJson.length} chars / {lineCount} lines
              </span>
              <span>{trimmedJson.length === 0 ? "Empty draft" : stateLabels[state]}</span>
            </div>
            <div className="grid gap-2">
              <div className="grid grid-cols-2 gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={!canValidate}
                  onClick={handleValidate}
                >
                  {state === "validating" ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <CheckCircle2 className="size-3.5" />
                  )}
                  Validate Plan
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={isBusy || rawJson.length === 0}
                  onClick={clearEditor}
                >
                  <Trash2 className="size-3.5" />
                  Clear
                </Button>
              </div>
              <Button
                type="button"
                size="sm"
                disabled={!canSubmit}
                onClick={handleSubmit}
                className="w-full"
              >
                {state === "submitting" ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Send className="size-3.5" />
                )}
                Submit Reviewed Plan
              </Button>
            </div>
          </div>
        </section>

        <section className="min-h-[34rem] overflow-y-auto bg-[var(--relay-page-body-bg)] lg:min-h-0">
          <div className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-[var(--relay-row-border)] bg-[var(--relay-page-header-bg)] px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">
                {getRightPaneTitle(state)}
              </h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                Validate before submission; edit resets the submit gate.
              </p>
            </div>
            <Badge variant={getStateBadgeVariant(state)} className="rounded-sm text-[10px]">
              {stateLabels[state]}
            </Badge>
          </div>
          <div className="p-4">
            <RightPane
              state={state}
              validationResponse={validationResponse}
              preview={preview}
              issues={issues}
              submitResponse={submitResponse}
              submitError={submitError}
            />
          </div>
        </section>
      </div>
    </div>
  );
}
