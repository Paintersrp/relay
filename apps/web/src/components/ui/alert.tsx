import { cn } from '@/lib/utils';
export const Alert=({className,...p}:React.HTMLAttributes<HTMLDivElement>)=><div className={cn('rounded-lg border border-cyan-900 bg-cyan-950/30 p-4 text-sm text-cyan-100',className)} {...p}/>;
