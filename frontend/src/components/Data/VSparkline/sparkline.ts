/**
 * The geometry of a sparkline: a series of values as the points of one line
 * in a fixed box, the lowest value on the floor and the highest on the ceiling.
 *
 * Kept apart from the component so the arithmetic is tested without a DOM.
 */
export const sparkWidth = 100
export const sparkHeight = 24
const inset = 2

export type SparkPoint = { x: number; y: number }

export function sparkPoints(values: readonly number[]): SparkPoint[] {
  if (values.length === 0) return []
  const low = Math.min(...values)
  const high = Math.max(...values)
  const span = high - low
  const floor = sparkHeight - inset
  const room = sparkHeight - inset * 2
  const step = values.length === 1 ? 0 : sparkWidth / (values.length - 1)
  return values.map((value, index) => ({
    x: values.length === 1 ? sparkWidth / 2 : round(index * step),
    // A flat series sits in the middle rather than on the floor, so no change
    // reads as no change and not as the smallest value.
    y: span === 0 ? sparkHeight / 2 : round(floor - ((value - low) / span) * room),
  }))
}

export function polyline(points: readonly SparkPoint[]): string {
  return points.map((point) => `${point.x},${point.y}`).join(' ')
}

/** How the series moved, first to last, for somebody who cannot see the line. */
export function trend(values: readonly number[]): 'rising' | 'falling' | 'flat' | 'empty' {
  if (values.length < 2) return values.length === 0 ? 'empty' : 'flat'
  const first = values[0] ?? 0
  const last = values[values.length - 1] ?? 0
  if (last > first) return 'rising'
  return last < first ? 'falling' : 'flat'
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}
