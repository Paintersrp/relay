import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { RelayApprovalGate } from '@/features/relay-runs'
import { ShieldCheck, ShieldX, Clock, SkipForward, ThumbsUp, ThumbsDown } from 'lucide-react'
import { cn } from '@/lib/utils'

const GATE_ICON: Record<RelayApprovalGate['state'], React.ReactNode> = {
  pending: <Clock className="w-4 h-4 text-yellow-400" />,
  approved: <ShieldCheck className="w-4 h-4 text-emerald-400" />,
  rejected: <ShieldX className="w-4 h-4 text-red-400" />,
  skipped: <SkipForward className="w-4 h-4 text-muted-foreground" />,
}

const GATE_VARIANT: Record<RelayApprovalGate['state'], 'warning' | 'success' | 'destructive' | 'secondary'> = {
  pending: 'warning',
  approved: 'success',
  rejected: 'destructive',
  skipped: 'secondary',
}

const GATE_LABEL: Record<RelayApprovalGate['state'], string> = {
  pending: 'Awaiting Decision',
  approved: 'Approved',
  rejected: 'Rejected',
  skipped: 'Skipped',
}

interface ApprovalCardProps {
  gate: RelayApprovalGate
  className?: string
}

export function ApprovalCard({ gate, className }: ApprovalCardProps) {
  return (
    <Card className={cn('border-border/60', className)}>
      <CardHeader className="p-3 pb-2">
        <CardTitle className="text-sm font-medium flex items-center gap-2">
          {GATE_ICON[gate.state]}
          {gate.label}
          <Badge variant={GATE_VARIANT[gate.state]} className="ml-auto text-xs">
            {GATE_LABEL[gate.state]}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0 flex flex-col gap-2">
        {gate.note && (
          <p className="text-xs text-muted-foreground/80 italic">{gate.note}</p>
        )}
        {gate.state === 'pending' && (
          <>
            <div className="flex items-center gap-2 mt-1">
              <Button
                variant="outline"
                size="sm"
                disabled
                className="gap-1.5 opacity-50 cursor-not-allowed"
                title="Approval submission is not available yet"
              >
                <ThumbsUp className="w-3.5 h-3.5" />
                Approve
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled
                className="gap-1.5 opacity-50 cursor-not-allowed text-destructive/60"
                title="Rejection is not available yet"
              >
                <ThumbsDown className="w-3.5 h-3.5" />
                Reject
              </Button>
            </div>
            <p className="text-xs text-muted-foreground/60">
              Approval gate actions are read-only until submission wiring is available.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  )
}
