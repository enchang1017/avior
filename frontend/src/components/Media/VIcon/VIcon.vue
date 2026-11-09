<script setup lang="ts">
import { computed } from 'vue'

import { icons, type IconName } from './icons'

/**
 * Every icon on the page, by name.
 *
 * The names are a closed registry rather than lucide's own exports, so an
 * icon is chosen for what it means here — `release`, `backup`, `pull-request`
 * — and only the ones named are in the bundle. Decorative by default: an icon
 * beside a word adds nothing for a screen reader. Pass `label` when the icon is
 * the only thing saying what a control does.
 */
const props = withDefaults(
  defineProps<{
    name: IconName
    size?: 'xs' | 'sm' | 'md' | 'lg'
    label?: string
    spin?: boolean
  }>(),
  { size: 'md', label: '', spin: false },
)

const sizes = {
  xs: 'size-3',
  sm: 'size-3.5',
  md: 'size-4',
  lg: 'size-5',
} as const

const accessible = computed(
  (): Record<string, string> =>
    props.label === '' ? { 'aria-hidden': 'true' } : { role: 'img', 'aria-label': props.label },
)
</script>

<template>
  <component
    :is="icons[props.name]"
    :class="[sizes[props.size], props.spin ? 'animate-spin' : '']"
    :stroke-width="1.75"
    :data-icon="props.name"
    v-bind="accessible"
  />
</template>
