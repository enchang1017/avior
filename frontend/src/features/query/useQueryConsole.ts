import { ref } from 'vue'

import {
  api,
  classify,
  isGuardVerdict,
  type Failure,
  type GuardVerdict,
  type QueryRequest,
  type QueryResult,
  unreachable,
} from '@/shared/api'

type Target = Pick<QueryRequest, 'instanceId' | 'database' | 'statement'>

/** Which request is in flight, so the pane can say which answer it is waiting for. */
export type Running = 'execute' | 'inspect' | null

/**
 * useQueryConsole holds the console's request state. It is a composable rather
 * than a store because two consoles open side by side must not share a result.
 */
export function useQueryConsole() {
  const verdict = ref<GuardVerdict | null>(null)
  const result = ref<QueryResult | null>(null)
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const running = ref<Running>(null)

  function reset(): void {
    verdict.value = null
    result.value = null
    failure.value = null
  }

  async function inspect(target: Target): Promise<void> {
    busy.value = true
    running.value = 'inspect'
    reset()
    try {
      const { data, error, response } = await api.POST('/query/inspect', { body: target })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      verdict.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
      running.value = null
    }
  }

  async function execute(target: Target): Promise<void> {
    busy.value = true
    running.value = 'execute'
    reset()
    try {
      const { data, error, response } = await api.POST('/query/execute', { body: target })
      if (data === undefined) {
        const classified = classify(response.status, error)
        failure.value = classified
        // A refusal carries the verdict, and the operator needs to see the
        // rules alongside the message.
        if (classified.kind === 'rejected') verdict.value = classified.verdict
        else if (isGuardVerdict(error)) verdict.value = error
        return
      }
      result.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
      running.value = null
    }
  }

  return { verdict, result, failure, busy, running, inspect, execute }
}
