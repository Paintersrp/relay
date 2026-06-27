import * as React from "react";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Copy,
  ExternalLink,
  FileJson,
  Hash,
  Loader2,
  RefreshCw,
  Send,
  Trash2,
} from "lucide-react";

import {
  getEditorLineCount,
  getPlanSubmissionPreview,
  parsePlanJson,
  type PlanSubmissionPreview,
} from "@/components/relay/relayPlanSubmissionState";
import { computePlanJsonSha256 } from "@/components/relay/relayPlanArtifactHash";
import {
  canApprove,
  canRevise,
  canRunDriftReview,
  canSubmitAttempt,
  canVoid,
  formatConfidence,
  getDriftBadgeVariant,
  getReviewGateLabel,
  getReviewSourceLabel,
  parseDriftFindings,
} from "@/components/relay/relayPlanReviewWorkflow";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RelayApiError } from "@/features/relay-runs";
import {
  approvePlanAttempt,
  createPlanAttemptWithIntent,
  getPlanAttemptReviewGate,
  getPlanReviewSettings,
  relayPlanKeys,
  revisePlanAttempt,
  runPlanAttemptDriftReview,
  submitPlanAttempt,
  validatePlan,
  voidPlanAttempt,
  type PlanAttemptAPI,
  type PlanAttemptReviewGateAPI,
  type PlanReviewSettingsAPI,
  type PlanValidationIssue,
  type IntentPacketAPI,
  type ValidatePlanResponse,
  type PlannerPassPlan,
} from "@/features/relay-plans";
import { cn } from "@/lib/utils";
import { useQueryClient } from "@tanstack/react-query";

// ─── Types ─────────────────────────────────────────────────────────────────────────────────

type AttemptReviewState =
  | "draft"
  | "parse_failed"
  | "validating"
  | "validated"
  | "validation_failed"
  | "creating_attempt"
  | "attempt_ready"
  | "review_running"
  | "approving_attempt"
  | "revising_attempt"
  | "voiding_attempt"
  | "submitting_approved_attempt"
  | "submitted"
  | "action_failed";

type SettingsLoadState = "idle" | "loading" | "loaded" | "failed";

interface ActionError {
  message: string;
  issues: PlanValidationIssue[];
}

// ─── Constants ─────────────────────────────────────────────────────────────────────────────

const EXAMPLE_PLAN_JSON = `{
  "plan_meta": {
    "plan_id": "plan-example",
    "schema_version": "1.0.0",
    "created_at": "2026-06-21T00:00:00Z",
    "title": "Descriptive plan title",
    "goal": "High-level goal for this managed plan",
    "repo_target": "owner/repo",
    "branch_context": "main",
    "status": "active"
  },
  "source_intent": {
    "summary": "Brief description of what this plan achieves."
  },
  "passes": [
    {
      "pass_id": "PASS-001",
      "sequence": 1,
      "name": "First pass name",
      "goal": "Specific goal for this pass",
      "intended_execution_scope": ["path/to/file.ts"],
      "non_goals": ["Out-of-scope behavior for this pass"],
      "dependencies": [],
      "status": "planned"
    }
  ]
}`;

const STATE_LABELS: Record<AttemptReviewState, string> = {
  draft: "Draft",
  parse_failed: "Validation Failed",
  validating: "Validating",
  validated: "Plan JSON Valid",
  validation_failed: "Validation Failed",
  creating_attempt: "Creating Attempt",
  attempt_ready: "Attempt Ready",
  review_running: "Running Review",
  approving_attempt: "Approving",
  revising_attempt: "Revising",
  voiding_attempt: "Voiding",
  submitting_approved_attempt: "Submitting",
  submitted: "Submitted",
  action_failed: "Action Failed",
};

// ─── Helpers ─────────────────────────────────────────────────────────────────────────────────

function extractIssues(value: unknown): PlanValidationIssue[] {
  if (!value || typeof value !== "object") return [];
  const record = value as Record<string, unknown>;
  const directIssues = record.issues;
  if (Array.isArray(directIssues)) return directIssues as PlanValidationIssue[];
  const validation = record.validation;
  if (validation && typeof validation === "object") {
    const vi = (validation as Record<string, unknown>).issues;
    if (Array.isArray(vi)) return vi as PlanValidationIssue[];
  }
  const details = record.details;
  if (details && typeof details === "object") return extractIssues(details);
  return [];
}

function getActionError(error: unknown): ActionError {
  if (error instanceof RelayApiError) {
    const shape = error.errorShape;
    return { message: shape?.message || shape?.error || error.message, issues: extractIssues(shape) };
  }
  if (error instanceof Error) return { message: error.message, issues: [] };
  return { message: "Action failed.", issues: [] };
}

function getStateBadgeVariant(state: AttemptReviewState): React.ComponentProps<typeof Badge>["variant"] {
  switch (state) {
    case "validated": case "attempt_ready": case "submitted": return "success";
    case "parse_failed": case "validation_failed": case "action_failed": case "voiding_attempt": return "destructive";
    case "validating": case "creating_attempt": case "review_running": case "approving_attempt":
    case "revising_attempt": case "submitting_approved_attempt": return "info";
    default: return "outline";
  }
}

function splitConstraints(text: string): string[] {
  return text.split(/\r?\n/).map((l) => l.trim()).filter((l) => l.length > 0);
}

// ─── Small primitives ───────────────────────────────────────────────────────────────────

function MetaRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">{label}</span>
      <span className={cn("break-all text-xs text-foreground", mono && "font-mono")}>{value || "—"}</span>
    </div>
  );
}

function SectionHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="mb-3">
      <div className="text-sm font-semibold text-foreground">{title}</div>
      {subtitle && <p className="mt-0.5 text-[11px] text-muted-foreground">{subtitle}</p>}
    </div>
  );
}

function IssueList({ issues }: { issues: PlanValidationIssue[] }) {
  if (issues.length === 0) {
    return <p className="text-xs leading-relaxed text-muted-foreground">No detailed issue payload was returned.</p>;
  }
  return (
    <div className="space-y-2">
      {issues.map((issue, index) => (
        <div key={`${issue.path ?? "root"}-${issue.code ?? "issue"}-${index}`} className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-3 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[10px] text-muted-foreground">{issue.path || "root"}</span>
            {issue.code && <Badge variant="outline" className="h-auto rounded-sm px-1.5 py-0 text-[10px]">{issue.code}</Badge>}
            {issue.severity && <span className="text-[10px] uppercase tracking-[0.08em] text-muted-foreground">{issue.severity}</span>}
          </div>
          <p className="mt-1 text-xs leading-relaxed text-foreground">{issue.message}</p>
        </div>
      ))}
    </div>
  );
}

function LoadingPanel({ label }: { label: string }) {
  return (
    <div className="flex min-h-[12rem] items-center justify-center border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)]">
      <div className="flex flex-col items-center gap-3 text-center">
        <Loader2 className="size-6 animate-spin text-[var(--relay-accent)]" />
        <p className="text-sm font-medium text-foreground">{label}</p>
      </div>
    </div>
  );
}

function PreviewSummary({ preview }: { preview: PlanSubmissionPreview }) {
  return (
    <div className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-foreground">{preview.title}</h3>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[11px] text-muted-foreground">
            <span>{preview.planId}</span><span className="text-muted-foreground/60">/</span>
            <span>{preview.repoTarget}</span><span className="text-muted-foreground/60">/</span>
            <span>{preview.branchContext}</span>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant="info" className="rounded-sm">{preview.passCount} passes</Badge>
          <Badge variant="outline" className="rounded-sm">{preview.dependencyCount} dependencies</Badge>
        </div>
      </div>
      <p className="mt-3 text-xs leading-relaxed text-muted-foreground">{preview.goal}</p>
      <div className="mt-3 border-t border-[var(--relay-row-border)] pt-3">
        <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Source Intent</div>
        <p className="mt-1 text-xs leading-relaxed text-foreground/85">{preview.sourceIntentSummary}</p>
      </div>
    </div>
  );
}

// ─── Artifact + Intent Capture Panel ─────────────────────────────────────────────────────────

interface ArtifactIntentCaptureProps {
  projectId: string; planJsonArtifactPath: string; planJsonArtifactSha256: string;
  planMarkdownArtifactPath: string; planMarkdownArtifactSha256: string;
  literalUserRequest: string; intentConstraintsText: string;
  isCreating: boolean; canCreate: boolean; parsedPlan?: PlannerPassPlan;
  settingsLoadState: SettingsLoadState; settings?: PlanReviewSettingsAPI;
  onProjectIdChange: (v: string) => void; onJsonPathChange: (v: string) => void;
  onJsonSha256Change: (v: string) => void; onMarkdownPathChange: (v: string) => void;
  onMarkdownSha256Change: (v: string) => void; onLiteralRequestChange: (v: string) => void;
  onConstraintsChange: (v: string) => void; onComputeHash: () => void; onCreateAttempt: () => void;
}

function ArtifactIntentCapturePanel(props: ArtifactIntentCaptureProps) {
  const inputClass = "mt-1 w-full border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-1.5 font-mono text-xs text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-1 focus:ring-[var(--relay-accent)]";
  const labelClass = "text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground";
  return (
    <div className="space-y-4">
      <div className="border border-success/30 bg-success/12 px-4 py-3">
        <div className="flex items-center gap-2 text-sm font-medium text-success">
          <CheckCircle2 className="size-4" />Plan JSON is valid
        </div>
        <p className="mt-1 text-xs text-muted-foreground">Complete artifact references and intent capture to create a draft plan attempt.</p>
      </div>
      {props.parsedPlan && <PreviewSummary preview={getPlanSubmissionPreview(props.parsedPlan)} />}
      <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
        <SectionHeader title="Artifact References" subtitle="Required before creating a draft attempt. Paths are repo-relative." />
        <label className="block">
          <span className={labelClass}>Project ID <span className="text-destructive">*</span></span>
          <input type="text" value={props.projectId} onChange={(e) => props.onProjectIdChange(e.target.value)} placeholder="e.g. proj_abc123" className={inputClass} />
          {props.projectId.trim() && (
            <p className={cn("mt-1 text-[10px]",
              props.settingsLoadState === "loading" && "text-muted-foreground",
              props.settingsLoadState === "loaded" && "text-success",
              props.settingsLoadState === "failed" && "text-destructive",
            )}>
              {props.settingsLoadState === "loading" && "Loading project review settings..."}
              {props.settingsLoadState === "loaded" && props.settings && `Settings loaded — drift: ${props.settings.driftReviewMode}, tier: ${props.settings.modelTier}`}
              {props.settingsLoadState === "failed" && "Cannot create attempt: review settings could not be loaded for this project."}
            </p>
          )}
        </label>
        <label className="block">
          <span className={labelClass}>Plan JSON Artifact Path <span className="text-destructive">*</span></span>
          <input type="text" value={props.planJsonArtifactPath} onChange={(e) => props.onJsonPathChange(e.target.value)} placeholder="e.g. handoffs/planner/2026-06-26_plan.json" className={inputClass} />
        </label>
        <label className="block">
          <div className="flex items-center justify-between gap-2">
            <span className={labelClass}>Plan JSON SHA-256 <span className="text-destructive">*</span></span>
            <button type="button" onClick={props.onComputeHash} className="flex items-center gap-1 text-[10px] text-[var(--relay-accent)] hover:underline">
              <Hash className="size-3" />Compute from editor JSON
            </button>
          </div>
          <input type="text" value={props.planJsonArtifactSha256} onChange={(e) => props.onJsonSha256Change(e.target.value)} placeholder="sha256:..." className={cn(inputClass, "text-[11px]")} />
          <p className="mt-1 text-[10px] text-muted-foreground">Computed from current editor JSON; backend will verify.</p>
        </label>
        <label className="block">
          <span className={labelClass}>Plan Markdown Artifact Path <span className="text-muted-foreground/60">(optional)</span></span>
          <input type="text" value={props.planMarkdownArtifactPath} onChange={(e) => props.onMarkdownPathChange(e.target.value)} placeholder="e.g. handoffs/planner/2026-06-26_plan.md" className={inputClass} />
        </label>
        {props.planMarkdownArtifactPath.trim() && (
          <label className="block">
            <span className={labelClass}>Plan Markdown SHA-256</span>
            <input type="text" value={props.planMarkdownArtifactSha256} onChange={(e) => props.onMarkdownSha256Change(e.target.value)} placeholder="sha256:..." className={cn(inputClass, "text-[11px]")} />
          </label>
        )}
      </div>
      <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
        <SectionHeader title="Intent Capture" subtitle="Describe the user request that produced this plan." />
        <label className="block">
          <span className={labelClass}>Literal User Request <span className="text-destructive">*</span></span>
          <textarea value={props.literalUserRequest} onChange={(e) => props.onLiteralRequestChange(e.target.value)} rows={3} placeholder="Paste the user's original request here..." className="mt-1 w-full resize-none border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-1 focus:ring-[var(--relay-accent)]" />
        </label>
        <label className="block">
          <span className={labelClass}>Intent Constraints <span className="text-muted-foreground/60">(optional, one per line)</span></span>
          <textarea value={props.intentConstraintsText} onChange={(e) => props.onConstraintsChange(e.target.value)} rows={3} placeholder={"Do not change backend APIs\nKeep existing routes"} className="mt-1 w-full resize-none border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2 font-mono text-xs text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-1 focus:ring-[var(--relay-accent)]" />
        </label>
      </div>
      <Button type="button" size="sm" disabled={!props.canCreate || props.isCreating} onClick={props.onCreateAttempt} className="w-full">
        {props.isCreating ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}
        Create Draft Attempt
      </Button>
    </div>
  );
}

// ─── Attempt Metadata Panel ────────────────────────────────────────────────────────────────────

function AttemptMetadataPanel({ planAttempt, intentPacket }: { planAttempt: PlanAttemptAPI; intentPacket?: IntentPacketAPI }) {
  return (
    <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <SectionHeader title="Attempt Metadata" />
      <div className="grid gap-3 sm:grid-cols-2">
        <MetaRow label="Plan Attempt ID" value={planAttempt.planAttemptId} mono />
        <MetaRow label="Status" value={planAttempt.status} />
        <MetaRow label="Intent Thread ID" value={intentPacket?.intentThreadId ?? planAttempt.intentThreadId} mono />
        <MetaRow label="Root Intent Packet ID" value={intentPacket?.rootIntentPacketId ?? planAttempt.rootIntentPacketId} mono />
        <MetaRow label="Current Intent Packet ID" value={intentPacket?.intentPacketId ?? planAttempt.currentIntentPacketId} mono />
        <MetaRow label="Drift Review Mode" value={planAttempt.driftReviewMode} />
        <MetaRow label="Model Tier" value={planAttempt.modelTier} />
        <MetaRow label="Review State" value={planAttempt.reviewState} />
        <MetaRow label="JSON Artifact Path" value={planAttempt.planJsonArtifactPath} mono />
        <MetaRow label="JSON Artifact SHA-256" value={planAttempt.planJsonArtifactSha256} mono />
        {planAttempt.planMarkdownArtifactPath && (
          <>
            <MetaRow label="Markdown Artifact Path" value={planAttempt.planMarkdownArtifactPath} mono />
            <MetaRow label="Markdown Artifact SHA-256" value={planAttempt.planMarkdownArtifactSha256 ?? ""} mono />
          </>
        )}
      </div>
    </div>
  );
}

// ─── Review Gate Panel ─────────────────────────────────────────────────────────────────────────────

function ReviewGatePanel({ gate }: { gate: PlanAttemptReviewGateAPI }) {
  const label = getReviewGateLabel(gate.workflowState);
  const badgeVariant = getDriftBadgeVariant(gate.workflowState);
  return (
    <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <SectionHeader title="Review Gate" />
        <Badge variant={badgeVariant} className="rounded-sm text-[10px]">{label}</Badge>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <MetaRow label="Drift Review Mode" value={gate.driftReviewMode} />
        <MetaRow label="Model Tier" value={gate.modelTier} />
        <MetaRow label="Review Required" value={gate.reviewRequired ? "Yes" : "No"} />
        <MetaRow label="Model Call Allowed" value={gate.modelCallAllowed ? "Yes" : "No"} />
      </div>
      {gate.modelCallWarning && (
        <div className="border border-warning/30 bg-warning/10 px-3 py-2">
          <div className="flex items-center gap-2 text-xs font-medium text-warning"><AlertTriangle className="size-3.5" />Model Call Warning</div>
          <p className="mt-1 text-xs text-muted-foreground">{gate.modelCallWarning}</p>
        </div>
      )}
      {gate.allowedActions.length > 0 && (
        <div>
          <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Allowed Actions</div>
          <div className="flex flex-wrap gap-1.5">
            {gate.allowedActions.map((action) => (
              <Badge key={action} variant="outline" className="rounded-sm font-mono text-[10px]">{action}</Badge>
            ))}
          </div>
        </div>
      )}
      {gate.blockers && gate.blockers.length > 0 && (
        <div>
          <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Blockers</div>
          <div className="space-y-1.5">
            {gate.blockers.map((blocker, i) => (
              <div key={`${blocker.code}-${i}`} className="border border-destructive/30 bg-destructive/10 px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[10px] text-destructive">{blocker.code}</span>
                  <Badge variant={blocker.recoverable ? "warning" : "destructive"} className="h-auto rounded-sm px-1.5 py-0 text-[10px]">{blocker.recoverable ? "recoverable" : "non-recoverable"}</Badge>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">{blocker.message}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Drift Findings Panel ─────────────────────────────────────────────────────────────────────────────

function DriftFindingsPanel({ gate }: { gate: PlanAttemptReviewGateAPI }) {
  const review = gate.latestReview;
  if (!review) return null;
  const findings = parseDriftFindings(review.findingsJson);
  const alignmentVariant = getDriftBadgeVariant(review.overallAlignment);
  const [expanded, setExpanded] = React.useState(false);
  return (
    <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <SectionHeader title="Drift Review Result" />
        <Badge variant={alignmentVariant} className="rounded-sm text-[10px]">{review.overallAlignment}</Badge>
      </div>
      <div className="border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-xs text-muted-foreground">
        Drift review is advisory evidence. Review the Plan JSON before approving.
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <MetaRow label="Review Source" value={getReviewSourceLabel(review.reviewSource)} />
        <MetaRow label="Confidence" value={formatConfidence(review.confidence)} />
        <MetaRow label="Recommended Action" value={review.recommendedAction} />
        <MetaRow label="Approval Gate Status" value={review.approvalGateStatus} />
        <MetaRow label="Reviewed At" value={review.createdAt} />
      </div>
      {findings.length > 0 && (
        <div>
          <button type="button" onClick={() => setExpanded((v) => !v)} className="flex items-center gap-1.5 text-[11px] font-medium text-foreground hover:text-[var(--relay-accent)]">
            {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            {findings.length} finding{findings.length !== 1 ? "s" : ""}
          </button>
          {expanded && (
            <div className="mt-2 space-y-2">
              {findings.map((finding, i) => (
                <div key={finding.findingId ?? i} className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-3 py-2">
                  <div className="flex flex-wrap items-center gap-2">
                    {finding.severity && <Badge variant={getDriftBadgeVariant(finding.severity)} className="h-auto rounded-sm px-1.5 py-0 text-[10px]">{finding.severity}</Badge>}
                    {finding.findingId && <span className="font-mono text-[10px] text-muted-foreground">{finding.findingId}</span>}
                  </div>
                  <p className="mt-1 text-xs font-medium text-foreground">{finding.summary}</p>
                  {finding.evidence.length > 0 && (
                    <ul className="mt-1 list-inside list-disc space-y-0.5">
                      {finding.evidence.map((ev, j) => <li key={j} className="text-[11px] text-muted-foreground">{ev}</li>)}
                    </ul>
                  )}
                  {finding.suggestedResolution && <p className="mt-1 text-[11px] italic text-muted-foreground">Resolution: {finding.suggestedResolution}</p>}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Separate Drift Audit Panel ─────────────────────────────────────────────────────────────────────

function SeparateDriftAuditPanel({ planAttempt, intentPacket, gate, onRefreshGate, isRefreshing }: {
  planAttempt: PlanAttemptAPI; intentPacket?: IntentPacketAPI; gate?: PlanAttemptReviewGateAPI;
  onRefreshGate: () => void; isRefreshing: boolean;
}) {
  const instructions = gate?.externalReviewInstructions;
  const codeClass = "break-all rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-2 py-1 font-mono text-[11px] text-foreground";
  return (
    <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <SectionHeader title="Separate Drift Audit" subtitle="Use a separate chat or reviewer to retrieve the bounded review packet and submit a structured drift review result. This UI will display the externally submitted result after refresh." />
      <div className="space-y-2">
        <MetaRow label="Plan Attempt ID" value={planAttempt.planAttemptId} mono />
        <MetaRow label="Intent Thread ID" value={intentPacket?.intentThreadId ?? planAttempt.intentThreadId} mono />
        <MetaRow label="Root Intent Packet ID" value={intentPacket?.rootIntentPacketId ?? planAttempt.rootIntentPacketId} mono />
        <MetaRow label="Current Intent Packet ID" value={intentPacket?.intentPacketId ?? planAttempt.currentIntentPacketId} mono />
      </div>
      {instructions && (
        <div className="space-y-2">
          <div>
            <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Review Packet Route</div>
            <code className={codeClass}>{instructions.reviewPacketRoute}</code>
          </div>
          <div>
            <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Submit Review Route</div>
            <code className={codeClass}>{instructions.submitReviewRoute}</code>
          </div>
        </div>
      )}
      <Button type="button" variant="outline" size="sm" disabled={isRefreshing} onClick={onRefreshGate}>
        {isRefreshing ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
        Refresh Review Gate
      </Button>
    </div>
  );
}

// ─── Action Panel ───────────────────────────────────────────────────────────────────────────────────

interface ActionPanelProps {
  gate?: PlanAttemptReviewGateAPI; state: AttemptReviewState; revisionMode: boolean;
  modelCallConfirmed: boolean; forceHighAssurance: boolean; jsonArtifactReviewed: boolean;
  noReviewAcknowledged: boolean; driftAcknowledged: boolean; submissionConfirmed: boolean;
  onModelCallConfirmedChange: (v: boolean) => void; onForceHighAssuranceChange: (v: boolean) => void;
  onJsonArtifactReviewedChange: (v: boolean) => void; onNoReviewAcknowledgedChange: (v: boolean) => void;
  onDriftAcknowledgedChange: (v: boolean) => void; onSubmissionConfirmedChange: (v: boolean) => void;
  onRunDriftReview: (forceHighAssurance?: boolean) => void; onApprove: () => void;
  onStartRevision: () => void; onVoid: () => void; onSubmitApprovedAttempt: () => void;
}

function ActionPanel({ gate, state, revisionMode, modelCallConfirmed, forceHighAssurance,
  jsonArtifactReviewed, noReviewAcknowledged, driftAcknowledged, submissionConfirmed,
  onModelCallConfirmedChange, onForceHighAssuranceChange, onJsonArtifactReviewedChange,
  onNoReviewAcknowledgedChange, onDriftAcknowledgedChange, onSubmissionConfirmedChange,
  onRunDriftReview, onApprove, onStartRevision, onVoid, onSubmitApprovedAttempt }: ActionPanelProps) {
  const isBusy = ["review_running","approving_attempt","voiding_attempt","submitting_approved_attempt","revising_attempt"].includes(state);
  const driftAllowed = canRunDriftReview(gate);
  const approveKind = canApprove(gate);
  const submitAllowed = canSubmitAttempt(gate);
  const reviseAllowed = canRevise(gate);
  const voidAllowed = canVoid(gate);
  const ckClass = "mt-0.5 accent-[var(--relay-accent)]";
  const lbClass = "flex cursor-pointer items-start gap-2";
  const lbTxt = "text-xs text-muted-foreground";
  return (
    <div className="space-y-3 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <SectionHeader title="Actions" />
      {driftAllowed && !revisionMode && (
        <div className="space-y-2 border-b border-[var(--relay-row-border)] pb-3">
          <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Drift Review</div>
          <label className={lbClass}><input type="checkbox" checked={modelCallConfirmed} onChange={(e) => onModelCallConfirmedChange(e.target.checked)} className={ckClass} /><span className={lbTxt}>I understand this may call a configured model provider (cost-bearing).</span></label>
          <label className={lbClass}><input type="checkbox" checked={forceHighAssurance} onChange={(e) => onForceHighAssuranceChange(e.target.checked)} className={ckClass} /><span className={lbTxt}>Force high-assurance tier review.</span></label>
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" disabled={!modelCallConfirmed || isBusy} onClick={() => onRunDriftReview(false)}>
              {state === "review_running" ? <Loader2 className="size-3.5 animate-spin" /> : <ExternalLink className="size-3.5" />}Check Intent Drift
            </Button>
            {gate?.latestReview && (
              <Button type="button" variant="outline" size="sm" disabled={!modelCallConfirmed || isBusy} onClick={() => onRunDriftReview(forceHighAssurance)}>
                <RefreshCw className="size-3.5" />Rerun / Escalate
              </Button>
            )}
          </div>
        </div>
      )}
      {approveKind && !revisionMode && (
        <div className="space-y-2 border-b border-[var(--relay-row-border)] pb-3">
          <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Approval</div>
          <label className={lbClass}><input type="checkbox" checked={jsonArtifactReviewed} onChange={(e) => onJsonArtifactReviewedChange(e.target.checked)} className={ckClass} /><span className={lbTxt}>I have reviewed the raw Plan JSON artifact above.</span></label>
          {approveKind === "no_review_ack" && <label className={lbClass}><input type="checkbox" checked={noReviewAcknowledged} onChange={(e) => onNoReviewAcknowledgedChange(e.target.checked)} className={ckClass} /><span className={lbTxt}>I acknowledge that no drift review was performed before this approval.</span></label>}
          {approveKind === "drift_ack" && <label className={lbClass}><input type="checkbox" checked={driftAcknowledged} onChange={(e) => onDriftAcknowledgedChange(e.target.checked)} className={ckClass} /><span className={lbTxt}>I acknowledge the drift findings and approve despite detected drift.</span></label>}
          <Button type="button" size="sm" disabled={!jsonArtifactReviewed||(approveKind==="no_review_ack"&&!noReviewAcknowledged)||(approveKind==="drift_ack"&&!driftAcknowledged)||isBusy} onClick={onApprove}>
            {state === "approving_attempt" ? <Loader2 className="size-3.5 animate-spin" /> : <CheckCircle2 className="size-3.5" />}
            {approveKind==="no_review_ack"?"Approve Without Drift Review":approveKind==="drift_ack"?"Acknowledge Drift and Approve":"Approve Plan"}
          </Button>
        </div>
      )}
      {submitAllowed && !revisionMode && (
        <div className="space-y-2 border-b border-[var(--relay-row-border)] pb-3">
          <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">Final Submission</div>
          <label className={lbClass}><input type="checkbox" checked={submissionConfirmed} onChange={(e) => onSubmissionConfirmedChange(e.target.checked)} className={ckClass} /><span className={lbTxt}>I confirm this creates managed plan and pass records. This cannot be undone.</span></label>
          <Button type="button" size="sm" disabled={!submissionConfirmed||isBusy} onClick={onSubmitApprovedAttempt}>
            {state === "submitting_approved_attempt" ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}Submit Approved Attempt
          </Button>
        </div>
      )}
      {!revisionMode && (reviseAllowed||voidAllowed) && (
        <div className="flex flex-wrap gap-2">
          {reviseAllowed && <Button type="button" variant="outline" size="sm" disabled={isBusy} onClick={onStartRevision}>Start Revision</Button>}
          {voidAllowed && (
            <Button type="button" variant="outline" size="sm" disabled={isBusy} onClick={onVoid} className="text-destructive hover:bg-destructive/10">
              {state === "voiding_attempt" ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}Void Attempt
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Planner Chat Guidance Panel ───────────────────────────────────────────────────────────────────

function PlannerChatGuidancePanel({ planAttempt, intentPacket }: { planAttempt: PlanAttemptAPI; intentPacket?: IntentPacketAPI }) {
  const [copied, setCopied] = React.useState(false);
  const mdLine = planAttempt.planMarkdownArtifactPath
    ? `- Markdown: ${planAttempt.planMarkdownArtifactPath} (${planAttempt.planMarkdownArtifactSha256 ?? ""})`
    : null;
  const lines = [
    "Created Plan of Passes review files and draft attempt.",
    "",
    "Files:",
    `- JSON: ${planAttempt.planJsonArtifactPath} (${planAttempt.planJsonArtifactSha256})`,
    ...(mdLine ? [mdLine] : []),
    "",
    "IDs:",
    `- plan_attempt_id: ${planAttempt.planAttemptId}`,
    `- intent_thread_id: ${intentPacket?.intentThreadId ?? planAttempt.intentThreadId}`,
    `- root_intent_packet_id: ${intentPacket?.rootIntentPacketId ?? planAttempt.rootIntentPacketId}`,
    `- current_intent_packet_id: ${intentPacket?.intentPacketId ?? planAttempt.currentIntentPacketId}`,
    "",
    "State: awaiting approval, revision notes, drift audit, or abandon.",
  ];
  const template = lines.join("\n");
  const handleCopy = async () => {
    try { await navigator.clipboard.writeText(template); setCopied(true); setTimeout(() => setCopied(false), 2000); } catch { /* ignore */ }
  };
  return (
    <div className="space-y-2 border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <SectionHeader title="Planner Chat Guidance" />
        <Button type="button" variant="outline" size="sm" onClick={handleCopy}>
          <Copy className="size-3.5" />{copied ? "Copied!" : "Copy"}
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-sm border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-3 font-mono text-[11px] leading-relaxed text-foreground">{template}</pre>
    </div>
  );
}

// ─── Submitted Panel ─────────────────────────────────────────────────────────────────────────────────

function SubmittedPanel({ planAttempt }: { planAttempt: PlanAttemptAPI }) {
  return (
    <div className="space-y-3">
      <div className="border border-success/30 bg-success/12 px-4 py-3">
        <div className="flex items-center gap-2 text-sm font-medium text-success"><CheckCircle2 className="size-4" />Plan and pass records created</div>
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">Managed plan and pass records created from approved attempt <span className="font-mono">{planAttempt.planAttemptId}</span>.</p>
      </div>
      {planAttempt.submittedPlanId && (
        <div className="flex flex-wrap gap-2">
          <Button asChild size="sm"><Link to="/plans/$planId" params={{ planId: planAttempt.submittedPlanId }}>Open Plan</Link></Button>
          <Button asChild variant="outline" size="sm"><Link to="/plans">View Plans</Link></Button>
        </div>
      )}
    </div>
  );
}

// ─── Right Pane ───────────────────────────────────────────────────────────────────────────────────

interface RightPaneProps {
  state: AttemptReviewState; parsedPlan?: PlannerPassPlan; issues: PlanValidationIssue[];
  actionError?: ActionError; planAttempt?: PlanAttemptAPI; intentPacket?: IntentPacketAPI;
  gate?: PlanAttemptReviewGateAPI; settings?: PlanReviewSettingsAPI; revisionMode: boolean;
  projectId: string; planJsonArtifactPath: string; planJsonArtifactSha256: string;
  planMarkdownArtifactPath: string; planMarkdownArtifactSha256: string;
  literalUserRequest: string; intentConstraintsText: string; canCreate: boolean;
  settingsLoadState: SettingsLoadState;
  onProjectIdChange: (v: string) => void; onJsonPathChange: (v: string) => void;
  onJsonSha256Change: (v: string) => void; onMarkdownPathChange: (v: string) => void;
  onMarkdownSha256Change: (v: string) => void; onLiteralRequestChange: (v: string) => void;
  onConstraintsChange: (v: string) => void; onComputeHash: () => void; onCreateAttempt: () => void;
  modelCallConfirmed: boolean; forceHighAssurance: boolean; jsonArtifactReviewed: boolean;
  noReviewAcknowledged: boolean; driftAcknowledged: boolean; submissionConfirmed: boolean;
  isRefreshingGate: boolean;
  onModelCallConfirmedChange: (v: boolean) => void; onForceHighAssuranceChange: (v: boolean) => void;
  onJsonArtifactReviewedChange: (v: boolean) => void; onNoReviewAcknowledgedChange: (v: boolean) => void;
  onDriftAcknowledgedChange: (v: boolean) => void; onSubmissionConfirmedChange: (v: boolean) => void;
  onRunDriftReview: (forceHighAssurance?: boolean) => void; onApprove: () => void;
  onStartRevision: () => void; onVoid: () => void; onSubmitApprovedAttempt: () => void;
  onRefreshGate: () => void;
  validationResponse?: ValidatePlanResponse;
}

function RightPane(props: RightPaneProps) {
  const { state, parsedPlan, issues, actionError, planAttempt, intentPacket, gate, revisionMode } = props;

  if (state === "validating" || state === "creating_attempt") {
    return <LoadingPanel label={state === "validating" ? "Validating plan JSON..." : "Creating draft attempt..."} />;
  }

  if (state === "parse_failed" || state === "validation_failed") {
    return (
      <div className="space-y-3">
        <div className="border border-destructive/30 bg-destructive/10 px-4 py-3">
          <div className="flex items-center gap-2 text-sm font-medium text-destructive"><AlertTriangle className="size-4" />Validation failed</div>
          <p className="mt-1 text-xs text-muted-foreground">{issues.length} issue{issues.length === 1 ? "" : "s"} found.</p>
        </div>
        <IssueList issues={issues} />
      </div>
    );
  }

  if (state === "validated" && parsedPlan) {
    return (
      <ArtifactIntentCapturePanel
        projectId={props.projectId} planJsonArtifactPath={props.planJsonArtifactPath}
        planJsonArtifactSha256={props.planJsonArtifactSha256} planMarkdownArtifactPath={props.planMarkdownArtifactPath}
        planMarkdownArtifactSha256={props.planMarkdownArtifactSha256} literalUserRequest={props.literalUserRequest}
        intentConstraintsText={props.intentConstraintsText} isCreating={false} canCreate={props.canCreate}
        parsedPlan={parsedPlan} settingsLoadState={props.settingsLoadState} settings={props.settings}
        onProjectIdChange={props.onProjectIdChange} onJsonPathChange={props.onJsonPathChange}
        onJsonSha256Change={props.onJsonSha256Change} onMarkdownPathChange={props.onMarkdownPathChange}
        onMarkdownSha256Change={props.onMarkdownSha256Change} onLiteralRequestChange={props.onLiteralRequestChange}
        onConstraintsChange={props.onConstraintsChange} onComputeHash={props.onComputeHash} onCreateAttempt={props.onCreateAttempt}
      />
    );
  }

  if (state === "submitted" && planAttempt) {
    return <SubmittedPanel planAttempt={planAttempt} />;
  }

  if (planAttempt) {
    const isAttemptBusy = ["review_running","approving_attempt","voiding_attempt","revising_attempt","submitting_approved_attempt"].includes(state);
    return (
      <div className="space-y-4">
        {actionError && (
          <div className="border border-destructive/30 bg-destructive/10 px-4 py-3">
            <div className="flex items-center gap-2 text-sm font-medium text-destructive"><AlertTriangle className="size-4" />Action failed</div>
            <p className="mt-1 text-xs text-muted-foreground">{actionError.message}</p>
            {actionError.issues.length > 0 && <div className="mt-2"><IssueList issues={actionError.issues} /></div>}
          </div>
        )}
        {isAttemptBusy && (
          <LoadingPanel label={
            state==="review_running"?"Running drift review...":
            state==="approving_attempt"?"Approving attempt...":
            state==="voiding_attempt"?"Voiding attempt...":
            state==="revising_attempt"?"Creating revision...":
            "Submitting approved attempt..."}
          />
        )}
        {!isAttemptBusy && (
          <>
            {revisionMode && (
              <div className="border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
                <span className="font-medium">Revision mode:</span> Edit the Plan JSON in the left editor, update artifact references, then create a revision attempt. This does not mutate the current attempt.
              </div>
            )}
            <AttemptMetadataPanel planAttempt={planAttempt} intentPacket={intentPacket} />
            {gate && <ReviewGatePanel gate={gate} />}
            {gate?.latestReview && <DriftFindingsPanel gate={gate} />}
            <ActionPanel
              gate={gate} state={state} revisionMode={revisionMode}
              modelCallConfirmed={props.modelCallConfirmed} forceHighAssurance={props.forceHighAssurance}
              jsonArtifactReviewed={props.jsonArtifactReviewed} noReviewAcknowledged={props.noReviewAcknowledged}
              driftAcknowledged={props.driftAcknowledged} submissionConfirmed={props.submissionConfirmed}
              onModelCallConfirmedChange={props.onModelCallConfirmedChange} onForceHighAssuranceChange={props.onForceHighAssuranceChange}
              onJsonArtifactReviewedChange={props.onJsonArtifactReviewedChange} onNoReviewAcknowledgedChange={props.onNoReviewAcknowledgedChange}
              onDriftAcknowledgedChange={props.onDriftAcknowledgedChange} onSubmissionConfirmedChange={props.onSubmissionConfirmedChange}
              onRunDriftReview={props.onRunDriftReview} onApprove={props.onApprove}
              onStartRevision={props.onStartRevision} onVoid={props.onVoid} onSubmitApprovedAttempt={props.onSubmitApprovedAttempt}
            />
            <SeparateDriftAuditPanel
              planAttempt={planAttempt} intentPacket={intentPacket} gate={gate}
              onRefreshGate={props.onRefreshGate} isRefreshing={props.isRefreshingGate}
            />
            <PlannerChatGuidancePanel planAttempt={planAttempt} intentPacket={intentPacket} />
            {revisionMode && (
              <ArtifactIntentCapturePanel
                projectId={planAttempt.projectId} planJsonArtifactPath={props.planJsonArtifactPath}
                planJsonArtifactSha256={props.planJsonArtifactSha256} planMarkdownArtifactPath={props.planMarkdownArtifactPath}
                planMarkdownArtifactSha256={props.planMarkdownArtifactSha256} literalUserRequest={props.literalUserRequest}
                intentConstraintsText={props.intentConstraintsText} isCreating={false} canCreate={props.canCreate}
                parsedPlan={parsedPlan} settingsLoadState={props.settingsLoadState} settings={props.settings}
                onProjectIdChange={props.onProjectIdChange} onJsonPathChange={props.onJsonPathChange}
                onJsonSha256Change={props.onJsonSha256Change} onMarkdownPathChange={props.onMarkdownPathChange}
                onMarkdownSha256Change={props.onMarkdownSha256Change} onLiteralRequestChange={props.onLiteralRequestChange}
                onConstraintsChange={props.onConstraintsChange} onComputeHash={props.onComputeHash} onCreateAttempt={props.onCreateAttempt}
              />
            )}
          </>
        )}
      </div>
    );
  }

  // Default / draft state
  return (
    <div className="space-y-4">
      <div className="border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-4 py-3">
        <div className="font-medium text-foreground">Validation not run</div>
        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">Paste reviewed Plan of Passes JSON and validate it. After validation, provide artifact references and intent capture to create a draft plan attempt.</p>
      </div>
      {["Plan JSON Validation","Artifact & Intent Capture","Review Gate","Drift Review","Approve / Submit"].map((label) => (
        <div key={label} className="border border-dashed border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3">
          <div className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">{label}</div>
          <p className="mt-1 text-xs text-muted-foreground">Available after validation.</p>
        </div>
      ))}
    </div>
  );
}

// ─── Main Workbench ─────────────────────────────────────────────────────────────────────────────────

export function RelayPlanSubmissionWorkbench() {
  const queryClient = useQueryClient();
  const [rawJson, setRawJson] = React.useState("");
  const [state, setState] = React.useState<AttemptReviewState>("draft");
  const [validatedRawJson, setValidatedRawJson] = React.useState<string | undefined>();
  const [validationResponse, setValidationResponse] = React.useState<ValidatePlanResponse | undefined>();
  const [parsedPlan, setParsedPlan] = React.useState<PlannerPassPlan | undefined>();
  const [issues, setIssues] = React.useState<PlanValidationIssue[]>([]);
  const [actionError, setActionError] = React.useState<ActionError | undefined>();
  const [projectId, setProjectId] = React.useState("");
  const [planJsonArtifactPath, setPlanJsonArtifactPath] = React.useState("");
  const [planJsonArtifactSha256, setPlanJsonArtifactSha256] = React.useState("");
  const [planMarkdownArtifactPath, setPlanMarkdownArtifactPath] = React.useState("");
  const [planMarkdownArtifactSha256, setPlanMarkdownArtifactSha256] = React.useState("");
  const [literalUserRequest, setLiteralUserRequest] = React.useState("");
  const [intentConstraintsText, setIntentConstraintsText] = React.useState("");
  const [planAttempt, setPlanAttempt] = React.useState<PlanAttemptAPI | undefined>();
  const [intentPacket, setIntentPacket] = React.useState<IntentPacketAPI | undefined>();
  const [gate, setGate] = React.useState<PlanAttemptReviewGateAPI | undefined>();
  const [settings, setSettings] = React.useState<PlanReviewSettingsAPI | undefined>();
  const [settingsProjectId, setSettingsProjectId] = React.useState<string | undefined>();
  const [settingsLoadState, setSettingsLoadState] = React.useState<SettingsLoadState>("idle");
  const [modelCallConfirmed, setModelCallConfirmed] = React.useState(false);
  const [forceHighAssurance, setForceHighAssurance] = React.useState(false);
  const [jsonArtifactReviewed, setJsonArtifactReviewed] = React.useState(false);
  const [noReviewAcknowledged, setNoReviewAcknowledged] = React.useState(false);
  const [driftAcknowledged, setDriftAcknowledged] = React.useState(false);
  const [submissionConfirmed, setSubmissionConfirmed] = React.useState(false);
  const [revisionMode, setRevisionMode] = React.useState(false);
  const [isRefreshingGate, setIsRefreshingGate] = React.useState(false);

  // Load review settings whenever projectId changes. Settings are required before
  // create/revision to prevent silent project policy override.
  React.useEffect(() => {
    const pid = projectId.trim();
    if (!pid) {
      setSettings(undefined);
      setSettingsProjectId(undefined);
      setSettingsLoadState("idle");
      return;
    }
    let cancelled = false;
    setSettingsLoadState("loading");
    setSettings(undefined);
    setSettingsProjectId(undefined);
    void getPlanReviewSettings(pid)
      .then((response) => {
        if (cancelled) return;
        if (response.success && response.settings) {
          setSettings(response.settings);
          setSettingsProjectId(pid);
          setSettingsLoadState("loaded");
        } else {
          setSettingsLoadState("failed");
        }
      })
      .catch(() => {
        if (!cancelled) setSettingsLoadState("failed");
      });
    return () => { cancelled = true; };
  }, [projectId]);

  const trimmedJson = rawJson.trim();
  const lineCount = getEditorLineCount(rawJson);
  const isBusy = ["validating","creating_attempt","review_running","approving_attempt","voiding_attempt","revising_attempt","submitting_approved_attempt"].includes(state);
  const canValidate = trimmedJson.length > 0 && !isBusy;

  const trimmedProjectId = projectId.trim();
  const hasCurrentProjectSettings =
    settingsLoadState === "loaded" &&
    settingsProjectId === trimmedProjectId &&
    !!settings;
  const canCreate =
    trimmedProjectId.length > 0 &&
    hasCurrentProjectSettings &&
    planJsonArtifactPath.trim().length > 0 &&
    planJsonArtifactSha256.trim().length > 0 &&
    literalUserRequest.trim().length > 0 &&
    (state === "validated" || revisionMode);

  const prefillFromPlan = React.useCallback((plan: PlannerPassPlan) => {
    const pid = plan.plan_meta.project_id ?? plan.plan_meta.projectId ?? "";
    if (pid) setProjectId(pid);
    if (plan.source_intent.summary) setLiteralUserRequest((prev) => prev.trim() ? prev : plan.source_intent.summary);
  }, []);

  const resetResultState = React.useCallback(() => {
    setValidationResponse(undefined); setParsedPlan(undefined); setIssues([]);
    setValidatedRawJson(undefined); setActionError(undefined);
  }, []);

  const handleEditorChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => {
    setRawJson(event.target.value);
    if (state !== "draft" && state !== "validating" && !planAttempt) {
      setState("draft"); resetResultState();
    }
  };

  const clearEditor = () => {
    setRawJson(""); setState("draft"); setPlanAttempt(undefined); setIntentPacket(undefined);
    setGate(undefined); setSettings(undefined); setRevisionMode(false); resetResultState();
  };

  const handleValidate = async () => {
    const parsed = parsePlanJson(rawJson);
    setActionError(undefined);
    if (!parsed.ok) {
      setState("parse_failed"); setIssues([parsed.issue]); setValidationResponse(undefined);
      setParsedPlan(undefined); setValidatedRawJson(undefined); return;
    }
    setState("validating"); setIssues([]);
    try {
      const response = await validatePlan({ plan: parsed.plan });
      setValidationResponse(response);
      if (response.validation.valid) {
        setState("validated"); setValidatedRawJson(rawJson); setParsedPlan(parsed.plan);
        setIssues(response.validation.issues); prefillFromPlan(parsed.plan);
        // Settings are loaded reactively by the projectId effect; no inline fetch needed.
      } else {
        setState("validation_failed"); setValidatedRawJson(undefined); setParsedPlan(undefined); setIssues(response.validation.issues);
      }
    } catch (error) {
      const nextError = getActionError(error);
      setState("validation_failed");
      setIssues(nextError.issues.length > 0 ? nextError.issues : [{ severity: "error", code: "validation_request_failed", path: "root", message: nextError.message }]);
      setValidatedRawJson(undefined); setParsedPlan(undefined);
    }
  };

  // REQ-001: Parse current rawJson — do not use stale parsedPlan.
  const handleComputeHash = async () => {
    const parsed = parsePlanJson(rawJson);
    if (!parsed.ok) {
      setState("parse_failed");
      setIssues([parsed.issue]);
      setActionError(undefined);
      return;
    }
    try {
      const hash = await computePlanJsonSha256(parsed.plan);
      setPlanJsonArtifactSha256(hash);
      setParsedPlan(parsed.plan);
    } catch {
      setActionError({ message: "Unable to compute Plan JSON hash in this browser.", issues: [] });
    }
  };

  const refreshGate = async (pid: string, attemptId: string): Promise<PlanAttemptReviewGateAPI | undefined> => {
    try {
      const response = await getPlanAttemptReviewGate(pid, attemptId);
      if (response.success && response.reviewGate) { setGate(response.reviewGate); return response.reviewGate; }
    } catch { /* keep current */ }
    return undefined;
  };

  // REQ-002/003/004/005: Parse current rawJson immediately; no stale parsedPlan; no fallback mode/tier.
  const handleCreateAttempt = async () => {
    if (!canCreate) return;

    // Always parse the current editor JSON — never reuse potentially stale parsedPlan.
    const parsed = parsePlanJson(rawJson);
    if (!parsed.ok) {
      setState("parse_failed");
      setIssues([parsed.issue]);
      setActionError(undefined);
      return;
    }
    const currentPlan = parsed.plan;
    // Keep cached display state in sync with what we're about to submit.
    setParsedPlan(currentPlan);
    setValidatedRawJson(rawJson);

    const constraints = splitConstraints(intentConstraintsText);
    // REQ-003: In revision mode use attempt's project ID so the input field cannot redirect to a different project.
    const pid = revisionMode && planAttempt ? planAttempt.projectId : trimmedProjectId;
    if (!pid) {
      setActionError({ message: "Project ID is required.", issues: [] });
      return;
    }

    // REQ-004: Only include policy fields when settings are loaded for the exact current project.
    const policyFields = hasCurrentProjectSettings && settings
      ? { driftReviewMode: settings.driftReviewMode, modelTier: settings.modelTier }
      : {};

    const revisionRequest = {
      planArtifactRef: { path: planJsonArtifactPath.trim(), sha256: planJsonArtifactSha256.trim(), artifactKind: "planner-pass-plan-json" as const },
      optionalMarkdownRef: planMarkdownArtifactPath.trim() && planMarkdownArtifactSha256.trim()
        ? { path: planMarkdownArtifactPath.trim(), sha256: planMarkdownArtifactSha256.trim(), artifactKind: "planner-pass-plan-markdown" as const }
        : undefined,
      rawPlanJson: { content: currentPlan, contentHash: planJsonArtifactSha256.trim() },
      intentPacket: {
        summary: currentPlan.source_intent.summary,
        literalUserRequest: literalUserRequest.trim(),
        constraints,
        source: { capturedFrom: "planner_chat" as const, capturedBy: "relay-plan-review-ui", sourceArtifactPath: planJsonArtifactPath.trim() },
        redactionStatus: "verified_no_secrets" as const,
      },
    };

    if (revisionMode && planAttempt) {
      // REQ-003/005: Validate current editor plan before mutating.
      let validationResult;
      try {
        validationResult = await validatePlan({ plan: currentPlan });
      } catch (error) {
        setActionError(getActionError(error));
        return;
      }
      if (!validationResult.validation.valid) {
        setState("validation_failed");
        setIssues(validationResult.validation.issues);
        return;
      }
      setState("revising_attempt"); setActionError(undefined);
      try {
        const response = await revisePlanAttempt(pid, planAttempt.planAttemptId, revisionRequest);
        if (response.planAttempt) {
          setPlanAttempt(response.planAttempt); if (response.intentPacket) setIntentPacket(response.intentPacket);
          if (response.reviewGate) setGate(response.reviewGate); setRevisionMode(false); setState("attempt_ready");
          setModelCallConfirmed(false); setJsonArtifactReviewed(false); setNoReviewAcknowledged(false);
          setDriftAcknowledged(false); setSubmissionConfirmed(false);
          await refreshGate(pid, response.planAttempt.planAttemptId);
        } else { setActionError({ message: response.message ?? "Revision failed.", issues: [] }); setState("action_failed"); }
      } catch (error) { setActionError(getActionError(error)); setState("action_failed"); }
      return;
    }

    const createRequest = { ...revisionRequest, ...policyFields };
    setState("creating_attempt"); setActionError(undefined);
    try {
      const response = await createPlanAttemptWithIntent(pid, createRequest);
      if (response.planAttempt) {
        setPlanAttempt(response.planAttempt); if (response.intentPacket) setIntentPacket(response.intentPacket);
        if (response.reviewGate) setGate(response.reviewGate); setState("attempt_ready");
        void queryClient.invalidateQueries({ queryKey: relayPlanKeys.all });
        await refreshGate(pid, response.planAttempt.planAttemptId);
      } else { setActionError({ message: response.message ?? "Attempt creation failed.", issues: [] }); setState("validated"); }
    } catch (error) { setActionError(getActionError(error)); setState("validated"); }
  };

  const handleRunDriftReview = async (fha?: boolean) => {
    if (!planAttempt) return;
    const pid = planAttempt.projectId;
    setState("review_running"); setActionError(undefined);
    try {
      await runPlanAttemptDriftReview(pid, planAttempt.planAttemptId, { allowModelCall: true, forceHighAssurance: fha ?? false });
      setState("attempt_ready"); await refreshGate(pid, planAttempt.planAttemptId);
    } catch (error) { setActionError(getActionError(error)); setState("action_failed"); await refreshGate(pid, planAttempt.planAttemptId); }
  };

  const handleApprove = async () => {
    if (!planAttempt || !gate) return;
    const pid = planAttempt.projectId;
    const approveKind = canApprove(gate);
    if (!approveKind) return;
    setState("approving_attempt"); setActionError(undefined);
    try {
      const response = await approvePlanAttempt(pid, planAttempt.planAttemptId, {
        approved: true,
        acceptedDriftReviewId: gate.latestReview?.intentDriftReviewId,
        driftAcknowledged: approveKind === "drift_ack",
        noDriftReviewAcknowledged: approveKind === "no_review_ack",
      });
      if (response.planAttempt) setPlanAttempt(response.planAttempt);
      setState("attempt_ready"); await refreshGate(pid, planAttempt.planAttemptId);
    } catch (error) { setActionError(getActionError(error)); setState("action_failed"); await refreshGate(pid, planAttempt.planAttemptId); }
  };

  const handleVoid = async () => {
    if (!planAttempt) return;
    const confirmed = window.confirm(`Void plan attempt ${planAttempt.planAttemptId}? This cannot be undone.`);
    if (!confirmed) return;
    const pid = planAttempt.projectId;
    setState("voiding_attempt"); setActionError(undefined);
    try {
      // step-004: Apply backend-returned attempt to clear stale metadata.
      const response = await voidPlanAttempt(pid, planAttempt.planAttemptId);
      if (response.planAttempt) setPlanAttempt(response.planAttempt);
      setState("attempt_ready"); await refreshGate(pid, planAttempt.planAttemptId);
    } catch (error) { setActionError(getActionError(error)); setState("action_failed"); }
  };

  const handleStartRevision = () => {
    setRevisionMode(true); setModelCallConfirmed(false); setJsonArtifactReviewed(false);
    setNoReviewAcknowledged(false); setDriftAcknowledged(false); setSubmissionConfirmed(false);
  };

  const handleSubmitApprovedAttempt = async () => {
    if (!planAttempt || !gate || !canSubmitAttempt(gate)) return;
    const pid = planAttempt.projectId;
    setState("submitting_approved_attempt"); setActionError(undefined);
    try {
      const response = await submitPlanAttempt(pid, planAttempt.planAttemptId, {
        submissionConfirmed: true,
        reviewedPlanJsonArtifactSha256: planAttempt.planJsonArtifactSha256,
        acceptedDriftReviewId: planAttempt.acceptedDriftReviewId ?? gate.acceptedDriftReviewId,
      });
      if (response.planAttempt) setPlanAttempt(response.planAttempt);
      setState("submitted");
      void queryClient.invalidateQueries({ queryKey: relayPlanKeys.all });
    } catch (error) { setActionError(getActionError(error)); setState("action_failed"); await refreshGate(pid, planAttempt.planAttemptId); }
  };

  const handleRefreshGate = async () => {
    if (!planAttempt) return;
    setIsRefreshingGate(true);
    try { await refreshGate(planAttempt.projectId, planAttempt.planAttemptId); } finally { setIsRefreshingGate(false); }
  };

  const gutterLines = React.useMemo(() => Array.from({ length: lineCount }, (_, i) => i + 1), [lineCount]);
  const hasActiveAttempt = !!planAttempt && state !== "submitted";
  const editorEditWarning = hasActiveAttempt && rawJson !== validatedRawJson && !revisionMode;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-[var(--relay-content-bg)]">
      <div className="shrink-0 border-b border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FileJson className="size-4 text-[var(--relay-accent)]" />
            <span>Plan attempts require artifact references. Submission creates managed plan and pass records only after approval.</span>
          </div>
          <Badge variant={getStateBadgeVariant(state)} className="h-auto rounded-sm px-2 py-0.5 text-[10px]">{STATE_LABELS[state]}</Badge>
        </div>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto lg:grid-cols-[minmax(26rem,34rem)_minmax(0,1fr)] lg:overflow-hidden">
        <section className="flex min-h-[34rem] flex-col border-b border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] lg:min-h-0 lg:border-b-0 lg:border-r">
          <div className="flex shrink-0 items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">Plan of Passes JSON</h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">Reviewed structured Planner Pass Plan artifact</p>
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Badge variant="outline" className="rounded-sm font-mono text-[10px]">application/json</Badge>
              <Badge variant={getStateBadgeVariant(state)} className="rounded-sm text-[10px]">{STATE_LABELS[state]}</Badge>
            </div>
          </div>
          {editorEditWarning && (
            <div className="shrink-0 border-b border-warning/30 bg-warning/10 px-4 py-2">
              <p className="text-[11px] text-warning">
                Editing the JSON creates a new revision attempt; it does not mutate the current attempt. Use{" "}
                <button type="button" onClick={handleStartRevision} className="underline hover:no-underline">Start Revision</button>{" "}when ready.
              </p>
            </div>
          )}
          <div className="min-h-0 flex-1 overflow-hidden">
            <div className="flex h-full min-h-[24rem] overflow-hidden">
              <div className="w-12 shrink-0 overflow-hidden border-r border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] px-2 py-3 text-right font-mono text-[11px] leading-5 text-muted-foreground">
                {gutterLines.map((line) => <div key={line} className="h-5">{line}</div>)}
              </div>
              <textarea value={rawJson} onChange={handleEditorChange} placeholder={EXAMPLE_PLAN_JSON} spellCheck={false}
                className={cn("h-full min-h-[24rem] flex-1 resize-none border-0 bg-[var(--relay-content-bg)] px-3 py-3 font-mono text-[12px] leading-5 text-foreground outline-none placeholder:text-muted-foreground/70 focus:ring-0", isBusy && "opacity-80")}
              />
            </div>
          </div>
          <div className="shrink-0 border-t border-[var(--relay-row-border)] px-4 py-3">
            <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted-foreground">
              <span className="font-mono">{rawJson.length} chars / {lineCount} lines</span>
              <span>{trimmedJson.length === 0 ? "Empty draft" : STATE_LABELS[state]}</span>
            </div>
            <div className="grid gap-2">
              <div className="grid grid-cols-2 gap-2">
                <Button type="button" variant="outline" size="sm" disabled={!canValidate} onClick={handleValidate}>
                  {state === "validating" ? <Loader2 className="size-3.5 animate-spin" /> : <CheckCircle2 className="size-3.5" />}Validate Plan
                </Button>
                <Button type="button" variant="outline" size="sm" disabled={isBusy || rawJson.length === 0} onClick={clearEditor}>
                  <Trash2 className="size-3.5" />Clear
                </Button>
              </div>
            </div>
          </div>
        </section>
        <section className="min-h-[34rem] overflow-y-auto bg-[var(--relay-page-body-bg)] lg:min-h-0">
          <div className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-[var(--relay-row-border)] bg-[var(--relay-page-header-bg)] px-4 py-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">
                {planAttempt ? (revisionMode ? "Revision" : "Review Workflow") : (state === "validated" ? "Artifact & Intent Capture" : "Validation")}
              </h2>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                {planAttempt ? `Attempt ${planAttempt.planAttemptId}` : "Validate before creating a draft attempt."}
              </p>
            </div>
            <Badge variant={getStateBadgeVariant(state)} className="rounded-sm text-[10px]">{STATE_LABELS[state]}</Badge>
          </div>
          <div className="p-4">
            <RightPane
              state={state} parsedPlan={parsedPlan} issues={issues} actionError={actionError}
              planAttempt={planAttempt} intentPacket={intentPacket} gate={gate} settings={settings}
              revisionMode={revisionMode} projectId={projectId} planJsonArtifactPath={planJsonArtifactPath}
              planJsonArtifactSha256={planJsonArtifactSha256} planMarkdownArtifactPath={planMarkdownArtifactPath}
              planMarkdownArtifactSha256={planMarkdownArtifactSha256} literalUserRequest={literalUserRequest}
              intentConstraintsText={intentConstraintsText} canCreate={canCreate}
              settingsLoadState={settingsLoadState}
              onProjectIdChange={setProjectId} onJsonPathChange={setPlanJsonArtifactPath}
              onJsonSha256Change={setPlanJsonArtifactSha256} onMarkdownPathChange={setPlanMarkdownArtifactPath}
              onMarkdownSha256Change={setPlanMarkdownArtifactSha256} onLiteralRequestChange={setLiteralUserRequest}
              onConstraintsChange={setIntentConstraintsText} onComputeHash={handleComputeHash}
              onCreateAttempt={handleCreateAttempt} modelCallConfirmed={modelCallConfirmed}
              forceHighAssurance={forceHighAssurance} jsonArtifactReviewed={jsonArtifactReviewed}
              noReviewAcknowledged={noReviewAcknowledged} driftAcknowledged={driftAcknowledged}
              submissionConfirmed={submissionConfirmed} isRefreshingGate={isRefreshingGate}
              onModelCallConfirmedChange={setModelCallConfirmed} onForceHighAssuranceChange={setForceHighAssurance}
              onJsonArtifactReviewedChange={setJsonArtifactReviewed} onNoReviewAcknowledgedChange={setNoReviewAcknowledged}
              onDriftAcknowledgedChange={setDriftAcknowledged} onSubmissionConfirmedChange={setSubmissionConfirmed}
              onRunDriftReview={handleRunDriftReview} onApprove={handleApprove}
              onStartRevision={handleStartRevision} onVoid={handleVoid}
              onSubmitApprovedAttempt={handleSubmitApprovedAttempt} onRefreshGate={handleRefreshGate}
              validationResponse={validationResponse}
            />
          </div>
        </section>
      </div>
    </div>
  );
}
