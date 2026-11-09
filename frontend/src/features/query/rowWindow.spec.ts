import { describe, expect, it } from 'vitest'

import { maxRendered, rowWindow } from './rowWindow'

describe('rowWindow', () => {
  it('renders every row of a short result', () => {
    expect(rowWindow(3, 0, 600, 32)).toEqual({ start: 0, end: 3, before: 0, after: 0 })
  })

  it('never renders more than sixty rows of a long one', () => {
    const shown = rowWindow(100_000, 0, 10_000, 32)
    expect(shown.end - shown.start).toBe(maxRendered)
  })

  // The spacers stand in for the rows that are not drawn, so the scrollbar is
  // the length of the whole result.
  it('follows the scroll and pads for what it leaves out', () => {
    const shown = rowWindow(500, 3200, 640, 32)
    expect(shown.start).toBe(92)
    expect(shown.end).toBe(128)
    expect(shown.before + (shown.end - shown.start) * 32 + shown.after).toBe(500 * 32)
  })

  it('stops at the end rather than running past it', () => {
    const shown = rowWindow(500, 1_000_000, 640, 32)
    expect(shown.end).toBe(500)
    expect(shown.after).toBe(0)
  })
})
