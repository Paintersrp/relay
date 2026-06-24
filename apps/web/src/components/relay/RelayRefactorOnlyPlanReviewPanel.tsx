import * as React from "react";
import { useMutation } from "@tanstack/react-query";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { RelayRefactorValidationIssues } from "./RelayRefactorValidationIssues";
import {
  extractRefactorValidationIssues,
  generateRefactorOnlyPlan,
} from "@/features/relay-refactors";
import type {
  GenerateRefactorOnlyPlanResult,
  RefactorValidationIssue,
} from "@/features/relay-refactors";

const labelClass = "text-xs font-semibold uppercase tracking-wider text-muted-foreground";

interface RelayRefactorOnlyPlanReviewPanelProps {
  projectId: string;
  selectedCandidateIds: string[];
  onGenerated: () => void;
}

function CopyablePath({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = React.useState(false);

  const copy = async () => {
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="space-y-1">
      <p className={labelClass}>{label}</p>
      <div className="flex items-center gap-2">
        <code className="flex-1 overflow-x-auto rounded border border-[var(--relay-row-border)] bg-[var(--relay-page-body-bg)] px-2 py-1 font-mono text-xs">
          {value || "—"}
        </code>
        {value ? (
          <Button variant="outline" size="xs" onClick={copy}>
            {copied ? "Copied" : "Copy"}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

export function RelayRefactorOnlyPlanReviewPanel({
  projectId,
  selectedCandidateIds,
  onGenerated,
}: RelayRefactorOnlyPlanReviewPanelProps) {
  const [title, setTitle] = React.useState("");
  const [note, setNote] = React.useState("");
  const [issues, setIssues] = React.useState<RefactorValidationIssue[]>([]);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [result, setResult] = React.useState<GenerateRefactorOnlyPlanResult | null>(null);

  const mutation = useMutation({
    mutationFn: () =>
      generateRefactorOnlyPlan(projectId, {
        candidate_ids: selectedCandidateIds,
        title: title.trim() || undefined,
        note: note.trim() || undefined,
      }),
    onSuccess: (data) => {
      setResult(data);
      onGenerated();
    },
    onError: (err: unknown) => {
      setIssues(extractRefactorValidationIssues(err));
      setErrorMsg(err instanceof Error ? err.message : "Failed to generate plan");
    },
  });

  const canGenerate = selectedCandidateIds.length > 0 && !mutation.isPending;

  return (
    <div className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold text-foreground">
          Generate refactor-only plan
        </h3>
        <Badge variant="outline">{selectedCandidateIds.length} selected</Badge>
      </div>

      <p className="text-xs leading-relaxed text-muted-foreground">
        Only ready candidates can be selected. The generated Plan of Passes is a
        review artifact only.
      </p>

      {selectedCandidateIds.length > 0 ? (
        <div className="space-y-1">
          <p className={labelClass}>Selected candidates</p>
          <p className="font-mono text-[11px] text-muted-foreground">
            {selectedCandidateIds.join(", ")}
          </p>
        </div>
      ) : null}

      <RelayRefactorValidationIssues issues={issues} message={errorMsg} />

      <div className="space-y-1.5">
        <Label className={labelClass} htmlFor="generate-title">
          Title
        </Label>
        <Input
          id="generate-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Optional"
        />
      </div>

      <div className="space-y-1.5">
        <Label className={labelClass} htmlFor="generate-note">
          Note
        </Label>
        <Textarea
          id="generate-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          className="min-h-16"
          placeholder="Optional"
        />
      </div>

      <div className="flex items-center justify-end">
        <Button
          size="sm"
          onClick={() => {
            setIssues([]);
            setErrorMsg(null);
            mutation.mutate();
          }}
          disabled={!canGenerate}
        >
          {mutation.isPending ? "Generating…" : "Generate refactor-only plan"}
        </Button>
      </div>

      {result ? (
        <div className="space-y-3 rounded border border-warning/30 bg-warning/10 p-3">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-semibold text-foreground">
              Generated refactor-only plan — review required
            </h4>
            {result.submissionPolicy ? (
              <Badge variant="warning">{result.submissionPolicy}</Badge>
            ) : null}
          </div>

          <div className="space-y-1 text-xs">
            <p>
              <span className="font-semibold">Plan ID:</span>{" "}
              <span className="font-mono">{result.planId}</span>
            </p>
            {result.candidateIds.length > 0 ? (
              <p className="font-mono text-[11px] text-muted-foreground">
                Candidates: {result.candidateIds.join(", ")}
              </p>
            ) : null}
          </div>

          <CopyablePath label="JSON artifact path" value={result.jsonArtifactPath} />
          <CopyablePath
            label="Markdown artifact path"
            value={result.markdownArtifactPath}
          />

          {result.warnings.length > 0 ? (
            <div className="space-y-1 rounded border border-warning/30 bg-warning/10 p-2 text-xs text-warning">
              {result.warnings.map((warning, index) => (
                <p key={index}>{warning}</p>
              ))}
            </div>
          ) : null}

          <p className="text-xs leading-relaxed text-muted-foreground">
            This artifact has not been submitted as a managed plan. Review the
            generated Plan of Passes JSON, then submit it through the normal
            reviewed plan submission flow only after explicit confirmation.
          </p>
        </div>
      ) : null}
    </div>
  );
}
