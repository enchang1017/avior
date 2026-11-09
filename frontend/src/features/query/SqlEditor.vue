<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EditorView, keymap } from '@codemirror/view'
import {
  autocompletion,
  closeCompletion,
  completionStatus,
  type CompletionSource,
} from '@codemirror/autocomplete'
import { Compartment, EditorState, Prec } from '@codemirror/state'
import type { getCM } from '@replit/codemirror-vim'
import type * as LangSqlModule from '@codemirror/lang-sql'
import type { SQLConfig, SQLNamespace } from '@codemirror/lang-sql'

import { editorSetup } from './editorSetup'
import { editorTheme } from '@/shared/editorTheme'
import type { VimMode } from '@/shared/vimMode'

/**
 * A thin CodeMirror wrapper. The predecessor showed line numbers and highlighted SQL,
 * and losing that when reading a forty-column statement is the difference
 * between spotting a wrong table name and not.
 *
 * The dialect is PostgreSQL so double-quoted identifiers, dollar-quoted bodies
 * and its keywords are highlighted the way the operator wrote them.
 */
const props = withDefaults(
  defineProps<{
    modelValue: string
    /**
     * What the operator can name, as `schema.table` to its columns.
     *
     * Completion is offered from the catalogue rather than from a fixed word
     * list, so what it suggests is what the database actually has — a name it
     * offers is a name that exists, which is the only version of this feature
     * worth having.
     */
    schema?: SQLNamespace
    /**
     * The table and schema whose columns complete unqualified.
     *
     * Two names rather than one qualified one: lang-sql looks each up as a
     * single child of the level above, so `public.member` would be a table
     * whose name contains a dot rather than a table in a schema.
     */
    defaultTable?: string
    defaultSchema?: string
    /** FEAT-057. Modal editing, off unless the operator asked for it. */
    vimEnabled?: boolean
  }>(),
  { schema: () => ({}), defaultTable: '', defaultSchema: '', vimEnabled: false },
)
const emit = defineEmits<{
  'update:modelValue': [value: string]
  /** The mode the editor is in, for the indicator the page draws (AC-4). */
  'update:vimMode': [mode: VimMode]
}>()

const host = ref<HTMLDivElement | null>(null)
let view: EditorView | null = null

/*
 * The language extension is reconfigured rather than rebuilt, so choosing a
 * different table does not throw the editor away and take the operator's cursor
 * and undo history with it.
 */
const completion = new Compartment()
const language = new Compartment()
/*
 * Vim is a compartment for the same reason the language is one: switching it
 * has to leave the statement, the cursor and the undo history where they were
 * (AC-2). Rebuilding the editor would throw all three away, and a toggle that
 * loses a half-written statement is worse than no toggle.
 */
const modal = new Compartment()

/*
 * The SQL language — the grammar, the dialect, the completion sources — is a
 * fifth of the editor's weight, and none of it is needed to accept the first
 * keystroke. It is fetched as the editor mounts and switched in when it
 * arrives, highlighting what is already there (ADR 0009 P2, P9).
 */
type LangSql = typeof LangSqlModule
let lang: LangSql | null = null

function sqlConfig(module: LangSql): SQLConfig {
  return {
    dialect: module.PostgreSQL,
    schema: props.schema,
    ...(props.defaultTable === '' ? {} : { defaultTable: props.defaultTable }),
    ...(props.defaultSchema === '' ? {} : { defaultSchema: props.defaultSchema }),
  }
}

function sqlDialect() {
  return lang === null ? [] : lang.sql({ ...sqlConfig(lang), upperCaseKeywords: true })
}

/**
 * How many characters are typed before suggestions are worth computing.
 *
 * One letter matches eighty-five keywords, and producing that list blocks the
 * main thread long enough to swallow the next keystroke — measured at 375ms
 * before this, and unchanged by a production build, so it is the work and not
 * the bundling. Two letters is where a list starts being an answer rather than
 * an alphabet.
 */
const minimumPrefix = 2

/**
 * Wraps a completion source so it stays quiet until there is enough to go on.
 *
 * Explicit requests are always served: somebody who pressed the shortcut is
 * asking for the whole list and can wait for it.
 */
function afterEnoughTyping(source: CompletionSource): CompletionSource {
  return (context) => {
    if (context.explicit) return source(context)
    const word = context.matchBefore(/[\w$]+/)
    if (word === null || word.to - word.from < minimumPrefix) return null
    return source(context)
  }
}

function completionSources() {
  if (lang === null) return []
  return autocompletion({
    // Nobody scans eighty-five. The rest are still matched and still reachable
    // by typing another letter, they are just not drawn.
    maxRenderedOptions: 20,
    // A typist mid-word is not asking for suggestions yet. This keeps a fast
    // one from computing a list per keystroke and throwing each away.
    activateOnTypingDelay: 150,
    override: [
      afterEnoughTyping(lang.schemaCompletionSource(sqlConfig(lang))),
      afterEnoughTyping(lang.keywordCompletionSource(lang.PostgreSQL, true)),
    ],
  })
}

let gone = false

function createView(): void {
  if (host.value === null || gone) return
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        // Before the setup, which is what the package requires: its keymap has
        // to see a key before the default one does, or normal-mode letters are
        // inserted as text.
        /*
         * FEAT-057 AC-7. Escape belongs to whichever of the two is showing.
         *
         * Vim's keymap takes Escape to leave insert mode, and it does so even
         * when the completion list is open — so dismissing a suggestion also
         * kicked the operator into normal mode, and the next letters they typed
         * became commands. This runs first and only when there is a list to
         * close, so with none open Escape falls through to Vim unchanged.
         */
        Prec.highest(
          keymap.of([
            {
              key: 'Escape',
              run: (target: EditorView) =>
                completionStatus(target.state) !== null && closeCompletion(target),
            },
          ]),
        ),
        modal.of([]),
        editorSetup(),
        // The setup installs no completion; this is the only one.
        completion.of(completionSources()),
        editorTheme,
        language.of(sqlDialect()),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) emit('update:modelValue', update.state.doc.toString())
        }),
      ],
    }),
  })
  if (props.vimEnabled) void switchVim(true)
  void loadLanguage()
}

/*
 * The editor is built the frame after the page first draws, not while it
 * mounts. Building it measures the DOM, and doing that inside the mount forced
 * the whole page's first layout into the same task: one 170 ms long task at 4x
 * CPU where there should be two short ones (ADR 0009 P11).
 */
onMounted(() => {
  requestAnimationFrame(() => setTimeout(createView, 0))
})

async function loadLanguage(): Promise<void> {
  lang = await import('@codemirror/lang-sql')
  view?.dispatch({
    effects: [language.reconfigure(sqlDialect()), completion.reconfigure(completionSources())],
  })
}

/*
 * Reports the mode to the page.
 *
 * Through the CodeMirror 5 API the package carries over, which is where the
 * mode lives — there is no CM6 state field for it and no status bar in the
 * package, so an indicator has to be built from this event.
 */
/*
 * Vim is a sixth of the editor's weight and most operators never turn it on,
 * so it is fetched the first time somebody does rather than with the page.
 */
async function switchVim(on: boolean): Promise<void> {
  if (!on) {
    view?.dispatch({ effects: modal.reconfigure([]) })
    // Leaving insert mode on the way out, so turning Vim back on does not
    // resume in a mode nobody chose.
    emit('update:vimMode', '')
    return
  }
  const keys = await import('@replit/codemirror-vim')
  if (view === null || !props.vimEnabled) return
  view.dispatch({ effects: modal.reconfigure(keys.vim()) })
  watchMode(keys.getCM(view))
}

function watchMode(cm: ReturnType<typeof getCM>) {
  if (view === null) return
  if (!cm) return
  emit('update:vimMode', 'normal')
  cm.on('vim-mode-change', (event: { mode: string }) => {
    emit('update:vimMode', event.mode as VimMode)
  })
}

onBeforeUnmount(() => {
  gone = true
  view?.destroy()
  view = null
})

// The catalogue arrives after the editor is mounted, and changes whenever the
// operator picks another table.
watch(
  () => [props.schema, props.defaultTable, props.defaultSchema],
  () =>
    view?.dispatch({
      effects: [language.reconfigure(sqlDialect()), completion.reconfigure(completionSources())],
    }),
  { deep: true },
)

// FEAT-057 AC-2. Reconfigured rather than rebuilt, so the switch costs nothing.
watch(
  () => props.vimEnabled,
  (on) => void switchVim(on),
)

// Only push in changes that came from elsewhere, or every keystroke would
// re-dispatch its own text and the cursor would jump to the end.
watch(
  () => props.modelValue,
  (next) => {
    if (view === null) return
    const current = view.state.doc.toString()
    if (current === next) return
    view.dispatch({ changes: { from: 0, to: current.length, insert: next } })
  },
)
</script>

<template>
  <!-- Colours live in editorTheme.ts so the editor and the page share one
       palette; only geometry is set here, with the size one step below the body
       scale and in rem so it moves with it. The focus outline is removed with
       `!` because CodeMirror injects its own styles outside any layer, where
       they outrank every utility. -->
  <div
    ref="host"
    class="editor min-h-52 overflow-hidden bg-surface [&_.cm-editor]:h-52 [&_.cm-editor]:font-mono [&_.cm-editor]:text-md [&_.cm-editor.cm-focused]:outline-none!"
  />
</template>
