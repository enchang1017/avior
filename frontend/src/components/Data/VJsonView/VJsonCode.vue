<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { json } from '@codemirror/lang-json'
import { editorSetup } from '@/features/query/editorSetup'
import { editorTheme } from '@/shared/editorTheme'

const props = withDefaults(defineProps<{ value: string; readOnly?: boolean }>(), { readOnly: true })
const emit = defineEmits<{ 'update:value': [value: string] }>()
const host = ref<HTMLElement | null>(null)
let view: EditorView | null = null
onMounted(() => {
  if (!host.value) return
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.value,
      extensions: [
        ...editorSetup(),
        editorTheme,
        json(),
        EditorView.lineWrapping,
        EditorState.readOnly.of(props.readOnly),
        EditorView.editable.of(!props.readOnly),
        EditorView.contentAttributes.of({
          'aria-label': props.readOnly ? 'JSON value' : 'Edit JSON value',
        }),
        EditorView.theme({
          '&': { height: 'calc(var(--spacing) * 80)' },
          '.cm-scroller': { overflow: 'auto' },
        }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !props.readOnly)
            emit('update:value', update.state.doc.toString())
        }),
      ],
    }),
  })
})
watch(
  () => props.value,
  (value) => {
    if (view && value !== view.state.doc.toString())
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
  },
)
onBeforeUnmount(() => view?.destroy())
defineExpose({ currentText: () => view?.state.doc.toString() ?? props.value })
</script>
<template>
  <div ref="host" class="min-w-0 border border-line font-mono text-sm" />
</template>
