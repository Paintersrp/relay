import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  type NextAuditWorkResponse,
  type NextPassWorkResponse,
  type PlanAPIPass,
  type PlanAPIReadPlan,
  nextAuditWorkQueryOptions,
  nextPassWorkQueryOptions,
} from "@/features/relay-plans";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

interface RelayPlanWorkflowPanelProps {
  plan: PlanAPIReadPlan;
  passes: PlanAPIPass[];
}

export function RelayPlanWorkflowPanel({ plan, passes }: RelayPlanWorkflowPanelProps) {
  const [showNextPassWork, setShowNextPassWork] = useState(false);
  const [showNextAuditWork, setShowNextAuditWork] = useState(false);

  const hasProjectId = Boolean(plan.projectId);

  const nextPassWorkQuery = useQuery({
    ...nextPassWorkQueryOptions(plan.projectId || "", plan.planId),
    enabled: false,
  });

  const nextAuditWorkQuery = useQuery({
    ...nextAuditWorkQueryOptions(plan.projectId || "", plan.planId),
    enabled: false,
  });

  const handleContinuePlan = async () => {
    setShowNextPassWork(true);
    setShowNextAuditWork(false);
    await nextPassWorkQuery.refetch();
  };

  const handleAuditReady = async () => {
    setShowNextAuditWork(true);
    setShowNextPassWork(false);
    await nextAuditWorkQuery.refetch();
  };

  const copyPlannerHandoffPrompt = (passId: string) => {
    const prompt = `Start ${passId} for project ${plan.projectId} and plan ${plan.planId}. Use the next-pass work packet shown in Relay as the source for the PASS handoff. Do not submit the run until the handoff is reviewed.`;
    navigator.clipboard.writeText(prompt);
  };

  const copyJSON = (obj: unknown) => {
    navigator.clipboard.writeText(JSON.stringify(obj, null, 2));
  };

  return (
    <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="border-b border-[var(--relay-row-border)] px-5 py-3">
        <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
          Project Workflow
        </h2>
      </div>

      <div className="p-5 space-y-4">
        {/* Project scope strip */}
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Project:</span>
            {plan.projectId ? (
              <Link
                to="/projects/$projectId"
                params={{ projectId: plan.projectId }}
                className="font-mono text-[var(--relay-accent)] hover:underline"
              >
                {plan.projectId}
              </Link>
            ) : (
              <span className="font-mono text-muted-foreground">unavailable</span>
            )}
          </div>
          <span className="text-muted-foreground/40">|</span>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Plan:</span>
            <span className="font-mono">{plan.planId}</span>
          </div>
          <span className="text-muted-foreground/40">|</span>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Status:</span>
            <Badge variant="outline" className="text-xs">
              {plan.status}
            </Badge>
          </div>
          <span className="text-muted-foreground/40">|</span>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Passes:</span>
            <span className="font-mono">{passes.length}</span>
          </div>
        </div>

        {/* Gate strip */}
        <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-3 text-xs text-muted-foreground">
          <div className="font-medium mb-2">Human Approval Gates:</div>
          <ol className="list-decimal list-inside space-y-1 text-xs">
            <li>Planner handoff review</li>
            <li>Packet / brief review</li>
            <li>Executor approval</li>
            <li>Audit decision approval</li>
          </ol>
          <div className="mt-2 text-xs">
            Relay surfaces work packets here. Planner handoffs and audit judgments are created/reviewed outside this panel and require user approval before submission.
          </div>
        </div>

        {!hasProjectId && (
          <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-xs text-destructive">
            <div className="font-medium">Project unavailable</div>
            <div className="mt-1">
              This plan is not associated with a project. Work-packet requests require project scope.
            </div>
          </div>
        )}

        {/* Action buttons */}
        <div className="flex gap-2">
          <Button
            type="button"
            size="sm"
            disabled={!hasProjectId || plan.status !== "active" || nextPassWorkQuery.isFetching}
            onClick={handleContinuePlan}
          >
            {nextPassWorkQuery.isFetching ? "Checking..." : "Continue Plan"}
          </Button>

          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!hasProjectId || plan.status !== "active" || nextAuditWorkQuery.isFetching}
            onClick={handleAuditReady}
          >
            {nextAuditWorkQuery.isFetching ? "Checking..." : "Audit Ready"}
          </Button>
        </div>

        {/* Next Pass Work Result */}
        {showNextPassWork && nextPassWorkQuery.data && (
          <NextPassWorkCard
            data={nextPassWorkQuery.data}
            onCopyPrompt={copyPlannerHandoffPrompt}
            onCopyJSON={copyJSON}
          />
        )}

        {/* Next Audit Work Result */}
        {showNextAuditWork && nextAuditWorkQuery.data && (
          <NextAuditWorkCard data={nextAuditWorkQuery.data} />
        )}
      </div>
    </section>
  );
}

interface NextPassWorkCardProps {
  data: NextPassWorkResponse;
  onCopyPrompt: (passId: string) => void;
  onCopyJSON: (obj: unknown) => void;
}

function NextPassWorkCard({ data, onCopyPrompt, onCopyJSON }: NextPassWorkCardProps) {
  if (!data.ok) {
    return (
      <div className="rounded border border-destructive/50 bg-destructive/10 p-4 space-y-2">
        <div className="font-medium text-sm text-destructive">Continue Plan Blocked</div>
        {data.blockers.map((blocker, index) => (
          <div key={index} className="text-xs space-y-1">
            <div className="font-mono font-medium">{blocker.code}</div>
            <div>{blocker.message}</div>
            {blocker.recoverable && (
              <div className="text-xs text-muted-foreground italic">Recoverable</div>
            )}
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4 space-y-4">
      <div className="font-medium text-sm">Next Pass Work</div>

      {data.selectedPass && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Selected Pass</div>
          <div className="space-y-1 text-xs">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Pass ID:</span>
              <span className="font-mono">{data.selectedPass.passId}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Sequence:</span>
              <span className="font-mono">{data.selectedPass.sequence}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Name:</span>
              <span>{data.selectedPass.name}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Status:</span>
              <Badge variant="outline" className="text-xs">
                {data.selectedPass.status}
              </Badge>
            </div>
            {data.selectedPass.goal && (
              <div className="flex items-start gap-2">
                <span className="text-muted-foreground">Goal:</span>
                <span className="flex-1">{data.selectedPass.goal}</span>
              </div>
            )}
          </div>
        </div>
      )}

      {data.context && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Context Summary</div>
          <div className="space-y-1 text-xs">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Context Ready:</span>
              <Badge variant={data.context.contextReady ? "success" : "destructive"} className="text-xs">
                {data.context.contextReady ? "Yes" : "No"}
              </Badge>
            </div>
            {data.context.sourceSnapshotId && (
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground">Source Snapshot ID:</span>
                <span className="font-mono">{data.context.sourceSnapshotId}</span>
              </div>
            )}
            {data.context.contextPacketId && (
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground">Context Packet ID:</span>
                <span className="font-mono">{data.context.contextPacketId}</span>
              </div>
            )}
            {data.context.coverageReportPath && (
              <div className="flex items-start gap-2">
                <span className="text-muted-foreground">Coverage Report:</span>
                <span className="font-mono text-xs break-all">{data.context.coverageReportPath}</span>
              </div>
            )}
          </div>
        </div>
      )}

      {data.handoffReadinessCriteria && data.handoffReadinessCriteria.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Handoff Readiness Criteria</div>
          <ul className="list-disc list-inside space-y-1 text-xs">
            {data.handoffReadinessCriteria.map((criterion, index) => (
              <li key={index}>{criterion}</li>
            ))}
          </ul>
        </div>
      )}

      {data.suggestedRunSubmission && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Suggested Run Submission</div>
          <div className="rounded bg-muted p-3 font-mono text-xs overflow-x-auto">
            <pre>{JSON.stringify(data.suggestedRunSubmission, null, 2)}</pre>
          </div>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => onCopyJSON(data.suggestedRunSubmission)}
            >
              Copy JSON
            </Button>
            {data.selectedPass && (
              <Button
                type="button"
                variant="outline"
                size="xs"
                onClick={() => onCopyPrompt(data.selectedPass!.passId)}
              >
                Copy Planner handoff prompt
              </Button>
            )}
          </div>
        </div>
      )}

      {data.associatedRuns && data.associatedRuns.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Associated Runs</div>
          <div className="space-y-2">
            {data.associatedRuns.map((run) => (
              <div
                key={run.runId}
                className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 text-xs"
              >
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Run ID:</span>
                  <Link to={run.workbenchPath || `/runs/${run.runId}`} className="font-mono text-[var(--relay-accent)] hover:underline">
                    {run.runId}
                  </Link>
                </div>
                {run.title && (
                  <div className="flex items-center gap-2 mt-1">
                    <span className="text-muted-foreground">Title:</span>
                    <span>{run.title}</span>
                  </div>
                )}
                <div className="flex items-center gap-2 mt-1">
                  <Badge variant="outline" className="text-xs">{run.lifecycleState}</Badge>
                  <span className="text-muted-foreground/60">•</span>
                  <span className="text-muted-foreground">{run.activeStep}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {data.dependencyStatus && data.dependencyStatus.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Dependency Status</div>
          <div className="space-y-1">
            {data.dependencyStatus.map((dep, index) => (
              <div key={index} className="flex items-center gap-2 text-xs">
                <span className="font-mono">{dep.passId}</span>
                <Badge variant={dep.satisfied ? "success" : "destructive"} className="text-xs">
                  {dep.satisfied ? "Satisfied" : "Not Satisfied"}
                </Badge>
                <span className="text-muted-foreground">{dep.status}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

interface NextAuditWorkCardProps {
  data: NextAuditWorkResponse;
}

function NextAuditWorkCard({ data }: NextAuditWorkCardProps) {
  if (!data.ok) {
    return (
      <div className="rounded border border-destructive/50 bg-destructive/10 p-4 space-y-2">
        <div className="font-medium text-sm text-destructive">Audit Ready Blocked</div>
        {data.blockers.map((blocker, index) => (
          <div key={index} className="text-xs space-y-1">
            <div className="font-mono font-medium">{blocker.code}</div>
            <div>{blocker.message}</div>
            {blocker.recoverable && (
              <div className="text-xs text-muted-foreground italic">Recoverable</div>
            )}
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4 space-y-4">
      <div className="font-medium text-sm">Next Audit Work</div>

      {data.selectedPass && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Selected Pass</div>
          <div className="space-y-1 text-xs">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Pass ID:</span>
              <span className="font-mono">{data.selectedPass.passId}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Name:</span>
              <span>{data.selectedPass.name}</span>
            </div>
          </div>
        </div>
      )}

      {data.selectedRun && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Selected Run</div>
          <div className="space-y-1 text-xs">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Run ID:</span>
              <Link
                to={data.selectedRun.workbenchPath || `/runs/${data.selectedRun.runId}/audit`}
                className="font-mono text-[var(--relay-accent)] hover:underline"
              >
                {data.selectedRun.runId}
              </Link>
            </div>
            {data.selectedRun.title && (
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground">Title:</span>
                <span>{data.selectedRun.title}</span>
              </div>
            )}
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="text-xs">{data.selectedRun.lifecycleState}</Badge>
              <span className="text-muted-foreground/60">•</span>
              <span className="text-muted-foreground">{data.selectedRun.activeStep}</span>
            </div>
          </div>
        </div>
      )}

      {data.allowedDecisions && data.allowedDecisions.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">Allowed Decisions</div>
          <div className="flex flex-wrap gap-2">
            {data.allowedDecisions.map((decision, index) => (
              <Badge key={index} variant="outline" className="text-xs">
                {decision}
              </Badge>
            ))}
          </div>
          <div className="text-xs text-muted-foreground">
            Apply decisions from the run audit workbench only, not from this panel.
          </div>
        </div>
      )}

      {data.selectedRun && (
        <div>
          <a
            href={`/runs/${data.selectedRun.runId}/audit`}
            className="inline-flex items-center gap-2 text-xs text-[var(--relay-accent)] hover:underline"
          >
            Open run audit workbench →
          </a>
        </div>
      )}

      <ArtifactReferencesSection
        title="Executor Results"
        references={data.executorResultReferences}
      />
      <ArtifactReferencesSection
        title="Validation Reports"
        references={data.validationReportReferences}
      />
      <ArtifactReferencesSection
        title="Audit Packets"
        references={data.auditPacketReferences}
      />
      <ArtifactReferencesSection
        title="Diff Evidence"
        references={data.diffEvidenceReferences}
      />
    </div>
  );
}

interface ArtifactReferencesSectionProps {
  title: string;
  references?: Array<{
    kind: string;
    label: string;
    filename: string;
    contentUrl: string;
    status: string;
    createdAt?: string;
  }>;
}

function ArtifactReferencesSection({ title, references }: ArtifactReferencesSectionProps) {
  if (!references || references.length === 0) return null;

  return (
    <div className="space-y-2">
      <div className="text-xs font-medium text-muted-foreground">{title}</div>
      <div className="space-y-2">
        {references.map((artifact, index) => (
          <div
            key={index}
            className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-2 text-xs"
          >
            <div className="flex items-center justify-between">
              <span className="font-medium">{artifact.label}</span>
              <Badge variant="outline" className="text-xs">
                {artifact.status}
              </Badge>
            </div>
            <div className="mt-1 font-mono text-xs text-muted-foreground">
              {artifact.filename}
            </div>
            {artifact.createdAt && (
              <div className="mt-1 text-xs text-muted-foreground">
                {new Date(artifact.createdAt).toLocaleString()}
              </div>
            )}
            <div className="mt-2">
              <a
                href={artifact.contentUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-[var(--relay-accent)] hover:underline"
              >
                View artifact →
              </a>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
