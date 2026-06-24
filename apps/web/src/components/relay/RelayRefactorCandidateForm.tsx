import * as React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { RelayRefactorValidationIssues } from "./RelayRefactorValidationIssues";
import {
  createRefactorCandidate,
  extractRefactorValidationIssues,
  formatLines,
  formatMetadata,
  parseLines,
  parseMetadata,
  relayRefactorKeys,
  updateRefactorCandidate,
} from "@/features/relay-refactors";
import type {
  RefactorCandidate,
  RefactorCandidateRequest,
  RefactorRiskLevel,
  RefactorValidationIssue,
} from "@/features/relay-refactors";

const RISK_LEVELS: RefactorRiskLevel[] = ["low", "medium", "high"];

const fieldLabelClass = "text-xs font-semibold uppercase tracking-wider text-muted-foreground";
const selectClass =
  "h-8 w-full rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30";

interface RelayRefactorCandidateFormProps {
  projectId: string;
  candidate?: RefactorCandidate;
  onClose: () => void;
}

interface ChecklistItem {
  label: string;
  satisfied: boolean;
}

export function RelayRefactorCandidateForm({
  projectId,
  candidate,
  onClose,
}: RelayRefactorCandidateFormProps) {
  const queryClient = useQueryClient();
  const isEdit = !!candidate;

  const [candidateId, setCandidateId] = React.useState(candidate?.candidateId ?? "");
  const [title, setTitle] = React.useState(candidate?.title ?? "");
  const [problemSummary, setProblemSummary] = React.useState(candidate?.problemSummary ?? "");
  const [currentBehavior, setCurrentBehavior] = React.useState(candidate?.currentBehavior ?? "");
  const [desiredBehavior, setDesiredBehavior] = React.useState(candidate?.desiredBehavior ?? "");
  const [rationale, setRationale] = React.useState(candidate?.rationale ?? "");
  const [proposedPassName, setProposedPassName] = React.useState(candidate?.proposedPassName ?? "");
  const [proposedPassGoal, setProposedPassGoal] = React.useState(candidate?.proposedPassGoal ?? "");
  const [proposedPassScope, setProposedPassScope] = React.useState(
    formatLines(candidate?.proposedPassScope),
  );
  const [nonGoals, setNonGoals] = React.useState(formatLines(candidate?.nonGoals));
  const [targetFiles, setTargetFiles] = React.useState(formatLines(candidate?.targetFiles));
  const [validationCommands, setValidationCommands] = React.useState(
    formatLines(candidate?.validationCommands),
  );
  const [auditFocus, setAuditFocus] = React.useState(formatLines(candidate?.auditFocus));
  const [constraints, setConstraints] = React.useState(formatLines(candidate?.constraints));
  const [riskLevel, setRiskLevel] = React.useState<RefactorRiskLevel>(
    candidate?.riskLevel ?? "medium",
  );
  const [dependencyNotes, setDependencyNotes] = React.useState(candidate?.dependencyNotes ?? "");
  const [sourceDiscoveryTaskIds, setSourceDiscoveryTaskIds] = React.useState("");
  const [candidateDependencyIds, setCandidateDependencyIds] = React.useState("");
  const [metadata, setMetadata] = React.useState(formatMetadata(candidate?.metadata));

  const [issues, setIssues] = React.useState<RefactorValidationIssue[]>([]);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const scopeList = parseLines(proposedPassScope);
  const nonGoalsList = parseLines(nonGoals);
  const targetFilesList = parseLines(targetFiles);
  const validationCommandsList = parseLines(validationCommands);
  const auditFocusList = parseLines(auditFocus);

  const checklist: ChecklistItem[] = [
    { label: "Proposed pass goal", satisfied: proposedPassGoal.trim().length > 0 },
    { label: "Problem statement", satisfied: problemSummary.trim().length > 0 },
    { label: "Bounded scope", satisfied: scopeList.length > 0 },
    { label: "Non-goals", satisfied: nonGoalsList.length > 0 },
    { label: "Target files", satisfied: targetFilesList.length > 0 },
    { label: "Validation commands", satisfied: validationCommandsList.length > 0 },
    { label: "Audit focus", satisfied: auditFocusList.length > 0 },
    { label: "Risk level", satisfied: riskLevel.trim().length > 0 },
  ];

  const canSubmit =
    title.trim().length > 0 &&
    problemSummary.trim().length > 0 &&
    desiredBehavior.trim().length > 0 &&
    rationale.trim().length > 0 &&
    proposedPassName.trim().length > 0 &&
    proposedPassGoal.trim().length > 0 &&
    scopeList.length > 0 &&
    nonGoalsList.length > 0 &&
    targetFilesList.length > 0 &&
    validationCommandsList.length > 0 &&
    auditFocusList.length > 0 &&
    riskLevel.trim().length > 0;

  const mutation = useMutation({
    mutationFn: (request: RefactorCandidateRequest) =>
      isEdit
        ? updateRefactorCandidate(projectId, candidate!.candidateId, request)
        : createRefactorCandidate(projectId, request),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: relayRefactorKeys.project(projectId),
      });
      onClose();
    },
    onError: (err: unknown) => {
      setIssues(extractRefactorValidationIssues(err));
      setErrorMsg(err instanceof Error ? err.message : "Failed to save candidate");
    },
  });

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    setIssues([]);
    setErrorMsg(null);

    const request: RefactorCandidateRequest = {
      title: title.trim(),
      problem_summary: problemSummary.trim(),
      current_behavior: currentBehavior.trim() || undefined,
      desired_behavior: desiredBehavior.trim(),
      rationale: rationale.trim(),
      proposed_pass_name: proposedPassName.trim(),
      proposed_pass_goal: proposedPassGoal.trim(),
      proposed_pass_scope: scopeList,
      non_goals: nonGoalsList,
      target_files: targetFilesList,
      validation_commands: validationCommandsList,
      audit_focus: auditFocusList,
      constraints: parseLines(constraints),
      risk_level: riskLevel,
      dependency_notes: dependencyNotes.trim() || undefined,
      source_discovery_task_ids: parseLines(sourceDiscoveryTaskIds),
      candidate_dependency_ids: parseLines(candidateDependencyIds),
      metadata: parseMetadata(metadata),
    };
    if (!isEdit && candidateId.trim()) {
      request.candidate_id = candidateId.trim();
    }

    mutation.mutate(request);
  };

  const renderListField = (
    id: string,
    label: string,
    value: string,
    setValue: (v: string) => void,
    required: boolean,
    placeholder = "One item per line",
  ) => (
    <div className="space-y-1.5">
      <Label className={fieldLabelClass} htmlFor={id}>
        {label} {required ? <span className="text-destructive">*</span> : null}
      </Label>
      <Textarea
        id={id}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder={placeholder}
        className="min-h-20 font-mono text-xs"
      />
    </div>
  );

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-4 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4"
    >
      <h3 className="text-sm font-semibold text-foreground">
        {isEdit ? "Edit pass-ready candidate" : "New pass-ready candidate"}
      </h3>

      {/* Pass-ready checklist */}
      <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-page-body-bg)] p-3">
        <p className={cn(fieldLabelClass, "mb-2")}>Pass-ready checklist</p>
        <ul className="grid gap-1.5 sm:grid-cols-2">
          {checklist.map((item) => (
            <li
              key={item.label}
              className={cn(
                "flex items-center gap-1.5 text-xs",
                item.satisfied ? "text-success" : "text-muted-foreground",
              )}
            >
              {item.satisfied ? (
                <Check className="size-3.5 shrink-0" />
              ) : (
                <X className="size-3.5 shrink-0" />
              )}
              <span>{item.label}</span>
            </li>
          ))}
        </ul>
      </div>

      <RelayRefactorValidationIssues issues={issues} message={errorMsg} />

      {!isEdit ? (
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="candidate-id">
            Candidate ID
          </Label>
          <Input
            id="candidate-id"
            value={candidateId}
            onChange={(e) => setCandidateId(e.target.value)}
            placeholder="Optional"
          />
        </div>
      ) : null}

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="candidate-title">
          Title <span className="text-destructive">*</span>
        </Label>
        <Input
          id="candidate-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
        />
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="candidate-problem">
          Problem summary <span className="text-destructive">*</span>
        </Label>
        <Textarea
          id="candidate-problem"
          value={problemSummary}
          onChange={(e) => setProblemSummary(e.target.value)}
          className="min-h-20"
          required
        />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="candidate-current">
            Current behavior
          </Label>
          <Textarea
            id="candidate-current"
            value={currentBehavior}
            onChange={(e) => setCurrentBehavior(e.target.value)}
            className="min-h-20"
          />
        </div>
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="candidate-desired">
            Desired behavior <span className="text-destructive">*</span>
          </Label>
          <Textarea
            id="candidate-desired"
            value={desiredBehavior}
            onChange={(e) => setDesiredBehavior(e.target.value)}
            className="min-h-20"
            required
          />
        </div>
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="candidate-rationale">
          Rationale <span className="text-destructive">*</span>
        </Label>
        <Textarea
          id="candidate-rationale"
          value={rationale}
          onChange={(e) => setRationale(e.target.value)}
          className="min-h-20"
          required
        />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="candidate-pass-name">
            Proposed pass name <span className="text-destructive">*</span>
          </Label>
          <Input
            id="candidate-pass-name"
            value={proposedPassName}
            onChange={(e) => setProposedPassName(e.target.value)}
            required
          />
        </div>
        <div className="space-y-1.5">
          <Label className={fieldLabelClass} htmlFor="candidate-risk">
            Risk level <span className="text-destructive">*</span>
          </Label>
          <select
            id="candidate-risk"
            className={selectClass}
            value={riskLevel}
            onChange={(e) => setRiskLevel(e.target.value)}
          >
            {RISK_LEVELS.map((level) => (
              <option key={level} value={level}>
                {level}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="candidate-pass-goal">
          Proposed pass goal <span className="text-destructive">*</span>
        </Label>
        <Textarea
          id="candidate-pass-goal"
          value={proposedPassGoal}
          onChange={(e) => setProposedPassGoal(e.target.value)}
          className="min-h-16"
          required
        />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        {renderListField(
          "candidate-scope",
          "Proposed pass scope",
          proposedPassScope,
          setProposedPassScope,
          true,
        )}
        {renderListField("candidate-non-goals", "Non-goals", nonGoals, setNonGoals, true)}
        {renderListField(
          "candidate-target-files",
          "Target files",
          targetFiles,
          setTargetFiles,
          true,
        )}
        {renderListField(
          "candidate-validation",
          "Validation commands",
          validationCommands,
          setValidationCommands,
          true,
        )}
        {renderListField(
          "candidate-audit-focus",
          "Audit focus",
          auditFocus,
          setAuditFocus,
          true,
        )}
        {renderListField(
          "candidate-constraints",
          "Constraints",
          constraints,
          setConstraints,
          false,
        )}
        {renderListField(
          "candidate-source-tasks",
          "Source discovery task IDs",
          sourceDiscoveryTaskIds,
          setSourceDiscoveryTaskIds,
          false,
        )}
        {renderListField(
          "candidate-deps",
          "Candidate dependency IDs",
          candidateDependencyIds,
          setCandidateDependencyIds,
          false,
        )}
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="candidate-dep-notes">
          Dependency notes
        </Label>
        <Textarea
          id="candidate-dep-notes"
          value={dependencyNotes}
          onChange={(e) => setDependencyNotes(e.target.value)}
          className="min-h-16"
        />
      </div>

      <div className="space-y-1.5">
        <Label className={fieldLabelClass} htmlFor="candidate-metadata">
          Metadata
        </Label>
        <Textarea
          id="candidate-metadata"
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
