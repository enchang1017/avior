<script setup lang="ts">
import { computed } from 'vue'

import { describeTrajectory, type StepState, type Trajectory } from './trajectory'

const props = withDefaults(
  defineProps<{
    trajectory: Trajectory
    /** `row` for a list cell, `steps` for the approval steps of the one being read. */
    layout?: 'row' | 'steps'
    /** One line under each step, in the steps layout: who, and when. */
    details?: string[]
  }>(),
  { layout: 'row', details: () => [] },
)

const gap = 28
const rowWidth = gap * 3 + 12
const xOf = (index: number) => 6 + index * gap

const spoken = computed(() => describeTrajectory(props.trajectory))
const currentIndex = computed(() =>
  props.trajectory.steps.findIndex((step) => step.state === 'current'),
)

const toneClass = computed(
  () =>
    ({
      target: 'text-target',
      halt: 'text-danger',
      go: 'text-ok',
      muted: 'text-fg-muted',
    })[props.trajectory.tone],
)

const starClass: Record<StepState, string> = {
  done: 'fill-fg stroke-fg',
  current: 'fill-target stroke-target',
  stopped: 'fill-none stroke-danger',
  todo: 'fill-none stroke-fg-faint',
}

const markClass: Record<StepState, string> = {
  done: 'border-fg bg-fg',
  current: 'border-target bg-target',
  stopped: 'border-danger',
  todo: 'border-fg-faint',
}

function segmentDashed(index: number): boolean {
  return props.trajectory.steps[index + 1]?.state === 'todo'
}
</script>

<template>
  <div v-if="layout === 'row'" class="trajectory flex flex-col gap-0.5" :title="spoken">
    <span class="relative block h-3 w-24">
      <span
        v-if="currentIndex !== -1"
        class="row-pulse pointer-events-none absolute top-0 left-0 size-3 rounded-pill bg-target"
        :style="{ transform: `translateX(${xOf(currentIndex) - 6}px)` }"
        aria-hidden="true"
      />
      <svg
        :viewBox="`0 0 ${rowWidth} 12`"
        class="block h-3 w-24 overflow-visible"
        role="img"
        :aria-label="spoken"
      >
        <line
          v-for="index in 3"
          :key="`segment-${index}`"
          :x1="xOf(index - 1) + 4"
          :x2="xOf(index) - 4"
          y1="6"
          y2="6"
          class="stroke-fg-faint"
          :stroke-dasharray="segmentDashed(index - 1) ? '2 2' : 'none'"
        />
        <template v-for="(step, index) in trajectory.steps" :key="step.label">
          <circle
            v-if="step.state === 'current'"
            :cx="xOf(index)"
            cy="6"
            r="5.5"
            class="halo fill-target-wash stroke-target-line"
          />
          <g v-if="step.state === 'stopped'" class="stroke-danger" :data-state="step.state">
            <line :x1="xOf(index) - 3" :x2="xOf(index) + 3" y1="3" y2="9" />
            <line :x1="xOf(index) - 3" :x2="xOf(index) + 3" y1="9" y2="3" />
          </g>
          <circle
            v-else
            :cx="xOf(index)"
            cy="6"
            :r="step.state === 'current' ? 3.5 : 3"
            :class="starClass[step.state]"
            :data-state="step.state"
          />
        </template>
      </svg>
    </span>
    <span class="caption font-mono text-3xs tracking-hud uppercase" :class="toneClass">
      {{ trajectory.caption }}
    </span>
  </div>

  <ol v-else class="trajectory m-0 flex list-none flex-col gap-3 p-0" :aria-label="spoken">
    <li
      v-for="(step, index) in trajectory.steps"
      :key="step.label"
      class="step flex items-start gap-3"
      :data-state="step.state"
      :style="{ animationDelay: `${index * 90}ms` }"
    >
      <span
        class="relative mt-1 flex size-3 flex-none items-center justify-center rounded-pill border"
        :class="markClass[step.state]"
        aria-hidden="true"
      >
        <span
          v-if="step.state === 'current'"
          class="absolute inset-0 animate-pulse rounded-pill bg-target"
        />
      </span>
      <span class="flex flex-col">
        <span
          class="text-sm"
          :class="
            step.state === 'todo'
              ? 'text-fg-muted'
              : step.state === 'stopped'
                ? 'text-danger'
                : 'text-fg'
          "
        >
          {{ step.label }}
        </span>
        <span v-if="details?.[index]" class="font-mono text-xs text-fg-muted">
          {{ details?.[index] }}
        </span>
      </span>
    </li>
  </ol>
</template>

<style scoped>
.row-pulse {
  opacity: 0;
}

:is(tr, [data-row]):is(:hover, :focus-within) .row-pulse {
  animation: var(--animate-pulse);
}

.step {
  animation: var(--animate-step-light);
}
</style>
