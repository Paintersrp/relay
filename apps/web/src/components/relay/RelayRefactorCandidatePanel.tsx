import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { relayPlanKeys } from "@/features/relay-plans";
import { RelayRefactorValidationIssues } from "./RelayRefactorValidationIssues";
import {
  canPromoteCandidate,
  canSelectCandidateForGeneratedPlan,
  deferRefactorCandidate,
  extractRefactorValidationIssues,
  groupCandidates,
  promoteRefactorCandidate,
  refactorCandidateStatusLabel,
  refactorPlacementSuggestionQueryOptions,
  rejectRefactorCandidate,
  relayRefactorKeys,
  supersedeRefactorCandidate,
} from "@/features/relay-refactors";
import type {
  RefactorCandidate,
  RefactorValidationIssue,
} from "@/features/relay-refactors";

const labelClass = "text-xs font-semibold uppercase tracking-wider text-muted-foreground";

function statusVariant(status: string) {
  switch (status) {
    case "ready":
      return "info" as const;
    case "scheduled":
      return "running" as const;
    case "scheduled_revision_required":
      return "warning" as const;
    case "completed":
    case "completed_with_warnings":
      return "success" as const;
    case "deferred":
    case "rejected":
    case "superseded":
      return "secondary" as const;
    default:
      return "outline" as const;
  }
}

interface RelayRefactorCandidatePanelProps {
  projectId: string;
  planId?: string;
  candidates: RefactorCandidate[];
  selectedIds: string[];
  onToggleSelect: (candidateId: string) => void;
  onEdit: (candidate: RefactorCandidate) => void;
}

export function RelayRefactorCandidatePanel({
  projectId,
  planId,
  candidates,
  selectedIds,
  onToggleSelect,
  onEdit,
}: RelayRefactorCandidatePanelProps) {
  const grouped = React.useMemo(() => groupCandidates(candidates), [candidates]);
  const selected = new Set(selectedIds);

  return (
    <div className="space-y-6">
      <CandidateBucket title="Ready Candidates" count={grouped.ready.length}>
        {grouped.ready.map((candidate) => (
          <ReadyCandidateCard
            key={candidate.candidateId}
            projectId={projectId}
            planId={planId}
            candidate={candidate}
            selected={selected.has(candidate.candidateId)}
            onToggleSelect={onToggleSelect}
            onEdit={onEdit}
          />
        ))}
      </CandidateBucket>

      <CandidateBucket
        title="Scheduled / In Flight"
        count={grouped.scheduled.length}
      >
        {grouped.scheduled.map((candidate) => (
          <ScheduledCandidateCard key={candidate.candidateId} candidate={candidate} />
        ))}
      </CandidateBucket>

      <CandidateBucket title="Completed" count={grouped.completed.length}>
        {grouped.completed.map((candidate) => (
          <SimpleCandidateCard key={candidate.candidateId} candidate={candidate} />
        ))}
      </CandidateBucket>

      <CandidateBucket
        title="Deferred / Rejected / Superseded"
        count={grouped.inactive.length}
      >
        {grouped.inactive.map((candidate) => (
          <InactiveCandidateCard key={candidate.candidateId} candidate={candidate} />
        ))}
      </CandidateBucket>

      {grouped.other.length > 0 ? (
        <CandidateBucket title="Other" count={grouped.other.length}>
          {grouped.other.map((candidate) => (
            <SimpleCandidateCard key={candidate.candidateId} candidate={candidate} />
          ))}
        </CandidateBucket>
      ) : null}
    </div>
  );
}

function CandidateBucket({
  title,
  count,
  children,
}: {
  title: string;
  count: number;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        <Badge variant="outline">{count}</Badge>
      </div>
      {count === 0 ? (
        <p className="text-xs text-muted-foreground">No candidates in this bucket.</p>
      ) : (
        <div className="space-y-3">{children}</div>
      )}
    </section>
  );
}

function CandidateHeader({ candidate }: { candidate: RefactorCandidate }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-2">
      <div className="min-w-0 space-y-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-foreground">{candidate.title}</span>
          <Badge variant={statusVariant(candidate.status)}>
            {refactorCandidateStatusLabel(candidate.status)}
          </Badge>
          <Badge variant="outline">risk: {candidate.riskLevel}</Badge>
        </div>
        <p className="font-mono text-[11px] text-muted-foreground">
          ID: {candidate.candidateId}
        </p>
      </div>
    </div>
  );
}

function ReadyCandidateCard({
  projectId,
  planId,
  candidate,
  selected,
  onToggleSelect,
  onEdit,
}: {
  projectId: string;
  planId?: string;
  candidate: RefactorCandidate;
  selected: boolean;
  onToggleSelect: (candidateId: string) => void;
  onEdit: (candidate: RefactorCandidate) => void;
}) {
  const selectable = canSelectCandidateForGeneratedPlan(candidate.status);
  const promotable = canPromoteCandidate(candidate.status);

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 space-y-3">
      <div className="flex items-start gap-3">
        {selectable ? (
          <Checkbox
            className="mt-1"
            checked={selected}
            onCheckedChange={() => onToggleSelect(candidate.candidateId)}
            aria-label={`Select ${candidate.title} for generated plan`}
          />
        ) : null}
        <div className="min-w-0 flex-1">
          <CandidateHeader candidate={candidate} />
          <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
            {candidate.problemSummary}
          </p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" size="xs" onClick={() => onEdit(candidate)}>
          Edit
        </Button>
        <CandidateLifecycleActions projectId={projectId} candidate={candidate} />
      </div>

      {planId && promotable ? (
        <CandidatePromotionControls
          projectId={projectId}
          planId={planId}
          candidate={candidate}
        />
      ) : null}
    </div>
  );
}

function ScheduledCandidateCard({ candidate }: { candidate: RefactorCandidate }) {
  const planId = candidate.metadata?.scheduled_plan_id ?? candidate.metadata?.plan_id;
  const passId = candidate.metadata?.scheduled_pass_id ?? candidate.metadata?.pass_id;

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 space-y-2">
      <CandidateHeader candidate={candidate} />
      {planId && passId ? (
        <Button asChild variant="outline" size="xs">
          <Link
            to="/plans/$planId/passes/$passId"
            params={{ planId, passId }}
          >
            View scheduled pass {passId}
          </Link>
        </Button>
      ) : planId ? (
        <Button asChild variant="outline" size="xs">
          <Link to="/plans/$planId" params={{ planId }}>
            View plan {planId}
          </Link>
        </Button>
      ) : (
        <p className="text-xs text-muted-foreground">
          Scheduling reference details are not available on this record.
        </p>
      )}
    </div>
  );
}

function SimpleCandidateCard({ candidate }: { candidate: RefactorCandidate }) {
  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4">
      <CandidateHeader candidate={candidate} />
    </div>
  );
}

function InactiveCandidateCard({ candidate }: { candidate: RefactorCandidate }) {
  const reason =
    candidate.deferReason ||
    candidate.rejectReason ||
    candidate.supersedeReason ||
    "";

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 space-y-2">
      <CandidateHeader candidate={candidate} />
      {reason ? (
        <p className="text-xs text-muted-foreground">
          <span className="font-semibold">Reason:</span> {reason}
        </p>
      ) : null}
      {candidate.supersededByCandidateId ? (
        <p className="font-mono text-[11px] text-muted-foreground">
          Superseded by: {candidate.supersededByCandidateId}
        </p>
      ) : null}
    </div>
  );
}

type LifecycleAction = "defer" | "reject" | "supersede";

function CandidateLifecycleActions({
  projectId,
  candidate,
}: {
  projectId: string;
  candidate: RefactorCandidate;
}) {
  const queryClient = useQueryClient();
  const [action, setAction] = React.useState<LifecycleAction | null>(null);
  const [reason, setReason] = React.useState("");
  const [supersededBy, setSupersededBy] = React.useState("");
  const [issues, setIssues] = React.useState<RefactorValidationIssue[]>([]);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const reset = () => {
    setAction(null);
    setReason("");
    setSupersededBy("");
    setIssues([]);
    setErrorMsg(null);
  };

  const mutation = useMutation({
    mutationFn: () => {
      if (action === "defer") {
        return deferRefactorCandidate(projectId, candidate.candidateId, {
          defer_reason: reason.trim() || undefined,
        });
      }
      if (action === "reject") {
        return rejectRefactorCandidate(projectId, candidate.candidateId, {
          reject_reason: reason.trim() || undefined,
        });
      }
      return supersedeRefactorCandidate(projectId, candidate.candidateId, {
        supersede_reason: reason.trim() || undefined,
        superseded_by_candidate_id: supersededBy.trim() || undefined,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: relayRefactorKeys.project(projectId),
      });
      reset();
    },
    onError: (err: unknown) => {
      setIssues(extractRefactorValidationIssues(err));
      setErrorMsg(err instanceof Error ? err.message : "Lifecycle action failed");
    },
  });

  if (!action) {
    return (
      <>
        <Button variant="outline" size="xs" onClick={() => setAction("defer")}>
          Defer
        </Button>
        <Button variant="outline" size="xs" onClick={() => setAction("reject")}>
          Reject
        </Button>
        <Button variant="outline" size="xs" onClick={() => setAction("supersede")}>
          Supersede
        </Button>
      </>
    );
  }

  return (
    <div className="w-full space-y-2 rounded border border-[var(--relay-row-border)] bg-[var(--relay-page-body-bg)] p-3">
      <p className={labelClass}>{action} candidate</p>
      <RelayRefactorValidationIssues issues={issues} message={errorMsg} />
      <div className="space-y-1.5">
        <Label className={labelClass} htmlFor={`reason-${candidate.candidateId}`}>
          Reason
        </Label>
        <Textarea
          id={`reason-${candidate.candidateId}`}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          className="min-h-14"
        />
      </div>
      {action === "supersede" ? (
        <div className="space-y-1.5">
          <Label className={labelClass} htmlFor={`superseded-by-${candidate.candidateId}`}>
            Superseded by candidate ID
          </Label>
          <Input
            id={`superseded-by-${candidate.candidateId}`}
            value={supersededBy}
            onChange={(e) => setSupersededBy(e.target.value)}
          />
        </div>
      ) : null}
      <div className="flex items-center justify-end gap-2">
        <Button variant="ghost" size="xs" onClick={reset}>
          Cancel
        </Button>
        <Button
          size="xs"
          onClick={() => {
            setIssues([]);
            setErrorMsg(null);
            mutation.mutate();
          }}
          disabled={mutation.isPending}
        >
          {mutation.isPending ? "Working…" : `Confirm ${action}`}
        </Button>
      </div>
    </div>
  );
}

function CandidatePromotionControls({
  projectId,
  planId,
  candidate,
}: {
  projectId: string;
  planId: string;
  candidate: RefactorCandidate;
}) {
  const queryClient = useQueryClient();
  const [afterPassId, setAfterPassId] = React.useState("");
  const [useSuggested, setUseSuggested] = React.useState(false);
  const [note, setNote] = React.useState("");
  const [suggestionSeen, setSuggestionSeen] = React.useState(false);
  const [issues, setIssues] = React.useState<RefactorValidationIssue[]>([]);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const placementQuery = useQuery(
    refactorPlacementSuggestionQueryOptions(projectId, candidate.candidateId, planId),
  );
  const suggestion = placementQuery.data;

  const promoteMutation = useMutation({
    mutationFn: () =>
      promoteRefactorCandidate(projectId, candidate.candidateId, {
        plan_id: planId,
        after_pass_id: afterPassId.trim() || undefined,
        use_suggested_placement: useSuggested,
        note: note.trim() || undefined,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: relayRefactorKeys.project(projectId),
      });
      void queryClient.invalidateQueries({
        queryKey: relayPlanKeys.detail(planId),
      });
    },
    onError: (err: unknown) => {
      setIssues(extractRefactorValidationIssues(err));
      setErrorMsg(err instanceof Error ? err.message : "Promotion failed");
    },
  });

  const result = promoteMutation.data;

  // Promotion requires the user to have either seen a placement suggestion or
  // manually entered an after_pass_id (or explicitly confirmed append via the
  // suggested-placement toggle after a suggestion is shown).
  const canPromote =
    suggestionSeen || afterPassId.trim().length > 0 || (useSuggested && !!suggestion);

  const handleSuggest = async () => {
    setIssues([]);
    setErrorMsg(null);
    const res = await placementQuery.refetch();
    if (res.data) {
      setSuggestionSeen(true);
      if (res.data.afterPassId) {
        setAfterPassId(res.data.afterPassId);
      }
    }
  };

  return (
    <div className="space-y-3 rounded border border-info/25 bg-info/5 p-3">
      <p className={labelClass}>Promote to plan {planId}</p>

      <RelayRefactorValidationIssues issues={issues} message={errorMsg} />

      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="xs"
          onClick={handleSuggest}
          disabled={placementQuery.isFetching}
        >
          {placementQuery.isFetching ? "Suggesting…" : "Suggest placement"}
        </Button>
      </div>

      {suggestion ? (
        <div className="space-y-1 rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 text-xs">
          <p>
            <span className="font-semibold">Reason:</span> {suggestion.placementReason}{" "}
            <span className="text-muted-foreground">({suggestion.confidence})</span>
          </p>
          {suggestion.afterPassId ? (
            <p>
              <span className="font-semibold">After pass:</span>{" "}
              <span className="font-mono">{suggestion.afterPassId}</span>
            </p>
          ) : (
            <p className="text-muted-foreground">No deterministic placement suggestion.</p>
          )}
          {suggestion.matchedPassIds.length > 0 ? (
            <p className="font-mono text-[11px] text-muted-foreground">
              Matched passes: {suggestion.matchedPassIds.join(", ")}
            </p>
          ) : null}
          {suggestion.matchedPaths.length > 0 ? (
            <p className="font-mono text-[11px] text-muted-foreground">
              Matched paths: {suggestion.matchedPaths.join(", ")}
            </p>
          ) : null}
          {suggestion.warnings.map((warning, index) => (
            <p key={index} className="text-warning">
              {warning}
            </p>
          ))}
        </div>
      ) : null}

      <div className="space-y-1.5">
        <Label className={labelClass} htmlFor={`after-pass-${candidate.candidateId}`}>
          After pass ID
        </Label>
        <Input
          id={`after-pass-${candidate.candidateId}`}
          value={afterPassId}
          onChange={(e) => setAfterPassId(e.target.value)}
          placeholder="Leave blank to append after the last pass"
        />
      </div>

      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <Checkbox
          checked={useSuggested}
          onCheckedChange={(checked) => {
            const next = checked === true;
            setUseSuggested(next);
            if (next && suggestion?.afterPassId) {
              setAfterPassId(suggestion.afterPassId);
            }
          }}
          disabled={!suggestion}
        />
        Use suggested placement
      </label>

      <div className="space-y-1.5">
        <Label className={labelClass} htmlFor={`note-${candidate.candidateId}`}>
          Note
        </Label>
        <Input
          id={`note-${candidate.candidateId}`}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Optional"
        />
      </div>

      <div className="flex items-center justify-end">
        <Button
          size="xs"
          onClick={() => {
            setIssues([]);
            setErrorMsg(null);
            promoteMutation.mutate();
          }}
          disabled={!canPromote || promoteMutation.isPending}
        >
          {promoteMutation.isPending ? "Promoting…" : "Promote to this plan"}
        </Button>
      </div>

      {result ? (
        <div className="space-y-1 rounded border border-success/30 bg-success/10 p-2 text-xs text-success">
          <p className="font-semibold">Promoted to {result.planId}</p>
          <p>
            Pass {result.passId} · sequence {result.sequence} · status{" "}
            {result.candidateStatus}
          </p>
          {result.placement.placementReason ? (
            <p>Placement: {result.placement.placementReason}</p>
          ) : null}
          {result.warnings.map((warning, index) => (
            <p key={index} className="text-warning">
              {warning}
            </p>
          ))}
          <Button asChild variant="outline" size="xs" className="mt-1">
            <Link
              to="/plans/$planId/passes/$passId"
              params={{ planId: result.planId, passId: result.passId }}
            >
              Open created pass
            </Link>
          </Button>
        </div>
      ) : null}
    </div>
  );
}
