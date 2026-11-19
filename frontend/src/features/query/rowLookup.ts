import { api, classify, describeFailure, unreachable, type QueryResult } from '@/shared/api'
import { cellValue } from './cellValue'
export type RowTarget = {
  database: string
  schema: string
  table: string
  original: Record<string, string | null>
}
export function rowSnapshot(result: QueryResult, index: number): Record<string, string | null> {
  return Object.fromEntries(
    result.columns.map((column, i) => [
      column.name,
      result.rows[index]?.[i] === null || result.rows[index]?.[i] === undefined
        ? null
        : cellValue(result.rows[index]?.[i], column).raw,
    ]),
  )
}
export async function lookupRow(
  target: RowTarget,
  instanceId: string,
  signal?: AbortSignal,
): Promise<QueryResult> {
  const built = await api.POST('/writes/row-current-template', {
    ...(signal ? { signal } : {}),
    body: {
      database: target.database,
      schema: target.schema,
      table: target.table,
      original: target.original,
    },
  })
  if (!built.data) throw new Error(describeFailure(classify(built.response.status, built.error)))
  signal?.throwIfAborted()
  const read = await api.POST('/query/execute', {
    ...(signal ? { signal } : {}),
    body: { instanceId, database: target.database, statement: built.data.statement, maxRows: 2 },
  })
  if (!read.data) throw new Error(describeFailure(classify(read.response.status, read.error)))
  if (read.data.rows.length > 1)
    throw new Error('The primary key did not identify one row. Reload the table metadata.')
  return read.data
}
export function rowError(error: unknown): string {
  return error instanceof Error ? error.message : describeFailure(unreachable(error))
}
