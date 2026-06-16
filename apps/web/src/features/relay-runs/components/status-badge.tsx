import { Badge } from '@/components/ui/badge'; import type { RelayRunStatusSeverity } from '../types';
const tone:Record<RelayRunStatusSeverity,string>={neutral:'border-slate-700',info:'border-cyan-700 text-cyan-200',success:'border-emerald-700 text-emerald-200',warning:'border-amber-700 text-amber-200',danger:'border-red-700 text-red-200'};
export function StatusBadge({severity,children}:{severity:RelayRunStatusSeverity;children:React.ReactNode}){return <Badge className={tone[severity]}>{children}</Badge>}
