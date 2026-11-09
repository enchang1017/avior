<script setup lang="ts">
import { flagPillText, flagText } from './text'
import { VBadge, VIcon } from '@/components'

const props = defineProps<{ idleDays: number }>()
</script>

<template>
  <VBadge
    tone="warn"
    size="sm"
    class="whitespace-nowrap"
    :title="flagText(props.idleDays)"
    data-flag
  >
    <VIcon name="warning" />
    <span aria-hidden="true">{{ flagPillText(props.idleDays) }}</span>
    <span class="sr-only">{{ flagText(props.idleDays) }}</span>
  </VBadge>
</template>
