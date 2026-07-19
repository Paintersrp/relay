import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RelayApiError } from "@/features/workflow-api";
import { mutationLeaseQueryOptions, packageKeys, reconcileMutationLease } from "@/features/relay-packages";

const operatorOperation = "local_operator.ticket_workflow";

export function RelayMutationLeaseStatus({ runId }: { runId: string }) {
  const queryClient = useQueryClient();
  const query = useQuery(mutationLeaseQueryOptions(runId));
  const [packetId, setPacketId] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const lease = query.data;
  const mutation = useMutation({
    mutationFn: () => {
      if (!lease) throw new Error("No active mutation lease is available for reconciliation.");
      return reconcileMutationLease(lease.ownerRunId, {
        packetId: packetId.trim(),
        operationId: operatorOperation,
        requiredDependencies: [
          { class: "workflow_run", key: `run:${lease.ownerRunId}` },
          { class: "repository_branch_mutation_lease", key: `lease:${lease.leaseId}` },
        ],
      });
    },
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: packageKeys.lease(runId) });
      if (lease && lease.ownerRunId !== runId) void queryClient.invalidateQueries({ queryKey: packageKeys.lease(lease.ownerRunId) });
    },
    onError: (value) => setError(value instanceof RelayApiError ? value.message : value instanceof Error ? value.message : "Mutation lease reconciliation failed."),
  });
  if (query.isLoading || query.error || !lease) return null;
  const heldByAnotherRun = lease.ownerRunId !== runId;
  return <div className="space-y-3 rounded border border-amber-500/30 bg-amber-500/10 p-3 text-xs"><div><p className="font-medium">Repository mutation lease blocks this Run</p><p className="mt-1 text-muted-foreground">{lease.repoTarget} / {lease.branch} / {lease.certainty} / {lease.reconciliationState}</p>{heldByAnotherRun ? <p className="mt-1 text-muted-foreground">Held by Run <Link className="underline" to="/runs/$runId/execute" params={{ runId: lease.ownerRunId }}>{lease.ownerRunId}</Link>.</p> : null}</div>{error ? <p role="alert" className="text-destructive">{error}</p> : null}<div><Label htmlFor="mutation-lease-packet">Local-operator packet ID</Label><Input id="mutation-lease-packet" className="mt-1 h-8 text-xs" value={packetId} onChange={(event) => setPacketId(event.target.value)} placeholder="Packet admission evidence" /></div><Button type="button" size="sm" variant="outline" disabled={mutation.isPending || !packetId.trim()} onClick={() => mutation.mutate()}>{mutation.isPending ? "Reconciling..." : "Reconcile retained lease"}</Button><p className="text-muted-foreground">Only durable reconciliation can release this lease. Local paths, process identities, and notes are not exposed.</p></div>;
}
