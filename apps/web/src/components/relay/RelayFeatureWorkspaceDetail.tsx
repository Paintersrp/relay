import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";

import { Button } from "@/components/ui/button";
import { RelayApiError } from "@/features/workflow-api";
import { actOnGuidedFeatureWorkspace, featureWorkspaceKeys, type GuidedFeatureAction, type GuidedFeatureDetail, type GuidedIntegrity, type GuidedOperationTransfer } from "@/features/relay-feature-workspaces";

function actionLabel(action: GuidedFeatureAction): string {
  switch (action) {
    case "continue_discovery": return "Continue discovery";
    case "close_discovery": return "Close discovery";
    case "complete_feature": return "Complete feature";
    case "reopen_discovery": return "Reopen discovery";
    case "author_requirements": return "Author Requirements";
    case "author_shared_design": return "Author Shared Design";
    case "author_delivery_ticket": return "Author Delivery Ticket";
    case "review_planning_candidate": return "Review planning candidate";
    case "approve_planning_candidate": return "Approve planning candidate";
    case "promote_planning_candidate": return "Promote planning candidate";
    case "continue_established_route": return "Continue established route";
    case "legacy_recovery": return "Adopt discovery lifecycle";
    case "select_delivery_ticket": return "Select delivery ticket";
    case "author_ticket_design_brief": return "Author Ticket Design Brief";
    case "review_ticket_design_brief": return "Review Ticket Design Brief";
    case "approve_ticket_design_brief": return "Approve Ticket Design Brief";
    case "prepare_package": return "Prepare package";
    case "approve_package": return "Approve package";
    case "launch_run": return "Launch run";
	case "continue_run": return "Continue run";
	case "recover_run": return "Recover run";
    case "prepare_audit": return "Prepare audit";
    case "record_audit_decision": return "Record audit decision";
    case "remediate": return "Remediate";
    case "prototype_execute": return "Execute prototype";
    case "prototype_cleanup": return "Clean up prototype";
    case "prototype_qa": return "Prepare prototype QA";
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

function IntegrityValue({ value }: { value: React.ReactNode }) {
  return <dd className="mt-1 text-sm">{value || "None recorded."}</dd>;
}

function IntegrityField({ label, value }: { label: string; value: React.ReactNode }) {
  return <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</dt><IntegrityValue value={value} /></div>;
}

function IntegritySubHeading({ children }: { children: React.ReactNode }) {
  return <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{children}</h4>;
}

function IntegrityRecord({ name, detail, children }: { name: string; detail: React.ReactNode; children?: React.ReactNode }) {
  return <div className="rounded border p-3 text-sm"><p className="font-medium">{name}</p>{detail ? <p className="mt-1 text-muted-foreground">{detail}</p> : null}{children}</div>;
}

function IntegritySection({ title, records }: { title: string; records: React.ReactNode[] }) {
  if (!records.length) return null;
  return <div><IntegritySubHeading>{title}</IntegritySubHeading><div className="mt-2 space-y-3">{records}</div></div>;
}

function IntegrityDiscoverySection({ discovery }: { discovery: GuidedIntegrity["discovery"] }) {
  const records: React.ReactNode[] = [];
  records.push(
    <IntegrityRecord key="current" name="Current basis" detail={`${discovery.currentRevisionId || "No revision recorded."}${discovery.currentPacket ? `; packet ${discovery.currentPacket.closurePacketId} (${discovery.currentPacket.sha256})` : ""}`}>
      {discovery.currentPacket ? <dl className="mt-2 grid gap-2 sm:grid-cols-2"><IntegrityField label="Closure packet" value={discovery.currentPacket.closurePacketId} /><IntegrityField label="Packet digest" value={discovery.currentPacket.sha256} /></dl> : null}
    </IntegrityRecord>,
  );
  if (discovery.history.length) {
    records.push(
      <div key="history">
        <IntegritySubHeading>Revision history</IntegritySubHeading>
        <div className="mt-2 space-y-2">{discovery.history.map((entry) => <IntegrityRecord key={entry.revisionId} name={`${entry.revisionId} (revision ${entry.revisionNumber})`} detail={entry.historical ? "historical" : "current"}>
          <dl className="mt-2 grid gap-2 sm:grid-cols-2">
            <IntegrityField label="Closure packet" value={entry.closurePacketId || "None"} />
            <IntegrityField label="Packet digest" value={entry.packetSha256 || "None"} />
            <IntegrityField label="Predecessor revision" value={entry.predecessorId || "None"} />
          </dl>
        </IntegrityRecord>)}</div>
      </div>,
    );
  }
  if (discovery.reopenEvents.length) {
    records.push(
      <div key="reopens">
        <IntegritySubHeading>Reopen and replacement linkage</IntegritySubHeading>
        <div className="mt-2 space-y-2">{discovery.reopenEvents.map((event) => <IntegrityRecord key={event.reopenEventId} name={event.reopenEventId} detail={`Reopened ${event.reopenedPacketId || "packet"}; replacement ${event.replacementRevisionId || "revision"}`} />)}</div>
      </div>,
    );
  }
  return <IntegritySection title="Integrity discovery" records={records} />;
}

function IntegrityAuthoritySection({ authority }: { authority: GuidedIntegrity["authority"] }) {
  const records = authority.map((revision) => <IntegrityRecord key={revision.authorityRevisionId} name={revision.authorityRevisionId} detail={`Revision ${revision.revisionNumber}; ${revision.historical ? "historical" : "current"}`}>
    {revision.layers.length ? <dl className="mt-2 space-y-3">{revision.layers.map((layer, index) => <div key={`${revision.authorityRevisionId}-${layer.kind}-${index}`}><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{layer.kind}</dt><dd className="mt-1 text-sm">
      <dl className="grid gap-2 sm:grid-cols-2"><IntegrityField label="Artifact" value={layer.artifactId || "None"} /><IntegrityField label="Digest" value={layer.sha256 || "None"} /><IntegrityField label="Source closure" value={layer.sourceClosureId || "None"} /></dl>
    </dd></div>)}</dl> : null}
  </IntegrityRecord>);
  return <IntegritySection title="Integrity authority" records={records} />;
}

function IntegrityPlanningSection({ planning }: { planning: GuidedIntegrity["planning"] }) {
  const records = planning.map((candidate) => <IntegrityRecord key={candidate.candidateId} name={candidate.candidateId} detail={`${candidate.family}; ${candidate.historical ? "historical" : "current"}; ${candidate.promoted ? "promoted into current authority" : "not promoted into current authority"}`}>
    <dl className="mt-2 grid gap-2 sm:grid-cols-2">
      <IntegrityField label="Artifact" value={candidate.artifactId || "None"} />
      <IntegrityField label="Digest" value={candidate.sha256 || "None"} />
      <IntegrityField label="Size (bytes)" value={candidate.sizeBytes} />
      <IntegrityField label="Approvals" value={candidate.approvals.length ? candidate.approvals.join(", ") : "None"} />
    </dl>
  </IntegrityRecord>);
  return <IntegritySection title="Integrity planning" records={records} />;
}

function IntegrityDeliverySection({ delivery }: { delivery: GuidedIntegrity["delivery"] }) {
  const records: React.ReactNode[] = [];
  if (delivery.frontier.length) {
    records.push(<div key="frontier"><IntegritySubHeading>Delivery Ticket frontier</IntegritySubHeading><dl className="mt-2 grid gap-2 sm:grid-cols-2">{delivery.frontier.map((entry) => <IntegrityField key={entry.ticketId} label={entry.ticketId} value={`Revision ${entry.revisionNumber}`} />)}</dl></div>);
  }
  const basis: Array<[string, React.ReactNode]> = [];
  if (delivery.selection) basis.push(["Selection", `${delivery.selection.selectionId} (${delivery.selection.state || "state not recorded"}; ${delivery.selection.ticketId || "ticket not recorded"} v${delivery.selection.revisionNumber || "not recorded"})`]);
  if (delivery.package) basis.push(["Package", `${delivery.package.packageId} (${delivery.package.sha256})`]);
  if (delivery.package?.approvalId) basis.push(["Package approval", delivery.package.approvalId]);
  if (delivery.run) basis.push(["Run", `${delivery.run.runId} (package ${delivery.run.packageId}; ${delivery.run.repoTarget || "no repo"} @ ${delivery.run.branch || "no branch"}, base ${delivery.run.baseCommit || "none"})`]);
  if (delivery.audit) basis.push(["Audit", `packet ${delivery.audit.auditPacketId}; decision ${delivery.audit.auditDecisionId || "none"}; audited ${delivery.audit.auditedCommit || "none"}`]);
  if (delivery.remediation) basis.push(["Remediation", delivery.remediation.seedIds.join(", ")]);
  records.push(<div key="basis"><IntegritySubHeading>Delivery identities</IntegritySubHeading><dl className="mt-2 grid gap-2 sm:grid-cols-2">{basis.map(([label, value]) => <IntegrityField key={label} label={label} value={value} />)}</dl></div>);
  if (delivery.briefs.length) {
    records.push(<div key="briefs"><IntegritySubHeading>Ticket Design Briefs</IntegritySubHeading><div className="mt-2 space-y-2">{delivery.briefs.map((brief) => <IntegrityRecord key={brief.briefId} name={brief.briefId} detail={`${brief.historical ? "historical" : "current"}; ${brief.status || "status not recorded"}`}>
      <dl className="mt-2 grid gap-2 sm:grid-cols-2">
        <IntegrityField label="Selection binding" value={`${brief.selectionId || "None"} (${brief.selectionState || "state not recorded"})`} />
        <IntegrityField label="Ticket revision" value={`${brief.ticketId || "None"} v${brief.revisionNumber || "not recorded"}`} />
        <IntegrityField label="Canonical filename" value={brief.filename} />
        <IntegrityField label="Digest" value={brief.sha256} />
        <IntegrityField label="Size (bytes)" value={brief.sizeBytes} />
        <IntegrityField label="Review state" value={brief.reviewState} />
        <IntegrityField label="Review disposition" value={brief.reviewDisposition} />
        <IntegrityField label="Review identity" value={brief.reviewId} />
        <IntegrityField label="Approval identity" value={brief.approvalId} />
      </dl>
    </IntegrityRecord>)}</div></div>);
  }
  return <IntegritySection title="Integrity delivery" records={records} />;
}

function IntegrityDiagnostics({ diagnostics }: { diagnostics: GuidedIntegrity["diagnostics"] }) {
  return <div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Integrity inspection diagnostics</h4>{diagnostics.length ? <ul className="mt-2 space-y-1 text-sm">{diagnostics.map((diagnostic, index) => <li key={`${diagnostic.domain}-${diagnostic.condition}-${index}`}><span className="font-medium">{diagnostic.domain}</span>: inspection source <span className="font-medium">{diagnostic.condition}</span>.</li>)}</ul> : <p className="mt-2 text-sm text-muted-foreground">No integrity inspection diagnostics recorded; absent identities are not errors.</p>}</div>;
}

function IntegrityPrototypeSection({ prototype }: { prototype: GuidedIntegrity["prototype"] }) {
  if (!prototype) return null;
  const records: React.ReactNode[] = [
    <div key="basis"><IntegritySubHeading>Prototype execution</IntegritySubHeading><dl className="mt-2 grid gap-2 sm:grid-cols-2">
      <IntegrityField label="Run" value={prototype.runId} />
      <IntegrityField label="Run state" value={prototype.runState} />
      <IntegrityField label="Proposal" value={prototype.proposalId || "None"} />
      <IntegrityField label="Authorization" value={prototype.authorizationId || "None"} />
      <IntegrityField label="Approval" value={prototype.approvalId || "None"} />
      <IntegrityField label="Discovery basis" value={prototype.discoveryRevisionId || "None"} />
    </dl></div>,
  ];
  if (prototype.cleanup.length) {
    records.push(<div key="cleanup"><IntegritySubHeading>Cleanup obligations</IntegritySubHeading><dl className="mt-2 grid gap-2 sm:grid-cols-2">{prototype.cleanup.map((item) => <IntegrityField key={item.cleanupObligationId} label={`${item.kind} (${item.status})`} value={item.cleanupObligationId} />)}</dl></div>);
  }
  if (prototype.qaPackets.length) {
    records.push(<div key="qa"><IntegritySubHeading>QA packets</IntegritySubHeading><div className="mt-2 space-y-2">{prototype.qaPackets.map((packet) => <IntegrityRecord key={packet.qaPacketId} name={packet.qaPacketId} detail={`${packet.status}${packet.admissionId ? `; admission ${packet.admissionId}` : ""}`}>
      {packet.evidence.length ? <dl className="mt-2 grid gap-2 sm:grid-cols-2">{packet.evidence.map((item) => <IntegrityField key={item.qaEvidenceId} label={item.semanticRole} value={`${item.qaEvidenceId} (${item.sha256}, ${item.sizeBytes} bytes, ${item.mediaType})`} />)}</dl> : null}
    </IntegrityRecord>)}</div></div>);
  }
  return <IntegritySection title="Integrity prototype" records={records} />;
}

function OperationTransfer({ transfer }: { transfer: GuidedOperationTransfer }) {
  const hasContent = transfer.frontier.length > 0 || transfer.members.length > 0 || transfer.authorityLayers.length > 0 || Boolean(transfer.ticket || transfer.package || transfer.run || transfer.audit || transfer.remediation || transfer.prototype);
  if (!hasContent) return null;
  return <div className="mt-4">
    <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Operation transfer</h3>
    <dl className="mt-2 grid gap-3 text-sm sm:grid-cols-2">
      {transfer.frontier.length ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Frontier tickets</dt><dd className="mt-1">{transfer.frontier.map((entry) => `${entry.ticketId} v${entry.revisionNumber} (priority ${entry.externalPriority}${entry.repoTarget ? `, ${entry.repoTarget}` : ""}${entry.branch ? ` @ ${entry.branch}` : ""})`).join("; ")}</dd></div> : null}
      {transfer.members.length ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Closure members</dt><dd className="mt-1">{transfer.members.join(", ")}</dd></div> : null}
      {transfer.authorityLayers.length ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Authority layers</dt><dd className="mt-1">{transfer.authorityLayers.join(", ")}</dd></div> : null}
      {transfer.ticket ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Selected delivery ticket</dt><dd className="mt-1">{transfer.ticket.ticketId} v{transfer.ticket.revisionNumber}{transfer.ticket.operationId ? ` — ${transfer.ticket.operationId}` : ""}{transfer.ticket.readiness.length ? `; readiness: ${transfer.ticket.readiness.join(", ")}` : ""}</dd></div> : null}
      {transfer.package ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Execution package</dt><dd className="mt-1">{transfer.package.packageId} ({transfer.package.state})</dd></div> : null}
      {transfer.run ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Package run</dt><dd className="mt-1">{transfer.run.runId} ({transfer.run.status}){transfer.run.repoTarget ? ` on ${transfer.run.repoTarget}${transfer.run.branch ? ` @ ${transfer.run.branch}` : ""}` : ""}{transfer.run.baseCommit ? ` base ${transfer.run.baseCommit}` : ""}</dd></div> : null}
      {transfer.audit ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Workflow audit</dt><dd className="mt-1">{transfer.audit.runId} ({transfer.audit.runStatus}) — {transfer.audit.auditState}{transfer.audit.auditPacketId ? `; packet ${transfer.audit.auditPacketId}` : ""}{transfer.audit.auditedCommit ? `; commit ${transfer.audit.auditedCommit}` : ""}</dd></div> : null}
      {transfer.remediation ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Remediation</dt><dd className="mt-1">{transfer.remediation.state}{transfer.remediation.seedIds.length ? `; seeds: ${transfer.remediation.seedIds.join(", ")}` : ""}</dd></div> : null}
      {transfer.prototype ? <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Prototype run</dt><dd className="mt-1">{transfer.prototype.runId} ({transfer.prototype.runState}){transfer.prototype.processOutcome ? `; outcome ${transfer.prototype.processOutcome}` : ""}{transfer.prototype.cleanup.length ? `; cleanup: ${transfer.prototype.cleanup.map((item) => `${item.kind}=${item.status}`).join(", ")}` : ""}{transfer.prototype.qaPackets.length ? `; QA: ${transfer.prototype.qaPackets.map((packet) => `${packet.packetId} (${packet.status}${packet.evidence.length ? `, evidence ${packet.evidence.join(", ")}` : ""})`).join(", ")}` : ""}</dd></div> : null}
    </dl>
  </div>;
}

export function RelayFeatureWorkspaceDetail({ detail }: { detail: GuidedFeatureDetail }) {
  const queryClient = useQueryClient();
  const [confirmed, setConfirmed] = React.useState(false);
  const [reopenMarkdown, setReopenMarkdown] = React.useState("");
  const [reopenCause, setReopenCause] = React.useState("");
  const [message, setMessage] = React.useState<string | null>(null);
  const workspaceId = detail.workspace.workspaceId;
  const primary = detail.availableActions.find((action) => action.primary) ?? {
    action: detail.primaryAction,
    primary: true,
    enabled: false,
    requiresConfirmation: true,
  };
  React.useEffect(() => { setConfirmed(false); setReopenMarkdown(""); setReopenCause(""); }, [primary.action]);
  const mutation = useMutation({
    mutationFn: async () => {
      const request: {
        expectedVersion: number;
        action: GuidedFeatureAction;
        confirmation: boolean;
        cause?: string;
        markdown?: string;
      } = {
        expectedVersion: detail.workspace.version,
        action: primary.action,
        confirmation: confirmed,
      };
      if (primary.action === "reopen_discovery") {
        request.cause = reopenCause;
        request.markdown = reopenMarkdown;
      }
      return actOnGuidedFeatureWorkspace(workspaceId, request);
    },
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
  const actionEnabled = primary.enabled && (primary.action !== "reopen_discovery" || (reopenMarkdown.trim().length > 0 && reopenCause.trim().length > 0));
  const actionDisabledReason = !primary.enabled ? (primary.blockedReason || "This action is blocked by the current guided workspace state.") : primary.action === "reopen_discovery" ? "Author the replacement integrated revision and its reopen cause before confirming the reopen." : "Confirm this action before continuing.";

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
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Closure basis</dt><dd className="mt-1 text-sm">{detail.discovery.basis || "No closure basis recorded."}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Reopen state</dt><dd className="mt-1 text-sm">{detail.discovery.reopenState || "none"}</dd></div>
      </dl>
      <div className="mt-4 rounded border p-3 text-sm"><span className="font-medium">Rationale: </span>{detail.discovery.rationale || "No rationale recorded."}</div>
    </section>

    <section aria-labelledby="guided-history" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-history" className="font-semibold">History</h2>
      <dl className="mt-3 grid gap-3 sm:grid-cols-2">
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Discovery currentness</dt><dd className="mt-1 text-sm">{detail.diagnostics.history.discoveryCurrentness || "None recorded."}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Status</dt><dd className="mt-1 text-sm">{detail.diagnostics.history.status || "None recorded."}</dd></div>
      </dl>
      <div className="mt-3"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Historical basis diagnostics</h3><ProjectionList items={detail.diagnostics.historical} /></div>
    </section>

    <section aria-labelledby="guided-delivery" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-delivery" className="font-semibold">Delivery</h2>
      <dl className="mt-3 grid gap-3 sm:grid-cols-3">
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Frontier</dt><dd className="mt-1 text-sm">{detail.ticketFrontier.status || "Not available"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Destination</dt><dd className="mt-1 text-sm">{detail.downstream.status || "Not selected"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Downstream</dt><dd className="mt-1 text-sm">{detail.downstream.summary || "No downstream guidance recorded."}</dd></div>
      </dl>
      <p className="mt-3 text-sm text-muted-foreground">{detail.ticketFrontier.summary || "No ticket frontier guidance recorded."}</p>
      {detail.delivery?.frontier.length ? <div className="mt-4">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Frontier tickets</h3>
        <ul className="mt-2 space-y-1 text-sm">{detail.delivery.frontier.map((entry) => <li key={entry.ticketId}>{entry.ticketId} v{entry.revisionNumber} (priority {entry.externalPriority}{entry.repoTarget ? `, ${entry.repoTarget}` : ""}{entry.branch ? ` @ ${entry.branch}` : ""})</li>)}</ul>
      </div> : null}
      {detail.delivery ? <dl className="mt-4 grid gap-3 sm:grid-cols-3">
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Selection</dt><dd className="mt-1 text-sm">{detail.delivery.selectionState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Ticket Design Brief</dt><dd className="mt-1 text-sm">{detail.delivery.briefState || "none"}{detail.delivery.briefReviewDisposition ? ` (${detail.delivery.briefReviewDisposition})` : ""}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Package</dt><dd className="mt-1 text-sm">{detail.delivery.packageState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Run</dt><dd className="mt-1 text-sm">{detail.delivery.runState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Audit</dt><dd className="mt-1 text-sm">{detail.delivery.auditState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Remediation</dt><dd className="mt-1 text-sm">{detail.delivery.remediationState || "none"}</dd></div>
      </dl> : null}
      <div className="mt-3 grid gap-3 sm:grid-cols-2"><div><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Blockers</h3><ProjectionList items={detail.ticketFrontier.blockers} /></div><div><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Pending downstream</h3><ProjectionList items={detail.ticketFrontier.downstream} /></div></div>
    </section>

    <section aria-labelledby="guided-prototype-qa" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-prototype-qa" className="font-semibold">Prototype and QA</h2>
      <p className="mt-1 text-sm"><span className="font-medium">Status: </span>{detail.prototypeQA.status || "Not available"}</p>
      <p className="mt-2 text-sm text-muted-foreground">{detail.prototypeQA.summary || "No prototype or QA guidance recorded."}</p>
      {detail.prototype ? <dl className="mt-4 grid gap-3 sm:grid-cols-2">
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Execution</dt><dd className="mt-1 text-sm">{detail.prototype.runState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Cleanup</dt><dd className="mt-1 text-sm">{detail.prototype.cleanupState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">QA packet</dt><dd className="mt-1 text-sm">{detail.prototype.qaState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">QA evidence</dt><dd className="mt-1 text-sm">{detail.prototype.evidenceState || "none"}</dd></div>
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Process outcome</dt><dd className="mt-1 text-sm">{detail.prototype.processOutcome || "none"}</dd></div>
      </dl> : null}
      <div className="mt-3"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Required evidence</h3><ProjectionList items={detail.prototypeQA.requiredEvidence} /></div>
    </section>

    <section aria-labelledby="guided-recovery" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-recovery" className="font-semibold">Blockers and recovery</h2>
      <p className="mt-1 text-sm">{detail.recovery.blocked ? "Progression is blocked." : "No currentness recovery is required."}</p>
      <p className="mt-2 text-sm text-muted-foreground">{detail.recovery.summary || "No recovery guidance recorded."}</p>
      {detail.recovery.category ? <p className="mt-2 text-xs text-muted-foreground">Recovery category: {detail.recovery.category}</p> : null}
      <div className="mt-3"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Recovery actions</h3><ProjectionList items={detail.recovery.actions} /></div>
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
        <div><dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Needs revision</dt><dd className="mt-1 text-sm">{detail.planning.needsRevision ?? 0}</dd></div>
      </dl>
    </section>

    <section aria-labelledby="guided-completion" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-completion" className="font-semibold">Completion and closing</h2>
      <p className="mt-1 text-sm text-muted-foreground">{detail.completion.recorded ? "Completion has been recorded." : detail.completion.ready ? "All completion gates are ready." : "Completion remains blocked by one or more gates."}</p>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">{detail.completion.gates.map((gate) => <div key={gate.name} className="rounded border p-3 text-sm"><span className="font-medium">{gate.name}</span><span className={gate.ready ? "ml-2 text-success" : "ml-2 text-destructive"}>{gate.ready ? "ready" : "blocked"}</span></div>)}</div>
    </section>

    <section aria-labelledby="guided-handoff" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-handoff" className="font-semibold">Handoff and return</h2>
      <p className="mt-1 text-sm text-muted-foreground">{detail.handoff.available ? detail.handoff.instruction : "No role handoff is currently selected."}</p>
      <p className="mt-2 text-sm text-muted-foreground">{detail.handoff.returnGuidance || "Return here after bounded role work."}</p>
      {detail.handoff.transfer ? <OperationTransfer transfer={detail.handoff.transfer} /> : null}
      <Link to="/projects/$projectId" params={{ projectId: detail.project.projectId }} className="mt-3 inline-flex text-sm font-medium underline-offset-2 hover:underline">Return to {detail.project.name || "Project"}</Link>
    </section>

    <section aria-labelledby="guided-action" className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <h2 id="guided-action" className="font-semibold">Next guided action</h2>
      <p className="mt-1 text-sm text-muted-foreground">The server selected one primary action for this workspace.</p>
       {primary.requiresConfirmation ? <label className="mt-4 flex items-start gap-2 text-sm"><input aria-label="Confirm guided action" type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} /><span>I confirm that I want to invoke “{actionLabel(primary.action)}” against this displayed workspace state.</span></label> : null}
       {primary.action === "reopen_discovery" ? <div className="mt-4 space-y-3">
         <label className="block text-sm">
           <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Replacement integrated revision (markdown)</span>
           <textarea aria-label="Replacement integrated revision" className="mt-1 w-full rounded border bg-background p-2 font-mono text-xs" rows={8} value={reopenMarkdown} onChange={(event) => setReopenMarkdown(event.target.value)} placeholder="# Reopened discovery" />
         </label>
         <label className="block text-sm">
           <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Reopen cause</span>
           <input aria-label="Reopen cause" className="mt-1 w-full rounded border bg-background p-2 text-sm" value={reopenCause} onChange={(event) => setReopenCause(event.target.value)} placeholder="Why is the closed discovery being reopened?" />
         </label>
       </div> : null}
       {primary.handoff ? <p role="status" className="mt-3 rounded border p-3 text-sm text-muted-foreground">{primary.handoff} Use the primary action to acknowledge this handoff, then return here after the bounded role work is complete.</p> : null}
       {!actionEnabled ? <p role="status" className="mt-3 text-sm text-muted-foreground">{actionDisabledReason}</p> : null}
       <Button className="mt-4" type="button" disabled={!actionEnabled || (primary.requiresConfirmation && !confirmed) || mutation.isPending} onClick={() => mutation.mutate()}>{mutation.isPending ? "Applying…" : actionLabel(primary.action)}</Button>
    </section>

    <details className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
      <summary className="cursor-pointer font-semibold">Diagnostics</summary>
      <div className="mt-4 space-y-5 text-sm">
        <div><h3 className="font-medium">Stale state</h3>{detail.diagnostics.staleItems.length ? <ProjectionList items={detail.diagnostics.staleItems} /> : <dl className="mt-2 grid gap-2 sm:grid-cols-2">{Object.entries(detail.diagnostics.stale).map(([name, value]) => <div key={name}><dt className="text-xs uppercase tracking-wide text-muted-foreground">{name}</dt><dd>{value || "None"}</dd></div>)}</dl>}</div>
        <div><h3 className="font-medium">Discovery diagnostics</h3><p className="mt-1 text-muted-foreground">Route material open: {detail.diagnostics.discovery.routeMaterialOpen ? "yes" : "no"}</p><div className="mt-3 grid gap-3 sm:grid-cols-2"><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Blockers</h4><ProjectionList items={detail.diagnostics.discovery.blockers} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Restoration actions</h4><ProjectionList items={detail.diagnostics.discovery.restorationActions} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Pending integrations</h4><ProjectionList items={detail.diagnostics.discovery.pendingIntegrations} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Active operations</h4><ProjectionList items={detail.diagnostics.discovery.activeOperations} /></div><div><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Required evidence</h4><ProjectionList items={detail.diagnostics.discovery.requiredEvidence} /></div></div></div>
        <div><h3 className="font-medium">Delivery diagnostics</h3><ProjectionList items={detail.diagnostics.delivery} empty="No delivery diagnostics recorded." /></div>
        <div><h3 className="font-medium">Prototype diagnostics</h3><ProjectionList items={detail.diagnostics.prototype} empty="No prototype diagnostics recorded." /></div>
        <div><h3 className="font-medium">Integrity identities</h3><p className="mt-1 text-sm text-muted-foreground">Read-only source identities for inspection; they are never action inputs.</p><div className="mt-3 grid gap-3 sm:grid-cols-2">
          <IntegrityDiscoverySection discovery={detail.diagnostics.integrity.discovery} />
          <IntegrityAuthoritySection authority={detail.diagnostics.integrity.authority} />
          <IntegrityPlanningSection planning={detail.diagnostics.integrity.planning} />
          <IntegrityDeliverySection delivery={detail.diagnostics.integrity.delivery} />
          <IntegrityPrototypeSection prototype={detail.diagnostics.integrity.prototype} />
        </div></div>
        <IntegrityDiagnostics diagnostics={detail.diagnostics.integrity.diagnostics} />
      </div>
    </details>
  </div>;
}
