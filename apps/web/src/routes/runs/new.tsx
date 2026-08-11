import { Link, createFileRoute } from "@tanstack/react-router";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/runs/new")({ component: NewRunPage });

function NewRunPage() {
  return <AppPageFrame title="New Run" description="Runs are created by approving an exact selected package."><div className="p-6"><p className="text-sm text-muted-foreground">Authored Execution Spec submission is retired. Prepare the selected approved Delivery Ticket document package with optional Deterministic Operations, then approve it to create the linked setup-ready Run.</p><Button asChild className="mt-4"><Link to="/execution-packages/new">Prepare selected package</Link></Button></div></AppPageFrame>;
}
