import type { Page } from '@playwright/test'

import type { components } from '@/gen/api'
import { administrator } from './api'

type Schemas = components['schemas']

/*
 * FEAT-071's answers, fixed. The e2e stack is seeded without the observatory's
 * snapshot history, so the page is driven over these: every kind of last-use
 * sentence the Last used tab can write appears at least once, which is what
 * lets AC-22 read the whole vocabulary off one rendered page.
 */

const dbaPermissions = [
  'query.inspect',
  'query.execute',
  'audit.read.own',
  'instance.read',
  'instance.maintain',
  'permission.manage',
  'write.approve',
  'provision.manage',
]

export const observatoryCallers: Record<string, Schemas['CurrentUser']> = {
  administrator: {
    ...administrator,
    roles: ['administrator'],
    permissions: [
      'query.inspect',
      'audit.read.all',
      'permission.manage',
      'threshold.manage',
      'account.manage',
    ],
  },
  dba: { ...administrator, username: 'dba', roles: ['dba'], permissions: dbaPermissions },
  developer: {
    ...administrator,
    username: 'dev',
    roles: ['developer'],
    permissions: ['query.inspect', 'query.execute', 'audit.read.own'],
  },
}

const usage: Schemas['ObservatoryUsage'] = {
  from: '2026-09-13T08:00:00Z',
  to: '2026-09-20T08:00:00Z',
  rows: [
    {
      operatorId: '00000000-0000-0000-0000-000000000011',
      operator: 'lin.wei',
      database: 'aviorfixture',
      statements: 42,
      allowed: 38,
      refused: 4,
      refusals: [
        { reason: 'wildcard_select', count: 3 },
        { reason: 'column_denied', count: 1 },
      ],
      executed: 38,
      elapsedP50Ms: 14,
      elapsedP95Ms: 220,
      rowsTotal: 12_400,
    },
  ],
}

const growth = (start: number, step: number): Schemas['ObservatorySizePoint'][] =>
  Array.from({ length: 30 }, (_, index) => ({
    day: `2026-08-${String(index + 1).padStart(2, '0')}`,
    sizeBytes: start + index * step,
  }))

const databases: Schemas['ObservatoryDatabases'] = {
  idleDays: 30,
  lastRunAt: '2026-09-20T08:15:00Z',
  flagsWithheld: null,
  databases: [
    {
      database: 'aviorfixture',
      sizeBytes: 3_221_225_472,
      sizeBytes7d: 3_100_000_000,
      sizeBytes30d: 2_900_000_000,
      growth: growth(2_900_000_000, 10_000_000),
      routinesTrackedSince: '2026-09-01T00:00:00Z',
      relationsTrackedSince: '2026-07-01T00:00:00Z',
      flagged: 3,
      lastResetAt: null,
    },
    {
      database: 'ledger',
      sizeBytes: 104_857_600,
      sizeBytes7d: null,
      sizeBytes30d: null,
      growth: [],
      routinesTrackedSince: null,
      relationsTrackedSince: null,
      flagged: 0,
      lastResetAt: '2026-09-03T00:00:00Z',
    },
  ],
}

function object(overrides: Partial<Schemas['ObservatoryObject']>): Schemas['ObservatoryObject'] {
  return {
    schema: 'public',
    name: 'member',
    kind: 'table',
    signature: null,
    table: null,
    tracked: true,
    notTrackedReason: null,
    lastUsedAt: null,
    lastUsedKind: null,
    lastUsedNode: null,
    daysAgo: null,
    flagged: false,
    constraintBacked: false,
    sizeBytes: null,
    growth30dBytes: null,
    calls: null,
    ...overrides,
  }
}

const version = (day: string, overrides: Partial<Schemas['ObservatoryRoutineVersion']> = {}) => ({
  name: `shop_settle_order_26_09_${day}`,
  signature: 'bigint',
  date: `2026-09-${day}`,
  latest: false,
  lastUsedAt: null,
  daysAgo: null,
  flagged: false,
  ...overrides,
})

export const observatoryObjects: Schemas['ObservatoryObjects'] = {
  database: 'aviorfixture',
  idleDays: 30,
  routinesTrackedSince: '2026-09-01T00:00:00Z',
  relationsTrackedSince: '2026-07-01T00:00:00Z',
  flagsWithheld: null,
  objects: [
    object({
      name: 'member',
      lastUsedAt: '2026-09-19T10:00:00Z',
      lastUsedKind: 'read',
      lastUsedNode: 'primary',
      daysAgo: 1,
      sizeBytes: 81_920,
      growth30dBytes: 8_192,
    }),
    object({
      name: 'bet_archive',
      lastUsedAt: '2026-08-02T10:00:00Z',
      lastUsedKind: 'written',
      daysAgo: 49,
      flagged: true,
      sizeBytes: 1_048_576,
      growth30dBytes: 0,
    }),
    object({ name: 'member_audit', flagged: true, sizeBytes: 16_384, growth30dBytes: null }),
    object({
      name: 'member_pkey',
      kind: 'index',
      table: 'public.member',
      constraintBacked: true,
      sizeBytes: 16_384,
    }),
    object({
      name: 'bet_placed_at_idx',
      kind: 'index',
      table: 'public.bet',
      lastUsedAt: '2026-09-18T00:00:00Z',
      lastUsedKind: 'scanned',
      daysAgo: 2,
      sizeBytes: 32_768,
    }),
    object({
      name: 'member_count',
      kind: 'function',
      signature: '',
      tracked: false,
      notTrackedReason: 'LANGUAGE sql needs track_functions = all',
    }),
    object({
      name: 'shop_settle_order_26_09_25',
      kind: 'function',
      signature: 'bigint',
      lastUsedAt: '2026-09-19T00:00:00Z',
      lastUsedKind: 'run',
      daysAgo: 1,
      calls: 12,
    }),
    object({
      name: 'shop_settle_order_26_09_21',
      kind: 'function',
      signature: 'bigint',
    }),
    object({
      name: 'shop_settle_order_26_09_19',
      kind: 'function',
      signature: 'bigint',
      lastUsedAt: '2026-08-10T00:00:00Z',
      lastUsedKind: 'run',
      daysAgo: 41,
      flagged: true,
    }),
    object({ name: 'settle_round', kind: 'procedure', signature: 'bigint, text' }),
  ],
  routineGroups: [
    {
      schema: 'public',
      base: 'shop_settle_order',
      latest: 'shop_settle_order_26_09_25',
      flaggedCount: 1,
      lastUsedAt: '2026-09-19T00:00:00Z',
      versions: [
        version('25', {
          latest: true,
          lastUsedAt: '2026-09-19T00:00:00Z',
          daysAgo: 1,
        }),
        version('21'),
        version('19', { flagged: true, lastUsedAt: '2026-08-10T00:00:00Z', daysAgo: 41 }),
      ],
    },
  ],
}

const health: Schemas['ObservatoryHealth'] = {
  database: 'aviorfixture',
  tables: [
    {
      schema: 'public',
      name: 'bet',
      sizeBytes: 5_242_880,
      liveTuples: 90_000,
      deadTuples: 10_000,
      deadRatio: 0.1,
      seqScans: 14,
      seqRowsRead: 1_260_000,
      idxScans: 4_000,
      lastAutovacuum: '2026-09-19T03:00:00Z',
      lastAutoanalyze: null,
    },
  ],
}

const activity: Schemas['ObservatoryActivity'] = {
  generatedAt: '2026-09-20T08:30:00Z',
  backends: [
    {
      pid: 4242,
      database: 'aviorfixture',
      role: 'avior_governed',
      state: 'active',
      startedAt: '2026-09-20T08:27:00Z',
      runningForMs: 180_000,
      waitEvent: null,
      text: 'SELECT id, placed_at FROM public.bet WHERE placed_at > $1 ORDER BY placed_at',
    },
  ],
}

/*
 * FEAT-072's two answers. The series is 28 six-hour buckets ending on the
 * fixed clock, with one outlier bucket so the latency chart's log axis has
 * something to hold apart from p50.
 */
const seriesStart = Date.parse('2026-09-20T12:00:00Z')
const usageSeries: Schemas['ObservatoryUsageSeries'] = {
  from: new Date(seriesStart).toISOString(),
  to: new Date(seriesStart + 28 * 21_600_000).toISOString(),
  bucketSeconds: 21_600,
  buckets: Array.from({ length: 28 }, (_, index) => ({
    start: new Date(seriesStart + index * 21_600_000).toISOString(),
    allowed: 4 + (index % 5),
    refused: index % 3,
    p50Ms: 30 + (index % 4) * 5,
    p95Ms: index === 13 ? 26_000 : 800 + (index % 6) * 40,
  })),
  totals: { statements: 212, allowed: 180, refused: 32, executed: 180, p50Ms: 38, p95Ms: 940 },
  previous: { statements: 190, allowed: 166, refused: 24, executed: 166, p50Ms: 41, p95Ms: 610 },
}

const summary: Schemas['ObservatorySummary'] = {
  idleDays: 30,
  flagsWithheld: null,
  largestTables: [
    {
      database: 'aviorfixture',
      schema: 'public',
      name: 'bet',
      sizeBytes: 48_234_496,
      growth30dBytes: 6_291_456,
    },
    {
      database: 'aviordeploy',
      schema: 'public',
      name: 'ledger_probe',
      sizeBytes: 9_437_184,
      growth30dBytes: null,
    },
  ],
  idle: [
    { database: 'aviorfixture', kind: 'function', count: 3 },
    { database: 'aviorfixture', kind: 'index', count: 1 },
  ],
}

const statements: Schemas['ObservatoryStatements'] = {
  statsSince: '2026-09-01T00:00:00Z',
  statements: [
    {
      queryId: '-4611686018427387904',
      database: 'aviorfixture',
      role: 'avior_governed',
      calls: 1_200,
      totalMs: 96_000,
      meanMs: 80,
      rows: 120_000,
      text: 'SELECT m.id, m.username, count(b.id) FROM public.member m JOIN public.bet b ON b.member_id = m.id WHERE b.placed_at > $1 GROUP BY m.id, m.username ORDER BY count(b.id) DESC LIMIT $2',
    },
  ],
}

/*
 * FEAT-073's answer: timed routines in three databases, a date-versioned
 * group of three, a procedure, a routine not called in the window, one not
 * timed, and a slowest top-level call on one routine only.
 */
const timedRoutine = (
  overrides: Partial<Schemas['ObservatoryRoutine']>,
): Schemas['ObservatoryRoutine'] => ({
  database: 'aviorfixture',
  schema: 'public',
  name: 'routine',
  signature: 'bigint',
  kind: 'function',
  timed: true,
  notTimedReason: null,
  calls: 0,
  totalMs: 0,
  selfMs: 0,
  meanMs: null,
  maxCallMs: null,
  ...overrides,
})

const routineList: Schemas['ObservatoryRoutine'][] = [
  timedRoutine({
    name: 'settle_round',
    calls: 48_000,
    totalMs: 912_000,
    selfMs: 604_000,
    meanMs: 19,
    maxCallMs: 2_400,
  }),
  timedRoutine({
    database: 'ledger',
    schema: 'ops',
    name: 'close_day',
    signature: 'date',
    kind: 'procedure',
    calls: 7,
    totalMs: 441_000,
    selfMs: 12_000,
    meanMs: 63_000,
  }),
  timedRoutine({
    name: 'shop_settle_order_26_09_25',
    calls: 9_000,
    totalMs: 270_000,
    selfMs: 270_000,
    meanMs: 30,
  }),
  timedRoutine({
    name: 'shop_settle_order_26_09_01',
    calls: 600,
    totalMs: 24_000,
    selfMs: 24_000,
    meanMs: 40,
  }),
  timedRoutine({
    database: 'aviordeploy',
    name: 'probe_member',
    signature: 'text',
    calls: 1_200,
    totalMs: 3_600,
    selfMs: 3_600,
    meanMs: 3,
  }),
  timedRoutine({
    name: 'shop_settle_order_26_08_14',
    calls: 1,
    totalMs: 55,
    selfMs: 55,
    meanMs: 55,
  }),
  timedRoutine({ name: 'lookup_member', signature: 'text' }),
  timedRoutine({
    name: 'member_label',
    signature: 'text',
    timed: false,
    notTimedReason: 'LANGUAGE sql needs track_functions = all',
  }),
]

export const observatoryRoutines: Schemas['ObservatoryRoutines'] = {
  from: '2026-09-13T08:00:00Z',
  to: '2026-09-20T08:00:00Z',
  statsSince: '2026-09-03T00:00:00Z',
  tracking: [
    { database: 'aviorfixture', since: '2026-09-01T00:00:00Z' },
    { database: 'ledger', since: '2026-09-12T00:00:00Z' },
    { database: 'aviordeploy', since: null },
  ],
  routines: routineList,
  groups: [
    {
      database: 'aviorfixture',
      schema: 'public',
      base: 'shop_settle_order',
      latest: 'shop_settle_order_26_09_25',
      versions: [
        { name: 'shop_settle_order_26_09_25', signature: 'bigint', date: '2026-09-25' },
        { name: 'shop_settle_order_26_09_01', signature: 'bigint', date: '2026-09-01' },
        { name: 'shop_settle_order_26_08_14', signature: 'bigint', date: '2026-08-14' },
      ],
    },
  ],
}

const routineOrder: Record<string, (row: Schemas['ObservatoryRoutine']) => number | null> = {
  mean: (row) => row.meanMs,
  total: (row) => row.totalMs,
  self: (row) => row.selfMs,
  calls: (row) => row.calls,
  slowest: (row) => row.maxCallMs,
}

/** The server's order: the figure asked for, in the direction asked for, a missing one last. */
function byFigure(
  figure: (row: Schemas['ObservatoryRoutine']) => number | null,
  descending: boolean,
): (a: Schemas['ObservatoryRoutine'], b: Schemas['ObservatoryRoutine']) => number {
  return (a, b) => {
    const fa = figure(a)
    const fb = figure(b)
    if (fa === null || fb === null) return fa === fb ? 0 : fa === null ? 1 : -1
    return descending ? fb - fa : fa - fb
  }
}

/** The routines answer narrowed and ordered the way the server is asked to. */
function answerRoutines(url: URL): Schemas['ObservatoryRoutines'] {
  const params = url.searchParams
  const database = params.get('database') ?? ''
  const search = (params.get('search') ?? '').toLowerCase()
  const figure = routineOrder[params.get('sort') ?? 'mean'] ?? routineOrder['mean']
  const descending = params.get('order') !== 'asc'
  const limit = Number(params.get('limit') ?? routineList.length)
  const routines = routineList
    .filter((row) => database === '' || row.database === database)
    .filter((row) => `${row.schema}.${row.name}`.toLowerCase().includes(search))
    .sort(figure ? byFigure(figure, descending) : () => 0)
    .slice(0, limit)
  return { ...observatoryRoutines, routines }
}

const statementsUnavailable = {
  code: 'observatory.statements_unavailable',
  message: 'pg_stat_statements is not installed in aviordb, so there are no statements to read.',
}

/**
 * Answers the observatory's endpoints, and /auth/me as the caller named.
 * Registered after the fixed API so these routes win over its fallback.
 */
export async function serveObservatory(
  page: Page,
  caller = 'administrator',
  statementsInstalled = true,
): Promise<void> {
  const me = observatoryCallers[caller] ?? administrator
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({ json: me }))
  await page.route('**/api/v1/observatory/**', (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/api\/v1/u, '')
    if (path === '/observatory/routines') return route.fulfill({ json: answerRoutines(url) })
    const answers: Record<string, unknown> = {
      '/observatory/usage': usage,
      '/observatory/databases': databases,
      '/observatory/objects': observatoryObjects,
      '/observatory/health': health,
      '/observatory/activity': activity,
      '/observatory/usage/series': usageSeries,
      '/observatory/summary': summary,
    }
    if (path === '/observatory/statements' && statementsInstalled) {
      return route.fulfill({ json: statements })
    }
    if (path === '/observatory/statements') {
      return route.fulfill({ status: 409, json: statementsUnavailable })
    }
    const body = answers[path]
    return body === undefined
      ? route.fulfill({ status: 500, json: { code: 'fixture.unanswered', message: path } })
      : route.fulfill({ json: body })
  })
}
