<script setup lang="ts">
import { computed } from 'vue'

import VIcon from '../../Media/VIcon/VIcon.vue'
import { parseQuery, withoutScope } from './scopes'

/**
 * Search with scopes: `database:aviorfixture` becomes a chip, and everything
 * else is words. The chips are the query's own tokens drawn back at the reader,
 * each removable, so what is filtering the list is always on screen.
 */
const model = defineModel<string>({ required: true })

const props = defineProps<{
  id: string
  /** The keys that are scopes on this page, in the order they are suggested. */
  keys: readonly string[]
  placeholder: string
}>()

const parsed = computed(() => parseQuery(model.value, props.keys))

function remove(index: number): void {
  model.value = withoutScope(model.value, props.keys, index)
}
</script>

<template>
  <div
    class="scope-search flex h-control w-full min-w-0 items-center gap-1.5 rounded-control border border-line bg-surface-sunken px-2 focus-within:border-primary hover:border-line-strong"
  >
    <VIcon name="search" size="sm" class="text-fg-muted" />
    <span
      v-for="(scope, index) in parsed.scopes"
      :key="`${scope.key}-${index}`"
      class="scope inline-flex h-5.5 flex-none items-center gap-1 rounded-control border border-line-strong bg-surface px-1.5 text-xs"
    >
      <span class="text-fg-muted">{{ scope.key }}:</span>
      <span class="font-mono text-fg">{{ scope.value }}</span>
      <button
        type="button"
        class="grid cursor-pointer place-items-center border-0 bg-transparent p-0 text-fg-muted hover:text-fg"
        :aria-label="`Remove ${scope.key}:${scope.value}`"
        @click="remove(index)"
      >
        <VIcon name="x" size="xs" />
      </button>
    </span>
    <input
      :id="props.id"
      v-model="model"
      class="h-full min-w-0 flex-1 border-0 bg-transparent font-[inherit] text-sm text-fg outline-none placeholder:text-fg-muted"
      type="search"
      autocomplete="off"
      spellcheck="false"
      :placeholder="props.placeholder"
    />
  </div>
</template>
