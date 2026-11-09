<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import StatementText from './StatementText.vue'
import { formatBytes, formatCount, formatMs, formatPercent } from './text'
import { useDatabasePick } from './useDatabasePick'
import { useLoad } from './useLoad'
import {
  VCheckbox,
  type SelectOption,
  VBanner,
  VButton,
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

type Sort = 'total' | 'mean' | 'calls'

const sortOptions: SelectOption[] = [
  { value: 'total', label: 'Total time' },
  { value: 'mean', label: 'Mean time' },
  { value: 'calls', label: 'Calls' },
]

const statements = useLoad<components['schemas']['ObservatoryStatements']>()
const activity = useLoad<components['schemas']['ObservatoryActivity']>()
const health = useLoad<components['schemas']['ObservatoryHealth']>()

const props = defineProps<{ sort?: Sort | undefined; database?: string | undefined }>()

const sort = ref<Sort>(props.sort ?? 'total')
const statementDatabase = ref(props.database ?? '')
const includeOwn = ref(false)
const minSeconds = ref<string | number>(60)

const databases = useDatabasePick((database) => {
  void health.run(() => api.GET('/observatory/health', { params: { query: { database } } }))
}, props.database)
const everyDatabase = computed<SelectOption[]>(() => [
  { value: '', label: 'Every database' },
  ...databases.options.value,
])

function loadStatements(): void {
  const database = statementDatabase.value
  void statements.run(() =>
    api.GET('/observatory/statements', {
      params: {
        query: {
          sort: sort.value,
          ...(database === '' ? {} : { database }),
          ...(includeOwn.value ? { includeOwn: true } : {}),
        },
      },
    }),
  )
}

function loadActivity(): void {
  const seconds = Number(minSeconds.value)
  const query = Number.isFinite(seconds) && seconds >= 0 ? { minSeconds: seconds } : {}
  void activity.run(() => api.GET('/observatory/activity', { params: { query } }))
}

const statementsWaiting = computed(
  () => statements.data.value === null && statements.failure.value === null,
)
const activityWaiting = computed(
  () => activity.data.value === null && activity.failure.value === null,
)
const healthWaiting = computed(
  () =>
    health.data.value === null &&
    health.failure.value === null &&
    databases.list.failure.value === null &&
    (databases.list.data.value === null || databases.database.value !== ''),
)

watch([sort, statementDatabase, includeOwn], loadStatements)
onMounted(() => {
  loadStatements()
  loadActivity()
})
</script>

<template>
  <section class="min-h-0 flex-1 overflow-auto" aria-label="Performance">
    <section class="py-2" aria-labelledby="statements-heading" data-testid="top-statements">
      <div class="flex flex-wrap items-end gap-3 px-3 pb-2">
        <h2 id="statements-heading" class="m-0 mr-auto self-center eyebrow">Top statements</h2>
        <VField label="Sort by" for="statements-sort">
          <VSelect
            id="statements-sort"
            :model-value="sort"
            :options="sortOptions"
            size="sm"
            @update:model-value="sort = $event as Sort"
          />
        </VField>
        <VField label="Database" for="statements-database">
          <VSelect
            id="statements-database"
            v-model="statementDatabase"
            :options="everyDatabase"
            size="sm"
          />
        </VField>
        <VCheckbox v-model="includeOwn" data-testid="include-own">
          Include Avior's own monitoring
        </VCheckbox>
      </div>
      <p v-if="statements.data.value?.statsSince" class="list-lede">
        Cumulative since
        <time
          :datetime="statements.data.value.statsSince"
          :title="fullStamp(statements.data.value.statsSince)"
          >{{ stamp(statements.data.value.statsSince) }}</time
        >.
      </p>
      <VBanner v-if="statements.failure.value" icon="circle-x" data-testid="statements-failure">
        {{ statements.failure.value }}
      </VBanner>
      <VEmpty v-else-if="statements.data.value?.statements.length === 0">
        No statement has been recorded.
      </VEmpty>
      <VTable v-if="(statements.data.value?.statements.length ?? 0) > 0" variant="log">
        <template #caption>The statements that cost the server most</template>
        <template #head>
          <th scope="col" class="w-1/2">Statement</th>
          <th scope="col">Database</th>
          <th scope="col">Role</th>
          <th scope="col" class="text-right">Calls</th>
          <th scope="col" class="text-right">Total</th>
          <th scope="col" class="text-right">Mean</th>
          <th scope="col" class="text-right">Rows</th>
        </template>
        <tr v-for="statement in statements.data.value?.statements ?? []" :key="statement.queryId">
          <td class="w-1/2"><StatementText :text="statement.text" /></td>
          <td class="font-mono whitespace-nowrap">{{ statement.database }}</td>
          <td class="font-mono whitespace-nowrap">{{ statement.role }}</td>
          <td class="text-right font-mono">{{ formatCount(statement.calls) }}</td>
          <td class="text-right font-mono whitespace-nowrap">{{ formatMs(statement.totalMs) }}</td>
          <td class="text-right font-mono whitespace-nowrap">{{ formatMs(statement.meanMs) }}</td>
          <td class="text-right font-mono">{{ formatCount(statement.rows) }}</td>
        </tr>
      </VTable>
      <VSkeleton
        v-else-if="statementsWaiting"
        variant="table"
        table="log"
        :headings="['Statement', 'Database', 'Role', 'Calls', 'Total', 'Mean', 'Rows']"
        :columns="['w-full', 'w-21/25', 'w-1/2', 'w-1/3', 'w-1/3', 'w-1/3', 'w-1/3']"
        :rows="4"
        :row-lines="2"
        label="Reading top statements"
      />
    </section>

    <section class="py-2" aria-labelledby="activity-heading" data-testid="running-now">
      <form class="flex flex-wrap items-end gap-3 px-3 pb-2" @submit.prevent="loadActivity">
        <h2 id="activity-heading" class="m-0 mr-auto self-center eyebrow">Running now</h2>
        <VField label="Running longer than (s)" for="activity-min-seconds">
          <VInput id="activity-min-seconds" v-model="minSeconds" type="number" min="0" size="sm" />
        </VField>
        <VButton type="submit" icon="replay" size="sm" :disabled="activity.busy.value">
          Refresh
        </VButton>
      </form>
      <p v-if="activity.data.value" class="list-lede">
        As of
        <time
          :datetime="activity.data.value.generatedAt"
          :title="fullStamp(activity.data.value.generatedAt)"
          >{{ stamp(activity.data.value.generatedAt) }}</time
        >. Refresh to read it again.
      </p>
      <VBanner v-if="activity.failure.value" icon="circle-x">{{ activity.failure.value }}</VBanner>
      <VEmpty v-else-if="activity.data.value?.backends.length === 0">
        Nothing has been running that long.
      </VEmpty>
      <VTable v-if="(activity.data.value?.backends.length ?? 0) > 0" variant="log">
        <template #caption>Statements running now, longest first</template>
        <template #head>
          <th scope="col" class="text-right">PID</th>
          <th scope="col">Database</th>
          <th scope="col">Role</th>
          <th scope="col">State</th>
          <th scope="col" class="text-right">Running for</th>
          <th scope="col">Waiting on</th>
          <th scope="col" class="w-1/2">Statement</th>
        </template>
        <tr v-for="backend in activity.data.value?.backends ?? []" :key="backend.pid">
          <td class="text-right font-mono">{{ backend.pid }}</td>
          <td class="font-mono whitespace-nowrap">{{ backend.database }}</td>
          <td class="font-mono whitespace-nowrap">{{ backend.role }}</td>
          <td class="whitespace-nowrap">{{ backend.state }}</td>
          <td class="text-right font-mono whitespace-nowrap" :title="fullStamp(backend.startedAt)">
            {{ formatMs(backend.runningForMs) }}
          </td>
          <td class="font-mono text-xs">{{ backend.waitEvent ?? '—' }}</td>
          <td class="w-1/2"><StatementText :text="backend.text" /></td>
        </tr>
      </VTable>
      <VSkeleton
        v-else-if="activityWaiting"
        variant="table"
        table="log"
        :headings="['PID', 'Database', 'Role', 'State', 'Running for', 'Waiting on', 'Statement']"
        :columns="['w-1/3', 'w-21/25', 'w-1/2', 'w-1/2', 'w-1/3', 'w-1/2', 'w-full']"
        :rows="3"
        label="Reading what is running now"
      />
    </section>

    <section class="py-2" aria-labelledby="health-heading" data-testid="table-health">
      <div class="flex flex-wrap items-end gap-3 px-3 pb-2">
        <h2 id="health-heading" class="m-0 mr-auto self-center eyebrow">Table health</h2>
        <VField label="Database" for="health-database">
          <VSelect
            id="health-database"
            :model-value="databases.database.value"
            :options="databases.options.value"
            size="sm"
            @update:model-value="databases.pick"
          />
        </VField>
      </div>
      <p class="list-lede">Reclaimable tuples are row versions that vacuum has not yet cleared.</p>
      <VBanner v-if="health.failure.value ?? databases.list.failure.value" icon="circle-x">
        {{ health.failure.value ?? databases.list.failure.value }}
      </VBanner>
      <VEmpty v-else-if="health.data.value?.tables.length === 0">No table to show.</VEmpty>
      <VTable v-if="(health.data.value?.tables.length ?? 0) > 0" variant="list">
        <template #caption>Vacuum and scan figures per table</template>
        <template #head>
          <th scope="col">Table</th>
          <th scope="col" class="text-right">Size</th>
          <th scope="col" class="text-right">Reclaimable tuples</th>
          <th scope="col" class="text-right">Reclaimable share</th>
          <th scope="col" class="text-right">Seq scans</th>
          <th scope="col" class="text-right">Rows by seq scan</th>
          <th scope="col">Last autovacuum</th>
          <th scope="col">Last autoanalyze</th>
        </template>
        <tr v-for="table in health.data.value?.tables ?? []" :key="`${table.schema}.${table.name}`">
          <td class="font-mono text-xs">{{ table.schema }}.{{ table.name }}</td>
          <td class="text-right font-mono whitespace-nowrap">{{ formatBytes(table.sizeBytes) }}</td>
          <td class="text-right font-mono">{{ formatCount(table.deadTuples) }}</td>
          <td class="text-right font-mono">{{ formatPercent(table.deadRatio) }}</td>
          <td class="text-right font-mono">{{ formatCount(table.seqScans) }}</td>
          <td class="text-right font-mono">{{ formatCount(table.seqRowsRead) }}</td>
          <td class="whitespace-nowrap" :title="fullStamp(table.lastAutovacuum ?? 'never')">
            {{ table.lastAutovacuum ? stamp(table.lastAutovacuum) : 'never' }}
          </td>
          <td class="whitespace-nowrap" :title="fullStamp(table.lastAutoanalyze ?? 'never')">
            {{ table.lastAutoanalyze ? stamp(table.lastAutoanalyze) : 'never' }}
          </td>
        </tr>
      </VTable>
      <VSkeleton
        v-else-if="healthWaiting"
        variant="table"
        :headings="[
          'Table',
          'Size',
          'Reclaimable tuples',
          'Reclaimable share',
          'Seq scans',
          'Rows by seq scan',
          'Last autovacuum',
          'Last autoanalyze',
        ]"
        :columns="['w-full', 'w-1/2', 'w-1/2', 'w-1/2', 'w-1/3', 'w-1/2', 'w-21/25', 'w-21/25']"
        :rows="4"
        label="Reading table health"
      />
    </section>
  </section>
</template>
