<script setup lang="ts">
import { computed } from 'vue'

import VIcon from '../../Media/VIcon/VIcon.vue'
import type { IconName } from '../../Media/VIcon/icons'

/**
 * A line across the page that something needs reading.
 *
 * The tone is the point. A refusal an operator caused, a disagreement Avior
 * cannot resolve, and a note about what a page does are three different things,
 * and three views had rendered all of them in the same red.
 */
const props = withDefaults(
  defineProps<{
    tone?: 'danger' | 'warn' | 'info'
    /** Set for anything the operator did not expect, so it is announced. */
    alert?: boolean
    /**
     * A mark for what kind of thing this is, when the words alone are slower
     * to scan than a shape. A lock reads as "not yours" before it is read.
     */
    icon?: IconName | ''
  }>(),
  { tone: 'danger', alert: true, icon: '' },
)

/*
 * Bound as an object rather than as `:role="alert ? 'alert' : undefined"`.
 * The build runs with exactOptionalPropertyTypes, under which binding undefined
 * to an optional attribute is an error rather than an omission — which is the
 * stricter reading and the right one: "absent" and "present and undefined" are
 * different things to the DOM.
 */
const announcement = computed(() => (props.alert ? { role: 'alert' } : {}))

const tones = {
  danger: 'border-b-danger-line bg-danger-bg text-danger',
  warn: 'border-b-warn-line bg-warn-bg text-warn',
  info: 'border-b-line bg-surface-sunken text-fg-soft',
} as const
</script>

<template>
  <p
    class="m-0 flex flex-wrap items-center gap-1.5 border-b px-3 py-2 text-xs"
    :class="tones[props.tone]"
    v-bind="announcement"
  >
    <VIcon v-if="props.icon !== ''" :name="props.icon" size="sm" />
    <slot />
  </p>
</template>
