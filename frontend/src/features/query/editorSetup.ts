import { type Extension, Compartment, EditorState } from '@codemirror/state'
import {
  drawSelection,
  dropCursor,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  type EditorView,
} from '@codemirror/view'
import {
  bracketMatching,
  defaultHighlightStyle,
  foldGutter,
  foldKeymap,
  indentOnInput,
  syntaxHighlighting,
} from '@codemirror/language'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { closeBrackets, closeBracketsKeymap, completionKeymap } from '@codemirror/autocomplete'

/**
 * CodeMirror's basic setup, spelled out, less what the console does not use.
 *
 * Linting is gone: nothing lints SQL here. Alt-drag rectangular selection is
 * gone too: it was the cheapest thing left to cut to keep the console's first
 * load under 200 KB with the editor in it. Search stays, but is fetched the
 * first time somebody presses Mod-F rather than with the page — it is a tenth
 * of the editor's weight and the console's first load is held to 200 KB
 * (ADR 0009 P2). The first press opens the panel once it has arrived.
 */
const search = new Compartment()

async function openSearch(view: EditorView): Promise<void> {
  const module = await import('@codemirror/search')
  view.dispatch({
    effects: search.reconfigure([
      module.search({ top: true }),
      module.highlightSelectionMatches(),
      keymap.of(module.searchKeymap),
    ]),
  })
  module.openSearchPanel(view)
}

const searchOnDemand = keymap.of([
  {
    key: 'Mod-f',
    run: (view) => {
      void openSearch(view)
      return true
    },
  },
])

export function editorSetup(): Extension[] {
  return [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    foldGutter(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
    bracketMatching(),
    closeBrackets(),
    highlightActiveLine(),
    search.of([]),
    searchOnDemand,
    keymap.of([
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...historyKeymap,
      ...foldKeymap,
      ...completionKeymap,
    ]),
  ]
}
