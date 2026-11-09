import type { components } from '@/gen/api'

export type GuardLimits = components['schemas']['GuardLimits']

/** What the guard said about one statement's cost, from an inspect or a run. */
export type Measurement = {
  estimatedCost?: number | null
  estimatedRows?: number | null
  limits: GuardLimits
}

export const warnAt = 0.8

export type GaugeLevel = 'unmeasured' | 'no-plan' | 'clear' | 'near' | 'over'

export type Gauge = {
  level: GaugeLevel
  /** How far towards the cap, 0 to 1, pinned full when over. Null when there is no bar to draw. */
  fraction: number | null
  cost: string
  /** Empty when there is no figure to hold against it: "no plan / 50" reads as a measure. */
  maxCost: string
  rows: string
  maxRows: string
  rowsOver: boolean
}

const number = new Intl.NumberFormat('en', { maximumFractionDigits: 2 })

const shown = (value: number | null | undefined, missing: string) =>
  value === null || value === undefined ? missing : number.format(value)

function levelOf(cost: number, cap: number | null): GaugeLevel {
  if (cap === null) return 'clear'
  if (cost > cap) return 'over'
  return cap <= 0 || cost / cap >= warnAt ? 'near' : 'clear'
}

/**
 * The gauge for a measurement. No estimate reads "no plan", never zero: zero
 * would say the statement is free, which is a different claim.
 */
export function gaugeOf(measurement: Measurement | null): Gauge {
  if (measurement === null) {
    return {
      level: 'unmeasured',
      fraction: null,
      cost: '—',
      maxCost: '—',
      rows: '—',
      maxRows: '—',
      rowsOver: false,
    }
  }
  const { estimatedCost: cost, estimatedRows: rows, limits } = measurement
  const hasCost = cost !== null && cost !== undefined
  const cap = limits.maxCost
  const rowsOver =
    rows !== null && rows !== undefined && limits.maxRows !== null && rows > limits.maxRows
  return {
    level: hasCost ? levelOf(cost, cap) : 'no-plan',
    fraction:
      hasCost && cap !== null ? (cap <= 0 ? 1 : Math.min(1, Math.max(0, cost / cap))) : null,
    cost: shown(cost, 'no plan'),
    maxCost: hasCost ? shown(cap, 'no cap') : '',
    rows: shown(rows, 'no plan'),
    maxRows: rows === null || rows === undefined ? '' : shown(limits.maxRows, 'no cap'),
    rowsOver,
  }
}
