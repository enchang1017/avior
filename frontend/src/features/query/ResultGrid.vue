<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { cellValue } from './cellValue'
import { rowWindow } from './rowWindow'
import { playScan } from './scan'
import type { QueryResult } from '@/shared/api'
import { VBadge, VButton, VDialog, VIcon, VInput, VTable } from '@/components'

/**
 * Bytebase's result surface: a search box over the rows, the row count, and an
 * export action, above a dense grid with a sticky header.
 *
 * The filter is client-side and deliberately dumb — it matches the rendered text
 * of any cell. It is for finding a row in what came back, not for narrowing the
 * query: narrowing means running a different statement, and that has to go
 * through the guard.
 */
const props = withDefaults(
  defineProps<{ result: QueryResult; animate?: boolean; offset?: number }>(),
  {
    animate: true,
    offset: 0,
  },
)

const filter = ref('')
const cellDetail = ref<{ title: string; value: ReturnType<typeof cellValue> } | null>(null)
const copyStatus = ref('')
function showCell(value: ReturnType<typeof cellValue>, column: number): void {
  if (!value.copyable) return
  cellDetail.value = { title: props.result.columns[column]?.name ?? 'Cell value', value }
  copyStatus.value = ''
}
function focusCell(event: MouseEvent): void {
  if (event.target instanceof Element && event.target.closest('button')) return
  ;(event.currentTarget as HTMLElement).focus({ preventScroll: true })
}
function cellKey(event: KeyboardEvent, value: ReturnType<typeof cellValue>, column: number): void {
  if (event.target !== event.currentTarget) return
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'c') {
    // Native text selections keep their normal copy behavior.
    if (window.getSelection()?.toString()) return
    event.preventDefault()
    void copyValue(value)
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    showCell(value, column)
    return
  }
  const cell = event.currentTarget as HTMLTableCellElement
  const row = cell.parentElement as HTMLTableRowElement
  let next: Element | null | undefined
  if (event.key === 'ArrowLeft') next = cell.previousElementSibling
  else if (event.key === 'ArrowRight') next = cell.nextElementSibling
  else if (event.key === 'ArrowUp') next = row.previousElementSibling?.children[cell.cellIndex]
  else if (event.key === 'ArrowDown') next = row.nextElementSibling?.children[cell.cellIndex]
  else return
  event.preventDefault()
  if (
    next instanceof HTMLElement &&
    next.tagName === 'TD' &&
    next.closest('tr')?.hasAttribute('data-row')
  ) {
    next.focus()
    next.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }
}

async function copyValue(value: ReturnType<typeof cellValue>): Promise<void> {
  copyStatus.value = ''
  if (!value.copyable) {
    copyStatus.value =
      value.kind === 'Masked' ? 'Masked values cannot be copied.' : 'NULL has no value to copy.'
    return
  }
  try {
    await navigator.clipboard.writeText(value.raw)
    copyStatus.value = value.kind === 'Empty string' ? 'Empty string copied.' : 'Copied.'
  } catch {
    copyStatus.value = 'Clipboard unavailable. Select the value and copy it.'
  }
}

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
  props.result.rows.map((values, index) => {
    const cells = values.map((cell, index) => cellValue(cell, props.result.columns[index]))
    return {
      ordinal: index + 1 + props.offset,
      cells,
      text: cells
        .map((cell) => (cell.kind === 'NULL' ? 'NULL' : cell.raw))
        .join(' ')
        .toLowerCase(),
    }
  }),
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
  () => {
    cellDetail.value = null
    copyStatus.value = ''
    if (props.animate) requestAnimationFrame(() => playScan(scan.value))
  },
  { immediate: true, flush: 'post' },
)

watch(
  () => props.result.auditId,
  () => {
    if (props.animate) rise()
  },
  { immediate: true },
)

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

/**
 * The browser writes out what it already holds. Asking the server to format it
 * would be a second read of production for data the operator is looking at;
 * FEAT-010 covers the audited server-side export with its own limits.
 */
function downloadCsv(): void {
  const newline = String.fromCharCode(13, 10)
  const header = props.result.columns.map((column) => quote(column.name)).join(',')
  const body = props.result.rows.map((cells) =>
    cells
      .map((cell, index) =>
        quote(
          cellValue(cell, props.result.columns[index]).kind === 'NULL'
            ? 'NULL'
            : cellValue(cell, props.result.columns[index]).raw,
        ),
      )
      .join(','),
  )
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
      <span :class="copyStatus ? 'text-xs text-fg-muted' : 'sr-only'" role="status">{{
        copyStatus
      }}</span>
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
            :class="{
              'nul text-fg-muted italic': cell.special && cell.kind !== 'Masked',
              'pr-20': cell.expandable,
            }"
            class="relative cursor-text focus:outline-2 focus:-outline-offset-2 focus:outline-primary"
            :tabindex="row.ordinal === drawn[0]?.ordinal && i === 0 ? 0 : -1"
            v-bind="cell.special ? { 'aria-label': cell.kind } : {}"
            :title="
              cell.copyable
                ? 'Use arrow keys to move, Ctrl/Cmd+C to copy, or Enter to inspect.'
                : cell.kind === 'Masked'
                  ? 'Masked value. Inspection and copying are unavailable.'
                  : 'NULL: no stored value. Use arrow keys to move.'
            "
            @click="focusCell"
            @keydown="cellKey($event, cell, i)"
          >
            {{ cell.preview }}
            <VButton
              v-if="cell.expandable"
              variant="bare"
              size="sm"
              class="absolute top-0 right-0"
              :aria-label="'Expand ' + (props.result.columns[i]?.name ?? 'cell')"
              @click.stop="showCell(cell, i)"
            >
              <span>Expand</span>
            </VButton>
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
    <VDialog
      :open="cellDetail !== null"
      :title="cellDetail?.title ?? 'Cell value'"
      motion="fade"
      @close="cellDetail = null"
    >
      <p v-if="cellDetail?.value.kind !== 'Value'" class="mt-0 text-xs text-fg-muted">
        {{ cellDetail?.value.kind }}
      </p>
      <pre class="m-0 overflow-auto font-mono text-sm break-words whitespace-pre-wrap">{{
        cellDetail?.value.detail
      }}</pre>
      <template #footer>
        <span class="text-xs text-fg-muted" role="status">{{ copyStatus }}</span
        ><VButton @click="cellDetail && copyValue(cellDetail.value)">Copy value</VButton>
      </template>
    </VDialog>
  </section>
</template>
