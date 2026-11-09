<script setup lang="ts">
import { truncate } from './overview'
import { formatMs } from './text'
import { VBanner, VButton, VCard, VEmpty, VSkeleton, VTable } from '@/components'
import type { components } from '@/gen/api'
import { fullStamp, stamp } from '@/shared/timestamp'

const props = defineProps<{
  activity: components['schemas']['ObservatoryActivity'] | null
  failure: string | null
  busy: boolean
  refreshSeconds: number | null
}>()

const emit = defineEmits<{ refresh: [] }>()
</script>

<template>
  <VCard as="section" pad aria-labelledby="live-heading" data-testid="live-panel">
    <div class="flex flex-wrap items-center gap-3 pb-2">
      <h2 id="live-heading" class="m-0 mr-auto eyebrow">Running now, longer than 60 s</h2>
      <span class="text-sm text-fg-soft" data-testid="live-cadence">
        {{
          props.refreshSeconds === null
            ? 'Auto-refresh is off'
            : `Refreshing every ${props.refreshSeconds} s`
        }}
      </span>
      <VButton icon="replay" size="sm" :disabled="props.busy" @click="emit('refresh')">
        Refresh
      </VButton>
    </div>
    <p v-if="props.activity" class="m-0 pb-2 text-sm text-fg-soft">
      As of
      <time :datetime="props.activity.generatedAt" :title="fullStamp(props.activity.generatedAt)">{{
        stamp(props.activity.generatedAt)
      }}</time
      >.
    </p>
    <VBanner v-if="props.failure" icon="circle-x">{{ props.failure }}</VBanner>
    <VSkeleton v-else-if="props.activity === null" />
    <VEmpty v-else-if="props.activity.backends.length === 0">
      Nothing has been running that long.
    </VEmpty>
    <VTable v-else variant="log" data-testid="live-table">
      <template #caption>Statements running longer than 60 seconds, longest first</template>
      <template #head>
        <th scope="col" class="text-right">Running for</th>
        <th scope="col">Database</th>
        <th scope="col">Role</th>
        <th scope="col" class="w-1/2">Statement</th>
      </template>
      <tr v-for="backend in props.activity.backends" :key="backend.pid">
        <td class="text-right font-mono whitespace-nowrap" :title="fullStamp(backend.startedAt)">
          {{ formatMs(backend.runningForMs) }}
        </td>
        <td class="font-mono whitespace-nowrap">{{ backend.database }}</td>
        <td class="font-mono whitespace-nowrap">{{ backend.role }}</td>
        <td class="w-1/2 font-mono text-xs" :title="backend.text">
          {{ truncate(backend.text, 120) }}
        </td>
      </tr>
    </VTable>
  </VCard>
</template>
