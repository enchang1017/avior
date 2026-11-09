import { describe, expect, it } from 'vitest'

import {
  daysAgoText,
  flagPillText,
  flagText,
  formatBytes,
  formatCount,
  formatDelta,
  formatGrowth,
  formatMs,
  formatPercent,
  groupCountText,
  lastUsedText,
  neverSeenSentence,
  newestFirst,
  utcDay,
  type ObservatoryObject,
  type RoutineGroup,
  type RoutineVersion,
  type Tracking,
} from './text'

const now = new Date('2026-09-27T10:00:00Z')

const tracking: Tracking = {
  idleDays: 30,
  routinesTrackedSince: '2026-09-27T00:15:00Z',
  relationsTrackedSince: '2026-08-01T00:00:00Z',
}

function object(overrides: Partial<ObservatoryObject>): ObservatoryObject {
  return {
    schema: 'public',
    name: 'thing',
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

function version(name: string, date: string, overrides: Partial<RoutineVersion> = {}) {
  return {
    name,
    signature: 'bigint',
    date,
    latest: false,
    lastUsedAt: null,
    daysAgo: null,
    flagged: false,
    ...overrides,
  }
}

const banned = /dead|unused|obsolete|deprecated/iu

describe('days ago', () => {
  it('says today, one day, and many days', () => {
    expect(daysAgoText(0)).toBe('today')
    expect(daysAgoText(1)).toBe('1 day ago')
    expect(daysAgoText(15)).toBe('15 days ago')
  })

  it('takes the UTC day of an instant', () => {
    expect(utcDay('2026-09-12T23:30:00-02:00')).toBe('2026-09-13')
    expect(utcDay('not a date')).toBe('not a date')
  })
})

describe('lastUsedText', () => {
  it('writes the sentence the owner asked for, one verb per kind of use', () => {
    const run = object({
      kind: 'function',
      lastUsedAt: '2026-09-12T08:00:00Z',
      lastUsedKind: 'run',
      daysAgo: 15,
    })
    expect(lastUsedText(run, tracking, now)).toBe('last run 2026-09-12 (15 days ago)')
    expect(
      lastUsedText(
        object({ lastUsedAt: '2026-09-20T00:00:00Z', lastUsedKind: 'written', daysAgo: 7 }),
        tracking,
        now,
      ),
    ).toBe('last written 2026-09-20 (7 days ago)')
    expect(
      lastUsedText(
        object({ kind: 'index', lastUsedAt: '2026-09-26T00:00:00Z', lastUsedKind: 'scanned' }),
        tracking,
        now,
      ),
    ).toBe('last scanned 2026-09-26 (1 day ago)')
  })

  it('falls back to the kind when the server names no use, and computes the days itself', () => {
    const table = object({ lastUsedAt: '2026-08-20T09:00:00Z' })
    expect(lastUsedText(table, tracking, now)).toBe('last used 2026-08-20 (38 days ago)')
  })

  it('says since when Avior has looked for an object never seen used', () => {
    expect(lastUsedText(object({}), tracking, now)).toBe(
      'not seen since tracking began on 2026-08-01 (57 days)',
    )
    expect(lastUsedText(object({ kind: 'procedure' }), tracking, now)).toBe(
      'not seen since tracking began on 2026-09-27',
    )
    expect(neverSeenSentence(null, 30, now)).toBe('not seen; tracking has not begun')
    expect(neverSeenSentence('2026-08-27T00:00:00Z', 30, now)).toBe(
      'not seen since tracking began on 2026-08-27 (31 days)',
    )
  })

  it('names why an object is not tracked', () => {
    const sql = object({
      kind: 'function',
      tracked: false,
      notTrackedReason: 'LANGUAGE sql needs track_functions = all',
    })
    expect(lastUsedText(sql, tracking, now)).toBe(
      'not tracked (LANGUAGE sql needs track_functions = all)',
    )
    expect(lastUsedText(object({ tracked: false }), tracking, now)).toBe(
      'not tracked (no reason given)',
    )
  })
})

describe('flag wording', () => {
  it('states the age, never a verdict', () => {
    expect(flagText(30)).toBe('flagged: last use more than 30 days ago')
    expect(flagText(1)).toBe('flagged: last use more than 1 day ago')
    expect(flagText(30)).not.toMatch(banned)
  })

  it('shortens the pill to the idle window, never a verdict', () => {
    expect(flagPillText(30)).toBe('idle > 30 days')
    expect(flagPillText(1)).toBe('idle > 1 day')
    expect(flagPillText(30)).not.toMatch(banned)
  })
})

describe('routine groups', () => {
  const group: RoutineGroup = {
    schema: 'public',
    base: 'shop_settle_order',
    latest: 'shop_settle_order_26_09_25',
    flaggedCount: 2,
    lastUsedAt: '2026-09-25T12:00:00Z',
    versions: [
      version('shop_settle_order_26_09_19', '2026-09-19', { flagged: true }),
      version('shop_settle_order_26_09_25', '2026-09-25', {
        latest: true,
        lastUsedAt: '2026-09-25T12:00:00Z',
        daysAgo: 2,
      }),
      version('shop_settle_order_26_09_21', '2026-09-21', { flagged: true }),
    ],
  }

  it('lists versions newest first without reordering what it was given', () => {
    expect(newestFirst(group.versions).map((one) => one.date)).toEqual([
      '2026-09-25',
      '2026-09-21',
      '2026-09-19',
    ])
    expect(group.versions[0]?.date).toBe('2026-09-19')
  })

  it('counts the versions, and the flagged ones only when there are any', () => {
    expect(groupCountText(group)).toBe('3 versions · 2 flagged')
    expect(
      groupCountText({ ...group, flaggedCount: 0, versions: group.versions.slice(0, 1) }),
    ).toBe('1 version')
  })
})

describe('figures', () => {
  it('writes sizes in binary units, and growth with its sign', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(-40 * 1024 ** 3)).toBe('−40.0 GB')
    expect(formatDelta(3 * 1024 ** 2, 1024 ** 2)).toBe('+2.0 MB')
    expect(formatDelta(1024, 2048)).toBe('−1.0 KB')
    expect(formatDelta(1024, 1024)).toBe('no change')
    expect(formatDelta(1024, null)).toBe('—')
    expect(formatGrowth(null)).toBe('—')
    expect(formatGrowth(0)).toBe('no change')
    expect(formatGrowth(2048)).toBe('+2.0 KB')
    expect(formatGrowth(-2048)).toBe('−2.0 KB')
  })

  it('writes durations at the scale they are read in', () => {
    expect(formatMs(null)).toBe('—')
    expect(formatMs(12.34)).toBe('12.3 ms')
    expect(formatMs(3400)).toBe('3.4 s')
    expect(formatMs(303_000)).toBe('5 min 3 s')
    expect(formatMs(7_500_000)).toBe('2 h 5 min')
    expect(formatPercent(0.1234)).toBe('12.3%')
  })
})

describe('counts', () => {
  it.each([
    [0, '0'],
    [7, '7'],
    [999, '999'],
    [1000, '1,000'],
    [219630, '219,630'],
    [1234567, '1,234,567'],
    [-4200, '-4,200'],
    [1234.5, '1,234.5'],
    [0.25, '0.25'],
  ])('writes %d as %s, as en-US would', (value, text) => {
    expect(formatCount(value)).toBe(text)
    expect(formatCount(value)).toBe(value.toLocaleString('en-US'))
  })
})
