import { describe, expect, it } from 'vitest'

import { bars, bucketAt, dayTicks, skyHeight, spanOf, totals, type AuditBucket } from './sky'
import { inclusiveEnd, spanFor, windowFromQuery, windowLabel, windowOf } from './window'

const hour = 3600

function week(start = '2026-09-18T00:00:00Z', count = 168): AuditBucket[] {
  const base = Date.parse(start)
  return Array.from({ length: count }, (_, i) => ({
    start: new Date(base + i * hour * 1000).toISOString(),
    allowed: i % 5,
    denied: i % 7 === 0 ? 2 : 0,
  }))
}

describe('the sky as bars', () => {
  it('stacks denied on allowed, scaled to the tallest hour', () => {
    const drawn = bars([
      { start: '2026-09-18T00:00:00Z', allowed: 3, denied: 1 },
      { start: '2026-09-18T01:00:00Z', allowed: 0, denied: 0 },
      { start: '2026-09-18T02:00:00Z', allowed: 1, denied: 1 },
    ])
    expect(drawn[0]).toMatchObject({
      allowedHeight: 48,
      deniedHeight: 16,
      deniedY: 0,
      allowedY: 16,
    })
    expect(drawn[1]).toMatchObject({ allowedHeight: 0, deniedHeight: 0, allowedY: skyHeight })
    expect(drawn[2]?.x).toBeCloseTo(666.7, 1)
  })

  it('draws an empty week flat rather than dividing by nothing', () => {
    const drawn = bars(week().map((b) => ({ ...b, allowed: 0, denied: 0 })))
    expect(drawn).toHaveLength(168)
    expect(drawn.every((b) => b.allowedHeight === 0 && b.deniedHeight === 0)).toBe(true)
  })

  it('counts allowed and denied apart', () => {
    expect(
      totals([
        { start: 'a', allowed: 2, denied: 1 },
        { start: 'b', allowed: 3, denied: 0 },
      ]),
    ).toEqual({ allowed: 5, denied: 1 })
  })

  it('ticks each UTC midnight', () => {
    const ticks = dayTicks(week('2026-09-18T05:00:00Z'))
    expect(ticks.map((t) => t.label)).toEqual([
      '09-19',
      '09-20',
      '09-21',
      '09-22',
      '09-23',
      '09-24',
      '09-25',
    ])
  })
})

describe('the brush', () => {
  it('snaps a point to the bucket under it, and stays on the strip', () => {
    expect(bucketAt(0, 168)).toBe(0)
    expect(bucketAt(0.5, 168)).toBe(84)
    expect(bucketAt(1, 168)).toBe(167)
    expect(bucketAt(-0.2, 168)).toBe(0)
    expect(bucketAt(1.4, 168)).toBe(167)
    expect(bucketAt(0.5, 0)).toBe(0)
  })

  it('covers the buckets between where it started and where it is, either way', () => {
    expect(spanOf(10, 4)).toEqual({ first: 4, last: 10 })
    expect(spanOf(4, 4)).toEqual({ first: 4, last: 4 })
  })

  it('turns a span into a half-open window of whole hours', () => {
    const buckets = week()
    expect(windowOf({ first: 2, last: 4 }, buckets, hour)).toEqual({
      from: '2026-09-18T02:00:00.000Z',
      to: '2026-09-18T05:00:00.000Z',
    })
    expect(windowOf({ first: 2, last: 400 }, buckets, hour)).toBeNull()
  })

  it('finds a window in the buckets again, and refuses one that does not line up', () => {
    const buckets = week()
    const window = { from: '2026-09-18T02:00:00.000Z', to: '2026-09-18T05:00:00.000Z' }
    expect(spanFor(window, buckets, hour)).toEqual({ first: 2, last: 4 })
    expect(spanFor({ from: window.from, to: '2026-09-18T05:30:00Z' }, buckets, hour)).toBeNull()
    expect(
      spanFor({ from: '2026-10-18T02:00:00Z', to: '2026-10-18T05:00:00Z' }, buckets, hour),
    ).toBeNull()
    expect(spanFor({ from: window.to, to: window.from }, buckets, hour)).toBeNull()
  })

  it('ends the log a microsecond before the exclusive end', () => {
    expect(inclusiveEnd('2026-09-18T05:00:00.000Z')).toBe('2026-09-18T04:59:59.999999Z')
    expect(() => inclusiveEnd('soon')).toThrow()
  })

  it('reads a window back from the URL only when it is whole and ordered', () => {
    expect(windowFromQuery('2026-09-18T02:00:00Z', '2026-09-18T05:00:00Z')).toEqual({
      from: '2026-09-18T02:00:00.000Z',
      to: '2026-09-18T05:00:00.000Z',
    })
    expect(windowFromQuery('2026-09-18T05:00:00Z', '2026-09-18T02:00:00Z')).toBeNull()
    expect(windowFromQuery('yesterday', '2026-09-18T02:00:00Z')).toBeNull()
    expect(windowFromQuery(undefined, '2026-09-18T02:00:00Z')).toBeNull()
  })

  it('labels a window in UTC', () => {
    expect(windowLabel({ from: '2026-09-18T02:00:00.000Z', to: '2026-09-20T15:00:00.000Z' })).toBe(
      '09-18 02:00 → 09-20 15:00',
    )
  })
})
