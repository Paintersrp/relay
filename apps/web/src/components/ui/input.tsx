import { cn } from '@/lib/utils';
export const Input=({className,...p}:React.InputHTMLAttributes<HTMLInputElement>)=><input className={cn('w-full rounded-md border border-slate-800 bg-slate-950 px-3 py-2 text-sm outline-none focus:border-cyan-500',className)} {...p}/>;
