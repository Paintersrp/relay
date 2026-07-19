export interface TicketAdmissionRequest {
  packetId: string;
  operationId: string;
  requiredDependencies?: { class: string; key: string }[];
}

export interface TicketFrontierEntry {
  ticketId: string;
  revisionRowId: number;
  revisionNumber: number;
  externalPriority: number;
  createdAt: string;
  repoTarget: string;
  branch: string;
  sourceClosureRowId: number;
  tieWithPrevious?: { previousTicketId: string; rule: "earlier_creation_time" | "stable_ticket_id" };
}

export interface TicketFrontier { workspaceId: string; entries: TicketFrontierEntry[] }
export interface TicketSelectionMember { ticketId: string; revisionRowId: number }
export interface SelectedTicket { ticketId: string; revisionRowId: number; revisionNumber: number; approvalRowId: number }
export interface TicketSelection { selectionId: string; state: string; rationale: string; createdAt: string; selectedTicket: SelectedTicket }

export interface PublishTicketRevisionRequest extends TicketAdmissionRequest {
  externalPriority: number;
  expectedRevisionNumber: number;
  revision: {
    repoTarget: string;
    branch: string;
    baseCommit: string;
    sourceClosureRowId: number;
    sourcePath: string;
    goal: string;
    context: string;
    transitionApplicability: "required" | "not_required";
    cancellationReason?: string;
    canonicalJson: unknown;
    renderedMarkdown: string;
    members: { kind: string; path?: string; text: string }[];
    dependencies: { revisionRowId: number; outcome: "satisfied" | "blocked" | "not_applicable" }[];
  };
}

export interface TicketDetail {
  ticketId: string;
  externalPriority: number;
  revision: { rowId: number; number: number; sourceClosureRowId: number; goal: string; approvals: { approvalId: string; state: string; authorityRevisionId: number | null; sourceClosureRowId: number; rationale: string }[] } | null;
  readiness: { ready: boolean; selected: boolean; reasons: string[] };
  history: { rowId: number; number: number; sourceClosureRowId: number; goal: string }[];
}
