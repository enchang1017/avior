<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import {
  meanText,
  parseRoutineState,
  rangeOptions,
  anySlowest,
  hiddenText,
  visibleRoutines,
  definitionLink,
  routineLabel,
  routineName,
  routineQuery,
  routineRows,
  routineStateQuery,
  ariaSort,
  nextSort,
  sortColumns,
  timeText,
  trackedSince,
  versionCountText,
  type RoutineState,
} from './routines'
import { formatCount, formatMs } from './text'
import { useLoad } from './useLoad'
import {
  VCheckbox,
  type SelectOption,
  VBadge,
  VBanner,
  VEmpty,
  VField,
  VInput,
  VSelect,
  VSkeleton,
  VTable,
} from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'
import { fullStamp, stamp } from '@/shared/timestamp'

type Schemas = components['schemas']

const searchDelayMs = 300

const route = useRoute()
const router = useRouter()

const routines = useLoad<Schemas['ObservatoryRoutines']>()
const databases = useLoad<Schemas['ObservatoryDatabases']>()

const state = computed(() => parseRoutineState(route.query))
const search = ref(state.value.search)

function change(next: Partial<RoutineState>): void {
  const kept = Object.fromEntries(
    Object.entries(route.query).filter(
      ([key]) => key !== 'database' && key !== 'search' && key !== 'uncalled',
    ),
  )
  void router.replace({ query: { ...kept, ...routineStateQuery({ ...state.value, ...next }) } })
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
function stopSearchTimer(): void {
  if (searchTimer !== null) clearTimeout(searchTimer)
  searchTimer = null
}
watch(search, (typed) => {
  stopSearchTimer()
  searchTimer = setTimeout(() => change({ search: String(typed).trim() }), searchDelayMs)
})
watch(
  () => state.value.search,
  (asked) => {
    if (String(search.value).trim() !== asked) search.value = asked
  },
)
onBeforeUnmount(stopSearchTimer)

function load(): void {
  const query = routineQuery(state.value)
  void routines.run(() => api.GET('/observatory/routines', { params: { query } }))
}

watch(
  () => JSON.stringify(routineQuery(state.value)),
  () => load(),
  { immediate: true },
)
onMounted(() => {
  void databases.run(() => api.GET('/observatory/databases'))
})

const databaseOptions = computed<SelectOption[]>(() => {
  const names = (databases.data.value?.databases ?? []).map((row) => row.database)
  const chosen = state.value.database
  const offered = [...new Set([...names, ...(chosen === '' ? [] : [chosen])])].sort()
  return [{ value: '', label: 'All databases' }, ...offered.map((n) => ({ value: n, label: n }))]
})

const visible = computed(() =>
  visibleRoutines(routines.data.value?.routines ?? [], state.value.hideUncalled),
)
const rows = computed(() => routineRows(visible.value.shown, routines.data.value?.groups ?? []))
const databaseShown = computed(() => state.value.database === '')
const since = computed(() =>
  trackedSince(routines.data.value?.tracking ?? [], state.value.database),
)
const statsSince = computed(() => routines.data.value?.statsSince ?? null)
const slowestShown = computed(() => anySlowest(visible.value.shown))
const spannedColumns = computed(() => 4 + (slowestShown.value ? 1 : 0))
const shownColumns = computed(() =>
  sortColumns.filter((column) => column.sort !== 'slowest' || slowestShown.value),
)
const failure = computed(() => routines.failure.value)
const waiting = computed(() => routines.data.value === null && failure.value === null)
const skeletonHeadings = computed(() => [
  ...(databaseShown.value ? ['Database'] : []),
  'Routine',
  ...sortColumns.filter((column) => column.sort !== 'slowest').map((column) => column.label),
])
const skeletonColumns = computed(() =>
  skeletonHeadings.value.map((heading) => (heading === 'Routine' ? 'w-full' : 'w-1/2')),
)
</script>

<template>
  <section
    class="flex min-h-0 flex-1 flex-col"
    aria-label="Routine timing"
    data-testid="routine-timing"
  >
    <div class="flex flex-none flex-wrap items-end gap-3 px-3 py-2">
      <VField label="Window" for="routines-range">
        <VSelect
          id="routines-range"
          :model-value="state.range"
          :options="rangeOptions"
          size="sm"
          @update:model-value="change({ range: $event as RoutineState['range'] })"
        />
      </VField>
      <VField label="Database" for="routines-database">
        <VSelect
          id="routines-database"
          :model-value="state.database"
          :options="databaseOptions"
          size="sm"
          @update:model-value="change({ database: $event })"
        />
      </VField>
      <VField label="Name" for="routines-search">
        <VInput
          id="routines-search"
          v-model="search"
          type="search"
          size="sm"
          placeholder="schema.name"
          autocomplete="off"
        />
      </VField>
      <VCheckbox
        :model-value="state.hideUncalled"
        data-testid="hide-uncalled"
        @update:model-value="change({ hideUncalled: $event === true })"
      >
        Hide routines with 0 calls
      </VCheckbox>
    </div>

    <p class="list-lede" data-testid="routines-caption">
      Averages over 15-minute snapshots: one slow call among many fast ones can hide.
      <template v-if="since">
        Tracked since
        <time :datetime="since" :title="fullStamp(since)">{{ stamp(since) }}</time
        >.
      </template>
      <template v-else>Tracking has not begun.</template>
      LANGUAGE sql routines are not timed.
      <span v-if="rows.length > 0 && !slowestShown" class="block" data-testid="routines-no-slowest">
        Slowest single call is shown only for routines an application calls directly with SELECT
        f(…); none on this page.
      </span>
    </p>

    <VBanner v-if="failure" icon="circle-x">{{ failure }}</VBanner>

    <div class="min-h-0 flex-1 overflow-auto">
      <VSkeleton
        v-if="waiting"
        variant="table"
        :headings="skeletonHeadings"
        :columns="skeletonColumns"
        label="Reading routine timing"
      />
      <p v-if="visible.hidden > 0" class="list-lede" data-testid="routines-hidden">
        {{ hiddenText(visible.hidden) }}
      </p>
      <VEmpty v-if="!waiting && rows.length === 0 && !failure">
        {{
          visible.hidden > 0
            ? 'Every routine here had no calls in the window.'
            : state.search === ''
              ? 'No routine to show.'
              : 'No routine matches that name.'
        }}
      </VEmpty>
      <VTable v-else-if="!waiting && rows.length > 0" variant="list" data-testid="routines-table">
        <template #caption>How long each routine takes, over the window</template>
        <template #head>
          <th v-if="databaseShown" scope="col">Database</th>
          <th scope="col">Routine</th>
          <th
            v-for="column in shownColumns"
            :key="column.sort"
            scope="col"
            class="text-right"
            :aria-sort="ariaSort(state, column.sort)"
            :data-sort="column.sort"
          >
            <button
              type="button"
              class="inline-flex cursor-pointer items-center gap-1 border-0 bg-transparent p-0 font-[inherit] text-[inherit] uppercase"
              @click="change(nextSort(state, column.sort))"
            >
              {{ column.label }}
              <span v-if="state.sort === column.sort" aria-hidden="true" data-sort-mark>
                {{ state.order === 'asc' ? '▲' : '▼' }}
              </span>
            </button>
          </th>
        </template>
        <template v-for="entry in rows" :key="entry.key">
          <tr v-if="entry.row === 'group'" data-routine-group>
            <td v-if="databaseShown" class="font-mono whitespace-nowrap">
              {{ entry.group.database }}
            </td>
            <td class="font-mono text-xs font-semibold">
              {{ entry.group.schema }}.{{ entry.group.base }}
            </td>
            <td :colspan="spannedColumns" class="text-sm text-fg-soft">
              {{ versionCountText(entry.count) }}
            </td>
          </tr>
          <tr
            v-else
            :data-routine-version="entry.row === 'version' ? '' : undefined"
            data-routine-row
          >
            <td v-if="databaseShown" class="font-mono whitespace-nowrap">
              {{ entry.routine.database }}
            </td>
            <td class="font-mono text-xs" :class="{ 'pl-6': entry.row === 'version' }">
              <RouterLink
                :to="definitionLink(entry.routine)"
                :title="routineName(entry.routine)"
                class="font-medium text-primary"
                data-routine-link
              >
                {{ routineLabel(entry.routine) }}
              </RouterLink>
              <VBadge v-if="entry.routine.kind === 'procedure'" size="sm">procedure</VBadge>
              <VBadge v-if="entry.row === 'version' && entry.latest" tone="primary" size="sm">
                latest
              </VBadge>
            </td>
            <td class="text-right font-mono">{{ formatCount(entry.routine.calls) }}</td>
            <td
              class="text-right font-mono"
              :class="entry.routine.timed ? 'whitespace-nowrap' : 'text-sm text-fg-soft'"
              data-cell="mean"
            >
              {{ meanText(entry.routine) }}
            </td>
            <td class="text-right font-mono whitespace-nowrap" data-cell="total">
              {{ timeText(entry.routine, entry.routine.totalMs) }}
            </td>
            <td class="text-right font-mono whitespace-nowrap" data-cell="self">
              {{ timeText(entry.routine, entry.routine.selfMs) }}
            </td>
            <td v-if="slowestShown" class="text-right" data-cell="max">
              <template v-if="entry.routine.maxCallMs !== null">
                <span class="font-mono whitespace-nowrap">
                  {{ formatMs(entry.routine.maxCallMs) }}
                </span>
                <span class="block text-xs text-fg-muted">
                  top-level calls<template v-if="statsSince">
                    since
                    <time :datetime="statsSince" :title="fullStamp(statsSince)">{{
                      stamp(statsSince)
                    }}</time></template
                  >
                </span>
              </template>
            </td>
          </tr>
        </template>
      </VTable>
    </div>
  </section>
</template>
