<script setup lang="ts">
import type { Connector } from './types'

/**
 * Thin lines joining boxes laid out in columns, drawn over them.
 *
 * The caller measures where its boxes are and passes each line's two ends in
 * the container's own pixels; this draws them, and nothing else — no motion,
 * no glow. A line runs right from its start, turns once halfway, and arrives
 * from the left, so boxes in neighbouring columns read as joined without the
 * line crossing either. FEAT-076's roles map is the one caller.
 *
 * Emphasis is a colour and a weight and nothing else: no transition, so
 * pointing at a box changes the picture at once and moving away restores it.
 */

defineProps<{ lines: Connector[] }>()

function elbow(line: Connector): string {
  const mid = line.turn ?? Math.round((line.x1 + line.x2) / 2)
  return `M${line.x1} ${line.y1} H${mid} V${line.y2} H${line.x2}`
}
</script>

<template>
  <svg
    class="pointer-events-none absolute inset-0 size-full overflow-visible"
    aria-hidden="true"
    data-testid="connectors"
  >
    <path
      v-for="line in lines"
      :key="line.key"
      :d="elbow(line)"
      :data-line="line.key"
      :data-dashed="line.dashed ? 'true' : undefined"
      :data-emphasis="line.emphasis"
      class="fill-none"
      :class="
        line.emphasis === 'lit'
          ? 'stroke-fg-soft'
          : line.emphasis === 'dim'
            ? 'stroke-line'
            : 'stroke-line-strong'
      "
      :stroke-width="line.emphasis === 'lit' ? 2 : 1"
      :stroke-dasharray="line.dashed ? '4 3' : 'none'"
    />
  </svg>
</template>
