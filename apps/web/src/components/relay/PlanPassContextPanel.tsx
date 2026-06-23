import { ExternalLink } from "lucide-react";

import { RelayStateBanner } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import type { PlanAPIPass } from "@/features/relay-plans";

interface PlanPassContextSummary {
  requiredRepositoryCount: number;
  seedSearchCount: number;
  seedFileCount: number;
  readinessCriteriaCount: number;
  blockedIfMissingCount: number;
}

function formatBooleanRequirement(value?: boolean): string {
  if (value === true) {
    return "Required";
  }

  if (value === false) {
    return "Optional";
  }

  return "Not specified";
}

function renderTokenList(values: string[], empty: string, tone = "default") {
  if (values.length === 0) {
    return <p className="text-xs text-muted-foreground">{empty}</p>;
  }

  const className =
    tone === "warning"
      ? "rounded-sm border border-destructive/25 bg-destructive/10 px-2 py-1 text-xs text-destructive"
      : "rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-2 py-1 text-xs text-foreground";

  return (
    <div className="flex flex-wrap gap-1.5">
      {values.map((value) => (
        <span key={value} className={className}>
          {value}
        </span>
      ))}
    </div>
  );
}

function renderMetric(label: string, value: string | number | undefined) {
  return (
    <div className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2">
      <div className="text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 text-sm font-medium text-foreground">
        {value ?? "Not specified"}
      </div>
    </div>
  );
}

export function summarizePlanPassContext(pass: PlanAPIPass): PlanPassContextSummary {
  return {
    requiredRepositoryCount: pass.contextPlan?.requiredRepositories.length ?? 0,
    seedSearchCount: pass.contextPlan?.seedSearchTerms.length ?? 0,
    seedFileCount: pass.contextPlan?.seedFilesToRead.length ?? 0,
    readinessCriteriaCount: pass.handoffReadinessCriteria?.length ?? 0,
    blockedIfMissingCount: pass.contextPlan?.blockedIfMissing.length ?? 0,
  };
}

export function PlanPassContextPanel({ pass }: { pass: PlanAPIPass }) {
  const summary = summarizePlanPassContext(pass);
  const hasContextFields =
    Boolean(pass.passType) ||
    Boolean(pass.riskLevel) ||
    summary.requiredRepositoryCount > 0 ||
    summary.seedSearchCount > 0 ||
    summary.seedFileCount > 0 ||
    summary.readinessCriteriaCount > 0 ||
    summary.blockedIfMissingCount > 0 ||
    Boolean(
      pass.sourceSnapshotRequirements?.requireGitStatus !== undefined ||
        pass.sourceSnapshotRequirements?.requireCommitSha !== undefined ||
        pass.sourceSnapshotRequirements?.allowDirtyWorktree !== undefined,
    ) ||
    Boolean(
      pass.contextBudget?.maxFiles ||
        pass.contextBudget?.maxBytes ||
        pass.contextBudget?.maxSearchResults ||
        pass.contextBudget?.maxContextLines,
    );

  return (
    <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="border-b border-[var(--relay-row-border)] px-5 py-2.5">
        <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
          Source & Context
        </span>
      </div>

      {!hasContextFields ? (
        <div className="px-5 py-4">
          <RelayStateBanner
            tone="empty"
            density="compact"
            title="No pass context metadata"
            description="This pass does not currently include Plan v2 context plan, source snapshot, or readiness metadata."
          />
        </div>
      ) : (
        <div className="space-y-5 px-5 py-4">
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {renderMetric("Pass Type", pass.passType)}
            {renderMetric("Risk", pass.riskLevel)}
            {renderMetric(
              "Readiness",
              summary.readinessCriteriaCount
                ? `${summary.readinessCriteriaCount} criteria`
                : "None",
            )}
            {renderMetric(
              "Context Budget",
              pass.contextBudget?.maxFiles || pass.contextBudget?.maxBytes
                ? "Configured"
                : "Default",
            )}
          </div>

          <div>
            <div className="mb-2 text-[11px] text-muted-foreground">
              Required repositories
            </div>
            {renderTokenList(
              pass.contextPlan?.requiredRepositories ?? [],
              "No required repositories listed.",
            )}
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <div>
              <div className="mb-2 text-[11px] text-muted-foreground">
                Seed searches
              </div>
              {pass.contextPlan?.seedSearchTerms?.length ? (
                <div className="space-y-2">
                  {pass.contextPlan.seedSearchTerms.map((term) => (
                    <div
                      key={`${term.repoId}:${term.query}`}
                      className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-[11px] text-foreground">
                          {term.repoId}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {formatBooleanRequirement(term.required)}
                        </span>
                      </div>
                      <div className="mt-1 text-sm text-foreground">{term.query}</div>
                      {term.purpose ? (
                        <div className="mt-1 text-xs text-muted-foreground">
                          {term.purpose}
                        </div>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">
                  No seed searches listed.
                </p>
              )}
            </div>

            <div>
              <div className="mb-2 text-[11px] text-muted-foreground">
                Seed files to read
              </div>
              {pass.contextPlan?.seedFilesToRead?.length ? (
                <div className="space-y-2">
                  {pass.contextPlan.seedFilesToRead.map((file) => (
                    <div
                      key={`${file.repoId}:${file.path}`}
                      className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-[11px] text-foreground">
                          {file.repoId}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {formatBooleanRequirement(file.required)}
                        </span>
                      </div>
                      <div className="mt-1 break-all font-mono text-[11px] text-foreground">
                        {file.path}
                      </div>
                      {file.purpose ? (
                        <div className="mt-1 text-xs text-muted-foreground">
                          {file.purpose}
                        </div>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">
                  No seed file reads listed.
                </p>
              )}
            </div>
          </div>

          <div>
            <div className="mb-2 text-[11px] text-muted-foreground">
              Coverage expectations
            </div>
            {renderTokenList(
              pass.contextPlan?.contextCoverageExpectations ?? [],
              "No coverage expectations listed.",
            )}
          </div>

          <div>
            <div className="mb-2 text-[11px] text-muted-foreground">
              Blocked if missing
            </div>
            {renderTokenList(
              pass.contextPlan?.blockedIfMissing ?? [],
              "No blocked-if-missing entries listed.",
              "warning",
            )}
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <div className="space-y-2">
              <div className="text-[11px] text-muted-foreground">
                Source snapshot requirements
              </div>
              <div className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2 text-xs text-foreground">
                <div>Git status: {formatBooleanRequirement(pass.sourceSnapshotRequirements?.requireGitStatus)}</div>
                <div>Commit SHA: {formatBooleanRequirement(pass.sourceSnapshotRequirements?.requireCommitSha)}</div>
                <div>
                  Dirty worktree: {formatBooleanRequirement(pass.sourceSnapshotRequirements?.allowDirtyWorktree)}
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <div className="text-[11px] text-muted-foreground">
                Context budget
              </div>
              <div className="rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2 text-xs text-foreground">
                <div>Max files: {pass.contextBudget?.maxFiles ?? "Not specified"}</div>
                <div>Max bytes: {pass.contextBudget?.maxBytes ?? "Not specified"}</div>
                <div>
                  Max search results: {pass.contextBudget?.maxSearchResults ?? "Not specified"}
                </div>
                <div>
                  Max context lines: {pass.contextBudget?.maxContextLines ?? "Not specified"}
                </div>
              </div>
            </div>
          </div>

          <div>
            <div className="mb-2 text-[11px] text-muted-foreground">
              Handoff readiness criteria
            </div>
            {renderTokenList(
              pass.handoffReadinessCriteria ?? [],
              "No handoff readiness criteria listed.",
            )}
          </div>

          {pass.contextParseWarnings?.length ? (
            <div>
              <div className="mb-2 text-[11px] text-muted-foreground">
                Parse warnings
              </div>
              <div className="space-y-2">
                {pass.contextParseWarnings.map((warning) => (
                  <div
                    key={warning}
                    className="rounded-sm border border-[var(--warning)]/25 bg-[var(--warning)]/10 px-3 py-2 text-xs text-[var(--warning)]"
                  >
                    {warning}
                  </div>
                ))}
              </div>
            </div>
          ) : null}

          {pass.associatedRuns?.length ? (
            <div>
              <div className="mb-2 text-[11px] text-muted-foreground">
                Associated runs
              </div>
              <div className="flex flex-wrap gap-2">
                {pass.associatedRuns.map((run) => (
                  <Button
                    key={run.id}
                    asChild
                    variant="outline"
                    size="xs"
                    className="rounded-sm px-3 text-[11px]"
                  >
                    <a href={run.workbenchPath}>
                      <ExternalLink className="size-3" />
                      {run.id}
                    </a>
                  </Button>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}
