import { computed, ref, shallowRef, watch } from 'vue'

import { api, classify, describeFailure, unreachable } from '@/shared/api'
import { refQuery } from './focus'
import { withExpansion } from './scene'
import type {
  FocusRef,
  LineageConfidence,
  LineageGraph,
  LineageNodeKind,
  LineagePath,
} from './types'
import { useLineageAnswer } from './useLineageAnswer'

export type GraphQuestion = {
  instanceId: string
  database: string
  focus: FocusRef
  hops: number
  kinds: LineageNodeKind[] | null
  confidence: LineageConfidence[] | null
  extensions: boolean
}

function graphQuery(question: GraphQuestion, expand?: string) {
  return {
    instanceId: question.instanceId,
    database: question.database,
    ...refQuery(question.focus),
    ...(question.hops === 1 ? {} : { hops: question.hops }),
    ...(question.hops === 3 ? { cluster: true } : {}),
    ...(question.kinds === null ? {} : { kinds: question.kinds }),
    ...(question.confidence === null ? {} : { confidence: question.confidence }),
    ...(question.extensions ? { extensions: true } : {}),
    ...(expand === undefined ? {} : { expand }),
  }
}

/**
 * The neighbourhood of the focus, and the hubs expanded in it. An answer is
 * kept while the database stays the same, so the stars glide from where they
 * were rather than blinking out between two answers.
 */
export function useLineageGraph(question: () => GraphQuestion | null) {
  const asked = useLineageAnswer(
    question,
    (query, signal) => api.GET('/lineage/graph', { params: { query: graphQuery(query) }, signal }),
    (before, after) => before.database === after.database,
  )
  const expanded = shallowRef<LineageGraph | null>(null)
  const expanding = ref<string | null>(null)
  const expandFailure = ref('')
  watch(asked.answer, () => {
    expanded.value = null
  })

  async function expand(hub: string): Promise<void> {
    const current = graph.value
    const asking = question()
    if (current === null || asking === null) return
    expanding.value = hub
    expandFailure.value = ''
    try {
      const { data, error, response } = await api.GET('/lineage/graph', {
        params: { query: graphQuery(asking, hub) },
      })
      if (data === undefined)
        expandFailure.value = describeFailure(classify(response.status, error))
      else if (data.state === 'ready' && graph.value === current) {
        expanded.value = withExpansion(current, data, hub)
      }
    } catch (thrown) {
      expandFailure.value = describeFailure(unreachable(thrown))
    } finally {
      expanding.value = null
    }
  }

  const graph = computed(() => expanded.value ?? asked.answer.value)
  const failure = computed(() => asked.failure.value || expandFailure.value)
  return { ...asked, graph, failure, expand, expanding }
}

export type PathQuestion = {
  instanceId: string
  database: string
  from: FocusRef
  to: FocusRef
  maxHops: number
}

export function useLineagePath(question: () => PathQuestion | null) {
  return useLineageAnswer<PathQuestion, LineagePath>(question, (asked, signal) => {
    const query = {
      instanceId: asked.instanceId,
      database: asked.database,
      ...refQuery(asked.from, 'from'),
      ...refQuery(asked.to, 'to'),
      maxHops: asked.maxHops,
    }
    return api.GET('/lineage/path', { params: { query }, signal })
  })
}
