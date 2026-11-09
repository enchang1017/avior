<script setup lang="ts">
import { computed } from 'vue'

import { VIcon, VPopover } from '@/components'
import { lazyPart } from './lazyPart'
import type { LineageNode } from './types'

// The list is only drawn once the chip is opened, so it is only fetched then.
const DynamicList = lazyPart(() => import('./DynamicList.vue'), 'Loading the routines', 'inline')

/**
 * The routines that build SQL at run time, folded into a chip beside the
 * legend. Each is named whether or not the drawing reaches it, and marked when
 * it does not, because the page draws one neighbourhood and warns about the
 * whole database.
 */
const props = defineProps<{ routines: LineageNode[]; total: number; drawn: ReadonlySet<string> }>()
defineEmits<{ open: [id: string] }>()

const count = computed(() => Math.max(props.total, props.routines.length))
</script>

<template>
  <VPopover label="Routines that build SQL at run time" side="above" align="right" offset="wide">
    <template #trigger>
      <span
        class="dynamic-chip inline-flex items-center gap-1 rounded-xs border border-warn-line bg-warn-bg px-1.5 font-mono text-2xs text-warn"
        data-testid="dynamic-chip"
      >
        <VIcon name="warning" size="xs" />
        {{ count }} dynamic SQL {{ count === 1 ? 'routine' : 'routines' }}
      </span>
    </template>
    <DynamicList :routines="routines" :count="count" :drawn="drawn" @open="$emit('open', $event)" />
  </VPopover>
</template>
