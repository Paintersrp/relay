import { queryOptions } from "@tanstack/react-query";
import { getTicket, getTicketFrontier } from "./api";

export const ticketKeys = { all: ["delivery-tickets"] as const, frontier: (workspaceId: string) => [...ticketKeys.all, "frontier", workspaceId] as const, detail: (ticketId: string) => [...ticketKeys.all, "detail", ticketId] as const };
export function ticketFrontierQueryOptions(workspaceId: string) { return queryOptions({ queryKey: ticketKeys.frontier(workspaceId), queryFn: () => getTicketFrontier(workspaceId), enabled: workspaceId.trim().length > 0, staleTime: 5_000 }); }
export function ticketDetailQueryOptions(ticketId: string) { return queryOptions({ queryKey: ticketKeys.detail(ticketId), queryFn: () => getTicket(ticketId), enabled: ticketId.trim().length > 0, staleTime: 5_000 }); }
