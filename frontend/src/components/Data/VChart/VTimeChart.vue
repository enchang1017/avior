<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { useDrawIn } from './drawIn'
import { describeSeries, frame, isBlank, nearestIndex, toChartX, type Box } from './geometry'
import { readingsAt, timeLayout } from './timeLayout'
import { chartDot, chartFill, chartStroke, chartSwatch } from './tone'
import type { Series, TimeChartMode, TimeChartScale } from './types'

/**
 * A series over time, as lines or as stacked areas, in hand-rolled SVG
 * (FEAT-072; ADR 0009 P7 leaves no room for a chart library).
 *
 * The drawing is measured in the host's own pixels, so text is never
 * stretched: a ResizeObserver hands the width to one computed layout, and the
 * pointer only ever moves an index found by bisection.
 */
const props = withDefaults(
  defineProps<{
    series: readonly Series[]
    mode: TimeChartMode
    caption: string
    formatY: (value: number) => string
    formatX: (epochMs: number) => string
    loading?: boolean
    emptyText?: string
    height?: number
    scale?: TimeChartScale
  }>(),
  { loading: false, emptyText: 'No data in this range.', height: 180, scale: 'linear' },
)

const emit = defineEmits<{ select: [x: number] }>()

const host = ref<HTMLElement | null>(null)
const width = ref(600)
const active = ref<number | null>(null)
const byKeyboard = ref(false)
let observer: ResizeObserver | null = null
let strip: DOMRect | null = null

const blank = computed(() => isBlank(props.series))
const box = computed<Box>(() => ({ width: width.value, height: props.height, frame }))
const layout = computed(() =>
  timeLayout(
    props.series,
    props.mode,
    box.value,
    { formatY: props.formatY, formatX: props.formatX },
    props.scale,
  ),
)
const summary = computed(() => describeSeries(props.series, props.formatY, props.formatX))
const { playing, done } = useDrawIn(() => !props.loading && !blank.value)

const activeX = computed(() =>
  active.value === null ? null : (layout.value.pixelsX[active.value] ?? null),
)
const readings = computed(() =>
  active.value === null ? [] : readingsAt(props.series, props.mode, active.value, props.formatY),
)
const activeLabel = computed(() => {
  const x = active.value === null ? undefined : layout.value.xs[active.value]
  return x === undefined ? '' : props.formatX(x)
})
const spoken = computed(() =>
  byKeyboard.value && active.value !== null
    ? `${activeLabel.value}: ${readings.value.map((r) => `${r.label} ${r.text}`).join(', ')}`
    : '',
)
const tooltipStyle = computed(() => {
  const x = activeX.value ?? 0
  const flip = x > width.value / 2
  const shift = flip
    ? `calc(${x}px - 100% - var(--spacing) * 3)`
    : `calc(${x}px + var(--spacing) * 3)`
  return { transform: `translate(${shift}, ${frame.top}px)` }
})
const heightStyle = computed(() => ({ height: `${props.height}px` }))

function measure(next: number): void {
  const rounded = Math.round(next)
  if (rounded > 0 && rounded !== width.value) width.value = rounded
}

onMounted(() => {
  if (host.value === null) return
  if (typeof ResizeObserver === 'undefined') {
    measure(host.value.clientWidth)
    return
  }
  // The observer's first report comes after the browser's own layout. Reading
  // clientWidth here instead forced a layout of the whole freshly mounted
  // overview inside the mount, the long task FEAT-072 AC-13 caught.
  observer = new ResizeObserver((entries) => measure(entries[0]?.contentRect.width ?? 0))
  observer.observe(host.value)
})
onBeforeUnmount(() => observer?.disconnect())

function setActive(index: number | null, keyboard: boolean): void {
  byKeyboard.value = keyboard
  if (index !== active.value) active.value = index
}

function enter(event: PointerEvent): void {
  strip = (event.currentTarget as HTMLElement).getBoundingClientRect()
}

function move(event: PointerEvent): void {
  strip ??= (event.currentTarget as HTMLElement).getBoundingClientRect()
  const x = toChartX(event.clientX, strip.left, strip.width, width.value)
  const index = nearestIndex(layout.value.pixelsX, x)
  setActive(index < 0 ? null : index, false)
}

function leave(): void {
  strip = null
  if (!byKeyboard.value) setActive(null, false)
}

function choose(): void {
  const x = active.value === null ? undefined : layout.value.xs[active.value]
  if (x !== undefined) emit('select', x)
}

const steps: Record<string, (index: number, last: number) => number> = {
  ArrowLeft: (index) => Math.max(0, index - 1),
  ArrowRight: (index, last) => Math.min(last, index + 1),
  Home: () => 0,
  End: (_, last) => last,
}

function key(event: KeyboardEvent): void {
  const last = layout.value.xs.length - 1
  if (last < 0) return
  if (event.key === 'Enter') {
    event.preventDefault()
    choose()
    return
  }
  const step = steps[event.key]
  if (step === undefined) return
  event.preventDefault()
  setActive(step(active.value ?? last, last), true)
}

function focus(): void {
  const last = layout.value.xs.length - 1
  if (active.value === null && last >= 0) setActive(last, true)
}

function blur(): void {
  setActive(null, false)
}
</script>

<template>
  <figure ref="host" class="m-0 flex min-w-0 flex-col gap-2" :data-mode="mode">
    <figcaption class="flex flex-col">
      <span class="eyebrow">{{ caption }}</span>
      <span class="sr-only" data-testid="chart-summary">{{ summary }}</span>
      <span v-if="layout.logarithmic" class="text-2xs text-fg-muted" data-testid="chart-scale">
        Logarithmic scale: each gridline is ten times the one below
      </span>
    </figcaption>
    <div
      v-if="loading"
      class="rounded-card bg-surface-sunken"
      :style="heightStyle"
      role="status"
      :aria-label="`Loading ${caption}`"
      data-testid="chart-skeleton"
    />
    <p
      v-else-if="blank"
      class="m-0 flex items-center justify-center rounded-card bg-surface-sunken text-sm text-fg-muted"
      :style="heightStyle"
      role="status"
      data-testid="chart-empty"
    >
      {{ emptyText }}
    </p>
    <div v-else class="relative">
      <div
        class="plot cursor-crosshair rounded-control"
        tabindex="0"
        role="group"
        aria-roledescription="chart"
        :aria-label="`${caption}. Arrow keys move between buckets, Enter opens one.`"
        data-testid="chart-plot"
        @pointerenter="enter"
        @pointermove="move"
        @pointerleave="leave"
        @click="choose"
        @keydown="key"
        @focus="focus"
        @blur="blur"
      >
        <svg
          :viewBox="`0 0 ${box.width} ${box.height}`"
          class="block h-auto w-full overflow-visible font-mono text-3xs select-none"
          aria-hidden="true"
        >
          <g class="stroke-line">
            <line
              v-for="tick in layout.yTicks"
              :key="tick.value"
              :x1="frame.left"
              :x2="box.width - frame.right"
              :y1="tick.at"
              :y2="tick.at"
            />
          </g>
          <g class="fill-fg-muted">
            <text
              v-for="tick in layout.yTicks"
              :key="tick.value"
              :x="frame.left - 6"
              :y="tick.at"
              text-anchor="end"
              dominant-baseline="middle"
            >
              {{ tick.label }}
            </text>
            <text
              v-for="(tick, index) in layout.xTicks"
              :key="tick.value"
              :x="tick.at"
              :y="box.height - 6"
              :text-anchor="
                index === 0 ? 'start' : index === layout.xTicks.length - 1 ? 'end' : 'middle'
              "
            >
              {{ tick.label }}
            </text>
          </g>
          <g
            class="marks"
            :class="{ 'draw-in': playing }"
            data-testid="chart-marks"
            @animationend="done"
          >
            <template v-for="one in layout.drawn" :key="one.id">
              <path v-if="one.area" :d="one.area" :class="chartFill[one.tone]" />
              <path
                :d="one.line"
                class="fill-none"
                :class="chartStroke[one.tone]"
                stroke-width="1.5"
                stroke-linejoin="round"
                stroke-linecap="round"
                :data-series="one.id"
              />
            </template>
          </g>
          <g v-if="activeX !== null" data-testid="chart-crosshair">
            <line
              :x1="activeX"
              :x2="activeX"
              :y1="frame.top"
              :y2="box.height - frame.bottom"
              class="stroke-line-strong"
            />
            <template v-for="one in layout.drawn" :key="one.id">
              <circle
                v-if="active !== null && (one.tops[active] ?? null) !== null"
                :cx="activeX"
                :cy="one.tops[active] ?? 0"
                r="3"
                :class="chartDot[one.tone]"
              />
            </template>
          </g>
        </svg>
      </div>
      <div
        v-if="active !== null"
        class="pointer-events-none absolute top-0 left-0 z-10 min-w-40 rounded-control border border-line-strong bg-surface-raised px-2 py-1.5 font-mono text-2xs shadow-md"
        :style="tooltipStyle"
        aria-hidden="true"
        data-testid="chart-tooltip"
      >
        <p class="m-0 mb-1 text-fg-muted">{{ activeLabel }}</p>
        <ul class="m-0 flex list-none flex-col gap-0.5 p-0">
          <li v-for="reading in readings" :key="reading.id" class="flex items-center gap-2">
            <span class="size-2 flex-none rounded-pill" :class="chartSwatch[reading.tone]" />
            <span class="min-w-0 flex-1 truncate text-fg-soft">{{ reading.label }}</span>
            <span class="text-fg tabular-nums">{{ reading.text }}</span>
          </li>
        </ul>
      </div>
      <p class="sr-only" aria-live="polite" data-testid="chart-spoken">{{ spoken }}</p>
    </div>
    <ul
      v-if="series.length > 0"
      class="m-0 flex list-none flex-wrap gap-x-3 gap-y-1 p-0 font-mono text-2xs text-fg-soft"
      data-testid="chart-legend"
    >
      <li v-for="one in series" :key="one.id" class="flex items-center gap-1.5">
        <span class="size-2 rounded-pill" :class="chartSwatch[one.tone]" aria-hidden="true" />
        {{ one.label }}
      </li>
    </ul>
  </figure>
</template>

<style scoped>
.draw-in {
  transform-box: fill-box;
  transform-origin: left;
  animation: var(--animate-sweep-in);
}
</style>
