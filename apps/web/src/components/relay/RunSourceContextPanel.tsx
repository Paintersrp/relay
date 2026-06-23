import * as React from "react";
import { Copy, ExternalLink, FileSearch } from "lucide-react";

import { RelayStateSurface } from "@/components/relay/RelayStateSurface";
import { Button } from "@/components/ui/button";
import type {
  RelayArtifact,
  RelayRun,
  RelaySourceVisibilitySummary,
} from "@/features/relay-runs";
import {
  RunStageInspectorSection,
  RunStageKeyValueRow,
  RunStageSectionLabel,
} from "./RunStagePrimitives";

type CopyState = "idle" | "copied" | "failed";

const SAFE_CONTEXT_ARTIFACT_KINDS = [
  "planner_handoff_provenance_json",
  "context_packet_json",
  "context_packet_markdown",
  "context_coverage_report_json",
] as const;

function firstArtifactByKind(
  artifacts: RelayArtifact[],
  ...kinds: readonly string[]
): RelayArtifact | undefined {
  return artifacts.find((artifact) => {
    const kind = artifact.storageKind ?? artifact.kind;
    return kinds.includes(kind);
  });
}

function truncateMiddle(value: string, visible = 10): string {
  if (value.length <= visible * 2 + 1) {
    return value;
  }

  return `${value.slice(0, visible)}…${value.slice(-visible)}`;
}

function copyText(value: string, onStateChange: (state: CopyState) => void) {
  void (async () => {
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }

      await navigator.clipboard.writeText(value);
      onStateChange("copied");
    } catch {
      onStateChange("failed");
    }
  })();
}

function CopyValueButton({
  label,
  value,
}: {
  label: string;
  value?: string;
}) {
  const [copyState, setCopyState] = React.useState<CopyState>("idle");

  if (!value) {
    return null;
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="h-6 gap-1 rounded-sm px-1.5 text-[10px] text-muted-foreground"
      onClick={() => copyText(value, setCopyState)}
      title={`Copy ${label}`}
    >
      <Copy className="size-3" />
      {copyState !== "idle" ? (
        <span aria-live="polite">
          {copyState === "copied" ? "Copied" : "Copy failed"}
        </span>
      ) : (
        <span className="sr-only">Copy {label}</span>
      )}
    </Button>
  );
}

function ArtifactLink({ artifact }: { artifact: RelayArtifact }) {
  const href = artifact.contentUrl ?? artifact.path;

  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1 rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-2 py-1 text-[11px] text-foreground transition-colors hover:bg-[var(--relay-panel-hover-bg)]"
    >
      <ExternalLink className="size-3 text-muted-foreground" />
      <span>{artifact.label || artifact.filename}</span>
    </a>
  );
}

export function buildRunSourceVisibilitySummary(
  run: RelayRun,
  artifacts: RelayArtifact[],
): RelaySourceVisibilitySummary {
  const provenance = run.provenance;
  const planContext = run.planContext;
  const sourceContext = run.sourceContext;
  const warnings: string[] = [];
  const blockers: string[] = [];

  const provenanceArtifact = firstArtifactByKind(
    artifacts,
    "planner_handoff_provenance_json",
  );
  const contextPacketArtifact = firstArtifactByKind(artifacts, "context_packet_json");
  const coverageReportArtifact = firstArtifactByKind(
    artifacts,
    "context_coverage_report_json",
  );

  const summary: RelaySourceVisibilitySummary = {
    plannerHandoffSha256:
      provenance?.plannerHandoffSha256 ?? planContext?.plannerHandoffSha256,
    sourceArtifactPath:
      provenance?.sourceArtifactPath ?? planContext?.sourceArtifactPath,
    contextPacketId:
      sourceContext?.contextPacketId ??
      provenance?.contextPacketId ??
      planContext?.contextPacketId,
    sourceSnapshotId:
      sourceContext?.sourceSnapshotId ??
      provenance?.sourceSnapshotId ??
      planContext?.sourceSnapshotId,
    coverageReportPath: sourceContext?.coverageReportPath,
    provenanceArtifact,
    contextPacketArtifact,
    coverageReportArtifact,
    blockers,
    warnings,
  };

  if (summary.contextPacketId && !contextPacketArtifact) {
    warnings.push("Context packet metadata exists, but no safe context packet artifact is attached.");
  }

  if (summary.plannerHandoffSha256 && !provenanceArtifact) {
    warnings.push("Run provenance metadata exists, but the provenance artifact is missing.");
  }

  if (
    !summary.plannerHandoffSha256 &&
    !summary.sourceArtifactPath &&
    !summary.contextPacketId &&
    !summary.sourceSnapshotId &&
    SAFE_CONTEXT_ARTIFACT_KINDS.every(
      (kind) => !firstArtifactByKind(artifacts, kind),
    )
  ) {
    warnings.push("No submission provenance or source-context metadata is stored for this run.");
  }

  return summary;
}

export function RunSourceContextPanel({
  run,
  artifacts,
}: {
  run: RelayRun;
  artifacts: RelayArtifact[];
}) {
  const summary = React.useMemo(
    () => buildRunSourceVisibilitySummary(run, artifacts),
    [artifacts, run],
  );
  const markdownArtifact = firstArtifactByKind(artifacts, "context_packet_markdown");
  const readinessLabel =
    summary.contextPacketId && summary.sourceSnapshotId
      ? "Context ready"
      : summary.contextPacketId || summary.sourceSnapshotId
        ? "Context missing"
        : "No source context recorded";
  const hasSummaryData = Boolean(
    summary.plannerHandoffSha256 ||
      summary.sourceArtifactPath ||
      summary.contextPacketId ||
      summary.sourceSnapshotId ||
      summary.provenanceArtifact ||
      summary.contextPacketArtifact ||
      summary.coverageReportArtifact ||
      markdownArtifact,
  );

  if (!hasSummaryData) {
    return (
      <RelayStateSurface
        tone="empty"
        title="No source context recorded"
        description="This run does not currently include persisted planner provenance, source snapshot metadata, or context packet visibility."
        metadata="Standalone and older runs can legitimately have no source context."
      />
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <RunStageInspectorSection
        title={readinessLabel}
        description="Read-only source grounding and provenance for this run."
        actions={<FileSearch className="size-4 text-muted-foreground" />}
      >
        <dl>
          <RunStageKeyValueRow
            label="Plan"
            value={run.planContext?.planTitle ?? run.planContext?.planId}
          >
            <CopyValueButton label="plan ID" value={run.planContext?.planId} />
          </RunStageKeyValueRow>
          <RunStageKeyValueRow
            label="Pass"
            value={run.planContext?.passName ?? run.planContext?.passId}
          >
            <CopyValueButton label="pass ID" value={run.planContext?.passId} />
          </RunStageKeyValueRow>
          <RunStageKeyValueRow
            label="Handoff SHA"
            value={
              summary.plannerHandoffSha256
                ? truncateMiddle(summary.plannerHandoffSha256, 12)
                : undefined
            }
            mono
          >
            <CopyValueButton
              label="handoff SHA-256"
              value={summary.plannerHandoffSha256}
            />
          </RunStageKeyValueRow>
          <RunStageKeyValueRow
            label="Artifact"
            value={summary.sourceArtifactPath}
            mono
          />
          <RunStageKeyValueRow
            label="Context Packet"
            value={summary.contextPacketId}
            mono
          />
          <RunStageKeyValueRow
            label="Snapshot"
            value={summary.sourceSnapshotId}
            mono
          />
          <RunStageKeyValueRow
            label="Coverage"
            value={summary.coverageReportPath}
            mono
          />
        </dl>
      </RunStageInspectorSection>

      <RunStageInspectorSection
        title="Safe Artifacts"
        description="Persisted metadata artifacts only. No raw project source content is shown here."
      >
        <div className="flex flex-wrap gap-2">
          {summary.provenanceArtifact ? (
            <ArtifactLink artifact={summary.provenanceArtifact} />
          ) : null}
          {summary.contextPacketArtifact ? (
            <ArtifactLink artifact={summary.contextPacketArtifact} />
          ) : null}
          {markdownArtifact ? <ArtifactLink artifact={markdownArtifact} /> : null}
          {summary.coverageReportArtifact ? (
            <ArtifactLink artifact={summary.coverageReportArtifact} />
          ) : null}
          {!summary.provenanceArtifact &&
          !summary.contextPacketArtifact &&
          !markdownArtifact &&
          !summary.coverageReportArtifact ? (
            <span className="text-xs text-muted-foreground">
              No safe source-context artifacts are attached to this run yet.
            </span>
          ) : null}
        </div>
      </RunStageInspectorSection>

      {summary.warnings?.length ? (
        <RunStageInspectorSection
          title="Warnings"
          description="Relay found partial source-context metadata for this run."
        >
          <div className="space-y-1 text-xs text-muted-foreground">
            {summary.warnings.map((warning) => (
              <div key={warning}>{warning}</div>
            ))}
          </div>
        </RunStageInspectorSection>
      ) : null}

      {summary.blockers?.length ? (
        <RunStageInspectorSection title="Blockers">
          <div className="space-y-1 text-xs text-destructive">
            {summary.blockers.map((blocker) => (
              <div key={blocker}>{blocker}</div>
            ))}
          </div>
        </RunStageInspectorSection>
      ) : null}

      <div className="px-1">
        <RunStageSectionLabel>Visibility Boundary</RunStageSectionLabel>
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
          Relay shows IDs, hashes, associations, and links to existing safe
          artifacts only. It does not render raw repository file contents in this
          panel.
        </p>
      </div>
    </div>
  );
}
