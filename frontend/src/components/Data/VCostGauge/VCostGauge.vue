<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { gaugeOf, type GaugeLevel, type Measurement } from './gauge'

const props = defineProps<{
  measurement: Measurement | null
  /** The statement changed after it was measured. */
  stale?: boolean
}>()

const gauge = computed(() => gaugeOf(props.measurement))

const fillClass: Record<GaugeLevel, string> = {
  unmeasured: 'bg-fg-faint',
  'no-plan': 'bg-fg-faint',
  clear: 'bg-ok',
  near: 'bg-warn',
  over: 'bg-danger',
}

const valueClass: Record<GaugeLevel, string> = {
  unmeasured: 'text-fg-muted',
  'no-plan': 'text-fg-muted',
  clear: 'text-fg',
  near: 'text-warn',
  over: 'text-danger',
}

const swept = ref(0)
let frame = 0

watch(
  () => gauge.value.fraction,
  (fraction) => {
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => {
      swept.value = fraction ?? 0
    })
  },
  { immediate: true },
)

onBeforeUnmount(() => cancelAnimationFrame(frame))

const fill = computed(() => ({ transform: `scaleX(${swept.value})` }))
const alerting = computed(() => gauge.value.level === 'near' || gauge.value.level === 'over')
</script>

<template>
  <div
    class="cost-gauge flex min-w-0 items-center gap-3 font-mono text-xs transition-opacity duration-150"
    :class="stale ? 'opacity-50' : ''"
    :data-level="gauge.level"
    :data-stale="stale ? 'true' : 'false'"
    data-testid="cost-gauge"
  >
    <span class="text-3xs tracking-hud text-fg-muted uppercase">Plan cost</span>
    <span
      v-if="gauge.fraction !== null"
      class="relative h-1 w-32 overflow-hidden rounded-pill bg-line"
      role="meter"
      aria-label="Plan cost against the cap"
      aria-valuemin="0"
      aria-valuemax="1"
      :aria-valuenow="gauge.fraction"
    >
      <span
        class="fill absolute inset-0 origin-left rounded-pill transition-transform duration-(--duration-sweep) ease-out"
        :class="fillClass[gauge.level]"
        :style="fill"
      />
      <span class="absolute inset-y-0 left-4/5 w-px bg-target" aria-hidden="true" />
    </span>
    <span
      :key="gauge.level"
      class="cost"
      :class="[valueClass[gauge.level], alerting ? 'gauge-alert' : '']"
    >
      {{ gauge.cost
      }}<span v-if="gauge.maxCost !== ''" class="text-fg-muted"> / {{ gauge.maxCost }}</span>
    </span>
    <span v-if="gauge.level === 'over'" class="rule text-danger">cost_limit_exceeded</span>
    <span class="text-3xs tracking-hud text-fg-muted uppercase">Rows</span>
    <span class="rows" :class="gauge.rowsOver ? 'text-danger' : 'text-fg'">
      {{ gauge.rows
      }}<span v-if="gauge.maxRows !== ''" class="text-fg-muted"> / {{ gauge.maxRows }}</span>
    </span>
    <span v-if="stale" class="text-3xs tracking-hud text-fg-muted uppercase">stale</span>
  </div>
</template>

<style scoped>
.gauge-alert {
  display: inline-block;
  animation: var(--animate-gauge-alert);
}
</style>
