<script setup lang="ts">
/**
 * A few stars that twinkle on the empty ground: opacity only, each its own
 * small layer, placed in the margins so they are never behind text. They are
 * the ground's, not the content's, and stand still under reduced motion.
 */
const stars = [
  'top-1/8 left-1/10 size-0.5 [animation-delay:-0.4s]',
  'top-1/5 right-1/6 size-0.75 [animation-delay:-1.6s]',
  'top-2/3 left-1/12 size-0.5 [animation-delay:-2.2s]',
  'bottom-1/8 right-1/10 size-0.75 [animation-delay:-0.9s]',
  'top-1/3 left-1/5 size-0.5 [animation-delay:-2.8s]',
  'bottom-1/5 left-2/5 size-0.5 [animation-delay:-1.2s]',
] as const
</script>

<template>
  <div class="twinkles pointer-events-none fixed inset-0 overflow-hidden" aria-hidden="true">
    <span
      v-for="star in stars"
      :key="star"
      class="star absolute animate-twinkle rounded-pill bg-fg opacity-25"
      :class="star"
    />
  </div>
</template>
