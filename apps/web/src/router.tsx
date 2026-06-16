import { formatDistanceToNow } from 'date-fns';
import { AppShell } from './features/relay-runs/components/app-shell';
import { mockRuns, getMockRun } from './features/relay-runs/mock-data';
import { RELAY_RUN_STEPS, type RelayRunStepKey } from './features/relay-runs/types';
import { RunWorkbenchLayout } from './features/relay-runs/components/run-workbench-layout';
import { Button } from './components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './components/ui/card';
import { Input } from './components/ui/input';
import { Label } from './components/ui/label';
import { Textarea } from './components/ui/textarea';
import { Table, TBody, TD, TH, THead, TR } from './components/ui/table';
import { StatusBadge } from './features/relay-runs/components/status-badge';
import { Alert } from './components/ui/alert';

function RunsPage(){return <AppShell><div className="mb-6 flex items-center justify-between"><div><h2 className="text-3xl font-bold">Runs</h2><p className="text-slate-400">Mock Relay run list for the frontend prototype.</p></div><a href="/runs/new"><Button>New Run</Button></a></div><Card><CardContent className="pt-5"><Table><THead><TR><TH>Run</TH><TH>Repo</TH><TH>Branch</TH><TH>Model</TH><TH>State</TH><TH>Active step</TH><TH>Updated</TH><TH /></TR></THead><TBody>{mockRuns.map(r=><TR key={r.id}><TD className="font-medium">{r.title}</TD><TD>{r.repo}</TD><TD>{r.branch}</TD><TD>{r.model}</TD><TD><StatusBadge severity={r.statusSeverity}>{r.state}</StatusBadge></TD><TD>{RELAY_RUN_STEPS.find(s=>s.key===r.activeStep)?.label}</TD><TD>{formatDistanceToNow(new Date(r.updatedAt),{addSuffix:true})}</TD><TD><a href={`/runs/${r.id}/${r.activeStep}`}><Button variant="outline">Open</Button></a></TD></TR>)}</TBody></Table></CardContent></Card></AppShell>}
function NewRunPage(){return <AppShell><div className="mb-6"><h2 className="text-3xl font-bold">New Run</h2><p className="text-slate-400">Manual planner handoff intake scaffold.</p></div><Card className="max-w-3xl"><CardHeader><CardTitle>Planner handoff</CardTitle><CardDescription>Real MCP/manual intake endpoints come in a later pass.</CardDescription></CardHeader><CardContent className="grid gap-4"><Label>Task title<Input placeholder="Implement Relay feature" className="mt-2"/></Label><Label>Repo target<Input placeholder="Paintersrp/relay" className="mt-2"/></Label><Label>Model<select className="mt-2 w-full rounded-md border border-slate-800 bg-slate-950 px-3 py-2"><option>GPT-5.5</option><option>GPT-5.4</option></select></Label><Label>Planner handoff<Textarea placeholder="Paste surgical implementation handoff..." className="mt-2"/></Label><Alert>This prototype uses mock data only and does not submit to the Relay backend yet.</Alert><Button disabled>Submit disabled in scaffold</Button></CardContent></Card></AppShell>}
function WorkbenchPage({runId,step}:{runId:string;step:RelayRunStepKey}){return <AppShell><RunWorkbenchLayout run={getMockRun(runId)} step={step}/></AppShell>}
export function Router(){const path=window.location.pathname; if(path==='/') return <RunsPage/>; if(path==='/runs') return <RunsPage/>; if(path==='/runs/new') return <NewRunPage/>; const m=path.match(/^\/runs\/([^/]+)\/(intake|prepare|execute|audit)$/); if(m) return <WorkbenchPage runId={m[1]} step={m[2] as RelayRunStepKey}/>; return <AppShell><Card><CardHeader><CardTitle>Not found</CardTitle></CardHeader><CardContent><a href="/runs"><Button>Back to runs</Button></a></CardContent></Card></AppShell>}
