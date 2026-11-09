<script setup lang="ts">
import { VBadge } from '@/components'

defineProps<{ confidence: 'exact' | 'parsed' | 'dynamic' }>()

const tones = { exact: 'plain', parsed: 'plain', dynamic: 'warn' } as const
</script>

<template>
  <VBadge
    :tone="tones[confidence]"
    size="sm"
    class="confidence font-mono tracking-hud"
    :class="confidence === 'parsed' ? 'border-dashed' : ''"
    :data-confidence="confidence"
  >
    {{ confidence.toUpperCase() }}
  </VBadge>
</template>
