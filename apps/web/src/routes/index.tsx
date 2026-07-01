import { createFileRoute } from "@tanstack/react-router";
import { HomeOverview } from "@/components/relay/shell/HomeOverview";

// Render the Home_Overview landing surface at the application root (Req 3.1, 10.2).
export const Route = createFileRoute("/")({
  component: HomeOverviewPage,
});

function HomeOverviewPage() {
  return <HomeOverview />;
}
