import { describe, expect, it } from 'vitest'

import { listPage, pageSize, recent, recentCount } from './partitionList'
import type { PartitionEntry } from './useTablePartitions'

function month(i: number): PartitionEntry {
  const label = `p${String(i).padStart(3, '0')}`
  return {
    schema: 'public',
    name: `rounds_${label}`,
    bound: `FOR VALUES FROM ('${label}') TO ('${label}z')`,
    isDefault: false,
    partitioned: false,
    approxRows: i,
    totalBytes: 8192,
  }
}

const fallback: PartitionEntry = {
  schema: 'public',
  name: 'rounds_default',
  bound: 'DEFAULT',
  isDefault: true,
  partitioned: false,
  approxRows: 3,
  totalBytes: 8192,
}

// As the server sends them: by upper bound, lowest first, DEFAULT last.
const partitions = [...Array.from({ length: 300 }, (_, i) => month(i)), fallback]

describe('partitionList', () => {
  it('shows the DEFAULT and the newest partitions, newest first', () => {
    const names = recent(partitions).map((partition) => partition.name)
    expect(names).toHaveLength(recentCount + 1)
    expect(names.slice(0, 3)).toEqual(['rounds_default', 'rounds_p299', 'rounds_p298'])
    expect(names.at(-1)).toBe('rounds_p288')
  })

  it('shows every partition there is when there are fewer than that', () => {
    expect(recent([month(0), fallback]).map((partition) => partition.name)).toEqual([
      'rounds_default',
      'rounds_p000',
    ])
  })

  it('pages the whole list, newest or oldest first', () => {
    const newest = listPage(partitions, { filter: '', newestFirst: true, page: 1 })
    expect(newest.rows).toHaveLength(pageSize)
    expect(newest.pages).toBe(7)
    expect(newest.rows[0]?.name).toBe('rounds_default')
    expect(newest.rows[1]?.name).toBe('rounds_p299')

    const second = listPage(partitions, { filter: '', newestFirst: false, page: 2 })
    expect(second.rows[0]?.name).toBe('rounds_p050')
    expect(second.page).toBe(2)
  })

  it('filters on name and bound, and keeps the page inside what matched', () => {
    const byName = listPage(partitions, { filter: 'P01', newestFirst: false, page: 4 })
    expect(byName.matched).toBe(10)
    expect(byName.page).toBe(1)
    expect(byName.rows[0]?.name).toBe('rounds_p010')

    const byBound = listPage(partitions, { filter: "'p007z'", newestFirst: false, page: 1 })
    expect(byBound.rows.map((partition) => partition.name)).toEqual(['rounds_p007'])

    const none = listPage(partitions, { filter: 'nothing', newestFirst: true, page: 1 })
    expect(none.matched).toBe(0)
    expect(none.pages).toBe(1)
  })
})
