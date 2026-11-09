import type { RouteLocationRaw } from 'vue-router'

import type { components } from '@/gen/api'
import type { UsageScope } from './access'

export type UsageRow = components['schemas']['ObservatoryUsageRow']

export const windows = [7, 30, 90] as const
export type WindowDays = (typeof windows)[number]

export function windowRange(days: number, now: Date): { from: string; to: string } {
  const from = new Date(now.getTime() - days * 86_400_000)
  return { from: from.toISOString(), to: now.toISOString() }
}

/** The most frequent refusal reasons, "code ×count", at most three. */
export function topRefusals(row: UsageRow, shown = 3): string {
  if (row.refusals.length === 0) return '—'
  return row.refusals
    .slice(0, shown)
    .map((refusal) => `${refusal.reason} ×${refusal.count}`)
    .join(', ')
}

/**
 * The FEAT-003 log for one row, carrying the operator, the database and the
 * window as query parameters (FEAT-071 AC-5). Every operator reads everybody's
 * usage, but the logs keep their own permissions: the administrator's log needs
 * audit.read.all, and the personal log shows only the caller's own rows. A row
 * the caller has no log for links nowhere.
 */
export function auditLink(
  row: UsageRow,
  auditLog: UsageScope,
  range: { from: string; to: string },
  self: string | undefined,
): RouteLocationRaw | null {
  const query: Record<string, string> = { database: row.database, ...range }
  if (auditLog === 'all') return { name: 'audit-all', query: { ...query, operator: row.operator } }
  if (auditLog === 'own' && row.operatorId === self) return { name: 'audit-mine', query }
  return null
}
