import type { components } from '@/gen/api'

export type AuditHistogram = components['schemas']['AuditHistogram']
export type AuditBucket = components['schemas']['AuditBucket']

export const skyWidth = 1000
export const skyHeight = 64

export type Bar = {
  start: string
  x: number
  width: number
  allowedY: number
  allowedHeight: number
  deniedY: number
  deniedHeight: number
}

export type Tick = { x: number; label: string }

/** A brushed window: the first and the last bucket it covers, inclusive. */
export type Span = { first: number; last: number }

export function slotWidth(count: number): number {
  return count === 0 ? skyWidth : skyWidth / count
}

/** The buckets as stacked bars: allowed at the bottom in starlight, denied above in red. */
export function bars(buckets: readonly AuditBucket[]): Bar[] {
  const tallest = Math.max(1, ...buckets.map((b) => b.allowed + b.denied))
  const slot = slotWidth(buckets.length)
  const scale = (count: number) => Math.round((count / tallest) * skyHeight * 10) / 10
  return buckets.map((bucket, index) => {
    const allowedHeight = scale(bucket.allowed)
    const deniedHeight = scale(bucket.denied)
    return {
      start: bucket.start,
      x: Math.round(index * slot * 10) / 10,
      width: Math.max(1, Math.round((slot - 1) * 10) / 10),
      allowedY: skyHeight - allowedHeight,
      allowedHeight,
      deniedY: skyHeight - allowedHeight - deniedHeight,
      deniedHeight,
    }
  })
}

export function totals(buckets: readonly AuditBucket[]): { allowed: number; denied: number } {
  return buckets.reduce(
    (sum, b) => ({ allowed: sum.allowed + b.allowed, denied: sum.denied + b.denied }),
    { allowed: 0, denied: 0 },
  )
}

const dayLabel = (iso: string) => iso.slice(5, 10)

/** One tick per UTC midnight in the window, labelled MM-DD. */
export function dayTicks(buckets: readonly AuditBucket[]): Tick[] {
  const slot = slotWidth(buckets.length)
  return buckets.flatMap((bucket, index) =>
    new Date(bucket.start).getUTCHours() === 0
      ? [
          {
            x: Math.round(index * slot * 10) / 10,
            label: dayLabel(new Date(bucket.start).toISOString()),
          },
        ]
      : [],
  )
}

/** The bucket under a point along the strip, as a fraction of its width. */
export function bucketAt(fraction: number, count: number): number {
  if (count === 0) return 0
  return Math.min(count - 1, Math.max(0, Math.floor(fraction * count)))
}

export function spanOf(anchor: number, current: number): Span {
  return { first: Math.min(anchor, current), last: Math.max(anchor, current) }
}
