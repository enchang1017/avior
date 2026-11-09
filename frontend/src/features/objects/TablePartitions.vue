<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { listPage, recent, recentCount } from './partitionList'
import type { ExactCount, PartitionEntry, TablePartitioning } from './useTablePartitions'
import {
  type SelectOption,
  VBanner,
  VButton,
  VCard,
  VInput,
  VPager,
  VSelect,
  VTable,
} from '@/components'

/**
 * FEAT-075: a partitioned table as its partitions, or a partition as the table
 * it belongs to.
 *
 * Every row count here is the planner's estimate and is labelled as one. The
 * exact count is a statement the operator asks for, one partition at a time.
 */
const props = defineProps<{
  partitioning: TablePartitioning
  counts: Record<string, ExactCount>
}>()

const emit = defineEmits<{ count: [partition: PartitionEntry] }>()

const numbers = new Intl.NumberFormat('en')

/*
 * Kept here rather than imported from the Observatory's: a shared import splits
 * that module into a chunk of its own, and the Observatory route pays for the
 * extra request in its first-load budget (P3).
 */
const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

function formatBytes(bytes: number): string {
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${unit === 0 ? String(value) : value.toFixed(1)} ${units[unit]}`
}

function rows(value: number | null | undefined): string {
  return value === null || value === undefined ? 'unknown' : `~${numbers.format(value)}`
}

const keyOf = (partition: PartitionEntry): string => `${partition.schema}.${partition.name}`

/** The count's answer once there is one: the rows as the server wrote them, or why not. */
function settled(partition: PartitionEntry): string | null {
  const count = props.counts[keyOf(partition)]
  if (count?.state === 'counted') return count.rows
  if (count?.state === 'refused') return count.message
  return null
}

function settledClass(partition: PartitionEntry): string {
  return props.counts[keyOf(partition)]?.state === 'refused'
    ? 'refused text-xs text-danger whitespace-normal'
    : 'counted font-mono'
}

/*
 * The newest few and the DEFAULT by default: a table partitioned by month for
 * years has hundreds of partitions, and a page of all of them buries the ones
 * an operator came for. The whole list is a click away, filtered and paged.
 */
const showAll = ref(false)
const filter = ref('')
const order = ref<'newest' | 'oldest'>('newest')
const page = ref(1)

const orderOptions: SelectOption[] = [
  { value: 'newest', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest first' },
]

watch([filter, order, () => props.partitioning], () => {
  page.value = 1
})

const listed = computed(() =>
  listPage(props.partitioning.partitions, {
    filter: filter.value,
    newestFirst: order.value === 'newest',
    page: page.value,
  }),
)

const shown = computed(() =>
  showAll.value ? listed.value.rows : recent(props.partitioning.partitions),
)

const hidden = computed(() => props.partitioning.partitions.length - shown.value.length)

function chooseOrder(value: string): void {
  order.value = value === 'oldest' ? 'oldest' : 'newest'
}

const summary = computed(() => {
  const p = props.partitioning
  return [
    p.key ?? '',
    `${p.partitions.length} partitions`,
    `${rows(p.approxRows)} rows (estimated)`,
    formatBytes(p.totalBytes ?? 0),
  ].join(' · ')
})
</script>

<template>
  <VCard v-if="props.partitioning.parent" as="section" pad class="partition-of">
    <h2 class="mx-0 mt-0 mb-2 text-md font-bold">Partition</h2>
    <p class="text-sm">
      A partition of
      <span class="font-mono font-semibold"
        >{{ props.partitioning.parent.schema }}.{{ props.partitioning.parent.name }}</span
      >, holding <span class="font-mono">{{ props.partitioning.parent.bound }}</span
      >. Query the parent and the planner picks the partition.
    </p>
  </VCard>

  <VCard v-if="props.partitioning.strategy" as="section" pad class="partitions">
    <h2 class="mx-0 mt-0 mb-1 text-md font-bold">Partitions</h2>
    <p class="summary mx-0 mt-0 mb-2 font-mono text-xs text-fg-soft">{{ summary }}</p>

    <VBanner
      v-for="warning in props.partitioning.warnings"
      :key="warning.code + (warning.subject ?? '')"
      tone="warn"
      icon="warning"
      :alert="false"
      class="warning mb-2"
    >
      {{ warning.message }}
    </VBanner>

    <div class="list-controls mb-2 flex flex-wrap items-center gap-2">
      <span v-if="!showAll" class="text-xs text-fg-soft">
        The DEFAULT partition and the newest {{ recentCount
        }}<template v-if="hidden > 0"> · {{ hidden }} more not shown</template>
      </span>
      <template v-else>
        <VInput
          v-model="filter"
          size="sm"
          type="search"
          class="max-w-xs filter"
          placeholder="Filter by name or bound"
          aria-label="Filter partitions"
        />
        <VSelect
          id="partition-order"
          size="sm"
          class="order"
          :model-value="order"
          :options="orderOptions"
          aria-label="Order partitions"
          @update:model-value="chooseOrder"
        />
        <span class="matched text-xs text-fg-soft"
          >{{ listed.matched }} of {{ props.partitioning.partitions.length }}</span
        >
      </template>
      <VButton
        size="sm"
        class="show-all ml-auto"
        :icon="showAll ? 'chevron-down' : 'chevron-right'"
        @click="showAll = !showAll"
      >
        {{
          showAll
            ? 'Show the newest only'
            : `Show all ${props.partitioning.partitions.length} partitions`
        }}
      </VButton>
    </div>

    <div class="overflow-x-auto">
      <VTable variant="structure">
        <template #head>
          <th scope="col">Partition</th>
          <th scope="col">Bound</th>
          <th scope="col" class="text-right">Rows (estimated)</th>
          <th scope="col" class="text-right">Size</th>
          <th scope="col" class="text-right">Exact rows</th>
        </template>
        <tr v-for="partition in shown" :key="keyOf(partition)">
          <td class="name font-mono font-semibold whitespace-nowrap">
            {{ partition.name }}
          </td>
          <td class="bound font-mono text-fg-soft">{{ partition.bound }}</td>
          <td class="estimate text-right font-mono whitespace-nowrap">
            {{ rows(partition.approxRows) }}
          </td>
          <td class="text-right font-mono whitespace-nowrap">
            {{ formatBytes(partition.totalBytes) }}
          </td>
          <td class="exact text-right whitespace-nowrap">
            <span v-if="settled(partition)" :class="settledClass(partition)">{{
              settled(partition)
            }}</span>
            <VButton
              v-else
              size="sm"
              class="count"
              :disabled="props.counts[keyOf(partition)]?.state === 'counting'"
              title="Counts this partition exactly, through the guard"
              @click="emit('count', partition)"
            >
              {{ props.counts[keyOf(partition)]?.state === 'counting' ? 'Counting…' : 'Count' }}
            </VButton>
          </td>
        </tr>
      </VTable>
    </div>
    <VPager
      v-if="showAll && listed.pages > 1"
      class="pager"
      :page="listed.page"
      :can-go-back="listed.page > 1"
      :has-more="listed.page < listed.pages"
      @previous="page = listed.page - 1"
      @next="page = listed.page + 1"
    >
      Page {{ listed.page }} of {{ listed.pages }}
    </VPager>
  </VCard>
</template>
