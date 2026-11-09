import { computed, shallowRef, watch } from 'vue'

import type { Measurement } from '@/components/Data/VCostGauge/gauge'

/**
 * The last measurement Inspect or Run made, and whether the statement has
 * changed since. It never measures by itself: every inspect is an audited
 * probe that spends the caller's statement budget.
 */
export function useCostGauge(
  verdict: () => Measurement | null,
  result: () => Measurement | null,
  statement: () => string,
) {
  const last = shallowRef<Measurement | null>(null)
  const measured = shallowRef('')

  watch(
    () => verdict() ?? result(),
    (next) => {
      if (next === null) return
      last.value = next
      measured.value = statement()
    },
    { immediate: true },
  )

  const stale = computed(() => last.value !== null && statement() !== measured.value)

  return { measurement: last, stale }
}
