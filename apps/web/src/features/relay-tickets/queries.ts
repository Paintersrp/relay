import { queryOptions } from "@tanstack/react-query";
import { getTicket, getTicketFrontier } from "./api";

export const ticketKeys = { all: ["delivery-tickets"] as const, frontier: (workspaceId: string, packetId: string, operationId: string) => [...ticketKeys.all, "frontier", workspaceId, packetId, operationId] as const, detail: (ticketId: string) => [...ticketKeys.all, "detail", ticketId] as const };
export function ticketFrontierQueryOptions(workspaceId: string, packetId: string, operationId: string) { return queryOptions({ queryKey: ticketKeys.frontier(workspaceId, packetId, operationId), queryFn: () => getTicketFrontier(workspaceId, packetId, operationId), enabled: packetId.trim().length > 0 && operationId.trim().length > 0, staleTime: 5_000 }); }
export function ticketDetailQueryOptions(ticketId: string) { return queryOptions({ queryKey: ticketKeys.detail(ticketId), queryFn: () => getTicket(ticketId), enabled: ticketId.trim().length > 0, staleTime: 5_000 }); }
