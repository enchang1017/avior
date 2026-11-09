<script setup lang="ts">
import { computed, defineAsyncComponent, h } from 'vue'
import { RouterLink } from 'vue-router'

import type { NodeRef } from '@/components/Data/VSchemaChart/graph'
import { lineageLocation } from '@/features/lineage/address'
import type { TableRef } from '@/shared/api'
import ChartPlaceholder from './ChartPlaceholder.vue'
import { useChartCollapsed } from './chartCollapsed'

const props = defineProps<{
  instanceId: string
  database: string | null
  target: (NodeRef & { kind?: TableRef['kind'] }) | null
}>()

const emit = defineEmits<{ choose: [ref: TableRef] }>()

/*
 * The panel draws at once and keeps the chart's own shape while its chunk and
 * its graph arrive, so the tree under it never moves when either lands.
 */
const SchemaChartBody = defineAsyncComponent({
  loader: () => import('./SchemaChartBody.vue'),
  loadingComponent: () => h(ChartPlaceholder, { database: props.database ?? '' }),
  delay: 0,
})

const collapsed = useChartCollapsed()
const lineage = computed(() =>
  props.database === null ? null : lineageLocation(props.database, props.target),
)
</script>

<template>
  <section class="chart-panel flex flex-col border-b border-line" data-testid="chart-panel">
    <header class="flex items-center px-3 py-1.5">
      <span class="font-mono text-3xs tracking-hud text-fg-muted uppercase">Chart</span>
      <RouterLink
        v-if="lineage"
        :to="lineage"
        class="chart-lineage ml-2 font-mono text-3xs tracking-hud text-primary uppercase no-underline hover:underline"
      >
        Open in Lineage
      </RouterLink>
      <button
        type="button"
        class="chart-toggle ml-auto cursor-pointer rounded-sm border-0 bg-transparent px-1.5 py-0.5 font-mono text-3xs tracking-hud text-fg-muted uppercase hover:text-fg"
        :aria-expanded="!collapsed"
        @click="collapsed = !collapsed"
      >
        {{ collapsed ? 'Show' : 'Hide' }}
      </button>
    </header>
    <template v-if="!collapsed">
      <p v-if="database === null" class="m-0 px-3 pb-3 text-xs text-fg-muted">
        Pick a database to chart it.
      </p>
      <SchemaChartBody
        v-else
        :instance-id="instanceId"
        :database="database"
        :target="target"
        @choose="emit('choose', $event)"
      >
        <template #waiting="{ failure }">
          <ChartPlaceholder :database="database" :failure="failure" />
        </template>
      </SchemaChartBody>
    </template>
  </section>
</template>
