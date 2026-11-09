import { api } from '@/shared/api'
import type { LineageRoutine } from './types'
import { useLineageAnswer } from './useLineageAnswer'

export type RoutineQuestion = { instanceId: string; database: string; routine: string }

export function useLineageRoutine(question: () => RoutineQuestion | null) {
  return useLineageAnswer<RoutineQuestion, LineageRoutine>(question, (query, signal) =>
    api.GET('/lineage/routine', { params: { query }, signal }),
  )
}
