import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
export function LogPreviewPanel({logs}:{logs:string[]}){return <Card><CardHeader><CardTitle>Live Logs</CardTitle></CardHeader><CardContent><pre className="overflow-auto rounded-md bg-black p-4 text-xs text-emerald-200">{logs.join('\n')}</pre></CardContent></Card>}
