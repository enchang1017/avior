<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'

import VSparkline from '../VSparkline/VSparkline.vue'
import type { Delta } from './types'

/**
 * One headline figure: what it is, how much, how it moved, and its history
 * when one is kept. With `to` the whole card is the way into the detail.
 */
const props = defineProps<{
  label: string
  value: string
  delta?: Delta | null
  spark?: readonly number[] | null
  to?: RouteLocationRaw
  loading?: boolean
  hint?: string
}>()

const arrows: Record<Delta['direction'], string> = { up: '↑', down: '↓', flat: '–' }
const moved: Record<Delta['direction'], string> = { up: 'up', down: 'down', flat: 'unchanged' }

const deltaTone = computed(() => {
  const good = props.delta?.good ?? null
  if (good === null) return 'bg-surface-sunken text-fg-muted'
  return good ? 'bg-ok-bg text-ok' : 'bg-danger-bg text-danger'
})
const hasSpark = computed(() => (props.spark?.length ?? 0) >= 2)
</script>

<template>
  <component
    :is="to === undefined ? 'div' : RouterLink"
    :to="to"
    class="kpi-card flex min-h-32 min-w-0 flex-col gap-1.5 rounded-card border border-line bg-surface p-3 text-fg no-underline"
    :class="to === undefined ? '' : 'cursor-pointer ease-colours hover:border-line-strong'"
    :aria-busy="loading ? 'true' : undefined"
  >
    <span class="truncate eyebrow">{{ label }}</span>
    <template v-if="loading">
      <span
        class="block h-7 w-24 rounded-sm bg-surface-sunken"
        role="status"
        :aria-label="`Loading ${label}`"
        data-testid="kpi-skeleton"
      />
      <span class="block h-4 w-16 rounded-sm bg-surface-sunken" />
      <span class="block h-6 w-24 rounded-sm bg-surface-sunken" />
    </template>
    <template v-else>
      <span class="font-mono text-2xl leading-tight tabular-nums" data-testid="kpi-value">
        {{ value }}
      </span>
      <span class="flex min-h-6 items-center gap-2">
        <span
          v-if="delta"
          class="inline-flex items-center gap-1 rounded-pill px-1.5 font-mono text-2xs"
          :class="deltaTone"
          data-testid="kpi-delta"
          :data-direction="delta.direction"
        >
          <span aria-hidden="true">{{ arrows[delta.direction] }}</span>
          <span class="sr-only">{{ moved[delta.direction] }}</span>
          {{ delta.text }}
        </span>
        <VSparkline v-if="hasSpark && spark" :values="spark" :label="label" class="ml-auto" />
      </span>
      <span v-if="hint" class="text-2xs text-fg-muted">{{ hint }}</span>
    </template>
  </component>
</template>
