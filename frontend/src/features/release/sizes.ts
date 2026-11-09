import type { components } from '@/gen/api'

export type RelationSize = components['schemas']['RelationSize']

/**
 * How a row estimate reads on the review screen.
 *
 * `null` is not zero and must never render as a number. An approver is being
 * asked whether an index build holds a lock for a moment or for an hour, and a
 * table holding forty million rows shown as "0 rows" answers that wrongly
 * rather than declining to answer. FEAT-017 AC-21.
 */
export function describeRows(size: RelationSize): string {
  if (size.rows !== null) return `${new Intl.NumberFormat().format(size.rows)} rows`
  return size.inTarget ? 'no estimate yet' : 'not in the target'
}

const units = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB']

/** The size on disk, table and indexes and TOAST together. `null` as above. */
export function describeBytes(size: RelationSize): string {
  const bytes = size.totalBytes
  if (bytes === null) return size.inTarget ? 'unknown' : '—'

  let scaled = bytes
  let unit = 0
  while (scaled >= 1024 && unit < units.length - 1) {
    scaled /= 1024
    unit += 1
  }
  // Whole bytes, and one decimal above that: an approver compares magnitudes
  // here and a third significant figure is noise between them.
  const rounded = unit === 0 ? String(scaled) : scaled.toFixed(1)
  return `${rounded} ${units[unit]}`
}

/**
 * Whether the figures are worth a section at all.
 *
 * Nothing rather than an empty table when the target could not be asked: an
 * empty table reads as "these relations are empty", which is the one reading
 * AC-21 exists to prevent.
 */
export function anySizes(sizes: RelationSize[] | undefined): boolean {
  return sizes !== undefined && sizes.length > 0
}
