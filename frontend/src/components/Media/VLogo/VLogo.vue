<script setup lang="ts">
/**
 * Southward: Avior's mark and wordmark.
 *
 * A four-point star with straight, unequal rays, the south ray twice the
 * others, because the False Cross Avior belongs to is how sailors find south;
 * the small dot is its hot companion. Three rules keep it from being the
 * generic sparkle: the rays are straight, they are unequal, and the mark is
 * never tilted, haloed or filled with a gradient.
 *
 * The wordmark is drawn as strokes — Λ without its crossbar — so it needs no
 * font and scales cleanly.
 */
const props = withDefaults(defineProps<{ wordmark?: boolean; size?: 'sm' | 'md' | 'lg' }>(), {
  wordmark: false,
  size: 'md',
})

const heights = { sm: 'h-6', md: 'h-7.5', lg: 'h-12' } as const
</script>

<template>
  <span class="logo inline-flex items-center gap-2" role="img" aria-label="Avior">
    <svg :class="heights[props.size]" viewBox="0 0 64 64" aria-hidden="true">
      <path
        class="fill-target"
        d="M27 6 L30.394 21.606 L44 25 L30.394 28.394 L27 61 L23.606 28.394 L10 25 L23.606 21.606 Z"
      />
      <circle class="fill-primary" cx="51" cy="10" r="4.5" />
    </svg>
    <svg
      v-if="props.wordmark"
      class="h-3.5 stroke-fg"
      viewBox="-2 -2 115 28"
      fill="none"
      stroke-width="2.4"
      stroke-linecap="square"
      stroke-linejoin="miter"
      aria-hidden="true"
    >
      <path d="M0 24 L9 0 L18 24" />
      <path transform="translate(27 0)" d="M0 0 L9 24 L18 0" />
      <path transform="translate(54 0)" d="M0 0 V24" />
      <path transform="translate(63 0)" d="M11 0 A11 12 0 1 1 10.99 0 Z" />
      <path transform="translate(94 0)" d="M0 24 V0 H8 A6.5 6.5 0 0 1 8 13 H0 M7.5 13 L15 24" />
    </svg>
  </span>
</template>
