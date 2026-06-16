import * as React from 'react'
import { Link } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Zap, List, PlusCircle } from 'lucide-react'

interface AppShellProps {
  children: React.ReactNode
  className?: string
}

export function AppShell({ children, className }: AppShellProps) {
  return (
    <div className={cn('flex flex-col min-h-screen bg-background text-foreground', className)}>
      {/* Top navigation bar */}
      <header className="flex items-center h-12 px-4 border-b border-border/60 bg-muted/30 shrink-0">
        <Link
          to="/"
          className="flex items-center gap-2 font-semibold text-sm hover:text-foreground text-foreground transition-colors mr-4"
        >
          <Zap className="w-4 h-4 text-primary" />
          <span className="tracking-tight">Relay</span>
          <span className="text-muted-foreground font-normal text-xs ml-0.5">workbench</span>
        </Link>

        <Separator orientation="vertical" className="h-5 mx-3" />

        <nav className="flex items-center gap-1">
          <Button variant="ghost" size="sm" asChild className="text-xs h-7 gap-1.5">
            <Link to="/runs">
              <List className="w-3.5 h-3.5" />
              Runs
            </Link>
          </Button>
          <Button variant="ghost" size="sm" asChild className="text-xs h-7 gap-1.5">
            <Link to="/runs/new">
              <PlusCircle className="w-3.5 h-3.5" />
              New Run
            </Link>
          </Button>
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <span className="text-xs text-muted-foreground font-mono">
            Go :{' '}
            <span className="text-muted-foreground/60">8080</span>
            {' '}/{' '}
            React :{' '}
            <span className="text-muted-foreground/60">3000</span>
          </span>
        </div>
      </header>

      {/* Page content */}
      <main className="flex-1 flex flex-col overflow-hidden">
        {children}
      </main>
    </div>
  )
}
