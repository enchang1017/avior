<script setup lang="ts">
import { computed, ref } from 'vue'

import {
  bars,
  bucketAt,
  dayTicks,
  skyHeight,
  skyWidth,
  slotWidth,
  spanOf,
  totals,
  type AuditBucket,
  type Span,
} from './sky'
import { useCountUp } from './countUp'

const props = defineProps<{
  buckets: AuditBucket[]
  /** The brushed span, owned by the caller so it can come from the URL. */
  span: Span | null
  label?: string
}>()

const emit = defineEmits<{ brush: [span: Span]; clear: [] }>()

const labelBand = 18
const viewBox = `0 0 ${skyWidth} ${skyHeight + labelBand}`

const drawnBars = computed(() => bars(props.buckets))
const ticks = computed(() => dayTicks(props.buckets))
const counted = computed(() => totals(props.buckets))
const allowedShown = useCountUp(() => counted.value.allowed)
const deniedShown = useCountUp(() => counted.value.denied)
const slot = computed(() => slotWidth(props.buckets.length))

const anchor = ref<number | null>(null)
const draft = ref<Span | null>(null)
let strip: { left: number; width: number } | null = null

const shown = computed(() => draft.value ?? props.span)

function bucketUnder(event: PointerEvent): number {
  if (strip === null || strip.width === 0) return 0
  return bucketAt((event.clientX - strip.left) / strip.width, props.buckets.length)
}

function start(event: PointerEvent): void {
  if (props.buckets.length === 0 || event.button !== 0) return
  const svg = event.currentTarget as SVGSVGElement
  const box = svg.getBoundingClientRect()
  strip = { left: box.left, width: box.width }
  svg.setPointerCapture(event.pointerId)
  anchor.value = bucketUnder(event)
  draft.value = { first: anchor.value, last: anchor.value }
}

function move(event: PointerEvent): void {
  if (anchor.value === null) return
  const next = spanOf(anchor.value, bucketUnder(event))
  if (next.first !== draft.value?.first || next.last !== draft.value.last) draft.value = next
}

function finish(): void {
  const span = draft.value
  anchor.value = null
  draft.value = null
  strip = null
  if (span !== null) emit('brush', span)
}

function cancel(): void {
  anchor.value = null
  draft.value = null
  strip = null
}
</script>

<template>
  <section class="flex flex-col gap-1.5" data-testid="audit-sky">
    <header class="flex items-center gap-4 font-mono text-3xs tracking-hud text-fg-muted uppercase">
      <span>{{ label ?? 'Sky · last 7 days · hourly' }}</span>
      <span class="ml-auto flex items-center gap-1.5">
        <span class="size-1.5 rounded-pill bg-fg" aria-hidden="true" />
        Allowed <span class="allowed-total text-fg-soft tabular-nums">{{ allowedShown }}</span>
      </span>
      <span class="flex items-center gap-1.5">
        <span class="size-1.5 rounded-pill bg-danger" aria-hidden="true" />
        Refused <span class="denied-total text-fg-soft tabular-nums">{{ deniedShown }}</span>
      </span>
      <slot name="window" />
    </header>
    <svg
      :viewBox="viewBox"
      class="block h-auto w-full cursor-crosshair touch-none sky font-mono text-3xs select-none"
      role="img"
      :aria-label="`Allowed ${counted.allowed}, refused ${counted.denied}, over ${buckets.length} hours. Drag to narrow the log.`"
      @pointerdown="start"
      @pointermove="move"
      @pointerup="finish"
      @pointercancel="cancel"
      @keydown.esc="emit('clear')"
    >
      <rect :width="skyWidth" :height="skyHeight" class="fill-surface-sunken" />
      <rect
        v-if="shown"
        class="brush fill-primary/15 stroke-primary"
        :x="shown.first * slot"
        :width="(shown.last - shown.first + 1) * slot"
        y="0"
        :height="skyHeight"
        :data-first="shown.first"
        :data-last="shown.last"
      />
      <g v-if="shown" class="brush-handles fill-primary" aria-hidden="true">
        <rect :x="shown.first * slot - 3" y="0" width="6" :height="skyHeight" class="opacity-25" />
        <rect
          :x="(shown.last + 1) * slot - 3"
          y="0"
          width="6"
          :height="skyHeight"
          class="opacity-25"
        />
        <rect :x="shown.first * slot - 1" y="0" width="2" :height="skyHeight" />
        <rect :x="(shown.last + 1) * slot - 1" y="0" width="2" :height="skyHeight" />
      </g>
      <g class="sky-grow" aria-hidden="true">
        <template v-for="bar in drawnBars" :key="bar.start">
          <rect
            v-if="bar.allowedHeight > 0"
            :x="bar.x"
            :y="bar.allowedY"
            :width="bar.width"
            :height="bar.allowedHeight"
            class="fill-fg-muted"
          />
          <rect
            v-if="bar.deniedHeight > 0"
            :x="bar.x"
            :y="bar.deniedY"
            :width="bar.width"
            :height="bar.deniedHeight"
            class="fill-danger"
          />
        </template>
      </g>
      <g class="fill-fg-muted stroke-line-strong" aria-hidden="true">
        <template v-for="tick in ticks" :key="tick.x">
          <line :x1="tick.x" :x2="tick.x" :y1="skyHeight" :y2="skyHeight + 4" />
          <text :x="tick.x + 2" :y="skyHeight + 14" class="stroke-none">{{ tick.label }}</text>
        </template>
      </g>
    </svg>
  </section>
</template>

<style scoped>
.sky-grow {
  transform-box: fill-box;
  transform-origin: bottom;
  animation: var(--animate-sky-grow);
}
</style>
