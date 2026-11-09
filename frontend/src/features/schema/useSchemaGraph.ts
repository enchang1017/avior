import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'

import type { NodeRef, SchemaGraph } from '@/components/Data/VSchemaChart/graph'
import { api, classify, describeFailure, unreachable } from '@/shared/api'

/**
 * The graph of the database the console is pointed at, read only while the
 * chart is open. The request leaves after the tree's, and nothing waits on it.
 */
export function useSchemaGraph(
  instanceId: () => string,
  database: () => string | null,
  open: () => boolean,
) {
  const graph = shallowRef<SchemaGraph | null>(null)
  const failure = ref('')
  let inFlight: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | null = null

  function stop(): void {
    if (timer !== null) clearTimeout(timer)
    timer = null
    inFlight?.abort()
    inFlight = null
  }

  async function load(name: string): Promise<void> {
    const controller = new AbortController()
    inFlight = controller
    try {
      const { data, error, response } = await api.GET('/schema/graph', {
        params: { query: { instanceId: instanceId(), database: name } },
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      if (data === undefined) failure.value = describeFailure(classify(response.status, error))
      else graph.value = data
    } catch (thrown) {
      if (!controller.signal.aborted) failure.value = describeFailure(unreachable(thrown))
    }
  }

  watch(
    () => [database(), open()] as const,
    ([name, isOpen]) => {
      stop()
      graph.value = null
      failure.value = ''
      if (name === null || !isOpen) return
      timer = setTimeout(() => {
        timer = null
        void load(name)
      }, 0)
    },
    { immediate: true },
  )

  onBeforeUnmount(stop)

  return { graph, failure }
}

/**
 * Whether the answer shows the target touching nothing, so the chart has
 * nothing to draw for it. No target, or no answer yet, is not that.
 */
export function isIsolated(graph: SchemaGraph | null, target: NodeRef | null): boolean {
  if (graph === null || target === null) return false
  const touches = (ref: NodeRef) => ref.schema === target.schema && ref.name === target.name
  return !graph.edges.some((edge) => touches(edge.from) !== touches(edge.to))
}
