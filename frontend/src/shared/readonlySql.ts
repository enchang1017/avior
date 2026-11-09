import { EditorView, lineNumbers } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import { sql, PostgreSQL } from '@codemirror/lang-sql'

import { editorTheme } from './editorTheme'

/**
 * A CodeMirror view nobody can type into, for SQL somebody reads.
 *
 * A module of its own so the components that show one load it on demand: the
 * editor is most of the weight of the pages that use it, and none of the rest.
 */
export type ReadonlySql = {
  show(code: string): void
  destroy(): void
}

export function mountReadonlySql(
  parent: HTMLElement,
  code: string,
  options: { gutter?: boolean; wrap?: boolean } = {},
): ReadonlySql {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: code,
      extensions: [
        options.gutter === true ? lineNumbers() : [],
        sql({ dialect: PostgreSQL, upperCaseKeywords: true }),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        options.wrap === false ? [] : EditorView.lineWrapping,
        editorTheme,
      ],
    }),
  })
  return {
    show(next) {
      if (view.state.doc.toString() === next) return
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } })
    },
    destroy() {
      view.destroy()
    },
  }
}
