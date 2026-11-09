import type { RouteLocationRaw } from 'vue-router'

import type { BarDatum, SelectOption } from '@/components'
import type { components } from '@/gen/api'
import { drill, rangeLabel, ranges, type RangeId } from './overview'
import { formatCount, formatMs } from './text'

type Schemas = components['schemas']
export type Routine = Schemas['ObservatoryRoutine']
export type RoutineTimingGroup = Schemas['ObservatoryRoutineTimingGroup']
export type RoutineTracking = Schemas['ObservatoryRoutines']['tracking']

export const routineSorts = ['mean', 'total', 'self', 'calls', 'slowest'] as const
export type RoutineSort = (typeof routineSorts)[number]

export const routineOrders = ['desc', 'asc'] as const
export type RoutineOrder = (typeof routineOrders)[number]

export type RoutineState = {
  range: RangeId
  sort: RoutineSort
  order: RoutineOrder
  database: string
  search: string
  /** Routines not called in the window are hidden unless the operator asks for them. */
  hideUncalled: boolean
}

export const rangeOptions: SelectOption[] = ranges.map((id) => ({
  value: id,
  label: `Last ${rangeLabel[id]}`,
}))

/** The column a header sorts by, in the table's order. */
export const sortColumns: readonly { sort: RoutineSort; label: string }[] = [
  { sort: 'calls', label: 'Calls' },
  { sort: 'mean', label: 'Mean per call' },
  { sort: 'total', label: 'Total time' },
  { sort: 'self', label: 'Self time' },
  { sort: 'slowest', label: 'Slowest top-level call' },
]

/**
 * What clicking a header asks for: a new column opens largest first, and the
 * column already sorted by turns round.
 */
export function nextSort(
  state: Pick<RoutineState, 'sort' | 'order'>,
  column: RoutineSort,
): Pick<RoutineState, 'sort' | 'order'> {
  if (state.sort !== column) return { sort: column, order: 'desc' }
  return { sort: column, order: state.order === 'desc' ? 'asc' : 'desc' }
}

/** A header's aria-sort: the sorted column says which way, the others none. */
export function ariaSort(
  state: Pick<RoutineState, 'sort' | 'order'>,
  column: RoutineSort,
): 'ascending' | 'descending' | 'none' {
  if (state.sort !== column) return 'none'
  return state.order === 'asc' ? 'ascending' : 'descending'
}

const pick = <T extends string>(asked: unknown, allowed: readonly T[], fallback: T): T =>
  typeof asked === 'string' && (allowed as readonly string[]).includes(asked)
    ? (asked as T)
    : fallback

const text = (asked: unknown): string => (typeof asked === 'string' ? asked : '')

/** The tab's choices as the address holds them; a value it does not know falls back to the default. */
export function parseRoutineState(query: Record<string, unknown>): RoutineState {
  return {
    range: pick(query['range'], ranges, '7d'),
    sort: pick(query['sort'], routineSorts, 'mean'),
    order: pick(query['order'], routineOrders, 'desc'),
    database: text(query['database']),
    search: text(query['search']).trim(),
    hideUncalled: query['uncalled'] !== 'shown',
  }
}

/** The address for a state: the window and the sort always, a filter only when one is set. */
export function routineStateQuery(state: RoutineState): Record<string, string> {
  return {
    range: state.range,
    sort: state.sort,
    ...(state.order === 'asc' ? { order: 'asc' } : {}),
    ...(state.database === '' ? {} : { database: state.database }),
    ...(state.search === '' ? {} : { search: state.search }),
    ...(state.hideUncalled ? {} : { uncalled: 'shown' }),
  }
}

export type RoutineQuery = {
  range: RangeId
  sort: RoutineSort
  order?: RoutineOrder
  database?: string
  search?: string
  limit?: number
}

export function routineQuery(state: RoutineState, limit?: number): RoutineQuery {
  return {
    range: state.range,
    sort: state.sort,
    ...(state.order === 'asc' ? { order: 'asc' as const } : {}),
    ...(state.database === '' ? {} : { database: state.database }),
    ...(state.search === '' ? {} : { search: state.search }),
    ...(limit === undefined ? {} : { limit }),
  }
}

export function routineName(routine: Pick<Routine, 'schema' | 'name' | 'signature'>): string {
  return `${routine.schema}.${routine.name}(${routine.signature})`
}

/** What a row shows: the name alone, its date version included. */
export function routineLabel(routine: Pick<Routine, 'schema' | 'name'>): string {
  return `${routine.schema}.${routine.name}`
}

/**
 * The routine in the Database Objects viewer (FEAT-023). The viewer is
 * addressed by name and answers every overload of it in argument order, so an
 * overload opens beside its siblings; its own signature is the link's title.
 */
export function definitionLink(
  routine: Pick<Routine, 'database' | 'schema' | 'name' | 'kind'>,
): RouteLocationRaw {
  return {
    name: routine.kind === 'procedure' ? 'objects-stored-procedure' : 'objects-function',
    query: { database: routine.database, schema: routine.schema, name: routine.name },
  }
}

/** Whether any routine on the page has a slowest top-level call to show. */
export function anySlowest(routines: readonly Pick<Routine, 'maxCallMs'>[]): boolean {
  return routines.some((routine) => routine.maxCallMs !== null)
}

export type RoutineRow =
  | { row: 'routine'; key: string; routine: Routine }
  | { row: 'group'; key: string; group: RoutineTimingGroup; count: number }
  | { row: 'version'; key: string; routine: Routine; latest: boolean }

const routineKey = (database: string, schema: string, name: string, signature: string) =>
  [database, schema, name, signature].join('\u0000')

const keyOf = (routine: Routine) =>
  routineKey(routine.database, routine.schema, routine.name, routine.signature)

function groupRows(group: RoutineTimingGroup, answered: Map<string, Routine>): RoutineRow[] {
  const groupKey = `group:${[group.database, group.schema, group.base].join('\u0000')}`
  const versions = [...group.versions]
    .sort((a, b) => b.date.localeCompare(a.date) || a.name.localeCompare(b.name))
    .flatMap((version): RoutineRow[] => {
      const routine = answered.get(
        routineKey(group.database, group.schema, version.name, version.signature),
      )
      return routine === undefined
        ? []
        : [
            {
              row: 'version',
              key: `${groupKey}:${keyOf(routine)}`,
              routine,
              latest: version.name === group.latest,
            },
          ]
    })
  if (versions.length === 0) return []
  const [only] = versions
  if (versions.length === 1 && only !== undefined && only.row === 'version') {
    return [{ row: 'routine', key: `routine:${keyOf(only.routine)}`, routine: only.routine }]
  }
  return [{ row: 'group', key: groupKey, group, count: versions.length }, ...versions]
}

/**
 * The timing table's rows, in the order the answer sorted them. A
 * date-versioned routine is drawn inside its group, placed where the group's
 * first member stood, newest version first, so the versions read together. A
 * group left with one version on the page is that version's plain row.
 */
export function routineRows(
  routines: readonly Routine[],
  groups: readonly RoutineTimingGroup[],
): RoutineRow[] {
  const answered = new Map(routines.map((routine) => [keyOf(routine), routine]))
  const memberOf = new Map<string, RoutineTimingGroup>()
  for (const group of groups) {
    for (const version of group.versions) {
      memberOf.set(routineKey(group.database, group.schema, version.name, version.signature), group)
    }
  }
  const placed = new Set<RoutineTimingGroup>()
  const rows: RoutineRow[] = []
  for (const routine of routines) {
    const group = memberOf.get(keyOf(routine))
    if (group === undefined) {
      rows.push({ row: 'routine', key: `routine:${keyOf(routine)}`, routine })
    } else if (!placed.has(group)) {
      placed.add(group)
      rows.push(...groupRows(group, answered))
    }
  }
  return rows
}

/** The routines the table shows, and how many were left out for not being called. */
export function visibleRoutines(
  routines: readonly Routine[],
  hideUncalled: boolean,
): { shown: Routine[]; hidden: number } {
  if (!hideUncalled) return { shown: [...routines], hidden: 0 }
  const shown = routines.filter((routine) => routine.calls > 0)
  return { shown, hidden: routines.length - shown.length }
}

export function hiddenText(hidden: number): string {
  return `${formatCount(hidden)} ${hidden === 1 ? 'routine' : 'routines'} with no calls hidden`
}

export function versionCountText(count: number): string {
  return `${count} ${count === 1 ? 'version' : 'versions'}`
}

/** Why a routine carries no timing, as the operator reads it. */
export function notTimedText(routine: Routine): string {
  const reason = routine.notTimedReason ?? 'no reason given'
  return reason.startsWith('not timed') ? reason : `not timed (${reason})`
}

/** Mean per call: the reason when the routine is not timed, a dash when it was not called. */
export function meanText(routine: Routine): string {
  if (!routine.timed) return notTimedText(routine)
  return formatMs(routine.meanMs)
}

export function timeText(routine: Routine, ms: number): string {
  return routine.timed ? formatMs(ms) : '—'
}

/**
 * When the tracking the figures rest on began: the chosen database's, or the
 * earliest of those answered when every database is shown.
 */
export function trackedSince(tracking: RoutineTracking, database: string): string | null {
  const dates = tracking
    .filter((entry) => database === '' || entry.database === database)
    .map((entry) => entry.since)
    .filter((since): since is string => since !== null)
    .sort()
  return dates[0] ?? null
}

/** The overview's top routines, one bar each, by total time as the answer sorted them. */
export function routineBars(routines: readonly Routine[], shown = 10): BarDatum[] {
  return routines.slice(0, shown).map((routine) => ({
    id: keyOf(routine),
    label: `${routine.schema}.${routine.name}`,
    value: routine.totalMs,
    detail: `${formatCount(routine.calls)} calls · ${formatMs(routine.meanMs)} mean`,
  }))
}

/** The timing tab sorted by total time over the overview's range, narrowed to a bar's routine. */
export function routineDrill(range: RangeId, database: string, bar?: string): RouteLocationRaw {
  if (bar === undefined) return drill('routines', { sort: 'total', range, database })
  const [barDatabase = database, schema = '', name = ''] = bar.split('\u0000')
  return drill('routines', {
    sort: 'total',
    range,
    database: barDatabase,
    search: `${schema}.${name}`,
  })
}
