import type { RouteLocationRaw } from 'vue-router'

import type { BarDatum, BarSegment, ChartTone, Delta, Series } from '@/components'
import type { components } from '@/gen/api'
import type { UsageRow } from './usage'
import type { OverviewPanels } from './access'
import { formatCount } from './text'

type Schemas = components['schemas']
export type UsageBucket = Schemas['ObservatoryUsageBucket']
export type UsageTotals = Schemas['ObservatoryUsageTotals']
export type SizedDatabase = Schemas['ObservatoryDatabase']
export type Summary = Schemas['ObservatorySummary']
export type Statement = Schemas['ObservatoryStatement']
export type ObjectKind = Summary['idle'][number]['kind']

export const ranges = ['24h', '7d', '30d'] as const
export type RangeId = (typeof ranges)[number]
export const refreshes = ['off', '30', '60'] as const
export type RefreshId = (typeof refreshes)[number]

export type OverviewState = { range: RangeId; db: string; refresh: RefreshId }

const hourMs = 3_600_000
const dayMs = 86_400_000

const rangeHours: Record<RangeId, number> = { '24h': 24, '7d': 168, '30d': 720 }

const pick = <T extends string>(asked: unknown, allowed: readonly T[], fallback: T): T =>
  typeof asked === 'string' && (allowed as readonly string[]).includes(asked)
    ? (asked as T)
    : fallback

export function parseState(query: Record<string, unknown>): OverviewState {
  const db = query['db']
  return {
    range: pick(query['range'], ranges, '7d'),
    db: typeof db === 'string' ? db : '',
    refresh: pick(query['refresh'], refreshes, 'off'),
  }
}

export function stateQuery(state: OverviewState): Record<string, string> {
  return { range: state.range, db: state.db, refresh: state.refresh }
}

export type Window = { from: string; to: string; bucketSeconds: number }

/** The window a range covers, aligned to UTC as the series endpoint aligns its buckets. */
export function rangeWindow(range: RangeId, now: Date): Window {
  const hours = rangeHours[range]
  const bucketMs = hours > 336 ? dayMs : hours > 48 ? 6 * hourMs : hourMs
  const to = Math.ceil((now.getTime() + 1) / bucketMs) * bucketMs
  const from = to - hours * hourMs
  return {
    from: new Date(from).toISOString(),
    to: new Date(to).toISOString(),
    bucketSeconds: bucketMs / 1000,
  }
}

export function volumeSeries(buckets: readonly UsageBucket[]): Series[] {
  const at = (bucket: UsageBucket) => Date.parse(bucket.start)
  return [
    {
      id: 'allowed',
      label: 'Allowed',
      tone: 'ok',
      points: buckets.map((b) => ({ x: at(b), y: b.allowed })),
    },
    {
      id: 'refused',
      label: 'Refused',
      tone: 'danger',
      points: buckets.map((b) => ({ x: at(b), y: b.refused })),
    },
  ]
}

export function latencySeries(buckets: readonly UsageBucket[]): Series[] {
  const at = (bucket: UsageBucket) => Date.parse(bucket.start)
  return [
    {
      id: 'p50',
      label: 'p50',
      tone: 'primary',
      points: buckets.map((b) => ({ x: at(b), y: b.p50Ms })),
    },
    {
      id: 'p95',
      label: 'p95',
      tone: 'warn',
      points: buckets.map((b) => ({ x: at(b), y: b.p95Ms })),
    },
  ]
}

/** Which way is better for a figure; size growth is neither. */
export type Goodness = 'higher-is-better' | 'lower-is-better' | 'neutral'

export function delta(
  current: number | null,
  previous: number | null,
  goodness: Goodness,
  format: (magnitude: number) => string,
): Delta | null {
  if (current === null || previous === null) return null
  const change = current - previous
  if (change === 0) return { text: 'no change', direction: 'flat', good: null }
  const direction = change > 0 ? 'up' : 'down'
  const good = goodness === 'neutral' ? null : change > 0 === (goodness === 'higher-is-better')
  return {
    text: `${change > 0 ? '+' : '−'}${format(Math.abs(change))} vs previous`,
    direction,
    good,
  }
}

export function refusalRate(totals: UsageTotals): number | null {
  return totals.statements === 0 ? null : totals.refused / totals.statements
}

export function operatorBars(rows: readonly UsageRow[], shown = 10): BarDatum[] {
  const byOperator = new Map<string, { label: string; allowed: number; refused: number }>()
  for (const row of rows) {
    const seen = byOperator.get(row.operatorId) ?? { label: row.operator, allowed: 0, refused: 0 }
    byOperator.set(row.operatorId, {
      label: seen.label,
      allowed: seen.allowed + row.allowed,
      refused: seen.refused + row.refused,
    })
  }
  return [...byOperator.entries()]
    .map(([id, sum]) => ({
      id,
      label: sum.label,
      value: sum.allowed + sum.refused,
      segments: [
        { id: 'allowed', label: 'Allowed', tone: 'ok' as const, value: sum.allowed },
        { id: 'refused', label: 'Refused', tone: 'danger' as const, value: sum.refused },
      ],
    }))
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
    .slice(0, shown)
}

export function reasonBars(rows: readonly UsageRow[]): BarDatum[] {
  const counts = new Map<string, number>()
  for (const row of rows) {
    for (const refusal of row.refusals) {
      counts.set(refusal.reason, (counts.get(refusal.reason) ?? 0) + refusal.count)
    }
  }
  return [...counts.entries()]
    .map(([reason, count]) => ({ id: reason, label: reason, value: count }))
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
}

export const kindTones: Record<ObjectKind, ChartTone> = {
  function: 'primary',
  procedure: 'series',
  table: 'warn',
  index: 'muted',
}
const kindOrder: readonly ObjectKind[] = ['function', 'procedure', 'table', 'index']
const kindLabel: Record<ObjectKind, string> = {
  function: 'Functions',
  procedure: 'Procedures',
  table: 'Tables',
  index: 'Indexes',
}

export const kindLegend = kindOrder.map((kind) => ({
  id: kind,
  label: kindLabel[kind],
  tone: kindTones[kind],
}))

export function idleBars(idle: Summary['idle']): BarDatum[] {
  const byDatabase = new Map<string, Map<ObjectKind, number>>()
  for (const entry of idle) {
    const kinds = byDatabase.get(entry.database) ?? new Map<ObjectKind, number>()
    kinds.set(entry.kind, (kinds.get(entry.kind) ?? 0) + entry.count)
    byDatabase.set(entry.database, kinds)
  }
  return [...byDatabase.entries()]
    .map(([database, kinds]) => {
      const segments: BarSegment[] = kindOrder
        .filter((kind) => (kinds.get(kind) ?? 0) > 0)
        .map((kind) => ({
          id: kind,
          label: kindLabel[kind],
          tone: kindTones[kind],
          value: kinds.get(kind) ?? 0,
        }))
      return {
        id: database,
        label: database,
        value: segments.reduce((sum, segment) => sum + segment.value, 0),
        segments,
      }
    })
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
}

export function idleTotal(idle: Summary['idle']): number {
  return idle.reduce((sum, entry) => sum + entry.count, 0)
}

const tableKey = '\u0000'

export function tableBars(
  tables: Summary['largestTables'],
  formatGrowth: (bytes: number | null) => string,
  shown = 10,
): BarDatum[] {
  return tables.slice(0, shown).map((table) => ({
    id: `${table.database}${tableKey}${table.schema}.${table.name}`,
    label: `${table.database} · ${table.schema}.${table.name}`,
    value: table.sizeBytes,
    detail: `${formatGrowth(table.growth30dBytes)} in 30 days`,
  }))
}

export function databaseOfTable(id: string): string {
  return id.split(tableKey)[0] ?? ''
}

export function truncate(text: string, length: number): string {
  const flat = text.replace(/\s+/gu, ' ').trim()
  return flat.length <= length ? flat : `${flat.slice(0, length - 1)}…`
}

export function statementBars(statements: readonly Statement[], shown = 10): BarDatum[] {
  return [...statements]
    .sort((a, b) => b.meanMs - a.meanMs)
    .slice(0, shown)
    .map((statement) => ({
      id: statement.queryId,
      label: truncate(statement.text === '' ? statement.queryId : statement.text, 60),
      value: statement.meanMs,
      detail: `${formatCount(statement.calls)} calls in ${statement.database}`,
    }))
}

const utcDate = (ms: number) => new Date(ms).toISOString().slice(0, 10)

/** The latest size a database had on or before a day, when it had one. */
function sizeOn(database: SizedDatabase, day: string): number | null {
  let found: number | null = null
  for (const point of database.growth) {
    if (point.day > day) break
    found = point.sizeBytes
  }
  return found
}

export function chosenDatabases(
  databases: readonly SizedDatabase[],
  db: string,
): readonly SizedDatabase[] {
  return db === '' ? databases : databases.filter((database) => database.database === db)
}

export function sizeSeries(databases: readonly SizedDatabase[], window: Window): Series[] {
  const from = utcDate(Date.parse(window.from))
  const to = utcDate(Date.parse(window.to))
  return databases.map((database) => ({
    id: database.database,
    label: database.database,
    tone: 'series' as const,
    points: database.growth
      .filter((point) => point.day >= from && point.day <= to)
      .map((point) => ({ x: Date.parse(`${point.day}T00:00:00Z`), y: point.sizeBytes })),
  }))
}

export type SizeFigures = { now: number | null; growth: number | null; spark: number[] | null }

/** The summed size now, its growth since the window began, and the summed daily points. */
export function sizeFigures(databases: readonly SizedDatabase[], window: Window): SizeFigures {
  const measured = databases.filter((database) => database.sizeBytes !== null)
  if (measured.length === 0) return { now: null, growth: null, spark: null }
  const now = measured.reduce((sum, database) => sum + (database.sizeBytes ?? 0), 0)
  const from = utcDate(Date.parse(window.from))
  const to = utcDate(Date.parse(window.to))
  const days = [
    ...new Set(
      measured.flatMap((database) =>
        database.growth.map((point) => point.day).filter((day) => day >= from && day <= to),
      ),
    ),
  ].sort()
  const summed = (day: string) =>
    measured.reduce((sum, database) => sum + (sizeOn(database, day) ?? 0), 0)
  const start = measured.some((database) => sizeOn(database, from) !== null)
    ? summed(from)
    : days.length > 0
      ? summed(days[0] ?? from)
      : null
  const spark = days.map(summed)
  return {
    now,
    growth: start === null ? null : now - start,
    spark: spark.length > 1 ? spark : null,
  }
}

export type OverviewTab = 'usage' | 'capacity' | 'last-used' | 'performance' | 'routines'

/** A drill-down into one of the FEAT-071 tabs, its filters in the address. */
export function drill(
  tab: OverviewTab,
  filters: Record<string, string | undefined>,
): RouteLocationRaw {
  const query: Record<string, string> = { tab }
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== '') query[key] = value
  }
  return { name: 'observatory', query }
}

export function bucketWindow(x: number, bucketSeconds: number): { from: string; to: string } {
  return {
    from: new Date(x).toISOString(),
    to: new Date(x + bucketSeconds * 1000).toISOString(),
  }
}

/** An axis or tooltip time: hour and minute for hourly buckets, the day for daily ones. */
export function bucketFormatter(bucketSeconds: number): (epochMs: number) => string {
  return (epochMs) => {
    const iso = new Date(epochMs).toISOString()
    return bucketSeconds >= 86_400 ? iso.slice(5, 10) : `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
  }
}

export const rangeLabel: Record<RangeId, string> = {
  '24h': '24 hours',
  '7d': '7 days',
  '30d': '30 days',
}

/** Size growth over the range: shown, never judged. */
export function growthDelta(
  growth: number | null,
  range: RangeId,
  format: (bytes: number) => string,
): Delta | null {
  if (growth === null) return null
  if (growth === 0)
    return { text: `no change in ${rangeLabel[range]}`, direction: 'flat', good: null }
  return {
    text: `${growth > 0 ? '+' : '−'}${format(Math.abs(growth))} in ${rangeLabel[range]}`,
    direction: growth > 0 ? 'up' : 'down',
    good: null,
  }
}

/** A sparkline's values, or none when fewer than two would be drawn. */
export function spark(values: readonly (number | null)[]): number[] | null {
  const drawn = values.filter((value): value is number => value !== null)
  return drawn.length > 1 ? drawn : null
}

/** The databases a caller's statements went to, most statements first. */
export function databaseBars(rows: readonly UsageRow[], shown = 10): BarDatum[] {
  const byDatabase = new Map<string, { allowed: number; refused: number }>()
  for (const row of rows) {
    const seen = byDatabase.get(row.database) ?? { allowed: 0, refused: 0 }
    byDatabase.set(row.database, {
      allowed: seen.allowed + row.allowed,
      refused: seen.refused + row.refused,
    })
  }
  return [...byDatabase.entries()]
    .map(([database, sum]) => ({
      id: database,
      label: database,
      value: sum.allowed + sum.refused,
      segments: [
        { id: 'allowed', label: 'Allowed', tone: 'ok' as const, value: sum.allowed },
        { id: 'refused', label: 'Refused', tone: 'danger' as const, value: sum.refused },
      ],
    }))
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
    .slice(0, shown)
}

export function rowsTotal(rows: readonly UsageRow[]): number {
  return rows.reduce((sum, row) => sum + row.rowsTotal, 0)
}

/** How many KPI cards the overview draws for these panels, in template order. */
export function kpiCount(panels: OverviewPanels): number {
  const usage = panels.usage ? 3 + (panels.operators ? 0 : 1) : 0
  return usage + (panels.server ? 3 : 0)
}

/**
 * The wide grid's class for a number of cards: every row full, or the last
 * short by one at most, so no card is left alone on a row.
 */
export function kpiColumns(count: number): string {
  if (count >= 7) return 'lg:grid-cols-4'
  if (count === 4) return 'lg:grid-cols-4'
  if (count >= 5) return 'lg:grid-cols-3'
  if (count === 3) return 'lg:grid-cols-3'
  return 'lg:grid-cols-2'
}

/** A bucket width as the captions name it. */
export function bucketWord(bucketSeconds: number): string {
  if (bucketSeconds >= 86_400) return 'day'
  if (bucketSeconds >= 21_600) return '6 hours'
  return 'hour'
}
