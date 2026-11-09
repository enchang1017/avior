<script setup lang="ts">
defineProps<{ database: string; failure?: string }>()
</script>

<template>
  <div class="chart-placeholder flex flex-col gap-1">
    <div class="flex aspect-[5/3] items-center justify-center px-3">
      <p v-if="failure" class="chart-failure m-0 text-xs text-danger">{{ failure }}</p>
      <p v-else class="m-0 font-mono text-3xs tracking-hud text-fg-muted uppercase">
        Charting {{ database }}
      </p>
    </div>
    <p class="m-0 px-1 font-mono text-3xs tracking-hud uppercase" aria-hidden="true">&nbsp;</p>
  </div>
</template>
