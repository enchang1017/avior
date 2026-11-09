import type { BarDatum, Series, TimeChartMode } from './types'

/**
 * The arithmetic of the chart primitives, kept apart from the components so it
 * is tested without a DOM. Every function here is pure and linear in the
 * number of points: 12 series of 744 buckets is the load it is sized for.
 */

export type Frame = { left: number; right: number; top: number; bottom: number }
export type Box = { width: number; height: number; frame: Frame }
export type Tick = { value: number; at: number; label: string }
export type Band = { low: number[]; high: number[] }

export const frame: Frame = { left: 56, right: 12, top: 8, bottom: 22 }

export function plotWidth(box: Box): number {
  return Math.max(1, box.width - box.frame.left - box.frame.right)
}

export function plotHeight(box: Box): number {
  return Math.max(1, box.height - box.frame.top - box.frame.bottom)
}

function round(value: number): number {
  return Math.round(value * 10) / 10
}

/** The x values every series shares: the first series' points, since all agree. */
export function sharedXs(series: readonly Series[]): number[] {
  return (series[0]?.points ?? []).map((point) => point.x)
}

/** True when nothing would be drawn: no points, or every value null or zero. */
export function isBlank(series: readonly Series[]): boolean {
  return series.every((one) => one.points.every((point) => point.y === null || point.y === 0))
}

/**
 * Each series' band in a stacked chart: its floor is the sum of those below
 * it, its ceiling that plus its own value. A null counts as zero.
 */
export function stack(series: readonly Series[]): Band[] {
  const count = sharedXs(series).length
  let floor: number[] = new Array<number>(count).fill(0)
  return series.map((one) => {
    const high = floor.map((base, index) => base + (one.points[index]?.y ?? 0))
    const band = { low: floor, high }
    floor = high
    return band
  })
}

/** The lowest and highest value the chart must hold, always including zero. */
export function valueRange(series: readonly Series[], mode: TimeChartMode): [number, number] {
  if (mode === 'stacked-area') {
    const top = stack(series).at(-1)?.high ?? []
    return [0, top.reduce((most, value) => Math.max(most, value), 0)]
  }
  let low = 0
  let high = 0
  for (const one of series) {
    for (const point of one.points) {
      if (point.y === null) continue
      if (point.y < low) low = point.y
      if (point.y > high) high = point.y
    }
  }
  return [low, high]
}

/**
 * The powers of ten that cover [low, high] on a logarithmic axis, at least two
 * of them. Both bounds must be positive.
 */
export function logTicks(low: number, high: number): number[] {
  const first = Math.floor(Math.log10(low))
  const last = Math.max(first + 1, Math.ceil(Math.log10(high)))
  const ticks: number[] = []
  for (let power = first; power <= last; power += 1) ticks.push(10 ** power)
  return ticks
}

/** The smallest and largest positive value drawn, or null when there is none. */
export function positiveRange(series: readonly Series[]): [number, number] | null {
  let low = Infinity
  let high = 0
  for (const one of series) {
    for (const point of one.points) {
      if (point.y === null || point.y <= 0) continue
      if (point.y < low) low = point.y
      if (point.y > high) high = point.y
    }
  }
  return high > 0 ? [low, high] : null
}

/** A step of 1, 2, 2.5 or 5 times a power of ten, near the rough one asked for. */
export function niceStep(rough: number): number {
  if (!(rough > 0) || !Number.isFinite(rough)) return 1
  const power = 10 ** Math.floor(Math.log10(rough))
  const scaled = rough / power
  const nice = scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 2.5 ? 2.5 : scaled <= 5 ? 5 : 10
  return nice * power
}

/** Round-numbered values that cover [low, high] in about `count` intervals. */
export function niceTicks(low: number, high: number, count = 3): number[] {
  if (high <= low) return low === 0 ? [0, 1] : [Math.min(0, low), Math.max(0, high) || 1]
  const step = niceStep((high - low) / count)
  const first = Math.floor(low / step) * step
  const last = Math.ceil(high / step) * step
  const ticks: number[] = []
  for (let value = first; value <= last + step / 2; value += step) {
    ticks.push(Math.round(value / step) * step)
  }
  return ticks
}

export type Scale = { toPixel: (value: number) => number; domain: [number, number] }

export function xScale(xs: readonly number[], box: Box): Scale {
  const first = xs[0] ?? 0
  const last = xs[xs.length - 1] ?? first
  const span = last - first
  const width = plotWidth(box)
  const left = box.frame.left
  return {
    domain: [first, last],
    toPixel: (value) =>
      round(span === 0 ? left + width / 2 : left + ((value - first) / span) * width),
  }
}

export function yScale(ticks: readonly number[], box: Box): Scale {
  const low = ticks[0] ?? 0
  const high = ticks[ticks.length - 1] ?? 1
  const span = high - low || 1
  const height = plotHeight(box)
  const floor = box.frame.top + height
  return {
    domain: [low, high],
    toPixel: (value) => round(floor - ((value - low) / span) * height),
  }
}

/**
 * A line through the values, broken where one is null. A value with no
 * neighbour becomes a zero-length stroke, which a round cap draws as a dot.
 */
export function linePath(
  pixelsX: readonly number[],
  values: readonly (number | null)[],
  toY: (value: number) => number,
): string {
  const parts: string[] = []
  let open = false
  values.forEach((value, index) => {
    if (value === null) {
      open = false
      return
    }
    const next = values[index + 1]
    const lonely = !open && (next === null || next === undefined)
    const point = `${pixelsX[index] ?? 0} ${toY(value)}`
    parts.push(open ? `L${point}` : lonely ? `M${point}l0 0` : `M${point}`)
    open = true
  })
  return parts.join('')
}

/** The outline of a band: along its ceiling left to right, back along its floor. */
export function areaPath(
  pixelsX: readonly number[],
  band: Band,
  toY: (value: number) => number,
): string {
  if (pixelsX.length === 0) return ''
  const top = band.high.map((value, index) => `${pixelsX[index] ?? 0} ${toY(value)}`)
  const bottom = band.low.map((value, index) => `${pixelsX[index] ?? 0} ${toY(value)}`).reverse()
  return `M${top.join('L')}L${bottom.join('L')}Z`
}

/** The index of the value nearest `target` in a sorted list, by bisection. */
export function nearestIndex(sorted: readonly number[], target: number): number {
  if (sorted.length === 0) return -1
  let low = 0
  let high = sorted.length - 1
  while (high - low > 1) {
    const middle = (low + high) >> 1
    if ((sorted[middle] ?? 0) <= target) low = middle
    else high = middle
  }
  const below = sorted[low] ?? 0
  const above = sorted[high] ?? 0
  return Math.abs(target - below) <= Math.abs(above - target) ? low : high
}

/** Up to `count` indices spread evenly from the first to the last. */
export function spreadIndices(length: number, count: number): number[] {
  if (length <= 0) return []
  if (length === 1 || count <= 1) return [0]
  const wanted = Math.min(length, count)
  const indices = Array.from({ length: wanted }, (_, step) =>
    Math.round((step * (length - 1)) / (wanted - 1)),
  )
  return [...new Set(indices)]
}

export function xTicks(
  xs: readonly number[],
  scale: Scale,
  box: Box,
  format: (value: number) => string,
): Tick[] {
  const count = Math.max(2, Math.floor(plotWidth(box) / 110))
  return spreadIndices(xs.length, count).map((index) => {
    const value = xs[index] ?? 0
    return { value, at: scale.toPixel(value), label: format(value) }
  })
}

export function yTicks(
  values: readonly number[],
  scale: Scale,
  format: (value: number) => string,
): Tick[] {
  return values.map((value) => ({ value, at: scale.toPixel(value), label: format(value) }))
}

/** Where a pointer at `clientX` falls in the chart's own coordinates. */
export function toChartX(clientX: number, left: number, shownWidth: number, width: number): number {
  if (shownWidth <= 0) return clientX - left
  return ((clientX - left) / shownWidth) * width
}

type Extent = { min: number; max: number; last: number | null }

function extent(values: readonly (number | null)[]): Extent | null {
  let min = Infinity
  let max = -Infinity
  let last: number | null = null
  for (const value of values) {
    if (value === null) continue
    if (value < min) min = value
    if (value > max) max = value
    last = value
  }
  return last === null ? null : { min, max, last }
}

/** One sentence per series, for somebody who cannot see the lines. */
export function describeSeries(
  series: readonly Series[],
  formatY: (value: number) => string,
  formatX: (value: number) => string,
): string {
  const xs = sharedXs(series)
  const first = xs[0]
  const last = xs[xs.length - 1]
  if (first === undefined || last === undefined) return 'No data.'
  const lines = series.map((one) => {
    const found = extent(one.points.map((point) => point.y))
    if (found === null) return `${one.label}: no values.`
    return `${one.label}: low ${formatY(found.min)}, high ${formatY(found.max)}, latest ${formatY(found.last ?? 0)}.`
  })
  return [`From ${formatX(first)} to ${formatX(last)}.`, ...lines].join(' ')
}

/** A bar's length, whole or as the sum of its segments. */
export function barTotal(datum: BarDatum): number {
  if (datum.segments === undefined || datum.segments.length === 0) return datum.value
  return datum.segments.reduce((sum, segment) => sum + segment.value, 0)
}

/** Each bar's share of the longest, 0 to 1. */
export function barFractions(data: readonly BarDatum[]): number[] {
  const totals = data.map(barTotal)
  const longest = totals.reduce((most, value) => Math.max(most, value), 0)
  return totals.map((value) => (longest > 0 ? Math.max(0, value) / longest : 0))
}

export function describeBars(
  data: readonly BarDatum[],
  formatValue: (value: number) => string,
): string {
  if (data.length === 0) return 'No data.'
  const largest = data.reduce((top, datum) => (barTotal(datum) > barTotal(top) ? datum : top))
  const noun = data.length === 1 ? 'bar' : 'bars'
  return `${data.length} ${noun}; the largest is ${largest.label} at ${formatValue(barTotal(largest))}.`
}
