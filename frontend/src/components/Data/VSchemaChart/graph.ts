import type { components } from '@/gen/api'

export type SchemaGraph = components['schemas']['SchemaGraph']
export type SchemaNode = components['schemas']['SchemaNode']
export type SchemaEdge = components['schemas']['SchemaEdge']
export type NodeRef = components['schemas']['SchemaNodeRef']

export const maxDrawnNodes = 150
export const maxDrawnEdges = 300

export type Drawn = {
  nodes: SchemaNode[]
  edges: SchemaEdge[]
  /** Objects the database holds, as the server counted them before its own cap. */
  total: number
}

export function keyOf(ref: NodeRef): string {
  return `${ref.schema}.${ref.name}`
}

export type Adjacency = Map<string, Set<string>>

export function adjacency(edges: readonly SchemaEdge[]): Adjacency {
  const around: Adjacency = new Map()
  const link = (a: string, b: string) => {
    const set = around.get(a) ?? new Set<string>()
    set.add(b)
    around.set(a, set)
  }
  for (const edge of edges) {
    const from = keyOf(edge.from)
    const to = keyOf(edge.to)
    if (from === to) continue
    link(from, to)
    link(to, from)
  }
  return around
}

function degreeOf(around: Adjacency, key: string): number {
  return around.get(key)?.size ?? 0
}

/** Most connected first, then by name, so the same graph always keeps the same objects. */
function byDegree(around: Adjacency): (a: string, b: string) => number {
  return (a, b) => degreeOf(around, b) - degreeOf(around, a) || (a < b ? -1 : a > b ? 1 : 0)
}

function neighbourhood(target: string, around: Adjacency, limit: number): string[] {
  const order = byDegree(around)
  const first = [...(around.get(target) ?? [])].sort(order)
  const kept = [target, ...first].slice(0, limit)
  if (kept.length >= limit) return kept
  const seen = new Set(kept)
  const second = new Set<string>()
  for (const near of first) {
    for (const far of around.get(near) ?? []) if (!seen.has(far)) second.add(far)
  }
  return [...kept, ...[...second].sort(order)].slice(0, limit)
}

function edgesWithin(
  edges: readonly SchemaEdge[],
  kept: Set<string>,
  around: Adjacency,
  target: string | null,
): SchemaEdge[] {
  const inside = edges.filter((edge) => kept.has(keyOf(edge.from)) && kept.has(keyOf(edge.to)))
  if (inside.length <= maxDrawnEdges) return inside
  const weight = (edge: SchemaEdge) => {
    const from = keyOf(edge.from)
    const to = keyOf(edge.to)
    const touches = from === target || to === target ? 1_000_000 : 0
    return touches + degreeOf(around, from) + degreeOf(around, to)
  }
  return inside
    .map((edge, index) => ({ edge, index, weight: weight(edge) }))
    .sort((a, b) => b.weight - a.weight || a.index - b.index)
    .slice(0, maxDrawnEdges)
    .map((entry) => entry.edge)
}

/**
 * What the chart draws of a graph: everything when it fits, otherwise the
 * target and its neighbours up to the cap, or the most connected objects when
 * nothing is targeted.
 */
export function drawnPart(graph: SchemaGraph, target: NodeRef | null): Drawn {
  const around = adjacency(graph.edges)
  const byKey = new Map(graph.nodes.map((node) => [keyOf(node), node]))
  const targetKey = target !== null && byKey.has(keyOf(target)) ? keyOf(target) : null
  const total = Math.max(graph.total.nodes, graph.nodes.length)

  let keys: string[]
  if (graph.nodes.length <= maxDrawnNodes) keys = [...byKey.keys()]
  else if (targetKey !== null) keys = neighbourhood(targetKey, around, maxDrawnNodes)
  else keys = [...byKey.keys()].sort(byDegree(around)).slice(0, maxDrawnNodes)

  const kept = new Set(keys)
  const nodes = graph.nodes.filter((node) => kept.has(keyOf(node)))
  return { nodes, edges: edgesWithin(graph.edges, kept, around, targetKey), total }
}
