<script setup lang="ts">
/**
 * Avior as the light the sign-in page is lit by (FEAT-074): the loader's star
 * at hero size, its halo and flare painted once, its companion on a slow
 * 24 s orbit. Decoration only, so it is hidden from assistive technology.
 */
</script>

<template>
  <div class="hero-star aspect-square w-hero-star" aria-hidden="true">
    <div class="loader-bloom" />
    <div class="hero-flare" />
    <div class="hero-orbit animate-comet-slow will-change-transform">
      <span class="comet-tail" />
      <span class="comet-head" />
    </div>
    <svg class="loader-mark" viewBox="10 6 34 55">
      <path
        class="fill-target"
        d="M27 6 L30.394 21.606 L44 25 L30.394 28.394 L27 61 L23.606 28.394 L10 25 L23.606 21.606 Z"
      />
      <circle class="fill-star-core" cx="27" cy="25" r="1.4" />
    </svg>
  </div>
</template>
