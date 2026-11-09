<script setup lang="ts">
import DelayedLoader from '@/shared/DelayedLoader.vue'

/** A select's shape while its options are on the way, so it is never an empty list. */
defineProps<{ label: string }>()
</script>

<template>
  <div
    class="pending-select flex h-control-sm items-center overflow-hidden rounded-control border border-line bg-surface-sunken px-2"
  >
    <DelayedLoader size="inline" :label="label" />
  </div>
</template>
