<script setup lang="ts">
import { computed, ref } from 'vue'

import FlagMark from './FlagMark.vue'
import { flagOptions, kindOptions, objectRows, qualifiedName } from './lastUsed'
import { formatBytes, formatGrowth, groupCountText, lastUsedText, type Tracking } from './text'
import { useDatabasePick } from './useDatabasePick'
import { useLoad } from './useLoad'
import { VBadge, VBanner, VEmpty, VField, VSelect, VSkeleton, VTable } from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'

const props = defineProps<{ database?: string | undefined; flagged?: boolean }>()

const objects = useLoad<components['schemas']['ObservatoryObjects']>()
const databases = useDatabasePick((database) => {
  void objects.run(() => api.GET('/observatory/objects', { params: { query: { database } } }))
}, props.database)

const kind = ref('')
const flagged = ref(props.flagged ? 'flagged' : 'all')
const now = ref(new Date())

const tracking = computed<Tracking>(() => ({
  idleDays: objects.data.value?.idleDays ?? 30,
  routinesTrackedSince: objects.data.value?.routinesTrackedSince ?? null,
  relationsTrackedSince: objects.data.value?.relationsTrackedSince ?? null,
}))
const shown = computed(() =>
  objectRows(
    objects.data.value?.objects ?? [],
    objects.data.value?.routineGroups ?? [],
    kind.value,
    flagged.value === 'flagged',
  ),
)
const failure = computed(() => databases.list.failure.value ?? objects.failure.value)
const waiting = computed(
  () =>
    objects.data.value === null &&
    failure.value === null &&
    (databases.list.data.value === null || databases.database.value !== ''),
)
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="Last used" data-testid="last-used">
    <div class="flex flex-none flex-wrap items-end gap-3 px-3 py-2">
      <VField label="Database" for="last-used-database">
        <VSelect
          id="last-used-database"
          :model-value="databases.database.value"
          :options="databases.options.value"
          size="sm"
          @update:model-value="databases.pick"
        />
      </VField>
      <VField label="Kind" for="last-used-kind">
        <VSelect id="last-used-kind" v-model="kind" :options="kindOptions" size="sm" />
      </VField>
      <VField label="Show" for="last-used-flagged">
        <VSelect id="last-used-flagged" v-model="flagged" :options="flagOptions" size="sm" />
      </VField>
    </div>

    <p class="list-lede" data-testid="columns-note">
      Columns are not listed: PostgreSQL keeps no per-column usage statistics.
    </p>

    <VBanner v-if="failure" icon="circle-x">{{ failure }}</VBanner>
    <VBanner v-else-if="objects.data.value?.flagsWithheld" tone="warn" :alert="false" icon="info">
      {{ objects.data.value.flagsWithheld }}
    </VBanner>

    <div class="min-h-0 flex-1 overflow-auto">
      <VSkeleton
        v-if="waiting"
        variant="table"
        :headings="['Object', 'Kind', 'Last use', 'Flag', 'Size', 'Growth 30 days']"
        :columns="['w-full', 'w-1/3', 'w-21/25', 'w-1/3', 'w-1/2', 'w-1/2']"
        :row-lines="2"
        label="Reading when each object was last used"
      />
      <VEmpty v-else-if="shown.length === 0 && !objects.busy.value && !failure">
        {{ flagged === 'flagged' ? 'Nothing here is flagged.' : 'No object to show.' }}
      </VEmpty>
      <VTable v-else-if="shown.length > 0" variant="list" data-testid="objects-table">
        <template #caption>When each object was last seen used</template>
        <template #head>
          <th scope="col">Object</th>
          <th scope="col">Kind</th>
          <th scope="col">Last use</th>
          <th scope="col">Flag</th>
          <th scope="col" class="text-right">Size</th>
          <th scope="col" class="text-right">Growth 30 days</th>
        </template>
        <template v-for="entry in shown" :key="entry.key">
          <tr v-if="entry.row === 'group'" data-routine-group>
            <td class="font-mono text-xs font-semibold">
              {{ entry.group.schema }}.{{ entry.group.base }}
            </td>
            <td colspan="5" class="text-sm text-fg-soft">{{ groupCountText(entry.group) }}</td>
          </tr>
          <tr
            v-else
            :data-flagged="entry.object.flagged"
            :data-routine-version="entry.row === 'version' ? '' : undefined"
          >
            <td class="font-mono text-xs" :class="{ 'pl-6': entry.row === 'version' }">
              {{ qualifiedName(entry.object) }}
              <VBadge v-if="entry.row === 'version' && entry.latest" tone="primary" size="sm">
                latest
              </VBadge>
              <span v-if="entry.object.table" class="block text-fg-muted">
                on {{ entry.object.table }}
              </span>
            </td>
            <td>
              <VBadge size="sm">{{ entry.object.kind }}</VBadge>
            </td>
            <td class="text-sm">
              {{ lastUsedText(entry.object, tracking, now) }}
              <span v-if="entry.object.lastUsedNode" class="block text-xs text-fg-muted">
                on {{ entry.object.lastUsedNode }}
              </span>
            </td>
            <td>
              <FlagMark v-if="entry.object.flagged" :idle-days="tracking.idleDays" />
              <VBadge v-else-if="entry.object.constraintBacked" size="sm">
                enforces a constraint
              </VBadge>
            </td>
            <td class="text-right font-mono whitespace-nowrap">
              {{ entry.object.sizeBytes === null ? '' : formatBytes(entry.object.sizeBytes) }}
            </td>
            <td class="text-right font-mono whitespace-nowrap">
              {{ entry.object.kind === 'table' ? formatGrowth(entry.object.growth30dBytes) : '' }}
            </td>
          </tr>
        </template>
      </VTable>
    </div>
  </section>
</template>
