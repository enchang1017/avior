import type { FocusRef, LineageFocusKind, LineageNode } from './types'

export const focusKinds: readonly LineageFocusKind[] = [
  'table',
  'view',
  'matview',
  'sequence',
  'routine',
]

export function isFocusKind(value: string): value is LineageFocusKind {
  return (focusKinds as readonly string[]).includes(value)
}

const focusKindOf = (node: LineageNode): LineageFocusKind =>
  node.kind === 'function' || node.kind === 'procedure' ? 'routine' : node.kind

const sameName = (a: { schema: string; name: string }, b: { schema: string; name: string }) =>
  a.schema === b.schema && a.name === b.name

/**
 * How to name a node the server answered with. A routine carries its
 * signature only when another routine known to the page shares its name,
 * because a name that is unique resolves without one.
 */
export function refOf(node: LineageNode, known: Iterable<LineageNode> = []): FocusRef {
  const kind = focusKindOf(node)
  const ref: FocusRef = { schema: node.schema, name: node.name, kind }
  if (kind !== 'routine') return ref
  const overloaded = [...known].some(
    (other) => other.id !== node.id && focusKindOf(other) === 'routine' && sameName(other, node),
  )
  return overloaded ? { ...ref, signature: node.arguments ?? '' } : ref
}

/** Whether a node is the object a reference names. */
export function names(ref: FocusRef, node: LineageNode): boolean {
  if (!sameName(ref, node) || focusKindOf(node) !== ref.kind) return false
  return ref.signature === undefined || ref.signature === (node.arguments ?? '')
}

/** One string per reference, for a select's value. */
export function refKey(ref: FocusRef): string {
  return JSON.stringify([ref.kind, ref.schema, ref.name, ref.signature ?? null])
}

export function refLabel(ref: FocusRef): string {
  const signature = ref.signature === undefined ? '' : `(${ref.signature})`
  const kind = ref.kind === 'table' ? '' : ` · ${ref.kind}`
  return `${ref.schema}.${ref.name}${signature}${kind}`
}

/** The query parameters that name an object, with a prefix for a path's two ends. */
export function refQuery(ref: FocusRef, prefix: '' | 'from' | 'to' = ''): Record<string, string> {
  const key = (field: string) =>
    prefix === '' ? field : `${prefix}${field.charAt(0).toUpperCase()}${field.slice(1)}`
  return {
    [key('schema')]: ref.schema,
    [key('name')]: ref.name,
    [key('kind')]: ref.kind,
    ...(ref.signature === undefined ? {} : { [key('signature')]: ref.signature }),
  }
}
