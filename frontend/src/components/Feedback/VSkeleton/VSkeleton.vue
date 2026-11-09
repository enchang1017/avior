<script setup lang="ts">
import { computed } from 'vue'

import { tableVariants, type TableVariant } from '../../Data/VTable/variants'

/**
 * The shape of what is coming, while it is still coming.
 *
 * A spinner in the toolbar and an empty page below it is what this replaces. It
 * says something is happening and nothing about what — so a slow read and a
 * page that rendered nothing look the same, and the operator's next move is to
 * reload a request that was already in flight.
 *
 * Blocks in the proportions of the thing being waited for, so the layout does
 * not jump when it arrives.
 *
 * `variant="table"` is a list waiting for its rows: a head row and body rows
 * drawn by the same table variant as the list, so each row is the height of the
 * row that replaces it. `columns` is one bar width per column, and `headings`,
 * when given, are the list's own column names; `rowLines` stacks that many
 * lines in the first cell, for a list whose rows wrap. It holds still: a pulse per cell
 * would be dozens of infinite animations on a page that is only waiting.
 *
 * `variant="list"` is a stacked list waiting for its items: `rows` items of
 * `rowLines` lines each, ruled the way the list's items are.
 *
 * Announced once rather than per block. A screen reader reading out nine
 * identical placeholders is worse than the blank page.
 */
const props = withDefaults(
  defineProps<{
    lines?: number
    label?: string
    variant?: 'lines' | 'table' | 'list'
    columns?: string[]
    headings?: string[]
    rows?: number
    table?: TableVariant
    /** The rows hold small controls, so each is a control's height, not a line's. */
    controls?: boolean
    rowLines?: number
  }>(),
  {
    lines: 3,
    label: 'Reading',
    variant: 'lines',
    columns: () => ['w-31/50', 'w-full', 'w-21/25'],
    headings: () => [],
    rows: 6,
    table: 'list',
    controls: false,
    rowLines: 1,
  },
)

const widths = ['w-31/50', 'w-full', 'w-21/25']

const tableClasses = computed(() => tableVariants[props.table])
const cellHeight = computed(() => (props.controls ? 'h-control-sm' : 'h-lh'))
</script>

<template>
  <div
    v-if="props.variant === 'table'"
    role="status"
    :aria-label="props.label"
    data-skeleton="table"
  >
    <table :class="tableClasses" aria-hidden="true">
      <thead>
        <tr>
          <th v-for="column in props.columns.length" :key="column" scope="col">
            <template v-if="props.headings[column - 1] !== undefined">
              {{ props.headings[column - 1] }}
            </template>
            <span v-else class="flex h-lh items-center">
              <span class="block h-2 w-1/2 rounded-sm bg-line" />
            </span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in props.rows" :key="row" data-skeleton-row>
          <td v-for="(width, column) in props.columns" :key="column">
            <span
              v-for="line in column === 0 ? props.rowLines : 1"
              :key="line"
              class="flex items-center"
              :class="cellHeight"
            >
              <span class="block h-3 rounded-sm bg-line" :class="width" />
            </span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <div
    v-else-if="props.variant === 'list'"
    role="status"
    :aria-label="props.label"
    data-skeleton="list"
  >
    <div
      v-for="row in props.rows"
      :key="row"
      class="flex flex-col gap-1 border-b border-line px-4 py-2.5"
      data-skeleton-row
    >
      <span v-for="line in props.rowLines" :key="line" class="flex h-lh items-center">
        <span class="block h-3 rounded-sm bg-line" :class="widths[(row + line) % 3]" />
      </span>
    </div>
  </div>
  <!-- Uneven widths, because a stack of identical bars reads as a component
       that failed to lay out rather than as text that has not arrived. A pulse
       is decoration: somebody who asked for less motion still sees the blocks,
       and only the animation goes. -->
  <div v-else class="flex flex-col gap-3 py-4" role="status" :aria-label="props.label">
    <div
      v-for="line in props.lines"
      :key="line"
      class="shimmer block h-4 rounded-sm bg-line motion-reduce:opacity-50"
      :class="widths[line % 3]"
    />
  </div>
</template>
