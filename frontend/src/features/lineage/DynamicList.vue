<script setup lang="ts">
import type { LineageNode } from './types'

defineProps<{ routines: LineageNode[]; count: number; drawn: ReadonlySet<string> }>()
const emit = defineEmits<{ open: [id: string] }>()
</script>

<template>
  <div class="dynamic-tray flex flex-col gap-2 text-xs text-fg-soft" data-testid="dynamic-tray">
    <p class="m-0">
      These routines build SQL at run time; what it touches can't be known, so it is never drawn.
    </p>
    <ul class="m-0 flex list-none flex-col gap-1 p-0">
      <li v-for="routine in routines" :key="routine.id" class="flex items-center gap-2">
        <button
          type="button"
          class="routine cursor-pointer border-0 bg-transparent p-0 font-mono text-sm text-fg hover:text-primary"
          :title="`${routine.schema}.${routine.name}(${routine.arguments ?? ''})`"
          @click="emit('open', routine.id)"
        >
          {{ routine.name }}
        </button>
        <span
          v-if="!drawn.has(routine.id)"
          class="off-view rounded-xs border border-line px-1 font-mono text-2xs text-fg-muted"
          >not in current view</span
        >
      </li>
    </ul>
    <p v-if="count > routines.length" class="m-0 text-fg-muted">
      and {{ count - routines.length }} more
    </p>
  </div>
</template>
