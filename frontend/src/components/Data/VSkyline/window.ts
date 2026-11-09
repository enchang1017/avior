import type { AuditBucket, Span } from './sky'

/** A window of the log: `from` inclusive, `to` exclusive, both instants. */
export type Window = { from: string; to: string }

export function windowOf(
  span: Span,
  buckets: readonly AuditBucket[],
  bucketSeconds: number,
): Window | null {
  const first = buckets[span.first]
  const last = buckets[span.last]
  if (first === undefined || last === undefined) return null
  const end = new Date(new Date(last.start).getTime() + bucketSeconds * 1000)
  return { from: new Date(first.start).toISOString(), to: end.toISOString() }
}

/** The span a window covers in these buckets, or null when it does not line up with them. */
export function spanFor(
  window: Window,
  buckets: readonly AuditBucket[],
  bucketSeconds: number,
): Span | null {
  const from = Date.parse(window.from)
  const to = Date.parse(window.to)
  if (Number.isNaN(from) || Number.isNaN(to) || to <= from) return null
  const starts = buckets.map((b) => Date.parse(b.start))
  const first = starts.indexOf(from)
  const last = starts.indexOf(to - bucketSeconds * 1000)
  return first === -1 || last === -1 || last < first ? null : { first, last }
}

/**
 * The inclusive end the log takes for an exclusive one: a microsecond before
 * it, which is PostgreSQL's timestamptz resolution, so nothing at the boundary
 * is counted twice or dropped.
 */
export function inclusiveEnd(exclusive: string): string {
  const millis = Date.parse(exclusive)
  if (Number.isNaN(millis)) throw new Error(`not an instant: ${exclusive}`)
  return new Date(millis - 1).toISOString().replace(/Z$/u, '999Z')
}

/** A window as the URL carries it, and back. Only whole, ordered instants survive the trip. */
export function windowFromQuery(from: unknown, to: unknown): Window | null {
  if (typeof from !== 'string' || typeof to !== 'string') return null
  const start = Date.parse(from)
  const end = Date.parse(to)
  if (Number.isNaN(start) || Number.isNaN(end) || end <= start) return null
  return { from: new Date(start).toISOString(), to: new Date(end).toISOString() }
}

export function windowLabel(window: Window): string {
  const at = (iso: string) => `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
  return `${at(window.from)} → ${at(window.to)}`
}
