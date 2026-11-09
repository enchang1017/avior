import { describe, expect, it } from 'vitest'

import { since } from './since'

const now = new Date('2026-09-26T03:00:00Z')

describe('how long ago', () => {
  it('reads in one unit, hours up to two days', () => {
    expect(since('2026-09-26T02:59:30Z', now)).toBe('just now')
    expect(since('2026-09-26T02:48:00Z', now)).toBe('12m ago')
    expect(since('2026-09-25T22:00:00Z', now)).toBe('5h ago')
    expect(since('2026-09-24T04:00:00Z', now)).toBe('47h ago')
    expect(since('2026-09-23T03:00:00Z', now)).toBe('3d ago')
  })

  it('reads a moment ahead of this clock as now, and passes anything unreadable through', () => {
    expect(since('2026-09-26T03:05:00Z', now)).toBe('just now')
    expect(since('not a date', now)).toBe('not a date')
  })
})
