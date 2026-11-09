import { describe, expect, it } from 'vitest'

import {
  bucketFormatter,
  bucketWindow,
  databaseBars,
  databaseOfTable,
  delta,
  drill,
  growthDelta,
  idleBars,
  idleTotal,
  latencySeries,
  operatorBars,
  parseState,
  rangeWindow,
  bucketWord,
  reasonBars,
  refusalRate,
  rowsTotal,
  sizeFigures,
  sizeSeries,
  spark,
  statementBars,
  stateQuery,
  tableBars,
  truncate,
  volumeSeries,
  type SizedDatabase,
  type Statement,
  type UsageBucket,
  kpiColumns,
  kpiCount,
} from './overview'
import { formatBytes, formatCount, formatGrowth, formatMs } from './text'
import type { UsageRow } from './usage'

const bucket = (
  start: string,
  allowed: number,
  refused: number,
  p95: number | null,
): UsageBucket => ({ start, allowed, refused, p50Ms: p95 === null ? null : p95 / 2, p95Ms: p95 })

const row = (operator: string, database: string, allowed: number, refused: number) =>
  ({
    operatorId: `id-${operator}`,
    operator,
    database,
    statements: allowed + refused,
    allowed,
    refused,
    refusals: refused > 0 ? [{ reason: 'wildcard_select', count: refused }] : [],
    executed: allowed,
    elapsedP50Ms: 1,
    elapsedP95Ms: 2,
    rowsTotal: 0,
  }) as UsageRow

describe('the address of the overview', () => {
  it('defaults to seven days, every database and no refresh', () => {
    expect(parseState({})).toEqual({ range: '7d', db: '', refresh: 'off' })
  })

  it('keeps what it recognises and drops what it does not', () => {
    expect(parseState({ range: '24h', db: 'aviorfixture', refresh: '30' })).toEqual({
      range: '24h',
      db: 'aviorfixture',
      refresh: '30',
    })
    expect(parseState({ range: '1y', db: ['a'], refresh: '5' })).toEqual({
      range: '7d',
      db: '',
      refresh: 'off',
    })
  })

  it('writes every choice back', () => {
    expect(stateQuery({ range: '30d', db: 'x', refresh: '60' })).toEqual({
      range: '30d',
      db: 'x',
      refresh: '60',
    })
  })
})

describe('a range as a window', () => {
  const now = new Date('2026-09-27T10:20:00Z')

  it('is hourly for a day and ends at the close of the current hour', () => {
    expect(rangeWindow('24h', now)).toEqual({
      from: '2026-09-26T11:00:00.000Z',
      to: '2026-09-27T11:00:00.000Z',
      bucketSeconds: 3600,
    })
  })

  it('is six-hourly for seven days and ends at the close of the current six hours', () => {
    expect(rangeWindow('7d', now)).toEqual({
      from: '2026-09-20T12:00:00.000Z',
      to: '2026-09-27T12:00:00.000Z',
      bucketSeconds: 21600,
    })
  })

  it('names each bucket width in words', () => {
    expect([3600, 21600, 86400].map(bucketWord)).toEqual(['hour', '6 hours', 'day'])
  })

  it('is daily for thirty days and ends at the close of the current UTC day', () => {
    expect(rangeWindow('30d', now)).toEqual({
      from: '2026-08-29T00:00:00.000Z',
      to: '2026-09-28T00:00:00.000Z',
      bucketSeconds: 86400,
    })
  })

  it('gives a bucket its own window for a drill-down', () => {
    expect(bucketWindow(Date.parse('2026-09-27T10:00:00Z'), 3600)).toEqual({
      from: '2026-09-27T10:00:00.000Z',
      to: '2026-09-27T11:00:00.000Z',
    })
  })

  it('labels hourly buckets with the time and daily ones with the day', () => {
    const at = Date.parse('2026-09-27T10:00:00Z')
    expect(bucketFormatter(3600)(at)).toBe('09-27 10:00')
    expect(bucketFormatter(86400)(at)).toBe('09-27')
  })
})

describe('buckets as series', () => {
  const buckets = [
    bucket('2026-09-27T09:00:00Z', 3, 1, 40),
    bucket('2026-09-27T10:00:00Z', 0, 0, null),
  ]

  it('stacks allowed and refused on the bucket start', () => {
    const [allowed, refused] = volumeSeries(buckets)
    expect(allowed?.tone).toBe('ok')
    expect(refused?.tone).toBe('danger')
    expect(allowed?.points).toEqual([
      { x: Date.parse('2026-09-27T09:00:00Z'), y: 3 },
      { x: Date.parse('2026-09-27T10:00:00Z'), y: 0 },
    ])
    expect(refused?.points.map((p) => p.y)).toEqual([1, 0])
  })

  it('leaves a gap where nothing ran rather than a zero latency', () => {
    const [p50, p95] = latencySeries(buckets)
    expect(p50?.points.map((p) => p.y)).toEqual([20, null])
    expect(p95?.points.map((p) => p.y)).toEqual([40, null])
  })

  it('draws a sparkline only from two values or more', () => {
    expect(spark([1, null, 3])).toEqual([1, 3])
    expect(spark([null, 4])).toBeNull()
  })
})

describe('deltas', () => {
  it('judges a rise by which way is better', () => {
    expect(delta(120, 100, 'lower-is-better', formatMs)).toEqual({
      text: '+20 ms vs previous',
      direction: 'up',
      good: false,
    })
    expect(delta(80, 100, 'lower-is-better', formatMs)?.good).toBe(true)
    expect(delta(5, 3, 'higher-is-better', formatCount)?.good).toBe(true)
  })

  it('does not judge a neutral figure or no change', () => {
    expect(delta(5, 3, 'neutral', formatCount)).toEqual({
      text: '+2 vs previous',
      direction: 'up',
      good: null,
    })
    expect(delta(3, 3, 'lower-is-better', formatCount)).toEqual({
      text: 'no change',
      direction: 'flat',
      good: null,
    })
  })

  it('has nothing to say without both figures', () => {
    expect(delta(null, 3, 'neutral', formatCount)).toBeNull()
    expect(delta(3, null, 'neutral', formatCount)).toBeNull()
  })

  it('shows size growth over the range without judging it', () => {
    expect(growthDelta(1024, '7d', formatBytes)).toEqual({
      text: '+1.0 KB in 7 days',
      direction: 'up',
      good: null,
    })
    expect(growthDelta(0, '24h', formatBytes)?.direction).toBe('flat')
    expect(growthDelta(null, '24h', formatBytes)).toBeNull()
  })

  it('has no refusal rate over no statements', () => {
    const totals = { statements: 4, allowed: 3, refused: 1, executed: 3, p50Ms: 1, p95Ms: 2 }
    expect(refusalRate(totals)).toBe(0.25)
    expect(refusalRate({ ...totals, statements: 0, allowed: 0, refused: 0 })).toBeNull()
  })
})

describe('usage rows as bars', () => {
  const rows = [row('lin.wei', 'a', 5, 1), row('lin.wei', 'b', 2, 2), row('amy', 'a', 1, 0)]

  it('sums an operator across databases, most statements first, allowed and refused apart', () => {
    const bars = operatorBars(rows)
    expect(bars.map((bar) => [bar.id, bar.label, bar.value])).toEqual([
      ['id-lin.wei', 'lin.wei', 10],
      ['id-amy', 'amy', 1],
    ])
    expect(bars[0]?.segments?.map((s) => s.value)).toEqual([7, 3])
  })

  it('keeps the top ten operators', () => {
    const many = Array.from({ length: 12 }, (_, i) => row(`op${i}`, 'a', i + 1, 0))
    expect(operatorBars(many)).toHaveLength(10)
    expect(operatorBars(many)[0]?.label).toBe('op11')
  })

  it('sums a caller’s statements and rows by database, most statements first', () => {
    const bars = databaseBars(rows)
    expect(bars.map((bar) => [bar.id, bar.value])).toEqual([
      ['a', 7],
      ['b', 4],
    ])
    expect(bars[1]?.segments?.map((s) => s.value)).toEqual([2, 2])
    expect(rowsTotal([{ ...row('amy', 'a', 1, 0), rowsTotal: 5 }, row('amy', 'b', 1, 0)])).toBe(5)
  })

  it('counts refusals by reason across operators', () => {
    const withCost = { ...row('amy', 'a', 0, 2), refusals: [{ reason: 'cost', count: 2 }] }
    expect(reasonBars([...rows, withCost]).map((bar) => [bar.id, bar.value])).toEqual([
      ['wildcard_select', 3],
      ['cost', 2],
    ])
  })
})

describe('the summary as bars', () => {
  it('splits idle objects of a database by kind in a fixed order', () => {
    const idle = [
      { database: 'a', kind: 'index' as const, count: 2 },
      { database: 'a', kind: 'function' as const, count: 1 },
      { database: 'b', kind: 'table' as const, count: 5 },
    ]
    const bars = idleBars(idle)
    expect(bars.map((bar) => [bar.id, bar.value])).toEqual([
      ['b', 5],
      ['a', 3],
    ])
    expect(bars[1]?.segments?.map((s) => s.id)).toEqual(['function', 'index'])
    expect(idleTotal(idle)).toBe(8)
  })

  it('names a large table with its database, which the drill-down reads back', () => {
    const [bar] = tableBars(
      [{ database: 'a', schema: 'public', name: 'bet', sizeBytes: 2048, growth30dBytes: 1024 }],
      formatGrowth,
    )
    expect(bar?.label).toBe('a · public.bet')
    expect(bar?.detail).toBe('+1.0 KB in 30 days')
    expect(databaseOfTable(bar?.id ?? '')).toBe('a')
  })

  it('labels the slowest statements with their text, cut short, or their id', () => {
    const statement = (queryId: string, text: string, meanMs: number) =>
      ({ queryId, text, meanMs, calls: 3, database: 'a' }) as Statement
    const bars = statementBars([
      statement('1', 'select 1', 5),
      statement('2', `select ${'x, '.repeat(40)}y\n  from t`, 50),
      statement('3', '', 10),
    ])
    expect(bars.map((bar) => bar.id)).toEqual(['2', '3', '1'])
    expect(bars[0]?.label).toHaveLength(60)
    expect(bars[0]?.label.endsWith('…')).toBe(true)
    expect(bars[1]?.label).toBe('3')
  })

  it('flattens whitespace before cutting', () => {
    expect(truncate('select\n   1', 20)).toBe('select 1')
  })
})

describe('size history', () => {
  const database = (name: string, points: [string, number][]): SizedDatabase =>
    ({
      database: name,
      sizeBytes: points.at(-1)?.[1] ?? null,
      growth: points.map(([day, sizeBytes]) => ({ day, sizeBytes })),
    }) as SizedDatabase
  const window = {
    from: '2026-09-20T11:00:00.000Z',
    to: '2026-09-27T11:00:00.000Z',
    bucketSeconds: 3600,
  }
  const databases = [
    database('a', [
      ['2026-09-10', 100],
      ['2026-09-21', 150],
      ['2026-09-27', 200],
    ]),
    database('b', [
      ['2026-09-21', 10],
      ['2026-09-27', 30],
    ]),
  ]

  it('draws one line per database over the days in the range', () => {
    const lines = sizeSeries(databases, window)
    expect(lines.map((line) => line.id)).toEqual(['a', 'b'])
    expect(lines[0]?.points.map((p) => p.y)).toEqual([150, 200])
  })

  it('sums the size now, its growth since the window began and the daily points', () => {
    expect(sizeFigures(databases, window)).toEqual({
      now: 230,
      growth: 130,
      spark: [160, 230],
    })
  })

  it('has no figures before anything was measured', () => {
    expect(sizeFigures([], window)).toEqual({ now: null, growth: null, spark: null })
  })
})

describe('drill-downs', () => {
  it('open a tab with only the filters that are set', () => {
    expect(
      drill('last-used', { database: 'a', flagged: '1', operator: undefined, db: '' }),
    ).toEqual({ name: 'observatory', query: { tab: 'last-used', database: 'a', flagged: '1' } })
  })
})

describe('the KPI grid', () => {
  it.each([
    [{ usage: true, operators: false, server: true }, 7, 'lg:grid-cols-4'],
    [{ usage: true, operators: true, server: true }, 6, 'lg:grid-cols-3'],
    [{ usage: true, operators: false, server: false }, 4, 'lg:grid-cols-4'],
    [{ usage: false, operators: false, server: true }, 3, 'lg:grid-cols-3'],
  ])('lays %o out as %i cards with no card alone on a row', (panels, count, columns) => {
    expect(kpiCount(panels)).toBe(count)
    expect(kpiColumns(count)).toBe(columns)
    const width = Number(columns.slice('lg:grid-cols-'.length))
    const last = count % width
    expect(last === 0 || last >= width - 1).toBe(true)
  })
})
