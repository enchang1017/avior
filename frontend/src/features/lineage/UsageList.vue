<script setup lang="ts">
import ConfidenceBadge from './ConfidenceBadge.vue'
import { countText, viaText, whereText, type Hop } from './impact'
import { nodeLabel } from './scene'
import type { LineageColumnEntry } from './types'

defineProps<{
  title: string
  rows: readonly { entry: LineageColumnEntry; via?: readonly Hop[] }[]
  /** Said under the heading, when the list is cut or needs a word. */
  note?: string
}>()

const emit = defineEmits<{ open: [id: string] }>()
</script>

<template>
  <section class="usage flex flex-col" :aria-label="title">
    <h3 class="m-0 flex items-center justify-between px-3 pt-3 pb-1 eyebrow">
      {{ title }}
      <span class="count font-mono normal-case">{{ countText(rows.map((row) => row.entry)) }}</span>
    </h3>
    <p v-if="note" class="note m-0 px-3 pb-1 text-xs text-fg-soft">{{ note }}</p>
    <ul class="m-0 list-none p-0">
      <li
        v-for="(row, index) in rows"
        :key="`${row.entry.object.id}:${row.entry.kind}:${row.entry.line ?? index}:${index}`"
        class="entry flex items-start gap-2 border-b border-line px-3 py-1.5"
      >
        <div class="flex min-w-0 flex-1 flex-col">
          <button
            type="button"
            class="cursor-pointer truncate border-0 bg-transparent p-0 text-left font-mono text-sm text-fg hover:text-primary"
            @click="emit('open', row.entry.object.id)"
          >
            {{ nodeLabel(row.entry.object) }}
            <span class="text-2xs text-fg-muted">{{ row.entry.object.kind }}</span>
            <span v-if="row.entry.via" class="via ml-1 text-2xs text-fg-soft">
              · {{ row.entry.via }}
            </span>
          </button>
          <span v-if="row.via" class="chain font-mono text-2xs text-fg-soft">
            {{ viaText(row.via) }}
          </span>
          <span class="truncate font-mono text-2xs text-fg-muted" :title="whereText(row.entry)">
            {{ whereText(row.entry) }}
          </span>
        </div>
        <ConfidenceBadge :confidence="row.entry.confidence" />
      </li>
    </ul>
  </section>
</template>
