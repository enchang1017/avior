import { api } from '@/shared/api'
import type { LineageOverview } from './types'
import { useLineageAnswer } from './useLineageAnswer'

export type OverviewQuestion = { instanceId: string; database: string; extensions: boolean }

/** The database at the scale of its schemas, for the galaxy overview. */
export function useLineageOverview(question: () => OverviewQuestion | null) {
  return useLineageAnswer<OverviewQuestion, LineageOverview>(question, (asked, signal) =>
    api.GET('/lineage/overview', { params: { query: asked }, signal }),
  )
}
