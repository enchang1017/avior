import type { LineageEdge, LineageRoute } from './types'

/** One hop of a route, in the order it is walked, and what joins its two ends. */
export type Hop = {
  from: string
  to: string
  word: string
  /** Walked against the edge's arrow: routes are found over edges either way. */
  against: boolean
  /** The edge that joins them, and what it rests on. */
  edge: LineageEdge | null
}

function wordOf(edge: LineageEdge): string {
  switch (edge.kind) {
    case 'view_read':
    case 'routine_read':
    case 'policy_read':
      return 'reads'
    case 'routine_write':
      return 'writes'
    case 'foreign_key':
      return edge.access === 'write' ? 'FK cascade' : 'FK'
    case 'trigger':
      return 'trigger'
    case 'call':
      return 'calls'
    case 'default':
      return 'default'
    case 'inheritance':
      return 'inherits'
  }
}

/** The route as hops: each pair of neighbours in it, the edge between them, and its direction. */
export function hopsOf(route: LineageRoute): Hop[] {
  const hops: Hop[] = []
  for (let index = 0; index + 1 < route.nodes.length; index += 1) {
    const from = route.nodes[index] ?? ''
    const to = route.nodes[index + 1] ?? ''
    const joins = (edge: LineageEdge) =>
      (edge.from === from && edge.to === to) || (edge.from === to && edge.to === from)
    const edge = [route.edges[index], ...route.edges].find(
      (one): one is LineageEdge => one !== undefined && joins(one),
    )
    hops.push({
      from,
      to,
      word: edge === undefined ? '?' : wordOf(edge),
      against: edge !== undefined && edge.from !== from,
      edge: edge ?? null,
    })
  }
  return hops
}
