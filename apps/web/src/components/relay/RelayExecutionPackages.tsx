import * as React from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RelayApiError } from "@/features/workflow-api";
import { approveExecutionPackage, executionPackageQueryOptions, packageKeys, prepareExecutionPackage, type PackageArtifactInput } from "@/features/relay-packages";

const operatorOperation = "local_operator.ticket_workflow";
function report(error: unknown): string { return error instanceof RelayApiError ? error.message : error instanceof Error ? error.message : "Execution package operation failed."; }
async function sha256(bytes: ArrayBuffer): Promise<string> { const digest = await crypto.subtle.digest("SHA-256", bytes); return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join(""); }
function base64(bytes: Uint8Array): string { let result = ""; for (let offset = 0; offset < bytes.length; offset += 0x8000) result += String.fromCharCode(...bytes.subarray(offset, Math.min(offset + 0x8000, bytes.length))); return btoa(result); }
async function artifact(file: File): Promise<PackageArtifactInput> { const bytes = await file.arrayBuffer(); return { displayName: file.name, expectedSha256: await sha256(bytes), bytesBase64: base64(new Uint8Array(bytes)) }; }

export function RelayExecutionPackageCreate() {
  const [packetId, setPacketId] = React.useState("");
  const [selectionId, setSelectionId] = React.useState("");
  const [brief, setBrief] = React.useState<File | null>(null);
  const [operations, setOperations] = React.useState<File | null>(null);
  const [createdPackageId, setCreatedPackageId] = React.useState<string | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const mutation = useMutation({ mutationFn: async () => {
    if (!brief) throw new Error("Select the Ticket Design Brief.");
    const ticketDesignBrief = await artifact(brief);
    const deterministicOperations = operations ? await artifact(operations) : undefined;
    const requiredDependencies = [
      { class: "execution_package_selection", key: `selection:${selectionId.trim()}` },
      { class: "execution_package_ticket_design_brief", key: `${ticketDesignBrief.displayName}:${ticketDesignBrief.expectedSha256}` },
      ...(deterministicOperations ? [{ class: "execution_package_deterministic_operations", key: `${deterministicOperations.displayName}:${deterministicOperations.expectedSha256}` }] : []),
    ];
    return prepareExecutionPackage({ packetId: packetId.trim(), operationId: operatorOperation, selectionId: selectionId.trim(), ticketDesignBrief, deterministicOperations, requiredDependencies });
  }, onSuccess: (value) => { setCreatedPackageId(value.packageId); setError(null); }, onError: (value) => setError(report(value)) });
  return <main className="mx-auto w-full max-w-3xl space-y-6 p-6"><section className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6"><h1 className="text-xl font-semibold">Prepare execution package</h1><p className="mt-1 text-sm text-muted-foreground">One approved Ticket Design Brief may include optional exact Deterministic Operations. Approval creates the linked setup-ready Run.</p>{error ? <p role="alert" className="mt-3 rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{error}</p> : null}{createdPackageId ? <p className="mt-3 rounded border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm">Package prepared. <Link className="underline" to="/execution-packages/$packageId" params={{ packageId: createdPackageId }}>Review package basis</Link></p> : null}<form className="mt-5 grid gap-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}><div><Label htmlFor="package-packet">Local-operator packet ID</Label><Input id="package-packet" value={packetId} onChange={(event) => setPacketId(event.target.value)} required /></div><div><Label htmlFor="package-selection">Selection ID</Label><Input id="package-selection" value={selectionId} onChange={(event) => setSelectionId(event.target.value)} required /></div><div><Label htmlFor="package-brief">Ticket Design Brief</Label><Input id="package-brief" type="file" accept="text/markdown,.md" onChange={(event) => setBrief(event.target.files?.[0] ?? null)} required /></div><div><Label htmlFor="package-operations">Deterministic Operations (optional)</Label><Input id="package-operations" type="file" accept="application/json,.json" onChange={(event) => setOperations(event.target.files?.[0] ?? null)} /></div><Button type="submit" disabled={mutation.isPending || !packetId.trim() || !selectionId.trim() || !brief}>{mutation.isPending ? "Preparing…" : "Prepare immutable package"}</Button></form></section></main>;
}

export function RelayExecutionPackageDetail({ packageId }: { packageId: string }) {
  const queryClient = useQueryClient();
  const query = useQuery(executionPackageQueryOptions(packageId));
  const [packetId, setPacketId] = React.useState("");
  const [confirmationEvidence, setConfirmationEvidence] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const approval = useMutation({ mutationFn: () => { const value = query.data; if (!value) throw new Error("Package basis is unavailable."); return approveExecutionPackage(value.packageId, { packetId: packetId.trim(), operationId: operatorOperation, expectedPackageSha256: value.packageSha256, operatorConfirmationEvidence: confirmationEvidence, requiredDependencies: [{ class: "execution_package_basis", key: `package:${value.packageId}:${value.packageSha256}` }] }); }, onSuccess: () => { setError(null); void queryClient.invalidateQueries({ queryKey: packageKeys.detail(packageId) }); }, onError: (value) => setError(report(value)) });
  if (query.isLoading) return <main className="mx-auto w-full max-w-3xl p-6 text-sm text-muted-foreground">Loading immutable package basis…</main>;
  if (query.error || !query.data) return <main className="mx-auto w-full max-w-3xl p-6" role="alert">Unable to load execution package basis.</main>;
  const value = query.data;
  return <main className="mx-auto w-full max-w-3xl space-y-6 p-6"><section className="rounded border border-[var(--relay-row-border)] bg-[var(--panel)] p-6"><h1 className="text-xl font-semibold">Execution package basis</h1><p className="mt-1 break-all text-sm text-muted-foreground">{value.packageId}</p><dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">Immutable package SHA</dt><dd className="break-all">{value.packageSha256}</dd></div><div><dt className="text-muted-foreground">Repository / branch</dt><dd>{value.repoTarget} / {value.branch}</dd></div><div><dt className="text-muted-foreground">Base commit</dt><dd className="break-all">{value.baseCommit}</dd></div><div><dt className="text-muted-foreground">Authority basis</dt><dd className="break-all">{value.authoritySha256}</dd></div><div><dt className="text-muted-foreground">Source basis</dt><dd className="break-all">{value.sourceSha256}</dd></div></dl><h2 className="mt-6 font-semibold">Selected Ticket Design Brief</h2><p className="mt-2 break-all text-sm">{value.ticketDesignBrief.displayName} · {value.ticketDesignBrief.sha256}</p>{value.deterministicOperations ? <><h2 className="mt-4 font-semibold">Deterministic Operations</h2><p className="mt-2 break-all text-sm">{value.deterministicOperations.displayName} · {value.deterministicOperations.sha256} · {value.deterministicOperationsCoverage ?? "unknown coverage"}</p></> : null}<h2 className="mt-6 font-semibold">Selected members</h2><ul className="mt-2 space-y-2 text-sm">{value.members.map((member) => <li key={member.selectionMemberRowId} className="rounded border p-3">Ticket revision row {member.revisionRowId} · sequence {member.sequence} · <span className="break-all">{member.memberSha256}</span></li>)}</ul></section><section className="rounded border border-[var(--relay-row-border)] bg-[var(--panel)] p-6"><h2 className="font-semibold">Approve exact package and create linked Run</h2>{error ? <p role="alert" className="mt-3 rounded border border-destructive/30 bg-destructive/10 p-3 text-destructive">{error}</p> : null}{value.run ? <p className="mt-3 rounded border p-3 text-sm">One linked setup-ready Run: <Link className="underline" to="/runs/$runId" params={{ runId: value.run.runId }}>{value.run.runId}</Link></p> : <form className="mt-5 grid gap-4" onSubmit={(event) => { event.preventDefault(); approval.mutate(); }}><div><Label htmlFor="approval-packet">Local-operator packet ID</Label><Input id="approval-packet" value={packetId} onChange={(event) => setPacketId(event.target.value)} required /></div><div><Label htmlFor="approval-evidence">Operator confirmation evidence</Label><textarea id="approval-evidence" className="flex min-h-20 w-full rounded border border-input bg-transparent px-3 py-2 text-sm shadow-sm" value={confirmationEvidence} onChange={(event) => setConfirmationEvidence(event.target.value)} required rows={3} /></div><Button type="submit" disabled={approval.isPending}>Approve exact package and create linked Run</Button></form>}</section></main>;
}
