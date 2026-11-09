import { refLabel } from './focus'
import type { FocusRef } from './types'

/** A focus as the picker holds it: an object, and a column of it when one is named. */
export type FocusPick = { ref: FocusRef; column: string | null }

const relational = (ref: FocusRef) =>
  ref.kind === 'table' || ref.kind === 'view' || ref.kind === 'matview'

const qualifiedName = (ref: FocusRef) => `${ref.schema}.${ref.name}`

/** What the picker shows for a focus: the object's label, or `schema.table.column`. */
export function focusText(ref: FocusRef | null, column: string | null): string {
  if (ref === null) return ''
  return column === null ? refLabel(ref) : `${qualifiedName(ref)}.${column}`
}

/**
 * The relation a text names by `schema.name`, or by its bare name when only
 * one relation has it. Names are matched as typed, then without case.
 */
export function relationNamed(text: string, refs: readonly FocusRef[]): FocusRef | null {
  const candidates = refs.filter(relational)
  for (const same of [
    (a: string, b: string) => a === b,
    (a: string, b: string) => a.toLowerCase() === b.toLowerCase(),
  ]) {
    const qualified = candidates.find((ref) => same(qualifiedName(ref), text))
    if (qualified !== undefined) return qualified
    const bare = candidates.filter((ref) => same(ref.name, text))
    if (bare.length === 1) return bare[0] ?? null
  }
  return null
}

/**
 * The relation a text is naming a column of: everything before its last dot,
 * when that names one relation.
 */
export function columnOwner(text: string, refs: readonly FocusRef[]): FocusRef | null {
  const dot = text.lastIndexOf('.')
  return dot <= 0 ? null : relationNamed(text.slice(0, dot).trim(), refs)
}

/**
 * What a typed text picks: an object by its label or its name, or a column as
 * `table.column` or `schema.table.column`. A column is taken only when the
 * relation's columns are known and hold it. Anything else picks nothing.
 */
export function resolveFocusText(
  text: string,
  refs: readonly FocusRef[],
  columnsOf: (ref: FocusRef) => readonly string[] | undefined,
): FocusPick | null {
  const typed = text.trim()
  if (typed === '') return null
  const labelled = refs.find((ref) => refLabel(ref) === typed)
  if (labelled !== undefined) return { ref: labelled, column: null }
  const named = relationNamed(typed, refs)
  if (named !== null) return { ref: named, column: null }
  const owner = columnOwner(typed, refs)
  if (owner === null) return null
  const wanted = typed
    .slice(typed.lastIndexOf('.') + 1)
    .trim()
    .toLowerCase()
  const column = columnsOf(owner)?.find((name) => name.toLowerCase() === wanted)
  return column === undefined ? null : { ref: owner, column }
}

/** At most this many suggestions are offered at once. */
export const maxSuggestions = 50

/**
 * What the picker offers for a text: a relation's columns once the text names
 * it and a dot, otherwise the objects whose labels hold the text.
 */
export function focusSuggestions(
  text: string,
  refs: readonly FocusRef[],
  columnsOf: (ref: FocusRef) => readonly string[] | undefined,
): string[] {
  const typed = text.trim().toLowerCase()
  const owner = columnOwner(text.trim(), refs)
  const columns = owner === null ? undefined : columnsOf(owner)
  if (owner !== null && columns !== undefined) {
    const partial = typed.slice(typed.lastIndexOf('.') + 1)
    return columns
      .filter((name) => name.toLowerCase().startsWith(partial))
      .map((name) => `${qualifiedName(owner)}.${name}`)
      .slice(0, maxSuggestions)
  }
  return refs
    .map(refLabel)
    .filter((label) => typed === '' || label.toLowerCase().includes(typed))
    .slice(0, maxSuggestions)
}
