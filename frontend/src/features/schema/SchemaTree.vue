<script setup lang="ts">
import { computed, ref } from 'vue'

import { type SelectOption, VBadge, VButton, VIcon, VInput, VLoader, VSelect } from '@/components'
import type { ColumnRef, TableRef } from '@/shared/api'

/**
 * Bytebase's schema tree, with the predecessor's column list underneath.
 *
 * Bytebase browses a searchable tree rather than dropdowns, which is what makes
 * a database with two hundred tables usable at all. The predecessor's contribution is
 * the column list with an Insert button: picking columns is the job, and a tree
 * that only reaches the table leaves it half done.
 *
 * A denied column is shown and disabled, never hidden. An operator has to be
 * able to see that a column exists and is refused.
 */
const props = defineProps<{
  databases: string[]
  tables: TableRef[]
  columns: ColumnRef[]
  database: string | null
  table: TableRef | null
  selected: Set<string>
  orderBy: string
  descending: boolean
  orderableColumns: string[]
  temporalColumns: string[]
  allSelected: boolean
  busy: boolean
  /**
   * What the Insert button is inserting into.
   *
   * `read` is the query console's: a SELECT, which is the only statement an
   * ORDER BY belongs to. `write` is FEAT-009's composer, where ordering is
   * meaningless and the button says which statement it will write. Two
   * components would be one tree maintained twice, and the tree is the part
   * that took the work.
   */
  mode?: 'read' | 'write'
  /** What the button says in write mode: UPDATE, DELETE or INSERT. */
  insertLabel?: string
  /**
   * Whether the button works with no column ticked. A DELETE names none.
   *
   * Phrased as the exception rather than as the requirement, because Vue casts
   * an absent Boolean prop to `false` rather than leaving it undefined: a
   * `needsColumns?: boolean` defaulting to true through `?? true` never sees a
   * nullish value to default, so it read as false everywhere it was not passed
   * and the Insert button stopped being disabled with nothing selected.
   */
  columnsOptional?: boolean
}>()

// Named apart from the props they default, so a template expression cannot be
// read as either one.
const treeMode = computed(() => props.mode ?? 'read')
const buttonLabel = computed(() => props.insertLabel ?? 'Insert')
const requiresColumns = computed(() => !props.columnsOptional)

const emit = defineEmits<{
  'choose-database': [value: string | null]
  'choose-table': [value: TableRef | null]
  toggle: [name: string]
  'toggle-all': []
  'order-by': [column: string]
  'toggle-direction': []
  insert: []
}>()

/** Room above the tree for a chart of the schema: orientation, never the control. */
defineSlots<{ chart?: () => unknown }>()

const filter = ref('')

/** Nothing, then the dates, then the rest — the order the parent computed. */
const orderOptions = computed<SelectOption[]>(() => [
  { value: '', label: 'nothing' },
  ...props.orderableColumns.map((name) => ({
    value: name,
    label: props.temporalColumns.includes(name) ? `${name} · date` : name,
  })),
])

function key(table: TableRef): string {
  return `${table.schema}.${table.name}`
}

function iconOf(table: TableRef): 'eye' | 'layers' | 'table' {
  if (table.kind === 'view') return 'eye'
  return table.kind === 'partitioned' ? 'layers' : 'table'
}

function isCurrent(table: TableRef): boolean {
  return props.table !== null && key(props.table) === key(table)
}

const matchingDatabases = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  if (needle === '') return props.databases
  // A database stays listed when one of its tables matches, or the filter would
  // hide the branch the operator is looking inside.
  return props.databases.filter(
    (name) =>
      name.toLowerCase().includes(needle) ||
      (name === props.database && matchingTables.value.length > 0),
  )
})

/*
 * An empty list is only an answer once the request for it has come back; until
 * then it is a question still in flight, and saying "nothing matches" would be
 * reporting an answer nobody has given.
 */
const databasesPending = computed(() => props.busy && props.databases.length === 0)
const tablesPending = computed(() => props.busy && props.tables.length === 0)

const matchingTables = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  if (needle === '') return props.tables
  return props.tables.filter((table) => key(table).toLowerCase().includes(needle))
})
</script>

<template>
  <aside class="tree flex min-h-0 w-tree flex-col border-r border-line bg-surface">
    <slot name="chart" />
    <div class="flex h-9 items-center justify-between px-3">
      <span class="hud text-fg-muted">Schema</span>
      <VIcon v-if="props.busy" name="loader" size="sm" spin class="text-fg-muted" />
    </div>

    <div class="relative px-3 pb-2.5">
      <VIcon
        name="search"
        size="sm"
        class="absolute top-1/2 left-5 -translate-y-1/2 text-fg-muted"
      />
      <VInput
        v-model="filter"
        size="sm"
        class="pl-7"
        type="search"
        placeholder="Search databases and tables"
      />
    </div>

    <div class="min-h-0 flex-initial overflow-y-auto border-b border-line pb-2">
      <div v-if="databasesPending" class="px-3 py-2">
        <VLoader size="inline" label="Loading databases" />
      </div>
      <p v-else-if="matchingDatabases.length === 0" class="m-0 px-3 py-2 text-xs text-fg-muted">
        Nothing matches “{{ filter }}”.
      </p>

      <template v-for="name in matchingDatabases" :key="name">
        <button
          type="button"
          class="node database flex h-7 w-full cursor-pointer items-center gap-1.5 border-0 bg-transparent px-2.5 text-left text-sm text-fg-soft [font:inherit] hover:bg-surface-sunken hover:text-fg"
          :class="{ open: name === props.database }"
          @click="emit('choose-database', name === props.database ? null : name)"
        >
          <VIcon
            :name="name === props.database ? 'chevron-down' : 'chevron-right'"
            size="sm"
            class="text-fg-muted"
          />
          <VIcon name="database" size="sm" class="text-fg-muted" />
          <span
            class="node-label truncate"
            :class="{ 'font-medium text-fg': name === props.database }"
            >{{ name }}</span
          >
        </button>

        <ul
          v-if="name === props.database"
          class="tables m-0 flex list-none flex-col gap-px px-1.5 py-0.5"
        >
          <li v-for="t in matchingTables" :key="key(t)">
            <!-- The open table is the target: lit amber, the one thing the
                 console is pointed at. -->
            <button
              type="button"
              class="node flex h-7 w-full cursor-pointer items-center gap-1.5 rounded-control border-0 pr-2 pl-7 text-left text-sm [font:inherit]"
              :class="
                isCurrent(t)
                  ? 'current bg-target-wash text-target shadow-glow-target'
                  : 'bg-transparent text-fg hover:bg-surface-sunken'
              "
              @click="emit('choose-table', t)"
            >
              <VIcon
                :name="iconOf(t)"
                size="sm"
                :class="isCurrent(t) ? 'text-target' : 'text-fg-muted'"
              />
              <span class="node-label truncate" :class="{ 'font-medium': isCurrent(t) }">{{
                t.name
              }}</span>
              <!-- Its partitions are not listed: the parent is what a statement
                   names, and the planner picks the partitions. FEAT-075. -->
              <VBadge
                v-if="t.kind === 'partitioned'"
                size="sm"
                class="partitioned flex-none whitespace-nowrap"
                :title="`${t.partitions ?? 0} partitions, not listed: query the parent`"
              >
                partitioned · {{ t.partitions ?? 0 }}
              </VBadge>
              <span
                class="node-schema ml-auto font-mono text-2xs"
                :class="isCurrent(t) ? 'text-target' : 'text-fg-muted'"
                >{{ t.schema }}</span
              >
            </button>
          </li>
          <li v-if="tablesPending" class="m-0 list-none px-3 py-2">
            <VLoader size="inline" label="Loading tables" />
          </li>
          <li
            v-else-if="matchingTables.length === 0"
            class="m-0 list-none px-3 py-2 text-xs text-fg-muted"
          >
            No tables match.
          </li>
        </ul>
      </template>
    </div>

    <!-- A floor under the columns, because the tree above them only shrinks
         once something claims the room: with a long table list and a basis of
         zero, this section was squeezed to nothing and took Insert with it. -->
    <section v-if="props.columns.length > 0" class="columns flex min-h-9/20 flex-1 flex-col">
      <div class="flex items-center justify-between gap-2 border-b border-line px-3 py-2">
        <label class="flex cursor-pointer items-center gap-1.75 text-xs text-fg-soft">
          <input
            type="checkbox"
            :checked="props.allSelected"
            :disabled="props.busy"
            @change="emit('toggle-all')"
          />
          <span>{{ props.selected.size }} of {{ props.columns.length }} columns</span>
        </label>
        <VButton
          variant="primary"
          size="sm"
          icon="plus"
          class="insert"
          :disabled="(requiresColumns && props.selected.size === 0) || props.busy"
          @click="emit('insert')"
        >
          {{ buttonLabel }}
        </VButton>
      </div>

      <!--
        Ordering is the clause an operator adds by hand more than any other, and
        what they almost always mean by it is "newest first" — so the dates sit
        at the top of the list rather than being hunted for among forty columns.
      -->
      <div
        v-if="treeMode === 'read'"
        class="order-by flex items-stretch gap-1.5 border-b border-line px-2.5 py-1.5 [&_input]:h-full [&_input]:font-mono"
      >
        <label for="order-column" class="flex items-center hud whitespace-nowrap text-fg-muted"
          >Order by</label
        >
        <VSelect
          id="order-column"
          size="sm"
          :model-value="props.orderBy"
          :options="orderOptions"
          :disabled="props.busy"
          @update:model-value="emit('order-by', $event)"
        />
        <span class="flex font-mono">
          <VButton
            size="sm"
            class="direction"
            :icon="props.descending ? 'sort-descending' : 'sort-ascending'"
            :disabled="props.orderBy === '' || props.busy"
            :title="props.descending ? 'Newest first' : 'Oldest first'"
            @click="emit('toggle-direction')"
          >
            {{ props.descending ? 'DESC' : 'ASC' }}
          </VButton>
        </span>
      </div>

      <ul class="column-list m-0 min-h-0 flex-1 list-none overflow-y-auto px-0 py-1">
        <li v-for="column in props.columns" :key="column.name">
          <label
            class="column flex h-7 items-center gap-2 px-3 hover:bg-surface-sunken"
            :class="column.denied ? 'denied cursor-not-allowed' : 'cursor-pointer'"
          >
            <input
              type="checkbox"
              :checked="props.selected.has(column.name)"
              :disabled="column.denied || props.busy"
              @change="emit('toggle', column.name)"
            />
            <span
              class="font-mono text-xs"
              :class="column.denied ? 'text-fg-muted line-through' : 'text-fg'"
              >{{ column.name }}</span
            >
            <span
              class="font-mono text-2xs text-fg-muted"
              :class="column.denied ? 'ml-0' : 'ml-auto'"
              >{{ column.type }}</span
            >
            <VBadge v-if="column.denied" tone="danger" size="sm" class="ml-auto">
              <VIcon name="lock" size="xs" />
              denied
            </VBadge>
          </label>
        </li>
      </ul>
    </section>
  </aside>
</template>
