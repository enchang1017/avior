import { api } from '@/shared/api'
import type { LineageColumn } from './types'
import { useLineageAnswer } from './useLineageAnswer'

export type ColumnQuestion = {
  instanceId: string
  database: string
  schema: string
  table: string
  /** Absent, the question is the whole table's. */
  column?: string
}

/** Who writes and who reads one column, or one table: the first fifty, with its impact. */
export function useLineageColumn(question: () => ColumnQuestion | null) {
  return useLineageAnswer<ColumnQuestion, LineageColumn>(question, (query, signal) =>
    api.GET('/lineage/column', { params: { query }, signal }),
  )
}
