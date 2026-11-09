import type { PartitionEntry } from './useTablePartitions'

/** How many of the newest partitions the table page shows before "Show all". */
export const recentCount = 12

/** How many partitions one page of the full list holds. */
export const pageSize = 50

/*
 * The server orders a range's partitions by upper bound, lowest first, with
 * the DEFAULT last, so newest is the end of the list. A table partitioned by
 * month for twenty years has two hundred and forty of them, and the ones an
 * operator asks about are this month's, the next ones and the DEFAULT.
 */
function bounded(partitions: PartitionEntry[]): PartitionEntry[] {
  return partitions.filter((partition) => !partition.isDefault)
}

/** The DEFAULT partition, then the newest few, newest first. */
export function recent(partitions: PartitionEntry[]): PartitionEntry[] {
  const newest = bounded(partitions).slice(-recentCount).reverse()
  return [...partitions.filter((partition) => partition.isDefault), ...newest]
}

export type ListQuery = { filter: string; newestFirst: boolean; page: number }

export type ListPage = { rows: PartitionEntry[]; matched: number; pages: number; page: number }

/** One page of every partition, filtered on name and bound, in the order asked for. */
export function listPage(partitions: PartitionEntry[], query: ListQuery): ListPage {
  const needle = query.filter.trim().toLowerCase()
  const ordered = query.newestFirst ? [...partitions].reverse() : [...partitions]
  const matching = ordered.filter(
    (partition) =>
      needle === '' ||
      partition.name.toLowerCase().includes(needle) ||
      partition.bound.toLowerCase().includes(needle),
  )
  const pages = Math.max(1, Math.ceil(matching.length / pageSize))
  const page = Math.min(Math.max(1, query.page), pages)
  return {
    rows: matching.slice((page - 1) * pageSize, page * pageSize),
    matched: matching.length,
    pages,
    page,
  }
}
