import { onBeforeUnmount, ref, shallowRef, watch, type Ref, type ShallowRef } from 'vue'

import { classify, describeFailure, unreachable } from '@/shared/api'

/** How long the page waits before asking again while a database's lineage is still building. */
export const buildingRetryMs = 1500

type Answered<T> = { data?: T; error?: unknown; response: Response }

export type Answer<T> = {
  answer: ShallowRef<T | null>
  building: Ref<boolean>
  failure: Ref<string>
  /** The server's code for a refusal, so the page can answer one it knows. */
  refusal: Ref<string>
  busy: Ref<boolean>
  asked: Ref<number>
}

/**
 * One lineage answer, kept for as long as what it was asked about holds.
 *
 * A `building` answer is never kept: it carries no graph and says nothing
 * about the database, so it is not drawn as if it did. The page asks again
 * until the snapshot is built, and a changed question drops whatever was in
 * flight for the old one.
 */
export function useLineageAnswer<Q, T extends { state: 'ready' | 'building' }>(
  question: () => Q | null,
  send: (query: Q, signal: AbortSignal) => Promise<Answered<T>>,
  keeps: (before: Q, after: Q) => boolean = () => false,
): Answer<T> {
  const answer: ShallowRef<T | null> = shallowRef(null)
  const building = ref(false)
  const failure = ref('')
  const refusal = ref('')
  const busy = ref(false)
  const asked = ref(0)
  let controller: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | null = null

  function stop(): void {
    if (timer !== null) clearTimeout(timer)
    timer = null
    controller?.abort()
    controller = null
  }

  async function ask(query: Q): Promise<void> {
    const current = new AbortController()
    controller = current
    busy.value = true
    asked.value += 1
    try {
      const { data, error, response } = await send(query, current.signal)
      if (current.signal.aborted) return
      if (data === undefined) {
        const refused = classify(response.status, error)
        failure.value = describeFailure(refused)
        refusal.value = refused.kind === 'error' ? refused.code : refused.kind
      } else if (data.state === 'building') {
        building.value = true
        timer = setTimeout(() => {
          timer = null
          void ask(query)
        }, buildingRetryMs)
      } else {
        building.value = false
        answer.value = data
      }
    } catch (thrown) {
      if (!current.signal.aborted) failure.value = describeFailure(unreachable(thrown))
    } finally {
      if (!current.signal.aborted) busy.value = false
    }
  }

  watch(
    question,
    (query, before) => {
      stop()
      if (query === null || before === null || before === undefined || !keeps(before, query)) {
        answer.value = null
      }
      failure.value = ''
      refusal.value = ''
      building.value = false
      busy.value = false
      asked.value = 0
      if (query === null) return
      void ask(query)
    },
    { immediate: true, deep: true },
  )

  onBeforeUnmount(stop)

  return { answer, building, failure, refusal, busy, asked }
}
