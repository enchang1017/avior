<script setup lang="ts">
import VLoader from '@/components/Feedback/VLoader/VLoader.vue'

/*
 * The page loader over a route still loading its code (FEAT-074 AC-3). It
 * covers the page under the HUD without taking a place in the layout, so
 * neither its arrival nor its going shifts anything, and it appears only after
 * the 150 ms `--animate-pending` holds it back.
 */
</script>

<template>
  <div
    class="route-pending pointer-events-none absolute inset-x-0 top-hud bottom-0 z-10 grid animate-pending place-items-center bg-ground"
  >
    <VLoader size="page" label="Opening the page" />
  </div>
</template>
