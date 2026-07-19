import { createFileRoute } from "@tanstack/react-router";
import { RelayExecutionPackageDetail } from "@/components/relay/RelayExecutionPackages";

export const Route = createFileRoute("/execution-packages/$packageId")({ component: PackageDetailPage });

function PackageDetailPage() { const { packageId } = Route.useParams(); return <RelayExecutionPackageDetail packageId={packageId} />; }
