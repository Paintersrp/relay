import { createFileRoute } from "@tanstack/react-router";
import { RelayExecutionPackageCreate } from "@/components/relay/RelayExecutionPackages";

export const Route = createFileRoute("/execution-packages/new")({ component: RelayExecutionPackageCreate });
