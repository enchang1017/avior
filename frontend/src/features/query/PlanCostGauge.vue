<script setup lang="ts">
import VCostGauge from '@/components/Data/VCostGauge/VCostGauge.vue'
import type { Measurement } from '@/components/Data/VCostGauge/gauge'
import { useCostGauge } from './useCostGauge'

const props = defineProps<{
  verdict: Measurement | null
  result: Measurement | null
  statement: string
}>()

const { measurement, stale } = useCostGauge(
  () => props.verdict,
  () => props.result,
  () => props.statement,
)
</script>

<template>
  <VCostGauge class="px-4 pt-3" :measurement="measurement" :stale="stale" />
</template>
