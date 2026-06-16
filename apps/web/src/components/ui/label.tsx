import { cn } from '@/lib/utils';
export const Label=({className,...p}:React.LabelHTMLAttributes<HTMLLabelElement>)=><label className={cn('text-sm font-medium text-slate-200',className)} {...p}/>;
