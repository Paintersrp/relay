import { cn } from '@/lib/utils';
export function Badge({className,...p}:React.HTMLAttributes<HTMLSpanElement>){return <span className={cn('inline-flex rounded-full border border-slate-700 px-2 py-0.5 text-xs font-medium text-slate-200',className)} {...p}/>}
