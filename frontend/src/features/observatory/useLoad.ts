import { ref, type Ref } from 'vue'

import { classify, describeFailure, unreachable } from '@/shared/api'

type Answer<T> = { data?: T; error?: unknown; response: { status: number } }

/**
 * useLoad holds one request's answer, its failure as a line an operator can
 * read, and whether it is in flight. Only the latest call may write: a slow
 * answer to a filter the operator has since changed would otherwise land on
 * top of the one they asked for.
 */
export function useLoad<T>() {
  const data = ref<T | null>(null) as Ref<T | null>
  const failure = ref<string | null>(null)
  const status = ref(0)
  const busy = ref(false)
  let latest = 0

  async function run(request: () => Promise<Answer<T>>): Promise<void> {
    const call = ++latest
    busy.value = true
    failure.value = null
    try {
      const answer = await request()
      if (call !== latest) return
      status.value = answer.response.status
      if (answer.data === undefined) {
        data.value = null
        failure.value = describeFailure(classify(answer.response.status, answer.error))
        return
      }
      data.value = answer.data
    } catch (thrown) {
      if (call !== latest) return
      data.value = null
      status.value = 0
      failure.value = describeFailure(unreachable(thrown))
    } finally {
      if (call === latest) busy.value = false
    }
  }

  return { data, failure, status, busy, run }
}
