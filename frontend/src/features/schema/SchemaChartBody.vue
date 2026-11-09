<script setup lang="ts">
import { computed, shallowRef, type Component } from 'vue'
import { RouterLink } from 'vue-router'

import type { NodeRef } from '@/components/Data/VSchemaChart/graph'
import { lineageLocation } from '@/features/lineage/address'
import type { TableRef } from '@/shared/api'
import { isIsolated, useSchemaGraph } from './useSchemaGraph'

const props = defineProps<{
  instanceId: string
  database: string
  target: (NodeRef & { kind?: TableRef['kind'] }) | null
}>()
const emit = defineEmits<{ choose: [ref: TableRef] }>()
defineSlots<{ waiting(props: { failure: string }): unknown }>()

/*
 * The chart is a chunk of its own, and the waiting slot stays drawn until both
 * the chunk and the graph are here, so nothing between them is ever empty.
 */
const chart = shallowRef<Component | null>(null)
const chartFailure = shallowRef('')
import('@/components/Data/VSchemaChart/VSchemaChart.vue')
  .then((loaded) => {
    chart.value = loaded.default
  })
  .catch(() => {
    chartFailure.value = 'The chart could not be loaded. Reload the page to try again.'
  })

const { graph, failure } = useSchemaGraph(
  () => props.instanceId,
  () => props.database,
  () => true,
)

const isolated = computed(() => isIsolated(graph.value, props.target))
const lineage = computed(() => lineageLocation(props.database, props.target))
</script>

<template>
  <p
    v-if="graph && !failure && isolated"
    class="chart-isolated m-0 px-3 pb-2 font-mono text-3xs tracking-hud text-fg-muted uppercase"
  >
    No relationships ·
    <RouterLink :to="lineage" class="open-lineage text-primary no-underline hover:underline">
      Open in Lineage
    </RouterLink>
  </p>
  <component
    :is="chart"
    v-else-if="chart && graph && !failure"
    :graph="graph"
    :database="database"
    :target="target"
    @choose="emit('choose', $event)"
  />
  <slot v-else name="waiting" :failure="failure || chartFailure" />
</template>
