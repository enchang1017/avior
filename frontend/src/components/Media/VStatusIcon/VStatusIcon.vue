<script setup lang="ts">
import { computed } from 'vue'

import VIcon from '../VIcon/VIcon.vue'
import type { IconName } from '../VIcon/icons'

/**
 * Where something stands, as a shape and a colour at once.
 *
 * The shape carries it for somebody who cannot tell the colours apart: a tick
 * is allowed, a cross refused, a triangle needs reading, a dashed ring has not
 * started and a turning one is running now.
 */
export type Status = 'ok' | 'refused' | 'warn' | 'pending' | 'running' | 'idle'

const props = withDefaults(
  defineProps<{ status: Status; size?: 'sm' | 'md' | 'lg'; label?: string }>(),
  {
    size: 'md',
    label: '',
  },
)

const looks: Record<Status, { icon: IconName; tone: string }> = {
  ok: { icon: 'circle-check', tone: 'text-ok' },
  refused: { icon: 'circle-x', tone: 'text-danger' },
  warn: { icon: 'warning', tone: 'text-warn' },
  pending: { icon: 'circle-dot', tone: 'text-target' },
  running: { icon: 'loader', tone: 'text-primary' },
  idle: { icon: 'circle-dashed', tone: 'text-fg-muted' },
}

const look = computed(() => looks[props.status])
</script>

<template>
  <span class="status-icon inline-flex items-center" :class="look.tone" :data-status="props.status">
    <VIcon
      :name="look.icon"
      :size="props.size"
      :label="props.label"
      :spin="props.status === 'running'"
    />
  </span>
</template>
