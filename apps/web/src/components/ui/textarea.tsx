import { cn } from '@/lib/utils';
export const Textarea=({className,...p}:React.TextareaHTMLAttributes<HTMLTextAreaElement>)=><textarea className={cn('min-h-36 w-full rounded-md border border-slate-800 bg-slate-950 px-3 py-2 text-sm outline-none focus:border-cyan-500',className)} {...p}/>;
