import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { RelayFeatureWorkspaceDetail } from "@/components/relay/RelayFeatureWorkspaceDetail";
import { Button } from "@/components/ui/button";
import { featureWorkspaceDetailQueryOptions } from "@/features/relay-feature-workspaces";

export const Route = createFileRoute("/feature-workspaces/$workspaceId")({ component: FeatureWorkspacePage });
function FeatureWorkspacePage() { const { workspaceId } = Route.useParams(); const query = useQuery(featureWorkspaceDetailQueryOptions(workspaceId)); if (query.isLoading) return <main className="p-6">Loading workspace…</main>; if (query.error || !query.data) return <main className="space-y-3 p-6"><p role="alert">Workspace could not be loaded.</p><Button asChild variant="outline"><Link to="/feature-workspaces/new">Create a workspace</Link></Button></main>; return <main className="mx-auto w-full max-w-5xl p-6"><RelayFeatureWorkspaceDetail detail={query.data} /></main>; }
