// ============================================================
// Relay React Query helpers — Pass 1 mock-only.
// All queries return mock data. No real API calls are made.
// Pass 3 will replace these with real backend JSON endpoints.
// ============================================================

import { queryOptions } from '@tanstack/react-query'
import { MOCK_RUNS, getMockRun } from './mock-data'
import type { RelayRun } from './types'

// Simulate async data fetch from mock data with a slight delay
async function fetchMockRuns(): Promise<RelayRun[]> {
  await new Promise((resolve) => setTimeout(resolve, 80))
  return MOCK_RUNS
}

async function fetchMockRun(id: string): Promise<RelayRun | null> {
  await new Promise((resolve) => setTimeout(resolve, 60))
  return getMockRun(id) ?? null
}

// Query key factory
export const relayRunKeys = {
  all: ['relay-runs'] as const,
  list: () => [...relayRunKeys.all, 'list'] as const,
  detail: (id: string) => [...relayRunKeys.all, 'detail', id] as const,
}

// Query options for all runs (list page)
export const runsListQueryOptions = queryOptions({
  queryKey: relayRunKeys.list(),
  queryFn: fetchMockRuns,
  staleTime: 5 * 60 * 1000,
})

// Query options for a single run (workbench step pages)
export function runDetailQueryOptions(id: string) {
  return queryOptions({
    queryKey: relayRunKeys.detail(id),
    queryFn: () => fetchMockRun(id),
    staleTime: 2 * 60 * 1000,
  })
}

// Native Intl formatters — do not add date-fns (CR9)
const dateFormatter = new Intl.DateTimeFormat('en-US', {
  dateStyle: 'medium',
  timeStyle: 'short',
})

const relativeDateFormatter = new Intl.RelativeTimeFormat('en-US', {
  numeric: 'auto',
})

export function formatRunDate(iso: string): string {
  return dateFormatter.format(new Date(iso))
}

export function formatRunDateRelative(iso: string): string {
  const now = Date.now()
  const then = new Date(iso).getTime()
  const diffSeconds = Math.round((then - now) / 1000)
  const diffMinutes = Math.round(diffSeconds / 60)
  const diffHours = Math.round(diffMinutes / 60)
  const diffDays = Math.round(diffHours / 24)

  if (Math.abs(diffSeconds) < 60) return relativeDateFormatter.format(diffSeconds, 'second')
  if (Math.abs(diffMinutes) < 60) return relativeDateFormatter.format(diffMinutes, 'minute')
  if (Math.abs(diffHours) < 24) return relativeDateFormatter.format(diffHours, 'hour')
  return relativeDateFormatter.format(diffDays, 'day')
}
