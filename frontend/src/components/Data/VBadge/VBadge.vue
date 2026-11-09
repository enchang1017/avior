<script setup lang="ts">
import { computed } from 'vue'

import { toneLine, toneText, toneWash, type Tone } from '../../tone'

/**
 * A short word about the state of the thing beside it.
 *
 * Twenty-five chips were spans with a class, each view adding its own modifier
 * for the colour it wanted — `blocked`, `self`, `lapsed`, `chip-ok` — and three
 * views had written the same four tones out again under different names. The
 * tone is now the only thing a view says; what it looks like is decided here.
 *
 * `plain` is the chip on a surface that needs no colour: a kind, a count. The
 * `primary` tone marks the one row that is the operator's own, and `agent` an
 * account that is nobody's person: edged like the operator's own, but quiet.
 */
const props = withDefaults(
  defineProps<{
    tone?: 'plain' | 'primary' | 'agent' | Tone
    /** The rail's and the top bar's, a step smaller than one beside body text. */
    size?: 'md' | 'sm'
  }>(),
  { tone: 'plain', size: 'md' },
)

defineSlots<{ default?: () => unknown }>()

const colours = computed(() => {
  if (props.tone === 'plain') return 'border-line bg-surface-sunken text-fg-soft'
  if (props.tone === 'primary') return 'border-primary-soft bg-primary-wash text-primary'
  if (props.tone === 'agent') return 'border-primary-soft bg-surface-sunken text-fg-soft'
  return [toneLine[props.tone], toneWash[props.tone], toneText[props.tone]]
})

const shape = computed(() =>
  props.size === 'sm' ? 'px-1.5 py-0.25 text-2xs' : 'px-1.75 py-0.25 text-xs',
)
</script>

<template>
  <span
    class="chip inline-flex items-center gap-1 rounded-pill border font-medium"
    :class="[colours, shape]"
  >
    <slot />
  </span>
</template>
