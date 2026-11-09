import { describe, expect, it } from 'vitest'

import { anySizes, describeBytes, describeRows, type RelationSize } from './sizes'

function size(over: Partial<RelationSize> = {}): RelationSize {
  return { relation: 'public.bet', inTarget: true, rows: 0, totalBytes: 0, ...over }
}

describe('the relation sizes on the review screen', () => {
  it('shows a real zero as a number', () => {
    expect(describeRows(size({ rows: 0 }))).toBe('0 rows')
    expect(describeBytes(size({ totalBytes: 0 }))).toBe('0 bytes')
  })

  // The whole point of AC-21: a figure nobody obtained must not read as one.
  it('never renders an absent figure as zero', () => {
    expect(describeRows(size({ rows: null }))).toBe('no estimate yet')
    expect(describeRows(size({ rows: null, inTarget: false }))).toBe('not in the target')
    expect(describeBytes(size({ totalBytes: null }))).toBe('unknown')
    expect(describeBytes(size({ totalBytes: null, inTarget: false }))).toBe('—')
  })

  it('groups a large estimate so it can be read at a glance', () => {
    const shown = describeRows(size({ rows: 41_000_000 }))

    expect(shown.endsWith(' rows')).toBe(true)
    expect(shown.replace(/\D/g, '')).toBe('41000000')
    // Longer than the bare digits, whichever separator the locale uses.
    expect(shown.length).toBeGreaterThan('41000000 rows'.length)
  })

  it('scales bytes to the unit an approver compares in', () => {
    expect(describeBytes(size({ totalBytes: 512 }))).toBe('512 bytes')
    expect(describeBytes(size({ totalBytes: 2048 }))).toBe('2.0 KiB')
    expect(describeBytes(size({ totalBytes: 3 * 1024 ** 3 }))).toBe('3.0 GiB')
    expect(describeBytes(size({ totalBytes: 5 * 1024 ** 5 }))).toBe('5120.0 TiB')
  })

  it('has no section to show when the target could not be asked', () => {
    expect(anySizes([])).toBe(false)
    expect(anySizes(undefined)).toBe(false)
    expect(anySizes([size()])).toBe(true)
  })
})
