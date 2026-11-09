import { computed, ref, shallowRef, watch } from 'vue'

import { api, classify, describeFailure, unreachable } from '@/shared/api'
import type { LineageColumn } from './types'
import type { ColumnQuestion } from './useLineageColumn'

/** The pages after a column's first, fifty at a time, on asking. */
export function useColumnPages(question: () => ColumnQuestion, first: () => LineageColumn) {
  const more = shallowRef<NonNullable<LineageColumn['entries']>>([])
  const next = ref<string | undefined>(first().next)
  const failure = ref('')
  const loading = ref(false)
  watch(first, (answer) => {
    more.value = []
    next.value = answer.next
  })

  async function load(): Promise<void> {
    const cursor = next.value
    if (cursor === undefined) return
    loading.value = true
    failure.value = ''
    try {
      const { data, error, response } = await api.GET('/lineage/column', {
        params: { query: { ...question(), cursor } },
      })
      if (data === undefined) failure.value = describeFailure(classify(response.status, error))
      else {
        more.value = [...more.value, ...(data.entries ?? [])]
        next.value = data.next
      }
    } catch (thrown) {
      failure.value = describeFailure(unreachable(thrown))
    } finally {
      loading.value = false
    }
  }

  const entries = computed(() => [...(first().entries ?? []), ...more.value])
  return { entries, next, failure, loading, load }
}
