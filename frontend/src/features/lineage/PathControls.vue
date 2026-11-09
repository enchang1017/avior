<script setup lang="ts">
import { VFilterSelect } from '@/components'
import PendingSelect from './PendingSelect.vue'
import { focusSuggestions, resolveFocusText } from './focusText'
import type { FocusRef } from './types'
import { useTypedPick } from './useTypedPick'

/** Path mode's own controls: the object to trace a route to, and the most hops. */
const props = defineProps<{
  focusRefs: readonly FocusRef[]
  toValue: string
  maxHops: number
  pending?: boolean
}>()

const emit = defineEmits<{ to: [ref: FocusRef | null]; maxHops: [hops: number] }>()

const noColumns = () => undefined
const to = useTypedPick(
  () => props.toValue,
  (text) => focusSuggestions(text, props.focusRefs, noColumns),
  (text) => {
    const pick = resolveFocusText(text, props.focusRefs, noColumns)
    if (text.trim() === '' || pick !== null) emit('to', pick?.ref ?? null)
    return text.trim() === '' || pick !== null
  },
)

const segment = (on: boolean) =>
  on
    ? 'border-primary-soft bg-primary-wash text-primary'
    : 'border-line bg-transparent text-fg-soft hover:text-fg'
</script>

<template>
  <div class="contents">
    <div class="flex items-center gap-2">
      <label class="eyebrow" for="lineage-to">To</label>
      <div class="w-64">
        <PendingSelect v-if="pending" label="Loading objects" />
        <VFilterSelect
          v-else
          id="lineage-to"
          v-model="to.typed.value"
          size="sm"
          :options="to.options.value"
          :match-typed="false"
          empty-label="Choose an object…"
          @pick="to.pick"
        />
      </div>
      <span v-if="to.unresolved.value" class="unresolved text-xs text-danger" role="status">
        No object by that name
      </span>
    </div>
    <div class="flex items-center gap-2" role="group" aria-label="Most hops">
      <span class="eyebrow">Max</span>
      <div class="flex">
        <button
          v-for="choice in [3, 4, 5]"
          :key="choice"
          type="button"
          class="max-hops h-control-sm w-7 cursor-pointer border font-mono text-xs ease-colours first:rounded-l-control last:rounded-r-control"
          :class="segment(maxHops === choice)"
          :aria-pressed="maxHops === choice"
          @click="emit('maxHops', choice)"
        >
          {{ choice }}
        </button>
      </div>
    </div>
  </div>
</template>
