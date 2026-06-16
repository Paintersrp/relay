import { cn } from '@/lib/utils';
export const Card=({className,...p}:React.HTMLAttributes<HTMLDivElement>)=><div className={cn('rounded-xl border border-slate-800 bg-slate-950/70 shadow',className)} {...p}/>;
export const CardHeader=({className,...p}:React.HTMLAttributes<HTMLDivElement>)=><div className={cn('space-y-1.5 p-5',className)} {...p}/>;
export const CardTitle=({className,...p}:React.HTMLAttributes<HTMLHeadingElement>)=><h3 className={cn('text-lg font-semibold',className)} {...p}/>;
export const CardDescription=({className,...p}:React.HTMLAttributes<HTMLParagraphElement>)=><p className={cn('text-sm text-slate-400',className)} {...p}/>;
export const CardContent=({className,...p}:React.HTMLAttributes<HTMLDivElement>)=><div className={cn('p-5 pt-0',className)} {...p}/>;
