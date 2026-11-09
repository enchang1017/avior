<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { VBanner, VButton, VIcon } from '@/components'
import { groupEntries } from './impact'
import { nodeLabel } from './scene'
import type { LineageColumn } from './types'
import UsageList from './UsageList.vue'
import { useColumnPages } from './useColumnPages'
import type { ColumnQuestion } from './useLineageColumn'

/** What touches a column or a table, by kind, then its triggers and its indirect reach. */
const props = defineProps<{
  question: ColumnQuestion
  sentence: string
  answer: LineageColumn
}>()

const emit = defineEmits<{ open: [id: string] }>()

const pages = useColumnPages(
  () => props.question,
  () => props.answer,
)
const groups = computed(() => groupEntries(pages.entries.value))
const triggers = computed(() => props.answer.triggers ?? [])
const indirect = computed(() => props.answer.indirect ?? [])
const indirectTotal = computed(() => props.answer.indirectTotal ?? indirect.value.length)
const indirectNote = computed(() =>
  indirectTotal.value > indirect.value.length
    ? `${indirect.value.length} of ${indirectTotal.value} shown.`
    : '',
)

const copied = ref('')
async function copy(): Promise<void> {
  const { impactMarkdown } = await import('./impactMarkdown')
  const text = impactMarkdown({
    sentence: props.sentence,
    groups: groups.value,
    triggers: triggers.value,
    indirect: indirect.value,
    indirectTotal: indirectTotal.value,
  })
  try {
    await navigator.clipboard.writeText(text)
    copied.value = 'Copied as Markdown.'
  } catch {
    copied.value = 'The browser would not copy. Select the text instead.'
  }
}
watch(
  () => props.sentence,
  () => (copied.value = ''),
)
</script>

<template>
  <div class="impact-detail flex flex-col">
    <div class="flex items-center gap-2 px-3 pt-3">
      <VButton size="sm" icon="copy" class="copy" @click="copy">Copy as Markdown</VButton>
      <span v-if="copied" class="text-2xs text-fg-soft" role="status">{{ copied }}</span>
    </div>
    <UsageList
      v-for="group in groups"
      :key="group.title"
      :title="group.title"
      :rows="group.entries.map((entry) => ({ entry }))"
      @open="emit('open', $event)"
    />
    <p v-if="groups.length === 0" class="m-0 px-3 pt-3 text-xs text-fg-muted">
      Nothing the database holds reads or writes it.
    </p>
    <div v-if="pages.next.value !== undefined" class="flex px-3 py-2">
      <VButton size="sm" :disabled="pages.loading.value" @click="pages.load()">
        <VIcon v-if="pages.loading.value" name="loader" spin size="sm" />
        Load more
      </VButton>
    </div>
    <VBanner v-if="pages.failure.value" icon="circle-x">{{ pages.failure.value }}</VBanner>

    <section v-if="triggers.length > 0" class="triggers flex flex-col" aria-label="Triggers">
      <h3 class="m-0 flex items-center justify-between px-3 pt-3 pb-1 eyebrow">
        Triggers <span class="font-mono">{{ triggers.length }}</span>
      </h3>
      <ul class="m-0 list-none p-0">
        <li
          v-for="trigger in triggers"
          :key="trigger.name"
          class="flex items-baseline gap-2 border-b border-line px-3 py-1.5 font-mono text-sm"
        >
          {{ trigger.name }}
          <button
            type="button"
            class="cursor-pointer truncate border-0 bg-transparent p-0 text-left font-mono text-2xs text-fg-soft hover:text-primary"
            @click="emit('open', trigger.function.id)"
          >
            runs {{ nodeLabel(trigger.function) }}
          </button>
          <span class="ml-auto text-2xs text-fg-muted">{{ trigger.events.join(', ') }}</span>
        </li>
      </ul>
    </section>

    <UsageList
      v-if="indirect.length > 0"
      class="indirect"
      title="Indirect"
      :rows="indirect"
      :note="indirectNote"
      @open="emit('open', $event)"
    />
  </div>
</template>
