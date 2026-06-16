import { getMockRun, mockRuns } from './mock-data';
const API_BASE_URL = import.meta.env.VITE_RELAY_API_BASE_URL ?? 'http://localhost:8080';
export { API_BASE_URL };
export async function listRuns(){ return mockRuns; }
export async function getRun(runId: string){ return getMockRun(runId); }
const notImplemented=()=>{ throw new Error('Relay API endpoint is not implemented in this scaffold pass.'); };
export const submitPlannerHandoff=notImplemented; export const approveIntake=notImplemented; export const startPrepare=notImplemented; export const approveExecutorBrief=notImplemented; export const startExecutor=notImplemented; export const requestAudit=notImplemented; export const approveCloseout=notImplemented;
