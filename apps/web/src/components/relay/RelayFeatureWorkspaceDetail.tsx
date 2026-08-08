import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";

import { Button } from "@/components/ui/button";
import { RelayApiError } from "@/features/workflow-api";
import { actOnGuidedFeatureWorkspace, featureWorkspaceKeys, type GuidedFeatureAction, type GuidedFeatureDetail } from "@/features/relay-feature-workspaces";

function actionLabel(action: GuidedFeatureAction): string {
  switch (action) {
    case "continue_discovery": return "Continue discovery";
    case "close_discovery": return "Close discovery";
    case "complete_feature": return "Complete feature";
    case "completion_recorded": return "Completion recorded";
  }
}

function errorMessage(error: unknown): string {
  if (!(error instanceof RelayApiError)) return error instanceof Error ? error.message : "Workspace action failed.";
  switch (error.errorShape?.error) {
    case "VERSION_CONFLICT": return "This workspace changed in another session. The latest guided state has been reloaded; review it before trying again.";
    case "CURRENTNESS_BLOCKED": return "This action is blocked because the workspace is not current. Review the planning and diagnostics guidance.";
    case "GUIDED_ACTION_BLOCKED": return "The guided action is currently blocked. Review the latest workspace projection.";
    case "COMPLETION_CONFLICT": return "Completion changed before this action finished. Review the latest completion gates.";
    default: return error.message;
  }
}

function ProjectionList({ items, empty = "None recorded." }: { items: string[]; empty?: string }) {
  return items.length ? <ul className="mt-2 list-disc space-y-1 pl-5 text-sm">{items.map((item, index) => <li key={`${item}-${index}`}>{item}</li>)}</ul> : <p className="mt-2 text-sm text-muted-foreground">{empty}</p>;
}

export function RelayFeatureWorkspaceDetail({ detail }: { detail: GuidedFeatureDetail }) {
  const queryClient = useQueryClient();
  const [confirmed, setConfirmed] = React.useState(false);
  const [message, setMessage] = React.useState<string | null>(null);
  const workspaceId = detail.workspace.workspaceId;
  const primary = detail.availableActions.find((action) => action.primary) ?? {
    action: detail.primaryAction,
    primary: true,
    enabled: false,
    requiresConfirmation: true,
  };
  const mutation = useMutation({
    mutationFn: () => actOnGuidedFeatureWorkspace(workspaceId, {
      expectedVersion: detail.workspace.version,
      action: primary.action,
      confirmation: confirmed,
      destination: detail.discovery.destination || undefined,
    }),
    onSuccess: (next) => {
      queryClient.setQueryData(featureWorkspaceKeys.guided(workspaceId), next);
      setConfirmed(false);
      setMessage(null);
    },
    onError: (error) => {
      setMessage(errorMessage(error));
      if (error instanceof RelayApiError && (error.status === 409 || ["CURRENTNESS_BLOCKED", "GUIDED_ACTION_BLOCKED", "COMPLETION_CONFLICT"].includes(error.errorShape?.error ?? ""))) {
        void queryClient.invalidateQueries({ queryKey: featureWorkspaceKeys.guided(workspaceId) });
      }
    },
  });
  const actionEnabled = primary.enabled;
  const actionDisabledReason = primary.enabled ? "Confirm this action before continuing." : "This action is blocked by the current guided workspace state.";

  return <div className="space-y-6">
    <div className="flex items-center gap-2">
      <Link to="/projects/$projectId" params={{ projectId: detail.project.projectId }} className="inline-flex items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground">
        <ArrowLeft className="size-4" aria-hidden="true" />
        <span>Back to {detail.project.name || "Project"}</span>
      </Link>
    </div>

    <section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Guided feature workspace</p>
      <div className="mt-2 flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{detail.workspace.featureSlug}</h1>
        <span className="rounded bg-muted px-2 py-1 text-xs">{detail.workspace.state}</span>
        <span className="text-sm text-muted-foreground">Version {detail.workspace.version}</span>
      </div>
      <p className="mt-1 text-sm text-muted-foreground">Project: <Link to="/projects/$projectId" params={{ projectId: detail.project.projectId }} className="font-medium text-foreground underline-offset-2 hover:underline">{detail.project.name || detail.project.projectId}</Link></p>
      {message ? <div role="alert" className="mt-4 rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{message}</div> : null}
    </section>

    <section aria-labelledby="guided-discovery" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-discovery" className="font-semibold">Discovery</h2>
      <dl className="mt-3 grid gap-3 sm:grid-cols-2">
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">State</dt><dd className="mt-1 text-sm">{detail.discovery.state}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Destination</dt><dd className="mt-1 text-sm">{detail.discovery.destination || "Not selected"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Currentness</dt><dd className="mt-1 text-sm">{detail.discovery.currentness || "Not available"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Continuation</dt><dd className="mt-1 text-sm">{detail.discovery.continuation || "No continuation guidance recorded."}</dd></div>
      </dl>
      <div className="mt-4 rounded border p-3 text-sm"><span className="font-medium">Rationale: </span>{detail.discovery.rationale || "No rationale recorded."}</div>
    </section>

    <section aria-labelledby="guided-authority" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-authority" className="font-semibold">Authority</h2>
      <p className="mt-1 text-sm text-muted-foreground">Current revision: {detail.authority.currentRevisionNumber || "None"}</p>
      <div className="mt-3 space-y-2">{detail.authority.revisions.length ? detail.authority.revisions.map((revision) => <div key={revision.revisionNumber} className="rounded border p-3 text-sm"><span className="font-medium">Revision {revision.revisionNumber}</span><span className="ml-2 text-muted-foreground">{revision.historical ? "historical" : "current"}</span><p className="mt-1 text-muted-foreground">Layers: {revision.layers.length ? revision.layers.join(", ") : "None"}</p></div>) : <p className="text-sm text-muted-foreground">No authority revisions recorded.</p>}</div>
    </section>

    <section aria-labelledby="guided-planning" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-planning" className="font-semibold">Planning and currentness</h2>
      <dl className="mt-3 grid gap-3 sm:grid-cols-3">
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Readiness</dt><dd className="mt-1 text-sm">{detail.planning.readiness || "Not available"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Status</dt><dd className="mt-1 text-sm">{detail.planning.status || "Not available"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Recovery</dt><dd className="mt-1 text-sm">{detail.planning.recoveryCategory || "None"}</dd></div>
      </dl>
    </section>

    <section aria-labelledby="guided-completion" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-completion" className="font-semibold">Completion</h2>
      <p className="mt-1 text-sm text-muted-foreground">{detail.completion.recorded ? "Completion has been recorded." : detail.completion.ready ? "All completion gates are ready." : "Completion remains blocked by one or more gates."}</p>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">{detail.completion.gates.map((gate) => <div key={gate.name} className="rounded border p-3 text-sm"><span className="font-medium">{gate.name}</span><span className={gate.ready ? "ml-2 text-success" : "ml-2 text-destructive"}>{gate.ready ? "ready" : "blocked"}</span></div>)}</div>
    </section>

    <section aria-labelledby="guided-action" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-action" className="font-semibold">Next guided action</h2>
      <p className="mt-1 text-sm text-muted-foreground">The server selected one primary action for this workspace.</p>
      <label className="mt-4 flex items-start gap-2 text-sm"><input aria-label="Confirm guided action" type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} /><span>I confirm that I want to invoke “{actionLabel(primary.action)}” against this displayed workspace state.</span></label>
      {!actionEnabled ? <p role="status" className="mt-3 text-sm text-muted-foreground">{actionDisabledReason}</p> : null}
      <Button className="mt-4" type="button" disabled={!actionEnabled || !confirmed || mutation.isPending} onClick={() => mutation.mutate()}>{mutation.isPending ? "Applying…" : actionLabel(primary.action)}</Button>
    </section>

    <details className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <summary className="cursor-pointer font-semibold">Diagnostics</summary>
      <div className="mt-4 space-y-5 text-sm">
        <div><h3 className="font-medium">History</h3><p className="mt-1 text-muted-foreground">Currentness: {detail.diagnostics.history.discoveryCurrentness || "None"}</p><p className="text-muted-foreground">Historical identity: {detail.diagnostics.history.historicalIdentity || "None"}</p></div>
        <div><h3 className="font-medium">Stale state</h3><dl className="mt-2 grid gap-2 sm:grid-cols-2">{Object.entries(detail.diagnostics.stale).map(([name, value]) => <div key={name}><dt className="text-xs uppercase tracking-wide text-muted-foreground">{name}</dt><dd>{value || "None"}</dd></div>)}</dl></div>
        <div><h3 className="font-medium">Discovery diagnostics</h3><p className="mt-1 text-muted-foreground">Route material open: {detail.diagnostics.discovery.routeMaterialOpen ? "yes" : "no"}</p><div className="mt-3 grid gap-3 sm:grid-cols-2"><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Blockers</h4><ProjectionList items={detail.diagnostics.discovery.blockers} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Restoration actions</h4><ProjectionList items={detail.diagnostics.discovery.restorationActions} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Pending integrations</h4><ProjectionList items={detail.diagnostics.discovery.pendingIntegrations} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Active operations</h4><ProjectionList items={detail.diagnostics.discovery.activeOperations} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Required evidence</h4><ProjectionList items={detail.diagnostics.discovery.requiredEvidence} /></div></div></div>
      </div>
    </details>
  </div>;
}
