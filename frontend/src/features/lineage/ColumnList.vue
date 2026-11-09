<script setup lang="ts">
import { VBadge, VIcon } from '@/components'
import type { components } from '@/gen/api'

defineProps<{
  counts: readonly components['schemas']['LineageColumnCount'][]
  current: string | null
}>()

const emit = defineEmits<{ pick: [column: string] }>()
</script>

<template>
  <ul class="counts m-0 list-none px-3 py-2" aria-label="Columns">
    <li
      v-for="count in counts"
      :key="count.column.name"
      class="flex items-center gap-2 py-0.5 font-mono text-sm"
      :class="count.column.name === current ? 'current text-target' : ''"
    >
      <button
        type="button"
        class="pick cursor-pointer border-0 bg-transparent p-0 font-mono text-sm text-inherit hover:text-primary"
        @click="emit('pick', count.column.name)"
      >
        {{ count.column.name }}
      </button>
      <VIcon
        v-if="count.foreignKey"
        name="key"
        size="sm"
        class="fk text-fg-muted"
        label="foreign key"
      />
      <VBadge v-if="count.column.restricted" tone="danger" size="sm" class="restricted">
        restricted
      </VBadge>
      <span class="tally ml-auto text-2xs text-fg-soft">
        W {{ count.writes }} · R {{ count.reads }}
      </span>
    </li>
  </ul>
</template>
