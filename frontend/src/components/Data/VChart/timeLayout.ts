import {
  areaPath,
  linePath,
  logTicks,
  niceTicks,
  positiveRange,
  sharedXs,
  stack,
  valueRange,
  xScale,
  xTicks,
  yScale,
  yTicks,
  type Box,
  type Tick,
} from './geometry'
import type { ChartTone, Series, TimeChartMode, TimeChartScale } from './types'

export type DrawnSeries = {
  id: string
  label: string
  tone: ChartTone
  line: string
  area: string | null
  /** The height of the series' mark at each bucket: its value, or its band's ceiling. */
  tops: (number | null)[]
}

export type TimeLayout = {
  xs: number[]
  pixelsX: number[]
  xTicks: Tick[]
  yTicks: Tick[]
  drawn: DrawnSeries[]
  logarithmic: boolean
}

export type Formats = { formatY: (value: number) => string; formatX: (value: number) => string }

/** Everything the time chart draws, from its data and its size, in one pass. */
export function timeLayout(
  series: readonly Series[],
  mode: TimeChartMode,
  box: Box,
  formats: Formats,
  scale: TimeChartScale = 'linear',
): TimeLayout {
  const xs = sharedXs(series)
  const x = xScale(xs, box)
  const positive = scale === 'log' && mode === 'line' ? positiveRange(series) : null
  const floor = positive?.[0] ?? 1
  const at = (value: number | null) =>
    value === null ? null : positive ? Math.log10(Math.max(value, floor)) : value
  const [low, high] = valueRange(series, mode)
  const raw = positive ? logTicks(positive[0], positive[1]) : niceTicks(low, high, 3)
  const ticks = positive ? raw.map(Math.log10) : raw
  const y = yScale(ticks, box)
  const pixelsX = xs.map(x.toPixel)
  const bands = mode === 'stacked-area' ? stack(series) : null
  const drawn = series.map((one, index) => {
    const band = bands?.[index]
    const values = band ? band.high : one.points.map((point) => at(point.y))
    return {
      id: one.id,
      label: one.label,
      tone: one.tone,
      line: linePath(pixelsX, values, y.toPixel),
      area: band ? areaPath(pixelsX, band, y.toPixel) : null,
      tops: values.map((value) => (value === null ? null : y.toPixel(value))),
    }
  })
  return {
    xs,
    pixelsX,
    xTicks: xTicks(xs, x, box, formats.formatX),
    yTicks: positive
      ? raw.map((value) => ({
          value,
          at: y.toPixel(Math.log10(value)),
          label: formats.formatY(value),
        }))
      : yTicks(ticks, y, formats.formatY),
    drawn,
    logarithmic: positive !== null,
  }
}

export type Reading = { id: string; label: string; tone: ChartTone; text: string }

/** Each series' exact value at one bucket, as the tooltip lists it. */
export function readingsAt(
  series: readonly Series[],
  mode: TimeChartMode,
  index: number,
  formatY: (value: number) => string,
): Reading[] {
  return series.map((one) => {
    const value = one.points[index]?.y ?? null
    const shown = value === null && mode === 'stacked-area' ? 0 : value
    return {
      id: one.id,
      label: one.label,
      tone: one.tone,
      text: shown === null ? 'no value' : formatY(shown),
    }
  })
}
