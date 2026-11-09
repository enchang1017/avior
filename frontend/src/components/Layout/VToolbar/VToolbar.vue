<script setup lang="ts">
import VIcon from '../../Media/VIcon/VIcon.vue'

/**
 * The sticky bar a pane opens with.
 *
 * Two shapes sit on it. A page header names what the page is; an action bar
 * carries the buttons and names nothing, because the rail and the tab above it
 * already said where the operator is. Both stick, both hold a busy indicator,
 * and both push what comes after the spacer to the right — so the bar itself is
 * one thing and the two shapes are what fill it.
 */
const props = withDefaults(defineProps<{ busy?: boolean; busyLabel?: string }>(), {
  busy: false,
  busyLabel: 'Reading',
})

// Declared rather than inferred: a component whose slots are inferred cannot be
// used inside another's slot without vue-tsc failing to name the type it made.
defineSlots<{ lead?: () => unknown; default?: () => unknown }>()
</script>

<template>
  <div
    class="sticky top-0 z-1 flex min-h-14 flex-none items-center gap-2 border-b border-line bg-surface px-4 py-2.5"
  >
    <slot name="lead" />

    <span v-if="props.busy" class="inline-flex items-center gap-1 text-xs text-fg-soft">
      <VIcon name="loader" spin />
      {{ props.busyLabel }}
    </span>

    <div class="flex-1" />
    <slot />
  </div>
</template>
