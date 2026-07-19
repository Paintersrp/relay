import { requestWorkflowJson } from "@/features/workflow-api";

export interface CutoverState {
  active: boolean;
  state?: {
    activationId: string;
    status: string;
    boundaryStatus: string;
    rollbackStatus: string;
    rollForwardStatus: string;
    activatedAt?: string;
  };
}

export interface CutoverReadiness {
  ready: boolean;
  prepared: boolean;
  active: boolean;
  boundaryCrossed: boolean;
  prerequisites: string[];
  obligations: string[];
  rollForwardCriteria: string[];
  evidence: Array<{ prerequisite: string; evidence: string }>;
  activationEvidence: Array<{ kind: string; obligation: string; evidence: string }>;
}

export async function getCutoverState(): Promise<CutoverState> {
  const path = "/api/cutover/state";
  return requestWorkflowJson<CutoverState>("GET", path);
}

export async function getCutoverReadiness(activationId: string): Promise<{ readiness: CutoverReadiness }> {
  const path = `/api/cutover/activations/${encodeURIComponent(activationId)}/readiness`;
  return requestWorkflowJson<{ readiness: CutoverReadiness }>("GET", path);
}

export async function getCutoverHistory(): Promise<{ items: Array<{ activationId: string; status: string; boundaryStatus: string; activatedAt?: string }>; count: number }> {
  const path = "/api/cutover/history";
  return requestWorkflowJson<{ items: Array<{ activationId: string; status: string; boundaryStatus: string; activatedAt?: string }>; count: number }>("GET", path);
}

export async function activateCutover(activationId: string): Promise<{ activation: unknown }> {
  const path = "/api/cutover/activate";
  return requestWorkflowJson<{ activation: unknown }>("POST", path, { activationId });
}

export async function rollbackCutover(activationId: string): Promise<{ activation: unknown }> {
  const path = "/api/cutover/rollback";
  return requestWorkflowJson<{ activation: unknown }>("POST", path, { activationId });
}

export async function recordRollForwardEvidence(activationId: string, criterionSequence: number, evidence: string): Promise<{ recorded: boolean }> {
  const path = "/api/cutover/roll-forward-evidence";
  return requestWorkflowJson<{ recorded: boolean }>("POST", path, { activationId, criterionSequence, evidence });
}
