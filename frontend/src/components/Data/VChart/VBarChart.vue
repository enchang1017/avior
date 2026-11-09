<script setup lang="ts">
import { computed, ref } from 'vue'

import { useDrawIn } from './drawIn'
import { barFractions, barTotal, describeBars } from './geometry'
import { chartSwatch } from './tone'
import type { BarDatum, ChartTone } from './types'

/**
 * Ranked horizontal bars, one button per row, in the order given.
 *
 * A bar's length is a scaleX on a full-width strip, so drawing it in is the
 * same transform animated from zero: nothing is laid out twice.
 */
const props = withDefaults(
  defineProps<{
    data: readonly BarDatum[]
    caption: string
    formatValue: (value: number) => string
    loading?: boolean
    emptyText?: string
    legend?: readonly { id: string; label: string; tone: ChartTone }[]
  }>(),
  { loading: false, emptyText: 'No data in this range.', legend: () => [] },
)

const emit = defineEmits<{ select: [id: string] }>()

const active = ref<string | null>(null)
const fractions = computed(() => barFractions(props.data))
const summary = computed(() => describeBars(props.data, props.formatValue))
const legendItems = computed(() => props.legend ?? [])
const blank = computed(() => props.data.length === 0)
const { playing, done } = useDrawIn(() => !props.loading && !blank.value)

function spokenSegments(datum: BarDatum): string {
  return (datum.segments ?? [])
    .map((segment) => `${segment.label} ${props.formatValue(segment.value)}`)
    .join(', ')
}

function show(id: string): void {
  active.value = id
}

function hide(id: string): void {
  if (active.value === id) active.value = null
}
</script>

<template>
  <figure class="m-0 flex min-w-0 flex-col gap-2">
    <figcaption class="flex flex-col">
      <span class="eyebrow">{{ caption }}</span>
      <span class="sr-only" data-testid="chart-summary">{{ summary }}</span>
    </figcaption>
    <div
      v-if="loading"
      class="flex flex-col gap-3 py-1"
      role="status"
      :aria-label="`Loading ${caption}`"
      data-testid="chart-skeleton"
    >
      <div v-for="row in 5" :key="row" class="h-4 rounded-sm bg-surface-sunken" />
    </div>
    <p
      v-else-if="blank"
      class="m-0 rounded-card bg-surface-sunken px-4 py-8 text-center text-sm text-fg-muted"
      role="status"
      data-testid="chart-empty"
    >
      {{ emptyText }}
    </p>
    <ul v-else class="m-0 flex list-none flex-col gap-0.5 p-0" data-testid="chart-bars">
      <li v-for="(datum, index) in data" :key="datum.id" class="relative">
        <button
          type="button"
          class="bar-row grid w-full cursor-pointer grid-cols-[minmax(0,--spacing(40))_minmax(0,1fr)_auto] items-center gap-x-3 rounded-control px-2 py-1 text-left font-mono text-2xs ease-colours hover:bg-surface-sunken focus-visible:bg-surface-sunken"
          :data-id="datum.id"
          @click="emit('select', datum.id)"
          @mouseenter="show(datum.id)"
          @mouseleave="hide(datum.id)"
          @focus="show(datum.id)"
          @blur="hide(datum.id)"
        >
          <span class="flex min-w-0 flex-col">
            <span class="truncate text-fg-soft" :title="datum.label">{{ datum.label }}</span>
            <span v-if="datum.detail" class="truncate text-3xs text-fg-muted">
              {{ datum.detail }}
            </span>
          </span>
          <span class="block h-2 overflow-hidden rounded-xs bg-surface-sunken" aria-hidden="true">
            <span
              class="bar-fill flex h-full origin-left"
              :class="{ 'draw-in': playing }"
              :style="{ transform: `scaleX(${fractions[index] ?? 0})` }"
              @animationend="done"
            >
              <template v-if="datum.segments && datum.segments.length > 0">
                <span
                  v-for="segment in datum.segments"
                  :key="segment.id"
                  class="h-full basis-0"
                  :class="chartSwatch[segment.tone]"
                  :style="{ flexGrow: Math.max(0, segment.value) }"
                />
              </template>
              <span v-else class="h-full flex-1 bg-primary" />
            </span>
          </span>
          <span class="text-fg tabular-nums">{{ formatValue(barTotal(datum)) }}</span>
          <span v-if="datum.segments && datum.segments.length > 0" class="sr-only">
            {{ spokenSegments(datum) }}
          </span>
        </button>
        <div
          v-if="active === datum.id"
          class="pointer-events-none absolute right-0 bottom-full z-10 mb-1 min-w-40 rounded-control border border-line-strong bg-surface-raised px-2 py-1.5 font-mono text-2xs shadow-md"
          aria-hidden="true"
          data-testid="chart-tooltip"
        >
          <p class="m-0 flex gap-3 text-fg">
            <span class="min-w-0 flex-1 break-all">{{ datum.label }}</span>
            <span class="tabular-nums">{{ formatValue(barTotal(datum)) }}</span>
          </p>
          <ul
            v-if="datum.segments && datum.segments.length > 0"
            class="m-0 mt-1 flex list-none flex-col gap-0.5 p-0"
          >
            <li v-for="segment in datum.segments" :key="segment.id" class="flex items-center gap-2">
              <span class="size-2 flex-none rounded-pill" :class="chartSwatch[segment.tone]" />
              <span class="min-w-0 flex-1 truncate text-fg-soft">{{ segment.label }}</span>
              <span class="text-fg tabular-nums">{{ formatValue(segment.value) }}</span>
            </li>
          </ul>
          <p v-if="datum.detail" class="m-0 mt-1 text-fg-muted">{{ datum.detail }}</p>
        </div>
      </li>
    </ul>
    <ul
      v-if="legendItems.length > 0"
      class="m-0 flex list-none flex-wrap gap-x-3 gap-y-1 p-0 font-mono text-2xs text-fg-soft"
      data-testid="chart-legend"
    >
      <li v-for="item in legendItems" :key="item.id" class="flex items-center gap-1.5">
        <span class="size-2 rounded-pill" :class="chartSwatch[item.tone]" aria-hidden="true" />
        {{ item.label }}
      </li>
    </ul>
  </figure>
</template>

<style scoped>
.draw-in {
  animation: var(--animate-sweep-in);
}
</style>
