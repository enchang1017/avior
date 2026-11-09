<script setup lang="ts">
import { computed } from 'vue'

import { tableVariants, type TableVariant } from './variants'

/**
 * Rows and columns.
 *
 * Fifteen tables were a bare `<table>` with its own block of CSS, and the
 * blocks had drifted: nine shades of the same list, told apart by a sixteenth
 * of a rem. The shapes that remain are the ones that genuinely differ, named by
 * what they are for, and a view picks one rather than writing its own.
 *
 * The caller writes the header cells and the body rows; the table, its head
 * row, its body and every cell's padding and rule are drawn here. A cell's own
 * utility always wins over the table's — the table's rules carry no weight of
 * their own — so a right-aligned figure is `class="text-right"` on its cell.
 *
 * A row marked `data-detail` is the expansion of the row above it: it is not
 * highlighted under the pointer, and it carries no rule between the two.
 */
const props = withDefaults(
  defineProps<{
    variant?: TableVariant
    /** The caption is for assistive technology unless the table shows it. */
    captionVisible?: boolean
  }>(),
  { variant: 'list', captionVisible: false },
)

defineSlots<{
  caption?: () => unknown
  head?: () => unknown
  default?: () => unknown
}>()

const classes = computed(() => tableVariants[props.variant])
</script>

<template>
  <table :class="classes">
    <caption v-if="$slots.caption" :class="props.captionVisible ? 'pb-2 text-left' : 'sr-only'">
      <slot name="caption" />
    </caption>
    <thead v-if="$slots.head">
      <tr>
        <slot name="head" />
      </tr>
    </thead>
    <tbody>
      <slot />
    </tbody>
  </table>
</template>
