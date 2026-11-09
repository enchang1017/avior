<script setup lang="ts">
import { computed } from 'vue'

import { VBadge, VBanner, VButton, VIcon } from '@/components'
import { lazyPart } from './lazyPart'
import ColumnList from './ColumnList.vue'
import { impactSentence } from './impactSentence'
import NotVisible from './NotVisible.vue'
import { qualified } from './scene'
import type { LineageNode } from './types'
import { useLineageColumn, type ColumnQuestion } from './useLineageColumn'

const ImpactDetail = lazyPart(() => import('./ImpactDetail.vue'), 'Loading the impact')

const props = defineProps<{
  instanceId: string
  database: string
  table: LineageNode
  column: string | null
}>()

const emit = defineEmits<{ pick: [column: string | null]; open: [id: string] }>()

const question = computed<ColumnQuestion>(() => ({
  instanceId: props.instanceId,
  database: props.database,
  schema: props.table.schema,
  table: props.table.name,
  ...(props.column === null ? {} : { column: props.column }),
}))
const usage = useLineageColumn(() => question.value)

const answer = computed(() => usage.answer.value)
const name = computed(() =>
  props.column === null ? qualified(props.table) : `${qualified(props.table)}.${props.column}`,
)
const sentence = computed(() => {
  const summary = answer.value?.summary
  return summary === undefined ? '' : impactSentence(name.value, summary)
})
const counts = computed(() => answer.value?.counts ?? [])
const restricted = computed(() => answer.value?.column?.restricted === true)
</script>

<template>
  <section class="column-panel flex flex-col" data-testid="column-panel" aria-label="Column">
    <header class="flex flex-col gap-1 border-b border-line px-3 py-3">
      <p class="m-0 eyebrow">{{ column === null ? table.kind : 'Column' }}</p>
      <h2 class="m-0 flex items-center gap-2 font-mono text-md font-normal break-all text-target">
        {{ qualified(table) }}<template v-if="column !== null">.{{ column }}</template>
        <VBadge v-if="restricted" tone="danger" size="sm" class="restricted">restricted</VBadge>
      </h2>
      <p v-if="sentence" class="impact m-0 text-sm text-fg" data-testid="impact-summary">
        {{ sentence }}
      </p>
      <p v-else class="m-0 text-xs text-fg-soft">
        Who writes it and who reads it, from the database's own objects.
      </p>
    </header>

    <VBanner v-if="usage.failure.value" icon="circle-x">{{ usage.failure.value }}</VBanner>
    <p v-if="usage.building.value" class="building m-0 px-3 py-3 text-xs text-fg-soft">
      Lineage for {{ database }} is still being built. Asking again…
    </p>
    <p
      v-else-if="answer === null && !usage.failure.value"
      class="m-0 flex items-center gap-2 px-3 py-3 text-xs text-fg-soft"
    >
      <VIcon name="loader" spin size="sm" />
      Reading who touches it…
    </p>

    <template v-if="column === null">
      <ColumnList :counts="counts" :current="null" @pick="emit('pick', $event)" />
    </template>
    <div v-else class="flex px-3 pt-2">
      <VButton variant="bare" size="sm" icon="arrow-left" @click="emit('pick', null)">
        All columns
      </VButton>
    </div>

    <ImpactDetail
      v-if="answer"
      :question="question"
      :sentence="sentence"
      :answer="answer"
      @open="emit('open', $event)"
    />

    <template v-if="answer && column !== null">
      <h3 class="m-0 px-3 pt-3 eyebrow">Columns of {{ table.name }}</h3>
      <ColumnList :counts="counts" :current="column" @pick="emit('pick', $event)" />
    </template>

    <NotVisible />
  </section>
</template>
