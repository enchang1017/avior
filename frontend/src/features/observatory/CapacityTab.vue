<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { formatBytes, formatDelta, utcDay } from './text'
import { useLoad } from './useLoad'
import { VBadge, VBanner, VEmpty, VSkeleton, VSparkline, VTable } from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'
import { fullStamp, stamp } from '@/shared/timestamp'

const props = defineProps<{ database?: string | undefined }>()

const databases = useLoad<components['schemas']['ObservatoryDatabases']>()
const rows = computed(() => {
  const all = databases.data.value?.databases ?? []
  return props.database ? all.filter((row) => row.database === props.database) : all
})

const waiting = computed(() => databases.data.value === null && databases.failure.value === null)

onMounted(() => {
  void databases.run(() => api.GET('/observatory/databases'))
})
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="Capacity">
    <p v-if="databases.data.value" class="list-lede">
      <template v-if="databases.data.value.lastRunAt">
        Last snapshot
        <time
          :datetime="databases.data.value.lastRunAt"
          :title="fullStamp(databases.data.value.lastRunAt)"
          >{{ stamp(databases.data.value.lastRunAt) }}</time
        >.
      </template>
      <template v-else>No snapshot has run yet.</template>
      An object is flagged when its last use is more than
      {{ databases.data.value.idleDays }} days ago.
      <template v-if="props.database">Showing {{ props.database }} only.</template>
    </p>

    <VBanner v-if="databases.failure.value" icon="circle-x">{{ databases.failure.value }}</VBanner>
    <VBanner v-else-if="databases.data.value?.flagsWithheld" tone="warn" :alert="false" icon="info">
      {{ databases.data.value.flagsWithheld }}
    </VBanner>
    <VEmpty
      v-if="rows.length === 0 && !waiting && !databases.busy.value && !databases.failure.value"
    >
      No database you can reach has been measured yet.
    </VEmpty>

    <div v-if="rows.length > 0" class="min-h-0 flex-1 overflow-auto">
      <VTable variant="list" data-testid="capacity-table">
        <template #caption>Size and growth per database</template>
        <template #head>
          <th scope="col">Database</th>
          <th scope="col" class="text-right">Size now</th>
          <th scope="col" class="text-right">Δ 7 days</th>
          <th scope="col" class="text-right">Δ 30 days</th>
          <th scope="col">Growth</th>
          <th scope="col" class="text-right">Flagged</th>
          <th scope="col">Statistics reset</th>
        </template>
        <tr v-for="row in rows" :key="row.database">
          <td class="font-mono whitespace-nowrap">{{ row.database }}</td>
          <td class="text-right font-mono whitespace-nowrap">
            {{ row.sizeBytes === null ? '—' : formatBytes(row.sizeBytes) }}
          </td>
          <td class="text-right font-mono whitespace-nowrap">
            {{ formatDelta(row.sizeBytes, row.sizeBytes7d) }}
          </td>
          <td class="text-right font-mono whitespace-nowrap">
            {{ formatDelta(row.sizeBytes, row.sizeBytes30d) }}
          </td>
          <td>
            <VSparkline
              :values="row.growth.map((point) => point.sizeBytes)"
              :label="`${row.database} size over ${row.growth.length} days`"
            />
          </td>
          <td class="text-right">
            <VBadge v-if="row.flagged > 0" tone="warn">{{ row.flagged }} flagged</VBadge>
            <span v-else class="font-mono text-fg-muted">0</span>
          </td>
          <td class="text-sm text-fg-soft">
            {{ row.lastResetAt ? `reset ${utcDay(row.lastResetAt)}` : '—' }}
          </td>
        </tr>
      </VTable>
    </div>
    <div v-else-if="waiting" class="min-h-0 flex-1 overflow-auto">
      <VSkeleton
        variant="table"
        :headings="[
          'Database',
          'Size now',
          'Δ 7 days',
          'Δ 30 days',
          'Growth',
          'Flagged',
          'Statistics reset',
        ]"
        :columns="['w-21/25', 'w-1/2', 'w-1/2', 'w-1/2', 'w-full', 'w-1/3', 'w-1/2']"
        label="Reading capacity"
      />
    </div>
  </section>
</template>
