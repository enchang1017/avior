<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { ReadonlySql } from './readonlySql'

/**
 * Read-only SQL, highlighted.
 *
 * A definition can be four hundred lines, and reading one as plain text is the
 * difference between spotting a wrong table name and not. The extensions are
 * composed rather than taken from basicSetup: a viewer has no use for the
 * editing keymaps, the history or the autocomplete, and a search panel that
 * opens on a keystroke in something that cannot be edited reads as a fault.
 *
 * No fixed height. The definition grows the page and the pane scrolls, so a
 * long procedure is read by scrolling the page rather than a box inside it.
 *
 * CodeMirror arrives after the page does; until then the script is plain text
 * in the same face, so reading never waits on it.
 */
const props = defineProps<{ code: string }>()

const host = ref<HTMLDivElement | null>(null)
const ready = ref(false)
let view: ReadonlySql | null = null
let gone = false

onMounted(async () => {
  const { mountReadonlySql } = await import('./readonlySql')
  if (gone || host.value === null) return
  view = mountReadonlySql(host.value, props.code, { gutter: true })
  ready.value = true
})

onBeforeUnmount(() => {
  gone = true
  view?.destroy()
  view = null
})

watch(
  () => props.code,
  (next) => view?.show(next),
)
</script>

<template>
  <!-- Colours live in editorTheme.ts so the editor and the page share one
       palette; only geometry is set here. A viewer has no caret: one blinking
       in text that cannot be edited invites the operator to type into it.
       Important where CodeMirror's own unlayered sheet sets the same thing. -->
  <div
    ref="host"
    class="script overflow-hidden rounded-control border border-line bg-surface [&_.cm-cursor]:hidden! [&_.cm-editor]:font-mono [&_.cm-editor]:text-md [&_.cm-editor.cm-focused]:outline-none!"
  >
    <pre
      v-if="!ready"
      class="m-0 overflow-auto px-2 py-1 font-mono text-md leading-code whitespace-pre-wrap text-fg"
      >{{ props.code }}</pre
    >
  </div>
</template>
