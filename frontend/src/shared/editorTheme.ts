import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { EditorView } from '@codemirror/view'
import { tags } from '@lezer/highlight'
import type { Extension } from '@codemirror/state'

/**
 * The editor's theme, written against the same CSS variables as the rest of the
 * page.
 *
 * CodeMirror accepts any CSS value, so a theme built from `var(...)` follows the
 * palette on its own: switching light to dark needs no editor rebuild, and the
 * `system` setting tracks the operating system live without a listener. Hard
 * coding two colour sets here would mean tearing the editor down on every
 * toggle, which loses the cursor and the undo history.
 */
const chrome = EditorView.theme({
  '&': {
    color: 'var(--color-fg)',
    backgroundColor: 'var(--color-surface)',
  },
  '.cm-content': {
    caretColor: 'var(--color-primary)',
  },
  '.cm-cursor, .cm-dropCursor': {
    borderLeftColor: 'var(--color-primary)',
  },
  /*
   * The selection, at CodeMirror's own specificity.
   *
   * Its base theme colours a focused selection through
   * `&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground`
   * — five classes — so the obvious `.cm-selectionBackground` loses to it and
   * the editor keeps its default `#d7d4f0`, which is invisible on white and on
   * the dark ground alike. The long form is repeated here to match it.
   */
  '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground': {
    backgroundColor: 'var(--color-cm-selection)',
  },
  '.cm-selectionBackground, .cm-content ::selection': {
    backgroundColor: 'var(--color-cm-selection)',
  },
  // Translucent. The selection layer sits at z-index -2, behind the lines, so
  // an opaque active line hides every selection made on it — which is every
  // selection somebody makes by double-clicking a word.
  '.cm-activeLine': {
    backgroundColor: 'var(--color-cm-active-line)',
  },
  '.cm-gutters': {
    color: 'var(--color-fg-muted)',
    backgroundColor: 'var(--color-surface-sunken)',
    borderRight: '1px solid var(--color-line)',
  },
  '.cm-activeLineGutter': {
    backgroundColor: 'var(--color-primary-wash)',
    color: 'var(--color-fg-soft)',
  },
  '.cm-foldPlaceholder': {
    backgroundColor: 'var(--color-surface-sunken)',
    border: '1px solid var(--color-line-strong)',
    color: 'var(--color-fg-soft)',
  },
  '.cm-matchingBracket, .cm-nonmatchingBracket': {
    backgroundColor: 'var(--color-primary-wash)',
    outline: '1px solid var(--color-line-strong)',
  },
  '.cm-tooltip': {
    backgroundColor: 'var(--color-surface)',
    border: '1px solid var(--color-line-strong)',
    borderRadius: 'var(--radius-control)',
    boxShadow: 'var(--shadow-sm)',
    color: 'var(--color-fg)',
  },
  // basicSetup brings autocompletion, and its popup carries its own light
  // colours. Left alone it is a white box floating on a dark editor.
  '.cm-tooltip.cm-tooltip-autocomplete > ul': {
    fontFamily: 'inherit',
    maxHeight: '12rem',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li': {
    padding: '0.125rem 0.375rem',
    color: 'var(--color-fg)',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]': {
    backgroundColor: 'var(--color-primary)',
    color: 'var(--color-on-primary)',
  },
  '.cm-completionIcon': {
    color: 'var(--color-fg-muted)',
    opacity: '1',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected] .cm-completionIcon': {
    color: 'var(--color-on-primary)',
  },
  '.cm-completionMatchedText': {
    color: 'var(--color-cm-keyword)',
    textDecoration: 'none',
    fontWeight: '600',
  },
  '.cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected] .cm-completionMatchedText': {
    color: 'var(--color-on-primary)',
    textDecoration: 'underline',
  },
  '.cm-completionDetail': {
    color: 'var(--color-fg-muted)',
    fontStyle: 'normal',
  },
  '.cm-panels': {
    backgroundColor: 'var(--color-surface-sunken)',
    color: 'var(--color-fg)',
  },
})

/**
 * Syntax colours.
 *
 * A bracket-quoted identifier gets its own colour because that is what the
 * generated statement is made of, and telling `[Member]` from `Member` at a
 * glance is how an operator spots a name the generator did not quote.
 */
const highlight = HighlightStyle.define([
  { tag: tags.keyword, color: 'var(--color-cm-keyword)', fontWeight: '600' },
  { tag: tags.string, color: 'var(--color-cm-string)' },
  // lang-sql tags a bracket-quoted identifier as a special string. It gets the
  // identifier colour rather than the literal colour: telling [Member] from
  // 'Member' at a glance is how an operator spots a quoting mistake.
  { tag: tags.special(tags.string), color: 'var(--color-cm-bracket-name)' },
  { tag: [tags.number, tags.bool, tags.null], color: 'var(--color-cm-number)' },
  { tag: [tags.comment, tags.lineComment, tags.blockComment], color: 'var(--color-cm-comment)' },
  { tag: [tags.operator, tags.punctuation, tags.separator], color: 'var(--color-cm-operator)' },
  { tag: tags.variableName, color: 'var(--color-fg)' },
  { tag: [tags.typeName, tags.className], color: 'var(--color-cm-bracket-name)' },
  { tag: tags.invalid, color: 'var(--color-danger)' },
])

/** editorTheme is the extension the editor installs. */
export const editorTheme: Extension = [chrome, syntaxHighlighting(highlight)]
