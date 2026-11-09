<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'

import type { UsageScope } from './access'
import { formatCount, formatMs } from './text'
import { auditLink, topRefusals, windowRange, windows } from './usage'
import { useLoad } from './useLoad'
import {
  type SelectOption,
  VBanner,
  VEmpty,
  VField,
  VSelect,
  VSkeleton,
  VTable,
} from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'

const props = defineProps<{
  scope: UsageScope
  auditLog: UsageScope
  self?: string | undefined
  from?: string | undefined
  to?: string | undefined
  operator?: string | undefined
  database?: string | undefined
  reason?: string | undefined
}>()

const chosen = props.from && props.to ? { from: props.from, to: props.to } : null

const windowOptions: SelectOption[] = [
  ...(chosen ? [{ value: 'chosen', label: 'The window chosen on the overview' }] : []),
  ...windows.map((days) => ({ value: String(days), label: `Last ${days} days` })),
]

const days = ref(chosen ? 'chosen' : '7')
const range = ref(chosen ?? windowRange(7, new Date()))
const usage = useLoad<components['schemas']['ObservatoryUsage']>()
const rows = computed(() => {
  const all = usage.data.value?.rows ?? []
  const reason = props.reason
  return reason ? all.filter((row) => row.refusals.some((r) => r.reason === reason)) : all
})

const waiting = computed(() => usage.data.value === null && usage.failure.value === null)

const skeletonHeadings = [
  'Operator',
  'Database',
  'Statements',
  'Allowed',
  'Refused',
  'Top refusal reasons',
  'Executed',
  'p50',
  'p95',
  'Rows',
  '',
]
const skeletonColumns = [
  'w-21/25',
  'w-21/25',
  'w-1/3',
  'w-1/3',
  'w-1/3',
  'w-full',
  'w-1/3',
  'w-1/3',
  'w-1/3',
  'w-1/3',
  'w-1/2',
]

function load(): void {
  range.value =
    days.value === 'chosen' && chosen ? chosen : windowRange(Number(days.value), new Date())
  const query = {
    ...range.value,
    ...(props.database ? { database: props.database } : {}),
    ...(props.operator ? { operatorId: props.operator } : {}),
  }
  void usage.run(() => api.GET('/observatory/usage', { params: { query } }))
}

watch(days, load)
onMounted(load)
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="Avior usage">
    <div class="flex flex-none items-end gap-3 px-3 py-2">
      <VField label="Window" for="usage-window">
        <VSelect id="usage-window" v-model="days" :options="windowOptions" size="sm" />
      </VField>
      <p class="m-0 text-sm text-fg-soft">
        {{ props.scope === 'all' ? 'Every operator' : 'Your statements only' }}. Elapsed and rows
        are over executed statements; a refused statement never ran.
        <template v-if="props.database">Database {{ props.database }}.</template>
        <template v-if="props.reason">Rows with a {{ props.reason }} refusal.</template>
      </p>
    </div>

    <VBanner v-if="usage.failure.value" icon="circle-x">{{ usage.failure.value }}</VBanner>
    <VEmpty v-else-if="rows.length === 0 && !usage.busy.value && !waiting">
      No statement was submitted in this window.
    </VEmpty>

    <div v-if="rows.length > 0" class="min-h-0 flex-1 overflow-auto">
      <VTable variant="list" data-testid="usage-table">
        <template #caption>Statements per operator and database</template>
        <template #head>
          <th scope="col">Operator</th>
          <th scope="col">Database</th>
          <th scope="col" class="text-right">Statements</th>
          <th scope="col" class="text-right">Allowed</th>
          <th scope="col" class="text-right">Refused</th>
          <th scope="col">Top refusal reasons</th>
          <th scope="col" class="text-right">Executed</th>
          <th scope="col" class="text-right">p50</th>
          <th scope="col" class="text-right">p95</th>
          <th scope="col" class="text-right">Rows</th>
          <th scope="col"><span class="sr-only">Audit log</span></th>
        </template>
        <tr v-for="row in rows" :key="`${row.operatorId}:${row.database}`">
          <td class="whitespace-nowrap">{{ row.operator }}</td>
          <td class="font-mono whitespace-nowrap">{{ row.database }}</td>
          <td class="text-right font-mono">{{ formatCount(row.statements) }}</td>
          <td class="text-right font-mono text-ok">{{ formatCount(row.allowed) }}</td>
          <td class="text-right font-mono" :class="row.refused > 0 ? 'text-danger' : ''">
            {{ formatCount(row.refused) }}
          </td>
          <td class="font-mono text-xs">{{ topRefusals(row) }}</td>
          <td class="text-right font-mono">{{ formatCount(row.executed) }}</td>
          <td class="text-right font-mono whitespace-nowrap">{{ formatMs(row.elapsedP50Ms) }}</td>
          <td class="text-right font-mono whitespace-nowrap">{{ formatMs(row.elapsedP95Ms) }}</td>
          <td class="text-right font-mono">{{ formatCount(row.rowsTotal) }}</td>
          <td class="whitespace-nowrap">
            <RouterLink
              v-if="auditLink(row, props.auditLog, range, props.self)"
              :to="auditLink(row, props.auditLog, range, props.self)!"
              class="text-sm font-medium text-primary"
            >
              Audit log
            </RouterLink>
          </td>
        </tr>
      </VTable>
    </div>
    <div v-else-if="waiting" class="min-h-0 flex-1 overflow-auto">
      <VSkeleton
        variant="table"
        :headings="skeletonHeadings"
        :columns="skeletonColumns"
        label="Reading usage"
      />
    </div>
  </section>
</template>
