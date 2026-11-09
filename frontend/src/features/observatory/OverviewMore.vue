<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from 'vue'
import type { RouteLocationRaw } from 'vue-router'

import type { OverviewPanels } from './access'
import {
  bucketFormatter,
  databaseBars,
  databaseOfTable,
  drill,
  idleBars,
  kindLegend,
  operatorBars,
  reasonBars,
  sizeSeries,
  statementBars,
  tableBars,
  type SizedDatabase,
  type Statement,
  type Summary,
  type Window,
} from './overview'
import { formatBytes, formatCount, formatGrowth, formatMs } from './text'
import type { UsageRow } from './usage'
import { useInView } from './useInView'
import { VBanner, VBarChart, VCard, VTimeChart } from '@/components'

// The routine chart sits below this half's fold, and loads only once it is near the view.
const RoutineChart = defineAsyncComponent(() => import('./RoutineChart.vue'))

type Loaded<T> = { data: T | null; busy: boolean; failure: string | null }

const props = defineProps<{
  panels: OverviewPanels
  window: Window
  db: string
  sized: Loaded<readonly SizedDatabase[]>
  summary: Loaded<Summary>
  usage: Loaded<readonly UsageRow[]>
  statements: Loaded<readonly Statement[]>
}>()

const emit = defineEmits<{ open: [target: RouteLocationRaw] }>()

const pending = <T,>(loaded: Loaded<T>) => loaded.data === null && loaded.failure === null
const range = computed(() => ({ from: props.window.from, to: props.window.to }))

const sizes = computed(() => sizeSeries(props.sized.data ?? [], props.window))
const operators = computed(() => operatorBars(props.usage.data ?? []))
const reasons = computed(() => reasonBars(props.usage.data ?? []))
const ownDatabases = computed(() => databaseBars(props.usage.data ?? []))
const tables = computed(() => tableBars(props.summary.data?.largestTables ?? [], formatGrowth))
const idle = computed(() => idleBars(props.summary.data?.idle ?? []))
const slowest = computed(() => statementBars(props.statements.data ?? []))
const idleDays = computed(() => props.summary.data?.idleDays ?? 30)
const formatDay = bucketFormatter(86_400)
const routineSlot = ref<HTMLElement | null>(null)
const routinesSeen = useInView(routineSlot)

function open(target: RouteLocationRaw): void {
  emit('open', target)
}
</script>

<template>
  <div class="grid grid-cols-1 gap-3 lg:grid-cols-2" data-testid="overview-more">
    <VCard v-if="props.panels.server" pad data-chart="size">
      <VBanner v-if="props.sized.failure" icon="circle-x">{{ props.sized.failure }}</VBanner>
      <VTimeChart
        v-else
        :series="sizes"
        mode="line"
        caption="Size of each governed database, one line per database, from the daily snapshots"
        :format-y="formatBytes"
        :format-x="formatDay"
        :loading="pending(props.sized)"
        empty-text="No size snapshot falls in this range."
        @select="open(drill('capacity', { database: props.db }))"
      />
    </VCard>

    <VCard v-if="props.panels.operators" pad data-chart="operators">
      <VBanner v-if="props.usage.failure" icon="circle-x">{{ props.usage.failure }}</VBanner>
      <VBarChart
        v-else
        :data="operators"
        caption="The ten operators who submitted the most statements, allowed and refused"
        :format-value="formatCount"
        :loading="pending(props.usage)"
        empty-text="Nobody submitted a statement in this range."
        @select="open(drill('usage', { operator: $event, database: props.db, ...range }))"
      />
    </VCard>

    <VCard v-if="props.panels.usage && !props.panels.operators" pad data-chart="databases">
      <VBanner v-if="props.usage.failure" icon="circle-x">{{ props.usage.failure }}</VBanner>
      <VBarChart
        v-else
        :data="ownDatabases"
        caption="The databases your statements went to, most statements first"
        :format-value="formatCount"
        :loading="pending(props.usage)"
        empty-text="You submitted no statement in this range."
        @select="open(drill('usage', { database: $event, ...range }))"
      />
    </VCard>

    <VCard v-if="props.panels.usage" pad data-chart="reasons">
      <VBanner v-if="props.usage.failure" icon="circle-x">{{ props.usage.failure }}</VBanner>
      <VBarChart
        v-else
        :data="reasons"
        caption="Why statements were refused, by reason, most frequent first"
        :format-value="formatCount"
        :loading="pending(props.usage)"
        empty-text="No statement was refused in this range."
        @select="open(drill('usage', { reason: $event, database: props.db, ...range }))"
      />
    </VCard>

    <VCard v-if="props.panels.server" pad data-chart="tables">
      <VBanner v-if="props.summary.failure" icon="circle-x">{{ props.summary.failure }}</VBanner>
      <VBarChart
        v-else
        :data="tables"
        caption="The ten largest tables across the governed databases, by total size"
        :format-value="formatBytes"
        :loading="pending(props.summary)"
        empty-text="No table has been measured yet."
        @select="open(drill('capacity', { database: databaseOfTable($event) }))"
      />
    </VCard>

    <VCard v-if="props.panels.server" pad data-chart="idle">
      <VBanner v-if="props.summary.failure" icon="circle-x">{{ props.summary.failure }}</VBanner>
      <VBarChart
        v-else
        :data="idle"
        :caption="`Objects idle more than ${idleDays} days in each database, by kind`"
        :format-value="formatCount"
        :legend="kindLegend"
        :loading="pending(props.summary)"
        empty-text="Nothing is flagged idle."
        @select="open(drill('last-used', { database: $event, flagged: '1' }))"
      />
    </VCard>

    <VCard v-if="props.panels.server" pad data-chart="slowest">
      <VBanner v-if="props.statements.failure" icon="circle-x">
        {{ props.statements.failure }}
      </VBanner>
      <VBarChart
        v-else
        :data="slowest"
        caption="The slowest statements by mean time, from pg_stat_statements"
        :format-value="formatMs"
        :loading="pending(props.statements)"
        empty-text="No statement has been recorded."
        @select="open(drill('performance', { sort: 'mean', database: props.db }))"
      />
    </VCard>

    <div v-if="props.panels.server" ref="routineSlot">
      <RoutineChart v-if="routinesSeen" :db="props.db" @open="open" />
    </div>
  </div>
</template>
