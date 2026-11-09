import { describe, expect, it } from 'vitest'

import { fullStamp, stamp } from './timestamp'

const now = new Date('2026-09-26T03:00:00Z')

describe('a printed moment', () => {
  it('is the time alone for today, in UTC', () => {
    expect(stamp('2026-09-26T02:51:00Z', now)).toBe('02:51')
    expect(stamp('2026-09-26T10:51:00+08:00', now)).toBe('02:51')
  })

  it('carries the month and day for an older one this year', () => {
    expect(stamp('2026-09-25T18:51:30Z', now)).toBe('09-25 18:51')
    expect(stamp('2026-09-26T01:30:00+08:00', now)).toBe('09-25 17:30')
    expect(stamp('2026-12-18T14:05:00Z', now)).toBe('12-18 14:05')
  })

  it('carries the year outside this one', () => {
    expect(stamp('2025-12-31T23:59:00Z', now)).toBe('2025-12-31 23:59')
    expect(stamp('2027-01-02T00:00:00Z', now)).toBe('2027-01-02 00:00')
  })

  it('reads the day in UTC, not in the browser', () => {
    expect(stamp('2026-09-26T00:05:00Z', new Date('2026-09-25T23:59:00-02:00'))).toBe('00:05')
  })

  it('titles the full ISO timestamp, and passes anything unreadable through', () => {
    expect(fullStamp('2026-09-25T18:51:30.123+00:00')).toBe('2026-09-25T18:51:30.123Z')
    expect(stamp('not a date', now)).toBe('not a date')
    expect(fullStamp('not a date')).toBe('not a date')
  })
})
