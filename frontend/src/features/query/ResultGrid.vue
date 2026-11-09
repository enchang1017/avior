<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { rowWindow } from './rowWindow'
import { playScan } from './scan'
import type { QueryResult } from '@/shared/api'
import { VBadge, VButton, VIcon, VInput, VTable } from '@/components'

/**
 * Bytebase's result surface: a search box over the rows, the row count, and an
 * export action, above a dense grid with a sticky header.
 *
 * The filter is client-side and deliberately dumb — it matches the rendered text
 * of any cell. It is for finding a row in what came back, not for narrowing the
 * query: narrowing means running a different statement, and that has to go
 * through the guard.
 */
const props = defineProps<{ result: QueryResult }>()

const filter = ref('')

/*
 * Virtualised: only the rows in view, and a few either side, are in the DOM
 * (ADR 0009 P17). The spacers above and below are sized in rows, so the
 * scrollbar is still the length of the whole result.
 */
const scroller = ref<HTMLDivElement | null>(null)
const scan = ref<HTMLDivElement | null>(null)
const scrollTop = ref(0)
const viewport = ref(0)
const rowHeight = ref(32)
let observer: ResizeObserver | null = null

function measure(): void {
  const element = scroller.value
  if (element === null) return
  viewport.value = element.clientHeight
  const row = element.querySelector<HTMLElement>('tbody tr[data-row]')
  if (row !== null && row.offsetHeight > 0) rowHeight.value = row.offsetHeight
}

function onScroll(): void {
  scrollTop.value = scroller.value?.scrollTop ?? 0
}

const rows = computed(() =>
  props.result.rows.map((cells, index) => ({
    ordinal: index + 1,
    cells,
    text: cells.map(render).join(' ').toLowerCase(),
  })),
)

const shown = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  if (needle === '') return rows.value
  return rows.value.filter((row) => row.text.includes(needle))
})

const windowed = computed(() =>
  rowWindow(shown.value.length, scrollTop.value, viewport.value, rowHeight.value),
)
const drawn = computed(() => shown.value.slice(windowed.value.start, windowed.value.end))
const spacers = computed(() => ({
  '--rows-before': `${windowed.value.before}px`,
  '--rows-after': `${windowed.value.after}px`,
}))

/*
 * A new result's visible rows rise into place one after another, once: only
 * the rows drawn when it arrives, never rows scrolled to later, and only
 * transform and opacity. The set is kept until the next result: taking the
 * class off a row whose animation has ended would restyle it for nothing
 * (ADR 0009 P13 measured exactly that).
 */
const rising = ref(new Set<number>())

function rise(): void {
  rising.value = new Set(drawn.value.map((row) => row.ordinal))
}

// Once per result, after its rows are on screen: the next frame, not this one.
watch(
  () => props.result,
  () => requestAnimationFrame(() => playScan(scan.value)),
  { immediate: true, flush: 'post' },
)

watch(() => props.result.auditId, rise, { immediate: true })

/*
 * Measured in a task of its own, never the one that mounts the rows. Reading
 * the scroller's size there forced a style and layout pass into the render
 * task, and the wider window it asked for forced a second: one task, two
 * passes, and ADR 0009's longest (P11). The first window, sized for no
 * viewport, already covers the rows in view.
 */
onMounted(() => {
  if (typeof ResizeObserver !== 'function' || scroller.value === null) {
    measure()
    return
  }
  observer = new ResizeObserver(() => setTimeout(measure))
  observer.observe(scroller.value)
})
onBeforeUnmount(() => observer?.disconnect())

function render(cell: unknown): string {
  if (cell === null || cell === undefined) return 'NULL'
  if (typeof cell === 'object') return JSON.stringify(cell)
  if (typeof cell === 'string') return cell
  if (typeof cell === 'number' || typeof cell === 'boolean') return cell.toString()
  return JSON.stringify(cell) ?? ''
}

/**
 * The browser writes out what it already holds. Asking the server to format it
 * would be a second read of production for data the operator is looking at;
 * FEAT-010 covers the audited server-side export with its own limits.
 */
function downloadCsv(): void {
  const newline = String.fromCharCode(13, 10)
  const header = props.result.columns.map((column) => quote(column.name)).join(',')
  const body = props.result.rows.map((cells) => cells.map((cell) => quote(render(cell))).join(','))
  const blob = new Blob([[header, ...body].join(newline)], { type: 'text/csv;charset=utf-8' })

  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = `avior-${props.result.auditId}.csv`
  link.click()
  URL.revokeObjectURL(link.href)
}

function quote(value: string): string {
  return `"${value.replaceAll('"', '""')}"`
}
</script>

<template>
  <section class="result relative flex min-h-0 flex-1 flex-col bg-surface">
    <div class="flex h-10 flex-none items-center gap-3 border-b border-line px-3">
      <div class="relative w-56">
        <VIcon
          name="search"
          size="sm"
          class="absolute top-1/2 left-2 -translate-y-1/2 text-fg-muted"
        />
        <VInput
          v-model="filter"
          size="sm"
          class="pl-7"
          type="search"
          placeholder="Search results"
        />
      </div>
      <span class="count hud text-fg-muted">
        {{ shown.length === rows.length ? rows.length : `${shown.length} of ${rows.length}` }}
        rows
      </span>
      <VBadge v-if="props.result.truncated" tone="danger" size="sm">truncated</VBadge>
      <div class="flex-1" />
      <VButton size="sm" variant="bare" icon="download" @click="downloadCsv">Export</VButton>
    </div>

    <div
      ref="scan"
      class="scan pointer-events-none absolute inset-x-0 top-18 z-2 h-8 opacity-0 scan-light"
      aria-hidden="true"
    />

    <div
      ref="scroller"
      class="min-h-0 flex-1 overflow-auto"
      :style="spacers"
      @scroll.passive="onScroll"
    >
      <VTable variant="grid">
        <!-- Without a caption Chromium treats a CSS-styled table as layout and
             strips the header roles, leaving the grid unreadable to a screen
             reader. -->
        <template #caption>Query result: {{ props.result.rowCount }} rows</template>
        <template #head>
          <th scope="col" class="w-px text-right ordinal">#</th>
          <th v-for="column in props.result.columns" :key="column.name" scope="col">
            {{ column.name }}
            <span class="text-fg-faint">·</span>
            <span class="type text-fg-faint">{{ column.type }}</span>
          </th>
        </template>
        <tr v-if="windowed.before > 0" class="spacer" aria-hidden="true">
          <td :colspan="props.result.columns.length + 1" class="h-(--rows-before) border-0 p-0" />
        </tr>
        <tr
          v-for="(row, index) in drawn"
          :key="row.ordinal"
          data-row
          :class="
            rising.has(row.ordinal)
              ? 'rise animate-rise [animation-delay:calc(var(--row)*var(--stagger))]'
              : ''
          "
          :style="rising.has(row.ordinal) ? { '--row': index } : undefined"
        >
          <th scope="row" class="w-px text-right text-2xs text-fg-muted ordinal">
            {{ row.ordinal }}
          </th>
          <td
            v-for="(cell, i) in row.cells"
            :key="i"
            :class="{ 'nul text-fg-muted italic': cell === null }"
          >
            {{ render(cell) }}
          </td>
        </tr>
        <tr v-if="windowed.after > 0" class="spacer" aria-hidden="true">
          <td :colspan="props.result.columns.length + 1" class="h-(--rows-after) border-0 p-0" />
        </tr>
      </VTable>
      <p v-if="shown.length === 0" class="m-0 px-3 py-4 text-sm text-fg-muted">
        No row matches “{{ filter }}”.
      </p>
    </div>
  </section>
</template>
