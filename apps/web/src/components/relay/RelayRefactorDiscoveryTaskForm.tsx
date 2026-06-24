import * as React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { RelayRefactorValidationIssues } from "./RelayRefactorValidationIssues";
import {
  createRefactorDiscoveryTask,
  extractRefactorValidationIssues,
  formatLines,
  formatMetadata,
  parseLines,
  parseMetadata,
  parseTags,
  relayRefactorKeys,
  updateRefactorDiscoveryTask,
} from "@/features/relay-refactors";
import type {
  RefactorDiscoveryTask,
  RefactorDiscoveryTaskRequest,
  RefactorTargetScopeKind,
  RefactorValidationIssue,
} from "@/features/relay-refactors";

const TARGET_SCOPE_KINDS: RefactorTargetScopeKind[] = [
  "repository",
  "subsystem",
  "directory",
  "file_set",
  "plan",
  "pass",
];

const PRIORITIES = ["low", "normal", "high", "urgent"];

const fieldLabelClass = "text-xs font-semibold uppercase tracking-wider text-muted-foreground";
const selectClass =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30";

interface RelayRefactorDiscoveryTaskFormProps {
  projectId: string;
  task?: RefactorDiscoveryTask;
  onClose: () => void;
}

export function RelayRefactorDiscoveryTaskForm({
  projectId,
  task,
  onClose,
}: RelayRefactorDiscoveryTaskFormProps) {
  const queryClient = useQueryClient();
  const isEdit = !!task;

  const [discoveryTaskId, setDiscoveryTaskId] = React.useState(task?.discoveryTaskId ?? "");
  const [title, setTitle] = React.useState(task?.title ?? "");
  const [analysisPrompt, setAnalysisPrompt] = React.useState(task?.analysisPrompt ?? "");
  const [scopeKind, setScopeKind] = React.useState<RefactorTargetScopeKind>(
    task?.targetScope?.kind ?? "repository",
  );
  const [scopeValues, setScopeValues] = React.useState(
    formatLines(task?.targetScope?.values),
  );
  const [priority, setPriority] = React.useState(task?.priority ?? "normal");
  const [tags, setTags] = React.useState((task?.tags ?? []).join(", "));
  const [metadata, setMetadata] = React.useState(formatMetadata(task?.metadata));

  const [issues, setIssues] = React.useState<RefactorValidationIssue[]>([]);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const scopeValueList = parseLines(scopeValues);
  const canSubmit =
    title.trim().length > 0 &&
    analysisPrompt.trim().length > 0 &&
    scopeValueList.length > 0;

  const mutation = useMutation({
    mutationFn: (request: RefactorDiscoveryTaskRequest) =>
      isEdit
        ? updateRefactorDiscoveryTask(projectId, task!.discoveryTaskId, request)
        : createRefactorDiscoveryTask(projectId, request),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: relayRefactorKeys.project(projectId),
      });
      onClose();
    },
    onError: (err: unknown) => {
      setIssues(extractRefactorValidationIssues(err));
      setErrorMsg(err instanceof Error ? err.message : "Failed to save discovery task");
    },
  });

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    setIssues([]);
    setErrorMsg(null);

    const request: RefactorDiscoveryTaskRequest = {
      title: title.trim(),
      analysis_prompt: analysisPrompt.trim(),
      target_scope: { kind: scopeKind, values: scopeValueList },
      priority: priority.trim() || undefined,
      tags: parseTags(tags),
      metadata: parseMetadata(metadata),
    };
    if (!isEdit && discoveryTaskId.trim()) {
      request.discovery_task_id = discoveryTaskId.trim();
    }

    mutation.mutate(request);
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4"
    >
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-foreground">
          {isEdit ? "Edit discovery task" : "New discovery task"}
        </h3>
        <Badge>{isEdit ? task?.status : "draft"}</Badge>
      </div>

      <RelayRefactorValidationIssues issues={issues} message={errorMsg} />

      {!isEdit ? (
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="discovery-task-id">
            Discovery task ID
          </Label>
          <Input
            id="discovery-task-id"
            value={discoveryTaskId}
            onChange={(e) => setDiscoveryTaskId(e.target.value)}
            placeholder="Optional"
          />
          <p className="text-[11px] text-muted-foreground">
            Leave blank for a backend-generated ID if supported by the service;
            otherwise backend validation will require it.
          </p>
        </div>
      ) : null}

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="discovery-title">
          Title <span className="text-destructive">*</span>
        </Label>
        <Input
          id="discovery-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
        />
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="discovery-prompt">
          Analysis prompt <span className="text-destructive">*</span>
        </Label>
        <Textarea
          id="discovery-prompt"
          value={analysisPrompt}
          onChange={(e) => setAnalysisPrompt(e.target.value)}
          className="min-h-24"
          required
        />
        <p className="text-[11px] text-muted-foreground">
          A manual analysis prompt. Relay does not analyze the repository
          automatically.
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="discovery-scope-kind">
            Target scope kind
          </Label>
          <select
            id="discovery-scope-kind"
            className={selectClass}
            value={scopeKind}
            onChange={(e) => setScopeKind(e.target.value)}
          >
            {TARGET_SCOPE_KINDS.map((kind) => (
              <option key={kind} value={kind}>
                {kind}
              </option>
            ))}
          </select>
        </div>
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="discovery-priority">
            Priority
          </Label>
          <select
            id="discovery-priority"
            className={selectClass}
            value={priority}
            onChange={(e) => setPriority(e.target.value)}
          >
            {PRIORITIES.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="discovery-scope-values">
          Target scope values <span className="text-destructive">*</span>
        </Label>
        <Textarea
          id="discovery-scope-values"
          value={scopeValues}
          onChange={(e) => setScopeValues(e.target.value)}
          placeholder="One value per line"
          className="min-h-20 font-mono text-xs"
        />
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="discovery-tags">
          Tags
        </Label>
        <Input
          id="discovery-tags"
          value={tags}
          onChange={(e) => setTags(e.target.value)}
          placeholder="Comma or newline separated"
        />
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="discovery-metadata">
          Metadata
        </Label>
        <Textarea
          id="discovery-metadata"
          value={metadata}
          onChange={(e) => setMetadata(e.target.value)}
          placeholder="key=value per line"
          className="min-h-16 font-mono text-xs"
        />
      </div>

      <div className="flex items-center justify-end gap-2 pt-1">
        <Button type="button" variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" size="sm" disabled={!canSubmit || mutation.isPending}>
          {mutation.isPending ? "Saving…" : isEdit ? "Save" : "Create"}
        </Button>
      </div>
    </form>
  );
}
