<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import TablePartitions from './TablePartitions.vue'
import { useTablePartitions, type PartitionEntry } from './useTablePartitions'
import { useTableStructure, type TableColumn } from './useTableStructure'
import {
  type SelectOption,
  VBadge,
  VBanner,
  VCard,
  VEmpty,
  VIcon,
  VLoader,
  VPageHeader,
  VSelect,
  VTable,
} from '@/components'
import SqlScript from '@/shared/SqlScript.vue'
import { describeFailure, type TableRef } from '@/shared/api'

// FEAT-012 turns this into a picker fed by db_instance.
const instanceId = '00000000-0000-0000-0000-0000000000a1'

const browser = useTableStructure(instanceId)
const partitions = useTablePartitions(instanceId)

/*
 * FEAT-075. Read beside the structure, for a partitioned table from the list
 * and for anything opened by link, which may be a partition the list leaves
 * out. A plain table from the list has nothing to say, so it is not asked.
 */
watch(
  () => [browser.database.value, browser.selected.value] as const,
  ([database, table]) => {
    const listed = browser.tables.value.find(
      (t) => t.schema === table?.schema && t.name === table.name,
    )
    if (
      database === null ||
      table === null ||
      (listed !== undefined && listed.kind !== 'partitioned')
    ) {
      partitions.clear()
      return
    }
    void partitions.load(database, table.schema, table.name)
  },
)

function countExactly(partition: PartitionEntry): void {
  const database = browser.database.value
  if (database !== null) void partitions.countExactly(database, partition)
}

const bannerText = computed(() => {
  const failure = browser.failure.value
  return failure === null ? null : describeFailure(failure)
})

const partitionsText = computed(() => {
  const failure = partitions.failure.value
  return failure === null ? null : describeFailure(failure)
})

const databaseOptions = computed<SelectOption[]>(() => [
  { value: '', label: 'Choose a database…' },
  ...browser.databases.value.map((name) => ({ value: name, label: name })),
])

const qualified = (table: TableRef): string => `${table.schema}.${table.name}`

function iconOf(table: TableRef): 'eye' | 'layers' | 'table' {
  if (table.kind === 'view') return 'eye'
  return table.kind === 'partitioned' ? 'layers' : 'table'
}

function isChosen(table: TableRef): boolean {
  const current = browser.selected.value
  return current !== null && current.schema === table.schema && current.name === table.name
}

/**
 * Absent, not empty.
 *
 * The API distinguishes them and so does the reader: a column with `DEFAULT ''`
 * has a default, and one with none has not. What is folded here is the third
 * case — a comment somebody set to an empty string, which says nothing and
 * should read as nothing rather than as a blank row with a border.
 */
function text(value: TableColumn['default']): string | null {
  return value === null || value === undefined || value === '' ? null : value
}

const route = useRoute()
const asked = (key: string): string | null => {
  const value = route.query[key]
  return typeof value === 'string' && value !== '' ? value : null
}

/** A link that names a table is a structure already asked for, from the first paint. */
const linked = ref(asked('database') !== null && asked('schema') !== null && asked('name') !== null)

const reading = computed(
  () =>
    browser.structure.value === null &&
    (linked.value || (browser.busy.value && browser.selected.value !== null)),
)

// FEAT-066 AC-18: ⌘K's Object action opens this page at a table.
onMounted(async () => {
  try {
    await browser.loadDatabases()
    const database = asked('database')
    if (database === null) return
    await browser.chooseDatabase(database)
    const schema = asked('schema')
    const name = asked('name')
    if (schema === null || name === null) return
    const listed = browser.tables.value.find((t) => t.schema === schema && t.name === name)
    await browser.choose(listed ?? { schema, name, kind: 'table' })
  } finally {
    linked.value = false
  }
})
</script>

<template>
  <div
    class="workspace grid min-h-0 flex-1 grid-cols-[--spacing(72)_1fr] overflow-hidden bg-ground"
  >
    <aside
      class="tree flex min-h-0 flex-col overflow-hidden border-r border-line bg-surface"
      aria-label="Tables"
    >
      <div class="flex flex-col gap-1 border-b border-line px-3 py-2.5">
        <label class="eyebrow" for="table-database">Database</label>
        <VSelect
          id="table-database"
          :model-value="browser.database.value ?? ''"
          :options="databaseOptions"
          :disabled="browser.busy.value"
          @update:model-value="browser.chooseDatabase($event || null)"
        />
      </div>

      <ul class="list m-0 min-h-0 list-none overflow-y-auto p-1">
        <li v-for="table in browser.tables.value" :key="qualified(table)">
          <button
            type="button"
            class="row flex w-full cursor-pointer items-center gap-1.75 rounded-control border-0 px-2 py-1.25 text-left [font:inherit]"
            :class="
              isChosen(table)
                ? 'chosen bg-primary-wash text-primary'
                : 'bg-transparent text-fg hover:bg-surface-sunken'
            "
            @click="browser.choose(table)"
          >
            <VIcon :name="iconOf(table)" />
            <!-- The size the query console's table list uses, so moving between
                 the two pages is not moving between two designs. -->
            <span
              class="name min-w-0 flex-1 truncate font-sans text-sm"
              :class="isChosen(table) ? 'font-semibold' : 'font-normal'"
              >{{ table.name }}</span
            >
            <VBadge
              v-if="table.kind === 'partitioned'"
              size="sm"
              class="partitioned flex-none whitespace-nowrap"
            >
              partitioned · {{ table.partitions ?? 0 }}
            </VBadge>
            <span
              class="ml-auto text-xs text-fg-soft"
              :class="{ 'font-semibold': isChosen(table) }"
              >{{ table.schema }}</span
            >
          </button>
        </li>
      </ul>
    </aside>

    <main class="pane flex min-h-0 min-w-0 flex-col overflow-hidden">
      <VPageHeader
        icon="table"
        subtitle="Table definition"
        :busy="browser.busy.value"
        busy-label="loading"
      >
        <template #title>
          <h1
            v-if="browser.structure.value"
            class="name m-0 font-mono text-xl leading-tight font-semibold"
          >
            <span class="font-normal text-fg-muted">{{ browser.structure.value.schema }}.</span
            >{{ browser.structure.value.name }}
          </h1>
          <h1 v-else class="m-0 font-sans text-xl leading-tight font-medium text-fg-muted">
            Nothing selected
          </h1>
        </template>

        <VBadge v-if="browser.structure.value">
          {{ browser.structure.value.kind }}
        </VBadge>
        <span v-if="browser.structure.value" class="text-xs text-fg-soft">
          owned by {{ browser.structure.value.owner }}
        </span>
      </VPageHeader>

      <VBanner v-if="bannerText" icon="circle-x">{{ bannerText }}</VBanner>

      <div v-else-if="reading" class="flex justify-center py-10">
        <VLoader size="section" label="Reading the table" />
      </div>

      <VEmpty v-else-if="browser.structure.value === null">
        Pick a table to read its columns, indexes, permissions and description.
      </VEmpty>

      <div v-else class="sections flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3">
        <VCard as="section" pad>
          <h2 class="mx-0 mt-0 mb-2 text-md font-bold">Description</h2>
          <p v-if="text(browser.structure.value.comment)" class="max-w-2xl text-md">
            {{ browser.structure.value.comment }}
          </p>
          <!-- Said rather than left blank: a table with no comment and a page
               that failed to load a comment look the same when neither speaks. -->
          <p v-else class="text-sm text-fg-soft">
            Nobody has written down what this
            {{ browser.structure.value.kind === 'view' ? 'view' : 'table' }} is for.
          </p>
        </VCard>

        <VCard as="section" pad>
          <h2 class="mx-0 mt-0 mb-2 text-md font-bold">Columns</h2>
          <div class="overflow-x-auto">
            <VTable variant="structure">
              <template #head>
                <th scope="col">Name</th>
                <th scope="col">Type</th>
                <th scope="col">Null</th>
                <th scope="col">Default</th>
                <th scope="col">Description</th>
              </template>
              <tr v-for="column in browser.structure.value.columns" :key="column.name">
                <td class="name font-mono font-semibold whitespace-nowrap">{{ column.name }}</td>
                <td class="font-mono text-fg-soft">{{ column.type }}</td>
                <!-- One shape for both answers. A chip against a bare word
                       read as two different kinds of answer to one question,
                       and the constraint is what the column says, so it says
                       it in SQL. -->
                <!-- Nullable is the absence of a constraint, so it is the quieter
                       of the two. -->
                <td
                  class="font-mono whitespace-nowrap"
                  :class="column.nullable ? 'text-fg-muted' : 'text-fg'"
                >
                  {{ column.nullable ? 'NULL' : 'NOT NULL' }}
                </td>
                <td class="font-mono text-fg-soft">{{ text(column.default) ?? '—' }}</td>
                <td class="max-w-112">{{ text(column.comment) ?? '' }}</td>
              </tr>
            </VTable>
          </div>
        </VCard>

        <VCard as="section" pad>
          <h2 class="mx-0 mt-0 mb-2 text-md font-bold">Indexes</h2>
          <SqlScript
            v-if="browser.sections.value.indexes > 0"
            :code="browser.structure.value.indexes.join('\n\n')"
          />
          <p v-else class="text-sm text-fg-soft">
            {{
              browser.structure.value.kind === 'view'
                ? 'A view carries no index.'
                : 'This table has no index.'
            }}
          </p>
        </VCard>

        <VCard as="section" pad>
          <h2 class="mx-0 mt-0 mb-2 text-md font-bold">Permissions</h2>
          <SqlScript
            v-if="browser.sections.value.grants > 0"
            :code="browser.structure.value.grants.join('\n')"
          />
          <!-- PUBLIC and the owner are excluded as defaults rather than
               decisions, so an empty list does not mean nobody may read it. -->
          <p v-else class="text-sm text-fg-soft">
            Nobody has been granted anything on this beyond the defaults its owner and PUBLIC
            already hold.
          </p>
        </VCard>

        <TablePartitions
          v-if="partitions.partitioning.value"
          :partitioning="partitions.partitioning.value"
          :counts="partitions.counts.value"
          @count="countExactly"
        />
        <VBanner v-else-if="partitionsText" icon="circle-x">{{ partitionsText }}</VBanner>
      </div>
    </main>
  </div>
</template>
