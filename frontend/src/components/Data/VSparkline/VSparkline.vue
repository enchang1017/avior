<script setup lang="ts">
import { computed } from 'vue'

import { polyline, sparkHeight, sparkPoints, sparkWidth, trend } from './sparkline'

/**
 * A series as one small line, drawn inline beside the figure it explains.
 *
 * Inline SVG and nothing else: ADR 0009 P7 holds a new dependency to 10 KB,
 * and a chart library for a line of forty points would spend the budget on
 * axes nobody reads here. The label is what a screen reader hears, and the
 * trend word is appended so the shape is not the only thing that carries it.
 */
const props = defineProps<{
  values: readonly number[]
  label: string
}>()

const points = computed(() => sparkPoints(props.values))
const last = computed(() => points.value[points.value.length - 1] ?? null)
const spoken = computed(() => {
  const moved = trend(props.values)
  return moved === 'empty' ? `${props.label}: no history yet` : `${props.label}: ${moved}`
})
</script>

<template>
  <svg
    :viewBox="`0 0 ${sparkWidth} ${sparkHeight}`"
    preserveAspectRatio="none"
    class="sparkline block h-6 w-24 overflow-visible"
    role="img"
    :aria-label="spoken"
    :data-trend="trend(props.values)"
  >
    <title>{{ spoken }}</title>
    <line
      v-if="points.length === 0"
      x1="0"
      :x2="sparkWidth"
      :y1="sparkHeight / 2"
      :y2="sparkHeight / 2"
      class="stroke-fg-faint"
      stroke-dasharray="2 3"
      vector-effect="non-scaling-stroke"
    />
    <polyline
      v-else
      :points="polyline(points)"
      class="fill-none stroke-primary"
      stroke-width="1.5"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
    <circle v-if="last" :cx="last.x" :cy="last.y" r="1.5" class="fill-primary" />
  </svg>
</template>
