import { asWorkflowRecord, RelayApiError, requestWorkflowJson, requiredWorkflowArray, requiredWorkflowInteger, requiredWorkflowString, type WorkflowHttpMethod, type WorkflowJsonRecord } from "@/features/workflow-api";
import type { AuthorityRevision, CompleteFeatureWorkspaceRequest, CreateDiscoveryTicketRequest, CreateFeatureWorkspaceRequest, FeatureCompletionStatus, FeatureWorkspace, FeatureWorkspaceDetail, GoverningArtifactApproval, GuidedFeatureAction, GuidedFeatureActionRequest, GuidedFeatureDetail, GuidedFrontierEntry, GuidedOperationTransfer, ProjectFeatureWorkspaceListResponse, ProjectFeatureWorkspaceSummary, PublishAuthorityRequest, RecordAuthorityApprovalRequest, ResolveDiscoveryTicketRequest, RouteFeatureWorkspaceRequest } from "./types";

function record(value: unknown, method: WorkflowHttpMethod, path: string, context: string): WorkflowJsonRecord { return asWorkflowRecord(value, method, path, context); }
function nullableInteger(value: unknown, method: WorkflowHttpMethod, path: string, field: string): number | null { if (value === null || value === undefined) return null; if (!Number.isInteger(value)) throw new RelayApiError(`Malformed JSON response from ${method} ${path}: ${field} must be an integer or null`, 502, path, method); return value as number; }
function workspace(value: unknown, method: WorkflowHttpMethod, path: string): FeatureWorkspace { const item = record(value, method, path, "workspace"); return { workspaceId: requiredWorkflowString(item, "workspaceId", method, path, "workspace"), featureSlug: requiredWorkflowString(item, "featureSlug", method, path, "workspace"), state: requiredWorkflowString(item, "state", method, path, "workspace") as FeatureWorkspace["state"], version: requiredWorkflowInteger(item, "version", method, path, "workspace", 1), createdAt: requiredWorkflowString(item, "createdAt", method, path, "workspace", true), updatedAt: requiredWorkflowString(item, "updatedAt", method, path, "workspace", true) }; }
function authority(value: unknown, method: WorkflowHttpMethod, path: string): AuthorityRevision { const item = record(value, method, path, "authorityRevision"); return { authorityRevisionId: requiredWorkflowString(item, "authorityRevisionId", method, path, "authorityRevision"), revisionNumber: requiredWorkflowInteger(item, "revisionNumber", method, path, "authorityRevision", 1), sourceClosureRowId: nullableInteger(item.sourceClosureRowId, method, path, "sourceClosureRowId"), layers: requiredWorkflowArray(item, "layers", method, path, "authorityRevision").map((layer) => { const value = record(layer, method, path, "authority layer"); return { kind: requiredWorkflowString(value, "kind", method, path, "authority layer") as AuthorityRevision["layers"][number]["kind"], sequence: requiredWorkflowInteger(value, "sequence", method, path, "authority layer", 1), artifactRowId: nullableInteger(value.artifactRowId, method, path, "artifactRowId"), retainedArtifactRowId: nullableInteger(value.retainedArtifactRowId, method, path, "retainedArtifactRowId"), artifactSha256: requiredWorkflowString(value, "artifactSha256", method, path, "authority layer"), sourceClosureRowId: nullableInteger(value.sourceClosureRowId, method, path, "sourceClosureRowId"), approvalRowId: nullableInteger(value.approvalRowId, method, path, "approvalRowId") }; }), createdAt: requiredWorkflowString(item, "createdAt", method, path, "authorityRevision", true) }; }
function approval(value: unknown, method: WorkflowHttpMethod, path: string): GoverningArtifactApproval { const item = record(value, method, path, "approval"); return { approvalId: requiredWorkflowString(item, "approvalId", method, path, "approval"), workspaceRowId: requiredWorkflowInteger(item, "workspaceRowId", method, path, "approval", 1), artifactRowId: nullableInteger(item.artifactRowId, method, path, "artifactRowId"), retainedArtifactRowId: nullableInteger(item.retainedArtifactRowId, method, path, "retainedArtifactRowId"), family: requiredWorkflowString(item, "family", method, path, "approval") as GoverningArtifactApproval["family"], artifactSha256: requiredWorkflowString(item, "artifactSha256", method, path, "approval"), operatorConfirmationEvidence: requiredWorkflowString(item, "operatorConfirmationEvidence", method, path, "approval"), invalidatedByApprovalRowId: nullableInteger(item.invalidatedByApprovalRowId, method, path, "invalidatedByApprovalRowId"), supersededByApprovalRowId: nullableInteger(item.supersededByApprovalRowId, method, path, "supersededByApprovalRowId"), createdAt: requiredWorkflowString(item, "createdAt", method, path, "approval", true) }; }

function workspaceProject(value: unknown, method: WorkflowHttpMethod, path: string): FeatureWorkspaceDetail["project"] { const item = record(value, method, path, "workspace project"); return { projectId: requiredWorkflowString(item, "projectId", method, path, "workspace project"), name: requiredWorkflowString(item, "name", method, path, "workspace project", true) }; }

function detail(value: unknown, method: WorkflowHttpMethod, path: string): FeatureWorkspaceDetail { const item = record(value, method, path, "workspace detail"); const sourceBasis = record(item.sourceBasis, method, path, "sourceBasis"); return { workspace: workspace(item.workspace, method, path), project: workspaceProject(item.project, method, path), inputs: requiredWorkflowArray(item, "inputs", method, path, "workspace detail"), destinations: requiredWorkflowArray(item, "destinations", method, path, "workspace detail"), tickets: requiredWorkflowArray(item, "tickets", method, path, "workspace detail").map((ticket) => { const value = record(ticket, method, path, "ticket"); return { ticketId: requiredWorkflowString(value, "ticketId", method, path, "ticket"), ticketKey: requiredWorkflowString(value, "ticketKey", method, path, "ticket"), subject: requiredWorkflowString(value, "subject", method, path, "ticket"), state: requiredWorkflowString(value, "state", method, path, "ticket") as FeatureWorkspaceDetail["tickets"][number]["state"], version: requiredWorkflowInteger(value, "version", method, path, "ticket", 1), dependencies: requiredWorkflowArray(value, "dependencies", method, path, "ticket").map((dependency) => { const item = record(dependency, method, path, "ticket dependency"); return { dependsOnTicketRowId: requiredWorkflowInteger(item, "dependsOnTicketRowId", method, path, "ticket dependency", 1), kind: requiredWorkflowString(item, "kind", method, path, "ticket dependency") as "blocks" | "informs" }; }), resolutions: requiredWorkflowArray(value, "resolutions", method, path, "ticket").map((resolution) => { const item = record(resolution, method, path, "resolution"); return { resolutionId: requiredWorkflowString(item, "resolutionId", method, path, "resolution"), sequence: requiredWorkflowInteger(item, "sequence", method, path, "resolution", 1), kind: requiredWorkflowString(item, "kind", method, path, "resolution") as "resolved" | "rejected" | "deferred", artifactRowId: nullableInteger(item.artifactRowId, method, path, "artifactRowId"), retainedArtifactRowId: nullableInteger(item.retainedArtifactRowId, method, path, "retainedArtifactRowId"), artifactSha256: requiredWorkflowString(item, "artifactSha256", method, path, "resolution"), sourceClosureRowId: nullableInteger(item.sourceClosureRowId, method, path, "sourceClosureRowId"), createdAt: requiredWorkflowString(item, "createdAt", method, path, "resolution", true) }; }), createdAt: requiredWorkflowString(value, "createdAt", method, path, "ticket", true), updatedAt: requiredWorkflowString(value, "updatedAt", method, path, "ticket", true) }; }), routes: requiredWorkflowArray(item, "routes", method, path, "workspace detail").map((route) => { const value = record(route, method, path, "route"); return { routeId: requiredWorkflowString(value, "routeId", method, path, "route"), sequence: requiredWorkflowInteger(value, "sequence", method, path, "route", 1), workspaceVersion: requiredWorkflowInteger(value, "workspaceVersion", method, path, "route", 1), state: requiredWorkflowString(value, "state", method, path, "route") as FeatureWorkspaceDetail["routes"][number]["state"], createdAt: requiredWorkflowString(value, "createdAt", method, path, "route", true) }; }), authorityRevisions: requiredWorkflowArray(item, "authorityRevisions", method, path, "workspace detail").map((revision) => authority(revision, method, path)), sourceBasis: { status: requiredWorkflowString(sourceBasis, "status", method, path, "sourceBasis") as "retained" | "not_recorded", investigationCount: requiredWorkflowInteger(sourceBasis, "investigationCount", method, path, "sourceBasis", 0) } }; }
function guidedAction(value: unknown, method: WorkflowHttpMethod, path: string, context: string): GuidedFeatureAction {
  const action = typeof value === "string" ? value : "";
  if (!["continue_discovery", "close_discovery", "author_requirements", "author_shared_design", "author_delivery_ticket", "review_planning_candidate", "approve_planning_candidate", "promote_planning_candidate", "continue_established_route", "complete_feature", "legacy_recovery", "reopen_discovery", "select_delivery_ticket", "prepare_package", "approve_package", "launch_run", "prepare_audit", "record_audit_decision", "remediate", "prototype_execute", "prototype_cleanup", "prototype_qa"].includes(action)) {
    throw new RelayApiError(`Malformed JSON response from ${method} ${path}: ${context}.action is invalid`, 502, path, method);
  }
  return action as GuidedFeatureAction;
}

function guidedStringArray(value: unknown, method: WorkflowHttpMethod, path: string, context: string): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) {
    throw new RelayApiError(`Malformed JSON response from ${method} ${path}: ${context} must be an array of strings`, 502, path, method);
  }
  return value as string[];
}

function guidedArrayOrEmpty(value: unknown, method: WorkflowHttpMethod, path: string, context: string): unknown[] {
  return value === undefined || value === null ? [] : requiredWorkflowArray({ value }, "value", method, path, context);
}

function guidedStringArrayOrEmpty(value: unknown, method: WorkflowHttpMethod, path: string, context: string): string[] {
  return value === undefined || value === null ? [] : guidedStringArray(value, method, path, context);
}

function guidedOptionalString(value: WorkflowJsonRecord, field: string): string {
  return typeof value[field] === "string" ? value[field] as string : "";
}

function guidedFrontierEntries(value: unknown, method: WorkflowHttpMethod, path: string, context: string): GuidedFrontierEntry[] {
  return guidedArrayOrEmpty(value, method, path, context).map((entry) => {
    const item = record(entry, method, path, "guided frontier entry");
    return {
      ticketId: requiredWorkflowString(item, "ticketId", method, path, "guided frontier entry"),
      revisionNumber: requiredWorkflowInteger(item, "revisionNumber", method, path, "guided frontier entry", 1),
      externalPriority: requiredWorkflowInteger(item, "externalPriority", method, path, "guided frontier entry", 0),
      repoTarget: guidedOptionalString(item, "repoTarget"),
      branch: guidedOptionalString(item, "branch"),
    };
  });
}

function guidedTransfer(value: unknown, method: WorkflowHttpMethod, path: string): GuidedOperationTransfer | undefined {
  if (value === undefined || value === null) return undefined;
  const item = record(value, method, path, "guided operation transfer");
  const ticketValue = item.ticket === undefined || item.ticket === null ? undefined : record(item.ticket, method, path, "guided ticket transfer");
  const packageValue = item.package === undefined || item.package === null ? undefined : record(item.package, method, path, "guided package transfer");
  const runValue = item.run === undefined || item.run === null ? undefined : record(item.run, method, path, "guided run transfer");
  const auditValue = item.audit === undefined || item.audit === null ? undefined : record(item.audit, method, path, "guided audit transfer");
  const remediationValue = item.remediation === undefined || item.remediation === null ? undefined : record(item.remediation, method, path, "guided remediation transfer");
  const prototypeValue = item.prototype === undefined || item.prototype === null ? undefined : record(item.prototype, method, path, "guided prototype transfer");
  return {
    frontier: guidedFrontierEntries(item.frontier, method, path, "guided operation transfer.frontier"),
    members: guidedStringArrayOrEmpty(item.members, method, path, "guided operation transfer.members"),
    authorityLayers: guidedStringArrayOrEmpty(item.authorityLayers, method, path, "guided operation transfer.authorityLayers"),
    ticket: ticketValue === undefined ? undefined : { ticketId: requiredWorkflowString(ticketValue, "ticketId", method, path, "guided ticket transfer"), revisionNumber: requiredWorkflowInteger(ticketValue, "revisionNumber", method, path, "guided ticket transfer", 1), readiness: guidedStringArrayOrEmpty(ticketValue.readiness, method, path, "guided ticket transfer.readiness"), designBrief: guidedOptionalString(ticketValue, "designBrief") },
    package: packageValue === undefined ? undefined : { packageId: requiredWorkflowString(packageValue, "packageId", method, path, "guided package transfer"), state: requiredWorkflowString(packageValue, "state", method, path, "guided package transfer", true) },
    run: runValue === undefined ? undefined : { runId: requiredWorkflowString(runValue, "runId", method, path, "guided run transfer"), status: requiredWorkflowString(runValue, "status", method, path, "guided run transfer", true), repoTarget: guidedOptionalString(runValue, "repoTarget"), branch: guidedOptionalString(runValue, "branch"), baseCommit: guidedOptionalString(runValue, "baseCommit"), packageId: guidedOptionalString(runValue, "packageId") },
    audit: auditValue === undefined ? undefined : { runId: requiredWorkflowString(auditValue, "runId", method, path, "guided audit transfer"), runStatus: requiredWorkflowString(auditValue, "runStatus", method, path, "guided audit transfer", true), auditState: requiredWorkflowString(auditValue, "auditState", method, path, "guided audit transfer", true), auditPacketId: guidedOptionalString(auditValue, "auditPacketId"), auditedCommit: guidedOptionalString(auditValue, "auditedCommit") },
    remediation: remediationValue === undefined ? undefined : { state: requiredWorkflowString(remediationValue, "state", method, path, "guided remediation transfer", true), seedIds: guidedStringArrayOrEmpty(remediationValue.seedIds, method, path, "guided remediation transfer.seedIds") },
    prototype: prototypeValue === undefined ? undefined : {
      runId: requiredWorkflowString(prototypeValue, "runId", method, path, "guided prototype transfer"),
      runState: requiredWorkflowString(prototypeValue, "runState", method, path, "guided prototype transfer", true),
      processOutcome: guidedOptionalString(prototypeValue, "processOutcome"),
      cleanup: guidedArrayOrEmpty(prototypeValue.cleanup, method, path, "guided prototype transfer").map((entry) => { const value = record(entry, method, path, "guided cleanup transfer"); return { kind: guidedOptionalString(value, "kind"), status: guidedOptionalString(value, "status") }; }),
      qaPackets: guidedArrayOrEmpty(prototypeValue.qaPackets, method, path, "guided prototype transfer").map((entry) => { const value = record(entry, method, path, "guided QA packet transfer"); return { packetId: requiredWorkflowString(value, "packetId", method, path, "guided QA packet transfer"), status: requiredWorkflowString(value, "status", method, path, "guided QA packet transfer", true), evidence: guidedStringArrayOrEmpty(value.evidence, method, path, "guided QA packet transfer.evidence") }; }),
    },
  };
}

function guided(value: unknown, method: WorkflowHttpMethod, path: string): GuidedFeatureDetail {
  const item = record(value, method, path, "guided feature workspace");
  const discovery = record(item.discovery, method, path, "guided discovery");
  const authorityValue = item.authority === undefined ? {} : record(item.authority, method, path, "guided authority");
  const planning = record(item.planning, method, path, "guided planning");
  const completion = record(item.completion, method, path, "guided completion");
  const diagnostics = item.diagnostics === undefined ? {} : record(item.diagnostics, method, path, "guided diagnostics");
  const history = diagnostics.history === undefined || Array.isArray(diagnostics.history) ? {} : record(diagnostics.history, method, path, "guided history diagnostics");
  const stale = diagnostics.stale === undefined || Array.isArray(diagnostics.stale) ? {} : record(diagnostics.stale, method, path, "guided stale diagnostics");
  const discoveryDiagnostics = diagnostics.discovery === undefined || Array.isArray(diagnostics.discovery) ? {} : record(diagnostics.discovery, method, path, "guided discovery diagnostics");
  const currentness = item.currentness === undefined ? {} : record(item.currentness, method, path, "guided currentness");
  const delivery = item.delivery === undefined ? {} : record(item.delivery, method, path, "guided delivery");
  const prototype = item.prototype === undefined ? {} : record(item.prototype, method, path, "guided prototype");
  const recoveryProjection = item.recovery === undefined ? {} : record(item.recovery, method, path, "guided recovery projection");
  const ticketFrontier = item.ticketFrontier === undefined ? {} : record(item.ticketFrontier, method, path, "guided ticket frontier");
  const downstream = item.downstream === undefined ? {} : record(item.downstream, method, path, "guided downstream");
  const prototypeQA = item.prototypeQA === undefined ? {} : record(item.prototypeQA, method, path, "guided prototype QA");
  const recovery = item.recovery === undefined ? {} : record(item.recovery, method, path, "guided recovery");
  const handoff = item.handoff === undefined || item.handoff === null ? {} : record(item.handoff, method, path, "guided handoff");
  const revisions = guidedArrayOrEmpty(authorityValue.revisions, method, path, "guided authority").map((revision) => {
    const value = record(revision, method, path, "guided authority revision");
    return {
      revisionNumber: requiredWorkflowInteger(value, "revisionNumber", method, path, "guided authority revision", 0),
      layers: guidedStringArray(value.layers, method, path, "guided authority revision.layers"),
      historical: typeof value.historical === "boolean" ? value.historical : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided authority revision.historical must be a boolean`, 502, path, method); })(),
    };
  });
  const gates = requiredWorkflowArray(completion, "gates", method, path, "guided completion").map((gate) => {
    const value = record(gate, method, path, "guided completion gate");
    return { name: requiredWorkflowString(value, "name", method, path, "guided completion gate"), ready: typeof value.ready === "boolean" ? value.ready : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided completion gate.ready must be a boolean`, 502, path, method); })() };
  });
  const availableActions = requiredWorkflowArray(item, "availableActions", method, path, "guided feature workspace").map((action) => {
    const value = record(action, method, path, "guided action");
    return {
      action: guidedAction(value.action, method, path, "guided action"),
      primary: typeof value.primary === "boolean" ? value.primary : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided action.primary must be a boolean`, 502, path, method); })(),
      enabled: typeof value.enabled === "boolean" ? value.enabled : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided action.enabled must be a boolean`, 502, path, method); })(),
      requiresConfirmation: typeof value.requiresConfirmation === "boolean" ? value.requiresConfirmation : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided action.requiresConfirmation must be a boolean`, 502, path, method); })(),
      blockedReason: typeof value.blockedReason === "string" ? value.blockedReason : undefined,
      handoff: typeof value.handoff === "string" ? value.handoff : undefined,
    };
  });
  const parsedDelivery = item.delivery === undefined ? undefined : {
    frontier: guidedFrontierEntries(delivery.frontier, method, path, "guided delivery.frontier"),
    selectionState: requiredWorkflowString(delivery, "selectionState", method, path, "guided delivery", true),
    packageState: requiredWorkflowString(delivery, "packageState", method, path, "guided delivery", true),
    runState: requiredWorkflowString(delivery, "runState", method, path, "guided delivery", true),
    auditState: requiredWorkflowString(delivery, "auditState", method, path, "guided delivery", true),
    remediationState: requiredWorkflowString(delivery, "remediationState", method, path, "guided delivery", true),
  };
  return {
    workspace: workspace(item.workspace, method, path),
    project: workspaceProject(item.project, method, path),
    discovery: {
      state: requiredWorkflowString(discovery, "state", method, path, "guided discovery"),
      destination: guidedOptionalString(discovery, "destination"),
      rationale: requiredWorkflowString(discovery, "rationale", method, path, "guided discovery", true),
      continuation: guidedOptionalString(discovery, "continuation"),
      currentness: item.currentness === undefined ? guidedOptionalString(discovery, "currentness") : guidedOptionalString(currentness, "readiness"),
      basis: guidedOptionalString(discovery, "basis"),
      reopenState: guidedOptionalString(discovery, "reopenState"),
    },
    authority: { currentRevisionNumber: typeof authorityValue.currentRevisionNumber === "number" ? authorityValue.currentRevisionNumber : 0, revisions },
    planning: { readiness: guidedOptionalString(planning, "readiness") || guidedOptionalString(currentness, "readiness"), status: requiredWorkflowString(planning, "status", method, path, "guided planning", true), recoveryCategory: guidedOptionalString(planning, "recoveryCategory") || guidedOptionalString(currentness, "recoveryCategory"), candidateState: typeof planning.candidateState === "string" ? planning.candidateState : undefined, reviewState: typeof planning.reviewState === "string" ? planning.reviewState : undefined, approvalState: typeof planning.approvalState === "string" ? planning.approvalState : undefined, promotionState: typeof planning.promotionState === "string" ? planning.promotionState : undefined, candidateCount: typeof planning.candidateCount === "number" ? planning.candidateCount : undefined, awaitingReview: typeof planning.awaitingReview === "number" ? planning.awaitingReview : undefined, awaitingApproval: typeof planning.awaitingApproval === "number" ? planning.awaitingApproval : undefined, awaitingPromotion: typeof planning.awaitingPromotion === "number" ? planning.awaitingPromotion : undefined, promoted: typeof planning.promoted === "number" ? planning.promoted : undefined, historicalCount: typeof planning.historicalCount === "number" ? planning.historicalCount : undefined },
    delivery: parsedDelivery,
    prototype: item.prototype === undefined ? undefined : { runState: requiredWorkflowString(prototype, "runState", method, path, "guided prototype", true), cleanupState: requiredWorkflowString(prototype, "cleanupState", method, path, "guided prototype", true), qaState: requiredWorkflowString(prototype, "qaState", method, path, "guided prototype", true), evidenceState: requiredWorkflowString(prototype, "evidenceState", method, path, "guided prototype", true), processOutcome: guidedOptionalString(prototype, "processOutcome") },
    completion: { gates, ready: typeof completion.ready === "boolean" ? completion.ready : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided completion.ready must be a boolean`, 502, path, method); })(), recorded: typeof completion.recorded === "boolean" ? completion.recorded : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: guided completion.recorded must be a boolean`, 502, path, method); })() },
    ticketFrontier: { status: guidedOptionalString(ticketFrontier, "status") || (parsedDelivery && parsedDelivery.frontier.length > 0 ? `${parsedDelivery.frontier.length} delivery frontier ticket(s) ready` : ""), summary: guidedOptionalString(ticketFrontier, "summary") || "The server-owned delivery frontier is shown below.", blockers: guidedStringArrayOrEmpty(ticketFrontier.blockers ?? discoveryDiagnostics.blockers, method, path, "guided ticket frontier.blockers"), downstream: guidedStringArrayOrEmpty(ticketFrontier.downstream ?? discoveryDiagnostics.pendingIntegrations, method, path, "guided ticket frontier.downstream") },
    downstream: { status: guidedOptionalString(downstream, "status") || guidedOptionalString(discovery, "destination"), summary: guidedOptionalString(downstream, "summary") || guidedOptionalString(discovery, "continuation") },
    prototypeQA: { status: guidedOptionalString(prototypeQA, "status") || guidedOptionalString(prototype, "qaState"), summary: guidedOptionalString(prototypeQA, "summary") || "Prototype and QA remain bounded downstream operations.", requiredEvidence: guidedStringArrayOrEmpty(prototypeQA.requiredEvidence ?? discoveryDiagnostics.requiredEvidence, method, path, "guided prototype QA.requiredEvidence") },
    recovery: { blocked: typeof recovery.blocked === "boolean" ? recovery.blocked : recoveryProjection.state === "required", summary: guidedOptionalString(recovery, "summary") || guidedOptionalString(currentness, "effect"), category: guidedOptionalString(recovery, "category") || guidedOptionalString(recoveryProjection, "category") || guidedOptionalString(currentness, "recoveryCategory"), actions: guidedStringArrayOrEmpty(recovery.actions ?? recoveryProjection.available, method, path, "guided recovery.actions") },
    handoff: { available: typeof handoff.available === "boolean" ? handoff.available : item.handoff !== undefined && item.handoff !== null, instruction: guidedOptionalString(handoff, "instruction") || guidedOptionalString(handoff, "summary"), returnGuidance: guidedOptionalString(handoff, "returnGuidance") || guidedOptionalString(handoff, "resumeRoute"), transfer: guidedTransfer(handoff.transfer, method, path) },
    diagnostics: {
      history: { discoveryCurrentness: guidedOptionalString(history, "discoveryCurrentness"), status: guidedOptionalString(history, "status") },
      stale: { readiness: guidedOptionalString(stale, "readiness"), owner: guidedOptionalString(stale, "owner"), blockedOperation: guidedOptionalString(stale, "blockedOperation"), effect: guidedOptionalString(stale, "effect"), recoveryCategory: guidedOptionalString(stale, "recoveryCategory") },
      staleItems: Array.isArray(diagnostics.stale) ? guidedStringArrayOrEmpty(diagnostics.stale, method, path, "guided stale diagnostics") : [],
      historical: guidedStringArrayOrEmpty(diagnostics.historical, method, path, "guided historical diagnostics"),
      discovery: { blockers: guidedStringArrayOrEmpty(discoveryDiagnostics.blockers, method, path, "guided discovery diagnostics.blockers"), restorationActions: guidedStringArrayOrEmpty(discoveryDiagnostics.restorationActions, method, path, "guided discovery diagnostics.restorationActions"), pendingIntegrations: guidedStringArrayOrEmpty(discoveryDiagnostics.pendingIntegrations, method, path, "guided discovery diagnostics.pendingIntegrations"), activeOperations: guidedStringArrayOrEmpty(discoveryDiagnostics.activeOperations, method, path, "guided discovery diagnostics.activeOperations"), routeMaterialOpen: typeof discoveryDiagnostics.routeMaterialOpen === "boolean" ? discoveryDiagnostics.routeMaterialOpen : false, requiredEvidence: guidedStringArrayOrEmpty(discoveryDiagnostics.requiredEvidence, method, path, "guided discovery diagnostics.requiredEvidence") },
      delivery: guidedStringArrayOrEmpty(diagnostics.delivery, method, path, "guided delivery diagnostics"),
      prototype: guidedStringArrayOrEmpty(diagnostics.prototype, method, path, "guided prototype diagnostics"),
    },
    availableActions,
    primaryAction: guidedAction(item.primaryAction, method, path, "guided feature workspace"),
  };
}

function guidedResponse(value: unknown, method: WorkflowHttpMethod, path: string): GuidedFeatureDetail {
  return guided(record(value, method, path, "guided response").guided, method, path);
}

function completionStatus(value: unknown, method: WorkflowHttpMethod, path: string): FeatureCompletionStatus { const item = record(value, method, path, "completion status"); const current = item.currentDecision === undefined ? undefined : record(item.currentDecision, method, path, "current completion decision"); return { workspace: workspace(item.workspace, method, path), gates: requiredWorkflowArray(item, "gates", method, path, "completion status").map((gate) => { const value = record(gate, method, path, "completion gate"); const name = requiredWorkflowString(value, "name", method, path, "completion gate"); if (!["authority", "tickets", "integration", "transitions", "remediation", "audit"].includes(name) || typeof value.ready !== "boolean") throw new RelayApiError(`Malformed JSON response from ${method} ${path}: completion gate is invalid`, 502, path, method); return { name: name as FeatureCompletionStatus["gates"][number]["name"], ready: value.ready }; }), currentDecision: current ? { completionDecisionId: requiredWorkflowString(current, "completionDecisionId", method, path, "current completion decision"), authorityRevisionRowId: requiredWorkflowInteger(current, "authorityRevisionRowId", method, path, "current completion decision", 1), sourceClosureRowId: requiredWorkflowInteger(current, "sourceClosureRowId", method, path, "current completion decision", 1), decision: requiredWorkflowString(current, "decision", method, path, "current completion decision"), createdAt: requiredWorkflowString(current, "createdAt", method, path, "current completion decision", true) } : undefined }; }

export async function getFeatureWorkspace(workspaceId: string): Promise<FeatureWorkspaceDetail> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}`; return detail(await requestWorkflowJson<unknown>("GET", path), "GET", path); }
export async function getGuidedFeatureWorkspace(workspaceId: string): Promise<GuidedFeatureDetail> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/guided`; return guidedResponse(await requestWorkflowJson<unknown>("GET", path), "GET", path); }
export async function guidedFeatureWorkspaceAction(workspaceId: string, request: GuidedFeatureActionRequest): Promise<GuidedFeatureDetail> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/guided/actions`; return guidedResponse(await requestWorkflowJson<unknown>("POST", path, request), "POST", path); }
// Compatibility aliases keep the guided transport discoverable under both verb-first and resource-first names.
export const getFeatureWorkspaceGuided = getGuidedFeatureWorkspace;
export const actOnGuidedFeatureWorkspace = guidedFeatureWorkspaceAction;
export async function createFeatureWorkspace(request: CreateFeatureWorkspaceRequest): Promise<FeatureWorkspace> { const path = "/api/feature-workspaces"; const response = record(await requestWorkflowJson<unknown>("POST", path, request), "POST", path, "create workspace"); return workspace(response.workspace, "POST", path); }
export async function createDiscoveryTicket(workspaceId: string, request: CreateDiscoveryTicketRequest): Promise<void> { await requestWorkflowJson<unknown>("POST", `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/discovery-tickets`, request); }
export async function resolveDiscoveryTicket(workspaceId: string, ticketId: string, request: ResolveDiscoveryTicketRequest): Promise<void> { await requestWorkflowJson<unknown>("POST", `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/discovery-tickets/${encodeURIComponent(ticketId)}/resolutions`, request); }
export async function routeFeatureWorkspace(workspaceId: string, request: RouteFeatureWorkspaceRequest): Promise<void> { await requestWorkflowJson<unknown>("POST", `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/routes`, request); }
export async function publishFeatureAuthority(workspaceId: string, request: PublishAuthorityRequest): Promise<AuthorityRevision> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/authority-revisions`; const response = record(await requestWorkflowJson<unknown>("POST", path, request), "POST", path, "publish authority"); return authority(response.authorityRevision, "POST", path); }
export async function recordFeatureAuthorityApproval(workspaceId: string, request: RecordAuthorityApprovalRequest): Promise<GoverningArtifactApproval> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/authority-approvals`; const response = record(await requestWorkflowJson<unknown>("POST", path, request), "POST", path, "record approval"); return approval(response.approval, "POST", path); }
export async function getFeatureCompletionStatus(workspaceId: string): Promise<FeatureCompletionStatus> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/completion`; return completionStatus(await requestWorkflowJson<unknown>("GET", path), "GET", path); }
export async function completeFeatureWorkspace(workspaceId: string, request: CompleteFeatureWorkspaceRequest): Promise<FeatureCompletionStatus> { const path = `/api/feature-workspaces/${encodeURIComponent(workspaceId)}/completion`; const response = record(await requestWorkflowJson<unknown>("POST", path, request), "POST", path, "completion"); return completionStatus({ workspace: response.workspace, gates: [], currentDecision: response.decision }, "POST", path); }

function projectFeatureWorkspaceState(value: unknown, method: WorkflowHttpMethod, path: string, context: string): FeatureWorkspace["state"] { if (value === "open" || value === "closed") return value; throw new RelayApiError(`Malformed JSON response from ${method} ${path}: ${context}.state must be "open" or "closed"; received ${String(value)}`, 502, path, method); }

function projectFeatureWorkspaceSummary(value: unknown, method: WorkflowHttpMethod, path: string, context: string): ProjectFeatureWorkspaceSummary {
  const item = record(value, method, path, context);
  return {
    workspaceId: requiredWorkflowString(item, "workspaceId", method, path, context),
    projectId: requiredWorkflowString(item, "projectId", method, path, context),
    featureSlug: requiredWorkflowString(item, "featureSlug", method, path, context),
    state: projectFeatureWorkspaceState(item.state, method, path, context),
    version: requiredWorkflowInteger(item, "version", method, path, context, 1),
    createdAt: requiredWorkflowString(item, "createdAt", method, path, context, true),
    updatedAt: requiredWorkflowString(item, "updatedAt", method, path, context, true),
    progressionSummary: requiredWorkflowString(item, "progressionSummary", method, path, context, true),
    resumeSummary: requiredWorkflowString(item, "resumeSummary", method, path, context, true),
    blocked: typeof item.blocked === "boolean" ? item.blocked : (() => { throw new RelayApiError(`Malformed JSON response from ${method} ${path}: ${context}.blocked must be a boolean`, 502, path, method); })(),
    blockedReason: typeof item.blockedReason === "string" ? item.blockedReason : undefined,
    recoveryCategory: typeof item.recoveryCategory === "string" ? item.recoveryCategory : undefined,
  };
}

export async function listProjectFeatureWorkspaces(projectId: string, limit = 100): Promise<ProjectFeatureWorkspaceListResponse> {
  const path = `/api/projects/${encodeURIComponent(projectId)}/feature-workspaces?limit=${encodeURIComponent(String(limit))}`;
  const response = record(await requestWorkflowJson<unknown>("GET", path), "GET", path, "response");
  const items = requiredWorkflowArray(response, "items", "GET", path, "response");
  return {
    count: requiredWorkflowInteger(response, "count", "GET", path, "response", 0),
    items: items.map((item, index) => projectFeatureWorkspaceSummary(item, "GET", path, `items[${index}]`)),
  };
}
