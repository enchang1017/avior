import { nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'

import type { Measurement } from '@/components/Data/VCostGauge/gauge'
import { useCostGauge } from './useCostGauge'

const limits = { maxCost: 50, maxRows: 1000 }

describe('useCostGauge', () => {
  it('keeps the last measurement and reads stale once the statement changes', async () => {
    const verdict = ref<Measurement | null>(null)
    const result = ref<Measurement | null>(null)
    const statement = ref('SELECT id FROM public.member')
    const gauge = useCostGauge(
      () => verdict.value,
      () => result.value,
      () => statement.value,
    )
    expect(gauge.measurement.value).toBeNull()
    expect(gauge.stale.value).toBe(false)

    verdict.value = { estimatedCost: 12, estimatedRows: 3, limits }
    await nextTick()
    expect(gauge.measurement.value?.estimatedCost).toBe(12)

    statement.value = 'SELECT id FROM public.member WHERE id = 1'
    expect(gauge.stale.value).toBe(true)

    verdict.value = null
    await nextTick()
    expect(gauge.measurement.value?.estimatedCost).toBe(12)

    result.value = { estimatedCost: 4, estimatedRows: 1, limits }
    await nextTick()
    expect(gauge.measurement.value?.estimatedCost).toBe(4)
    expect(gauge.stale.value).toBe(false)
  })
})
