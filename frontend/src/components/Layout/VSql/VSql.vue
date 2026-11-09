<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { ReadonlySql } from '@/shared/readonlySql'

/**
 * SQL somebody reads rather than writes.
 *
 * Four places render a statement the operator did not type — the reverse of a
 * write, a request's own statement, a migration, an object's definition — and
 * every one of them was a bare `<pre>`. A forty-line UPDATE with no
 * highlighting is a wall, and the thing a reader is looking for in it is
 * exactly the part highlighting picks out: which table, which columns, what the
 * predicate matches.
 *
 * The same CodeMirror the editor uses, with everything that makes it an editor
 * taken out: no completion, no vim, no gutter, and read-only at both levels —
 * `readOnly` stops the document changing and `editable` stops the caret, so it
 * reads as text rather than as an input somebody's keystrokes vanish into.
 * Sharing the dialect and the theme is the point: a statement looks the same
 * wherever it is shown.
 *
 * CodeMirror is loaded when the first one mounts, not with the page: this
 * component sits in the shared barrel, and a static import put the editor in
 * the first paint of every page, sign-in included. Until it arrives the
 * statement is shown as plain text in the same face, so nothing waits on it.
 */
const props = withDefaults(
  defineProps<{
    code: string
    /**
     * How tall it may grow before it scrolls: a statement in a table row wants
     * less, and one that is the subject of its page wants no limit at all, so
     * the page scrolls once rather than a box scrolling inside it.
     */
    size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
    /**
     * Long lines wrap by default. Off, they keep the shape they were written
     * in and the block scrolls sideways, which is how a wide statement reads.
     */
    wrap?: boolean
  }>(),
  { size: 'xl', wrap: true },
)

/*
 * On the editor rather than the frame, because CodeMirror sizes its scroller
 * to its own box. Important where CodeMirror's injected theme sets the same
 * property: its sheet is unlayered, and an unlayered rule beats any utility.
 * Nothing to focus, so nothing draws a focus ring — a read-only block that
 * outlines itself reads as an input the reader failed to type into.
 */
const heights = {
  sm: '[&_.cm-editor]:max-h-48',
  md: '[&_.cm-editor]:max-h-56',
  lg: '[&_.cm-editor]:max-h-64',
  xl: '[&_.cm-editor]:max-h-72',
  full: '',
} as const

const host = ref<HTMLDivElement | null>(null)
const ready = ref(false)
let view: ReadonlySql | null = null
let gone = false

onMounted(async () => {
  const { mountReadonlySql } = await import('@/shared/readonlySql')
  if (gone || host.value === null) return
  view = mountReadonlySql(host.value, props.code, { wrap: props.wrap })
  ready.value = true
})

// The document is replaced rather than the view rebuilt: a reader who has
// scrolled a long statement keeps their place when the row beside it refreshes.
watch(
  () => props.code,
  (code) => view?.show(code),
)

onBeforeUnmount(() => {
  gone = true
  view?.destroy()
  view = null
})
</script>

<template>
  <div
    ref="host"
    class="sql-view overflow-hidden rounded-control border border-line bg-surface [&_.cm-editor]:text-sm [&_.cm-editor.cm-focused]:outline-none! [&_.cm-scroller]:overflow-auto"
    :class="heights[props.size]"
  >
    <pre
      v-if="!ready"
      class="m-0 overflow-auto px-1.5 py-1 font-mono text-sm leading-code text-fg"
      :class="props.wrap ? 'whitespace-pre-wrap' : 'whitespace-pre'"
      >{{ props.code }}</pre
    >
  </div>
</template>
