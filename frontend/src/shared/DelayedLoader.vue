<script setup lang="ts">
import { defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'

import { loaderDelayMs, type LoaderSize } from './loaderDelay'

/**
 * The loader, once whatever it stands in for has been pending for the delay.
 * Until then it draws nothing and its chunk is not fetched, so a page whose
 * answers come quickly never pays for it on its first load (ADR 0009 P3).
 */
const props = defineProps<{ size: LoaderSize; label: string }>()

const VLoader = defineAsyncComponent(() => import('@/components/Feedback/VLoader/VLoader.vue'))

const shown = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null

onMounted(() => {
  timer = setTimeout(() => (shown.value = true), loaderDelayMs)
})

onBeforeUnmount(() => {
  if (timer !== null) clearTimeout(timer)
})
</script>

<template>
  <VLoader v-if="shown" :size="props.size" :label="props.label" />
</template>
