<script setup lang="ts">
import { ref } from 'vue'

import { VButton } from '@/components'

const props = defineProps<{ text: string }>()
const open = ref(false)
</script>

<template>
  <div class="flex items-start gap-2">
    <pre
      class="m-0 min-w-0 flex-1 font-mono text-xs [word-break:break-word] whitespace-pre-wrap"
      :class="open ? '' : 'line-clamp-2'"
      data-statement-text
      >{{ props.text }}</pre
    >
    <VButton variant="bare" size="sm" :aria-expanded="open" @click="open = !open">
      {{ open ? 'Less' : 'More' }}
    </VButton>
  </div>
</template>
