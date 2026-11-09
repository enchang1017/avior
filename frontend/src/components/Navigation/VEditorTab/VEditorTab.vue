<script setup lang="ts">
import VIcon from '../../Media/VIcon/VIcon.vue'
import { environmentTab } from '@/shared/environment'
import type { CurrentUser } from '@/shared/api'

/**
 * One open statement, labelled with its database and tinted by the
 * environment: a 3px top edge and a wash. One deployment is one environment,
 * so every tab carries the same tint — it says which environment this is, not
 * which tab differs.
 */
const props = defineProps<{
  label: string
  environment: CurrentUser['environment']
  active: boolean
}>()

const emit = defineEmits<{ select: []; close: [] }>()
</script>

<template>
  <div
    class="editor-tab group flex h-8 max-w-60 items-center gap-1 rounded-t-control border-t-3 pr-1 pl-3 text-sm"
    :class="[
      props.active ? environmentTab(props.environment) : 'border-t-transparent text-fg-muted',
      props.active ? 'active' : 'hover:bg-surface',
    ]"
  >
    <button
      type="button"
      role="tab"
      :aria-selected="props.active"
      class="flex min-w-0 cursor-pointer items-center gap-2 border-0 bg-transparent p-0 font-[inherit] text-inherit"
      @click="emit('select')"
    >
      <VIcon :name="props.environment === 'production' ? 'shield-alert' : 'terminal'" size="sm" />
      <span class="truncate font-mono" :class="props.active ? 'text-fg' : ''">{{
        props.label
      }}</span>
    </button>
    <button
      type="button"
      class="close grid size-5 flex-none cursor-pointer place-items-center rounded-xs border-0 bg-transparent p-0 text-fg-muted hover:bg-surface-sunken hover:text-fg"
      :aria-label="`Close ${props.label}`"
      @click="emit('close')"
    >
      <VIcon name="x" size="xs" />
    </button>
  </div>
</template>
