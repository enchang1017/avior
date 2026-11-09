<script setup lang="ts">
import { computed } from 'vue'

import { toneAccent, type Tone } from '../../tone'

/**
 * A surface lifted off the page: the white box with a hairline edge.
 *
 * `pad` is the inset most cards read with. It is a switch rather than the
 * default because a card holding a table or a list wants the rows to reach its
 * edge, and a card that pads itself makes every one of those undo it.
 *
 * `tone` draws a heavier rule down the leading edge, for a card whose content
 * is a verdict — the drift check, the lint findings — so the answer is seen
 * before the card is read.
 */
const props = withDefaults(
  defineProps<{
    as?: 'div' | 'section' | 'article' | 'li'
    pad?: boolean
    tone?: Tone | 'none'
  }>(),
  { as: 'div', pad: false, tone: 'none' },
)

defineSlots<{ default?: () => unknown }>()

const classes = computed(() => [
  props.pad ? 'p-3' : '',
  props.tone === 'none' ? '' : ['border-l-4', toneAccent[props.tone]],
])
</script>

<template>
  <component :is="props.as" class="rounded-card border border-line bg-surface" :class="classes">
    <slot />
  </component>
</template>
