<script setup lang="ts">
import VTabs, { type TabOption } from '../../Navigation/VTabs/VTabs.vue'

/**
 * The results pane: Result, Plan and Messages, one at a time, under a readout
 * of what the last statement did.
 *
 * The page decides which tab is open, so a refusal can turn it to Messages;
 * the pane only draws. The Plan tab carries a `gauge` slot above the plan.
 */
export type ResultTab = 'result' | 'plan' | 'messages'

const model = defineModel<ResultTab>({ required: true })

const props = withDefaults(defineProps<{ messages?: number }>(), { messages: 0 })

defineSlots<{
  readout?: () => unknown
  result?: () => unknown
  gauge?: () => unknown
  plan?: () => unknown
  messages?: () => unknown
}>()

const tabs = (): TabOption[] => [
  { id: 'result', label: 'Result' },
  { id: 'plan', label: 'Plan' },
  { id: 'messages', label: 'Messages', ...(props.messages > 0 ? { count: props.messages } : {}) },
]
</script>

<template>
  <section class="result-pane flex min-h-0 flex-1 flex-col border-t border-line">
    <div class="flex flex-none items-end gap-4 border-b border-line px-3">
      <VTabs
        :model-value="model"
        :tabs="tabs()"
        label="Result pane"
        class="flex-none border-b-0"
        @update:model-value="model = $event as ResultTab"
      />
      <div class="readout-slot ml-auto flex h-9 min-w-0 items-center">
        <slot name="readout" />
      </div>
    </div>
    <div v-if="model === 'result'" class="pane-result flex min-h-0 flex-1 flex-col">
      <slot name="result" />
    </div>
    <div v-else-if="model === 'plan'" class="pane-plan min-h-0 flex-1 overflow-auto">
      <slot name="gauge" />
      <slot name="plan" />
    </div>
    <div v-else class="pane-messages min-h-0 flex-1 overflow-auto">
      <slot name="messages" />
    </div>
  </section>
</template>
