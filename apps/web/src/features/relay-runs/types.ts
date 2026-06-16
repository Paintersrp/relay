export type RelayRunStepKey = 'intake' | 'prepare' | 'execute' | 'audit';
export interface RelayRunStepDefinition { key: RelayRunStepKey; label: string; description: string; }
export const RELAY_RUN_STEPS: RelayRunStepDefinition[] = [
{key:'intake',label:'Intake / Configure',description:'Review incoming handoff, parsed metadata, run config, repo preflight, and intake validation.'},
{key:'prepare',label:'Compile / Render',description:'Compile the canonical packet, validate it, render the executor brief, and approve agent dispatch.'},
{key:'execute',label:'Execute',description:'Monitor the coding agent, validation output, changed files, and final executor result.'},
{key:'audit',label:'Audit / Close',description:'Review the audit packet, approve closeout, and prepare commit/push.'},
];
export type RelayRunState = 'intake_received'|'intake_validation_failed'|'intake_needs_review'|'approved_for_prepare'|'packet_compiling'|'packet_validation_failed'|'packet_repairing'|'packet_validated'|'brief_rendering'|'brief_validation_failed'|'brief_ready_for_review'|'approved_for_executor'|'executor_running'|'executor_blocked'|'executor_done'|'audit_running'|'audit_ready_for_review'|'audit_rejected'|'revision_required'|'approved_to_commit'|'accepted'|'accepted_with_warnings'|'blocked'|'done';
export type RelayRunStatusSeverity = 'neutral'|'info'|'success'|'warning'|'danger';
export interface RelayRunSummary { id:string; title:string; repo:string; branch?:string; model?:string; state:RelayRunState; activeStep:RelayRunStepKey; statusSeverity:RelayRunStatusSeverity; createdAt:string; updatedAt:string; }
export interface RelayRunArtifactPreview { kind:string; filename:string; status:'missing'|'ready'|'warning'|'blocked'; preview?:string; }
export interface RelayRunValidationItem { label:string; status:'pass'|'warning'|'fail'|'pending'|'not_run'; message?:string; }
export interface RelayRunDetail extends RelayRunSummary { artifacts: RelayRunArtifactPreview[]; validations: RelayRunValidationItem[]; logs: string[]; }
