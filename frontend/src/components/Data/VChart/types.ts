export type ChartTone = 'primary' | 'danger' | 'warn' | 'ok' | 'muted' | 'series'

export type SeriesPoint = {
  x: number
  y: number | null
}

export type Series = {
  id: string
  label: string
  tone: ChartTone
  points: readonly SeriesPoint[]
}

export type TimeChartMode = 'line' | 'stacked-area'

export type TimeChartScale = 'linear' | 'log'

export type BarSegment = {
  id: string
  label: string
  tone: ChartTone
  value: number
}

export type BarDatum = {
  id: string
  label: string
  value: number
  segments?: readonly BarSegment[]
  detail?: string
}

export type Delta = {
  text: string
  direction: 'up' | 'down' | 'flat'
  good: boolean | null
}
