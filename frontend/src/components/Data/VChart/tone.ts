import type { ChartTone } from './types'

/**
 * The classes that draw a series in each tone, written out in full so
 * Tailwind sees every one of them in the source. `series` is the quiet second
 * line beside a primary one; `muted` is quieter still.
 */
export const chartStroke: Record<ChartTone, string> = {
  primary: 'stroke-primary',
  danger: 'stroke-danger',
  warn: 'stroke-warn',
  ok: 'stroke-ok',
  muted: 'stroke-fg-muted',
  series: 'stroke-fg-soft',
}

export const chartFill: Record<ChartTone, string> = {
  primary: 'fill-primary/30',
  danger: 'fill-danger/30',
  warn: 'fill-warn/30',
  ok: 'fill-ok/30',
  muted: 'fill-fg-muted/30',
  series: 'fill-fg-soft/30',
}

export const chartSwatch: Record<ChartTone, string> = {
  primary: 'bg-primary',
  danger: 'bg-danger',
  warn: 'bg-warn',
  ok: 'bg-ok',
  muted: 'bg-fg-muted',
  series: 'bg-fg-soft',
}

export const chartDot: Record<ChartTone, string> = {
  primary: 'fill-primary',
  danger: 'fill-danger',
  warn: 'fill-warn',
  ok: 'fill-ok',
  muted: 'fill-fg-muted',
  series: 'fill-fg-soft',
}
