<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute, type RouteLocationRaw } from 'vue-router'

import { parseState } from './overview'
import { routineBars, routineDrill, routineQuery } from './routines'
import { formatMs } from './text'
import { useLoad } from './useLoad'
import { VBanner, VBarChart, VButton, VCard } from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'

const props = defineProps<{ db: string }>()
const emit = defineEmits<{ open: [target: RouteLocationRaw] }>()

const route = useRoute()
const range = computed(() => parseState(route.query).range)
const routines = useLoad<components['schemas']['ObservatoryRoutines']>()
const bars = computed(() => routineBars(routines.data.value?.routines ?? []))
const link = computed(() => routineDrill(range.value, props.db))

watch(
  [range, () => props.db],
  ([asked, database]) => {
    const query = routineQuery(
      { range: asked, sort: 'total', order: 'desc', database, search: '', hideUncalled: true },
      10,
    )
    void routines.run(() => api.GET('/observatory/routines', { params: { query } }))
  },
  { immediate: true },
)
</script>

<template>
  <VCard pad data-chart="routines">
    <VBanner v-if="routines.failure.value" icon="circle-x">{{ routines.failure.value }}</VBanner>
    <VBarChart
      v-else
      :data="bars"
      caption="The ten routines with the most total time over the range"
      :format-value="formatMs"
      :loading="routines.data.value === null && routines.failure.value === null"
      empty-text="No routine was called in this range."
      @select="emit('open', routineDrill(range, props.db, $event))"
    />
    <div class="mt-2 flex justify-end">
      <VButton :to="link" size="sm" variant="bare" icon="chevron-right">
        Open routine timing
      </VButton>
    </div>
  </VCard>
</template>
