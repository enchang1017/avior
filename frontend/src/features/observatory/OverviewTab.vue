<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter, type RouteLocationRaw } from 'vue-router'

import LivePanel from './LivePanel.vue'
import type { OverviewPanels } from './access'
import {
  bucketFormatter,
  bucketWindow,
  chosenDatabases,
  delta,
  drill,
  growthDelta,
  idleTotal,
  latencySeries,
  parseState,
  rangeLabel,
  ranges,
  rangeWindow,
  refreshes,
  refusalRate,
  rowsTotal,
  sizeFigures,
  spark,
  stateQuery,
  volumeSeries,
  type OverviewState,
  bucketWord,
  kpiColumns,
  kpiCount,
} from './overview'
import { formatBytes, formatCount, formatMs, formatPercent } from './text'
import { useInView } from './useInView'
import { useLoad } from './useLoad'
import {
  type SelectOption,
  VBanner,
  VCard,
  VField,
  VKpiCard,
  VSelect,
  VTimeChart,
} from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'

type Schemas = components['schemas']

const OverviewMore = defineAsyncComponent(() => import('./OverviewMore.vue'))

const props = defineProps<{ panels: OverviewPanels }>()

const route = useRoute()
const router = useRouter()

const state = computed(() => parseState(route.query))
function change(next: Partial<OverviewState>): void {
  void router.replace({ query: { ...route.query, ...stateQuery({ ...state.value, ...next }) } })
}

const rangeOptions: SelectOption[] = ranges.map((id) => ({
  value: id,
  label: `Last ${rangeLabel[id]}`,
}))
const refreshOptions: SelectOption[] = refreshes.map((id) => ({
  value: id,
  label: id === 'off' ? 'Off' : `Every ${id} s`,
}))

const series = useLoad<Schemas['ObservatoryUsageSeries']>()
const usage = useLoad<Schemas['ObservatoryUsage']>()
const databases = useLoad<Schemas['ObservatoryDatabases']>()
const summary = useLoad<Schemas['ObservatorySummary']>()
const activity = useLoad<Schemas['ObservatoryActivity']>()
const statements = useLoad<Schemas['ObservatoryStatements']>()

const span = ref(rangeWindow(state.value.range, new Date()))
const sentinel = ref<HTMLElement | null>(null)
const moreSeen = useInView(sentinel)

const dbQuery = () => (state.value.db === '' ? {} : { database: state.value.db })

function loadActivity(): void {
  const query = { minSeconds: 60, ...dbQuery() }
  void activity.run(() => api.GET('/observatory/activity', { params: { query } }))
}

function loadUsage(): void {
  const query = { from: span.value.from, to: span.value.to, ...dbQuery() }
  void usage.run(() => api.GET('/observatory/usage', { params: { query } }))
}

function loadMore(): void {
  if (props.panels.server) {
    const query = { sort: 'mean' as const, limit: 10, ...dbQuery() }
    void statements.run(() => api.GET('/observatory/statements', { params: { query } }))
  }
}

function load(): void {
  span.value = rangeWindow(state.value.range, new Date())
  if (props.panels.usage) {
    const query = { from: span.value.from, to: span.value.to, ...dbQuery() }
    void series.run(() => api.GET('/observatory/usage/series', { params: { query } }))
    loadUsage()
  }
  if (props.panels.server) {
    if (databases.data.value === null) {
      void databases.run(() => api.GET('/observatory/databases'))
    }
    void summary.run(() => api.GET('/observatory/summary', { params: { query: dbQuery() } }))
    loadActivity()
  }
  if (moreSeen.value) loadMore()
}

watch([() => state.value.range, () => state.value.db], load, { immediate: true })
watch(moreSeen, (seen) => {
  if (seen) loadMore()
})

let timer: ReturnType<typeof setInterval> | null = null
function stopTimer(): void {
  if (timer !== null) clearInterval(timer)
  timer = null
}
watch(
  () => state.value.refresh,
  (refresh) => {
    stopTimer()
    if (refresh !== 'off' && props.panels.server) {
      timer = setInterval(loadActivity, Number(refresh) * 1000)
    }
  },
  { immediate: true },
)
onBeforeUnmount(stopTimer)

const databaseOptions = computed<SelectOption[]>(() => {
  const names = props.panels.server
    ? (databases.data.value?.databases ?? []).map((row) => row.database)
    : (usage.data.value?.rows ?? []).map((row) => row.database)
  const offered = [...new Set([...names, ...(state.value.db === '' ? [] : [state.value.db])])]
  return [
    { value: '', label: 'All databases' },
    ...offered.sort().map((n) => ({ value: n, label: n })),
  ]
})

const buckets = computed(() => series.data.value?.buckets ?? [])
const totals = computed(() => series.data.value?.totals ?? null)
const previous = computed(() => series.data.value?.previous ?? null)
const bucketSeconds = computed(() => series.data.value?.bucketSeconds ?? span.value.bucketSeconds)
const formatX = computed(() => bucketFormatter(bucketSeconds.value))
const seriesPending = computed(() => series.data.value === null && series.failure.value === null)

const sized = computed(() => chosenDatabases(databases.data.value?.databases ?? [], state.value.db))
const size = computed(() => sizeFigures(sized.value, span.value))
const rate = computed(() => (totals.value ? refusalRate(totals.value) : null))
const previousRate = computed(() => (previous.value ? refusalRate(previous.value) : null))
const points = (ratio: number) => `${(ratio * 100).toFixed(1)} pts`

const range = computed(() => ({ from: span.value.from, to: span.value.to }))
const usageLink = computed(() => drill('usage', { database: state.value.db, ...range.value }))

function openBucket(x: number): void {
  const bucket = bucketWindow(x, bucketSeconds.value)
  void router.push(drill('usage', { database: state.value.db, ...bucket }))
}

function open(target: RouteLocationRaw): void {
  void router.push(target)
}

const loaded = <T,>(load: {
  data: { value: T | null }
  busy: { value: boolean }
  failure: { value: string | null }
}) => ({ data: load.data.value, busy: load.busy.value, failure: load.failure.value })
</script>

<template>
  <section
    class="flex min-h-0 flex-1 flex-col overflow-auto"
    aria-label="Overview"
    tabindex="0"
    data-testid="overview-scroll"
  >
    <div class="flex flex-none flex-wrap items-end gap-3 px-3 py-2" data-testid="overview-toolbar">
      <VField label="Range" for="overview-range">
        <VSelect
          id="overview-range"
          :model-value="state.range"
          :options="rangeOptions"
          size="sm"
          @update:model-value="change({ range: $event as OverviewState['range'] })"
        />
      </VField>
      <VField label="Database" for="overview-db">
        <VSelect
          id="overview-db"
          :model-value="state.db"
          :options="databaseOptions"
          size="sm"
          @update:model-value="change({ db: $event })"
        />
      </VField>
      <VField v-if="props.panels.server" label="Auto-refresh" for="overview-refresh">
        <VSelect
          id="overview-refresh"
          :model-value="state.refresh"
          :options="refreshOptions"
          size="sm"
          @update:model-value="change({ refresh: $event as OverviewState['refresh'] })"
        />
      </VField>
      <p v-if="props.panels.usage && !props.panels.operators" class="m-0 text-sm text-fg-soft">
        Usage figures are your own statements.
      </p>
    </div>

    <div class="flex flex-col gap-3 px-3 pb-4">
      <VBanner v-if="series.failure.value" icon="circle-x">{{ series.failure.value }}</VBanner>

      <div
        :class="['grid grid-cols-1 gap-3 sm:grid-cols-2', kpiColumns(kpiCount(props.panels))]"
        data-testid="overview-kpis"
      >
        <VKpiCard
          v-if="props.panels.server"
          label="Governed databases"
          :value="size.now === null ? '—' : formatBytes(size.now)"
          :delta="growthDelta(size.growth, state.range, formatBytes)"
          :spark="size.spark"
          :to="drill('capacity', { database: state.db })"
          :loading="databases.data.value === null && databases.failure.value === null"
          hint="Total size now, and its growth over the range"
        />
        <VKpiCard
          v-if="props.panels.usage"
          label="Statements through Avior"
          :value="totals ? formatCount(totals.statements) : '—'"
          :delta="
            delta(totals?.statements ?? null, previous?.statements ?? null, 'neutral', formatCount)
          "
          :spark="spark(buckets.map((b) => b.allowed + b.refused))"
          :to="usageLink"
          :loading="seriesPending"
          :hint="
            totals
              ? `${formatCount(totals.allowed)} allowed · ${formatCount(totals.refused)} refused`
              : 'Allowed and refused'
          "
        />
        <VKpiCard
          v-if="props.panels.usage"
          label="p95 elapsed"
          :value="totals ? formatMs(totals.p95Ms) : '—'"
          :delta="
            delta(totals?.p95Ms ?? null, previous?.p95Ms ?? null, 'lower-is-better', formatMs)
          "
          :spark="spark(buckets.map((b) => b.p95Ms))"
          :to="usageLink"
          :loading="seriesPending"
          hint="Over executed statements"
        />
        <VKpiCard
          v-if="props.panels.usage"
          label="Refusal rate"
          :value="rate === null ? '—' : formatPercent(rate)"
          :delta="delta(rate, previousRate, 'lower-is-better', points)"
          :spark="
            spark(
              buckets.map((b) =>
                b.allowed + b.refused === 0 ? null : b.refused / (b.allowed + b.refused),
              ),
            )
          "
          :to="usageLink"
          :loading="seriesPending"
        />
        <VKpiCard
          v-if="props.panels.usage && !props.panels.operators"
          label="Rows returned"
          :value="usage.data.value ? formatCount(rowsTotal(usage.data.value.rows)) : '—'"
          :to="usageLink"
          :loading="usage.data.value === null && usage.failure.value === null"
          hint="Over your executed statements"
        />
        <VKpiCard
          v-if="props.panels.server"
          :label="`Objects idle > ${summary.data.value?.idleDays ?? 30} days`"
          :value="summary.data.value ? formatCount(idleTotal(summary.data.value.idle)) : '—'"
          :to="drill('last-used', { database: state.db, flagged: '1' })"
          :loading="summary.data.value === null && summary.failure.value === null"
          hint="Now; no history is kept"
        />
        <VKpiCard
          v-if="props.panels.server"
          label="Running longer than 60 s"
          :value="activity.data.value ? formatCount(activity.data.value.backends.length) : '—'"
          :to="drill('performance', { database: state.db })"
          :loading="activity.data.value === null && activity.failure.value === null"
          hint="Now"
        />
      </div>

      <div v-if="props.panels.usage" class="grid grid-cols-1 gap-3 lg:grid-cols-2">
        <VCard pad data-chart="volume">
          <VTimeChart
            :series="volumeSeries(buckets)"
            mode="stacked-area"
            :caption="`Statements through Avior per ${bucketWord(bucketSeconds)}, allowed and refused`"
            :format-y="formatCount"
            :format-x="formatX"
            :loading="seriesPending"
            empty-text="No statement was submitted in this range."
            @select="openBucket"
          />
        </VCard>
        <VCard pad data-chart="latency">
          <VTimeChart
            :series="latencySeries(buckets)"
            mode="line"
            scale="log"
            :caption="`p50 and p95 elapsed per ${bucketWord(bucketSeconds)}, over executed statements`"
            :format-y="formatMs"
            :format-x="formatX"
            :loading="seriesPending"
            empty-text="No statement ran in this range."
            @select="openBucket"
          />
        </VCard>
      </div>

      <div ref="sentinel">
        <OverviewMore
          v-if="moreSeen"
          :panels="props.panels"
          :window="span"
          :db="state.db"
          :sized="{ ...loaded(databases), data: databases.data.value ? sized : null }"
          :summary="loaded(summary)"
          :usage="{ ...loaded(usage), data: usage.data.value?.rows ?? null }"
          :statements="{ ...loaded(statements), data: statements.data.value?.statements ?? null }"
          @open="open"
        />
      </div>

      <LivePanel
        v-if="props.panels.server"
        :activity="activity.data.value"
        :failure="activity.failure.value"
        :busy="activity.busy.value"
        :refresh-seconds="state.refresh === 'off' ? null : Number(state.refresh)"
        @refresh="loadActivity"
      />
    </div>
  </section>
</template>
