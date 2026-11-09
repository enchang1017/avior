import { ref } from 'vue'

import { api, classify, describeFailure, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type TablePartitioning = components['schemas']['TablePartitioning']
export type PartitionEntry = components['schemas']['PartitionEntry']

/** An exact count as it stands: asked, answered, or refused with the reason. */
export type ExactCount =
  | { state: 'counting' }
  | { state: 'counted'; rows: string }
  | { state: 'refused'; message: string }

/**
 * A name as PostgreSQL reads it exactly, case and all. The catalogue gave it, so
 * it is quoted rather than judged: a partition named in mixed case is still one
 * an operator may count.
 */
export function quoted(name: string): string {
  return `"${name.replaceAll('"', '""')}"`
}

/** A count arrives as a number, or as a string carrying every digit of a bigint. */
function cellText(cell: unknown): string {
  return typeof cell === 'number' || typeof cell === 'string' ? String(cell) : '—'
}

/*
 * count(ctid), not count(*). A Seq Scan feeding count(*) emits every column of
 * the table, which is PostgreSQL's physical target list, and the guard reads
 * every column a plan node outputs: a partition whose table restricts a column
 * would be refused for a column the count never reads. ctid is never null, so
 * the number is the same, and the scan emits it alone.
 */
export function countStatement(partition: Pick<PartitionEntry, 'schema' | 'name'>): string {
  return `SELECT count(ctid) AS exact_rows FROM ${quoted(partition.schema)}.${quoted(partition.name)}`
}

/**
 * useTablePartitions reads FEAT-075's partitioning of one table, and counts a
 * partition exactly on request.
 *
 * The catalogue's numbers are estimates and say so. An exact count reads every
 * row, so it is never run on its own: it is an ordinary statement sent through
 * the guard, refused by the same cost cap and audited like any other the
 * operator runs, and only when they press for it.
 */
export function useTablePartitions(instanceId: string) {
  const partitioning = ref<TablePartitioning | null>(null)
  const counts = ref<Record<string, ExactCount>>({})
  const failure = ref<Failure | null>(null)
  const busy = ref(false)

  async function load(database: string, schema: string, table: string): Promise<void> {
    partitioning.value = null
    counts.value = {}
    failure.value = null
    busy.value = true
    try {
      const { data, error, response } = await api.GET('/schema/partitions', {
        params: { query: { instanceId, database, schema, table } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      partitioning.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  function settle(key: string, count: ExactCount): void {
    counts.value = { ...counts.value, [key]: count }
  }

  async function countExactly(database: string, partition: PartitionEntry): Promise<void> {
    const key = `${partition.schema}.${partition.name}`
    settle(key, { state: 'counting' })
    try {
      const { data, error, response } = await api.POST('/query/execute', {
        body: { instanceId, database, statement: countStatement(partition) },
      })
      if (data === undefined) {
        settle(key, {
          state: 'refused',
          message: describeFailure(classify(response.status, error)),
        })
        return
      }
      settle(key, { state: 'counted', rows: cellText(data.rows[0]?.[0]) })
    } catch (thrown) {
      settle(key, { state: 'refused', message: describeFailure(unreachable(thrown)) })
    }
  }

  function clear(): void {
    partitioning.value = null
    counts.value = {}
    failure.value = null
  }

  return { partitioning, counts, failure, busy, load, countExactly, clear }
}
