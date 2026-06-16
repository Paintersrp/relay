import { cn } from '@/lib/utils'; export const Skeleton=({className,...p}:React.HTMLAttributes<HTMLDivElement>)=><div className={cn('animate-pulse rounded-md bg-slate-800',className)} {...p}/>;
