import { keyOf, type NodeRef, type SchemaEdge } from './graph'

const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`

function names(refs: NodeRef[]): string {
  const unique = [...new Set(refs.map((ref) => ref.name))].sort()
  if (unique.length === 0) return 'nothing'
  if (unique.length === 1) return unique[0] ?? ''
  return `${unique.slice(0, -1).join(', ')} and ${unique[unique.length - 1] ?? ''}`
}

/** The chart in words, for the screen reader that cannot see it. */
export function describeChart(
  database: string,
  objects: number,
  edges: readonly SchemaEdge[],
  target: NodeRef | null,
): string {
  const head = `${database}: ${plural(objects, 'object', 'objects')}, ${plural(edges.length, 'relationship', 'relationships')}.`
  if (target === null) return head
  const key = keyOf(target)
  const references = edges.filter((edge) => keyOf(edge.from) === key).map((edge) => edge.to)
  const referencedBy = edges.filter((edge) => keyOf(edge.to) === key).map((edge) => edge.from)
  const by = referencedBy.length === 0 ? 'by nothing' : `by ${names(referencedBy)}`
  return `${head} ${target.name} references ${names(references)} and is referenced ${by}.`
}

/** The caption under the chart, in full: what the pane cuts short, the title holds. */
export function chartCaption(database: string, objects: number, note: string): string {
  return [database, `${objects} objects`, 'fk/view edges', note].filter(Boolean).join(' · ')
}

/** "showing 150 of 612", or nothing when every object is drawn. */
export function showingNote(drawn: number, total: number): string {
  return drawn < total ? `showing ${drawn} of ${total}` : ''
}

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 2 })

/** A row count as the chart prints it: 1.25M, 84K, or nothing when never analysed. */
export function rowsLabel(rows: number | null): string {
  return rows === null || rows < 0 ? '' : compact.format(rows)
}
