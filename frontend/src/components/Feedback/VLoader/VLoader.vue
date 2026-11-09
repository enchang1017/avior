<script setup lang="ts">
/**
 * Something is on its way: Avior's star, with its blue companion circling it.
 *
 * One design at three sizes (FEAT-074). `inline` sits in a line of text with
 * its label beside it; `section` holds the place of an area waiting for its
 * first answer; `page` is the whole view waiting for its code, and is what
 * `index.html` draws before the script has run.
 *
 * The star holds still; the comet and its tail turn as one pre-painted layer,
 * so the frame loop neither lays out nor paints. Under reduced motion the
 * comet stands at twelve o'clock. The label is the accessible name at every
 * size, and the visible text only at `inline`, where a slot may replace it.
 */
const props = withDefaults(
  defineProps<{
    size?: 'inline' | 'section' | 'page'
    label: string
  }>(),
  { size: 'section' },
)

defineSlots<{ default?: () => unknown }>()

const orbs = { inline: 'size-7', section: 'size-16', page: 'size-40' } as const
</script>

<template>
  <span
    class="loader inline-flex items-center gap-2.5"
    :class="props.size === 'inline' ? '' : 'flex-col'"
    :data-size="props.size"
    role="status"
    aria-live="polite"
    :aria-label="props.label"
  >
    <span class="loader-orb" :class="orbs[props.size]" aria-hidden="true">
      <span class="loader-bloom" />
      <span class="comet absolute inset-0 animate-comet will-change-transform">
        <span class="comet-tail" />
        <span class="comet-head" />
      </span>
      <svg class="loader-mark" viewBox="10 6 34 55">
        <path
          class="fill-target"
          d="M27 6 L30.394 21.606 L44 25 L30.394 28.394 L27 61 L23.606 28.394 L10 25 L23.606 21.606 Z"
        />
        <circle class="fill-star-core" cx="27" cy="25" r="1.6" />
      </svg>
    </span>
    <span v-if="props.size === 'inline'" class="text-sm text-fg-soft">
      <slot>{{ props.label }}</slot>
    </span>
  </span>
</template>
